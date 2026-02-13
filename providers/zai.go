package providers

import (
	"os"
)

// ZAIProvider wraps the OpenAI-compatible base provider with Z.ai-specific configuration
// Z.ai (formerly Zhipu AI) provides GLM models
// API Documentation: https://docs.z.ai/guides/overview/quick-start
type ZAIProvider struct {
	*OpenAICompatibleProvider
}

// NewZAIProvider creates a new Z.ai provider instance
// Requires ZAI_API_KEY environment variable to be set
//
// Popular models:
// - glm-5 (745B MoE, agentic, SOTA open-source coding)
// - glm-4.7 (flagship with reasoning, 200K context)
// - glm-4.6 (flagship, 200K context)
// - glm-4.5 (general purpose, 128K context)
// - glm-4.5v (vision, 128K context)
// - glm-4.5-flash (free tier, 128K context)
// - glm-4.5-air (lightweight, 128K context)
func NewZAIProvider() *ZAIProvider {
	apiKey := os.Getenv("ZAI_API_KEY")
	return &ZAIProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.z.ai/api/paas/v4",
			"Authorization",
			"Bearer ",
			"ZAI_API_KEY",
		),
	}
}
