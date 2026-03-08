package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

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
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Models     []ProviderModel `json:"models"`
	CustomURL  bool            `json:"custom_url,omitempty"`  // Provider accepts a custom base URL
	DefaultURL string          `json:"default_url,omitempty"` // Default base URL (shown as placeholder)
	NoAPIKey   bool            `json:"no_api_key,omitempty"`  // Provider doesn't require an API key
}

// GetProviders returns the list of supported providers and their models
func GetProviders() []ProviderInfo {
	return []ProviderInfo{
		{
			ID:   "anthropic",
			Name: "Anthropic (Claude)",
			Models: []ProviderModel{
				{Value: "claude-opus-4-6", Label: "Claude Opus 4.6 (Most Capable)", Recommended: true},
				{Value: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6"},
				{Value: "claude-sonnet-4-5", Label: "Claude Sonnet 4.5"},
				{Value: "claude-haiku-4-5", Label: "Claude Haiku 4.5 (Fast)"},
			},
		},
		{
			ID:   "openai",
			Name: "OpenAI (GPT)",
			Models: []ProviderModel{
				{Value: "gpt-5.4", Label: "GPT-5.4 (Latest)", Recommended: true},
				{Value: "gpt-5.2", Label: "GPT-5.2"},
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
				{Value: "gemini-3.1-pro-preview", Label: "Gemini 3.1 Pro Preview (Latest)", Recommended: true},
				{Value: "gemini-3.1-flash-lite-preview", Label: "Gemini 3.1 Flash Lite Preview (Fast)"},
				{Value: "gemini-3-pro-preview", Label: "Gemini 3 Pro Preview"},
				{Value: "gemini-3-flash-preview", Label: "Gemini 3 Flash Preview"},
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
				{Value: "accounts/fireworks/models/glm-5", Label: "GLM 5 (745B MoE, Agentic)", Recommended: true},
				{Value: "accounts/fireworks/models/minimax-m2p5", Label: "MiniMax M2.5"},
				{Value: "accounts/fireworks/models/kimi-k2-thinking", Label: "Kimi K2 Thinking (Reasoning)"},
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
			ID:   "zai",
			Name: "Z.ai (GLM)",
			Models: []ProviderModel{
				{Value: "glm-5", Label: "GLM 5 (745B MoE, Agentic)", Recommended: true},
				{Value: "glm-4.7", Label: "GLM 4.7 (200K)"},
				{Value: "glm-4.6", Label: "GLM 4.6 (Flagship, 200K)"},
				{Value: "glm-4.5", Label: "GLM 4.5 (128K)"},
				{Value: "glm-4.5v", Label: "GLM 4.5V (Vision, 128K)"},
				{Value: "glm-4.5-air", Label: "GLM 4.5 Air (Lightweight)"},
				{Value: "glm-4.5-flash", Label: "GLM 4.5 Flash (Free)"},
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
				{Value: "olafangensan-glm-4.7-flash-heretic", Label: "GLM 4.7 Flash Heretic (Thinking)", Recommended: true},
				{Value: "zai-org-glm-4.7", Label: "GLM 4.7 Private (203K context)"},
				{Value: "venice-uncensored", Label: "Venice Uncensored"},
			},
		},
		{
			ID:   "novita",
			Name: "Novita AI",
			Models: []ProviderModel{
				{Value: "deepseek/deepseek-v3.2", Label: "DeepSeek V3.2", Recommended: true},
				{Value: "openai/gpt-oss-120b", Label: "GPT OSS 120B"},
				{Value: "openai/gpt-oss-20b", Label: "GPT OSS 20B (Fast)"},
				{Value: "qwen/qwen3-coder-480b-a35b-instruct", Label: "Qwen 3 Coder 480B"},
				{Value: "zai-org/glm-5", Label: "GLM 5 (745B MoE, Agentic)"},
				{Value: "zai-org/glm-4.7", Label: "GLM 4.7"},
			},
		},
		{
			ID:   "mistral",
			Name: "Mistral AI",
			Models: []ProviderModel{
				{Value: "mistral-large-3-25-12", Label: "Mistral Large 3 (Flagship, Open-Weight)", Recommended: true},
				{Value: "mistral-medium-3-1-25-08", Label: "Mistral Medium 3.1 (Multimodal)"},
				{Value: "mistral-small-3-2-25-06", Label: "Mistral Small 3.2 (Fast)"},
				{Value: "magistral-medium-2509", Label: "Magistral Medium (Reasoning, 40K)"},
				{Value: "magistral-small-2509", Label: "Magistral Small (Reasoning, 40K)"},
				{Value: "devstral-2-25-12", Label: "Devstral 2 (Code Agents)"},
				{Value: "codestral-2508", Label: "Codestral (Code Completion)"},
				{Value: "ministral-3-14b-25-12", Label: "Ministral 3 14B (Open-Weight)"},
				{Value: "ministral-3-8b-25-12", Label: "Ministral 3 8B (Open-Weight, Fast)"},
				{Value: "ministral-3-3b-25-12", Label: "Ministral 3 3B (Open-Weight, Edge)"},
			},
		},
		{
			ID:         "ollama",
			Name:       "Ollama (Local)",
			CustomURL:  true,
			DefaultURL: "http://localhost:11434",
			NoAPIKey:   true,
			Models: []ProviderModel{
				{Value: "llama3.1:latest", Label: "Llama 3.1 (8B)", Recommended: true},
				{Value: "llama3.1:70b", Label: "Llama 3.1 (70B)"},
				{Value: "qwen3:latest", Label: "Qwen 3"},
				{Value: "qwen3:32b", Label: "Qwen 3 (32B)"},
				{Value: "gemma3:latest", Label: "Gemma 3"},
				{Value: "deepseek-r1:latest", Label: "DeepSeek R1"},
				{Value: "mistral:latest", Label: "Mistral"},
				{Value: "phi4:latest", Label: "Phi 4"},
				{Value: "command-r:latest", Label: "Command R"},
			},
		},
		{
			ID:   "cerebras",
			Name: "Cerebras (Ultra-fast)",
			Models: []ProviderModel{
				{Value: "gpt-oss-120b", Label: "GPT-OSS 120B (OpenAI Open-Weight, 128K)", Recommended: true},
				{Value: "llama-4-scout-17b-16e-instruct", Label: "Llama 4 Scout 17B"},
				{Value: "llama3.3-70b", Label: "Llama 3.3 70B"},
				{Value: "llama3.1-8b", Label: "Llama 3.1 8B (Ultra-fast)"},
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

// HandleOllamaModels proxies to Ollama's /api/tags to list locally available models.
// The browser can't call Ollama directly due to CORS.
func HandleOllamaModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Determine Ollama base URL: query param > config > env > default
	ollamaURL := r.URL.Query().Get("url")
	if ollamaURL == "" {
		cfg := config.GetConfig()
		if cfg != nil {
			ollamaURL = cfg.GetLLMConfig().BaseURL
		}
	}
	if ollamaURL == "" {
		ollamaURL = os.Getenv("OLLAMA_BASE_URL")
	}
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ollamaURL + "/api/tags")
	if err != nil {
		log.Printf("Failed to connect to Ollama at %s: %v", ollamaURL, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  fmt.Sprintf("Cannot connect to Ollama at %s", ollamaURL),
			"models": []interface{}{},
		})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read Ollama response", http.StatusInternalServerError)
		return
	}

	// Parse Ollama response and convert to our ProviderModel format
	var ollamaResp struct {
		Models []struct {
			Name    string `json:"name"`
			Size    int64  `json:"size"`
			Details struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
				Quantization  string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}

	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		// Forward raw response
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
		return
	}

	// Convert to ProviderModel format
	models := make([]ProviderModel, 0, len(ollamaResp.Models))
	for _, m := range ollamaResp.Models {
		label := m.Name
		if m.Details.ParameterSize != "" {
			label += fmt.Sprintf(" (%s", m.Details.ParameterSize)
			if m.Details.Quantization != "" {
				label += ", " + m.Details.Quantization
			}
			label += ")"
		}
		// Show size in GB
		if m.Size > 0 {
			sizeGB := float64(m.Size) / (1024 * 1024 * 1024)
			label += fmt.Sprintf(" [%.1fGB]", sizeGB)
		}
		models = append(models, ProviderModel{
			Value: m.Name,
			Label: label,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"models": models,
		"url":    ollamaURL,
	})
}
