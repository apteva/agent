package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/apteva/agent/config"
)

// modelCache stores cached model lists per provider
var modelCache sync.Map // map[string]*cachedModels

// cacheTTL is how long cached models remain valid
const cacheTTL = 1 * time.Hour

type cachedModels struct {
	Models    []ProviderModel
	FetchedAt time.Time
}

// providerEndpoint describes how to fetch models from a provider's API
type providerEndpoint struct {
	BaseURL   string // API base URL
	EnvVar    string // Env var for API key
	AuthStyle string // "bearer" or "x-api-key"
	// ModelsPath overrides the default "/models" suffix
	ModelsPath string
	// Filter controls which models to include from the response
	Filter func(id string, model openAIModel) bool
}

// openAIModel represents a model in OpenAI-compatible /models response
type openAIModel struct {
	ID      string      `json:"id"`
	Created int64       `json:"created"`
	OwnedBy string     `json:"owned_by,omitempty"`
	Object  string      `json:"object,omitempty"`
	// Venice-specific
	ModelSpec *modelSpec `json:"model_spec,omitempty"`
}

type modelSpec struct {
	Name                   string       `json:"name,omitempty"`
	Description            string       `json:"description,omitempty"`
	Traits                 []string     `json:"traits,omitempty"`
	Offline                bool         `json:"offline,omitempty"`
	AvailableContextTokens int          `json:"availableContextTokens,omitempty"`
	Capabilities           *modelCaps   `json:"capabilities,omitempty"`
}

type modelCaps struct {
	SupportsFunctionCalling bool `json:"supportsFunctionCalling,omitempty"`
	SupportsVision          bool `json:"supportsVision,omitempty"`
	SupportsReasoning       bool `json:"supportsReasoning,omitempty"`
	OptimizedForCode        bool `json:"optimizedForCode,omitempty"`
	SupportsWebSearch       bool `json:"supportsWebSearch,omitempty"`
}

// providerEndpoints maps provider IDs to their model listing endpoints
var providerEndpoints = map[string]providerEndpoint{
	"openai": {
		BaseURL:   "https://api.openai.com/v1",
		EnvVar:    "OPENAI_API_KEY",
		AuthStyle: "bearer",
		Filter:    filterChatModels,
	},
	"groq": {
		BaseURL:   "https://api.groq.com/openai/v1",
		EnvVar:    "GROQ_API_KEY",
		AuthStyle: "bearer",
	},
	"fireworks": {
		BaseURL:   "https://api.fireworks.ai/inference/v1",
		EnvVar:    "FIREWORKS_API_KEY",
		AuthStyle: "bearer",
	},
	"xai": {
		BaseURL:   "https://api.x.ai/v1",
		EnvVar:    "XAI_API_KEY",
		AuthStyle: "bearer",
	},
	"venice": {
		BaseURL:   "https://api.venice.ai/api/v1",
		EnvVar:    "VENICE_API_KEY",
		AuthStyle: "bearer",
	},
	"together": {
		BaseURL:   "https://api.together.xyz/v1",
		EnvVar:    "TOGETHER_API_KEY",
		AuthStyle: "bearer",
		Filter:    filterChatModels,
	},
	"novita": {
		BaseURL:   "https://api.novita.ai/v3/openai",
		EnvVar:    "NOVITA_API_KEY",
		AuthStyle: "bearer",
	},
	"mistral": {
		BaseURL:   "https://api.mistral.ai/v1",
		EnvVar:    "MISTRAL_API_KEY",
		AuthStyle: "bearer",
	},
	"cerebras": {
		BaseURL:   "https://api.cerebras.ai/v1",
		EnvVar:    "CEREBRAS_API_KEY",
		AuthStyle: "bearer",
	},
	"moonshot": {
		BaseURL:   "https://api.moonshot.ai/v1",
		EnvVar:    "MOONSHOT_API_KEY",
		AuthStyle: "bearer",
	},
	"zai": {
		BaseURL:    "https://api.z.ai/api/paas/v4",
		EnvVar:     "ZAI_API_KEY",
		AuthStyle:  "bearer",
		ModelsPath: "/models",
	},
}

// filterChatModels filters out non-chat models (embeddings, audio, etc.)
func filterChatModels(id string, model openAIModel) bool {
	lower := strings.ToLower(id)
	// Skip embedding, whisper, tts, dall-e, moderation models
	skipPrefixes := []string{"text-embedding", "whisper", "tts", "dall-e", "davinci", "babbage", "ada", "curie"}
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	skipContains := []string{"embedding", "moderation", "realtime", "transcription", "search"}
	for _, s := range skipContains {
		if strings.Contains(lower, s) {
			return false
		}
	}
	return true
}

// customFetchers maps provider IDs to custom (non-OpenAI-compatible) fetch functions
var customFetchers = map[string]func() []ProviderModel{
	"anthropic": fetchAnthropicModels,
	"gemini":    fetchGeminiModels,
}

// FetchModelsForProvider fetches models from a provider's API, using cache if available.
// Returns nil if provider doesn't support live fetching or fetch fails.
func FetchModelsForProvider(providerID string) []ProviderModel {
	// Check cache first
	if cached, ok := modelCache.Load(providerID); ok {
		cm := cached.(*cachedModels)
		if time.Since(cm.FetchedAt) < cacheTTL {
			return cm.Models
		}
	}

	var models []ProviderModel

	// Try custom fetcher first (Anthropic, Gemini)
	if fetcher, ok := customFetchers[providerID]; ok {
		models = fetcher()
	} else {
		// OpenAI-compatible endpoint
		endpoint, ok := providerEndpoints[providerID]
		if !ok {
			return nil
		}
		apiKey := os.Getenv(endpoint.EnvVar)
		if apiKey == "" {
			return nil
		}
		models = fetchOpenAICompatibleModels(providerID, endpoint, apiKey)
	}

	if models == nil {
		return nil
	}

	// Cache the results
	modelCache.Store(providerID, &cachedModels{
		Models:    models,
		FetchedAt: time.Now(),
	})

	return models
}

// fetchAnthropicModels fetches models from Anthropic's /v1/models endpoint.
// Response format: { "data": [{ "id": "...", "display_name": "...", "created_at": "..." }] }
func fetchAnthropicModels() []ProviderModel {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://api.anthropic.com/v1/models?limit=100", nil)
	if err != nil {
		log.Printf("⚠️  Model fetch: failed to create Anthropic request: %v", err)
		return nil
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("⚠️  Model fetch: failed to reach Anthropic: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("⚠️  Model fetch: Anthropic returned HTTP %d", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("⚠️  Model fetch: failed to read Anthropic response: %v", err)
		return nil
	}

	var anthropicResp struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			CreatedAt   string `json:"created_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		log.Printf("⚠️  Model fetch: failed to parse Anthropic response: %v", err)
		return nil
	}

	var models []ProviderModel
	for _, m := range anthropicResp.Data {
		if m.ID == "" {
			continue
		}
		label := m.DisplayName
		if label == "" {
			label = modelIDToLabel(m.ID)
		}
		models = append(models, ProviderModel{
			Value: m.ID,
			Label: label,
		})
	}

	log.Printf("📋 Model fetch: loaded %d models from anthropic", len(models))
	return models
}

// fetchGeminiModels fetches models from Google's generativelanguage API.
// Response format: { "models": [{ "name": "models/...", "displayName": "...", ... }] }
func fetchGeminiModels() []ProviderModel {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models?pageSize=100&key=%s", apiKey)

	resp, err := client.Get(url)
	if err != nil {
		log.Printf("⚠️  Model fetch: failed to reach Gemini: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("⚠️  Model fetch: Gemini returned HTTP %d", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("⚠️  Model fetch: failed to read Gemini response: %v", err)
		return nil
	}

	var geminiResp struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			Description                string   `json:"description"`
			InputTokenLimit            int      `json:"inputTokenLimit"`
			OutputTokenLimit           int      `json:"outputTokenLimit"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			Thinking                   bool     `json:"thinking,omitempty"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		log.Printf("⚠️  Model fetch: failed to parse Gemini response: %v", err)
		return nil
	}

	var models []ProviderModel
	for _, m := range geminiResp.Models {
		// Only include models that support generateContent (chat models)
		supportsChat := false
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" {
				supportsChat = true
				break
			}
		}
		if !supportsChat {
			continue
		}

		// Strip "models/" prefix from name
		id := strings.TrimPrefix(m.Name, "models/")

		label := m.DisplayName
		if label == "" {
			label = modelIDToLabel(id)
		}

		pm := ProviderModel{
			Value:       id,
			Label:       label,
			Description: m.Description,
			ContextSize: m.InputTokenLimit,
		}
		// Gemini multimodal models support vision
		if strings.Contains(id, "gemini") {
			pm.Capabilities = append(pm.Capabilities, "vision")
		}
		if m.Thinking {
			pm.Capabilities = append(pm.Capabilities, "reasoning")
		}

		models = append(models, pm)
	}

	log.Printf("📋 Model fetch: loaded %d models from gemini", len(models))
	return models
}

// fetchOpenAICompatibleModels fetches from an OpenAI-compatible /models endpoint
func fetchOpenAICompatibleModels(providerID string, endpoint providerEndpoint, apiKey string) []ProviderModel {
	modelsPath := "/models"
	if endpoint.ModelsPath != "" {
		modelsPath = endpoint.ModelsPath
	}
	url := endpoint.BaseURL + modelsPath

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("⚠️  Model fetch: failed to create request for %s: %v", providerID, err)
		return nil
	}

	if endpoint.AuthStyle == "bearer" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("⚠️  Model fetch: failed to reach %s at %s: %v", providerID, url, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("⚠️  Model fetch: %s returned HTTP %d", providerID, resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("⚠️  Model fetch: failed to read response from %s: %v", providerID, err)
		return nil
	}

	// Try OpenAI-style {"data": [...]} first, then bare array [...]
	var rawModels []openAIModel
	var modelsResp struct {
		Data []openAIModel `json:"data"`
	}
	if err := json.Unmarshal(body, &modelsResp); err == nil && len(modelsResp.Data) > 0 {
		rawModels = modelsResp.Data
	} else if err := json.Unmarshal(body, &rawModels); err != nil {
		log.Printf("⚠️  Model fetch: failed to parse %s response: %v", providerID, err)
		return nil
	}

	var models []ProviderModel
	for _, m := range rawModels {
		if m.ID == "" {
			continue
		}

		// Skip offline models
		if m.ModelSpec != nil && m.ModelSpec.Offline {
			continue
		}

		// Apply filter if defined
		if endpoint.Filter != nil && !endpoint.Filter(m.ID, m) {
			continue
		}

		// Use human-readable name from model_spec when available (e.g. Venice)
		label := ""
		if m.ModelSpec != nil && m.ModelSpec.Name != "" {
			label = m.ModelSpec.Name
		} else {
			label = modelIDToLabel(m.ID)
		}

		pm := ProviderModel{
			Value: m.ID,
			Label: label,
		}

		// Extract structured metadata from Venice-style responses
		if m.ModelSpec != nil {
			pm.Description = m.ModelSpec.Description
			pm.ContextSize = m.ModelSpec.AvailableContextTokens

			if m.ModelSpec.Capabilities != nil {
				if m.ModelSpec.Capabilities.SupportsVision {
					pm.Capabilities = append(pm.Capabilities, "vision")
				}
				if m.ModelSpec.Capabilities.SupportsReasoning {
					pm.Capabilities = append(pm.Capabilities, "reasoning")
				}
				if m.ModelSpec.Capabilities.SupportsFunctionCalling {
					pm.Capabilities = append(pm.Capabilities, "tools")
				}
				if m.ModelSpec.Capabilities.OptimizedForCode {
					pm.Capabilities = append(pm.Capabilities, "code")
				}
				if m.ModelSpec.Capabilities.SupportsWebSearch {
					pm.Capabilities = append(pm.Capabilities, "web")
				}
			}

			// Copy traits (e.g. "uncensored", "fastest", "default")
			pm.Tags = m.ModelSpec.Traits
		}

		models = append(models, pm)
	}

	log.Printf("📋 Model fetch: loaded %d models from %s", len(models), providerID)
	return models
}

// modelIDToLabel converts a model ID to a human-readable label
// e.g. "llama-3.3-70b-versatile" -> "Llama 3.3 70B Versatile"
func modelIDToLabel(id string) string {
	// Strip common prefixes
	label := id
	prefixes := []string{
		"accounts/fireworks/models/",
		"accounts/together/models/",
		"meta-llama/",
		"deepseek-ai/",
		"moonshotai/",
		"Qwen/",
		"qwen/",
		"openai/",
		"zai-org/",
		"deepseek/",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(label, prefix) {
			label = label[len(prefix):]
			break
		}
	}

	// Replace separators with spaces
	label = strings.ReplaceAll(label, "-", " ")
	label = strings.ReplaceAll(label, "_", " ")

	// Title case each word
	words := strings.Fields(label)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}

	return strings.Join(words, " ")
}

// GetProvidersWithLiveModels returns providers with live model data.
// Uses live-fetched models when available, falls back to hardcoded only
// when no API key is set or when live fetch hasn't completed yet.
func GetProvidersWithLiveModels() []ProviderInfo {
	providers := GetProviders()

	for i := range providers {
		providerID := providers[i].ID
		if providerID == "ollama" {
			continue // Ollama has its own live handler
		}

		// Only fetch if the provider has an API key
		if !config.HasAPIKey(providerID) {
			continue
		}

		live := FetchModelsForProvider(providerID)
		if live != nil {
			providers[i].Models = live
		}
		// else: keep hardcoded as fallback until cache warms up
	}

	return providers
}

// InvalidateModelCache clears the cached models for a provider (or all if empty)
func InvalidateModelCache(providerID string) {
	if providerID == "" {
		modelCache.Range(func(key, value interface{}) bool {
			modelCache.Delete(key)
			return true
		})
		log.Printf("📋 Model cache: cleared all")
	} else {
		modelCache.Delete(providerID)
		log.Printf("📋 Model cache: cleared %s", providerID)
	}
}

// WarmModelCache pre-fetches models for all providers with API keys.
// Call this at startup in a goroutine.
func WarmModelCache() {
	// OpenAI-compatible providers
	for providerID := range providerEndpoints {
		if config.HasAPIKey(providerID) {
			go FetchModelsForProvider(providerID)
		}
	}
	// Custom providers (Anthropic, Gemini)
	for providerID := range customFetchers {
		if config.HasAPIKey(providerID) {
			go FetchModelsForProvider(providerID)
		}
	}
}

// HandleProviderModels handles GET /providers/{id}/models — fetches live models for one provider
func HandleProviderModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract provider ID from path: /providers/{id}/models
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	providerID := parts[1]

	// Force refresh if requested
	if r.URL.Query().Get("refresh") == "true" {
		InvalidateModelCache(providerID)
	}

	live := FetchModelsForProvider(providerID)
	models := live

	// Fall back to hardcoded if live fetch not available
	if models == nil {
		for _, p := range GetProviders() {
			if p.ID == providerID {
				models = p.Models
				break
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"provider": providerID,
		"models":   models,
		"live":     live != nil,
	})
}
