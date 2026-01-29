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

	"agent-server/config"
	"agent-server/stream"
	"agent-server/tools"
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
// This helps models like Kimi K2 Thinking properly use their reasoning to inform tool calls.
func isThinkingModel(model string) bool {
	model = strings.ToLower(model)
	// Kimi thinking models (K2 Thinking, K2.5)
	if strings.Contains(model, "kimi") && (strings.Contains(model, "thinking") || strings.Contains(model, "k2.5") || strings.Contains(model, "k2-5")) {
		return true
	}
	// GLM thinking models
	if strings.Contains(model, "glm") && strings.Contains(model, "thinking") {
		return true
	}
	// DeepSeek R1 reasoning models
	if strings.Contains(model, "deepseek") && strings.Contains(model, "r1") {
		return true
	}
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
	translator := &OpenAITranslator{}
	var openaiMessages []OpenAIMessage
	var hasSystemMessage bool
	for _, msg := range messages {
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
					if textContent != "" {
						openaiMsg.Content = textContent
					}
					openaiMsg.ToolCalls = toolCalls
					// Preserve reasoning_content for thinking models (Kimi K2, etc.)
					if msg.ReasoningContent != "" {
						openaiMsg.ReasoningContent = msg.ReasoningContent
						log.Printf("🧠 Including reasoning_content in API message: %d chars", len(msg.ReasoningContent))
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
							var resultText string
							switch c := block.Content.(type) {
							case string:
								resultText = c
							default:
								contentJSON, _ := json.Marshal(block.Content)
								resultText = string(contentJSON)
							}

							openaiMessages = append(openaiMessages, OpenAIMessage{
								Role:       "tool",
								Content:    resultText,
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
		// Preserve reasoning_content for thinking models (Kimi K2, etc.)
		if msg.Role == "assistant" && msg.ReasoningContent != "" {
			openaiMsg.ReasoningContent = msg.ReasoningContent
			log.Printf("🧠 Including reasoning_content in API message: %d chars", len(msg.ReasoningContent))
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
		Model:    llmConfig.Model,
		Messages: openaiMessages,
		Stream:   true,
		Tools:    openaiTools,
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

	// For thinking models on Fireworks/Together (and other compatible APIs), use preserved reasoning history
	// This ensures the model's thinking is preserved across tool calls, improving tool argument quality
	isFireworks := strings.Contains(p.baseURL, "fireworks.ai")
	isTogether := strings.Contains(p.baseURL, "together.xyz")
	if (isFireworks || isTogether) && isThinkingModel(llmConfig.Model) {
		openaiReq.ReasoningHistory = "preserved"
		log.Printf("🧠 Using reasoning_history=preserved for thinking model: %s", llmConfig.Model)
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
