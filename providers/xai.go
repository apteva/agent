package providers

import (
	"os"
)

// XAIProvider wraps the OpenAI-compatible base provider with xAI-specific configuration
// xAI provides Grok models from X/Twitter
// API Documentation: https://docs.x.ai/api
type XAIProvider struct {
	*OpenAICompatibleProvider
}

// NewXAIProvider creates a new xAI provider instance
// Requires XAI_API_KEY environment variable to be set
//
// Popular models:
// - grok-4-1-fast-reasoning (recommended, fast reasoning model)
// - grok-4-1-fast-non-reasoning (fast non-reasoning model)
// - grok-2-latest (latest Grok 2)
// - grok-2-1212 (Grok 2 December 2024)
// - grok-beta (original beta)
func NewXAIProvider() *XAIProvider {
	apiKey := os.Getenv("XAI_API_KEY")
	return &XAIProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.x.ai/v1",
			"Authorization",
			"Bearer ",
			"XAI_API_KEY",
		),
	}
}
