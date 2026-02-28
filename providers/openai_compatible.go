package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/stream"
	"github.com/apteva/agent/tools"
)

// needsFixedTemperature checks if a model requires a fixed temperature and doesn't allow custom values
// This applies to reasoning/thinking models like:
// - Moonshot's kimi-k2.5 (direct API)
// - Fireworks' kimi_k25, kimi-k2-thinking (Fireworks API naming)
func needsFixedTemperature(model string) bool {
	model = strings.ToLower(model)
	// Moonshot direct API naming
	if model == "kimi-k2.5" || strings.HasPrefix(model, "kimi-k2.5-") {
		return true
	}
	// Fireworks API naming for Kimi models (kimi_k25, kimi-k2-thinking, etc.)
	if strings.Contains(model, "kimi") && (strings.Contains(model, "k25") || strings.Contains(model, "k2") || strings.Contains(model, "thinking")) {
		return true
	}
	return false
}

// needsMaxCompletionTokens checks if a model requires max_completion_tokens instead of max_tokens
// This applies to newer OpenAI models (gpt-4o, gpt-5.x, o1, o3, etc.)
func needsMaxCompletionTokens(model string) bool {
	model = strings.ToLower(model)
	// gpt-4o and variants
	if strings.HasPrefix(model, "gpt-4o") {
		return true
	}
	// gpt-5.x models
	if strings.HasPrefix(model, "gpt-5") {
		return true
	}
	// o1, o3 reasoning models
	if strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") {
		return true
	}
	return false
}

// isThinkingModel checks if a model is a thinking/reasoning model that benefits from
// reasoning_history: "preserved" to maintain thinking context across tool calls.
// This helps models like Kimi K2 Thinking, MiniMax M2.5, DeepSeek R1, etc. properly
// use their reasoning to inform tool calls.
func isThinkingModel(model string) bool {
	model = strings.ToLower(model)
	// Kimi thinking models (K2 Thinking, K2.5)
	if strings.Contains(model, "kimi") && (strings.Contains(model, "thinking") || strings.Contains(model, "k2.5") || strings.Contains(model, "k2-5") || strings.Contains(model, "k2p5") || strings.Contains(model, "k25")) {
		return true
	}
	// GLM thinking models
	if strings.Contains(model, "glm") && strings.Contains(model, "thinking") {
		return true
	}
	// GLM heretic models (thinking/reasoning via Venice)
	if strings.Contains(model, "glm") && strings.Contains(model, "heretic") {
		return true
	}
	// DeepSeek R1 reasoning models
	if strings.Contains(model, "deepseek") && strings.Contains(model, "r1") {
		return true
	}
	// MiniMax reasoning models (M2.5, etc.) — includes Fireworks "minimax-m2p5"
	if strings.Contains(model, "minimax") {
		return true
	}
	// Qwen thinking models
	if strings.Contains(model, "qwen") && strings.Contains(model, "thinking") {
		return true
	}
	// Generic: any model with "-thinking" or "-reasoning" suffix pattern
	if strings.Contains(model, "-thinking") || strings.Contains(model, "-reasoning") {
		return true
	}
	return false
}

// getReasoningHistoryForModel returns the appropriate reasoning_history value for a model on Fireworks.
// Different models support different values:
//   - Kimi K2: supports disabled/interleaved/preserved (default: preserved)
//   - MiniMax M2/M2.5: supports disabled/interleaved only (NO preserved)
//   - GLM-4.7: supports disabled/interleaved/preserved (default: interleaved)
//   - GLM-4.6 and older: supports disabled/interleaved only
//   - Others: default to interleaved (safest)
func getReasoningHistoryForModel(model string) string {
	model = strings.ToLower(model)
	// Kimi K2 models default to preserved and support it
	if strings.Contains(model, "kimi") {
		return "preserved"
	}
	// MiniMax does NOT support preserved — only disabled/interleaved
	if strings.Contains(model, "minimax") {
		return "interleaved"
	}
	// GLM-4.7 supports preserved
	if strings.Contains(model, "glm") && (strings.Contains(model, "4.7") || strings.Contains(model, "4p7")) {
		return "preserved"
	}
	// Default: interleaved is the safest option supported by all thinking models
	return "interleaved"
}

// needsThinkTagsInContent returns true if the provider expects <think> tags in content
// rather than a separate reasoning_content field. This is for providers that don't support
// reasoning_history or reasoning_content natively — we reconstruct <think> tags as a safety net.
func needsThinkTagsInContent(baseURL string, model string) bool {
	// Fireworks uses reasoning_content field with reasoning_history
	if strings.Contains(baseURL, "fireworks.ai") {
		return false
	}
	// Together AI uses "reasoning" field natively for thinking models
	if strings.Contains(baseURL, "together.xyz") {
		return false
	}
	// Direct MiniMax with reasoning_split=true uses reasoning_details
	if strings.Contains(baseURL, "minimax") {
		return false
	}
	// Moonshot Kimi uses reasoning_content natively
	if strings.Contains(baseURL, "moonshot.ai") {
		return false
	}
	// For unknown providers with thinking models, reconstruct <think> tags in content
	return isThinkingModel(model)
}

// shouldStripReasoningBetweenTurns returns true if reasoning_content should be removed
// from assistant messages that precede a new user turn (not during tool call loops).
// DeepSeek's API returns 400 if reasoning_content is present in older turns.
// Fireworks with reasoning_history handles this server-side, so we don't strip.
func shouldStripReasoningBetweenTurns(baseURL string, model string) bool {
	model = strings.ToLower(model)
	// DeepSeek native API: MUST remove reasoning_content between user turns
	if strings.Contains(baseURL, "deepseek.com") && strings.Contains(model, "deepseek") {
		return true
	}
	// Fireworks handles it via reasoning_history parameter — no stripping needed
	// Together, MiniMax, Moonshot — reasoning should be preserved
	return false
}

// OpenAICompatibleProvider provides a reusable base for all OpenAI-compatible APIs
// This includes: OpenAI, Groq, Together AI, Perplexity, Fireworks, and others
type OpenAICompatibleProvider struct {
	apiKey     string
	baseURL    string
	authHeader string // e.g., "Authorization"
	authPrefix string // e.g., "Bearer "
	keyEnvVar  string // For error messages
}

// NewOpenAICompatibleProvider creates a new OpenAI-compatible provider
func NewOpenAICompatibleProvider(apiKey, baseURL, authHeader, authPrefix, keyEnvVar string) *OpenAICompatibleProvider {
	return &OpenAICompatibleProvider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		authHeader: authHeader,
		authPrefix: authPrefix,
		keyEnvVar:  keyEnvVar,
	}
}

func (p *OpenAICompatibleProvider) IsConfigured() bool {
	return p.apiKey != ""
}

func (p *OpenAICompatibleProvider) GetRawStream(messages []stream.Message, customTools []tools.ToolDefinition, builtinTools []interface{}) (io.ReadCloser, error) {
	if !p.IsConfigured() {
		return nil, fmt.Errorf("%s not set", p.keyEnvVar)
	}

	// Get current configuration
	cfg := config.GetConfig()
	llmConfig := cfg.GetLLMConfig()

	// Convert custom tool definitions to OpenAI format
	// Note: OpenAI uses "parameters" instead of Anthropic's "input_schema"
	var openaiTools []interface{}
	for _, toolDef := range customTools {
		openaiTools = append(openaiTools, OpenAIToolDef{
			Type: "function",
			Function: OpenAIFunctionDef{
				Name:        toolDef.Name,
				Description: toolDef.Description,
				Parameters:  toolDef.InputSchema,
			},
		})
	}

	// Handle built-in tools: Moonshot supports $web_search as builtin_function
	isMoonshot := strings.Contains(p.baseURL, "api.moonshot.ai")
	if len(builtinTools) > 0 {
		if isMoonshot {
			for _, builtinTool := range builtinTools {
				if builtinConfig, ok := builtinTool.(config.BuiltinToolConfig); ok {
					if builtinConfig.Name == "web_search" {
						log.Printf("🌐 Moonshot: injecting $web_search builtin_function tool")
						openaiTools = append(openaiTools, map[string]interface{}{
							"type": "builtin_function",
							"function": map[string]interface{}{
								"name": "$web_search",
							},
						})
					} else {
						log.Printf("Warning: Moonshot provider doesn't support built-in tool: %s, ignoring", builtinConfig.Name)
					}
				}
			}
		} else {
			log.Printf("Warning: %s provider doesn't support built-in tools, ignoring %d built-in tools", p.keyEnvVar, len(builtinTools))
		}
	}

	// Translate messages to OpenAI format
	// For providers that need reasoning stripped between user turns (DeepSeek),
	// identify which assistant messages precede a user turn (vs tool loop)
	stripReasoning := shouldStripReasoningBetweenTurns(p.baseURL, llmConfig.Model)
	translator := &OpenAITranslator{}
	var openaiMessages []OpenAIMessage
	var hasSystemMessage bool
	for i, msg := range messages {
		// Strip reasoning_content from assistant messages that precede a user turn
		// (not during tool call loops where the next message is role:tool/tool_result)
		if stripReasoning && msg.Role == "assistant" && msg.ReasoningContent != "" {
			// Check if this is followed by a user message (not a tool result)
			if i+1 < len(messages) && messages[i+1].Role == "user" {
				// Check if the next user message is NOT a tool_result
				isToolResult := false
				if blocks, ok := messages[i+1].Content.([]interface{}); ok {
					for _, block := range blocks {
						if blockMap, ok := block.(map[string]interface{}); ok {
							if blockMap["type"] == "tool_result" {
								isToolResult = true
								break
							}
						}
					}
				}
				if !isToolResult {
					msg.ReasoningContent = "" // Strip between user turns
					msg.ReasoningDetails = nil
					log.Printf("🧠 Stripped reasoning_content from assistant message before user turn (DeepSeek compat)")
				}
			}
		}
		msg := msg // shadow to avoid modifying the original slice
		// Handle tool-related messages specially for OpenAI format
		if msg.Role == "assistant" {
			// Check if this is a tool_use message
			if blocks, ok := msg.Content.([]stream.ContentBlock); ok {
				hasToolUse := false
				for _, block := range blocks {
					if block.Type == "tool_use" {
						hasToolUse = true
						break
					}
				}

				if hasToolUse {
					// Convert to OpenAI tool_calls format
					var toolCalls []map[string]interface{}
					var textContent string

					for _, block := range blocks {
						if block.Type == "tool_use" {
							// Convert input map to JSON string
							argsJSON, _ := json.Marshal(block.Input)
							toolCalls = append(toolCalls, map[string]interface{}{
								"id":   block.ID,
								"type": "function",
								"function": map[string]interface{}{
									"name":      block.Name,
									"arguments": string(argsJSON),
								},
							})
						} else if block.Type == "text" {
							textContent += block.Text
						}
					}

					openaiMsg := OpenAIMessage{
						Role: "assistant",
					}
					// Preserve thinking content for thinking models
					if msg.ReasoningContent != "" {
						if needsThinkTagsInContent(p.baseURL, llmConfig.Model) {
							// Reconstruct <think> tags in content for providers that need it
							thinkContent := "<think>\n" + msg.ReasoningContent + "\n</think>\n"
							if textContent != "" {
								openaiMsg.Content = thinkContent + textContent
							} else {
								openaiMsg.Content = thinkContent
							}
							log.Printf("🧠 Reconstructed <think> tags in content: %d chars reasoning", len(msg.ReasoningContent))
						} else {
							if textContent != "" {
								openaiMsg.Content = textContent
							}
							openaiMsg.ReasoningContent = msg.ReasoningContent
							log.Printf("🧠 Including reasoning_content in API message: %d chars", len(msg.ReasoningContent))
						}
					} else if textContent != "" {
						openaiMsg.Content = textContent
					}
					openaiMsg.ToolCalls = toolCalls
					// Ensure content is explicitly set (not omitted) for tool_calls messages
					// Some strict APIs require "content": null or "content": "" alongside tool_calls
					if openaiMsg.Content == nil {
						openaiMsg.Content = ""
					}
					// Preserve reasoning_details for MiniMax M2.5
					if len(msg.ReasoningDetails) > 0 {
						openaiMsg.ReasoningDetails = msg.ReasoningDetails
						log.Printf("🧠 Including reasoning_details in API message: %d entries", len(msg.ReasoningDetails))
					}

					openaiMessages = append(openaiMessages, openaiMsg)
					continue
				}
			}
		} else if msg.Role == "user" {
			// Check if this is a tool_result message
			if blocks, ok := msg.Content.([]stream.ContentBlock); ok {
				hasToolResult := false
				for _, block := range blocks {
					if block.Type == "tool_result" {
						hasToolResult = true
						break
					}
				}

				if hasToolResult {
					// Convert to OpenAI tool role format
					for _, block := range blocks {
						if block.Type == "tool_result" {
							// Check if content has image blocks (from browser/computer tool screenshots)
							var content interface{}
							if contentBlocks, ok := block.Content.([]interface{}); ok && hasImageBlocks(contentBlocks) {
								// Convert to OpenAI multimodal content parts so vision models can see screenshots
								content = convertToOpenAIMultimodal(contentBlocks)
							} else {
								switch c := block.Content.(type) {
								case string:
									content = c
								default:
									contentJSON, _ := json.Marshal(block.Content)
									content = string(contentJSON)
								}
							}

							openaiMessages = append(openaiMessages, OpenAIMessage{
								Role:       "tool",
								Content:    content,
								ToolCallID: block.ToolUseID,
							})
						}
					}
					continue
				}
			}
		}

		// Normal message translation
		translatedContent, err := translator.TranslateMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("error translating message: %w", err)
		}
		openaiMsg := OpenAIMessage{
			Role:    msg.Role,
			Content: translatedContent,
		}
		// Preserve thinking content for thinking models
		if msg.Role == "assistant" && msg.ReasoningContent != "" {
			if needsThinkTagsInContent(p.baseURL, llmConfig.Model) {
				// Reconstruct <think> tags in content for providers that need it
				thinkContent := "<think>\n" + msg.ReasoningContent + "\n</think>\n"
				if contentStr, ok := translatedContent.(string); ok && contentStr != "" {
					openaiMsg.Content = thinkContent + contentStr
				} else {
					openaiMsg.Content = thinkContent
				}
				log.Printf("🧠 Reconstructed <think> tags in content: %d chars reasoning", len(msg.ReasoningContent))
			} else {
				openaiMsg.ReasoningContent = msg.ReasoningContent
				log.Printf("🧠 Including reasoning_content in API message: %d chars", len(msg.ReasoningContent))
			}
		}
		// Preserve reasoning_details for MiniMax M2.5
		if msg.Role == "assistant" && len(msg.ReasoningDetails) > 0 {
			openaiMsg.ReasoningDetails = msg.ReasoningDetails
			log.Printf("🧠 Including reasoning_details in API message: %d entries", len(msg.ReasoningDetails))
		}
		openaiMessages = append(openaiMessages, openaiMsg)
		if msg.Role == "system" {
			hasSystemMessage = true
		}
	}

	// Log system message status for debugging
	if hasSystemMessage {
		log.Printf("%s API - System message included in messages", p.keyEnvVar)
	}

	// Check if this is actual OpenAI API and a newer model
	isActualOpenAI := strings.Contains(p.baseURL, "api.openai.com")
	isNewerModel := needsMaxCompletionTokens(llmConfig.Model)

	// Prepare OpenAI request using config
	openaiReq := OpenAIRequest{
		Model:         llmConfig.Model,
		Messages:      openaiMessages,
		Stream:        true,
		StreamOptions: &StreamOptions{IncludeUsage: true},
		Tools:         openaiTools,
	}

	// Newer OpenAI models (gpt-5.x, o1, o3) don't support custom temperature
	// Moonshot reasoning models (kimi-k2.5) require fixed temperature (only 1 is allowed)
	// Only set temperature for models that support custom values
	if !(isActualOpenAI && isNewerModel) && !needsFixedTemperature(llmConfig.Model) {
		openaiReq.Temperature = llmConfig.Temperature
	}

	// Use max_completion_tokens for newer OpenAI models, max_tokens for others
	if isActualOpenAI && isNewerModel {
		openaiReq.MaxCompletionTokens = llmConfig.MaxTokens
		// Set reasoning effort for newer OpenAI models
		openaiReq.ReasoningEffort = "medium"
	} else {
		openaiReq.MaxTokens = llmConfig.MaxTokens
	}

	// For thinking models on Fireworks, set appropriate reasoning_history per model
	// Together AI does NOT support reasoning_history parameter (tool calling + reasoning are mutually exclusive)
	isFireworks := strings.Contains(p.baseURL, "fireworks.ai")
	isTogether := strings.Contains(p.baseURL, "together.xyz")
	if isFireworks && isThinkingModel(llmConfig.Model) {
		rh := getReasoningHistoryForModel(llmConfig.Model)
		openaiReq.ReasoningHistory = rh
		log.Printf("🧠 Fireworks: using reasoning_history=%s for model: %s", rh, llmConfig.Model)
	}
	_ = isTogether // Together doesn't support reasoning_history

	// For direct MiniMax API: send reasoning_split=true to get structured reasoning_details
	// This is the recommended approach for MiniMax M2.5 interleaved thinking
	isMiniMaxDirect := strings.Contains(p.baseURL, "minimax")
	if isMiniMaxDirect && isThinkingModel(llmConfig.Model) {
		reasoningSplit := true
		openaiReq.ReasoningSplit = &reasoningSplit
		log.Printf("🧠 MiniMax direct: setting reasoning_split=true for model: %s", llmConfig.Model)
	}

	reqBody, err := json.Marshal(openaiReq)
	if err != nil {
		return nil, fmt.Errorf("error preparing request: %w", err)
	}

	// Construct endpoint URL
	endpoint := p.baseURL + "/chat/completions"
	log.Printf("🔗 %s API call: %s (model: %s)", p.keyEnvVar, endpoint, llmConfig.Model)

	// Create HTTP request
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(p.authHeader, p.authPrefix+p.apiKey)

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error calling %s API: %w", p.keyEnvVar, err)
	}

	if resp.StatusCode != http.StatusOK {
		// Read error response for debugging
		errorBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("%s API error: %d - %s", p.keyEnvVar, resp.StatusCode, string(errorBody))
	}

	return resp.Body, nil
}

// StreamChat provides backward compatibility for streaming
func (p *OpenAICompatibleProvider) StreamChat(w http.ResponseWriter, message string) error {
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
	for scanner.Scan() {
		line := scanner.Text()

		// Pass through all lines from the stream
		fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading stream: %v", err)
		return err
	}

	return nil
}

// hasMediaBlocks checks if content blocks contain any image or document blocks (e.g., from browser screenshots or PDF tools)
func hasMediaBlocks(blocks []interface{}) bool {
	for _, block := range blocks {
		if blockMap, ok := block.(map[string]interface{}); ok {
			blockType := blockMap["type"]
			if blockType == "image" || blockType == "document" {
				return true
			}
		}
	}
	return false
}

// hasImageBlocks is kept for backward compatibility
func hasImageBlocks(blocks []interface{}) bool {
	return hasMediaBlocks(blocks)
}

// convertToOpenAIMultimodal converts Anthropic-style content blocks (with images/documents) to OpenAI multimodal format.
// This allows vision models to actually see browser screenshots in tool results.
// Document blocks (PDFs) are converted to text placeholders since OpenAI doesn't support native PDF processing.
func convertToOpenAIMultimodal(blocks []interface{}) []interface{} {
	var parts []interface{}
	for _, block := range blocks {
		blockMap, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		switch blockMap["type"] {
		case "text":
			if text, ok := blockMap["text"].(string); ok {
				parts = append(parts, map[string]interface{}{
					"type": "text",
					"text": text,
				})
			}
		case "image":
			if source, ok := blockMap["source"].(map[string]interface{}); ok {
				mediaType, _ := source["media_type"].(string)
				data, _ := source["data"].(string)
				if mediaType != "" && data != "" {
					dataURL := fmt.Sprintf("data:%s;base64,%s", mediaType, data)
					parts = append(parts, map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": dataURL,
						},
					})
				}
			}
		case "document":
			// OpenAI doesn't support native PDF/document processing
			// Convert to text placeholder instead of sending raw base64
			mediaType := "document"
			if source, ok := blockMap["source"].(map[string]interface{}); ok {
				if mt, ok := source["media_type"].(string); ok {
					mediaType = mt
				}
			}
			parts = append(parts, map[string]interface{}{
				"type": "text",
				"text": fmt.Sprintf("[%s document provided - this provider does not support native document processing]", mediaType),
			})
		}
	}
	return parts
}
