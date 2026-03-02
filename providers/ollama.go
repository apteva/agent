package providers

import (
	"os"
)

// OllamaProvider wraps the OpenAI-compatible base provider for local Ollama instances.
// Ollama exposes an OpenAI-compatible API at /v1 and does not require an API key.
type OllamaProvider struct {
	*OpenAICompatibleProvider
}

func NewOllamaProvider() *OllamaProvider {
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			"", // No API key needed
			baseURL+"/v1",
			"Authorization",
			"Bearer ",
			"OLLAMA_BASE_URL",
		),
	}
}
