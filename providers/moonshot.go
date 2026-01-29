package providers

import (
	"os"
)

// MoonshotProvider wraps the OpenAI-compatible base provider with Moonshot-specific configuration
// Moonshot AI (Kimi) provides advanced AI models with OpenAI-compatible API
// API Documentation: https://platform.moonshot.cn/docs
type MoonshotProvider struct {
	*OpenAICompatibleProvider
}

// NewMoonshotProvider creates a new Moonshot provider instance
// Requires MOONSHOT_API_KEY environment variable to be set
//
// Popular models:
// - kimi-k2-turbo-preview (Latest turbo model, recommended)
// - kimi-k2.5 (Kimi K2.5)
// - kimi-k2-0711-preview (K2 preview)
// - moonshot-v1-128k (128K context)
// - moonshot-v1-32k (32K context)
// - moonshot-v1-8k (8K context, fast)
func NewMoonshotProvider() *MoonshotProvider {
	apiKey := os.Getenv("MOONSHOT_API_KEY")
	return &MoonshotProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.moonshot.ai/v1",
			"Authorization",
			"Bearer ",
			"MOONSHOT_API_KEY",
		),
	}
}
