package config

import "os"

// SmallModels maps providers to their small/fast models for internal tasks
// (memory decisions, summarization, etc.)
// Updated: February 2026
var SmallModels = map[string]string{
	"anthropic":  "claude-haiku-4-5",                      // Fast, cheap, 1/3 cost of Sonnet 4
	"openai":     "gpt-4.1-mini",                          // Replaces gpt-4o-mini, 1M context
	"gemini":     "gemini-2.5-flash-lite",                 // Fast, low-cost, high-performance
	"venice":     "qwen3-4b",                              // Replaced llama-3.2-3b
	"openrouter": "meta-llama/llama-3.3-70b-instruct:free", // Free tier available
}

// EmbeddingModels maps providers to their default embedding models
// Updated: February 2026
var EmbeddingModels = map[string]string{
	"openai":      "text-embedding-3-small",        // 1536 dimensions, $0.02/1M tokens
	"gemini":      "text-embedding-004",            // 768 dimensions, free tier
	"jina":        "jina-embeddings-v3",            // 1024 dimensions, multilingual
	"voyage":      "voyage-3.5-lite",               // 1024 dimensions, improved over 3-lite
	"cohere":      "embed-english-v3.0",            // 1024 dimensions
	"huggingface": "BAAI/bge-small-en-v1.5",        // 384 dimensions, free tier
	"ollama":      "nomic-embed-text",              // Local, 768 dimensions
}

// EmbeddingEndpoints maps providers to their embedding API endpoints
var EmbeddingEndpoints = map[string]string{
	"openai":      "https://api.openai.com/v1/embeddings",
	"gemini":      "https://generativelanguage.googleapis.com/v1beta/models/%s:embedContent",
	"jina":        "https://api.jina.ai/v1/embeddings",
	"voyage":      "https://api.voyageai.com/v1/embeddings",
	"cohere":      "https://api.cohere.com/v1/embed",
	"huggingface": "https://api-inference.huggingface.co/models/%s", // Model name in URL
}

// EmbeddingDimensions maps providers to their embedding vector dimensions
var EmbeddingDimensions = map[string]int{
	"openai":      1536, // text-embedding-3-small
	"gemini":      768,  // text-embedding-004
	"jina":        1024, // jina-embeddings-v3
	"voyage":      1024, // voyage-3-lite
	"cohere":      1024, // embed-english-v3.0
	"huggingface": 384,  // BAAI/bge-small-en-v1.5 (varies by model)
	"ollama":      768,  // nomic-embed-text (varies by model)
}

// ProviderAPIKeyEnvVars maps providers to their API key environment variable names
var ProviderAPIKeyEnvVars = map[string]string{
	"anthropic":   "ANTHROPIC_API_KEY",
	"openai":      "OPENAI_API_KEY",
	"gemini":      "GEMINI_API_KEY",
	"venice":      "VENICE_API_KEY",
	"jina":        "JINA_API_KEY",
	"voyage":      "VOYAGE_API_KEY",
	"cohere":      "COHERE_API_KEY",
	"huggingface": "HUGGINGFACE_API_KEY",
	"openrouter":  "OPENROUTER_API_KEY",
}

// CompletionEndpoints maps providers to their chat completion API endpoints
var CompletionEndpoints = map[string]string{
	"anthropic": "https://api.anthropic.com/v1/messages",
	"openai":    "https://api.openai.com/v1/chat/completions",
	"gemini":    "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent",
	"venice":    "https://api.venice.ai/api/v1/chat/completions",
	"openrouter": "https://openrouter.ai/api/v1/chat/completions",
}

// GetSmallModel returns the small/fast model for a given provider
// Used for internal tasks like memory decisions, summarization
func GetSmallModel(provider string) string {
	if model, ok := SmallModels[provider]; ok {
		return model
	}
	// Fallback to OpenAI if available, otherwise return empty
	if HasAPIKey("openai") {
		return SmallModels["openai"]
	}
	return ""
}

// GetEmbeddingModelForProvider returns the default embedding model for a provider
func GetEmbeddingModelForProvider(provider string) string {
	if model, ok := EmbeddingModels[provider]; ok {
		return model
	}
	return EmbeddingModels["openai"]
}

// GetEmbeddingEndpoint returns the embedding API endpoint for a provider
func GetEmbeddingEndpoint(provider string) string {
	if endpoint, ok := EmbeddingEndpoints[provider]; ok {
		return endpoint
	}
	return EmbeddingEndpoints["openai"]
}

// GetCompletionEndpoint returns the chat completion API endpoint for a provider
func GetCompletionEndpoint(provider string) string {
	if endpoint, ok := CompletionEndpoints[provider]; ok {
		return endpoint
	}
	return CompletionEndpoints["openai"]
}

// HasAPIKey checks if an API key is available for a provider
func HasAPIKey(provider string) bool {
	envVar, ok := ProviderAPIKeyEnvVars[provider]
	if !ok {
		return false
	}
	return os.Getenv(envVar) != ""
}

// GetAPIKey returns the API key for a provider
func GetAPIKey(provider string) string {
	envVar, ok := ProviderAPIKeyEnvVars[provider]
	if !ok {
		return ""
	}
	return os.Getenv(envVar)
}

// GetAvailableEmbeddingProvider returns the first available embedding provider
// Priority: explicit config > same as main provider > free options > ollama
func GetAvailableEmbeddingProvider(configuredProvider, mainProvider string) string {
	// 1. Use explicitly configured provider if it has an API key (or is ollama)
	if configuredProvider != "" && configuredProvider != "auto" {
		if configuredProvider == "ollama" || HasAPIKey(configuredProvider) {
			return configuredProvider
		}
	}

	// 2. Try to use same provider as main LLM (for embedding-capable providers)
	embeddingCapableProviders := []string{"openai", "gemini", "voyage", "cohere"}
	for _, p := range embeddingCapableProviders {
		if p == mainProvider && HasAPIKey(p) {
			return p
		}
	}

	// 3. Try providers in preference order (if they have API keys)
	preferenceOrder := []string{"openai", "gemini", "jina", "voyage", "cohere"}
	for _, p := range preferenceOrder {
		if HasAPIKey(p) {
			return p
		}
	}

	// 4. Fall back to Ollama (local, no API key needed)
	// User must have Ollama running locally with nomic-embed-text model
	return "ollama"
}

// GetAvailableDecisionProvider returns the provider to use for memory decisions
// Uses the same provider as the main LLM when possible
func GetAvailableDecisionProvider(mainProvider string) string {
	// Providers that support chat completions for decision making
	decisionCapable := []string{"anthropic", "openai", "gemini", "venice", "openrouter"}

	// First try the main provider
	for _, p := range decisionCapable {
		if p == mainProvider && HasAPIKey(p) {
			return p
		}
	}

	// Fall back to any available provider
	for _, p := range decisionCapable {
		if HasAPIKey(p) {
			return p
		}
	}

	return "" // No provider available
}

// EmbeddingProviderSupported checks if a provider supports embeddings
func EmbeddingProviderSupported(provider string) bool {
	_, ok := EmbeddingModels[provider]
	return ok
}

// DecisionProviderSupported checks if a provider supports chat completions
func DecisionProviderSupported(provider string) bool {
	_, ok := CompletionEndpoints[provider]
	return ok
}

// GetAllEmbeddingProviders returns all supported embedding providers with their default models
func GetAllEmbeddingProviders() map[string]EmbeddingProviderInfo {
	providers := make(map[string]EmbeddingProviderInfo)
	for provider, model := range EmbeddingModels {
		providers[provider] = EmbeddingProviderInfo{
			Provider:     provider,
			DefaultModel: model,
			Dimensions:   EmbeddingDimensions[provider],
			HasAPIKey:    HasAPIKey(provider),
			EnvVar:       ProviderAPIKeyEnvVars[provider],
		}
	}
	return providers
}

// EmbeddingProviderInfo contains info about an embedding provider
type EmbeddingProviderInfo struct {
	Provider     string `json:"provider"`
	DefaultModel string `json:"default_model"`
	Dimensions   int    `json:"dimensions"`
	HasAPIKey    bool   `json:"has_api_key"`
	EnvVar       string `json:"env_var"`
}
