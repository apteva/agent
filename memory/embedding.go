package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
func NewEmbeddingProvider(cfg *config.MemoryConfig) (EmbeddingProvider, error) {
	provider := config.GetEmbeddingProvider(cfg)
	model := config.GetEmbeddingModel(cfg)

	switch provider {
	case "ollama":
		ollamaURL := config.GetOllamaURL(cfg)
		return NewOllamaEmbeddingProvider(ollamaURL, model), nil
	case "openai":
		fallthrough
	default:
		return NewOpenAIEmbeddingProvider(model)
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
