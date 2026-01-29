package providers

import (
	"os"
)

// FireworksProvider wraps the OpenAI-compatible base provider with Fireworks-specific configuration
// Fireworks AI provides fast inference with a wide range of open-source and custom models
// API Documentation: https://docs.fireworks.ai/api-reference
type FireworksProvider struct {
	*OpenAICompatibleProvider
}

// NewFireworksProvider creates a new Fireworks provider instance
// Requires FIREWORKS_API_KEY environment variable to be set
//
// Popular models:
// - accounts/fireworks/models/kimi-k2-thinking (Kimi K2 Thinking, reasoning model)
// - accounts/fireworks/models/llama-v3p3-70b-instruct (Llama 3.3 70B, recommended)
// - accounts/fireworks/models/llama-v3p1-405b-instruct (Llama 3.1 405B, largest)
// - accounts/fireworks/models/llama-v3p1-70b-instruct (Llama 3.1 70B)
// - accounts/fireworks/models/llama-v3p1-8b-instruct (Llama 3.1 8B, fast)
// - accounts/fireworks/models/mixtral-8x22b-instruct (Mixtral 8x22B)
// - accounts/fireworks/models/qwen2p5-72b-instruct (Qwen 2.5 72B)
// - accounts/fireworks/models/deepseek-v3 (DeepSeek V3)
// - accounts/fireworks/models/glm-4p7 (GLM-4 Plus 7B)
func NewFireworksProvider() *FireworksProvider {
	apiKey := os.Getenv("FIREWORKS_API_KEY")
	return &FireworksProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.fireworks.ai/inference/v1",
			"Authorization",
			"Bearer ",
			"FIREWORKS_API_KEY",
		),
	}
}
