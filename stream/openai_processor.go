package stream

import (
	"encoding/json"
	"log"
	"strings"
)

type OpenAIProcessor struct{
	hasStarted            bool
	toolCallStarted       map[int]bool // Track which tool call indices have started
	pendingUsage          *OpenAIUsage // Store usage when we need to emit tool_use first
	pendingContent        string // Store content from start chunk (xAI sends role+content together)

	// Track multiple parallel tool calls by index
	toolIDs        map[int]string
	toolNames      map[int]string
	toolArguments  map[int]string
	pendingToolUse []*StreamEvent // Queue for emitting multiple tool_use events

	// Accumulated reasoning content for thinking models (Kimi K2, etc.)
	// Must be preserved in conversation history for multi-step tool calls
	accumulatedReasoning string

	// Accumulated reasoning_details for MiniMax M2.5 (structured reasoning preservation)
	accumulatedReasoningDetails []interface{}

	// Track if we're inside <think> tags (for Together AI K2.5 which embeds thinking in content)
	inThinkTag       bool
	startInThinkMode bool   // Remember initial setting for reset between turns
	thinkBuffer      string // Buffer for content that might contain partial tags
}

type OpenAIStreamData struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"` // Token usage in final chunk
}

type OpenAIUsage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	PromptTokensDetails     *OpenAIPromptDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *OpenAICompletionDetails `json:"completion_tokens_details,omitempty"`
}

type OpenAIPromptDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type OpenAICompletionDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Delta        OpenAIDelta   `json:"delta"`
	FinishReason *string       `json:"finish_reason"`
}

type OpenAIDelta struct {
	Role             string           `json:"role,omitempty"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"` // For thinking/reasoning models (Fireworks Kimi K2, etc.)
	Reasoning        string           `json:"reasoning,omitempty"`         // For thinking/reasoning models (Together AI Kimi K2, etc.)
	ReasoningDetails []interface{}    `json:"reasoning_details,omitempty"` // For MiniMax M2.5 structured reasoning
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
}

type OpenAIToolCall struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function OpenAIFunctionCall    `json:"function"`
}

type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (p *OpenAIProcessor) ProcessLine(line string) (*StreamEvent, error) {
	// Initialize maps if needed
	if p.toolIDs == nil {
		p.toolIDs = make(map[int]string)
		p.toolNames = make(map[int]string)
		p.toolArguments = make(map[int]string)
		p.toolCallStarted = make(map[int]bool)
	}

	// Check if we have pending tool_use events to emit first
	if len(p.pendingToolUse) > 0 {
		event := p.pendingToolUse[0]
		p.pendingToolUse = p.pendingToolUse[1:]
		return event, nil
	}

	// OpenAI format: "data: {JSON}"
	if !strings.HasPrefix(line, "data: ") {
		return nil, nil // Skip non-data lines
	}

	// Extract JSON part
	jsonStr := strings.TrimPrefix(line, "data: ")
	if jsonStr == "[DONE]" {
		return nil, nil // Skip DONE marker
	}

	// Parse OpenAI response
	var openaiData OpenAIStreamData
	if err := json.Unmarshal([]byte(jsonStr), &openaiData); err != nil {
		return nil, err
	}

	// Log first reasoning chunk per stream as sample (avoid per-token log spam)
	if len(openaiData.Choices) > 0 && p.accumulatedReasoning == "" && len(jsonStr) < 500 {
		delta := openaiData.Choices[0].Delta
		if delta.ReasoningContent != "" || delta.Reasoning != "" || len(delta.ReasoningDetails) > 0 {
			log.Printf("🧠 First reasoning chunk: %s", jsonStr)
		}
	}

	// Check for usage data (comes in final chunk before [DONE])
	// But first, check if we have pending tool data that needs to be emitted
	if openaiData.Usage != nil {
		// If we have accumulated tool data, emit tool_use events first
		if len(p.toolIDs) > 0 {
			log.Printf("🔧 Usage received but have %d pending tools - emitting tool_use first", len(p.toolIDs))

			// Store usage for later
			p.pendingUsage = openaiData.Usage

			// Build and emit all tool_use events
			return p.emitAllToolEvents()
		}

		return openaiUsageToStreamEvent(openaiData.Usage), nil
	}

	// Check if we have pending usage to emit
	if p.pendingUsage != nil {
		usage := p.pendingUsage
		p.pendingUsage = nil
		return openaiUsageToStreamEvent(usage), nil
	}

	// Check if we have pending content from a start chunk (xAI sends role+content together)
	// We prepend it to whatever content comes next, so we don't lose either
	pendingContent := ""
	if p.pendingContent != "" {
		pendingContent = p.pendingContent
		p.pendingContent = ""
	}

	// Extract content from choices
	content := ""
	eventType := "content"

	if len(openaiData.Choices) > 0 {
		choice := openaiData.Choices[0]

		// Check if this is a finish event (including tool_calls finish)
		if choice.FinishReason != nil {
			log.Printf("🔧 OpenAI finish_reason: %s (pending tools: %d)", *choice.FinishReason, len(p.toolIDs))

			// Handle tool_calls finish OR stop with accumulated tool data
			// Some OpenAI-compatible APIs (like Fireworks) may use "stop" instead of "tool_calls"
			if *choice.FinishReason == "tool_calls" ||
				(*choice.FinishReason == "stop" && len(p.toolIDs) > 0) {
				// Tool calls complete, emit all tool_use events
				if len(p.toolIDs) > 0 {
					return p.emitAllToolEvents()
				}
				// If no tool data, just treat as stop
				eventType = "stop"
			} else if *choice.FinishReason == "stop" {
				eventType = "stop"
			}
		} else {
			content = choice.Delta.Content
			// Support both reasoning_content (Fireworks) and reasoning (Together AI) field names
			reasoningContent := choice.Delta.ReasoningContent
			if reasoningContent == "" {
				reasoningContent = choice.Delta.Reasoning
			}

			// Only emit start event once
			if choice.Delta.Role != "" && !p.hasStarted {
				eventType = "start"
				p.hasStarted = true
				// xAI/Grok sends role and content in the same chunk
				// Store content to emit after start event
				if content != "" {
					p.pendingContent = content
					content = "" // Don't include in start event
				}
			} else if reasoningContent != "" {
				// Handle reasoning/thinking content from thinking models (Kimi K2, etc.)
				// Accumulate for later use in conversation history (required by Kimi K2 for multi-step tool calls)
				p.accumulatedReasoning += reasoningContent
				// Emit as "thinking" event so client can display appropriately
				return &StreamEvent{
					Type:    "thinking",
					Content: reasoningContent,
				}, nil
			} else if len(choice.Delta.ReasoningDetails) > 0 {
				// Handle reasoning_details (MiniMax M2.5 with reasoning_split=true)
				// Each entry is {"type": "reasoning.text", "text": "...", ...}
				// Accumulate ALL entries and emit events for each (queue extras in pendingToolUse)
				var detailEvents []*StreamEvent
				for _, detail := range choice.Delta.ReasoningDetails {
					p.accumulatedReasoningDetails = append(p.accumulatedReasoningDetails, detail)
					if detailMap, ok := detail.(map[string]interface{}); ok {
						if text, ok := detailMap["text"].(string); ok && text != "" {
							p.accumulatedReasoning += text
							detailEvents = append(detailEvents, &StreamEvent{
								Type:    "thinking",
								Content: text,
							})
						}
					}
				}
				if len(detailEvents) > 0 {
					// Queue extra events if multiple details had text
					if len(detailEvents) > 1 {
						p.pendingToolUse = append(p.pendingToolUse, detailEvents[1:]...)
					}
					return detailEvents[0], nil
				}
			} else if len(choice.Delta.ToolCalls) > 0 {
				// Handle tool calls - OpenAI streams them incrementally
				// Process ALL tool calls in the array (for parallel tool calls)
				var eventsToEmit []*StreamEvent

				for _, toolCall := range choice.Delta.ToolCalls {
					idx := toolCall.Index

					// Check if this is the start of a tool call (has name)
					if toolCall.Function.Name != "" {
						// Store tool info by index
						p.toolIDs[idx] = toolCall.ID
						p.toolNames[idx] = toolCall.Function.Name
						p.toolArguments[idx] = "" // Reset arguments accumulator
						p.toolCallStarted[idx] = true

						log.Printf("🔧 Tool call started: index=%d, name=%s, id=%s", idx, toolCall.Function.Name, toolCall.ID)

						// Queue tool_call event
						eventsToEmit = append(eventsToEmit, &StreamEvent{
							Type:     "tool_call",
							ToolName: toolCall.Function.Name,
							ToolID:   toolCall.ID,
							Content:  "",
						})
					}

					// Check if this is a tool input delta (has arguments)
					if toolCall.Function.Arguments != "" {
						// Accumulate arguments for this tool index
						p.toolArguments[idx] += toolCall.Function.Arguments

						// Queue tool_input_delta event
						eventsToEmit = append(eventsToEmit, &StreamEvent{
							Type:    "tool_input_delta",
							ToolID:  p.toolIDs[idx],
							Content: toolCall.Function.Arguments,
						})
					}
				}

				// If we have events to emit, return the first and queue the rest
				if len(eventsToEmit) > 0 {
					if len(eventsToEmit) > 1 {
						p.pendingToolUse = append(p.pendingToolUse, eventsToEmit[1:]...)
					}
					return eventsToEmit[0], nil
				}

				// If no events generated, skip
				return nil, nil
			} else if content != "" || pendingContent != "" {
				// Prepend any pending content from start chunk
				content = pendingContent + content

				// Handle <think> tags embedded in content (Together AI K2.5)
				// K2.5 often starts thinking without <think> tag but ends with </think>
				return p.processContentWithThinkTags(content)
			} else {
				// Skip empty content events
				return nil, nil
			}
		}
	}

	return &StreamEvent{
		Type:    eventType,
		Content: content,
	}, nil
}

// processContentWithThinkTags handles content that may contain <think>...</think> tags
// Together AI K2.5 embeds thinking in content this way instead of using a separate reasoning field
func (p *OpenAIProcessor) processContentWithThinkTags(content string) (*StreamEvent, error) {
	// Fast path: if not in think mode and content has no angle brackets, skip tag processing entirely
	// This avoids getPartialTagSuffix() overhead for models that don't use think tags (MiniMax, etc.)
	if !p.inThinkTag && !p.startInThinkMode && p.thinkBuffer == "" && !strings.ContainsAny(content, "<>") {
		return &StreamEvent{
			Type:    "content",
			Content: content,
		}, nil
	}

	// Prepend any buffered content from a previous tag transition or partial tag
	if p.thinkBuffer != "" {
		content = p.thinkBuffer + content
		p.thinkBuffer = ""
	}

	// Check for partial tags at the end of content that might span chunks
	// Buffer them for the next call to avoid missing split tags
	if partial := getPartialTagSuffix(content); partial != "" {
		p.thinkBuffer = partial
		content = content[:len(content)-len(partial)]
		if content == "" {
			return nil, nil // Wait for more data
		}
	}

	// Check for </think> tag - this marks the end of thinking
	if strings.Contains(content, "</think>") {
		parts := strings.SplitN(content, "</think>", 2)
		thinkContent := parts[0]
		afterThink := ""
		if len(parts) > 1 {
			afterThink = parts[1]
		}

		// If we were in think mode OR this chunk has thinking content before </think>
		if p.inThinkTag || thinkContent != "" {
			p.inThinkTag = false
			// Accumulate the thinking content
			if thinkContent != "" {
				p.accumulatedReasoning += thinkContent
			}

			// If there's content after </think>, queue it as a pending event
			if afterThink != "" {
				p.pendingToolUse = append(p.pendingToolUse, &StreamEvent{
					Type:    "content",
					Content: afterThink,
				})
			}

			// Emit the final thinking chunk (if any)
			if thinkContent != "" {
				return &StreamEvent{
					Type:    "thinking",
					Content: thinkContent,
				}, nil
			}

			// If no thinking content but we have queued after-think content, emit it
			if len(p.pendingToolUse) > 0 {
				event := p.pendingToolUse[0]
				p.pendingToolUse = p.pendingToolUse[1:]
				return event, nil
			}
			return nil, nil
		}
	}

	// Check for <think> tag - this marks the start of thinking
	if strings.Contains(content, "<think>") {
		parts := strings.SplitN(content, "<think>", 2)
		beforeThink := parts[0]
		afterThink := ""
		if len(parts) > 1 {
			afterThink = parts[1]
		}

		p.inThinkTag = true

		// If there's content before <think>, emit it first, queue thinking content
		if beforeThink != "" {
			if afterThink != "" {
				p.accumulatedReasoning += afterThink
				p.pendingToolUse = append(p.pendingToolUse, &StreamEvent{
					Type:    "thinking",
					Content: afterThink,
				})
			}
			return &StreamEvent{
				Type:    "content",
				Content: beforeThink,
			}, nil
		}

		// Emit thinking content
		if afterThink != "" {
			p.accumulatedReasoning += afterThink
			return &StreamEvent{
				Type:    "thinking",
				Content: afterThink,
			}, nil
		}
		return nil, nil
	}

	// If we're inside think tags, emit as thinking
	if p.inThinkTag {
		p.accumulatedReasoning += content
		return &StreamEvent{
			Type:    "thinking",
			Content: content,
		}, nil
	}

	// Regular content
	return &StreamEvent{
		Type:    "content",
		Content: content,
	}, nil
}

// partialTagSuffixes is checked from longest to shortest to find partial <think>/<\/think> at chunk boundaries
var partialTagSuffixes = []string{
	"</think", "</thin", "</thi", "</th", "</t",
	"<think", "<thin", "<thi", "<th", "<t",
	"</", "<",
}

// getPartialTagSuffix checks if content ends with a partial <think> or </think> tag
// Returns the partial suffix that should be buffered for the next chunk
func getPartialTagSuffix(content string) string {
	for _, suffix := range partialTagSuffixes {
		if strings.HasSuffix(content, suffix) {
			// For bare "<", skip if it's part of a complete tag
			if suffix == "<" && (strings.HasSuffix(content, "<think>") || strings.HasSuffix(content, "</think>")) {
				continue
			}
			return suffix
		}
	}
	return ""
}

func (p *OpenAIProcessor) IsComplete(line string) bool {
	return strings.Contains(line, "data: [DONE]")
}

// SetStartInThinkMode sets the processor to start in thinking mode
// This is needed for models like Together AI K2.5 that start thinking without <think> tag
func (p *OpenAIProcessor) SetStartInThinkMode(start bool) {
	p.inThinkTag = start
	p.startInThinkMode = start // Remember the initial setting
}

// ResetForNewTurn resets the processor state for a new conversation turn
// This is needed when the same processor is reused across multiple iterations (e.g., after tool calls)
func (p *OpenAIProcessor) ResetForNewTurn() {
	p.hasStarted = false
	p.inThinkTag = p.startInThinkMode // Reset to initial think mode setting
	p.thinkBuffer = ""
	p.pendingContent = ""  // Clear stale content from previous turn
	p.pendingUsage = nil   // Clear stale usage from previous turn
	// Don't clear accumulatedReasoning - it's managed by ClearAccumulatedReasoning after use
}

// HasPendingEvents returns true if there are queued events waiting to be emitted
func (p *OpenAIProcessor) HasPendingEvents() bool {
	return len(p.pendingToolUse) > 0 || p.pendingUsage != nil
}

// GetAccumulatedReasoning returns the accumulated reasoning content without clearing it
// This is used to preserve reasoning_content in conversation history for thinking models (Kimi K2, etc.)
func (p *OpenAIProcessor) GetAccumulatedReasoning() string {
	return p.accumulatedReasoning
}

// ClearAccumulatedReasoning clears the accumulated reasoning content
// Called after the reasoning has been used for the next API call
func (p *OpenAIProcessor) ClearAccumulatedReasoning() {
	p.accumulatedReasoning = ""
}

// GetAccumulatedReasoningDetails returns the accumulated reasoning_details entries
// This is used to preserve reasoning_details in conversation history for MiniMax M2.5
func (p *OpenAIProcessor) GetAccumulatedReasoningDetails() []interface{} {
	return p.accumulatedReasoningDetails
}

// ClearAccumulatedReasoningDetails clears the accumulated reasoning_details
// Called after they have been attached to the assistant message for the next API call
func (p *OpenAIProcessor) ClearAccumulatedReasoningDetails() {
	p.accumulatedReasoningDetails = nil
}

// emitAllToolEvents creates tool_use events for all tracked tools
// Returns the first event and queues the rest in pendingToolUse
func (p *OpenAIProcessor) emitAllToolEvents() (*StreamEvent, error) {
	if len(p.toolIDs) == 0 {
		return nil, nil
	}

	var events []*StreamEvent

	// Build tool_use events for all tracked tools (iterate by index order)
	// Find max index first
	maxIdx := 0
	for idx := range p.toolIDs {
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	// Emit in order
	for idx := 0; idx <= maxIdx; idx++ {
		toolID, exists := p.toolIDs[idx]
		if !exists {
			continue
		}
		toolName := p.toolNames[idx]
		toolArgs := p.toolArguments[idx]

		// Parse the accumulated JSON arguments
		toolInput := make(map[string]interface{})
		if toolArgs != "" {
			if err := json.Unmarshal([]byte(toolArgs), &toolInput); err != nil {
				log.Printf("⚠️ Failed to parse tool arguments for %s: %v, args: %s", toolName, err, toolArgs)
				toolInput = make(map[string]interface{})
			}
		}

		log.Printf("🔧 Emitting tool_use event: index=%d, name=%s, id=%s, input=%v", idx, toolName, toolID, toolInput)

		events = append(events, &StreamEvent{
			Type:      "tool_use",
			ToolName:  toolName,
			ToolID:    toolID,
			ToolInput: toolInput,
			Content:   "",
		})
	}

	// Clear tool state
	p.toolIDs = make(map[int]string)
	p.toolNames = make(map[int]string)
	p.toolArguments = make(map[int]string)
	p.toolCallStarted = make(map[int]bool)

	// Return first event, queue the rest (including a stop event at the end)
	if len(events) > 0 {
		if len(events) > 1 {
			p.pendingToolUse = append(p.pendingToolUse, events[1:]...)
		}
		// Queue a stop event after all tool_use events
		// This ensures parallel tool execution triggers in processor.go
		p.pendingToolUse = append(p.pendingToolUse, &StreamEvent{
			Type:    "stop",
			Content: "",
		})
		return events[0], nil
	}

	return nil, nil
}

// openaiUsageToStreamEvent converts OpenAI usage data to a StreamEvent,
// including cached token and reasoning token details when available.
func openaiUsageToStreamEvent(usage *OpenAIUsage) *StreamEvent {
	event := &StreamEvent{
		Type:         "usage",
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}
	if usage.PromptTokensDetails != nil {
		event.CacheReadTokens = usage.PromptTokensDetails.CachedTokens
	}
	if usage.CompletionTokensDetails != nil {
		event.ReasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
	}
	return event
}