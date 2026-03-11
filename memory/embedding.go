package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/apteva/agent/config"
)

// getOpenAIKey retrieves the OpenAI API key from environment
func getOpenAIKey() string {
	return os.Getenv("OPENAI_API_KEY")
}

// EmbeddingProvider generates embeddings for text
type EmbeddingProvider interface {
	// GenerateEmbedding generates an embedding vector for the given text
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)

	// GenerateBatchEmbeddings generates embeddings for multiple texts at once
	// Returns embeddings in the same order as input texts
	GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error)

	// Name returns the provider name
	Name() string
}

// NewEmbeddingProvider creates an embedding provider based on config
// Supports: openai, gemini, jina, voyage, cohere, huggingface, ollama
// Provider MUST be explicitly configured - no auto-selection
func NewEmbeddingProvider(cfg *config.MemoryConfig) (EmbeddingProvider, error) {
	provider := config.GetEmbeddingProvider(cfg)

	// Require explicit provider configuration
	if provider == "" || provider == "auto" {
		return nil, fmt.Errorf("embedding_provider must be explicitly configured in memory settings (options: openai, gemini, jina, voyage, cohere, huggingface, ollama)")
	}

	// Get model for the selected provider (use config override if specified)
	model := cfg.EmbeddingModel
	if model == "" {
		model = config.GetEmbeddingModelForProvider(provider)
	}

	log.Printf("[Memory] Using %s/%s for embeddings", provider, model)

	switch provider {
	case "ollama":
		ollamaURL := config.GetOllamaURL(cfg)
		return NewOllamaEmbeddingProvider(ollamaURL, model), nil
	case "gemini":
		return NewGeminiEmbeddingProvider(model, cfg.EmbeddingDimensions)
	case "jina":
		return NewJinaEmbeddingProvider(model)
	case "voyage":
		return NewVoyageEmbeddingProvider(model)
	case "cohere":
		return NewCohereEmbeddingProvider(model)
	case "huggingface":
		return NewHuggingFaceEmbeddingProvider(model)
	case "openai":
		return NewOpenAIEmbeddingProvider(model)
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s (supported: openai, gemini, jina, voyage, cohere, huggingface, ollama)", provider)
	}
}

// OpenAIEmbeddingProvider uses OpenAI's embedding API
type OpenAIEmbeddingProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// OpenAI API request/response structures
type openAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// NewOpenAIEmbeddingProvider creates a new OpenAI embedding provider
func NewOpenAIEmbeddingProvider(model string) (*OpenAIEmbeddingProvider, error) {
	apiKey := getOpenAIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}

	return &OpenAIEmbeddingProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (p *OpenAIEmbeddingProvider) Name() string {
	return "openai"
}

func (p *OpenAIEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.GenerateBatchEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// GenerateBatchEmbeddings generates embeddings for multiple texts in a single API call
// OpenAI supports up to 2048 inputs per request, we use batches of 100 to be safe
func (p *OpenAIEmbeddingProvider) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	const batchSize = 100
	allEmbeddings := make([][]float32, len(texts))

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := p.generateBatchInternal(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d failed: %w", i, end, err)
		}

		for j, emb := range embeddings {
			allEmbeddings[i+j] = emb
		}
	}

	return allEmbeddings, nil
}

func (p *OpenAIEmbeddingProvider) generateBatchInternal(ctx context.Context, texts []string) ([][]float32, error) {
	req := openAIEmbeddingRequest{
		Model: p.model,
		Input: texts,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embResp openAIEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embResp.Data))
	}

	// OpenAI returns embeddings with index field, sort by index to ensure correct order
	result := make([][]float32, len(texts))
	for _, data := range embResp.Data {
		result[data.Index] = data.Embedding
	}

	return result, nil
}

// OllamaEmbeddingProvider uses Ollama's embedding API
type OllamaEmbeddingProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

// Ollama API request/response structures
type ollamaEmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

// NewOllamaEmbeddingProvider creates a new Ollama embedding provider
func NewOllamaEmbeddingProvider(baseURL, model string) *OllamaEmbeddingProvider {
	return &OllamaEmbeddingProvider{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second}, // Longer timeout for local models
	}
}

func (p *OllamaEmbeddingProvider) Name() string {
	return "ollama"
}

func (p *OllamaEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	req := ollamaEmbeddingRequest{
		Model:  p.model,
		Prompt: text,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embResp ollamaEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Embedding) == 0 {
		return nil, fmt.Errorf("no embedding returned from Ollama")
	}

	// Convert []float64 to []float32
	result := make([]float32, len(embResp.Embedding))
	for i, v := range embResp.Embedding {
		result[i] = float32(v)
	}

	return result, nil
}

// GenerateBatchEmbeddings for Ollama falls back to sequential calls
// since Ollama doesn't support batch embedding natively
func (p *OllamaEmbeddingProvider) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := p.GenerateEmbedding(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding %d failed: %w", i, err)
		}
		results[i] = emb
	}
	return results, nil
}

// ============================================================================
// Gemini Embedding Provider
// ============================================================================

// GeminiEmbeddingProvider uses Google's Gemini embedding API
type GeminiEmbeddingProvider struct {
	apiKey     string
	model      string
	dimensions int // output_dimensionality (0 = use model default)
	client     *http.Client
}

// Gemini API request/response structures
type geminiEmbeddingRequest struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
	OutputDimensionality int `json:"output_dimensionality,omitempty"`
}

type geminiBatchEmbeddingRequest struct {
	Requests []struct {
		Model   string `json:"model"`
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		OutputDimensionality int `json:"output_dimensionality,omitempty"`
	} `json:"requests"`
}

type geminiEmbeddingResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

type geminiBatchEmbeddingResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
}

// NewGeminiEmbeddingProvider creates a new Gemini embedding provider
// dimensions controls output_dimensionality (0 = use model default: 3072 for gemini-embedding-2)
func NewGeminiEmbeddingProvider(model string, dimensions int) (*GeminiEmbeddingProvider, error) {
	apiKey := config.GetAPIKey("gemini")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}

	if model == "" {
		model = config.EmbeddingModels["gemini"]
	}

	return &GeminiEmbeddingProvider{
		apiKey:     apiKey,
		model:      model,
		dimensions: dimensions,
		client:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (p *GeminiEmbeddingProvider) Name() string {
	return "gemini"
}

func (p *GeminiEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	req := geminiEmbeddingRequest{}
	req.Content.Parts = []struct {
		Text string `json:"text"`
	}{{Text: text}}
	if p.dimensions > 0 {
		req.OutputDimensionality = p.dimensions
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:embedContent?key=%s", p.model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embResp geminiEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Embedding.Values) == 0 {
		return nil, fmt.Errorf("no embedding returned from Gemini")
	}

	return embResp.Embedding.Values, nil
}

// GenerateBatchEmbeddings uses Gemini's batch embedding endpoint
func (p *GeminiEmbeddingProvider) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Gemini supports batch embeddings up to 100 texts
	const batchSize = 100
	allEmbeddings := make([][]float32, len(texts))

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := p.generateBatchInternal(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d failed: %w", i, end, err)
		}

		for j, emb := range embeddings {
			allEmbeddings[i+j] = emb
		}
	}

	return allEmbeddings, nil
}

func (p *GeminiEmbeddingProvider) generateBatchInternal(ctx context.Context, texts []string) ([][]float32, error) {
	// Build batch request
	var req geminiBatchEmbeddingRequest
	for _, text := range texts {
		reqItem := struct {
			Model   string `json:"model"`
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			OutputDimensionality int `json:"output_dimensionality,omitempty"`
		}{
			Model: "models/" + p.model,
		}
		reqItem.Content.Parts = []struct {
			Text string `json:"text"`
		}{{Text: text}}
		if p.dimensions > 0 {
			reqItem.OutputDimensionality = p.dimensions
		}
		req.Requests = append(req.Requests, reqItem)
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:batchEmbedContents?key=%s", p.model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embResp geminiBatchEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embResp.Embeddings))
	}

	result := make([][]float32, len(texts))
	for i, emb := range embResp.Embeddings {
		result[i] = emb.Values
	}

	return result, nil
}

// ============================================================================
// Jina Embedding Provider (free tier available)
// ============================================================================

// JinaEmbeddingProvider uses Jina AI's embedding API
type JinaEmbeddingProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// Jina API request/response structures (OpenAI-compatible)
type jinaEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type jinaEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// NewJinaEmbeddingProvider creates a new Jina embedding provider
func NewJinaEmbeddingProvider(model string) (*JinaEmbeddingProvider, error) {
	apiKey := config.GetAPIKey("jina")
	// Jina allows limited free usage without API key, but key is recommended
	if apiKey == "" {
		apiKey = "" // Will still work for basic usage
	}

	if model == "" {
		model = config.EmbeddingModels["jina"]
	}

	return &JinaEmbeddingProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (p *JinaEmbeddingProvider) Name() string {
	return "jina"
}

func (p *JinaEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.GenerateBatchEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// GenerateBatchEmbeddings uses Jina's batch embedding endpoint
func (p *JinaEmbeddingProvider) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	const batchSize = 100
	allEmbeddings := make([][]float32, len(texts))

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := p.generateBatchInternal(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d failed: %w", i, end, err)
		}

		for j, emb := range embeddings {
			allEmbeddings[i+j] = emb
		}
	}

	return allEmbeddings, nil
}

func (p *JinaEmbeddingProvider) generateBatchInternal(ctx context.Context, texts []string) ([][]float32, error) {
	req := jinaEmbeddingRequest{
		Model: p.model,
		Input: texts,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.jina.ai/v1/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Jina API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embResp jinaEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embResp.Data))
	}

	result := make([][]float32, len(texts))
	for _, data := range embResp.Data {
		result[data.Index] = data.Embedding
	}

	return result, nil
}

// ============================================================================
// Voyage Embedding Provider
// ============================================================================

// VoyageEmbeddingProvider uses Voyage AI's embedding API
type VoyageEmbeddingProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// Voyage API request/response structures
type voyageEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type voyageEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// NewVoyageEmbeddingProvider creates a new Voyage embedding provider
func NewVoyageEmbeddingProvider(model string) (*VoyageEmbeddingProvider, error) {
	apiKey := config.GetAPIKey("voyage")
	if apiKey == "" {
		return nil, fmt.Errorf("VOYAGE_API_KEY environment variable not set")
	}

	if model == "" {
		model = config.EmbeddingModels["voyage"]
	}

	return &VoyageEmbeddingProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (p *VoyageEmbeddingProvider) Name() string {
	return "voyage"
}

func (p *VoyageEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.GenerateBatchEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// GenerateBatchEmbeddings uses Voyage's batch embedding endpoint
func (p *VoyageEmbeddingProvider) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Voyage supports up to 128 texts per request
	const batchSize = 128
	allEmbeddings := make([][]float32, len(texts))

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := p.generateBatchInternal(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d failed: %w", i, end, err)
		}

		for j, emb := range embeddings {
			allEmbeddings[i+j] = emb
		}
	}

	return allEmbeddings, nil
}

func (p *VoyageEmbeddingProvider) generateBatchInternal(ctx context.Context, texts []string) ([][]float32, error) {
	req := voyageEmbeddingRequest{
		Model: p.model,
		Input: texts,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.voyageai.com/v1/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Voyage API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embResp voyageEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embResp.Data))
	}

	result := make([][]float32, len(texts))
	for _, data := range embResp.Data {
		result[data.Index] = data.Embedding
	}

	return result, nil
}

// ============================================================================
// Cohere Embedding Provider
// ============================================================================

// CohereEmbeddingProvider uses Cohere's embedding API
type CohereEmbeddingProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// Cohere API request/response structures
type cohereEmbeddingRequest struct {
	Model     string   `json:"model"`
	Texts     []string `json:"texts"`
	InputType string   `json:"input_type"`
}

type cohereEmbeddingResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// NewCohereEmbeddingProvider creates a new Cohere embedding provider
func NewCohereEmbeddingProvider(model string) (*CohereEmbeddingProvider, error) {
	apiKey := config.GetAPIKey("cohere")
	if apiKey == "" {
		return nil, fmt.Errorf("COHERE_API_KEY environment variable not set")
	}

	if model == "" {
		model = config.EmbeddingModels["cohere"]
	}

	return &CohereEmbeddingProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (p *CohereEmbeddingProvider) Name() string {
	return "cohere"
}

func (p *CohereEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.GenerateBatchEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// GenerateBatchEmbeddings uses Cohere's batch embedding endpoint
func (p *CohereEmbeddingProvider) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Cohere supports up to 96 texts per request
	const batchSize = 96
	allEmbeddings := make([][]float32, len(texts))

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := p.generateBatchInternal(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d failed: %w", i, end, err)
		}

		for j, emb := range embeddings {
			allEmbeddings[i+j] = emb
		}
	}

	return allEmbeddings, nil
}

func (p *CohereEmbeddingProvider) generateBatchInternal(ctx context.Context, texts []string) ([][]float32, error) {
	req := cohereEmbeddingRequest{
		Model:     p.model,
		Texts:     texts,
		InputType: "search_document", // For storing documents
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.cohere.com/v1/embed", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Cohere API error (status %d): %s", resp.StatusCode, string(body))
	}

	var embResp cohereEmbeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embResp.Embeddings))
	}

	return embResp.Embeddings, nil
}

// ============================================================================
// HuggingFace Embedding Provider
// ============================================================================

// HuggingFaceEmbeddingProvider uses HuggingFace's Inference API
type HuggingFaceEmbeddingProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewHuggingFaceEmbeddingProvider creates a new HuggingFace embedding provider
func NewHuggingFaceEmbeddingProvider(model string) (*HuggingFaceEmbeddingProvider, error) {
	apiKey := config.GetAPIKey("huggingface")
	if apiKey == "" {
		return nil, fmt.Errorf("HUGGINGFACE_API_KEY environment variable not set")
	}

	if model == "" {
		model = config.EmbeddingModels["huggingface"]
	}

	return &HuggingFaceEmbeddingProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second}, // Longer timeout for cold starts
	}, nil
}

func (p *HuggingFaceEmbeddingProvider) Name() string {
	return "huggingface"
}

func (p *HuggingFaceEmbeddingProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.GenerateBatchEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// GenerateBatchEmbeddings generates embeddings using HuggingFace Inference API
// HuggingFace supports batch inputs as an array
func (p *HuggingFaceEmbeddingProvider) GenerateBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// HuggingFace Inference API can handle batches, but let's be conservative
	const batchSize = 32
	allEmbeddings := make([][]float32, len(texts))

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := p.generateBatchInternal(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d failed: %w", i, end, err)
		}

		for j, emb := range embeddings {
			allEmbeddings[i+j] = emb
		}
	}

	return allEmbeddings, nil
}

func (p *HuggingFaceEmbeddingProvider) generateBatchInternal(ctx context.Context, texts []string) ([][]float32, error) {
	// HuggingFace Inference API expects {"inputs": [...]}
	reqBody := map[string]interface{}{
		"inputs": texts,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://router.huggingface.co/hf-inference/models/%s", p.model)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Check for model loading error (503)
		if resp.StatusCode == http.StatusServiceUnavailable {
			return nil, fmt.Errorf("HuggingFace model is loading, please retry in a few seconds: %s", string(body))
		}
		return nil, fmt.Errorf("HuggingFace API error (status %d): %s", resp.StatusCode, string(body))
	}

	// HuggingFace returns array of arrays: [[0.1, 0.2, ...], [0.3, 0.4, ...]]
	var embeddings [][]float32
	if err := json.Unmarshal(body, &embeddings); err != nil {
		// Try single embedding format (for single input)
		var singleEmbedding []float32
		if err2 := json.Unmarshal(body, &singleEmbedding); err2 != nil {
			return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(body))
		}
		embeddings = [][]float32{singleEmbedding}
	}

	if len(embeddings) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embeddings))
	}

	return embeddings, nil
}
