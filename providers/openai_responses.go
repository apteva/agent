package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/stream"
	"github.com/apteva/agent/tools"
)

// ==================== OpenAI Responses API Types ====================

// OpenAIResponsesRequest is the request structure for the Responses API
type OpenAIResponsesRequest struct {
	Model              string                   `json:"model"`
	Input              interface{}              `json:"input"`                          // Can be string or []ResponsesInputItem
	Tools              []interface{}            `json:"tools,omitempty"`
	PreviousResponseID string                   `json:"previous_response_id,omitempty"`
	Stream             bool                     `json:"stream"`
	Instructions       string                   `json:"instructions,omitempty"`
	Reasoning          *ResponsesReasoning      `json:"reasoning,omitempty"`
}

// ResponsesReasoning controls reasoning behavior for the Responses API
type ResponsesReasoning struct {
	Effort string `json:"effort,omitempty"` // "low", "medium", "high"
}

// ==================== OpenAI Responses Provider ====================

// OpenAIResponsesProvider implements the Provider interface for OpenAI's Responses API.
// Used when operator mode is enabled with GPT-5.4+ for native computer use support.
type OpenAIResponsesProvider struct {
	apiKey             string
	baseURL            string
	previousResponseID string // Track for chaining across turns
}

// NewOpenAIResponsesProvider creates a new Responses API provider
func NewOpenAIResponsesProvider() *OpenAIResponsesProvider {
	apiKey := os.Getenv("OPENAI_API_KEY")
	log.Printf("🔍 DEBUG NewOpenAIResponsesProvider: OPENAI_API_KEY length=%d, configured=%v", len(apiKey), apiKey != "")
	return &OpenAIResponsesProvider{
		apiKey:  apiKey,
		baseURL: "https://api.openai.com/v1",
	}
}

func (p *OpenAIResponsesProvider) IsConfigured() bool {
	return p.apiKey != ""
}

// APIKeyLen returns the length of the API key (for debugging without exposing the key)
func (p *OpenAIResponsesProvider) APIKeyLen() int {
	return len(p.apiKey)
}

// NewSession creates a per-request copy that can track previousResponseID
// without state leaking between concurrent conversations.
func (p *OpenAIResponsesProvider) NewSession() *OpenAIResponsesProvider {
	return &OpenAIResponsesProvider{
		apiKey:  p.apiKey,
		baseURL: p.baseURL,
	}
}

// SetPreviousResponseID implements ResponseIDTracker for conversation chaining
func (p *OpenAIResponsesProvider) SetPreviousResponseID(id string) {
	p.previousResponseID = id
	log.Printf("🔗 OpenAI Responses: set previous_response_id=%s", id)
}

// GetPreviousResponseID returns the current response ID for chaining
func (p *OpenAIResponsesProvider) GetPreviousResponseID() string {
	return p.previousResponseID
}

// GetRawStream implements the Provider interface for the Responses API
func (p *OpenAIResponsesProvider) GetRawStream(messages []stream.Message, customTools []tools.ToolDefinition, builtinTools []interface{}) (io.ReadCloser, error) {
	log.Printf("🔍 DEBUG GetRawStream: apiKey_len=%d, messages=%d, customTools=%d, builtinTools=%d",
		len(p.apiKey), len(messages), len(customTools), len(builtinTools))
	if !p.IsConfigured() {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}

	cfg := config.GetConfig()
	llmConfig := cfg.GetLLMConfig()

	// Build tools array
	var apiTools []interface{}

	// Add computer tool if configured in builtinTools
	for _, builtinTool := range builtinTools {
		if builtinConfig, ok := builtinTool.(config.BuiltinToolConfig); ok {
			if builtinConfig.Name == "computer" {
				displayWidth := 1024
				displayHeight := 768

				if builtinConfig.DisplayWidthPx != nil {
					displayWidth = *builtinConfig.DisplayWidthPx
				} else {
					operatorConfig := cfg.Get().Operator
					if operatorConfig != nil && operatorConfig.DisplayWidth > 0 {
						displayWidth = operatorConfig.DisplayWidth
					}
				}

				if builtinConfig.DisplayHeightPx != nil {
					displayHeight = *builtinConfig.DisplayHeightPx
				} else {
					operatorConfig := cfg.Get().Operator
					if operatorConfig != nil && operatorConfig.DisplayHeight > 0 {
						displayHeight = operatorConfig.DisplayHeight
					}
				}

				apiTools = append(apiTools, map[string]interface{}{
					"type":           "computer",
					"display_width":  displayWidth,
					"display_height": displayHeight,
					"environment":    "browser",
				})
				log.Printf("🖥️  OpenAI Responses: added computer tool (%dx%d)", displayWidth, displayHeight)
			}
		}
	}

	// Add custom function tools
	for _, toolDef := range customTools {
		apiTools = append(apiTools, map[string]interface{}{
			"type": "function",
			"name": toolDef.Name,
			"description": toolDef.Description,
			"parameters":  toolDef.InputSchema,
		})
	}

	// Translate messages to Responses API input format
	var systemPrompt string
	var input []interface{}

	for _, msg := range messages {
		if msg.Role == "system" {
			// System messages become the instructions field
			switch content := msg.Content.(type) {
			case string:
				systemPrompt = content
			case []stream.ContentBlock:
				for _, block := range content {
					if block.Type == "text" {
						systemPrompt += block.Text
					}
				}
			}
			continue
		}

		if msg.Role == "user" {
			// Check if this is a tool_result message
			if blocks, ok := msg.Content.([]stream.ContentBlock); ok {
				for _, block := range blocks {
					if block.Type == "tool_result" {
						// Convert tool results to appropriate Responses API format
						inputItem := p.convertToolResult(block)
						if inputItem != nil {
							input = append(input, inputItem)
						}
						continue
					}
				}
				// Check if we handled all blocks as tool results
				allToolResults := true
				for _, block := range blocks {
					if block.Type != "tool_result" {
						allToolResults = false
						break
					}
				}
				if allToolResults {
					continue
				}
			}

			// Regular user message
			contentParts := p.convertContentToResponsesFormat(msg.Content, "user")
			if contentParts != nil {
				input = append(input, map[string]interface{}{
					"type":    "message",
					"role":    "user",
					"content": contentParts,
				})
			}
		} else if msg.Role == "assistant" {
			// Check for tool_use blocks
			if blocks, ok := msg.Content.([]stream.ContentBlock); ok {
				hasToolUse := false
				for _, block := range blocks {
					if block.Type == "tool_use" {
						hasToolUse = true
						break
					}
				}

				if hasToolUse {
					// Build assistant message with function_call outputs
					var contentParts []interface{}
					for _, block := range blocks {
						if block.Type == "text" && block.Text != "" {
							contentParts = append(contentParts, map[string]interface{}{
								"type": "output_text",
								"text": block.Text,
							})
						} else if block.Type == "tool_use" {
							if block.Name == "computer" {
								// Convert to computer_call format
								contentParts = append(contentParts, p.convertToolUseToComputerCall(block))
							} else {
								// Regular function call
								argsJSON, _ := json.Marshal(block.Input)
								contentParts = append(contentParts, map[string]interface{}{
									"type":      "function_call",
									"id":        block.ID,
									"call_id":   block.ID,
									"name":      block.Name,
									"arguments": string(argsJSON),
								})
							}
						}
					}
					// In Responses API, assistant content goes as output items directly
					for _, part := range contentParts {
						input = append(input, part)
					}
					continue
				}
			}

			// Regular assistant message
			contentParts := p.convertContentToResponsesFormat(msg.Content, "assistant")
			if contentParts != nil {
				input = append(input, map[string]interface{}{
					"type":    "message",
					"role":    "assistant",
					"content": contentParts,
				})
			}
		}
	}

	// Build request
	req := OpenAIResponsesRequest{
		Model:  llmConfig.Model,
		Stream: true,
		Tools:  apiTools,
		Reasoning: &ResponsesReasoning{
			Effort: "medium",
		},
	}

	if systemPrompt != "" {
		req.Instructions = systemPrompt
	}

	// Use previous_response_id for chaining if available, otherwise send full input
	if p.previousResponseID != "" && len(input) > 0 {
		// When chaining, only send the new input items (tool results for the latest turn)
		// Find the new items: typically computer_call_output or function_call_output
		newItems := p.getNewInputItems(input)
		if len(newItems) > 0 {
			req.Input = newItems
			req.PreviousResponseID = p.previousResponseID
			log.Printf("🔗 OpenAI Responses: chaining with previous_response_id=%s, %d new items", p.previousResponseID, len(newItems))
		} else {
			req.Input = input
			log.Printf("🔗 OpenAI Responses: sending full input (%d items), no new items found for chaining", len(input))
		}
	} else {
		req.Input = input
		log.Printf("🔗 OpenAI Responses: sending full input (%d items)", len(input))
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error preparing request: %w", err)
	}

	// Log request size
	log.Printf("📊 OpenAI Responses API: request_size=%d, model=%s, tools=%d",
		len(reqBody), llmConfig.Model, len(apiTools))

	// Create HTTP request
	endpoint := p.baseURL + "/responses"
	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	client := &http.Client{}
	log.Printf("🔍 DEBUG: calling OpenAI Responses API at %s", endpoint)
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("🔴 DEBUG: HTTP request failed: %v", err)
		return nil, fmt.Errorf("error calling OpenAI Responses API: %w", err)
	}
	log.Printf("🔍 DEBUG: OpenAI Responses API returned status=%d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("🔴 DEBUG: OpenAI Responses API error body: %s", string(errorBody))
		return nil, fmt.Errorf("OpenAI Responses API error: %d - %s", resp.StatusCode, string(errorBody))
	}

	return resp.Body, nil
}

// convertContentToResponsesFormat converts message content to Responses API format
func (p *OpenAIResponsesProvider) convertContentToResponsesFormat(content interface{}, role string) interface{} {
	switch c := content.(type) {
	case string:
		if c == "" {
			return nil
		}
		return c

	case []stream.ContentBlock:
		var parts []interface{}
		for _, block := range c {
			switch block.Type {
			case "text":
				if block.Text != "" {
					parts = append(parts, map[string]interface{}{
						"type": "input_text",
						"text": block.Text,
					})
				}
			case "image":
				if block.Source != nil && block.Source.Data != "" {
					dataURL := fmt.Sprintf("data:%s;base64,%s", block.Source.MediaType, block.Source.Data)
					parts = append(parts, map[string]interface{}{
						"type":      "input_image",
						"image_url": dataURL,
						"detail":    "auto",
					})
				}
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return parts

	case []interface{}:
		var parts []interface{}
		for _, item := range c {
			if blockMap, ok := item.(map[string]interface{}); ok {
				blockType, _ := blockMap["type"].(string)
				switch blockType {
				case "text":
					if text, ok := blockMap["text"].(string); ok && text != "" {
						parts = append(parts, map[string]interface{}{
							"type": "input_text",
							"text": text,
						})
					}
				case "image":
					if source, ok := blockMap["source"].(map[string]interface{}); ok {
						mediaType, _ := source["media_type"].(string)
						data, _ := source["data"].(string)
						if data != "" {
							dataURL := fmt.Sprintf("data:%s;base64,%s", mediaType, data)
							parts = append(parts, map[string]interface{}{
								"type":      "input_image",
								"image_url": dataURL,
								"detail":    "auto",
							})
						}
					}
				}
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return parts
	}
	return nil
}

// convertToolResult converts a tool_result ContentBlock to Responses API format
func (p *OpenAIResponsesProvider) convertToolResult(block stream.ContentBlock) interface{} {
	toolUseID := block.ToolUseID

	// Check if this is a computer tool result (contains screenshot image)
	if contentBlocks, ok := block.Content.([]interface{}); ok {
		for _, item := range contentBlocks {
			if blockMap, ok := item.(map[string]interface{}); ok {
				if blockMap["type"] == "image" {
					if source, ok := blockMap["source"].(map[string]interface{}); ok {
						mediaType, _ := source["media_type"].(string)
						data, _ := source["data"].(string)
						if data != "" {
							dataURL := fmt.Sprintf("data:%s;base64,%s", mediaType, data)
							return map[string]interface{}{
								"type":    "computer_call_output",
								"call_id": toolUseID,
								"output": map[string]interface{}{
									"type":      "computer_screenshot",
									"image_url": dataURL,
								},
							}
						}
					}
				}
			}
		}
	}

	// Regular function tool result
	var outputStr string
	switch c := block.Content.(type) {
	case string:
		outputStr = c
	default:
		contentJSON, _ := json.Marshal(block.Content)
		outputStr = string(contentJSON)
	}

	return map[string]interface{}{
		"type":    "function_call_output",
		"call_id": toolUseID,
		"output":  outputStr,
	}
}

// convertToolUseToComputerCall converts a computer tool_use block to Responses API computer_call format
func (p *OpenAIResponsesProvider) convertToolUseToComputerCall(block stream.ContentBlock) interface{} {
	// The input from our operator format needs to be converted back to OpenAI's action format
	action, _ := block.Input["action"].(string)

	var actions []interface{}

	switch action {
	case "screenshot":
		actions = append(actions, map[string]interface{}{"type": "screenshot"})
	case "left_click", "click":
		openAIAction := map[string]interface{}{"type": "click", "button": "left"}
		if coord, ok := block.Input["coordinate"].([]interface{}); ok && len(coord) >= 2 {
			openAIAction["x"] = coord[0]
			openAIAction["y"] = coord[1]
		}
		actions = append(actions, openAIAction)
	case "right_click":
		openAIAction := map[string]interface{}{"type": "click", "button": "right"}
		if coord, ok := block.Input["coordinate"].([]interface{}); ok && len(coord) >= 2 {
			openAIAction["x"] = coord[0]
			openAIAction["y"] = coord[1]
		}
		actions = append(actions, openAIAction)
	case "double_click":
		openAIAction := map[string]interface{}{"type": "double_click"}
		if coord, ok := block.Input["coordinate"].([]interface{}); ok && len(coord) >= 2 {
			openAIAction["x"] = coord[0]
			openAIAction["y"] = coord[1]
		}
		actions = append(actions, openAIAction)
	case "type":
		openAIAction := map[string]interface{}{"type": "type"}
		if text, ok := block.Input["text"].(string); ok {
			openAIAction["text"] = text
		}
		actions = append(actions, openAIAction)
	case "key":
		openAIAction := map[string]interface{}{"type": "keypress"}
		if text, ok := block.Input["text"].(string); ok {
			// Convert "ctrl+c" to ["ctrl", "c"]
			keys := strings.Split(text, "+")
			var keyList []interface{}
			for _, k := range keys {
				keyList = append(keyList, strings.TrimSpace(k))
			}
			openAIAction["keys"] = keyList
		}
		actions = append(actions, openAIAction)
	case "scroll":
		openAIAction := map[string]interface{}{"type": "scroll"}
		if coord, ok := block.Input["coordinate"].([]interface{}); ok && len(coord) >= 2 {
			openAIAction["x"] = coord[0]
			openAIAction["y"] = coord[1]
		}
		dir, _ := block.Input["scroll_direction"].(string)
		amount, _ := block.Input["scroll_amount"].(float64)
		if amount == 0 {
			amount = 100
		}
		switch dir {
		case "up":
			openAIAction["scroll_x"] = 0
			openAIAction["scroll_y"] = -amount
		case "down":
			openAIAction["scroll_x"] = 0
			openAIAction["scroll_y"] = amount
		case "left":
			openAIAction["scroll_x"] = -amount
			openAIAction["scroll_y"] = 0
		case "right":
			openAIAction["scroll_x"] = amount
			openAIAction["scroll_y"] = 0
		}
		actions = append(actions, openAIAction)
	case "mouse_move":
		openAIAction := map[string]interface{}{"type": "move"}
		if coord, ok := block.Input["coordinate"].([]interface{}); ok && len(coord) >= 2 {
			openAIAction["x"] = coord[0]
			openAIAction["y"] = coord[1]
		}
		actions = append(actions, openAIAction)
	case "left_click_drag":
		openAIAction := map[string]interface{}{"type": "drag"}
		if start, ok := block.Input["start_coordinate"].([]interface{}); ok && len(start) >= 2 {
			openAIAction["start_x"] = start[0]
			openAIAction["start_y"] = start[1]
		}
		if end, ok := block.Input["coordinate"].([]interface{}); ok && len(end) >= 2 {
			openAIAction["end_x"] = end[0]
			openAIAction["end_y"] = end[1]
		}
		actions = append(actions, openAIAction)
	case "wait":
		actions = append(actions, map[string]interface{}{"type": "wait"})
	default:
		actions = append(actions, map[string]interface{}{"type": action})
	}

	return map[string]interface{}{
		"type":    "computer_call",
		"call_id": block.ID,
		"actions": actions,
		"status":  "completed",
	}
}

// getNewInputItems extracts only the new tool result items from the full input
// These are typically computer_call_output or function_call_output items
func (p *OpenAIResponsesProvider) getNewInputItems(input []interface{}) []interface{} {
	var newItems []interface{}
	for _, item := range input {
		if itemMap, ok := item.(map[string]interface{}); ok {
			itemType, _ := itemMap["type"].(string)
			if itemType == "computer_call_output" || itemType == "function_call_output" {
				newItems = append(newItems, item)
			}
		}
	}
	return newItems
}
