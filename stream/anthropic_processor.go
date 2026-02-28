package stream

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

type AnthropicProcessor struct{
	hasStarted                bool
	currentToolID             string
	currentToolName           string
	currentToolInput          string
	isServerTool              bool  // Track if current tool is server-executed
	inputTokens               int   // Captured from message_start usage
	cacheCreationInputTokens  int   // Captured from message_start usage
	cacheReadInputTokens      int   // Captured from message_start usage
}

type AnthropicStreamData struct {
	Type        string                 `json:"type"`
	Index       int                    `json:"index,omitempty"`
	Delta       AnthropicDelta         `json:"delta,omitempty"`
	Message     AnthropicMessage       `json:"message,omitempty"`
	ContentBlock AnthropicContentBlock `json:"content_block,omitempty"`
	ToolUseID   string                 `json:"tool_use_id,omitempty"`   // For web_search_tool_result
	Content     interface{}            `json:"content,omitempty"`       // Can be []WebSearchResult or WebFetchResult
	Usage       *AnthropicUsage        `json:"usage,omitempty"`         // For message_delta token usage
}

type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

type WebSearchResult struct {
	Type             string `json:"type"`              // "web_search_result"
	URL              string `json:"url"`
	Title            string `json:"title"`
	EncryptedContent string `json:"encrypted_content"`
	PageAge          string `json:"page_age"`
}

type WebFetchResult struct {
	Type        string           `json:"type"`         // "web_fetch_result"
	URL         string           `json:"url"`
	Content     DocumentContent  `json:"content"`
	RetrievedAt string           `json:"retrieved_at"`
}

type DocumentContent struct {
	Type      string         `json:"type"`      // "document"
	Source    DocumentSource `json:"source"`
	Title     string         `json:"title,omitempty"`
	Citations CitationsInfo  `json:"citations,omitempty"`
}

type DocumentSource struct {
	Type      string `json:"type"`       // "text", "base64"
	MediaType string `json:"media_type"` // "text/plain", "application/pdf"
	Data      string `json:"data"`       // The actual content
}

type CitationsInfo struct {
	Enabled bool `json:"enabled"`
}

type AnthropicDelta struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	PartialJSON  string                 `json:"partial_json,omitempty"`
}

type AnthropicMessage struct {
	ID    string          `json:"id"`
	Type  string          `json:"type"`
	Role  string          `json:"role"`
	Usage *AnthropicUsage `json:"usage,omitempty"` // Input tokens reported in message_start
}

type AnthropicContentBlock struct {
	Type      string                 `json:"type"`  // "text", "tool_use", "server_tool_use", "web_search_tool_result", "web_fetch_tool_result"
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"` // For tool results
	Content   interface{}            `json:"content,omitempty"`     // Can be []WebSearchResult or WebFetchResult
}

func (p *AnthropicProcessor) ProcessLine(line string) (*StreamEvent, error) {
	// Claude format: "event: type\ndata: {JSON}" or just "data: {JSON}"

	// Skip event lines, we only care about data lines
	if strings.HasPrefix(line, "event: ") {
		return nil, nil
	}

	if !strings.HasPrefix(line, "data: ") {
		return nil, nil // Skip non-data lines
	}

	// Extract JSON part
	jsonStr := strings.TrimPrefix(line, "data: ")

	// Parse Anthropic response
	var anthropicData AnthropicStreamData
	if err := json.Unmarshal([]byte(jsonStr), &anthropicData); err != nil {
		return nil, err
	}


	// Determine event type and content based on Anthropic's type
	var eventType, content string

	switch anthropicData.Type {
	case "message_start":
		if !p.hasStarted {
			eventType = "start"
			content = ""
			p.hasStarted = true
			// Capture input tokens from message_start (Anthropic reports input tokens here)
			if anthropicData.Message.Usage != nil {
				p.inputTokens = anthropicData.Message.Usage.InputTokens
				p.cacheCreationInputTokens = anthropicData.Message.Usage.CacheCreationInputTokens
				p.cacheReadInputTokens = anthropicData.Message.Usage.CacheReadInputTokens
				log.Printf("📊 Anthropic input tokens from message_start: %d (cache_create=%d, cache_read=%d)",
					p.inputTokens, p.cacheCreationInputTokens, p.cacheReadInputTokens)
			}
		} else {
			return nil, nil // Skip duplicate starts
		}
	case "content_block_start":
		// Check if this is a tool use block (custom tools)
		if anthropicData.ContentBlock.Type == "tool_use" {
			// DEBUG: Log computer tool content_block_start
			if anthropicData.ContentBlock.Name == "computer" {
				blockJSON, _ := json.Marshal(anthropicData.ContentBlock)
				log.Printf("🖥️  COMPUTER TOOL - Stream content_block_start: %s", string(blockJSON))
			}
			// Store tool info for accumulation
			p.currentToolID = anthropicData.ContentBlock.ID
			p.currentToolName = anthropicData.ContentBlock.Name
			p.currentToolInput = ""
			p.isServerTool = false

			// Emit tool_call event immediately with tool name
			return &StreamEvent{
				Type:     "tool_call",
				ToolName: p.currentToolName,
				ToolID:   p.currentToolID,
				Content:  "",
			}, nil
		}
		// Check if this is a server tool use block (built-in tools like web_search)
		if anthropicData.ContentBlock.Type == "server_tool_use" {
			// Store tool info for accumulation (same as custom tools)
			p.currentToolID = anthropicData.ContentBlock.ID
			p.currentToolName = anthropicData.ContentBlock.Name
			p.currentToolInput = ""
			p.isServerTool = true

			// Emit tool_call event immediately with tool name
			return &StreamEvent{
				Type:     "tool_call",
				ToolName: p.currentToolName,
				ToolID:   p.currentToolID,
				Content:  "",
			}, nil
		}
		// Check if this is a web search result block
		if anthropicData.ContentBlock.Type == "web_search_tool_result" {
			// Convert web_search_tool_result → tool_result (transparent to user)
			resultContent := formatWebSearchResults(anthropicData.ContentBlock.Content)

			return &StreamEvent{
				Type:       "tool_result", // ← Transparent conversion
				ToolID:     anthropicData.ContentBlock.ToolUseID,
				ToolName:   "web_search",
				Content:    resultContent,
				ToolResult: anthropicData.ContentBlock.Content, // Keep raw data for database
			}, nil
		}
		// Check if this is a web fetch result block
		if anthropicData.ContentBlock.Type == "web_fetch_tool_result" {
			// Convert web_fetch_tool_result → tool_result (transparent to user)
			resultContent := formatWebFetchResult(anthropicData.ContentBlock.Content)

			return &StreamEvent{
				Type:       "tool_result", // ← Transparent conversion
				ToolID:     anthropicData.ContentBlock.ToolUseID,
				ToolName:   "web_fetch",
				Content:    resultContent,
				ToolResult: anthropicData.ContentBlock.Content, // Keep raw data for database
			}, nil
		}
		// Skip text content blocks - we already handled start with message_start
		return nil, nil
	case "content_block_delta":
		if anthropicData.Delta.Type == "text_delta" {
			eventType = "content"
			content = anthropicData.Delta.Text
		} else if anthropicData.Delta.Type == "input_json_delta" {
			// DEBUG: Log computer tool input delta
			if p.currentToolName == "computer" {
				log.Printf("🖥️  COMPUTER TOOL - Stream input_json_delta: %s", anthropicData.Delta.PartialJSON)
			}
			// Accumulate tool input JSON
			p.currentToolInput += anthropicData.Delta.PartialJSON

			// Emit tool input delta for all tools (both custom and server)
			return &StreamEvent{
				Type:    "tool_input_delta",
				ToolID:  p.currentToolID,
				Content: anthropicData.Delta.PartialJSON,
			}, nil
		} else {
			// Skip other delta types
			return nil, nil
		}
	case "content_block_stop":
		// If we have accumulated tool input, emit the tool_use event now
		if p.currentToolID != "" && p.currentToolName != "" {
			// DEBUG: Log computer tool accumulated input
			if p.currentToolName == "computer" {
				log.Printf("🖥️  COMPUTER TOOL - Stream content_block_stop, accumulated input: %s", p.currentToolInput)
			}

			// Parse the accumulated JSON input
			toolInput := make(map[string]interface{})
			if p.currentToolInput != "" {
				if err := json.Unmarshal([]byte(p.currentToolInput), &toolInput); err != nil {
					// If parsing fails, log and continue with empty map
					fmt.Printf("Warning: failed to parse tool input JSON: %v\n", err)
				}
			}

			// DEBUG: Log computer tool parsed input
			if p.currentToolName == "computer" {
				inputJSON, _ := json.Marshal(toolInput)
				log.Printf("🖥️  COMPUTER TOOL - Stream parsed input: %s", string(inputJSON))
			}

			// Create tool_use event
			event := &StreamEvent{
				Type:      "tool_use",
				ToolName:  p.currentToolName,
				ToolID:    p.currentToolID,
				ToolInput: toolInput,
				Content:   "",
			}

			// Reset tool state
			p.currentToolID = ""
			p.currentToolName = ""
			p.currentToolInput = ""
			p.isServerTool = false

			return event, nil
		}

		return nil, nil
	case "web_search_tool_result":
		// Convert web_search_tool_result → tool_result (transparent to user)
		resultContent := formatWebSearchResults(anthropicData.Content)

		return &StreamEvent{
			Type:       "tool_result", // ← Transparent conversion
			ToolID:     anthropicData.ToolUseID,
			ToolName:   "web_search",
			Content:    resultContent,
			ToolResult: anthropicData.Content, // Keep raw data for database
		}, nil
	case "web_fetch_tool_result":
		// Convert web_fetch_tool_result → tool_result (transparent to user)
		resultContent := formatWebFetchResult(anthropicData.Content)

		return &StreamEvent{
			Type:       "tool_result", // ← Transparent conversion
			ToolID:     anthropicData.ToolUseID,
			ToolName:   "web_fetch",
			Content:    resultContent,
			ToolResult: anthropicData.Content, // Keep raw data for database
		}, nil
	case "message_stop":
		eventType = "stop"
		content = ""
	case "ping":
		// Skip ping events
		return nil, nil
	case "message_delta":
		// Extract usage information from message_delta event
		if anthropicData.Usage != nil {
			// Anthropic reports input_tokens in message_start and output_tokens in message_delta.
			// Combine both for accurate totals.
			inputTokens := anthropicData.Usage.InputTokens
			if inputTokens == 0 && p.inputTokens > 0 {
				inputTokens = p.inputTokens
			}
			outputTokens := anthropicData.Usage.OutputTokens

			// Merge cache tokens: prefer non-zero from message_delta, fall back to message_start
			cacheCreation := anthropicData.Usage.CacheCreationInputTokens
			if cacheCreation == 0 {
				cacheCreation = p.cacheCreationInputTokens
			}
			cacheRead := anthropicData.Usage.CacheReadInputTokens
			if cacheRead == 0 {
				cacheRead = p.cacheReadInputTokens
			}

			return &StreamEvent{
				Type:                "usage",
				InputTokens:         inputTokens,
				OutputTokens:        outputTokens,
				TotalTokens:         inputTokens + outputTokens,
				CacheCreationTokens: cacheCreation,
				CacheReadTokens:     cacheRead,
				Content:             "",
			}, nil
		}
		// Skip other message delta events without usage
		return nil, nil
	default:
		// Unknown type, skip
		return nil, nil
	}

	return &StreamEvent{
		Type:    eventType,
		Content: content,
	}, nil
}

func (p *AnthropicProcessor) IsComplete(line string) bool {
	return strings.Contains(line, "\"type\":\"message_stop\"")
}

func formatWebSearchResults(content interface{}) string {
	// Convert raw search results to user-friendly format
	results, ok := content.([]WebSearchResult)
	if !ok {
		return "Search completed successfully"
	}

	if len(results) == 0 {
		return "No search results found"
	}

	var summary strings.Builder
	summary.WriteString("Search completed successfully")

	// Optionally include summary info
	if len(results) > 0 {
		summary.WriteString(fmt.Sprintf(" - found %d results", len(results)))
	}

	return summary.String()
}

func formatWebFetchResult(result interface{}) string {
	// Convert web fetch result to user-friendly format
	webFetchResult, ok := result.(WebFetchResult)
	if !ok {
		return "Web fetch completed successfully"
	}

	var summary strings.Builder
	summary.WriteString("Web fetch completed successfully")

	// Add URL info if available
	if webFetchResult.URL != "" {
		summary.WriteString(fmt.Sprintf(" - fetched content from %s", webFetchResult.URL))
	}

	// Add title if available
	if webFetchResult.Content.Title != "" {
		summary.WriteString(fmt.Sprintf(" (\"%s\")", webFetchResult.Content.Title))
	}

	return summary.String()
}