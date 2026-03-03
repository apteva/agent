package providers

import (
	"os"
)

// CerebrasProvider wraps the OpenAI-compatible base provider with Cerebras-specific configuration
// Cerebras provides ultra-fast inference with their custom wafer-scale hardware
// API Documentation: https://inference-docs.cerebras.ai
type CerebrasProvider struct {
	*OpenAICompatibleProvider
}

// NewCerebrasProvider creates a new Cerebras provider instance
// Requires CEREBRAS_API_KEY environment variable to be set
//
// Popular models:
// - gpt-oss-120b (OpenAI open-weight MoE, 128k context, 3000 tok/s)
// - llama-4-scout-17b-16e-instruct (latest, fast)
// - llama3.3-70b (recommended, high quality)
// - llama3.1-8b (ultra-fast)
func NewCerebrasProvider() *CerebrasProvider {
	apiKey := os.Getenv("CEREBRAS_API_KEY")
	return &CerebrasProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.cerebras.ai/v1",
			"Authorization",
			"Bearer ",
			"CEREBRAS_API_KEY",
		),
	}
}
