package providers

import (
	"os"
)

// NovitaProvider wraps the OpenAI-compatible base provider with Novita AI-specific configuration
// Novita AI provides a wide range of open-source models via API
// API Documentation: https://novita.ai/docs/model-api/reference/llm/llm.html
type NovitaProvider struct {
	*OpenAICompatibleProvider
}

// NewNovitaProvider creates a new Novita AI provider instance
// Requires NOVITA_API_KEY environment variable to be set
//
// Popular models:
// - deepseek/deepseek-v3.2 (DeepSeek V3.2)
// - openai/gpt-oss-120b (GPT OSS 120B)
// - openai/gpt-oss-20b (GPT OSS 20B, fast)
// - qwen/qwen3-coder-480b-a35b-instruct (Qwen 3 Coder 480B)
// - zai-org/glm-4.7 (GLM 4.7)
func NewNovitaProvider() *NovitaProvider {
	apiKey := os.Getenv("NOVITA_API_KEY")
	return &NovitaProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.novita.ai/v3/openai",
			"Authorization",
			"Bearer ",
			"NOVITA_API_KEY",
		),
	}
}
