package memory

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"

	chromem "github.com/philippgille/chromem-go"
)

type MemoryManager struct {
	db                *sql.DB
	chromemDB         *chromem.DB
	collection        *chromem.Collection
	config            *config.MemoryConfig
	eventBus          *events.EventBus
	embeddingProvider EmbeddingProvider
	mu                sync.RWMutex
	initialized       bool
}

// MemorySource indicates where a memory originated from
type MemorySource string

const (
	SourceChat   MemorySource = "chat"   // Extracted from conversations
	SourceFile   MemorySource = "file"   // Chunked from uploaded documents
	SourceManual MemorySource = "manual" // Explicitly added by user/agent
	SourceMCP    MemorySource = "mcp"    // Synced from MCP resources
)

type Memory struct {
	ID           string                 `json:"id"`
	Content      string                 `json:"content"`
	Summary      string                 `json:"summary"`
	Embedding    []float32              `json:"-"` // Don't expose embeddings in JSON
	ThreadID     string                 `json:"thread_id"`
	MessageID    string                 `json:"message_id,omitempty"`
	Importance   float64                `json:"importance"`
	Category     string                 `json:"category"` // fact, preference, instruction, context, document
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	LastAccessed *time.Time             `json:"last_accessed,omitempty"`
	AccessCount  int                    `json:"access_count"`

	// Source tracking for RAG
	Source      MemorySource `json:"source"`                 // chat, file, manual
	SourceID    string       `json:"source_id,omitempty"`    // file_id for file source
	SourceName  string       `json:"source_name,omitempty"`  // filename for attribution

	// Chunking info for document memories
	ParentID    string `json:"parent_id,omitempty"`    // Groups chunks from same document
	ChunkIndex  int    `json:"chunk_index,omitempty"`  // Position in document
	TotalChunks int    `json:"total_chunks,omitempty"` // Total chunks in document

	// MCP resource tracking
	MCPServerID   int    `json:"mcp_server_id,omitempty"`
	MCPServerName string `json:"mcp_server_name,omitempty"`
	ResourceURI   string `json:"resource_uri,omitempty"`
}

type MemoryCandidate struct {
	Content    string                 `json:"content"`
	Summary    string                 `json:"summary,omitempty"`
	Importance float64                `json:"importance"`
	Category   string                 `json:"category"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// OpenAI API structures
type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type EmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func NewMemoryManager(db *sql.DB, cfg *config.MemoryConfig, eventBus *events.EventBus) (*MemoryManager, error) {
	m := &MemoryManager{
		db:       db,
		config:   cfg,
		eventBus: eventBus,
	}

	if cfg.Enabled {
		// Create embedding provider based on config
		provider, err := NewEmbeddingProvider(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create embedding provider: %w", err)
		}
		m.embeddingProvider = provider
		log.Printf("Memory manager using %s embedding provider", provider.Name())

		if err := m.initialize(); err != nil {
			return nil, fmt.Errorf("failed to initialize memory manager: %w", err)
		}
	}

	return m, nil
}

func (m *MemoryManager) initialize() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create ChromeM database
	m.chromemDB = chromem.NewDB()

	// Create in-memory vector collection
	embeddingFunc := func(ctx context.Context, text string) ([]float32, error) {
		return m.generateEmbedding(text)
	}

	var err error
	m.collection, err = m.chromemDB.CreateCollection("memories", nil, embeddingFunc)
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	// Load existing memories from database
	if err := m.loadMemoriesFromDB(); err != nil {
		log.Printf("Warning: Failed to load existing memories: %v", err)
		// Continue anyway - we can still work with new memories
	}

	m.initialized = true
	count := 0
	if m.collection != nil {
		count = m.collection.Count()
	}
	log.Printf("Memory manager initialized with %d memories", count)
	return nil
}

func (m *MemoryManager) loadMemoriesFromDB() error {
	rows, err := m.db.Query(`
		SELECT id, content, summary, embedding, thread_id, message_id,
		       importance, category, metadata, created_at, last_accessed, access_count,
		       source, source_id, source_name, parent_id, chunk_index, total_chunks
		FROM memories
		ORDER BY created_at DESC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var memory Memory
		var embeddingBytes []byte
		var metadataJSON string
		var lastAccessed sql.NullTime
		var threadID, messageID sql.NullString
		var source, sourceID, sourceName, parentID sql.NullString
		var chunkIndex, totalChunks sql.NullInt64

		err := rows.Scan(
			&memory.ID, &memory.Content, &memory.Summary, &embeddingBytes,
			&threadID, &messageID, &memory.Importance,
			&memory.Category, &metadataJSON, &memory.CreatedAt,
			&lastAccessed, &memory.AccessCount,
			&source, &sourceID, &sourceName, &parentID, &chunkIndex, &totalChunks,
		)
		if err != nil {
			log.Printf("Error scanning memory: %v", err)
			continue
		}

		if threadID.Valid {
			memory.ThreadID = threadID.String
		}
		if messageID.Valid {
			memory.MessageID = messageID.String
		}
		if lastAccessed.Valid {
			memory.LastAccessed = &lastAccessed.Time
		}

		// Handle source fields
		if source.Valid {
			memory.Source = MemorySource(source.String)
		} else {
			memory.Source = SourceChat // Default for existing memories
		}
		if sourceID.Valid {
			memory.SourceID = sourceID.String
		}
		if sourceName.Valid {
			memory.SourceName = sourceName.String
		}
		if parentID.Valid {
			memory.ParentID = parentID.String
		}
		if chunkIndex.Valid {
			memory.ChunkIndex = int(chunkIndex.Int64)
		}
		if totalChunks.Valid {
			memory.TotalChunks = int(totalChunks.Int64)
		}

		// Convert embedding bytes to float32 array
		if len(embeddingBytes) > 0 {
			memory.Embedding = bytesToFloat32(embeddingBytes)
		}

		// Parse metadata JSON
		if metadataJSON != "" {
			json.Unmarshal([]byte(metadataJSON), &memory.Metadata)
		}

		// Add to ChromeM collection
		if len(memory.Embedding) > 0 {
			doc := chromem.Document{
				ID:        memory.ID,
				Content:   memory.Content,
				Embedding: memory.Embedding,
				Metadata: map[string]string{
					"thread_id":   memory.ThreadID,
					"importance":  fmt.Sprintf("%.2f", memory.Importance),
					"category":    memory.Category,
					"source":      string(memory.Source),
					"source_name": memory.SourceName,
				},
			}
			m.collection.AddDocument(context.Background(), doc)
		}
	}

	return rows.Err()
}

func (m *MemoryManager) generateEmbedding(text string) ([]float32, error) {
	if m.embeddingProvider == nil {
		return nil, fmt.Errorf("embedding provider not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	return m.embeddingProvider.GenerateEmbedding(ctx, text)
}

// generateBatchEmbeddings generates embeddings for multiple texts in batch
// This is much faster than generating one at a time for large document ingestion
func (m *MemoryManager) generateBatchEmbeddings(texts []string) ([][]float32, error) {
	if m.embeddingProvider == nil {
		return nil, fmt.Errorf("embedding provider not configured")
	}

	// Use longer timeout for batch operations (100 texts * 1s each = 100s max)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	return m.embeddingProvider.GenerateBatchEmbeddings(ctx, texts)
}

func (m *MemoryManager) RetrieveRelevant(query string, threadID string, limit int) ([]*Memory, error) {
	if !m.initialized || !m.config.Enabled {
		return []*Memory{}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get collection count
	count := m.collection.Count()
	if count == 0 {
		return []*Memory{}, nil
	}

	// Adjust limit if it exceeds collection size
	if limit > count {
		limit = count
	}

	// Get minimum similarity threshold (default 0.3)
	minSimilarity := m.config.MinSimilarity
	if minSimilarity <= 0 {
		minSimilarity = 0.3
	}

	// Search in ChromeM collection
	results, err := m.collection.Query(context.Background(), query, limit, nil, nil)
	if err != nil {
		log.Printf("ChromeM query error: %v", err)
		return []*Memory{}, nil // Return empty instead of error to not break chat flow
	}

	memories := make([]*Memory, 0, len(results))
	for _, result := range results {
		// Filter by minimum similarity threshold
		if float64(result.Similarity) < minSimilarity {
			log.Printf("[Memory] Skipping result %s with similarity %.3f < %.3f", result.ID, result.Similarity, minSimilarity)
			continue
		}

		// Load full memory from database
		memory, err := m.getMemoryByID(result.ID)
		if err != nil {
			continue
		}

		// Store similarity score in metadata for logging
		if memory.Metadata == nil {
			memory.Metadata = make(map[string]interface{})
		}
		memory.Metadata["similarity"] = result.Similarity

		// Update access tracking
		go m.updateAccessTracking(result.ID)

		memories = append(memories, memory)
	}

	// Publish event
	if m.eventBus != nil {
		event := events.NewEvent(events.CategoryMemory, "memory_retrieved", events.LevelInfo).
			WithThread(threadID).
			WithData("count", len(memories)).
			WithData("query", query).
			WithData("min_similarity", minSimilarity)
		m.eventBus.Publish(event)
	}

	return memories, nil
}

func (m *MemoryManager) getMemoryByID(id string) (*Memory, error) {
	var memory Memory
	var embeddingBytes []byte
	var metadataJSON string
	var lastAccessed sql.NullTime
	var threadID, messageID sql.NullString
	var source, sourceID, sourceName, parentID sql.NullString
	var chunkIndex, totalChunks sql.NullInt64

	err := m.db.QueryRow(`
		SELECT id, content, summary, embedding, thread_id, message_id,
		       importance, category, metadata, created_at, last_accessed, access_count,
		       source, source_id, source_name, parent_id, chunk_index, total_chunks
		FROM memories WHERE id = ?
	`, id).Scan(
		&memory.ID, &memory.Content, &memory.Summary, &embeddingBytes,
		&threadID, &messageID, &memory.Importance,
		&memory.Category, &metadataJSON, &memory.CreatedAt,
		&lastAccessed, &memory.AccessCount,
		&source, &sourceID, &sourceName, &parentID, &chunkIndex, &totalChunks,
	)

	if err != nil {
		return nil, err
	}

	if threadID.Valid {
		memory.ThreadID = threadID.String
	}
	if messageID.Valid {
		memory.MessageID = messageID.String
	}
	if lastAccessed.Valid {
		memory.LastAccessed = &lastAccessed.Time
	}

	// Handle source fields
	if source.Valid {
		memory.Source = MemorySource(source.String)
	} else {
		memory.Source = SourceChat
	}
	if sourceID.Valid {
		memory.SourceID = sourceID.String
	}
	if sourceName.Valid {
		memory.SourceName = sourceName.String
	}
	if parentID.Valid {
		memory.ParentID = parentID.String
	}
	if chunkIndex.Valid {
		memory.ChunkIndex = int(chunkIndex.Int64)
	}
	if totalChunks.Valid {
		memory.TotalChunks = int(totalChunks.Int64)
	}

	if len(embeddingBytes) > 0 {
		memory.Embedding = bytesToFloat32(embeddingBytes)
	}

	if metadataJSON != "" {
		json.Unmarshal([]byte(metadataJSON), &memory.Metadata)
	}

	return &memory, nil
}

func (m *MemoryManager) updateAccessTracking(memoryID string) {
	_, err := m.db.Exec(`
		UPDATE memories
		SET last_accessed = CURRENT_TIMESTAMP,
		    access_count = access_count + 1
		WHERE id = ?
	`, memoryID)
	if err != nil {
		log.Printf("Error updating memory access tracking: %v", err)
	}
}

func (m *MemoryManager) ShouldRemember(messages []interface{}, threadID string) ([]MemoryCandidate, error) {
	if !m.initialized || !m.config.Enabled {
		return []MemoryCandidate{}, nil
	}

	// Format messages for decision model
	messagesJSON, _ := json.Marshal(messages)

	prompt := fmt.Sprintf(`Analyze this conversation and identify NEW, specific information worth remembering.

AVOID creating memories for:
- Generic greetings without specific content
- Information that is too vague or general
- Duplicate information that would likely already be known

CREATE memories for specific, concrete information:
- User preferences: "I prefer TypeScript", "I like concise explanations"
- Personal/work details: "My company is TechCorp", "I use PostgreSQL 15"
- Behavioral instructions: "Always include error handling in code examples"
- Technical facts: "My API endpoint is https://api.example.com/v1"
- Project decisions: "We decided to use Redis for session storage"
- Current work context: "Working on migrating from MongoDB to PostgreSQL"

IMPORTANT: Be specific and avoid duplicates. Instead of "User's favorite color is red" create ONE memory like "User prefers red color" in the preference category.

Categories: fact, preference, instruction, context

Return JSON array of distinct memories:
- content: specific information (avoid duplicates, be precise)
- summary: optional brief summary
- importance: 0.3-0.8 based on usefulness
- category: fact/preference/instruction/context

Conversation:
%s

Return JSON array:`, string(messagesJSON))

	// Call decision model
	completion, err := m.callCompletionAPI(prompt)
	if err != nil {
		return []MemoryCandidate{}, err
	}

	// Parse response
	var candidates []MemoryCandidate
	if err := json.Unmarshal([]byte(completion), &candidates); err != nil {
		// Try to extract JSON from response
		start := strings.Index(completion, "[")
		end := strings.LastIndex(completion, "]")
		if start >= 0 && end > start {
			jsonStr := completion[start : end+1]
			if err := json.Unmarshal([]byte(jsonStr), &candidates); err != nil {
				log.Printf("Failed to parse memory candidates: %v", err)
				return []MemoryCandidate{}, nil
			}
		}
	}

	// Filter by minimum importance
	filtered := []MemoryCandidate{}
	for _, c := range candidates {
		if c.Importance >= m.config.MinImportance {
			filtered = append(filtered, c)
		}
	}

	return filtered, nil
}

func (m *MemoryManager) callCompletionAPI(prompt string) (string, error) {
	openAIKey := getOpenAIKey()
	if openAIKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set for decision model")
	}

	req := CompletionRequest{
		Model:       m.config.DecisionModel,
		Temperature: 0.3,
		Messages: []Message{
			{Role: "system", Content: "You are a memory extraction assistant. Return only valid JSON."},
			{Role: "user", Content: prompt},
		},
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+openAIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenAI API error: %s", string(body))
	}

	var compResp CompletionResponse
	if err := json.Unmarshal(body, &compResp); err != nil {
		return "", err
	}

	if len(compResp.Choices) == 0 {
		return "", fmt.Errorf("no completion returned")
	}

	return compResp.Choices[0].Message.Content, nil
}

func (m *MemoryManager) StoreMemories(candidates []MemoryCandidate, threadID, messageID string) error {
	if !m.initialized || !m.config.Enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, candidate := range candidates {
		// Check for duplicate or very similar memories before storing
		if isDuplicate, err := m.checkForDuplicates(candidate.Content, candidate.Category); err != nil {
			log.Printf("Error checking for duplicates: %v", err)
			continue
		} else if isDuplicate {
			log.Printf("Skipping duplicate memory: %s", candidate.Content)
			continue
		}
		// Generate embedding
		embedding, err := m.generateEmbedding(candidate.Content)
		if err != nil {
			log.Printf("Failed to generate embedding: %v", err)
			continue
		}

		// Generate ID
		id := fmt.Sprintf("%d_%s", time.Now().UnixNano(), threadID[:8])

		// Convert metadata to JSON
		metadataJSON := "{}"
		if candidate.Metadata != nil {
			if data, err := json.Marshal(candidate.Metadata); err == nil {
				metadataJSON = string(data)
			}
		}

		// Store in database (source defaults to 'chat' for conversation memories)
		_, err = m.db.Exec(`
			INSERT INTO memories (id, content, summary, embedding, thread_id, message_id,
			                     importance, category, metadata, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id, candidate.Content, candidate.Summary, float32ToBytes(embedding),
			threadID, messageID, candidate.Importance, candidate.Category, metadataJSON, string(SourceChat))

		if err != nil {
			log.Printf("Failed to store memory: %v", err)
			continue
		}

		// Add to ChromeM collection
		doc := chromem.Document{
			ID:        id,
			Content:   candidate.Content,
			Embedding: embedding,
			Metadata: map[string]string{
				"thread_id":   threadID,
				"importance":  fmt.Sprintf("%.2f", candidate.Importance),
				"category":    candidate.Category,
				"source":      string(SourceChat),
				"source_name": "",
			},
		}
		m.collection.AddDocument(context.Background(), doc)

		// Publish event
		if m.eventBus != nil {
			event := events.NewEvent(events.CategoryMemory, "memory_stored", events.LevelInfo).
				WithThread(threadID).
				WithData("memory_id", id).
				WithData("category", candidate.Category).
				WithData("importance", candidate.Importance)
			m.eventBus.Publish(event)
		}
	}

	// Auto-prune if needed
	if m.config.AutoPrune {
		go m.pruneOldMemories()
	}

	return nil
}

func (m *MemoryManager) pruneOldMemories() {
	// Count total memories
	var count int
	m.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count)

	if count <= m.config.MaxMemories {
		return
	}

	// Delete least important, least accessed memories
	toDelete := count - m.config.MaxMemories
	_, err := m.db.Exec(`
		DELETE FROM memories
		WHERE id IN (
			SELECT id FROM memories
			ORDER BY importance ASC, access_count ASC, last_accessed ASC
			LIMIT ?
		)
	`, toDelete)

	if err != nil {
		log.Printf("Error pruning memories: %v", err)
	} else {
		log.Printf("Pruned %d old memories", toDelete)
		// Reload collection
		m.initialize()
	}
}

func (m *MemoryManager) GetAllMemories(threadID string) ([]*Memory, error) {
	query := `
		SELECT id, content, summary, thread_id, message_id,
		       importance, category, metadata, created_at, last_accessed, access_count,
		       source, source_id, source_name, parent_id, chunk_index, total_chunks
		FROM memories`
	args := []interface{}{}

	if threadID != "" {
		query += " WHERE thread_id = ?"
		args = append(args, threadID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memories := []*Memory{}
	for rows.Next() {
		var memory Memory
		var metadataJSON string
		var lastAccessed sql.NullTime
		var threadID, messageID sql.NullString
		var source, sourceID, sourceName, parentID sql.NullString
		var chunkIndex, totalChunks sql.NullInt64

		err := rows.Scan(
			&memory.ID, &memory.Content, &memory.Summary,
			&threadID, &messageID, &memory.Importance,
			&memory.Category, &metadataJSON, &memory.CreatedAt,
			&lastAccessed, &memory.AccessCount,
			&source, &sourceID, &sourceName, &parentID, &chunkIndex, &totalChunks,
		)
		if err != nil {
			log.Printf("Error scanning memory row: %v", err)
			continue
		}

		if threadID.Valid {
			memory.ThreadID = threadID.String
		}
		if messageID.Valid {
			memory.MessageID = messageID.String
		}
		if lastAccessed.Valid {
			memory.LastAccessed = &lastAccessed.Time
		}

		if metadataJSON != "" {
			json.Unmarshal([]byte(metadataJSON), &memory.Metadata)
		}

		// Handle source fields
		if source.Valid {
			memory.Source = MemorySource(source.String)
		}
		if sourceID.Valid {
			memory.SourceID = sourceID.String
		}
		if sourceName.Valid {
			memory.SourceName = sourceName.String
		}
		if parentID.Valid {
			memory.ParentID = parentID.String
		}
		if chunkIndex.Valid {
			memory.ChunkIndex = int(chunkIndex.Int64)
		}
		if totalChunks.Valid {
			memory.TotalChunks = int(totalChunks.Int64)
		}

		memories = append(memories, &memory)
	}

	return memories, rows.Err()
}

func (m *MemoryManager) DeleteMemory(id string) error {
	_, err := m.db.Exec("DELETE FROM memories WHERE id = ?", id)
	if err == nil {
		// Reinitialize to remove from collection
		m.initialize()
	}
	return err
}

func (m *MemoryManager) ClearAllMemories() error {
	_, err := m.db.Exec("DELETE FROM memories")
	if err == nil {
		// Reinitialize empty collection
		m.initialize()
	}
	return err
}

// DeleteMemoriesBySourceID deletes all memories associated with a source (e.g., file_id)
func (m *MemoryManager) DeleteMemoriesBySourceID(sourceID string) (int, error) {
	result, err := m.db.Exec("DELETE FROM memories WHERE source_id = ?", sourceID)
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	if count > 0 {
		// Reinitialize to remove from collection
		m.initialize()
		log.Printf("🗑️  Deleted %d memories for source: %s", count, sourceID)
	}
	return int(count), nil
}

// IngestFileResult contains the result of file ingestion
type IngestFileResult struct {
	FileID      string `json:"file_id"`
	FileName    string `json:"file_name"`
	ChunksCount int    `json:"chunks_count"`
	ParentID    string `json:"parent_id"`
}

// IngestFile processes a file and stores its content as memories
func (m *MemoryManager) IngestFile(fileID, fileName, mimeType string, data []byte) (*IngestFileResult, error) {
	log.Printf("[Ingest] Starting file ingestion: %s (type: %s, size: %d bytes)", fileName, mimeType, len(data))

	if !m.initialized || !m.config.Enabled {
		log.Printf("[Ingest] ❌ Memory manager not initialized or disabled")
		return nil, fmt.Errorf("memory manager not initialized or disabled")
	}

	// Extract text from file
	extractor := NewTextExtractor()
	if !extractor.CanExtract(mimeType) {
		log.Printf("[Ingest] ❌ Unsupported file type: %s", mimeType)
		return nil, fmt.Errorf("unsupported file type: %s", mimeType)
	}

	result, err := extractor.Extract(data, mimeType)
	if err != nil {
		log.Printf("[Ingest] ❌ Text extraction failed: %v", err)
		return nil, fmt.Errorf("failed to extract text: %w", err)
	}

	log.Printf("[Ingest] ✓ Extracted %d chars from %s", len(result.Content), fileName)

	if strings.TrimSpace(result.Content) == "" {
		log.Printf("[Ingest] ❌ No text content extracted")
		return nil, fmt.Errorf("no text content extracted from file")
	}

	// Chunk the content
	chunkSize := 1000
	chunkOverlap := 200
	if m.config.ChunkSize > 0 {
		chunkSize = m.config.ChunkSize
	}
	if m.config.ChunkOverlap > 0 {
		chunkOverlap = m.config.ChunkOverlap
	}

	log.Printf("[Ingest] Chunking with size=%d, overlap=%d", chunkSize, chunkOverlap)

	chunker := NewDocumentChunker(chunkSize, chunkOverlap)
	chunks := chunker.ChunkText(result.Content)

	if len(chunks) == 0 {
		log.Printf("[Ingest] ❌ No chunks generated")
		return nil, fmt.Errorf("no chunks generated from file")
	}

	log.Printf("[Ingest] ✓ Generated %d chunks", len(chunks))

	// Generate parent ID for this document
	parentID := fmt.Sprintf("doc_%s", fileID)

	// Get importance from config
	importance := 0.5
	if m.config.DocumentImportance > 0 {
		importance = m.config.DocumentImportance
	}

	// Use filename as source name, or title if extracted
	sourceName := fileName
	if result.Title != "" {
		sourceName = result.Title
	}

	// Generate all embeddings in batch (much faster than one-by-one)
	log.Printf("[Ingest] Generating embeddings for %d chunks in batch...", len(chunks))
	chunkTexts := make([]string, len(chunks))
	for i, chunk := range chunks {
		chunkTexts[i] = chunk.Content
	}

	embeddings, err := m.generateBatchEmbeddings(chunkTexts)
	if err != nil {
		log.Printf("[Ingest] ❌ Batch embedding generation failed: %v", err)
		return nil, fmt.Errorf("failed to generate embeddings: %w", err)
	}
	log.Printf("[Ingest] ✓ Generated %d embeddings", len(embeddings))

	m.mu.Lock()
	defer m.mu.Unlock()

	storedCount := 0
	for i, chunk := range chunks {
		embedding := embeddings[i]
		if embedding == nil {
			log.Printf("Skipping chunk %d: no embedding", i)
			continue
		}

		// Generate unique ID
		fileIDPrefix := fileID
		if len(fileIDPrefix) > 8 {
			fileIDPrefix = fileIDPrefix[:8]
		}
		id := fmt.Sprintf("%d_%s_%d", time.Now().UnixNano(), fileIDPrefix, i)

		// Prepare metadata
		metadata := map[string]interface{}{
			"chunk_start": chunk.StartPos,
			"chunk_end":   chunk.EndPos,
		}
		if result.Title != "" {
			metadata["title"] = result.Title
		}
		for k, v := range result.Metadata {
			metadata[k] = v
		}
		metadataJSON, _ := json.Marshal(metadata)

		// Store in database
		_, err = m.db.Exec(`
			INSERT INTO memories (id, content, summary, embedding, importance, category, metadata,
			                     source, source_id, source_name, parent_id, chunk_index, total_chunks)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id, chunk.Content, "", float32ToBytes(embedding), importance, "document", string(metadataJSON),
			string(SourceFile), fileID, sourceName, parentID, i, len(chunks))

		if err != nil {
			log.Printf("Failed to store memory chunk %d: %v", i, err)
			continue
		}

		// Add to ChromeM collection
		doc := chromem.Document{
			ID:        id,
			Content:   chunk.Content,
			Embedding: embedding,
			Metadata: map[string]string{
				"importance":   fmt.Sprintf("%.2f", importance),
				"category":     "document",
				"source":       string(SourceFile),
				"source_name":  sourceName,
				"parent_id":    parentID,
				"chunk_index":  fmt.Sprintf("%d", i),
				"total_chunks": fmt.Sprintf("%d", len(chunks)),
			},
		}
		m.collection.AddDocument(context.Background(), doc)

		storedCount++

		// Publish event
		if m.eventBus != nil {
			event := events.NewEvent(events.CategoryMemory, "memory_stored", events.LevelInfo).
				WithData("memory_id", id).
				WithData("category", "document").
				WithData("source", string(SourceFile)).
				WithData("source_name", sourceName).
				WithData("chunk_index", i).
				WithData("total_chunks", len(chunks))
			m.eventBus.Publish(event)
		}
	}

	log.Printf("📚 Ingested file %s: %d chunks stored from %s", fileID, storedCount, fileName)

	// Emit file_ingested event
	ingestEvent := events.NewEvent(events.CategoryFile, events.TypeFileIngested, events.LevelInfo).
		WithData("file_id", fileID).
		WithData("filename", fileName).
		WithData("mime_type", mimeType).
		WithData("chunks_created", storedCount).
		WithData("total_chars", len(result.Content)).
		WithData("embeddings_generated", storedCount)
	events.GetEventBus().Publish(ingestEvent)

	return &IngestFileResult{
		FileID:      fileID,
		FileName:    fileName,
		ChunksCount: storedCount,
		ParentID:    parentID,
	}, nil
}

// DeleteFileMemories removes all memories associated with a file
func (m *MemoryManager) DeleteFileMemories(fileID string) error {
	if !m.initialized {
		return fmt.Errorf("memory manager not initialized")
	}

	parentID := fmt.Sprintf("doc_%s", fileID)

	// Delete from database
	result, err := m.db.Exec("DELETE FROM memories WHERE source_id = ? OR parent_id = ?", fileID, parentID)
	if err != nil {
		return fmt.Errorf("failed to delete file memories: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("🗑️ Deleted %d memories for file %s", rows, fileID)
		// Reinitialize collection to remove from vector store
		m.initialize()
	}

	return nil
}

// IngestMCPResource processes an MCP resource and stores its content as memories
func (m *MemoryManager) IngestMCPResource(serverID int, serverName, uri, name, content, mimeType, contentChecksum string) (*IngestFileResult, error) {
	if !m.initialized || !m.config.Enabled {
		return nil, fmt.Errorf("memory manager not initialized or disabled")
	}

	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("empty content")
	}

	// Preprocess JSON content - convert to semantic prose for better embeddings
	processedContent, wasJSON := PreprocessContent(content, mimeType, name)
	if wasJSON {
		log.Printf("[MCP Ingest] Converted JSON to prose (%d -> %d chars)", len(content), len(processedContent))
	}

	// Chunk the content
	chunkSize := 1000
	chunkOverlap := 200
	if m.config.ChunkSize > 0 {
		chunkSize = m.config.ChunkSize
	}
	if m.config.ChunkOverlap > 0 {
		chunkOverlap = m.config.ChunkOverlap
	}

	chunker := NewDocumentChunker(chunkSize, chunkOverlap)
	chunks := chunker.ChunkText(processedContent)

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks generated from content")
	}

	// Generate parent ID for this resource
	parentID := fmt.Sprintf("mcp_%s", uri)

	// Get importance from config
	importance := 0.5
	if m.config.DocumentImportance > 0 {
		importance = m.config.DocumentImportance
	}

	// Generate all embeddings in batch
	chunkTexts := make([]string, len(chunks))
	for i, chunk := range chunks {
		chunkTexts[i] = chunk.Content
	}

	embeddings, err := m.generateBatchEmbeddings(chunkTexts)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embeddings: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	storedCount := 0
	for i, chunk := range chunks {
		embedding := embeddings[i]
		if embedding == nil {
			log.Printf("Skipping MCP chunk %d: no embedding", i)
			continue
		}

		// Generate unique ID
		id := fmt.Sprintf("%d_mcp_%d_%d", time.Now().UnixNano(), serverID, i)

		// Prepare metadata
		metadata := map[string]interface{}{
			"chunk_start":     chunk.StartPos,
			"chunk_end":       chunk.EndPos,
			"mcp_server_id":   serverID,
			"mcp_server_name": serverName,
			"resource_uri":    uri,
		}
		metadataJSON, _ := json.Marshal(metadata)

		// Store in database
		_, err = m.db.Exec(`
			INSERT INTO memories (id, content, summary, embedding, importance, category, metadata,
			                     source, source_id, source_name, parent_id, chunk_index, total_chunks,
			                     mcp_server_id, mcp_server_name, resource_uri, content_checksum)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id, chunk.Content, "", float32ToBytes(embedding), importance, "document", string(metadataJSON),
			string(SourceMCP), uri, name, parentID, i, len(chunks),
			serverID, serverName, uri, contentChecksum)

		if err != nil {
			log.Printf("Failed to store MCP memory chunk %d: %v", i, err)
			continue
		}

		// Add to ChromeM collection
		doc := chromem.Document{
			ID:        id,
			Content:   chunk.Content,
			Embedding: embedding,
			Metadata: map[string]string{
				"importance":      fmt.Sprintf("%.2f", importance),
				"category":        "document",
				"source":          string(SourceMCP),
				"source_name":     name,
				"parent_id":       parentID,
				"chunk_index":     fmt.Sprintf("%d", i),
				"total_chunks":    fmt.Sprintf("%d", len(chunks)),
				"mcp_server_name": serverName,
				"resource_uri":    uri,
			},
		}
		m.collection.AddDocument(context.Background(), doc)

		storedCount++

		// Publish event
		if m.eventBus != nil {
			event := events.NewEvent(events.CategoryMemory, "memory_stored", events.LevelInfo).
				WithData("memory_id", id).
				WithData("category", "document").
				WithData("source", string(SourceMCP)).
				WithData("source_name", name).
				WithData("mcp_server_name", serverName).
				WithData("resource_uri", uri).
				WithData("chunk_index", i).
				WithData("total_chunks", len(chunks))
			m.eventBus.Publish(event)
		}
	}

	log.Printf("📚 Ingested MCP resource %s: %d chunks stored from %s", uri, storedCount, serverName)

	return &IngestFileResult{
		FileID:      uri,
		FileName:    name,
		ChunksCount: storedCount,
		ParentID:    parentID,
	}, nil
}

// DeleteMCPResource removes all memories associated with an MCP resource
func (m *MemoryManager) DeleteMCPResource(uri string) (int, error) {
	if !m.initialized {
		return 0, fmt.Errorf("memory manager not initialized")
	}

	parentID := fmt.Sprintf("mcp_%s", uri)

	// Delete from database
	result, err := m.db.Exec("DELETE FROM memories WHERE resource_uri = ? OR parent_id = ?", uri, parentID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete MCP resource memories: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("🗑️ Deleted %d memories for MCP resource %s", rows, uri)
		// Reinitialize collection to remove from vector store
		m.initialize()
	}

	return int(rows), nil
}

// LoadMCPChecksums loads all MCP resource checksums from the database
// Returns a map of resource_uri -> content_checksum for efficient lookup
func (m *MemoryManager) LoadMCPChecksums() (map[string]string, error) {
	if !m.initialized {
		return nil, fmt.Errorf("memory manager not initialized")
	}

	checksums := make(map[string]string)

	rows, err := m.db.Query(`
		SELECT DISTINCT resource_uri, content_checksum
		FROM memories
		WHERE resource_uri IS NOT NULL
		  AND resource_uri != ''
		  AND content_checksum IS NOT NULL
		  AND content_checksum != ''
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query MCP checksums: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var uri, checksum string
		if err := rows.Scan(&uri, &checksum); err != nil {
			log.Printf("⚠️ Failed to scan MCP checksum row: %v", err)
			continue
		}
		checksums[uri] = checksum
	}

	log.Printf("📚 Loaded %d MCP resource checksums from database", len(checksums))
	return checksums, nil
}

// Helper functions
func float32ToBytes(floats []float32) []byte {
	buf := new(bytes.Buffer)
	for _, f := range floats {
		binary.Write(buf, binary.LittleEndian, f)
	}
	return buf.Bytes()
}

func bytesToFloat32(data []byte) []float32 {
	floats := make([]float32, len(data)/4)
	buf := bytes.NewReader(data)
	for i := range floats {
		var f float32
		binary.Read(buf, binary.LittleEndian, &f)
		floats[i] = f
	}
	return floats
}

func FormatMemoryContext(memories []*Memory) string {
	if len(memories) == 0 {
		return ""
	}

	// Group memories by source
	bySource := make(map[MemorySource][]*Memory)
	for _, mem := range memories {
		source := mem.Source
		if source == "" {
			source = SourceChat
		}
		bySource[source] = append(bySource[source], mem)
	}

	var lines []string
	lines = append(lines, "[KNOWLEDGE CONTEXT - Use when relevant to the query]")

	// Format chat memories
	if chatMems := bySource[SourceChat]; len(chatMems) > 0 {
		lines = append(lines, "\nFrom conversations:")
		for _, mem := range chatMems {
			lines = append(lines, fmt.Sprintf("  • %s", mem.Content))
		}
	}

	// Format file/document memories
	if fileMems := bySource[SourceFile]; len(fileMems) > 0 {
		lines = append(lines, "\nFrom documents:")
		for _, mem := range fileMems {
			if mem.SourceName != "" {
				if mem.TotalChunks > 1 {
					lines = append(lines, fmt.Sprintf("  [%s, part %d/%d] %s", mem.SourceName, mem.ChunkIndex+1, mem.TotalChunks, truncateContent(mem.Content, 500)))
				} else {
					lines = append(lines, fmt.Sprintf("  [%s] %s", mem.SourceName, truncateContent(mem.Content, 500)))
				}
			} else {
				lines = append(lines, fmt.Sprintf("  • %s", truncateContent(mem.Content, 500)))
			}
		}
	}

	// Format manual memories
	if manualMems := bySource[SourceManual]; len(manualMems) > 0 {
		lines = append(lines, "\nFrom notes:")
		for _, mem := range manualMems {
			lines = append(lines, fmt.Sprintf("  • %s", mem.Content))
		}
	}

	// Format MCP resource memories
	if mcpMems := bySource[SourceMCP]; len(mcpMems) > 0 {
		lines = append(lines, "\nFrom MCP resources:")
		for _, mem := range mcpMems {
			serverName := mem.MCPServerName
			if serverName == "" {
				serverName = "mcp"
			}
			if mem.TotalChunks > 1 {
				lines = append(lines, fmt.Sprintf("  [%s: %s, part %d/%d] %s", serverName, mem.SourceName, mem.ChunkIndex+1, mem.TotalChunks, truncateContent(mem.Content, 500)))
			} else {
				lines = append(lines, fmt.Sprintf("  [%s: %s] %s", serverName, mem.SourceName, truncateContent(mem.Content, 500)))
			}
		}
	}

	lines = append(lines, "\n[END CONTEXT]")
	return strings.Join(lines, "\n")
}

// truncateContent truncates content to maxLen characters
func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}

// checkForDuplicates checks if a similar memory already exists
func (m *MemoryManager) checkForDuplicates(content, category string) (bool, error) {
	// First check exact content matches in the same category
	rows, err := m.db.Query(`
		SELECT content FROM memories
		WHERE category = ? AND (
			content = ? OR
			LOWER(content) = LOWER(?) OR
			LENGTH(content) - LENGTH(REPLACE(LOWER(content), LOWER(?), '')) > LENGTH(?) * 0.8
		)
		LIMIT 5
	`, category, content, content, content, content)

	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var existingContent string
		if err := rows.Scan(&existingContent); err != nil {
			continue
		}

		// Check for semantic similarity
		similarity := calculateStringSimilarity(content, existingContent)
		if similarity > 0.8 { // 80% similar
			return true, nil
		}
	}

	// Also check using vector similarity if we have embeddings
	if m.collection.Count() > 0 {
		// Search for very similar content
		results, err := m.collection.Query(context.Background(), content, 3, nil, nil)
		if err == nil {
			for _, result := range results {
				// If we find a very similar memory, check if it's the same category
				existingMemory, err := m.getMemoryByID(result.ID)
				if err == nil && existingMemory.Category == category {
					similarity := calculateStringSimilarity(content, existingMemory.Content)
					if similarity > 0.85 { // Very high similarity threshold
						return true, nil
					}
				}
			}
		}
	}

	return false, nil
}

// calculateStringSimilarity calculates similarity between two strings
func calculateStringSimilarity(s1, s2 string) float64 {
	s1 = strings.ToLower(strings.TrimSpace(s1))
	s2 = strings.ToLower(strings.TrimSpace(s2))

	if s1 == s2 {
		return 1.0
	}

	// Simple Jaccard similarity using words
	words1 := strings.Fields(s1)
	words2 := strings.Fields(s2)

	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	// Create sets
	set1 := make(map[string]bool)
	set2 := make(map[string]bool)

	for _, word := range words1 {
		set1[word] = true
	}
	for _, word := range words2 {
		set2[word] = true
	}

	// Calculate intersection and union
	intersection := 0
	union := make(map[string]bool)

	for word := range set1 {
		union[word] = true
		if set2[word] {
			intersection++
		}
	}
	for word := range set2 {
		union[word] = true
	}

	if len(union) == 0 {
		return 0.0
	}

	return float64(intersection) / float64(len(union))
}