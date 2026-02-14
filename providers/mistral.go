package providers

import (
	"os"
)

// MistralProvider wraps the OpenAI-compatible base provider with Mistral-specific configuration
// Mistral AI provides high-performance open and commercial models
// API Documentation: https://docs.mistral.ai/api/
type MistralProvider struct {
	*OpenAICompatibleProvider
}

// NewMistralProvider creates a new Mistral provider instance
// Requires MISTRAL_API_KEY environment variable to be set
//
// Popular models (December 2025+):
// - mistral-large-3-25-12 (flagship, 675B MoE open-weight)
// - mistral-medium-3-1-25-08 (multimodal)
// - mistral-small-3-2-25-06 (fast, cost-effective)
// - magistral-medium-2509 (reasoning, 40K context)
// - devstral-2-25-12 (code agents)
// - codestral-2508 (code completion)
// - ministral-3-8b-25-12 (open-weight, fast)
func NewMistralProvider() *MistralProvider {
	apiKey := os.Getenv("MISTRAL_API_KEY")
	return &MistralProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.mistral.ai/v1",
			"Authorization",
			"Bearer ",
			"MISTRAL_API_KEY",
		),
	}
}
