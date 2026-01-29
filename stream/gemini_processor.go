package stream

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ==================== Gemini Stream Response Types ====================

// GeminiStreamResponse represents a single chunk from Gemini's streaming API
type GeminiStreamResponse struct {
	Candidates    []GeminiCandidate   `json:"candidates"`
	UsageMetadata *GeminiUsageMetadata `json:"usageMetadata,omitempty"`
}

// GeminiCandidate represents a response candidate
type GeminiCandidate struct {
	Content      *GeminiContentPart `json:"content,omitempty"`
	FinishReason string             `json:"finishReason,omitempty"` // "STOP", "MAX_TOKENS", etc.
	Index        int                `json:"index"`
}

// GeminiContentPart represents content in the response
type GeminiContentPart struct {
	Parts []GeminiPartResponse `json:"parts"`
	Role  string               `json:"role"` // "model"
}

// GeminiPartResponse represents a single part of the response
type GeminiPartResponse struct {
	Text             string                        `json:"text,omitempty"`
	FunctionCall     *GeminiFunctionCallResponse   `json:"functionCall,omitempty"`
	ThoughtSignature string                        `json:"thoughtSignature,omitempty"` // At Part level for Gemini 3
}

// GeminiFunctionCallResponse represents a function call from Gemini
type GeminiFunctionCallResponse struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// GeminiUsageMetadata contains token usage information
type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// ==================== Gemini Stream Processor ====================

// GeminiStreamProcessor processes Gemini streaming responses
type GeminiStreamProcessor struct {
	accumulatedText string
	toolCallIDs     map[string]string // Map tool names to generated IDs
	lastUsage       *GeminiUsageMetadata
	pendingEvents   []*StreamEvent // Queue for multi-event responses (tool_call + tool_use)
}

// NewGeminiStreamProcessor creates a new Gemini stream processor
func NewGeminiStreamProcessor() *GeminiStreamProcessor {
	return &GeminiStreamProcessor{
		toolCallIDs:   make(map[string]string),
		pendingEvents: make([]*StreamEvent, 0),
	}
}

// HasPendingEvents returns true if there are queued events waiting to be emitted
func (p *GeminiStreamProcessor) HasPendingEvents() bool {
	return len(p.pendingEvents) > 0
}

// ProcessLine processes a single line from the Gemini stream
func (p *GeminiStreamProcessor) ProcessLine(line string) (*StreamEvent, error) {
	// Check for pending events first (e.g., tool_use after tool_call)
	if len(p.pendingEvents) > 0 {
		event := p.pendingEvents[0]
		p.pendingEvents = p.pendingEvents[1:]
		return event, nil
	}

	// Gemini sends SSE format: "data: {...}"
	if !strings.HasPrefix(line, "data: ") {
		return nil, nil // Skip non-data lines
	}

	// Extract JSON data
	jsonData := strings.TrimPrefix(line, "data: ")
	if jsonData == "" || jsonData == "[DONE]" {
		return nil, nil
	}

	// Parse the JSON response
	var response GeminiStreamResponse
	if err := json.Unmarshal([]byte(jsonData), &response); err != nil {
		log.Printf("⚠️  Failed to parse Gemini response: %v, line: %s", err, jsonData)
		return nil, err
	}

	// Store usage metadata if present (usually in final chunk)
	if response.UsageMetadata != nil {
		p.lastUsage = response.UsageMetadata
	}

	// Process candidates
	if len(response.Candidates) == 0 {
		return nil, nil
	}

	candidate := response.Candidates[0]

	// Check for finish reason (end of stream)
	if candidate.FinishReason != "" {
		log.Printf("🏁 Gemini stream finished: reason=%s", candidate.FinishReason)

		// Send usage event if we have metadata
		if p.lastUsage != nil {
			return &StreamEvent{
				Type:         "usage",
				InputTokens:  p.lastUsage.PromptTokenCount,
				OutputTokens: p.lastUsage.CandidatesTokenCount,
				TotalTokens:  p.lastUsage.TotalTokenCount,
				Timestamp:    time.Now().UnixMilli(),
			}, nil
		}

		return &StreamEvent{
			Type:      "stop",
			Content:   "",
			Timestamp: time.Now().UnixMilli(),
		}, nil
	}

	// Process content if present
	if candidate.Content != nil && len(candidate.Content.Parts) > 0 {
		for i, part := range candidate.Content.Parts {
			// DEBUG: Log raw part structure
			partJSON, _ := json.Marshal(part)
			log.Printf("🔍 Gemini Part[%d] raw: %s", i, string(partJSON))

			// Handle text content
			if part.Text != "" {
				p.accumulatedText += part.Text
				return &StreamEvent{
					Type:      "content",
					Content:   part.Text,
					Timestamp: time.Now().UnixMilli(),
				}, nil
			}

			// Handle function calls (tool use)
			if part.FunctionCall != nil {
				// Generate a unique tool ID (Gemini doesn't provide one)
				toolID := p.getOrCreateToolID(part.FunctionCall.Name)

				// ThoughtSignature is at Part level, not inside FunctionCall
				log.Printf("🔧 Gemini function call: name=%s, id=%s, args=%v",
					part.FunctionCall.Name, toolID, part.FunctionCall.Args)
				log.Printf("🔧 Gemini thoughtSignature present: %v, length: %d",
					part.ThoughtSignature != "", len(part.ThoughtSignature))
				if part.ThoughtSignature != "" {
					log.Printf("🔧 Gemini thoughtSignature preview: %.100s...", part.ThoughtSignature)
				}

				// Queue tool_use event (to be emitted after tool_call)
				p.pendingEvents = append(p.pendingEvents, &StreamEvent{
					Type:             "tool_use",
					ToolName:         part.FunctionCall.Name,
					ToolID:           toolID,
					ToolInput:        part.FunctionCall.Args,
					ThoughtSignature: part.ThoughtSignature, // At Part level for Gemini 3
					Timestamp:        time.Now().UnixMilli(),
				})

				// Return tool_call event first (for UI consistency with Anthropic)
				return &StreamEvent{
					Type:      "tool_call",
					ToolName:  part.FunctionCall.Name,
					ToolID:    toolID,
					Timestamp: time.Now().UnixMilli(),
				}, nil
			}
		}
	}

	return nil, nil
}

// IsComplete checks if the stream is complete
func (p *GeminiStreamProcessor) IsComplete(line string) bool {
	// Gemini uses finishReason to indicate completion
	// This is a backup check in case ProcessLine didn't catch it
	return strings.Contains(line, `"finishReason"`)
}

// getOrCreateToolID generates a unique tool ID for each function call
// Gemini doesn't provide tool IDs, so we generate them locally
// Each call gets a unique ID, even for the same tool name
func (p *GeminiStreamProcessor) getOrCreateToolID(toolName string) string {
	// Always generate a new unique ID for each tool call
	// This ensures multiple calls to the same tool get different IDs
	id := fmt.Sprintf("toolu_%s", uuid.New().String()[:8])
	p.toolCallIDs[toolName] = id // Store for reference (e.g., tool results)
	return id
}

// Reset resets the processor state (useful for new conversations)
func (p *GeminiStreamProcessor) Reset() {
	p.accumulatedText = ""
	p.toolCallIDs = make(map[string]string)
	p.lastUsage = nil
	p.pendingEvents = make([]*StreamEvent, 0)
}
