package providers

import (
	"os"
)

// GroqProvider wraps the OpenAI-compatible base provider with Groq-specific configuration
// Groq provides ultra-fast inference with open-source models like Llama, Gemma, and Mixtral
// API Documentation: https://console.groq.com/docs/api-reference
type GroqProvider struct {
	*OpenAICompatibleProvider
}

// NewGroqProvider creates a new Groq provider instance
// Requires GROQ_API_KEY environment variable to be set
//
// Popular models:
// - llama-3.3-70b-versatile (recommended, 128k context)
// - llama-3.1-8b-instant (ultra-fast, 131k context)
// - mixtral-8x7b-32768 (32k context)
// - gemma2-9b-it (Google, efficient)
func NewGroqProvider() *GroqProvider {
	apiKey := os.Getenv("GROQ_API_KEY")
	return &GroqProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.groq.com/openai/v1",
			"Authorization",
			"Bearer ",
			"GROQ_API_KEY",
		),
	}
}
