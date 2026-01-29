package providers

import (
	"os"
)

// TogetherProvider wraps the OpenAI-compatible base provider with Together AI-specific configuration
// Together AI provides fast inference for open-source models including Kimi K2 Thinking
// API Documentation: https://docs.together.ai/reference
type TogetherProvider struct {
	*OpenAICompatibleProvider
}

// NewTogetherProvider creates a new Together AI provider instance
// Requires TOGETHER_API_KEY environment variable to be set
//
// Popular models:
// - moonshotai/Kimi-K2-Thinking (Kimi K2 Thinking, reasoning model)
// - moonshotai/Kimi-K2.5 (Kimi K2.5, reasoning model)
// - moonshotai/Kimi-K2-Instruct (Kimi K2 Instruct)
// - meta-llama/Llama-3.3-70B-Instruct-Turbo (Llama 3.3 70B, fast)
// - meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo (Llama 3.1 405B, largest)
// - meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo (Llama 3.1 70B)
// - meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo (Llama 3.1 8B, ultra-fast)
// - Qwen/Qwen2.5-72B-Instruct-Turbo (Qwen 2.5 72B)
// - deepseek-ai/DeepSeek-R1 (DeepSeek R1, reasoning)
// - deepseek-ai/DeepSeek-V3 (DeepSeek V3)
func NewTogetherProvider() *TogetherProvider {
	apiKey := os.Getenv("TOGETHER_API_KEY")
	return &TogetherProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.together.xyz/v1",
			"Authorization",
			"Bearer ",
			"TOGETHER_API_KEY",
		),
	}
}
