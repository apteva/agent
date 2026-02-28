package providers

import (
	"os"
)

type OpenAIRequest struct {
	Model               string                `json:"model"`
	Messages            []OpenAIMessage       `json:"messages"`
	MaxTokens           int                   `json:"max_tokens,omitempty"`            // For older models and compatible APIs
	MaxCompletionTokens int                   `json:"max_completion_tokens,omitempty"` // For newer OpenAI models (gpt-4o, gpt-5.x, o1, etc.)
	Temperature         *float64              `json:"temperature,omitempty"`
	Stream              bool                  `json:"stream"`
	StreamOptions       *StreamOptions        `json:"stream_options,omitempty"`        // Required for token usage in streaming mode
	Tools               []interface{}         `json:"tools,omitempty"`
	ReasoningEffort     string                `json:"reasoning_effort,omitempty"`      // For reasoning models (o1, o3, gpt-5.x)
	ReasoningHistory    string                `json:"reasoning_history,omitempty"`     // For thinking models (Kimi K2): "interleaved" or "preserved"
	ReasoningSplit      *bool                 `json:"reasoning_split,omitempty"`       // For MiniMax M2.5: true for structured reasoning_details
}

// StreamOptions controls streaming behavior (OpenAI-compatible APIs)
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type OpenAIMessage struct {
	Role             string                   `json:"role"`
	Content          interface{}              `json:"content,omitempty"`              // Can be string or array of content parts
	ReasoningContent string                   `json:"reasoning_content,omitempty"`    // For thinking models (Kimi K2, etc.) - MUST preserve in context
	ReasoningDetails []interface{}            `json:"reasoning_details,omitempty"`    // For MiniMax M2.5 structured reasoning
	ToolCalls        []map[string]interface{} `json:"tool_calls,omitempty"`           // For assistant tool calls
	ToolCallID       string                   `json:"tool_call_id,omitempty"`         // For tool role messages
}

type OpenAIToolDef struct {
	Type     string              `json:"type"`
	Function OpenAIFunctionDef   `json:"function"`
}

// OpenAIFunctionDef represents a function definition in OpenAI format
// Note: OpenAI uses "parameters" instead of Anthropic's "input_schema"
type OpenAIFunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type OpenAIStreamResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []OpenAIChoice         `json:"choices"`
}

type OpenAIChoice struct {
	Index int                    `json:"index"`
	Delta OpenAIDelta            `json:"delta"`
	FinishReason *string          `json:"finish_reason"`
}

type OpenAIDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// OpenAIProvider wraps the OpenAI-compatible base provider with OpenAI-specific configuration
type OpenAIProvider struct {
	*OpenAICompatibleProvider
}

func NewOpenAIProvider() *OpenAIProvider {
	apiKey := os.Getenv("OPENAI_API_KEY")
	return &OpenAIProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.openai.com/v1",
			"Authorization",
			"Bearer ",
			"OPENAI_API_KEY",
		),
	}
}