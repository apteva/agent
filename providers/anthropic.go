package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"agent-server/config"
	"agent-server/stream"
	"agent-server/tools"
)

type AnthropicRequest struct {
	Model       string                     `json:"model"`
	MaxTokens   int                        `json:"max_tokens"`
	Messages    []stream.Message           `json:"messages"`
	System      string                     `json:"system,omitempty"`
	Stream      bool                       `json:"stream"`
	Temperature *float64                   `json:"temperature,omitempty"`
	Tools       []interface{}              `json:"tools,omitempty"` // Mixed: custom + built-in tools
}

type AnthropicContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
}

type AnthropicStreamResponse struct {
	Type  string      `json:"type"`
	Delta ContentDelta `json:"delta,omitempty"`
}

type ContentDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicProvider struct {
	apiKey string
}

func NewAnthropicProvider() *AnthropicProvider {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	return &AnthropicProvider{
		apiKey: apiKey,
	}
}

func (p *AnthropicProvider) IsConfigured() bool {
	return p.apiKey != ""
}

func (p *AnthropicProvider) GetRawStream(messages []stream.Message, customTools []tools.ToolDefinition, builtinTools []interface{}) (io.ReadCloser, error) {
	if !p.IsConfigured() {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	// Get current configuration
	cfg := config.GetConfig()
	llmConfig := cfg.GetLLMConfig()

	// Combine custom and built-in tools
	var allTools []interface{}

	// Add custom tools
	for _, toolDef := range customTools {
		allTools = append(allTools, toolDef)
	}

	// Add built-in tools (convert from config format to API format)
	for _, builtinTool := range builtinTools {
		if builtinConfig, ok := builtinTool.(config.BuiltinToolConfig); ok {
			// Convert config object to API format
			apiTool := map[string]interface{}{
				"type": builtinConfig.Type,
				"name": builtinConfig.Name,
			}

			// Special handling for computer tool
			if builtinConfig.Type == "computer_20250124" {
				// Priority: builtin tool config > operator config > defaults
				displayWidth := 1024
				displayHeight := 768
				displayNumber := 1

				// First check if specified in builtin tool config
				if builtinConfig.DisplayWidthPx != nil {
					displayWidth = *builtinConfig.DisplayWidthPx
				} else {
					// Fall back to operator config
					cfg := config.GetConfig()
					operatorConfig := cfg.Get().Operator
					if operatorConfig != nil && operatorConfig.DisplayWidth > 0 {
						displayWidth = operatorConfig.DisplayWidth
					}
				}

				if builtinConfig.DisplayHeightPx != nil {
					displayHeight = *builtinConfig.DisplayHeightPx
				} else {
					// Fall back to operator config
					cfg := config.GetConfig()
					operatorConfig := cfg.Get().Operator
					if operatorConfig != nil && operatorConfig.DisplayHeight > 0 {
						displayHeight = operatorConfig.DisplayHeight
					}
				}

				if builtinConfig.DisplayNumber != nil {
					displayNumber = *builtinConfig.DisplayNumber
				}

				apiTool["display_width_px"] = displayWidth
				apiTool["display_height_px"] = displayHeight
				apiTool["display_number"] = displayNumber
			} else {
				// Add optional parameters for other builtin tools
				if builtinConfig.MaxUses != nil {
					apiTool["max_uses"] = *builtinConfig.MaxUses
				}
				if len(builtinConfig.AllowedDomains) > 0 {
					apiTool["allowed_domains"] = builtinConfig.AllowedDomains
				}
				if len(builtinConfig.BlockedDomains) > 0 {
					apiTool["blocked_domains"] = builtinConfig.BlockedDomains
				}
				if builtinConfig.Citations != nil {
					apiTool["citations"] = builtinConfig.Citations
				}
				if builtinConfig.MaxContentTokens != nil {
					apiTool["max_content_tokens"] = *builtinConfig.MaxContentTokens
				}
			}

			allTools = append(allTools, apiTool)
		}
	}

	// Extract system message if present and filter it out from messages
	var systemPrompt string
	var filteredMessages []stream.Message
	for _, msg := range messages {
		if msg.Role == "system" {
			// Handle Content which could be string or []ContentBlock
			switch content := msg.Content.(type) {
			case string:
				systemPrompt = content
			case []stream.ContentBlock:
				// For system messages, we only care about text blocks
				for _, block := range content {
					if block.Type == "text" {
						systemPrompt += block.Text
					}
				}
			}
		} else {
			// For non-system messages, ensure tool_use blocks have input field
			processedMsg := msg
			if blocks, ok := msg.Content.([]stream.ContentBlock); ok {
				for i := range blocks {
					if blocks[i].Type == "tool_use" {
						// DEBUG: Log computer tool before sending to Anthropic
						if blocks[i].Name == "computer" {
							inputJSON, _ := json.Marshal(blocks[i].Input)
							log.Printf("🖥️  COMPUTER TOOL - Sending to Anthropic API: id=%s, input=%s", blocks[i].ID, string(inputJSON))
						}
						// Ensure Input is not nil for tool_use blocks
						if blocks[i].Input == nil {
							blocks[i].Input = map[string]interface{}{}
							log.Printf("⚠️  Fixed nil input for tool_use block: %s", blocks[i].Name)
							if blocks[i].Name == "computer" {
								log.Printf("🖥️  COMPUTER TOOL - Had nil input, fixed with empty map")
							}
						}
					}
				}
				processedMsg.Content = blocks
			} else {
				// DEBUG: Content is not []stream.ContentBlock, log what it actually is
				log.Printf("⚠️  DEBUG: Message role=%s has content type %T (not []ContentBlock)", msg.Role, msg.Content)
			}
			filteredMessages = append(filteredMessages, processedMsg)
		}
	}

	// Add parallel tools prompt if enabled and tools are available
	finalSystemPrompt := systemPrompt
	if len(allTools) > 0 && config.IsParallelToolsEnabled(llmConfig) {
		parallelPrompt := config.GetParallelToolsPrompt()
		if finalSystemPrompt != "" {
			finalSystemPrompt += parallelPrompt
		} else {
			finalSystemPrompt = strings.TrimSpace(parallelPrompt)
		}
		log.Printf("🔀 Parallel tools enabled - injected parallel execution prompt")
	}

	// Prepare Anthropic request
	anthropicReq := AnthropicRequest{
		Model:       llmConfig.Model,
		MaxTokens:   llmConfig.MaxTokens,
		Temperature: llmConfig.Temperature,
		Messages:    filteredMessages,
		System:      finalSystemPrompt,
		Stream:      true,
		Tools:       allTools,
	}

	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("error preparing request: %w", err)
	}

	// DEBUG: Log full request to check tool_use blocks
	var prettyReq map[string]interface{}
	json.Unmarshal(reqBody, &prettyReq)
	if messages, ok := prettyReq["messages"].([]interface{}); ok {
		for i, msg := range messages {
			if msgMap, ok := msg.(map[string]interface{}); ok {
				if content, ok := msgMap["content"].([]interface{}); ok {
					for j, block := range content {
						if blockMap, ok := block.(map[string]interface{}); ok {
							if blockMap["type"] == "tool_use" {
								blockJSON, _ := json.MarshalIndent(blockMap, "", "  ")
								log.Printf("🔍 DEBUG: Message[%d].Content[%d] tool_use block:\n%s", i, j, string(blockJSON))
							}
						}
					}
				}
			}
		}
	}

	// DEBUG: Log request size breakdown
	totalSize := len(reqBody)
	messageCount := len(filteredMessages)

	// Calculate approximate token count (rough estimate: 1 token ≈ 4 characters)
	estimatedTokens := totalSize / 4

	// Find largest message
	largestMsgSize := 0
	largestMsgType := ""
	for _, msg := range filteredMessages {
		msgJSON, _ := json.Marshal(msg)
		msgSize := len(msgJSON)
		if msgSize > largestMsgSize {
			largestMsgSize = msgSize
			largestMsgType = msg.Role
			// Check if it contains tool_result with image data
			if blocks, ok := msg.Content.([]stream.ContentBlock); ok {
				for _, block := range blocks {
					if block.Type == "tool_result" && len(fmt.Sprintf("%v", block.Content)) > 100000 {
						largestMsgType = msg.Role + " (tool_result with image)"
					}
				}
			}
		}
	}

	log.Printf("📊 API Request: total=%s (%s), messages=%d, est_tokens=%d, largest_msg=%s (%s)",
		formatBytes(totalSize), formatBytes(totalSize), messageCount, estimatedTokens,
		largestMsgType, formatBytes(largestMsgSize))


	// Create HTTP request to Anthropic
	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// Add beta headers for built-in tools if we have any
	if len(builtinTools) > 0 {
		// Check if we have computer tool
		hasComputerTool := false
		for _, builtinTool := range builtinTools {
			if builtinConfig, ok := builtinTool.(config.BuiltinToolConfig); ok {
				if builtinConfig.Type == "computer_20250124" {
					hasComputerTool = true
					break
				}
			}
		}

		if hasComputerTool {
			req.Header.Set("anthropic-beta", "computer-use-2025-01-24")
		} else {
			req.Header.Set("anthropic-beta", "web-fetch-2025-09-10")
		}
	}

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error calling Anthropic API: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Read error response body for debugging
		errorBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("Anthropic API error response: %s", string(errorBody))
		return nil, fmt.Errorf("Anthropic API error: %d - %s", resp.StatusCode, string(errorBody))
	}

	return resp.Body, nil
}

// formatBytes converts bytes to human readable format
func formatBytes(bytes int) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}


// Keep the old method for backward compatibility during transition
func (p *AnthropicProvider) StreamChat(w http.ResponseWriter, message string) error {
	messages := []stream.Message{{Role: "user", Content: message}}
	rawStream, err := p.GetRawStream(messages, nil, nil)
	if err != nil {
		return err
	}
	defer rawStream.Close()

	// Set headers for streaming
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

	// Stream the response
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	scanner := bufio.NewScanner(rawStream)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024) // Start 64KB, grow to 2MB max for large server tool results
	for scanner.Scan() {
		line := scanner.Text()

		// Pass through all lines from Claude's stream
		fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading stream: %v", err)
		return err
	}

	return nil
}