package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/stream"
	"github.com/apteva/agent/tools"
)

// ==================== Gemini Request/Response Types ====================

// GeminiRequest is the request structure for Gemini API
type GeminiRequest struct {
	Contents          []GeminiContent         `json:"contents"`
	SystemInstruction *GeminiContent          `json:"system_instruction,omitempty"`
	GenerationConfig  *GeminiGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []GeminiToolConfig      `json:"tools,omitempty"`
}

// GeminiGenerationConfig for model parameters
type GeminiGenerationConfig struct {
	Temperature     *float64              `json:"temperature,omitempty"`
	TopP            *float64              `json:"topP,omitempty"`
	TopK            *int                  `json:"topK,omitempty"`
	MaxOutputTokens int                   `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *GeminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

// GeminiThinkingConfig for thinking mode (2.5 models)
type GeminiThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"` // 0 = disabled
}

// GeminiToolConfig wraps function declarations
type GeminiToolConfig struct {
	FunctionDeclarations []GeminiFunction `json:"functionDeclarations"`
}

// GeminiFunction represents a tool definition
type GeminiFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema
}

// ==================== Gemini Provider ====================

// GeminiProvider implements the Provider interface for Google Gemini API
type GeminiProvider struct {
	apiKey string
}

// NewGeminiProvider creates a new Gemini provider instance
func NewGeminiProvider() *GeminiProvider {
	apiKey := os.Getenv("GEMINI_API_KEY")
	return &GeminiProvider{
		apiKey: apiKey,
	}
}

// IsConfigured checks if the provider has an API key
func (p *GeminiProvider) IsConfigured() bool {
	return p.apiKey != ""
}

// GetRawStream implements the Provider interface for streaming responses
func (p *GeminiProvider) GetRawStream(messages []stream.Message, customTools []tools.ToolDefinition, builtinTools []interface{}) (io.ReadCloser, error) {
	if !p.IsConfigured() {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}

	// Get current configuration
	cfg := config.GetConfig()
	llmConfig := cfg.GetLLMConfig()

	// Create a fresh translator for each request to avoid stale tool name mappings
	translator := NewGeminiTranslator()

	// Log incoming messages for debugging
	log.Printf("🔍 Gemini: Received %d messages for translation", len(messages))
	for i, msg := range messages {
		contentType := fmt.Sprintf("%T", msg.Content)
		contentPreview := ""
		switch c := msg.Content.(type) {
		case string:
			if len(c) > 100 {
				contentPreview = c[:100] + "..."
			} else {
				contentPreview = c
			}
		case []stream.ContentBlock:
			contentPreview = fmt.Sprintf("[%d content blocks]", len(c))
		case []interface{}:
			contentPreview = fmt.Sprintf("[%d interface blocks]", len(c))
		}
		log.Printf("   [%d] role=%s, type=%s, preview=%s", i, msg.Role, contentType, contentPreview)
	}

	// Translate messages to Gemini format
	var geminiContents []GeminiContent
	var systemInstruction *GeminiContent

	for _, msg := range messages {
		if msg.Role == "system" {
			// Extract system instruction separately
			content, err := translator.TranslateMessage(msg)
			if err != nil {
				return nil, fmt.Errorf("error translating system message: %w", err)
			}
			if geminiContent, ok := content.(GeminiContent); ok {
				// Remove role from system instruction (Gemini doesn't use it)
				if len(geminiContent.Parts) > 0 {
					systemInstruction = &GeminiContent{
						Parts: geminiContent.Parts,
					}
					log.Printf("   ✓ System instruction extracted with %d parts", len(geminiContent.Parts))
				}
			}
		} else {
			// Translate regular messages
			content, err := translator.TranslateMessage(msg)
			if err != nil {
				log.Printf("   ✗ Failed to translate message: %v", err)
				return nil, fmt.Errorf("error translating message: %w", err)
			}
			if geminiContent, ok := content.(GeminiContent); ok {
				// Skip messages with empty parts - Gemini requires content
				if len(geminiContent.Parts) == 0 {
					log.Printf("   ⚠ Skipping %s message with empty parts", msg.Role)
					continue
				}
				geminiContents = append(geminiContents, geminiContent)
				log.Printf("   ✓ Translated %s message to Gemini format with %d parts", msg.Role, len(geminiContent.Parts))
			} else {
				log.Printf("   ✗ Type assertion failed for message: got %T", content)
			}
		}
	}

	// Translate tools to Gemini format
	var geminiTools []GeminiToolConfig
	if len(customTools) > 0 {
		var functions []GeminiFunction
		for _, tool := range customTools {
			// Clean the schema for Gemini (remove unsupported fields like "examples")
			cleanedSchema := cleanSchemaForGemini(tool.InputSchema)

			functions = append(functions, GeminiFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  cleanedSchema,
			})
		}
		geminiTools = append(geminiTools, GeminiToolConfig{
			FunctionDeclarations: functions,
		})
	}

	// Note: Gemini doesn't support built-in tools like web_search yet
	if len(builtinTools) > 0 {
		log.Printf("⚠️  Gemini provider doesn't support built-in tools, ignoring %d built-in tools", len(builtinTools))
	}

	// Prepare generation config (let model use default thinking behavior)
	generationConfig := &GeminiGenerationConfig{
		MaxOutputTokens: llmConfig.MaxTokens,
		Temperature:     llmConfig.Temperature,
	}

	// Prepare Gemini request
	geminiReq := GeminiRequest{
		Contents:          geminiContents,
		SystemInstruction: systemInstruction,
		GenerationConfig:  generationConfig,
		Tools:             geminiTools,
	}

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("error preparing request: %w", err)
	}

	// Log detailed request summary for debugging
	log.Printf("📤 Gemini API request: model=%s, messages=%d, tools=%d, size=%d bytes",
		llmConfig.Model, len(geminiContents), len(geminiTools), len(reqBody))

	// Log conversation structure
	for i, content := range geminiContents {
		partTypes := []string{}
		for _, part := range content.Parts {
			if part.Text != "" {
				partTypes = append(partTypes, "text")
			}
			if part.FunctionCall != nil {
				partTypes = append(partTypes, "functionCall:"+part.FunctionCall.Name)
			}
			if part.FunctionResponse != nil {
				partTypes = append(partTypes, "functionResponse:"+part.FunctionResponse.Name)
			}
			if part.InlineData != nil {
				partTypes = append(partTypes, "inlineData:"+part.InlineData.MimeType)
			}
		}
		log.Printf("   📝 [%d] role=%s, parts=%v", i, content.Role, partTypes)
	}

	// Construct endpoint URL (streaming version with SSE)
	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse",
		llmConfig.Model)

	// Create HTTP request
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.apiKey)

	// Make request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error calling Gemini API: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("❌ Gemini API error response: %s", string(errorBody))
		return nil, fmt.Errorf("Gemini API error: %d - %s", resp.StatusCode, string(errorBody))
	}

	log.Printf("✅ Gemini API connection established, streaming response...")
	return resp.Body, nil
}

// StreamChat provides backward compatibility for simple streaming (deprecated, use GetRawStream)
func (p *GeminiProvider) StreamChat(w http.ResponseWriter, message string) error {
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

	// Note: actual streaming is handled by GeminiStreamProcessor
	// This is just a simple pass-through for compatibility
	buf := make([]byte, 4096)
	for {
		n, err := rawStream.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error reading stream: %v", err)
			return err
		}
	}

	return nil
}

// cleanSchemaForGemini removes unsupported JSON Schema fields for Gemini
// Gemini doesn't support: examples, $schema, additionalProperties in some contexts
func cleanSchemaForGemini(schema map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{})

	for key, value := range schema {
		// Skip unsupported fields
		if key == "examples" || key == "$schema" {
			continue
		}

		// Recursively clean nested objects
		switch v := value.(type) {
		case map[string]interface{}:
			cleaned[key] = cleanSchemaForGemini(v)
		case []interface{}:
			// Clean array elements if they're objects
			cleanedArray := make([]interface{}, len(v))
			for i, item := range v {
				if itemMap, ok := item.(map[string]interface{}); ok {
					cleanedArray[i] = cleanSchemaForGemini(itemMap)
				} else {
					cleanedArray[i] = item
				}
			}
			cleaned[key] = cleanedArray
		default:
			cleaned[key] = value
		}
	}

	return cleaned
}
