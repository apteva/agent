package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
	"github.com/apteva/agent/mcp"
	"github.com/apteva/agent/operator"
	"github.com/apteva/agent/tools"
)

type Message struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"` // For thinking models (Kimi K2, etc.) - preserved in conversation history
}

type Provider interface {
	GetRawStream(messages []Message, customTools []tools.ToolDefinition, builtinTools []interface{}) (io.ReadCloser, error)
}

type MessageSaver interface {
	SaveMessage(threadID, role string, content interface{}, model *string, metadata map[string]interface{}) error
	CreateThread(title string) (string, error)
}

type ContentBlock struct {
	Type      string                 `json:"type"`                    // "text", "image", "document", "tool_use", "tool_result"
	Text      string                 `json:"text,omitempty"`          // for text blocks

	// Image/Document fields
	Source    *ContentSource         `json:"source,omitempty"`        // for image and document blocks

	// Tool fields
	ID               string                 `json:"id,omitempty"`               // for tool_use
	Name             string                 `json:"name,omitempty"`             // for tool_use
	Input            map[string]interface{} `json:"-"`                          // for tool_use - custom marshaling, see MarshalJSON
	ThoughtSignature string                 `json:"thought_signature,omitempty"` // for tool_use (Gemini 3 requires this)
	ToolUseID        string                 `json:"tool_use_id,omitempty"`      // for tool_result
	Content          interface{}            `json:"content,omitempty"`          // for tool_result (can be string or object)
	IsError          bool                   `json:"is_error,omitempty"`         // for tool_result
}

// MarshalJSON implements custom JSON marshaling for ContentBlock
// This ensures the 'input' field is only included for tool_use blocks (Anthropic requires it)
func (c ContentBlock) MarshalJSON() ([]byte, error) {
	type Alias ContentBlock
	aux := struct {
		Alias
		Input map[string]interface{} `json:"input,omitempty"`
	}{
		Alias: Alias(c),
	}

	// Only include input field for tool_use blocks
	if c.Type == "tool_use" {
		if c.Input == nil {
			aux.Input = map[string]interface{}{}
		} else {
			aux.Input = c.Input
		}
	}

	return json.Marshal(aux)
}

type ContentSource struct {
	Type      string `json:"type"`                     // "base64", "url", "file"
	MediaType string `json:"media_type,omitempty"`     // "image/jpeg", "application/pdf", etc.
	Data      string `json:"data,omitempty"`           // For base64
	URL       string `json:"url,omitempty"`            // For URL
	FileID    string `json:"file_id,omitempty"`        // For file reference
}

type StreamProcessor interface {
	ProcessLine(line string) (*StreamEvent, error)
	IsComplete(line string) bool
}

// PendingEventChecker is an optional interface for processors that queue events
type PendingEventChecker interface {
	HasPendingEvents() bool
}

// ReasoningAccumulator is an optional interface for processors that accumulate reasoning content
// Used by thinking models (Kimi K2, etc.) to preserve reasoning_content in conversation history
type ReasoningAccumulator interface {
	GetAccumulatedReasoning() string
	ClearAccumulatedReasoning()
}

// TurnResetter is an optional interface for processors that need to reset state between turns
// Used by thinking models that embed thinking in content (e.g., Together AI K2.5)
type TurnResetter interface {
	ResetForNewTurn()
}

// FileProcessor interface for filesystem operations
type FileProcessor interface {
	ProcessContentBlocks(blocks []ContentBlock, threadID, messageID, source string) ([]ContentBlock, error)
	ExtractImagesFromToolResult(result interface{}, threadID, source string) (interface{}, []string, error)
	IsEnabled() bool
}

type StreamEvent struct {
	Type              string                 `json:"type"`        // "content", "thinking", "start", "stop", "tool_use", "tool_result", "usage"
	Content           string                 `json:"content"`     // actual text content (for simple string results)
	ContentBlocks     interface{}            `json:"content_blocks,omitempty"` // for structured content (e.g., screenshot with image)
	ToolName          string                 `json:"tool_name,omitempty"`   // for tool_use events
	ToolDisplayName   string                 `json:"tool_display_name,omitempty"` // human-readable name like "Create Task"
	ToolID            string                 `json:"tool_id,omitempty"`     // for tool_use and tool_result events
	ToolInput         map[string]interface{} `json:"tool_input"`  // for tool_use events
	ToolResult        interface{}            `json:"tool_result,omitempty"` // for tool_result events (raw data for server tools)
	ThoughtSignature  string                 `json:"thought_signature,omitempty"` // for Gemini 3 function calls (required for context)
	Timestamp         int64                  `json:"timestamp"`   // Unix milliseconds timestamp

	// Token usage fields (for "usage" events)
	InputTokens       int                    `json:"input_tokens,omitempty"`
	OutputTokens      int                    `json:"output_tokens,omitempty"`
	TotalTokens       int                    `json:"total_tokens,omitempty"`
}

// sendCancelledEvent sends a cancellation SSE event to the client
func sendCancelledEvent(w http.ResponseWriter, reason string) {
	fmt.Fprintf(w, "data: {\"type\":\"cancelled\",\"reason\":\"%s\",\"timestamp\":%d}\n\n",
		reason, time.Now().UnixMilli())
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// UnifiedStreamResponse processes a raw stream and outputs unified format
func UnifiedStreamResponse(w http.ResponseWriter, rawReader io.Reader, processor StreamProcessor) error {
	// Set headers for streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	scanner := bufio.NewScanner(rawReader)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024) // Start 64KB, grow to 2MB max for large server tool results
	for scanner.Scan() {
		line := scanner.Text()

		// Process the line first
		event, err := processor.ProcessLine(line)
		if err != nil {
			// Skip lines that can't be processed (like ping events)
			continue
		}

		if event != nil {
				// Handle tool execution
			if event.Type == "tool_use" {
				// Output tool use event (use dynamic display name since we have full input)
				displayName := getDynamicToolDisplayName(event.ToolName, event.ToolInput)
				fmt.Fprintf(w, "data: {\"type\":\"tool_use\",\"tool_name\":\"%s\",\"tool_display_name\":\"%s\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
						event.ToolName, escapeJSON(displayName), event.ToolID, escapeJSON(event.Content), time.Now().UnixMilli())
					flusher.Flush()

					// Execute the tool
					tool, err := tools.GetTool(event.ToolName)
					if err != nil {
					log.Printf("Error getting tool %s: %v", event.ToolName, err)
					// Output error as tool result
					fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"Error: %s\",\"timestamp\":%d}\n\n",
						event.ToolID, escapeJSON(err.Error()), time.Now().UnixMilli())
				} else {
					// Publish tool invocation event to event bus
					eventBus := events.GetEventBus()
					toolStartTime := time.Now()
					toolEvent := events.NewEvent(events.CategoryTool, events.TypeToolInvocation, events.LevelInfo).
						WithData("tool_name", event.ToolName).
						WithData("tool_id", event.ToolID).
						WithData("input", event.ToolInput)
					eventBus.Publish(toolEvent)

					// Execute tool
					result, err := tool.Execute(event.ToolInput)
					if err != nil {
						log.Printf("Error executing tool %s: %v", event.ToolName, err)
						fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"Error: %s\",\"timestamp\":%d}\n\n",
							event.ToolID, escapeJSON(err.Error()), time.Now().UnixMilli())

						// Publish tool error event
						errorEvent := events.NewEvent(events.CategoryTool, events.TypeToolError, events.LevelError).
							WithData("tool_name", event.ToolName).
							WithData("tool_id", event.ToolID).
							WithError(err).
							WithDuration(toolStartTime)
						eventBus.Publish(errorEvent)
					} else {
						// Convert result to JSON string
						resultJSON, _ := json.Marshal(result)
						fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
							event.ToolID, escapeJSON(string(resultJSON)), time.Now().UnixMilli())

						// Publish tool result event (sanitized to prevent memory bloat from large base64 data)
						resultEvent := events.NewEvent(events.CategoryTool, events.TypeToolResult, events.LevelInfo).
							WithData("tool_name", event.ToolName).
							WithData("tool_id", event.ToolID).
							WithData("result", sanitizeForEvent(result)).
							WithDuration(toolStartTime)
						eventBus.Publish(resultEvent)
					}
				}
				flusher.Flush()
			} else if event.Type == "tool_result" {
				fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					event.ToolID, escapeJSON(event.Content), time.Now().UnixMilli())
				flusher.Flush()
			} else {
				fmt.Fprintf(w, "data: {\"type\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					event.Type, escapeJSON(event.Content), time.Now().UnixMilli())
				flusher.Flush()
			}

			// Check if this was a stop event to break after processing
			if event.Type == "stop" {
				break
			}
		}

		// Also check if stream is complete (as backup)
		if processor.IsComplete(line) {
			break
		}
	}

	// Drain any remaining pending events from the processor (for parallel tool calls)
	if checker, ok := processor.(PendingEventChecker); ok {
		for checker.HasPendingEvents() {
			event, err := processor.ProcessLine("")
			if err != nil || event == nil {
				break
			}
			// Stream the event
			fmt.Fprintf(w, "data: {\"type\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
				event.Type, escapeJSON(event.Content), time.Now().UnixMilli())
			flusher.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %w", err)
	}

	// No need to send completion event - processors handle their own stop events
	return nil
}

// getString safely extracts a string value from a map
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

// UnifiedToolConversationWithBuiltins handles a complete conversation with tool execution and message saving
func UnifiedToolConversationWithBuiltins(w http.ResponseWriter, provider Provider, processor StreamProcessor, messages []Message, customTools []tools.ToolDefinition, builtinTools []interface{}, messageSaver MessageSaver, threadID string, model *string, fileProcessor FileProcessor, taskID string) error {
	return UnifiedToolConversationWithContext(context.Background(), w, provider, processor, messages, customTools, builtinTools, messageSaver, threadID, "", model, fileProcessor, taskID)
}

// UnifiedToolConversationWithContext is like UnifiedToolConversationWithBuiltins but accepts a context for cancellation
func UnifiedToolConversationWithContext(ctx context.Context, w http.ResponseWriter, provider Provider, processor StreamProcessor, messages []Message, customTools []tools.ToolDefinition, builtinTools []interface{}, messageSaver MessageSaver, threadID string, requestID string, model *string, fileProcessor FileProcessor, taskID string) error {
	// Start conversation with current messages
	currentMessages := messages
	maxIterations := 10 // Prevent infinite loops

	for iteration := 0; iteration < maxIterations; iteration++ {
		// Check for cancellation at start of each iteration
		select {
		case <-ctx.Done():
			// Send cancellation event to client
			sendCancelledEvent(w, "request_cancelled")
			return ctx.Err()
		default:
		}
		// Calculate token attribution for this iteration
		tokenAttribution := CalculateTokenAttribution(currentMessages, customTools, builtinTools, 0)

		// Create a flat span for LLM API call (sibling to root span, not child)
		trace := events.GetCurrentTrace()
		var llmSpan *events.Span
		if trace != nil {
			cfg := config.GetConfig()
			llmConfig := cfg.GetLLMConfig()

			// Create flat sibling span at same level as handle_chat
			llmSpan = trace.StartSpan(fmt.Sprintf("llm_call_%d", iteration), events.SpanKindLLM).
				WithThreadID(threadID).
				WithTaskID(taskID).
				WithAttribute("provider", llmConfig.Provider).
				WithAttribute("model", llmConfig.Model).
				WithAttribute("iteration", iteration).
				WithAttribute("message_count", len(currentMessages)).
				WithAttribute("token_attribution", map[string]interface{}{
					"system_prompt":        tokenAttribution.SystemPrompt,
					"tool_definitions":     tokenAttribution.ToolDefinitions,
					"conversation_history": tokenAttribution.ConversationHistory,
					"memory_context":       tokenAttribution.MemoryContext,
					"user_message":         tokenAttribution.UserMessage,
					"estimated_total":      tokenAttribution.EstimatedTotal,
				})
		}

		// Get stream from provider (with retry for transient errors)
		var rawStream io.ReadCloser
		var err error
		maxRetries := 3
		for attempt := 0; attempt <= maxRetries; attempt++ {
			rawStream, err = provider.GetRawStream(currentMessages, customTools, builtinTools)
			if err == nil {
				break
			}
			if attempt < maxRetries && isTransientError(err) {
				delay := time.Duration(attempt+1) * 2 * time.Second
				log.Printf("🔄 Retrying provider request (attempt %d/%d) after %v: %v", attempt+2, maxRetries+1, delay, err)
				time.Sleep(delay)
				continue
			}
			if llmSpan != nil {
				llmSpan.RecordError(err).End()
			}
			return fmt.Errorf("error getting stream: %w", err)
		}

		// Process the stream and collect tool uses, save messages
		toolResults, assistantMessage, inputTokens, outputTokens, err := processStreamWithToolsAndSaveContext(ctx, w, rawStream, processor, messageSaver, threadID, requestID, model, fileProcessor, taskID)
		rawStream.Close()

		// Get accumulated reasoning content from processor (for thinking models like Kimi K2)
		var accumulatedReasoning string
		if reasoningAccum, ok := processor.(ReasoningAccumulator); ok {
			accumulatedReasoning = reasoningAccum.GetAccumulatedReasoning()
			if accumulatedReasoning != "" {
				log.Printf("🧠 Accumulated reasoning content: %d chars", len(accumulatedReasoning))
			}
		}

		if err != nil {
			// Check if cancelled - not a real error
			if ctx.Err() != nil {
				log.Printf("🛑 Stream cancelled by user")
				if llmSpan != nil {
					llmSpan.WithAttribute("cancelled", true).End()
				}
				return nil // Cancellation is not an error
			}
			log.Printf("🔴 Error processing stream: %v", err)
			if llmSpan != nil {
				llmSpan.RecordError(err).End()
			}
			return err
		}

		// End LLM span successfully with token usage and cost
		if llmSpan != nil {
			// Update token attribution with actual input tokens from LLM
			llmSpan.
				WithAttribute("tool_calls", len(toolResults)).
				WithAttribute("token_attribution_actual", inputTokens).
				WithTokenUsage(inputTokens, outputTokens).
				End()
		}

		log.Printf("🔵 Iteration %d complete: %d tool results, assistant_message_length=%d", iteration, len(toolResults), len(assistantMessage))

		// Check for cancellation after tool execution before starting next iteration
		select {
		case <-ctx.Done():
			log.Printf("🛑 Cancelled after tool execution in iteration %d", iteration)
			sendCancelledEvent(w, "request_cancelled")
			return nil
		default:
		}

		// If no tools were used, conversation is complete
		if len(toolResults) == 0 {
			log.Printf("✅ Conversation complete (no tool results)")
			break
		}

		log.Printf("🔄 Continuing conversation with %d tool results", len(toolResults))

		// DEBUG: Log current message count before adding tool messages
		log.Printf("📝 Current messages before adding tools: %d", len(currentMessages))

		// Build assistant message with text content + tool_use blocks
		var assistantContentBlocks []ContentBlock

		// Add text content if present (content generated before tool call)
		if assistantMessage != "" {
			assistantContentBlocks = append(assistantContentBlocks, ContentBlock{
				Type: "text",
				Text: assistantMessage,
			})
			log.Printf("📝 Including pre-tool text content (length=%d)", len(assistantMessage))
		}

		// Add tool use blocks
		for _, result := range toolResults {
			// Ensure input is never nil - use empty map if nil
			toolInput := result.ToolInput
			if toolInput == nil {
				toolInput = map[string]interface{}{}
			}

			// DEBUG: Log computer/browser tool result being added to conversation
			if result.ToolName == "computer" || result.ToolName == "browser" {
				inputJSON, _ := json.Marshal(toolInput)
				log.Printf("🖥️  COMPUTER TOOL - Adding to next turn: toolID=%s, input=%s", result.ToolID, string(inputJSON))
			}

			// Add tool use block to content
			log.Printf("🔍 Creating tool_use ContentBlock: name=%s, id=%s, hasThoughtSignature=%v, len=%d",
				result.ToolName, result.ToolID, result.ThoughtSignature != "", len(result.ThoughtSignature))

			assistantContentBlocks = append(assistantContentBlocks, ContentBlock{
				Type:             "tool_use",
				ID:               result.ToolID,
				Name:             result.ToolName,
				Input:            toolInput,
				ThoughtSignature: result.ThoughtSignature, // Preserve for Gemini 3
			})
		}

		// Add single assistant message with all content blocks (text + tool_use blocks)
		// Include reasoning_content if present (for thinking models like Kimi K2)
		assistantMsg := Message{
			Role:    "assistant",
			Content: assistantContentBlocks,
		}
		if accumulatedReasoning != "" {
			assistantMsg.ReasoningContent = accumulatedReasoning
			log.Printf("🧠 Including reasoning_content in assistant message: %d chars", len(accumulatedReasoning))
			// Clear reasoning after using it for the next API call
			if reasoningAccum, ok := processor.(ReasoningAccumulator); ok {
				reasoningAccum.ClearAccumulatedReasoning()
			}
		}
		currentMessages = append(currentMessages, assistantMsg)
		log.Printf("📝 Added assistant message with %d content blocks", len(assistantContentBlocks))

		// Add tool result messages
		for _, result := range toolResults {
			resultContent := result.Content
			resultPreview := resultContent
			if len(resultPreview) > 100 {
				resultPreview = resultPreview[:100] + "..."
			}

			// Determine content to use: ContentBlocks (structured) or Content (string)
			var toolResultContent interface{}
			if result.ContentBlocks != nil {
				toolResultContent = result.ContentBlocks
				log.Printf("📝 Adding tool_result: tool=%s, content_type=blocks, preview=%s",
					result.ToolName, resultPreview)

				// Extract files from tool result content blocks if filesystem is enabled
				// Skip for MCP tools — ProcessMCPToolResult already handled filesystem extraction
				// and built proper vision blocks with base64 data for the LLM
				if fileProcessor != nil && fileProcessor.IsEnabled() && !isMCPTool(result.ToolName) {
					// Try to convert to []ContentBlock for processing
					if blocksArray, ok := result.ContentBlocks.([]interface{}); ok {
						var contentBlocks []ContentBlock
						for _, block := range blocksArray {
							if blockMap, ok := block.(map[string]interface{}); ok {
								// Convert map to ContentBlock
								cb := ContentBlock{
									Type: getString(blockMap, "type"),
									Text: getString(blockMap, "text"),
								}
								// Handle source
								if sourceMap, ok := blockMap["source"].(map[string]interface{}); ok {
									cb.Source = &ContentSource{
										Type:      getString(sourceMap, "type"),
										MediaType: getString(sourceMap, "media_type"),
										Data:      getString(sourceMap, "data"),
										URL:       getString(sourceMap, "url"),
										FileID:    getString(sourceMap, "file_id"),
									}
								}
								contentBlocks = append(contentBlocks, cb)
							}
						}

						// Process for file extraction
						if len(contentBlocks) > 0 {
							extractedBlocks, err := fileProcessor.ProcessContentBlocks(contentBlocks, threadID, "", "tool_result")
							if err != nil {
								log.Printf("⚠️  Failed to process tool result for file extraction: %v", err)
							} else {
								// Convert back to []interface{} for toolResultContent
								var extractedArray []interface{}
								for _, eb := range extractedBlocks {
									blockMap := map[string]interface{}{
										"type": eb.Type,
									}
									if eb.Text != "" {
										blockMap["text"] = eb.Text
									}
									if eb.Source != nil {
										blockMap["source"] = map[string]interface{}{
											"type":       eb.Source.Type,
											"media_type": eb.Source.MediaType,
											"data":       eb.Source.Data,
											"url":        eb.Source.URL,
											"file_id":    eb.Source.FileID,
										}
									}
									extractedArray = append(extractedArray, blockMap)
								}
								toolResultContent = extractedArray
								log.Printf("📁 Processed tool result content blocks for file extraction")
							}
						}
					}
				}
			} else {
				toolResultContent = result.Content
				log.Printf("📝 Adding tool_result: tool=%s, content_length=%d, preview=%s",
					result.ToolName, len(resultContent), resultPreview)

				// Also check if Content is an array of blocks
				if fileProcessor != nil && fileProcessor.IsEnabled() {
					contentStr := result.Content
					if contentStr != "" {
						// Try to parse as JSON array
						var contentArray []interface{}
						if err := json.Unmarshal([]byte(contentStr), &contentArray); err == nil {
							// Successfully parsed as array, process for file extraction
							var contentBlocks []ContentBlock
							for _, block := range contentArray {
								if blockMap, ok := block.(map[string]interface{}); ok {
									cb := ContentBlock{
										Type: getString(blockMap, "type"),
										Text: getString(blockMap, "text"),
									}
									if sourceMap, ok := blockMap["source"].(map[string]interface{}); ok {
										cb.Source = &ContentSource{
											Type:      getString(sourceMap, "type"),
											MediaType: getString(sourceMap, "media_type"),
											Data:      getString(sourceMap, "data"),
											URL:       getString(sourceMap, "url"),
											FileID:    getString(sourceMap, "file_id"),
										}
									}
									contentBlocks = append(contentBlocks, cb)
								}
							}

							if len(contentBlocks) > 0 {
								extractedBlocks, err := fileProcessor.ProcessContentBlocks(contentBlocks, threadID, "", "tool_result")
								if err != nil {
									log.Printf("⚠️  Failed to process tool result for file extraction: %v", err)
								} else {
									// Convert back to JSON string
									var extractedArray []interface{}
									for _, eb := range extractedBlocks {
										blockMap := map[string]interface{}{
											"type": eb.Type,
										}
										if eb.Text != "" {
											blockMap["text"] = eb.Text
										}
										if eb.Source != nil {
											blockMap["source"] = map[string]interface{}{
												"type":       eb.Source.Type,
												"media_type": eb.Source.MediaType,
												"data":       eb.Source.Data,
												"url":        eb.Source.URL,
												"file_id":    eb.Source.FileID,
											}
										}
										extractedArray = append(extractedArray, blockMap)
									}
									// Convert back to JSON string for result.Content format
									if jsonBytes, err := json.Marshal(extractedArray); err == nil {
										toolResultContent = string(jsonBytes)
										log.Printf("📁 Processed tool result Content string for file extraction")
									}
								}
							}
						}
					}
				}
			}

			currentMessages = append(currentMessages, Message{
				Role: "user",
				Content: []ContentBlock{{
					Type:      "tool_result",
					ToolUseID: result.ToolID,
					Content:   toolResultContent,
					IsError:   false, // TODO: track errors properly
				}},
			})
		}

		log.Printf("📝 Total messages after adding tools: %d", len(currentMessages))

		// Reset processor state for new turn (needed for thinking models that track state)
		if turnResetter, ok := processor.(TurnResetter); ok {
			turnResetter.ResetForNewTurn()
			log.Printf("🔄 Reset processor for new turn")
		}

		// DEBUG: Log the full conversation structure being sent
		log.Printf("📋 Full conversation structure for iteration %d:", iteration+1)
		for i, msg := range currentMessages {
			contentType := "unknown"
			contentPreview := ""

			switch v := msg.Content.(type) {
			case string:
				contentType = "text"
				contentPreview = v
				if len(contentPreview) > 80 {
					contentPreview = contentPreview[:80] + "..."
				}
			case []ContentBlock:
				if len(v) == 1 {
					// Single content block
					contentType = v[0].Type
					if v[0].Type == "text" {
						contentPreview = v[0].Text
						if len(contentPreview) > 80 {
							contentPreview = contentPreview[:80] + "..."
						}
					} else if v[0].Type == "tool_use" {
						contentPreview = fmt.Sprintf("tool=%s id=%s", v[0].Name, v[0].ID)
					} else if v[0].Type == "tool_result" {
						content := fmt.Sprintf("%v", v[0].Content)
						if len(content) > 80 {
							content = content[:80] + "..."
						}
						contentPreview = fmt.Sprintf("tool_use_id=%s content=%s", v[0].ToolUseID, content)
					}
				} else if len(v) > 1 {
					// Multiple content blocks (text + tool_use)
					contentType = fmt.Sprintf("mixed[%d blocks]", len(v))
					parts := []string{}
					for _, block := range v {
						if block.Type == "text" {
							text := block.Text
							if len(text) > 30 {
								text = text[:30] + "..."
							}
							parts = append(parts, fmt.Sprintf("text:'%s'", text))
						} else if block.Type == "tool_use" {
							parts = append(parts, fmt.Sprintf("tool:%s", block.Name))
						}
					}
					contentPreview = fmt.Sprintf("[%s]", strings.Join(parts, ", "))
					if len(contentPreview) > 80 {
						contentPreview = contentPreview[:80] + "..."
					}
				}
			}

			log.Printf("  [%d] %s: %s - %s", i, msg.Role, contentType, contentPreview)
		}

		// Continue conversation with tool results
	}

	// Send final DONE event to signal conversation completion
	flusher, ok := w.(http.Flusher)
	if ok {
		fmt.Fprintf(w, "data: {\"type\":\"done\",\"content\":\"\",\"timestamp\":%d}\n\n", time.Now().UnixMilli())
		flusher.Flush()
	}

	return nil
}

// processStreamWithToolsAndSave processes a stream, collects tool execution results, and saves messages
func processStreamWithToolsAndSave(w http.ResponseWriter, rawReader io.Reader, processor StreamProcessor, messageSaver MessageSaver, threadID string, model *string, fileProcessor FileProcessor) ([]StreamEvent, string, int, int, error) {
	return processStreamWithToolsAndSaveContext(context.Background(), w, rawReader, processor, messageSaver, threadID, "", model, fileProcessor, "")
}

func processStreamWithToolsAndSaveContext(ctx context.Context, w http.ResponseWriter, rawReader io.Reader, processor StreamProcessor, messageSaver MessageSaver, threadID string, requestID string, model *string, fileProcessor FileProcessor, taskID string) ([]StreamEvent, string, int, int, error) {
	// Set headers for streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, "", 0, 0, fmt.Errorf("streaming not supported")
	}

	var toolResults []StreamEvent
	var assistantContent string
	var reasoningContent string // Track reasoning/thinking content for thinking models (Kimi K2, etc.)
	var preToolContent string // Track content that came before tool use
	toolInputs := make(map[string]map[string]interface{}) // Track tool inputs by tool ID
	toolNames := make(map[string]string) // Track tool names by tool ID
	toolThoughtSignatures := make(map[string]string) // Track thought signatures by tool ID (for Gemini 3)
	threadIDSent := false // Track if we've sent the thread_id
	requestIDSent := false // Track if we've sent the request_id

	// Track token usage
	var usageInputTokens, usageOutputTokens int

	// Pending custom tools for parallel execution
	var pendingCustomTools []ToolCall

	// Send start event and thread_id immediately for all providers
	// This ensures the client gets the thread_id before any content
	if !threadIDSent {
		fmt.Fprintf(w, "data: {\"type\":\"start\",\"content\":\"\",\"timestamp\":%d}\n\n", time.Now().UnixMilli())
		flusher.Flush()
		fmt.Fprintf(w, "data: {\"type\":\"thread_id\",\"thread_id\":\"%s\",\"timestamp\":%d}\n\n", threadID, time.Now().UnixMilli())
		flusher.Flush()
		threadIDSent = true
	}
	if !requestIDSent && requestID != "" {
		fmt.Fprintf(w, "data: {\"type\":\"request_id\",\"request_id\":\"%s\",\"timestamp\":%d}\n\n", requestID, time.Now().UnixMilli())
		flusher.Flush()
		requestIDSent = true
	}

	scanner := bufio.NewScanner(rawReader)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024) // Start 64KB, grow to 2MB max for large server tool results
	for scanner.Scan() {
		// Check for cancellation - stop streaming to client but let LLM finish in background
		select {
		case <-ctx.Done():
			sendCancelledEvent(w, "request_cancelled")
			// Drain the rest of the stream in background (don't block)
			go func() {
				for scanner.Scan() {
					// Just consume the rest
				}
			}()
			return toolResults, assistantContent, usageInputTokens, usageOutputTokens, ctx.Err()
		default:
		}
		line := scanner.Text()

		// Process the line first
		event, err := processor.ProcessLine(line)
		if err != nil {
			// Skip lines that can't be processed (like ping events)
			continue
		}

		if event != nil {
			// Handle usage event (token usage from LLM)
			if event.Type == "usage" {
				// Store usage data
				usageInputTokens = event.InputTokens
				usageOutputTokens = event.OutputTokens

				// Publish usage event to event bus
				eventBus := events.GetEventBus()
				usageEvent := events.NewEvent(events.CategoryLLM, events.TypeLLMTokens, events.LevelInfo).
					WithThread(threadID).
					WithTask(taskID).
					WithData("input_tokens", event.InputTokens).
					WithData("output_tokens", event.OutputTokens).
					WithData("total_tokens", event.TotalTokens)

				// Add model if available
				if model != nil && *model != "" {
					usageEvent.WithData("model", *model)
				}

				// Attach to current span and trace if available
				if span := events.GetCurrentSpan(); span != nil {
					usageEvent.WithSpan(span.ID)
				}
				if trace := events.GetCurrentTrace(); trace != nil {
					usageEvent.WithTrace(trace.ID)
				}

				eventBus.Publish(usageEvent)

				// Stream usage to client
				fmt.Fprintf(w, "data: {\"type\":\"usage\",\"input_tokens\":%d,\"output_tokens\":%d,\"total_tokens\":%d,\"timestamp\":%d}\n\n",
					event.InputTokens, event.OutputTokens, event.TotalTokens, time.Now().UnixMilli())
				flusher.Flush()

				log.Printf("📊 Token usage: input=%d, output=%d, total=%d", event.InputTokens, event.OutputTokens, event.TotalTokens)
				continue // Don't add to tool results
			}

			// Handle tool_call event (emitted before tool execution)
			if event.Type == "tool_call" {
				// Just stream it to the user, don't execute anything yet
				displayName := getToolDisplayName(event.ToolName)
				fmt.Fprintf(w, "data: {\"type\":\"tool_call\",\"tool_name\":\"%s\",\"tool_display_name\":\"%s\",\"tool_id\":\"%s\",\"content\":\"\",\"timestamp\":%d}\n\n",
					event.ToolName, escapeJSON(displayName), event.ToolID, time.Now().UnixMilli())
				flusher.Flush()
			} else if event.Type == "tool_input_delta" {
				// Stream tool input deltas to the user
				fmt.Fprintf(w, "data: {\"type\":\"tool_input_delta\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					event.ToolID, escapeJSON(event.Content), time.Now().UnixMilli())
				flusher.Flush()
			} else if event.Type == "tool_use" {
				// Handle tool execution
				// Store tool input, name, and thought signature for later use
				toolInputs[event.ToolID] = event.ToolInput
				toolNames[event.ToolID] = event.ToolName
				if event.ThoughtSignature != "" {
					toolThoughtSignatures[event.ToolID] = event.ThoughtSignature
				}

				// DEBUG: Log computer/browser tool details
				if event.ToolName == "computer" || event.ToolName == "browser" {
					inputJSON, _ := json.Marshal(event.ToolInput)
					log.Printf("🖥️  COMPUTER TOOL - Received from LLM: tool=%s, id=%s, input=%s", event.ToolName, event.ToolID, string(inputJSON))
				}

				// Save the assistant message with text content if we have any
				if assistantContent != "" {
					if err := messageSaver.SaveMessage(threadID, "assistant", assistantContent, model, nil); err != nil {
						log.Printf("Error saving assistant message: %v", err)
					}
					// Store this content for including with tool_use in next iteration
					preToolContent = assistantContent
					assistantContent = "" // Reset for content after tool use
				}

				// Save tool use message as an array of content blocks (required by Anthropic)
				// Ensure input is preserved even if empty
				var inputValue interface{}
				if event.ToolInput != nil {
					inputValue = event.ToolInput
				} else {
					// Always use empty map instead of nil
					inputValue = map[string]interface{}{}
					log.Printf("Tool use %s has nil input, using empty map", event.ToolName)
				}

				toolUseBlock := map[string]interface{}{
					"type":  "tool_use",
					"id":    event.ToolID,
					"name":  event.ToolName,
					"input": inputValue,
				}

				// DEBUG: Log what we're saving to database
				if event.ToolName == "computer" || event.ToolName == "browser" {
					blockJSON, _ := json.Marshal(toolUseBlock)
					log.Printf("🖥️  COMPUTER TOOL - Saving to DB: %s", string(blockJSON))
				}

				// Wrap in array for assistant messages
				toolUseContent := []interface{}{toolUseBlock}

				// Check if processor has accumulated reasoning content (for thinking models like Kimi K2)
				var metadata map[string]interface{}
				if reasoningAccum, ok := processor.(ReasoningAccumulator); ok {
					// Peek at accumulated reasoning (don't clear it yet - we need it for the API call)
					if reasoning := reasoningAccum.GetAccumulatedReasoning(); reasoning != "" {
						metadata = map[string]interface{}{"reasoning_content": reasoning}
						log.Printf("🧠 Saving reasoning_content in metadata: %d chars", len(reasoning))
					}
				}

				if err := messageSaver.SaveMessage(threadID, "assistant", toolUseContent, model, metadata); err != nil {
					log.Printf("Error saving tool use message: %v", err)
				}

				// Output tool use event (use dynamic display name since we have full input)
				displayName := getDynamicToolDisplayName(event.ToolName, event.ToolInput)
				fmt.Fprintf(w, "data: {\"type\":\"tool_use\",\"tool_name\":\"%s\",\"tool_display_name\":\"%s\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					event.ToolName, escapeJSON(displayName), event.ToolID, escapeJSON(event.Content), time.Now().UnixMilli())
				flusher.Flush()

				// Tool execution handling
				if event.ToolName == "$web_search" {
					// Moonshot built-in $web_search: echo arguments back as tool result
					// The model handles the actual search internally
					log.Printf("🌐 Moonshot $web_search: echoing arguments back (model handles search internally)")
					inputJSON, _ := json.Marshal(event.ToolInput)
					toolResultContent := string(inputJSON)

					// Save tool result message
					toolResultBlock := map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": event.ToolID,
						"content":     toolResultContent,
					}
					toolResultArray := []interface{}{toolResultBlock}
					if err := messageSaver.SaveMessage(threadID, "user", toolResultArray, nil, nil); err != nil {
						log.Printf("Error saving $web_search tool result message: %v", err)
					}

					// Create tool result for conversation continuation
					toolResult := StreamEvent{
						Type:      "tool_result",
						ToolID:    event.ToolID,
						ToolName:  event.ToolName,
						ToolInput: event.ToolInput,
						Content:   toolResultContent,
					}
					toolResults = append(toolResults, toolResult)

					// Output tool result event
					fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
						event.ToolID, escapeJSON(toolResultContent), time.Now().UnixMilli())
					flusher.Flush()

				} else if isMCPTool(event.ToolName) {
					// Execute MCP tool
					cfg := config.GetConfig()
					mcpConfig := cfg.Get().MCP
					eventBus := events.GetEventBus()

					var toolResultContent string
					var isError bool

					// Create flat span for tool execution
					trace := events.GetCurrentTrace()
					var toolSpan *events.Span
					if trace != nil {
						toolSpan = trace.StartSpan(fmt.Sprintf("tool_%s", event.ToolName), events.SpanKindTool).
							WithThreadID(threadID).
							WithTaskID(taskID).
							WithAttribute("tool_name", event.ToolName).
							WithAttribute("tool_id", event.ToolID).
							WithAttribute("tool_type", "mcp")
					}

					// Publish tool invocation event
					toolStartTime := time.Now()
					toolEvent := events.NewEvent(events.CategoryMCP, events.TypeMCPToolExecution, events.LevelInfo).
						WithThread(threadID).
						WithData("tool_name", event.ToolName).
						WithData("tool_id", event.ToolID).
						WithData("input", event.ToolInput)
					eventBus.Publish(toolEvent)

					// Create MCP notification callback for streaming progress/log events
					mcpNotifyCallback := func(n mcp.MCPNotification) {
						switch n.Type {
						case "progress":
							fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"progress\",\"tool_id\":\"%s\",\"content\":\"%s\",\"progress\":%.2f,\"timestamp\":%d}\n\n",
								event.ToolID, escapeJSON(n.Message), n.Progress, time.Now().UnixMilli())
							flusher.Flush()
						case "log":
							fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"log\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
								event.ToolID, escapeJSON(n.Message), time.Now().UnixMilli())
							flusher.Flush()
						}
					}
					result, err := mcp.ExecuteToolStreamingWithContext(ctx, event.ToolName, event.ToolInput, mcpConfig, threadID, mcpNotifyCallback)
					if err != nil {
						log.Printf("Error executing MCP tool %s: %v", event.ToolName, err)
						toolResultContent = fmt.Sprintf("Error: %s", err.Error())
						isError = true

						// End span with error
						if toolSpan != nil {
							toolSpan.RecordError(err).End()
						}

						// Publish MCP error event
						errorEvent := events.NewEvent(events.CategoryMCP, events.TypeMCPError, events.LevelError).
							WithThread(threadID).
							WithData("tool_name", event.ToolName).
							WithData("tool_id", event.ToolID).
							WithError(err).
							WithDuration(toolStartTime)
						eventBus.Publish(errorEvent)
					} else {
						// Process MCP tool result with vision/filesystem awareness
						visionEnabled := false
						if agentCfg := config.GetConfig().Get(); agentCfg.LLM.Vision != nil {
							visionEnabled = agentCfg.LLM.Vision.Enabled
						}

						toolContent, toolContentStr, fileIDs := ProcessMCPToolResult(result, visionEnabled, fileProcessor, threadID, event.ToolName)
						toolResultContent = toolContentStr
						isError = false

						if len(fileIDs) > 0 {
							log.Printf("📁 Extracted %d file(s) from MCP tool %s result", len(fileIDs), event.ToolName)
						}

						// End span successfully
						if toolSpan != nil {
							toolSpan.WithAttribute("result_size", len(toolResultContent)).End()
						}

						// Publish successful MCP tool result event (sanitized)
						resultEvent := events.NewEvent(events.CategoryMCP, events.TypeMCPToolExecution, events.LevelInfo).
							WithThread(threadID).
							WithData("tool_name", event.ToolName).
							WithData("tool_id", event.ToolID).
							WithData("result", sanitizeForEvent(result)).
							WithData("success", true).
							WithDuration(toolStartTime)
						eventBus.Publish(resultEvent)

						// Save tool result — use structured content (with image blocks) if available
						toolResultBlock := map[string]interface{}{
							"type":        "tool_result",
							"tool_use_id": event.ToolID,
							"content":     toolContent, // string or []interface{} with content blocks
							"is_error":    isError,
						}
						toolResultArray := []interface{}{toolResultBlock}
						if err := messageSaver.SaveMessage(threadID, "user", toolResultArray, nil, nil); err != nil {
							log.Printf("Error saving MCP tool result message: %v", err)
						}

						// Create tool result for conversation continuation
						toolResult := StreamEvent{
							Type:             "tool_result",
							ToolID:           event.ToolID,
							ToolName:         event.ToolName,
							ToolInput:        event.ToolInput,
							Content:          toolResultContent,      // String for SSE
							ThoughtSignature: event.ThoughtSignature, // Preserve for Gemini 3
						}
						// Set ContentBlocks if structured content (with images) so the
						// continuation loop feeds image blocks to the next LLM call
						if _, isBlocks := toolContent.([]interface{}); isBlocks {
							toolResult.ContentBlocks = toolContent
						}
						toolResults = append(toolResults, toolResult)
					}

					// Output tool result (always string for SSE)
					// Truncate large content for SSE — full data goes to LLM via continuation loop
					sseContent := toolResultContent
					if len(sseContent) > 4000 {
						sseContent = sseContent[:4000]
					}
					fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
						event.ToolID, escapeJSON(sseContent), time.Now().UnixMilli())
					flusher.Flush()

				} else if event.ToolName == "computer" || event.ToolName == "browser" {
					// DEBUG: Log computer/browser tool execution
					inputJSON, _ := json.Marshal(event.ToolInput)
					log.Printf("🖥️  COMPUTER TOOL - Executing: tool=%s, input=%s", event.ToolName, string(inputJSON))

					// Handle computer tool locally using operator module
					result, err := operator.HandleComputerToolWithContext(ctx, event.ToolInput)
					var toolResultContent interface{} // Can be string or content blocks
					var isError bool
					eventBus := events.GetEventBus()

					// Publish tool invocation event
					toolStartTime := time.Now()
					toolEvent := events.NewEvent(events.CategoryTool, events.TypeToolInvocation, events.LevelInfo).
						WithThread(threadID).
						WithData("tool_name", event.ToolName).
						WithData("tool_id", event.ToolID).
						WithData("input", event.ToolInput)
					eventBus.Publish(toolEvent)

					if err != nil {
						log.Printf("Error executing computer tool: %v", err)
						toolResultContent = fmt.Sprintf("Error: %s", err.Error())
						isError = true

						// Publish tool error event
						errorEvent := events.NewEvent(events.CategoryTool, events.TypeToolError, events.LevelError).
							WithThread(threadID).
							WithData("tool_name", event.ToolName).
							WithError(err).
							WithDuration(toolStartTime)
						eventBus.Publish(errorEvent)
					} else {
						// Check if this is a screenshot action
						action, _ := event.ToolInput["action"].(string)
						if action == "screenshot" {
							// Extract base64 screenshot data from result
							toolResultContent = formatScreenshotResult(result)
						} else {
							// For non-screenshot actions, return as JSON string
							resultJSON, _ := json.Marshal(result)
							toolResultContent = string(resultJSON)
						}
						isError = false

						// Publish successful tool result event (sanitized)
						resultEvent := events.NewEvent(events.CategoryTool, events.TypeToolResult, events.LevelInfo).
							WithThread(threadID).
							WithData("tool_name", event.ToolName).
							WithData("tool_id", event.ToolID).
							WithData("result", sanitizeForEvent(result)).
							WithData("success", true).
							WithDuration(toolStartTime)
						eventBus.Publish(resultEvent)
					}

					// Save tool result message - handle both string and content blocks
					var toolResultBlock map[string]interface{}
					var contentForDB interface{}
					var contentStr string

					// Format content based on type
					switch v := toolResultContent.(type) {
					case string:
						// Simple string content
						contentForDB = v
						contentStr = v
					case []interface{}:
						// Content blocks (e.g., screenshot with image)
						contentForDB = v
						// For logging, just show it's content blocks
						contentJSON, _ := json.Marshal(v)
						contentStr = string(contentJSON)
					default:
						// Fallback to JSON
						contentJSON, _ := json.Marshal(v)
						contentForDB = string(contentJSON)
						contentStr = string(contentJSON)
					}

					toolResultBlock = map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": event.ToolID,
						"content":     contentForDB,
						"is_error":    isError,
					}

					// DEBUG: Log tool result being saved (truncate base64 data)
					if len(contentStr) > 200 {
						contentStr = contentStr[:200] + "... [truncated]"
					}
					log.Printf("🖥️  COMPUTER TOOL - Saving result to DB: type=tool_result, tool_use_id=%s, content_type=%T, preview=%s",
						event.ToolID, toolResultContent, contentStr)

					// Wrap in array for proper format
					toolResultArray := []interface{}{toolResultBlock}
					if err := messageSaver.SaveMessage(threadID, "user", toolResultArray, nil, nil); err != nil {
						log.Printf("Error saving computer tool result message: %v", err)
					}

					// Create tool result for conversation continuation
					toolResult := StreamEvent{
						Type:             "tool_result",
						ToolID:           event.ToolID,
						ToolName:         event.ToolName,
						ToolInput:        event.ToolInput,        // IMPORTANT: Include input for next turn
						Content:          contentStr,              // Use string for streaming/logging
						ContentBlocks:    toolResultContent,       // Use structured content for next API call
						ThoughtSignature: event.ThoughtSignature, // Preserve for Gemini 3
					}
					toolResults = append(toolResults, toolResult)

					// Send immediate tool result to user with simple string (like other tools)
					action, _ := event.ToolInput["action"].(string)
					var displayContent string
					switch action {
					case "screenshot":
						displayContent = "Screenshot captured"
					case "click":
						displayContent = "Click performed"
					case "type":
						displayContent = "Text typed"
					case "scroll":
						displayContent = "Scrolled"
					case "key":
						displayContent = "Key pressed"
					case "move":
						displayContent = "Mouse moved"
					case "drag":
						displayContent = "Drag performed"
					case "double_click":
						displayContent = "Double-click performed"
					case "triple_click":
						displayContent = "Triple-click performed"
					default:
						displayContent = fmt.Sprintf("Action '%s' completed", action)
					}
					if isError {
						displayContent = fmt.Sprintf("Error: %s", contentStr)
					}
					fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"is_error\":%t,\"timestamp\":%d}\n\n",
						event.ToolID, escapeJSON(displayContent), isError, time.Now().UnixMilli())
					flusher.Flush()

				} else if isCustomTool(event.ToolName) {
					// Add custom tool to pending list for parallel execution
					// Tools will be executed in parallel when we hit the stop event
					pendingCustomTools = append(pendingCustomTools, ToolCall{
						ID:               event.ToolID,
						Name:             event.ToolName,
						Input:            event.ToolInput,
						ThoughtSignature: event.ThoughtSignature,
					})
					log.Printf("🔀 Queued custom tool for parallel execution: %s (id=%s), pending=%d",
						event.ToolName, event.ToolID, len(pendingCustomTools))
				} else {
					// Server tool - execution handled by provider, just wait for result
					// No local execution needed!
				}

			} else if event.Type == "tool_result" {
				// Handle both custom and server tool results transparently

				// Save tool result message (same format regardless of execution location)
				var toolResultContent interface{}
				if event.ToolResult != nil {
					// For server tools, use raw data
					toolResultContent = event.ToolResult
				} else {
					// For custom tools, use content string
					toolResultContent = event.Content
				}

				// Save tool result message as an array of content blocks
				toolResultBlock := map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": event.ToolID,
					"content":     toolResultContent,
					"is_error":    false,
				}
				// Wrap in array for proper format
				toolResultArray := []interface{}{toolResultBlock}
				if err := messageSaver.SaveMessage(threadID, "user", toolResultArray, nil, nil); err != nil {
					log.Printf("Error saving tool result message: %v", err)
				}

				// Create tool result for conversation continuation
				toolResult := StreamEvent{
					Type:      "tool_result",
					ToolID:    event.ToolID,
					ToolName:  event.ToolName,
					Content:   event.Content,
				}

				// Use stored tool name if available (important for server tools)
				if name, ok := toolNames[event.ToolID]; ok {
					toolResult.ToolName = name
				}

				// Use stored tool input (important for server tools)
				if input, ok := toolInputs[event.ToolID]; ok {
					toolResult.ToolInput = input
				} else if event.ToolInput != nil {
					toolResult.ToolInput = event.ToolInput
				}

				// Use stored thought signature (for Gemini 3)
				if sig, ok := toolThoughtSignatures[event.ToolID]; ok {
					toolResult.ThoughtSignature = sig
				}

				// Only add custom and MCP tool results for conversation continuation
				// Server tools (web_search, web_fetch) already have their results processed by the LLM
				if name, ok := toolNames[event.ToolID]; ok && (isCustomTool(name) || isMCPTool(name)) {
					toolResults = append(toolResults, toolResult)
				}

				// Stream output (same format regardless of execution location)
				fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					event.ToolID, escapeJSON(event.Content), time.Now().UnixMilli())
				flusher.Flush()

			} else if event.Type == "content" {
				// Accumulate assistant content
				assistantContent += event.Content

				// Publish streaming chunk event
				eventBus := events.GetEventBus()
				chunkEvent := events.NewEvent(events.CategoryChat, events.TypeChatStreamChunk, events.LevelDebug).
					WithThread(threadID).
					WithData("chunk", event.Content).
					WithData("chunk_length", len(event.Content))
				eventBus.Publish(chunkEvent)

				// Stream content normally
				fmt.Fprintf(w, "data: {\"type\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					event.Type, escapeJSON(event.Content), time.Now().UnixMilli())
				flusher.Flush()
			} else if event.Type == "thinking" {
				// Handle reasoning/thinking content from thinking models (Kimi K2, etc.)
				// Accumulate reasoning content for context preservation
				reasoningContent += event.Content

				// Stream as "thinking" type so client can display appropriately (e.g., collapsible)
				fmt.Fprintf(w, "data: {\"type\":\"thinking\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					escapeJSON(event.Content), time.Now().UnixMilli())
				flusher.Flush()
			} else if event.Type == "start" {
				// Skip processor start events - we already sent start/thread_id/request_id early
				continue
			} else {
				// Stream other events normally (stop, etc)
				fmt.Fprintf(w, "data: {\"type\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					event.Type, escapeJSON(event.Content), time.Now().UnixMilli())
				flusher.Flush()
			}

			// Execute pending custom tools in parallel before breaking on stop
			if event.Type == "stop" && len(pendingCustomTools) > 0 {
				log.Printf("🔀 Executing %d pending custom tools in parallel before stop", len(pendingCustomTools))

				// Create streaming callback for tool output
				// All events use type "tool_stream" with an "event" field to distinguish sub-types
				streamCallback := func(toolID string, streamEvent tools.ToolStreamEvent) {
					// Stream tool output events to client
					if streamEvent.Type == tools.ToolEventChunk || streamEvent.Type == tools.ToolEventOutput {
						eventType := "chunk"
						if streamEvent.Type == tools.ToolEventOutput {
							eventType = "output"
						}
						fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"%s\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
							eventType, toolID, escapeJSON(streamEvent.Content), time.Now().UnixMilli())
						flusher.Flush()
					} else if streamEvent.Type == tools.ToolEventProgress {
						progress := 0.0
						if streamEvent.Progress != nil {
							progress = *streamEvent.Progress
						}
						fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"progress\",\"tool_id\":\"%s\",\"content\":\"%s\",\"progress\":%.2f,\"timestamp\":%d}\n\n",
							toolID, escapeJSON(streamEvent.Content), progress, time.Now().UnixMilli())
						flusher.Flush()
					} else if streamEvent.Type == tools.ToolEventLog {
						fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"log\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
							toolID, escapeJSON(streamEvent.Content), time.Now().UnixMilli())
						flusher.Flush()
					}
				}

				// Execute with streaming support (for single streaming tool)
				parallelResults := ExecuteCustomToolsWithContext(ctx, pendingCustomTools, threadID, taskID, streamCallback)

				// Process each result: save to DB, stream to client, add to toolResults
				for _, result := range parallelResults {
					// Save tool result message
					toolResultBlock := map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": result.ID,
						"content":     result.Content,
						"is_error":    result.Error != nil,
					}
					toolResultArray := []interface{}{toolResultBlock}
					if err := messageSaver.SaveMessage(threadID, "user", toolResultArray, nil, nil); err != nil {
						log.Printf("Error saving parallel tool result message: %v", err)
					}

					// Find the original tool call for thought signature
					var thoughtSig string
					for _, tc := range pendingCustomTools {
						if tc.ID == result.ID {
							thoughtSig = tc.ThoughtSignature
							break
						}
					}

					// Create tool result for conversation continuation
					toolResult := StreamEvent{
						Type:             "tool_result",
						ToolID:           result.ID,
						ToolName:         result.Name,
						ToolInput:        result.Input,
						Content:          result.Content,
						ThoughtSignature: thoughtSig,
					}
					toolResults = append(toolResults, toolResult)

					// Stream tool result to client
					isError := result.Error != nil
					fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"is_error\":%t,\"timestamp\":%d}\n\n",
						result.ID, escapeJSON(result.Content), isError, time.Now().UnixMilli())
					flusher.Flush()
				}

				// Clear pending tools
				pendingCustomTools = nil
			}

			// Check if this was a stop event to break after processing
			if event.Type == "stop" {
				break
			}
		}

		// Also check if stream is complete (as backup)
		if processor.IsComplete(line) {
			break
		}
	}

	// Check for cancellation before draining pending events
	select {
	case <-ctx.Done():
		log.Printf("🛑 Cancelled before drain phase")
		return toolResults, assistantContent, usageInputTokens, usageOutputTokens, ctx.Err()
	default:
	}

	// Drain any remaining pending tool_use events from the processor (for parallel tool calls)
	// Collect custom tools for parallel execution
	var drainedCustomTools []ToolCall
	if checker, ok := processor.(PendingEventChecker); ok {
		for checker.HasPendingEvents() {
			event, err := processor.ProcessLine("")
			if err != nil || event == nil {
				break
			}

			// Handle tool_use events - execute the tool
			if event.Type == "tool_use" {
				log.Printf("🔧 Draining pending tool_use: %s (id=%s)", event.ToolName, event.ToolID)

				// Store tool input and name
				toolInputs[event.ToolID] = event.ToolInput
				toolNames[event.ToolID] = event.ToolName

				// Save tool use message
				toolUseBlock := map[string]interface{}{
					"type":  "tool_use",
					"id":    event.ToolID,
					"name":  event.ToolName,
					"input": event.ToolInput,
				}
				toolUseContent := []interface{}{toolUseBlock}
				if err := messageSaver.SaveMessage(threadID, "assistant", toolUseContent, model, nil); err != nil {
					log.Printf("Error saving tool use message: %v", err)
				}

				// Stream tool_use event (use dynamic display name since we have full input)
				displayName := getDynamicToolDisplayName(event.ToolName, event.ToolInput)
				fmt.Fprintf(w, "data: {\"type\":\"tool_use\",\"tool_name\":\"%s\",\"tool_display_name\":\"%s\",\"tool_id\":\"%s\",\"content\":\"\",\"timestamp\":%d}\n\n",
					event.ToolName, escapeJSON(displayName), event.ToolID, time.Now().UnixMilli())
				flusher.Flush()

				// Handle tool execution based on type
				if event.ToolName == "$web_search" {
					// Moonshot built-in $web_search: echo arguments back
					log.Printf("🌐 Moonshot $web_search (drain): echoing arguments back")
					inputJSON, _ := json.Marshal(event.ToolInput)
					toolResultContent := string(inputJSON)

					// Save and stream result immediately
					toolResultBlock := map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": event.ToolID,
						"content":     toolResultContent,
					}
					toolResultArray := []interface{}{toolResultBlock}
					if err := messageSaver.SaveMessage(threadID, "user", toolResultArray, nil, nil); err != nil {
						log.Printf("Error saving tool result message: %v", err)
					}
					toolResult := StreamEvent{
						Type:             "tool_result",
						ToolID:           event.ToolID,
						ToolName:         event.ToolName,
						ToolInput:        event.ToolInput,
						Content:          toolResultContent,
						ThoughtSignature: event.ThoughtSignature,
					}
					toolResults = append(toolResults, toolResult)
					fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
						event.ToolID, escapeJSON(toolResultContent), time.Now().UnixMilli())
					flusher.Flush()
				} else if isMCPTool(event.ToolName) {
					cfg := config.GetConfig()
					agentCfg := cfg.Get()
					mcpCfg := agentCfg.MCP
					// Create MCP notification callback for streaming progress/log events
					mcpNotifyCallback2 := func(n mcp.MCPNotification) {
						switch n.Type {
						case "progress":
							fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"progress\",\"tool_id\":\"%s\",\"content\":\"%s\",\"progress\":%.2f,\"timestamp\":%d}\n\n",
								event.ToolID, escapeJSON(n.Message), n.Progress, time.Now().UnixMilli())
							flusher.Flush()
						case "log":
							fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"log\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
								event.ToolID, escapeJSON(n.Message), time.Now().UnixMilli())
							flusher.Flush()
						}
					}
					result, err := mcp.ExecuteToolStreamingWithContext(ctx, event.ToolName, event.ToolInput, mcpCfg, threadID, mcpNotifyCallback2)
					var toolResultContent string
					var toolContent interface{}
					if err != nil {
						toolResultContent = fmt.Sprintf("Error: %v", err)
						toolContent = toolResultContent
					} else {
						// Process MCP tool result with vision/filesystem awareness
						visionEnabled := false
						if agentCfg.LLM.Vision != nil {
							visionEnabled = agentCfg.LLM.Vision.Enabled
						}
						toolContent, toolResultContent, _ = ProcessMCPToolResult(result, visionEnabled, fileProcessor, threadID, event.ToolName)
					}

					// Save tool result — use structured content if available
					toolResultBlock := map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": event.ToolID,
						"content":     toolContent,
					}
					toolResultArray := []interface{}{toolResultBlock}
					if err := messageSaver.SaveMessage(threadID, "user", toolResultArray, nil, nil); err != nil {
						log.Printf("Error saving tool result message: %v", err)
					}
					toolResult := StreamEvent{
						Type:             "tool_result",
						ToolID:           event.ToolID,
						ToolName:         event.ToolName,
						ToolInput:        event.ToolInput,
						Content:          toolResultContent,
						ThoughtSignature: event.ThoughtSignature,
					}
					if _, isBlocks := toolContent.([]interface{}); isBlocks {
						toolResult.ContentBlocks = toolContent
					}
					toolResults = append(toolResults, toolResult)
					fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
						event.ToolID, escapeJSON(toolResultContent), time.Now().UnixMilli())
					flusher.Flush()
				} else if isCustomTool(event.ToolName) {
					// Queue custom tools for parallel execution
					drainedCustomTools = append(drainedCustomTools, ToolCall{
						ID:               event.ToolID,
						Name:             event.ToolName,
						Input:            event.ToolInput,
						ThoughtSignature: event.ThoughtSignature,
					})
					log.Printf("🔀 Queued drained custom tool for parallel execution: %s (id=%s)", event.ToolName, event.ToolID)
				} else {
					toolResultContent := fmt.Sprintf("Error: Unknown tool type for %s", event.ToolName)
					toolResultBlock := map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": event.ToolID,
						"content":     toolResultContent,
					}
					toolResultArray := []interface{}{toolResultBlock}
					if err := messageSaver.SaveMessage(threadID, "user", toolResultArray, nil, nil); err != nil {
						log.Printf("Error saving tool result message: %v", err)
					}
					toolResult := StreamEvent{
						Type:             "tool_result",
						ToolID:           event.ToolID,
						ToolName:         event.ToolName,
						ToolInput:        event.ToolInput,
						Content:          toolResultContent,
						ThoughtSignature: event.ThoughtSignature,
					}
					toolResults = append(toolResults, toolResult)
					fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
						event.ToolID, escapeJSON(toolResultContent), time.Now().UnixMilli())
					flusher.Flush()
				}
			}
		}
	}

	// Execute drained custom tools in parallel
	if len(drainedCustomTools) > 0 {
		log.Printf("🔀 Executing %d drained custom tools in parallel", len(drainedCustomTools))

		// Create streaming callback for drained tools
		// All events use type "tool_stream" with an "event" field to distinguish sub-types
		drainStreamCallback := func(toolID string, streamEvent tools.ToolStreamEvent) {
			if streamEvent.Type == tools.ToolEventChunk || streamEvent.Type == tools.ToolEventOutput {
				eventType := "chunk"
				if streamEvent.Type == tools.ToolEventOutput {
					eventType = "output"
				}
				fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"%s\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					eventType, toolID, escapeJSON(streamEvent.Content), time.Now().UnixMilli())
				flusher.Flush()
			} else if streamEvent.Type == tools.ToolEventProgress {
				progress := 0.0
				if streamEvent.Progress != nil {
					progress = *streamEvent.Progress
				}
				fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"progress\",\"tool_id\":\"%s\",\"content\":\"%s\",\"progress\":%.2f,\"timestamp\":%d}\n\n",
					toolID, escapeJSON(streamEvent.Content), progress, time.Now().UnixMilli())
				flusher.Flush()
			} else if streamEvent.Type == tools.ToolEventLog {
				fmt.Fprintf(w, "data: {\"type\":\"tool_stream\",\"event\":\"log\",\"tool_id\":\"%s\",\"content\":\"%s\",\"timestamp\":%d}\n\n",
					toolID, escapeJSON(streamEvent.Content), time.Now().UnixMilli())
				flusher.Flush()
			}
		}

		parallelResults := ExecuteCustomToolsWithContext(ctx, drainedCustomTools, threadID, taskID, drainStreamCallback)

		for _, result := range parallelResults {
			// Save tool result message
			toolResultBlock := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": result.ID,
				"content":     result.Content,
				"is_error":    result.Error != nil,
			}
			toolResultArray := []interface{}{toolResultBlock}
			if err := messageSaver.SaveMessage(threadID, "user", toolResultArray, nil, nil); err != nil {
				log.Printf("Error saving parallel tool result message: %v", err)
			}

			// Find the original tool call for thought signature
			var thoughtSig string
			for _, tc := range drainedCustomTools {
				if tc.ID == result.ID {
					thoughtSig = tc.ThoughtSignature
					break
				}
			}

			// Create tool result for conversation continuation
			toolResult := StreamEvent{
				Type:             "tool_result",
				ToolID:           result.ID,
				ToolName:         result.Name,
				ToolInput:        result.Input,
				Content:          result.Content,
				ThoughtSignature: thoughtSig,
			}
			toolResults = append(toolResults, toolResult)

			// Stream tool result to client
			isError := result.Error != nil
			fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"tool_id\":\"%s\",\"content\":\"%s\",\"is_error\":%t,\"timestamp\":%d}\n\n",
				result.ID, escapeJSON(result.Content), isError, time.Now().UnixMilli())
			flusher.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, "", 0, 0, fmt.Errorf("error reading stream: %w", err)
	}

	// Save final assistant message if we have content
	// This includes content after tool use (especially for server tools like web_search)
	if assistantContent != "" {
		dbSaveStart := time.Now()
		if err := messageSaver.SaveMessage(threadID, "assistant", assistantContent, model, nil); err != nil {
			log.Printf("Error saving final assistant message: %v", err)
			// Publish database error event
			eventBus := events.GetEventBus()
			dbErrorEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBError, events.LevelError).
				WithThread(threadID).
				WithError(err).
				WithDuration(dbSaveStart)
			eventBus.Publish(dbErrorEvent)
		} else {
			// Publish database insert success event
			eventBus := events.GetEventBus()
			dbInsertEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBInsert, events.LevelInfo).
				WithThread(threadID).
				WithData("table", "messages").
				WithData("role", "assistant").
				WithData("content_length", len(assistantContent)).
				WithDuration(dbSaveStart)
			eventBus.Publish(dbInsertEvent)

			// Publish LLM response event with token info if available
			llmResponseEvent := events.NewEvent(events.CategoryLLM, events.TypeLLMResponse, events.LevelInfo).
				WithThread(threadID).
				WithData("response_length", len(assistantContent)).
				WithData("model", model)
			eventBus.Publish(llmResponseEvent)
		}
	}

	// Return pre-tool content if we have tools (to include in next iteration)
	// Otherwise return final assistant content (for conversations without tools)
	contentToReturn := assistantContent
	if len(toolResults) > 0 && preToolContent != "" {
		contentToReturn = preToolContent
	}

	return toolResults, contentToReturn, usageInputTokens, usageOutputTokens, nil
}

// UnifiedToolConversation - backwards compatible version
func UnifiedToolConversation(w http.ResponseWriter, provider Provider, processor StreamProcessor, messages []Message, toolDefs []tools.ToolDefinition, messageSaver MessageSaver, threadID string, model *string) error {
	return UnifiedToolConversationWithBuiltins(w, provider, processor, messages, toolDefs, nil, messageSaver, threadID, model, nil, "")
}

func isCustomTool(toolName string) bool {
	// Simple check to distinguish custom vs built-in tools
	builtinTools := map[string]bool{
		"web_search": true,
		"web_fetch":  true,
		"computer":   true, // Computer tool handled locally but is builtin
		"browser":    true, // Browser tool handled locally like computer (for non-Claude providers)
	}
	return !builtinTools[toolName]
}

func isMCPTool(toolName string) bool {
	// Check if this tool is an MCP tool
	cfg := config.GetConfig()
	mcpConfig := cfg.Get().MCP
	return mcp.IsMCPTool(toolName, mcpConfig)
}

// getToolDisplayName returns the human-readable display name for a tool
// Checks custom tools first, then MCP tools, then falls back to title-casing the name
func getToolDisplayName(toolName string) string {
	// First check custom tools registry
	displayName := tools.GetDisplayName(toolName)
	if displayName != "" && displayName != toolName {
		return displayName
	}

	// Check MCP tools
	mcpDisplayName := mcp.GetToolDisplayName(toolName)
	if mcpDisplayName != "" {
		return mcpDisplayName
	}

	// Fall back to title-casing the tool name
	return toTitleCase(toolName)
}

// getDynamicToolDisplayName returns a context-specific display name based on tool input params.
// Falls back to the static display name if the tool doesn't support dynamic names.
func getDynamicToolDisplayName(toolName string, params map[string]interface{}) string {
	// Try dynamic display name first (e.g. "Calling Jarvis" for call_agent)
	if dynName := tools.GetDynamicDisplayName(toolName, params); dynName != "" {
		return dynName
	}
	// Fall back to static display name
	return getToolDisplayName(toolName)
}

// toTitleCase converts "tool-name" or "tool_name" to "Tool Name"
func toTitleCase(s string) string {
	words := make([]byte, 0, len(s))
	capitalizeNext := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '-' || c == '_' {
			words = append(words, ' ')
			capitalizeNext = true
		} else if capitalizeNext && c >= 'a' && c <= 'z' {
			words = append(words, c-32)
			capitalizeNext = false
		} else {
			words = append(words, c)
			capitalizeNext = false
		}
	}
	return string(words)
}

// detectImageMediaType detects the actual image format from base64 data
// PNG starts with iVBORw0KGgo, JPEG starts with /9j/, GIF starts with R0lGOD, WebP starts with UklGR
func detectImageMediaType(base64Data string) string {
	if strings.HasPrefix(base64Data, "iVBORw0KGgo") {
		return "image/png"
	}
	if strings.HasPrefix(base64Data, "/9j/") {
		return "image/jpeg"
	}
	if strings.HasPrefix(base64Data, "R0lGOD") {
		return "image/gif"
	}
	if strings.HasPrefix(base64Data, "UklGR") {
		return "image/webp"
	}
	// Default to png as it's most common for screenshots
	return "image/png"
}

// formatScreenshotResult formats screenshot result into Anthropic content blocks format
func formatScreenshotResult(result map[string]interface{}) []interface{} {
	// HandleComputerTool returns Anthropic format directly:
	// {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "..."}}

	// Check if result is already in Anthropic image format
	if resultType, ok := result["type"].(string); ok && resultType == "image" {
		if source, ok := result["source"].(map[string]interface{}); ok {
			if base64Data, hasData := source["data"].(string); hasData {
				// Detect actual media type from base64 data to avoid mismatches
				actualMediaType := detectImageMediaType(base64Data)
				declaredMediaType, _ := source["media_type"].(string)
				if declaredMediaType != actualMediaType {
					log.Printf("⚠️  Fixing media type mismatch: declared=%s, actual=%s", declaredMediaType, actualMediaType)
					source["media_type"] = actualMediaType
				}
				// Already in correct format, wrap in content blocks array
				log.Printf("✅ Screenshot result already in Anthropic format (media_type=%s)", actualMediaType)
				return []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Screenshot captured",
					},
					result, // Use the result directly as it's already formatted
				}
			}
		}
	}

	// Legacy format: {"data": {"result": {"screenshot_url": "data:image/jpeg;base64,..."}}}
	var base64Data string
	var mediaType string

	// Navigate through nested structure
	if data, ok := result["data"].(map[string]interface{}); ok {
		if resultData, ok := data["result"].(map[string]interface{}); ok {
			if screenshotURL, ok := resultData["screenshot_url"].(string); ok {
				// Extract base64 data from data URL (format: "data:image/jpeg;base64,...")
				if strings.HasPrefix(screenshotURL, "data:") {
					parts := strings.SplitN(screenshotURL, ",", 2)
					if len(parts) == 2 {
						base64Data = parts[1]
						// Extract media type from data URL
						if strings.Contains(parts[0], "image/") {
							mediaTypeParts := strings.Split(parts[0], ";")
							if len(mediaTypeParts) > 0 {
								mediaType = strings.TrimPrefix(mediaTypeParts[0], "data:")
							}
						}
					}
				}
			}
		}
	}

	// Always detect actual media type from base64 data to avoid mismatches
	if base64Data != "" {
		detectedType := detectImageMediaType(base64Data)
		if mediaType != "" && mediaType != detectedType {
			log.Printf("⚠️  Fixing legacy media type mismatch: declared=%s, actual=%s", mediaType, detectedType)
		}
		mediaType = detectedType
	}

	// If we couldn't extract base64 data, return error message
	if base64Data == "" {
		log.Printf("⚠️  Failed to extract screenshot base64 data from result: %+v", result)
		return []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "Screenshot captured but failed to extract image data",
			},
		}
	}

	// Return content blocks with text + image
	return []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": "Screenshot captured",
		},
		map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": mediaType,
				"data":       base64Data,
			},
		},
	}
}

// sanitizeForEvent removes or truncates large data (like base64 images) from tool results
// before storing in the event bus to prevent memory bloat. The actual result sent to
// the client and LLM is unchanged - this only affects debug/monitoring events.
func sanitizeForEvent(result interface{}) interface{} {
	return sanitizeValue(result, 0)
}

func sanitizeValue(v interface{}, depth int) interface{} {
	if depth > 10 {
		return "[max depth reached]"
	}

	switch val := v.(type) {
	case map[string]interface{}:
		sanitized := make(map[string]interface{})
		for k, v := range val {
			sanitized[k] = sanitizeValue(v, depth+1)
		}
		return sanitized

	case []interface{}:
		sanitized := make([]interface{}, len(val))
		for i, v := range val {
			sanitized[i] = sanitizeValue(v, depth+1)
		}
		return sanitized

	case string:
		// Truncate large strings (likely base64 data)
		if len(val) > 1000 {
			// Check if it looks like base64
			if looksLikeBase64(val) {
				return fmt.Sprintf("[base64 data truncated, %d bytes]", len(val))
			}
			return val[:500] + fmt.Sprintf("... [truncated, %d bytes total]", len(val))
		}
		return val

	default:
		return v
	}
}

func looksLikeBase64(s string) bool {
	if len(s) < 100 {
		return false
	}
	// Check first 100 chars for base64 pattern
	sample := s[:100]
	validChars := 0
	for _, c := range sample {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			validChars++
		}
	}
	return validChars > 90 // >90% base64 chars
}

// escapeJSON escapes quotes and newlines for JSON embedding
func escapeJSON(s string) string {
	result := ""
	for _, char := range s {
		switch char {
		case '"':
			result += "\\\""
		case '\\':
			result += "\\\\"
		case '\n':
			result += "\\n"
		case '\r':
			result += "\\r"
		case '\t':
			result += "\\t"
		default:
			result += string(char)
		}
	}
	return result
}

// isTransientError checks if an error from a provider is a transient/retryable error
// (429 rate limit, 503 service unavailable, 529 overloaded, or connection errors)
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Check for HTTP status codes that are transient
	for _, code := range []string{": 429 ", ": 503 ", ": 529 "} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	// Check for connection-level failures
	for _, phrase := range []string{"connection refused", "connection reset", "EOF", "timeout"} {
		if strings.Contains(strings.ToLower(msg), phrase) {
			return true
		}
	}
	return false
}