package config

import (
	"encoding/json"
	"net/http"

	"github.com/apteva/agent/config"
)

// ProviderModel represents a model available for a provider
type ProviderModel struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Recommended bool   `json:"recommended,omitempty"`
}

// ProviderInfo represents a provider and its available models
type ProviderInfo struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Models []ProviderModel `json:"models"`
}

// GetProviders returns the list of supported providers and their models
func GetProviders() []ProviderInfo {
	return []ProviderInfo{
		{
			ID:   "anthropic",
			Name: "Anthropic (Claude)",
			Models: []ProviderModel{
				{Value: "claude-sonnet-4-20250514", Label: "Claude Sonnet 4 (Latest)", Recommended: true},
				{Value: "claude-opus-4-20250514", Label: "Claude Opus 4 (Most Capable)"},
				{Value: "claude-sonnet-4-5", Label: "Claude 4.5 Sonnet"},
				{Value: "claude-haiku-4-5", Label: "Claude 4.5 Haiku (Fast)"},
				{Value: "claude-3-7-sonnet-20250219", Label: "Claude 3.7 Sonnet"},
				{Value: "claude-3-5-sonnet-20241022", Label: "Claude 3.5 Sonnet"},
				{Value: "claude-3-5-haiku-20241022", Label: "Claude 3.5 Haiku"},
				{Value: "claude-3-opus-20240229", Label: "Claude 3 Opus"},
			},
		},
		{
			ID:   "openai",
			Name: "OpenAI (GPT)",
			Models: []ProviderModel{
				{Value: "gpt-5.2", Label: "GPT-5.2 (Latest)", Recommended: true},
				{Value: "gpt-5", Label: "GPT-5"},
				{Value: "gpt-5-mini", Label: "GPT-5 Mini (Fast & Cheap)"},
				{Value: "gpt-5-nano", Label: "GPT-5 Nano (Ultra-fast)"},
				{Value: "gpt-4o", Label: "GPT-4o"},
				{Value: "gpt-4o-mini", Label: "GPT-4o Mini"},
				{Value: "gpt-4-turbo", Label: "GPT-4 Turbo"},
				{Value: "gpt-3.5-turbo", Label: "GPT-3.5 Turbo"},
			},
		},
		{
			ID:   "groq",
			Name: "Groq (Ultra-fast)",
			Models: []ProviderModel{
				{Value: "openai/gpt-oss-20b", Label: "GPT-OSS-20B (Recommended)", Recommended: true},
				{Value: "llama-3.3-70b-versatile", Label: "Llama 3.3 70B"},
				{Value: "llama-3.1-8b-instant", Label: "Llama 3.1 8B (Ultra-fast)"},
				{Value: "mixtral-8x7b-32768", Label: "Mixtral 8x7B"},
				{Value: "gemma2-9b-it", Label: "Gemma 2 9B"},
			},
		},
		{
			ID:   "gemini",
			Name: "Google (Gemini)",
			Models: []ProviderModel{
				{Value: "gemini-3-pro-preview", Label: "Gemini 3 Pro Preview (Latest)", Recommended: true},
				{Value: "gemini-3-flash-preview", Label: "Gemini 3 Flash Preview (Fast)"},
				{Value: "gemini-2.5-flash", Label: "Gemini 2.5 Flash (Fast & Cheap)"},
				{Value: "gemini-2.5-pro", Label: "Gemini 2.5 Pro (Advanced)"},
				{Value: "gemini-1.5-flash", Label: "Gemini 1.5 Flash"},
				{Value: "gemini-1.5-pro", Label: "Gemini 1.5 Pro"},
			},
		},
		{
			ID:   "fireworks",
			Name: "Fireworks AI",
			Models: []ProviderModel{
				{Value: "accounts/fireworks/models/kimi-k2-thinking", Label: "Kimi K2 Thinking (Reasoning)", Recommended: true},
				{Value: "accounts/fireworks/models/kimi-k2p5", Label: "Kimi K2.5"},
				{Value: "accounts/fireworks/models/qwen3-vl-235b-a22b-thinking", Label: "Qwen 3 VL 235B Thinking (Vision)"},
				{Value: "accounts/fireworks/models/kimi-k2-instruct-0905", Label: "Kimi K2 Instruct"},
				{Value: "accounts/fireworks/models/qwen3-8b", Label: "Qwen 3 8B (Fast)"},
				{Value: "accounts/fireworks/models/glm-4p7", Label: "GLM-4 Plus 7B"},
				{Value: "accounts/fireworks/models/llama-v3p3-70b-instruct", Label: "Llama 3.3 70B"},
				{Value: "accounts/fireworks/models/llama-v3p1-405b-instruct", Label: "Llama 3.1 405B (Largest)"},
				{Value: "accounts/fireworks/models/llama-v3p1-70b-instruct", Label: "Llama 3.1 70B"},
				{Value: "accounts/fireworks/models/llama-v3p1-8b-instruct", Label: "Llama 3.1 8B (Fast)"},
				{Value: "accounts/fireworks/models/deepseek-v3", Label: "DeepSeek V3"},
				{Value: "accounts/fireworks/models/qwen2p5-72b-instruct", Label: "Qwen 2.5 72B"},
				{Value: "accounts/fireworks/models/mixtral-8x22b-instruct", Label: "Mixtral 8x22B"},
			},
		},
		{
			ID:   "xai",
			Name: "xAI (Grok)",
			Models: []ProviderModel{
				{Value: "grok-4-1-fast-reasoning", Label: "Grok 4.1 Fast Reasoning", Recommended: true},
				{Value: "grok-4-1-fast-non-reasoning", Label: "Grok 4.1 Fast Non-Reasoning"},
				{Value: "grok-2-latest", Label: "Grok 2 Latest"},
				{Value: "grok-2-1212", Label: "Grok 2 (Dec 2024)"},
				{Value: "grok-beta", Label: "Grok Beta"},
			},
		},
		{
			ID:   "moonshot",
			Name: "Moonshot AI (Kimi)",
			Models: []ProviderModel{
				{Value: "kimi-k2-turbo-preview", Label: "Kimi K2 Turbo Preview (Latest)", Recommended: true},
				{Value: "kimi-k2.5", Label: "Kimi K2.5"},
				{Value: "kimi-k2-0711-preview", Label: "Kimi K2 Preview"},
				{Value: "moonshot-v1-128k", Label: "Moonshot V1 128K (Large Context)"},
				{Value: "moonshot-v1-32k", Label: "Moonshot V1 32K"},
				{Value: "moonshot-v1-8k", Label: "Moonshot V1 8K (Fast)"},
			},
		},
		{
			ID:   "venice",
			Name: "Venice AI (Uncensored)",
			Models: []ProviderModel{
				{Value: "zai-org-glm-4.7", Label: "GLM 4.7 Private (203K context)", Recommended: true},
				{Value: "venice-uncensored", Label: "Venice Uncensored"},
			},
		},
		{
			ID:   "together",
			Name: "Together AI",
			Models: []ProviderModel{
				{Value: "moonshotai/Kimi-K2-Thinking", Label: "Kimi K2 Thinking (Reasoning)", Recommended: true},
				{Value: "moonshotai/Kimi-K2.5", Label: "Kimi K2.5"},
				{Value: "moonshotai/Kimi-K2-Instruct", Label: "Kimi K2 Instruct"},
				{Value: "deepseek-ai/DeepSeek-R1", Label: "DeepSeek R1 (Reasoning)"},
				{Value: "deepseek-ai/DeepSeek-V3", Label: "DeepSeek V3"},
				{Value: "meta-llama/Llama-3.3-70B-Instruct-Turbo", Label: "Llama 3.3 70B Turbo"},
				{Value: "meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo", Label: "Llama 3.1 405B Turbo (Largest)"},
				{Value: "meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo", Label: "Llama 3.1 70B Turbo"},
				{Value: "meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo", Label: "Llama 3.1 8B Turbo (Fast)"},
				{Value: "Qwen/Qwen2.5-72B-Instruct-Turbo", Label: "Qwen 2.5 72B Turbo"},
			},
		},
	}
}

// EmbeddingProviderInfo represents an embedding provider and its configuration
type EmbeddingProviderInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DefaultModel string `json:"default_model"`
	Dimensions   int    `json:"dimensions"`
	HasAPIKey    bool   `json:"has_api_key"`
	EnvVar       string `json:"env_var"`
}

// GetEmbeddingProviders returns the list of supported embedding providers
func GetEmbeddingProviders() []EmbeddingProviderInfo {
	providers := []EmbeddingProviderInfo{
		{
			ID:           "openai",
			Name:         "OpenAI",
			DefaultModel: "text-embedding-3-small",
			Dimensions:   1536,
			HasAPIKey:    config.HasAPIKey("openai"),
			EnvVar:       "OPENAI_API_KEY",
		},
		{
			ID:           "gemini",
			Name:         "Google Gemini",
			DefaultModel: "text-embedding-004",
			Dimensions:   768,
			HasAPIKey:    config.HasAPIKey("gemini"),
			EnvVar:       "GEMINI_API_KEY",
		},
		{
			ID:           "jina",
			Name:         "Jina AI",
			DefaultModel: "jina-embeddings-v3",
			Dimensions:   1024,
			HasAPIKey:    config.HasAPIKey("jina"),
			EnvVar:       "JINA_API_KEY",
		},
		{
			ID:           "voyage",
			Name:         "Voyage AI",
			DefaultModel: "voyage-3.5-lite",
			Dimensions:   1024,
			HasAPIKey:    config.HasAPIKey("voyage"),
			EnvVar:       "VOYAGE_API_KEY",
		},
		{
			ID:           "cohere",
			Name:         "Cohere",
			DefaultModel: "embed-english-v3.0",
			Dimensions:   1024,
			HasAPIKey:    config.HasAPIKey("cohere"),
			EnvVar:       "COHERE_API_KEY",
		},
		{
			ID:           "huggingface",
			Name:         "HuggingFace",
			DefaultModel: "BAAI/bge-small-en-v1.5",
			Dimensions:   384,
			HasAPIKey:    config.HasAPIKey("huggingface"),
			EnvVar:       "HUGGINGFACE_API_KEY",
		},
		{
			ID:           "ollama",
			Name:         "Ollama (Local)",
			DefaultModel: "nomic-embed-text",
			Dimensions:   768,
			HasAPIKey:    true, // Always available if Ollama is running
			EnvVar:       "",
		},
	}
	return providers
}

// ProvidersResponse is the response format for the /providers endpoint
type ProvidersResponse struct {
	LLM       []ProviderInfo          `json:"llm"`
	Embedding []EmbeddingProviderInfo `json:"embedding"`
}

// HandleProviders handles GET requests for available providers and models
func HandleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := ProvidersResponse{
		LLM:       GetProviders(),
		Embedding: GetEmbeddingProviders(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
