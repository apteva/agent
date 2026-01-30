package stream

import (
	"encoding/json"
	"log"

	"github.com/apteva/agent/tools"
)

// TokenAttribution tracks estimated token counts for each input component
type TokenAttribution struct {
	SystemPrompt        int `json:"system_prompt"`
	ToolDefinitions     int `json:"tool_definitions"`
	ConversationHistory int `json:"conversation_history"`
	MemoryContext       int `json:"memory_context"`
	UserMessage         int `json:"user_message"`
	EstimatedTotal      int `json:"estimated_total"`
	ActualTotal         int `json:"actual_total,omitempty"` // Filled in after LLM response
}

// EstimateTokens estimates the number of tokens for a given text
// Uses a simple heuristic: ~4 characters per token (average for English text)
// This is a rough approximation that works across different LLM providers
func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	// Add 3 to round up: (len + 3) / 4
	return (len(text) + 3) / 4
}

// EstimateTokensFromInterface estimates tokens from various content types
func EstimateTokensFromInterface(content interface{}) int {
	switch v := content.(type) {
	case string:
		return EstimateTokens(v)
	case []ContentBlock:
		total := 0
		for _, block := range v {
			total += estimateContentBlockTokens(block)
		}
		return total
	case []interface{}:
		// Handle JSON-unmarshalled content blocks
		total := 0
		for _, item := range v {
			if blockMap, ok := item.(map[string]interface{}); ok {
				total += estimateMapBlockTokens(blockMap)
			}
		}
		return total
	default:
		// Fallback: serialize to JSON and count
		data, err := json.Marshal(v)
		if err != nil {
			return 0
		}
		return EstimateTokens(string(data))
	}
}

// estimateContentBlockTokens estimates tokens for a ContentBlock
func estimateContentBlockTokens(block ContentBlock) int {
	total := 0

	// Text content
	total += EstimateTokens(block.Text)

	// Tool use content (name + input)
	if block.Type == "tool_use" {
		total += EstimateTokens(block.Name)
		if block.Input != nil {
			inputJSON, _ := json.Marshal(block.Input)
			total += EstimateTokens(string(inputJSON))
		}
	}

	// Tool result content
	if block.Type == "tool_result" {
		if contentStr, ok := block.Content.(string); ok {
			total += EstimateTokens(contentStr)
		} else if block.Content != nil {
			contentJSON, _ := json.Marshal(block.Content)
			total += EstimateTokens(string(contentJSON))
		}
	}

	// Image content - estimate based on size
	// Images are typically tokenized by the model (varies by provider)
	// Using a rough estimate: base64 images are about 1 token per 3 bytes
	if block.Type == "image" && block.Source != nil && block.Source.Data != "" {
		total += len(block.Source.Data) / 3
	}

	return total
}

// estimateMapBlockTokens estimates tokens from a map-based content block
func estimateMapBlockTokens(block map[string]interface{}) int {
	total := 0

	if text, ok := block["text"].(string); ok {
		total += EstimateTokens(text)
	}

	if blockType, ok := block["type"].(string); ok {
		if blockType == "tool_use" {
			if name, ok := block["name"].(string); ok {
				total += EstimateTokens(name)
			}
			if input, ok := block["input"]; ok {
				inputJSON, _ := json.Marshal(input)
				total += EstimateTokens(string(inputJSON))
			}
		}

		if blockType == "tool_result" {
			if content, ok := block["content"]; ok {
				if contentStr, ok := content.(string); ok {
					total += EstimateTokens(contentStr)
				} else {
					contentJSON, _ := json.Marshal(content)
					total += EstimateTokens(string(contentJSON))
				}
			}
		}

		// Image content
		if blockType == "image" {
			if source, ok := block["source"].(map[string]interface{}); ok {
				if data, ok := source["data"].(string); ok {
					total += len(data) / 3
				}
			}
		}
	}

	return total
}

// EstimateToolDefinitionsTokens estimates tokens for tool definitions
func EstimateToolDefinitionsTokens(customTools []tools.ToolDefinition, builtinTools []interface{}) int {
	total := 0

	// Custom tools
	for _, tool := range customTools {
		toolJSON, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		total += EstimateTokens(string(toolJSON))
	}

	// Builtin tools
	for _, tool := range builtinTools {
		toolJSON, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		total += EstimateTokens(string(toolJSON))
	}

	return total
}

// CalculateTokenAttribution calculates token attribution for all input components
// Parameters:
//   - messages: the full message array (including system message)
//   - customTools: custom tool definitions
//   - builtinTools: builtin tool definitions
//   - memoryContextLen: character length of memory context (if tracked separately)
func CalculateTokenAttribution(messages []Message, customTools []tools.ToolDefinition, builtinTools []interface{}, memoryContextLen int) TokenAttribution {
	attr := TokenAttribution{}

	// Extract components from messages
	var systemPromptContent interface{}
	var conversationHistory []Message
	var lastUserMessage Message
	hasUserMessage := false

	for i, msg := range messages {
		if msg.Role == "system" {
			systemPromptContent = msg.Content
		} else if msg.Role == "user" {
			// Check if this is the last user message
			isLast := true
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Role == "user" {
					isLast = false
					break
				}
			}
			if isLast {
				lastUserMessage = msg
				hasUserMessage = true
			} else {
				conversationHistory = append(conversationHistory, msg)
			}
		} else {
			// Assistant messages go to conversation history
			conversationHistory = append(conversationHistory, msg)
		}
	}

	// Calculate system prompt tokens
	if systemPromptContent != nil {
		attr.SystemPrompt = EstimateTokensFromInterface(systemPromptContent)
	}

	// Calculate tool definitions tokens
	attr.ToolDefinitions = EstimateToolDefinitionsTokens(customTools, builtinTools)

	// Calculate conversation history tokens
	for _, msg := range conversationHistory {
		// Add role overhead (~1-2 tokens)
		attr.ConversationHistory += 2
		attr.ConversationHistory += EstimateTokensFromInterface(msg.Content)
	}

	// Calculate user message tokens (minus memory context if tracked)
	if hasUserMessage {
		userMsgTokens := EstimateTokensFromInterface(lastUserMessage.Content)

		// If memory context length is provided, subtract its estimated tokens
		if memoryContextLen > 0 {
			attr.MemoryContext = EstimateTokens(string(make([]byte, memoryContextLen)))
			attr.UserMessage = userMsgTokens - attr.MemoryContext
			if attr.UserMessage < 0 {
				attr.UserMessage = 0
			}
		} else {
			attr.UserMessage = userMsgTokens
		}
	}

	// Calculate total
	attr.EstimatedTotal = attr.SystemPrompt + attr.ToolDefinitions +
		attr.ConversationHistory + attr.MemoryContext + attr.UserMessage

	log.Printf("📊 Token attribution: system=%d, tools=%d, history=%d, memory=%d, user=%d, total=%d",
		attr.SystemPrompt, attr.ToolDefinitions, attr.ConversationHistory,
		attr.MemoryContext, attr.UserMessage, attr.EstimatedTotal)

	return attr
}
