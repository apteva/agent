package providers

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/apteva/agent/stream"
)

// MessageTranslator converts messages from universal format to provider-specific format
type MessageTranslator interface {
	TranslateMessage(stream.Message) (interface{}, error)
}

// AnthropicTranslator - Anthropic uses our universal format natively
type AnthropicTranslator struct{}

func (t *AnthropicTranslator) TranslateMessage(msg stream.Message) (interface{}, error) {
	// Anthropic uses the same format, so we can pass through
	return msg.Content, nil
}

// OpenAITranslator - Convert to OpenAI's format
type OpenAITranslator struct{}

func (t *OpenAITranslator) TranslateMessage(msg stream.Message) (interface{}, error) {
	// Handle different content types
	switch content := msg.Content.(type) {
	case string:
		// Simple text message
		return content, nil

	case []stream.ContentBlock:
		// Convert content blocks to OpenAI format
		var openAIContent []map[string]interface{}

		for _, block := range content {
			switch block.Type {
			case "text":
				openAIContent = append(openAIContent, map[string]interface{}{
					"type": "text",
					"text": block.Text,
				})

			case "image":
				if block.Source == nil {
					continue
				}

				// Convert image to OpenAI format
				var imageURL string
				switch block.Source.Type {
				case "base64":
					// OpenAI expects data URL format for base64
					imageURL = fmt.Sprintf("data:%s;base64,%s", block.Source.MediaType, block.Source.Data)
				case "url":
					imageURL = block.Source.URL
				default:
					// Skip unsupported image types for now
					continue
				}

				openAIContent = append(openAIContent, map[string]interface{}{
					"type": "image_url",
					"image_url": map[string]interface{}{
						"url": imageURL,
					},
				})

			case "document":
				// OpenAI doesn't support PDFs directly
				// For now, we'll add a text message explaining this
				if block.Source != nil && block.Source.MediaType == "application/pdf" {
					openAIContent = append(openAIContent, map[string]interface{}{
						"type": "text",
						"text": "[PDF document provided - OpenAI does not support native PDF processing. Please use Anthropic provider for PDF support.]",
					})
				}

			case "tool_use":
				// OpenAI uses function call format, not content blocks for tool use
				// This is handled separately in the request building
				continue

			case "tool_result":
				// Convert tool_result to text content for OpenAI
				// OpenAI doesn't have a native tool_result format in messages
				var resultText string
				switch c := block.Content.(type) {
				case string:
					resultText = c
				default:
					// Convert to JSON string
					contentJSON, err := json.Marshal(block.Content)
					if err == nil {
						resultText = string(contentJSON)
					} else {
						resultText = fmt.Sprintf("%v", block.Content)
					}
				}

				openAIContent = append(openAIContent, map[string]interface{}{
					"type": "text",
					"text": resultText,
				})

			default:
				continue
			}
		}

		return openAIContent, nil

	case []interface{}:
		// Already an array, try to convert each element
		var openAIContent []map[string]interface{}

		for _, item := range content {
			// Try to parse as ContentBlock
			itemJSON, err := json.Marshal(item)
			if err != nil {
				continue
			}

			var block stream.ContentBlock
			if err := json.Unmarshal(itemJSON, &block); err != nil {
				continue
			}

			// Process the block
			switch block.Type {
			case "text":
				openAIContent = append(openAIContent, map[string]interface{}{
					"type": "text",
					"text": block.Text,
				})

			case "image":
				if block.Source == nil {
					continue
				}

				var imageURL string
				switch block.Source.Type {
				case "base64":
					imageURL = fmt.Sprintf("data:%s;base64,%s", block.Source.MediaType, block.Source.Data)
				case "url":
					imageURL = block.Source.URL
				default:
					continue
				}

				openAIContent = append(openAIContent, map[string]interface{}{
					"type": "image_url",
					"image_url": map[string]interface{}{
						"url": imageURL,
					},
				})

			case "document":
				// OpenAI doesn't support PDFs directly
				if block.Source != nil && block.Source.MediaType == "application/pdf" {
					openAIContent = append(openAIContent, map[string]interface{}{
						"type": "text",
						"text": "[PDF document provided - OpenAI does not support native PDF processing. Please use Anthropic provider for PDF support.]",
					})
				}

			case "tool_use":
				// OpenAI uses function call format, not content blocks for tool use
				continue

			case "tool_result":
				// Convert tool_result to text content for OpenAI
				var resultText string
				switch c := block.Content.(type) {
				case string:
					resultText = c
				default:
					// Convert to JSON string
					contentJSON, err := json.Marshal(block.Content)
					if err == nil {
						resultText = string(contentJSON)
					} else {
						resultText = fmt.Sprintf("%v", block.Content)
					}
				}

				openAIContent = append(openAIContent, map[string]interface{}{
					"type": "text",
					"text": resultText,
				})
			}
		}

		return openAIContent, nil

	default:
		// For any other type, try to convert to string
		return fmt.Sprintf("%v", content), nil
	}
}

// ==================== Gemini Types ====================

// GeminiContent represents a message in Gemini's format
type GeminiContent struct {
	Role  string       `json:"role"` // "user" or "model" - REQUIRED, no omitempty
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart represents a single part of content (text, image, function call, etc.)
type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	InlineData       *GeminiInlineData       `json:"inline_data,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"` // At Part level for Gemini 3
}

// GeminiInlineData for base64-encoded media
type GeminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

// GeminiFunctionCall for tool use
type GeminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// GeminiFunctionResponse for tool results
type GeminiFunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// ==================== Gemini Translator ====================

// GeminiTranslator - Convert to Gemini's format
type GeminiTranslator struct {
	toolNamesByID map[string]string // Track tool names by ID for function responses
}

func NewGeminiTranslator() *GeminiTranslator {
	return &GeminiTranslator{
		toolNamesByID: make(map[string]string),
	}
}

func (t *GeminiTranslator) TranslateMessage(msg stream.Message) (interface{}, error) {
	// Map roles: "assistant" -> "model", "user" -> "user"
	role := msg.Role
	if role == "assistant" {
		role = "model"
	}

	var parts []GeminiPart

	switch content := msg.Content.(type) {
	case string:
		// Simple text message
		parts = append(parts, GeminiPart{Text: content})

	case []stream.ContentBlock:
		// Convert content blocks to Gemini parts
		for _, block := range content {
			switch block.Type {
			case "text":
				parts = append(parts, GeminiPart{Text: block.Text})

			case "image":
				if block.Source == nil {
					continue
				}

				// Convert image to Gemini inline_data format
				if block.Source.Type == "base64" {
					parts = append(parts, GeminiPart{
						InlineData: &GeminiInlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				}
				// Note: Gemini also supports URL-based images via FileAPI, but that requires pre-upload

			case "audio":
				// Gemini supports audio via inline_data (wav, mp3, aiff, aac, ogg, flac)
				// Audio is processed at 32 tokens/second, max 9.5 hours
				if block.Source == nil {
					continue
				}

				if block.Source.Type == "base64" {
					parts = append(parts, GeminiPart{
						InlineData: &GeminiInlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				}

			case "document":
				// Gemini supports documents via inline_data (similar to images)
				if block.Source != nil && block.Source.Type == "base64" {
					parts = append(parts, GeminiPart{
						InlineData: &GeminiInlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				}

			case "video":
				// Gemini supports video via inline_data (mp4, webm, mov, etc.)
				// Videos should be under 20MB for inline data
				// Supported formats: video/mp4, video/webm, video/quicktime, etc.
				if block.Source == nil {
					continue
				}

				if block.Source.Type == "base64" {
					parts = append(parts, GeminiPart{
						InlineData: &GeminiInlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				}

			case "tool_use":
				// Store tool name for later use in function responses
				t.toolNamesByID[block.ID] = block.Name

				// DEBUG: Log thought signature from ContentBlock
				log.Printf("🔍 Gemini Translator tool_use: name=%s, id=%s, hasThoughtSignature=%v, len=%d",
					block.Name, block.ID, block.ThoughtSignature != "", len(block.ThoughtSignature))

				// Convert to Gemini functionCall format (thought_signature at Part level for Gemini 3)
				parts = append(parts, GeminiPart{
					FunctionCall: &GeminiFunctionCall{
						Name: block.Name,
						Args: block.Input,
					},
					ThoughtSignature: block.ThoughtSignature, // At Part level
				})

			case "tool_result":
				// Convert to Gemini functionResponse format
				var response map[string]interface{}

				// Parse content as JSON for response
				switch c := block.Content.(type) {
				case string:
					// Try to parse as JSON
					if err := json.Unmarshal([]byte(c), &response); err != nil {
						// If not JSON, wrap in a result field
						response = map[string]interface{}{
							"result": c,
						}
					}
				case map[string]interface{}:
					response = c
				default:
					// Convert to JSON string and wrap
					contentJSON, _ := json.Marshal(block.Content)
					response = map[string]interface{}{
						"result": string(contentJSON),
					}
				}

				// Look up tool name from tool_use_id
				toolName := t.toolNamesByID[block.ToolUseID]
				if toolName == "" {
					// Fallback: try to get from block.Name if available
					toolName = block.Name
				}

				parts = append(parts, GeminiPart{
					FunctionResponse: &GeminiFunctionResponse{
						Name:     toolName,
						Response: response,
					},
				})
			}
		}

	case []interface{}:
		// Handle array of blocks (from database)
		for _, item := range content {
			// Try to parse as ContentBlock
			itemJSON, err := json.Marshal(item)
			if err != nil {
				continue
			}

			var block stream.ContentBlock
			if err := json.Unmarshal(itemJSON, &block); err != nil {
				continue
			}

			// Process the block
			switch block.Type {
			case "text":
				parts = append(parts, GeminiPart{Text: block.Text})

			case "image":
				if block.Source != nil && block.Source.Type == "base64" {
					parts = append(parts, GeminiPart{
						InlineData: &GeminiInlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				}

			case "audio":
				// Gemini supports audio via inline_data (wav, mp3, aiff, aac, ogg, flac)
				if block.Source != nil && block.Source.Type == "base64" {
					parts = append(parts, GeminiPart{
						InlineData: &GeminiInlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				}

			case "document":
				// Gemini supports documents via inline_data
				if block.Source != nil && block.Source.Type == "base64" {
					parts = append(parts, GeminiPart{
						InlineData: &GeminiInlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				}

			case "video":
				// Gemini supports video via inline_data
				if block.Source != nil && block.Source.Type == "base64" {
					parts = append(parts, GeminiPart{
						InlineData: &GeminiInlineData{
							MimeType: block.Source.MediaType,
							Data:     block.Source.Data,
						},
					})
				}

			case "tool_use":
				t.toolNamesByID[block.ID] = block.Name

				// DEBUG: Log thought signature from ContentBlock ([]interface{} path)
				log.Printf("🔍 Gemini Translator tool_use (interface): name=%s, id=%s, hasThoughtSignature=%v, len=%d",
					block.Name, block.ID, block.ThoughtSignature != "", len(block.ThoughtSignature))

				parts = append(parts, GeminiPart{
					FunctionCall: &GeminiFunctionCall{
						Name: block.Name,
						Args: block.Input,
					},
					ThoughtSignature: block.ThoughtSignature, // At Part level
				})

			case "tool_result":
				var response map[string]interface{}
				switch c := block.Content.(type) {
				case string:
					if err := json.Unmarshal([]byte(c), &response); err != nil {
						response = map[string]interface{}{"result": c}
					}
				case map[string]interface{}:
					response = c
				default:
					contentJSON, _ := json.Marshal(block.Content)
					response = map[string]interface{}{"result": string(contentJSON)}
				}

				toolName := t.toolNamesByID[block.ToolUseID]
				if toolName == "" {
					toolName = block.Name
				}

				parts = append(parts, GeminiPart{
					FunctionResponse: &GeminiFunctionResponse{
						Name:     toolName,
						Response: response,
					},
				})
			}
		}

	default:
		// For any other type, convert to string
		parts = append(parts, GeminiPart{Text: fmt.Sprintf("%v", content)})
	}

	return GeminiContent{
		Role:  role,
		Parts: parts,
	}, nil
}