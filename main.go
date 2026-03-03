package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apteva/agent/a2a"
	"github.com/apteva/agent/agents"
	"github.com/apteva/agent/config"
	"github.com/apteva/agent/discovery"
	"github.com/apteva/agent/events"
	"github.com/apteva/agent/filesystem"
	handlerConfig "github.com/apteva/agent/handlers/config"
	"github.com/apteva/agent/handlers/memories"
	handlerMcp "github.com/apteva/agent/handlers/mcp"
	"github.com/apteva/agent/handlers/activity"
	"github.com/apteva/agent/handlers/observability"
	handlerSkills "github.com/apteva/agent/handlers/skills"
	"github.com/apteva/agent/handlers/tasks"
	"github.com/apteva/agent/handlers/threads"
	"github.com/apteva/agent/mcp"
	"github.com/apteva/agent/skills"
	"github.com/apteva/agent/memory"
	"github.com/apteva/agent/operator"
	"github.com/apteva/agent/pdf"
	"github.com/apteva/agent/providers"
	"github.com/apteva/agent/realtime"
	"github.com/apteva/agent/reflection"
	"github.com/apteva/agent/scheduler"
	"github.com/apteva/agent/stream"
	"github.com/apteva/agent/tools"
	"github.com/apteva/agent/web"

	_ "github.com/mattn/go-sqlite3"
)

// Version info - set at build time via ldflags
// Example: go build -ldflags="-X main.Version=1.0.0 -X main.BuildTime=2024-01-01T00:00:00Z"
var (
	Version   = ""  // Set via -ldflags, empty means use VERSION file
	BuildTime = ""  // Set via -ldflags
)

func getVersion() string {
	// If Version was set at build time, use it
	if Version != "" {
		return Version
	}
	// Otherwise read from VERSION file (development mode)
	data, err := os.ReadFile("VERSION")
	if err != nil {
		return "1.0.0" // fallback
	}
	return strings.TrimSpace(string(data))
}


type HealthResponse struct {
	Status  string `json:"status"`
	Uptime  string `json:"uptime"`
	Version string `json:"version"`
}

type ChatRequest struct {
	Message       interface{} `json:"message"`                   // Can be string or []ContentBlock
	ThreadID      *string     `json:"thread_id,omitempty"`
	Stream        *bool       `json:"stream,omitempty"`          // If false, return complete response (default: true)
	RawMode       *bool       `json:"raw_mode,omitempty"`        // If true, bypass processor and stream raw LLM chunks
	System        interface{} `json:"system,omitempty"`          // Additional system context (string or []string)
	Source        *string     `json:"source,omitempty"`          // Trace source: chat, webhook, task, subtask, agent_call (optional, auto-detected if not provided)
	ParentTraceID *string     `json:"parent_trace_id,omitempty"` // Parent trace ID for linking subtask executions
}

type ChatResponse struct {
	ThreadID string `json:"thread_id"`
}

type ContentBlock struct {
	Type      string                 `json:"type"`                    // "text", "image", "document", "tool_use", "tool_result"
	Text      string                 `json:"text,omitempty"`          // for text blocks

	// Image/Document fields
	Source    *ContentSource         `json:"source,omitempty"`        // for image and document blocks

	// Tool fields
	ID        string                 `json:"id,omitempty"`            // for tool_use
	Name      string                 `json:"name,omitempty"`          // for tool_use
	Input     map[string]interface{} `json:"input,omitempty"`         // for tool_use
	ToolUseID string                 `json:"tool_use_id,omitempty"`   // for tool_result
	Content   interface{}            `json:"content,omitempty"`       // for tool_result (can be string or object)
	IsError   bool                   `json:"is_error,omitempty"`      // for tool_result
}

type ContentSource struct {
	Type      string `json:"type"`                     // "base64", "url", "file"
	MediaType string `json:"media_type,omitempty"`     // "image/jpeg", "application/pdf", etc.
	Data      string `json:"data,omitempty"`           // For base64
	URL       string `json:"url,omitempty"`            // For URL
	FileID    string `json:"file_id,omitempty"`        // For file reference
}

// Thread moved to threads package
type Thread = threads.Thread

// Message moved to threads package
type Message = threads.Message

// FileManagerAdapter adapts FileManager to stream.FileProcessor interface
type FileManagerAdapter struct {
	fm *filesystem.FileManager
}

func (a *FileManagerAdapter) ProcessContentBlocks(blocks []stream.ContentBlock, threadID, messageID, source string) ([]stream.ContentBlock, error) {
	if a.fm == nil {
		return blocks, nil
	}
	// Convert stream.ContentBlock to filesystem.ContentBlock
	fsBlocks := make([]filesystem.ContentBlock, len(blocks))
	for i, block := range blocks {
		fsBlocks[i] = filesystem.ContentBlock{
			Type:      block.Type,
			Text:      block.Text,
			ID:        block.ID,
			Name:      block.Name,
			Input:     block.Input,
			ToolUseID: block.ToolUseID,
			Content:   block.Content,
			IsError:   block.IsError,
		}
		if block.Source != nil {
			fsBlocks[i].Source = &filesystem.ContentSource{
				Type:      block.Source.Type,
				MediaType: block.Source.MediaType,
				Data:      block.Source.Data,
				URL:       block.Source.URL,
				FileID:    block.Source.FileID,
			}
		}
	}

	// Process with FileManager
	processedBlocks, err := a.fm.ProcessContentBlocks(fsBlocks, threadID, messageID, source)
	if err != nil {
		return nil, err
	}

	// Convert back to stream.ContentBlock, preserving tool_use fields
	streamBlocks := make([]stream.ContentBlock, len(processedBlocks))
	for i, block := range processedBlocks {
		streamBlocks[i] = stream.ContentBlock{
			Type: block.Type,
			Text: block.Text,
		}
		if block.Source != nil {
			streamBlocks[i].Source = &stream.ContentSource{
				Type:      block.Source.Type,
				MediaType: block.Source.MediaType,
				Data:      block.Source.Data,
				URL:       block.Source.URL,
				FileID:    block.Source.FileID,
			}
		}
		// Preserve tool_use fields from original block
		if i < len(blocks) {
			streamBlocks[i].ID = blocks[i].ID
			streamBlocks[i].Name = blocks[i].Name
			streamBlocks[i].Input = blocks[i].Input
			streamBlocks[i].ToolUseID = blocks[i].ToolUseID
		}
	}

	return streamBlocks, nil
}

func (a *FileManagerAdapter) ExpandFileReferences(blocks []stream.ContentBlock) ([]stream.ContentBlock, error) {
	if a.fm == nil {
		return blocks, nil
	}
	// Convert stream.ContentBlock to filesystem.ContentBlock
	fsBlocks := make([]filesystem.ContentBlock, len(blocks))
	for i, block := range blocks {
		fsBlocks[i] = filesystem.ContentBlock{
			Type:      block.Type,
			Text:      block.Text,
			ID:        block.ID,
			Name:      block.Name,
			Input:     block.Input,
			ToolUseID: block.ToolUseID,
			Content:   block.Content,
			IsError:   block.IsError,
		}
		if block.Source != nil {
			fsBlocks[i].Source = &filesystem.ContentSource{
				Type:      block.Source.Type,
				MediaType: block.Source.MediaType,
				Data:      block.Source.Data,
				URL:       block.Source.URL,
				FileID:    block.Source.FileID,
			}
		}
	}

	// Expand with FileManager
	expandedBlocks, err := a.fm.ExpandFileReferences(fsBlocks)
	if err != nil {
		return nil, err
	}

	// Convert back to stream.ContentBlock
	streamBlocks := make([]stream.ContentBlock, len(expandedBlocks))
	for i, block := range expandedBlocks {
		streamBlocks[i] = stream.ContentBlock{
			Type: block.Type,
			Text: block.Text,
		}
		if block.Source != nil {
			streamBlocks[i].Source = &stream.ContentSource{
				Type:      block.Source.Type,
				MediaType: block.Source.MediaType,
				Data:      block.Source.Data,
				URL:       block.Source.URL,
				FileID:    block.Source.FileID,
			}
		}
		// Preserve tool fields from original block
		if i < len(blocks) {
			streamBlocks[i].ID = blocks[i].ID
			streamBlocks[i].Name = blocks[i].Name
			streamBlocks[i].Input = blocks[i].Input
			streamBlocks[i].ToolUseID = blocks[i].ToolUseID
		}
	}

	return streamBlocks, nil
}

func (a *FileManagerAdapter) IsEnabled() bool {
	if a.fm == nil {
		return false
	}
	return a.fm.IsEnabled()
}

func (a *FileManagerAdapter) ExtractImagesFromToolResult(result interface{}, threadID, source string) (interface{}, []string, error) {
	if a.fm == nil {
		return result, nil, nil
	}
	return a.fm.ExtractImagesFromToolResult(result, threadID, source)
}

// ResponseCapture captures SSE events to build a complete response
type ResponseCapture struct {
	http.ResponseWriter
	textChunks          []string
	contentBlocks       []map[string]interface{}
	inputTokens         int
	outputTokens        int
	cacheCreationTokens int
	cacheReadTokens     int
	reasoningTokens     int
}

func (rc *ResponseCapture) Write(p []byte) (int, error) {
	// Parse SSE events and capture all relevant events
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			dataJSON := strings.TrimPrefix(line, "data: ")
			var event map[string]interface{}
			if err := json.Unmarshal([]byte(dataJSON), &event); err == nil {
				eventType, _ := event["type"].(string)

				switch eventType {
				case "content":
					// Capture text content for backward-compatible response field
					if text, ok := event["content"].(string); ok {
						rc.textChunks = append(rc.textChunks, text)
					}
					// Add to content blocks
					rc.contentBlocks = append(rc.contentBlocks, map[string]interface{}{
						"type": "text",
						"text": event["content"],
					})

				case "tool_use":
					// Add tool use block
					block := map[string]interface{}{
						"type": "tool_use",
						"id":   event["tool_id"],
						"name": event["tool_name"],
					}
					// Always include input field (even if empty)
					if toolInput, ok := event["tool_input"].(map[string]interface{}); ok && toolInput != nil {
						block["input"] = toolInput
					} else {
						block["input"] = map[string]interface{}{}
					}
					rc.contentBlocks = append(rc.contentBlocks, block)

				case "tool_result":
					// Add tool result block
					block := map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": event["tool_id"],
					}
					// Get content from event
					if content := event["content"]; content != nil {
						block["content"] = content
					} else {
						block["content"] = ""
					}
					rc.contentBlocks = append(rc.contentBlocks, block)

				case "usage":
					// Capture token usage
					if v, ok := event["input_tokens"].(float64); ok {
						rc.inputTokens = int(v)
					}
					if v, ok := event["output_tokens"].(float64); ok {
						rc.outputTokens = int(v)
					}
					if v, ok := event["cache_creation_tokens"].(float64); ok {
						rc.cacheCreationTokens = int(v)
					}
					if v, ok := event["cache_read_tokens"].(float64); ok {
						rc.cacheReadTokens = int(v)
					}
					if v, ok := event["reasoning_tokens"].(float64); ok {
						rc.reasoningTokens = int(v)
					}
				}
			}
		}
	}
	// Don't actually write to the underlying ResponseWriter
	return len(p), nil
}

// Flush implements http.Flusher interface (required for streaming)
func (rc *ResponseCapture) Flush() {
	// No-op for capture mode - we're just collecting data
	// If the underlying writer supports flushing, we could call it, but not needed here
}

func (rc *ResponseCapture) ExtractCompleteResponse() string {
	return strings.Join(rc.textChunks, "")
}

func (rc *ResponseCapture) ExtractContentBlocks() []map[string]interface{} {
	return rc.contentBlocks
}

func (rc *ResponseCapture) ExtractUsage() map[string]interface{} {
	if rc.inputTokens == 0 && rc.outputTokens == 0 {
		return nil
	}
	usage := map[string]interface{}{
		"input_tokens":  rc.inputTokens,
		"output_tokens": rc.outputTokens,
		"total_tokens":  rc.inputTokens + rc.outputTokens,
	}
	if rc.cacheCreationTokens > 0 {
		usage["cache_creation_tokens"] = rc.cacheCreationTokens
	}
	if rc.cacheReadTokens > 0 {
		usage["cache_read_tokens"] = rc.cacheReadTokens
	}
	if rc.reasoningTokens > 0 {
		usage["reasoning_tokens"] = rc.reasoningTokens
	}
	return usage
}




var (
	startTime           = time.Now()
	anthropicProvider   *providers.AnthropicProvider
	openaiProvider      *providers.OpenAIProvider
	groqProvider        *providers.GroqProvider
	geminiProvider      *providers.GeminiProvider
	fireworksProvider   *providers.FireworksProvider
	xaiProvider         *providers.XAIProvider
	zaiProvider         *providers.ZAIProvider
	novitaProvider      *providers.NovitaProvider
	veniceProvider      *providers.VeniceProvider
	moonshotProvider    *providers.MoonshotProvider
	togetherProvider    *providers.TogetherProvider
	mistralProvider     *providers.MistralProvider
	ollamaProvider      *providers.OllamaProvider
	cerebrasProvider    *providers.CerebrasProvider
	db                  *sql.DB
	messageSaver        threads.MessageSaver
	memoryManager       *memory.MemoryManager
	agentScheduler      *scheduler.Scheduler
	agentReflection     *reflection.ReflectionScheduler
	discoveryService    discovery.DiscoveryService
	agentClient         *agents.AgentClient
	fileManager         *filesystem.FileManager
	cleanupJob          *filesystem.CleanupJob
	requestTracker      *agents.RequestTracker
	telemetrySubscriber *events.TelemetrySubscriber
	telemetrySubID      string
)

func loadEnv() {
	// Try multiple locations for .env file
	envPaths := []string{
		".env",                    // Current directory
		"../agent/.env",           // Parent/agent directory
	}

	// Also try executable's directory
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		envPaths = append(envPaths, filepath.Join(execDir, ".env"))
	}

	var loadedFrom string
	var file *os.File
	var err error

	for _, path := range envPaths {
		file, err = os.Open(path)
		if err == nil {
			loadedFrom = path
			break
		}
	}

	if file == nil {
		log.Printf("Warning: .env file not found (searched: %v)", envPaths)
		return
	}
	defer file.Close()

	var loadedKeys []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Only set if not already set (allow env override)
			if os.Getenv(key) == "" {
				os.Setenv(key, value)
				loadedKeys = append(loadedKeys, key)
			}
		}
	}

	log.Printf("Loaded .env from %s (keys: %v)", loadedFrom, loadedKeys)
}

func initDB() {
	var err error
	dbConnStart := time.Now()

	// Determine database path from DATA_DIR environment variable
	dbPath := "./app.db"
	if dataDir := os.Getenv("DATA_DIR"); dataDir != "" {
		dbPath = dataDir + "/app.db"
		// Ensure directory exists
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			log.Printf("Warning: Failed to create data directory %s: %v", dataDir, err)
		}
	}
	log.Printf("Using database path: %s", dbPath)

	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}

	// Publish database connection event
	eventBus := events.GetEventBus()
	connEvent := events.NewEvent(events.CategoryDatabase, "db_connection", events.LevelInfo).
		WithData("database", "sqlite3").
		WithData("path", dbPath).
		WithDuration(dbConnStart)
	eventBus.Publish(connEvent)

	// Create threads table
	createThreadsTable := `
	CREATE TABLE IF NOT EXISTS threads (
		id TEXT PRIMARY KEY,
		title TEXT,
		type TEXT DEFAULT 'chat',
		parent_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		metadata TEXT DEFAULT '{}'
	);`

	tableCreateStart := time.Now()
	result, err := db.Exec(createThreadsTable)
	if err != nil {
		log.Fatal("Failed to create threads table:", err)
	}
	rowsAffected, _ := result.RowsAffected()

	// Publish table creation event
	createEvent := events.NewEvent(events.CategoryDatabase, "db_schema", events.LevelInfo).
		WithData("operation", "CREATE TABLE IF NOT EXISTS").
		WithData("table", "threads").
		WithData("rows_affected", rowsAffected).
		WithDuration(tableCreateStart)
	eventBus.Publish(createEvent)

	// Create messages table
	createMessagesTable := `
	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		thread_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		model TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		metadata TEXT DEFAULT '{}',
		FOREIGN KEY (thread_id) REFERENCES threads (id) ON DELETE CASCADE
	);`

	tableCreateStart = time.Now()
	result, err = db.Exec(createMessagesTable)
	if err != nil {
		log.Fatal("Failed to create messages table:", err)
	}
	rowsAffected, _ = result.RowsAffected()

	// Publish table creation event
	createEvent = events.NewEvent(events.CategoryDatabase, "db_schema", events.LevelInfo).
		WithData("operation", "CREATE TABLE IF NOT EXISTS").
		WithData("table", "messages").
		WithData("rows_affected", rowsAffected).
		WithDuration(tableCreateStart)
	eventBus.Publish(createEvent)

	// Create tasks table
	createTasksTable := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		thread_id TEXT,
		title TEXT NOT NULL,
		description TEXT,
		type TEXT NOT NULL DEFAULT 'once' CHECK (type IN ('once', 'recurring')),
		status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
		priority INTEGER DEFAULT 5 CHECK (priority >= 1 AND priority <= 10),
		execute_at DATETIME,
		executed_at DATETIME,
		recurrence TEXT,
		next_run DATETIME,
		result TEXT,
		source TEXT DEFAULT 'user',
		delegator_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (thread_id) REFERENCES threads (id) ON DELETE SET NULL
	);`

	tableCreateStart = time.Now()
	result, err = db.Exec(createTasksTable)
	if err != nil {
		log.Fatal("Failed to create tasks table:", err)
	}
	rowsAffected, _ = result.RowsAffected()

	// Publish table creation event
	createEvent = events.NewEvent(events.CategoryDatabase, "db_schema", events.LevelInfo).
		WithData("operation", "CREATE TABLE IF NOT EXISTS").
		WithData("table", "tasks").
		WithData("rows_affected", rowsAffected).
		WithDuration(tableCreateStart)
	eventBus.Publish(createEvent)

	// Create indexes for tasks table
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)",
		"CREATE INDEX IF NOT EXISTS idx_tasks_execute_at ON tasks(execute_at)",
		"CREATE INDEX IF NOT EXISTS idx_tasks_next_run ON tasks(next_run)",
		"CREATE INDEX IF NOT EXISTS idx_tasks_thread ON tasks(thread_id)",
	}

	for _, indexSQL := range indexes {
		if _, err := db.Exec(indexSQL); err != nil {
			log.Printf("Warning: Failed to create index: %v", err)
		}
	}

	// Create memories table
	createMemoriesTable := `
	CREATE TABLE IF NOT EXISTS memories (
		id TEXT PRIMARY KEY,
		content TEXT NOT NULL,
		summary TEXT,
		embedding BLOB,
		thread_id TEXT,
		message_id TEXT,
		importance REAL DEFAULT 0.5,
		category TEXT,
		metadata TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_accessed TIMESTAMP,
		access_count INTEGER DEFAULT 0,
		source TEXT DEFAULT 'chat',
		source_id TEXT,
		source_name TEXT,
		parent_id TEXT,
		chunk_index INTEGER,
		total_chunks INTEGER,
		FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
		FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
	);`

	tableCreateStart = time.Now()
	result, err = db.Exec(createMemoriesTable)
	if err != nil {
		log.Fatal("Failed to create memories table:", err)
	}

	// Migrate existing memories table to add new columns if missing
	memoryMigrations := []string{
		"ALTER TABLE memories ADD COLUMN source TEXT DEFAULT 'chat'",
		"ALTER TABLE memories ADD COLUMN source_id TEXT",
		"ALTER TABLE memories ADD COLUMN source_name TEXT",
		"ALTER TABLE memories ADD COLUMN parent_id TEXT",
		"ALTER TABLE memories ADD COLUMN chunk_index INTEGER",
		"ALTER TABLE memories ADD COLUMN total_chunks INTEGER",
		// MCP resource columns
		"ALTER TABLE memories ADD COLUMN mcp_server_id INTEGER",
		"ALTER TABLE memories ADD COLUMN mcp_server_name TEXT",
		"ALTER TABLE memories ADD COLUMN resource_uri TEXT",
		// MCP checksum for avoiding re-embedding unchanged resources
		"ALTER TABLE memories ADD COLUMN content_checksum TEXT",
	}
	for _, migration := range memoryMigrations {
		db.Exec(migration) // Ignore errors (column may already exist)
	}
	rowsAffected, _ = result.RowsAffected()

	// Publish table creation event
	createEvent = events.NewEvent(events.CategoryDatabase, "db_schema", events.LevelInfo).
		WithData("operation", "CREATE TABLE IF NOT EXISTS").
		WithData("table", "memories").
		WithData("rows_affected", rowsAffected).
		WithDuration(tableCreateStart)
	eventBus.Publish(createEvent)

	// Create indexes for memories table
	memoryIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_memories_thread ON memories(thread_id)",
		"CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category)",
		"CREATE INDEX IF NOT EXISTS idx_memories_importance ON memories(importance DESC)",
		"CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_memories_accessed ON memories(last_accessed DESC)",
		"CREATE INDEX IF NOT EXISTS idx_memories_resource_uri ON memories(resource_uri)",
		"CREATE INDEX IF NOT EXISTS idx_memories_content_checksum ON memories(content_checksum)",
	}

	for _, indexSQL := range memoryIndexes {
		if _, err := db.Exec(indexSQL); err != nil {
			log.Printf("Warning: Failed to create memory index: %v", err)
		}
	}

	// Create traces table
	createTracesTable := `
	CREATE TABLE IF NOT EXISTS traces (
		id TEXT PRIMARY KEY,
		parent_trace_id TEXT,
		source TEXT DEFAULT 'chat',
		name TEXT NOT NULL,
		thread_id TEXT,
		service_name TEXT DEFAULT 'agent-core',
		start_time TIMESTAMP NOT NULL,
		end_time TIMESTAMP,
		duration_ms INTEGER,
		status TEXT CHECK (status IN ('ok', 'error', 'unfinished')) DEFAULT 'unfinished',
		root_span_id TEXT,
		span_count INTEGER DEFAULT 0,
		error_count INTEGER DEFAULT 0,
		metadata TEXT DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE SET NULL,
		FOREIGN KEY (parent_trace_id) REFERENCES traces(id) ON DELETE SET NULL
	);`

	tableCreateStart = time.Now()
	result, err = db.Exec(createTracesTable)
	if err != nil {
		log.Fatal("Failed to create traces table:", err)
	}
	rowsAffected, _ = result.RowsAffected()

	createEvent = events.NewEvent(events.CategoryDatabase, "db_schema", events.LevelInfo).
		WithData("operation", "CREATE TABLE IF NOT EXISTS").
		WithData("table", "traces").
		WithData("rows_affected", rowsAffected).
		WithDuration(tableCreateStart)
	eventBus.Publish(createEvent)

	// Create spans table
	createSpansTable := `
	CREATE TABLE IF NOT EXISTS spans (
		id TEXT PRIMARY KEY,
		trace_id TEXT NOT NULL,
		parent_span_id TEXT,
		name TEXT NOT NULL,
		kind TEXT CHECK (kind IN ('internal', 'server', 'client', 'tool', 'llm', 'agent')) DEFAULT 'internal',
		start_time TIMESTAMP NOT NULL,
		end_time TIMESTAMP,
		duration_ms INTEGER,
		status TEXT CHECK (status IN ('ok', 'error', 'running')) DEFAULT 'running',
		status_message TEXT,
		category TEXT,
		thread_id TEXT,
		session_id TEXT,
		agent_id TEXT,
		attributes TEXT DEFAULT '{}',
		input_data TEXT,
		output_data TEXT,
		token_usage_input INTEGER,
		token_usage_output INTEGER,
		cost_usd REAL,
		cache_creation_tokens INTEGER DEFAULT 0,
		cache_read_tokens INTEGER DEFAULT 0,
		reasoning_tokens INTEGER DEFAULT 0,
		provider TEXT,
		model TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (trace_id) REFERENCES traces(id) ON DELETE CASCADE,
		FOREIGN KEY (parent_span_id) REFERENCES spans(id) ON DELETE CASCADE,
		FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE SET NULL
	);`

	tableCreateStart = time.Now()
	result, err = db.Exec(createSpansTable)
	if err != nil {
		log.Fatal("Failed to create spans table:", err)
	}
	rowsAffected, _ = result.RowsAffected()

	createEvent = events.NewEvent(events.CategoryDatabase, "db_schema", events.LevelInfo).
		WithData("operation", "CREATE TABLE IF NOT EXISTS").
		WithData("table", "spans").
		WithData("rows_affected", rowsAffected).
		WithDuration(tableCreateStart)
	eventBus.Publish(createEvent)

	// Migration: Add task_id to spans for task usage tracking
	db.Exec("ALTER TABLE spans ADD COLUMN task_id TEXT")

	// Migration: Add detailed token tracking columns to spans
	db.Exec("ALTER TABLE spans ADD COLUMN cache_creation_tokens INTEGER DEFAULT 0")
	db.Exec("ALTER TABLE spans ADD COLUMN cache_read_tokens INTEGER DEFAULT 0")
	db.Exec("ALTER TABLE spans ADD COLUMN reasoning_tokens INTEGER DEFAULT 0")
	db.Exec("ALTER TABLE spans ADD COLUMN provider TEXT")
	db.Exec("ALTER TABLE spans ADD COLUMN model TEXT")

	// Migration: Add parent_trace_id and source to traces for hierarchy tracking
	db.Exec("ALTER TABLE traces ADD COLUMN parent_trace_id TEXT REFERENCES traces(id)")
	db.Exec("ALTER TABLE traces ADD COLUMN source TEXT DEFAULT 'chat'")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_traces_parent ON traces(parent_trace_id)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_traces_source ON traces(source)")

	// Create events table
	createEventsTable := `
	CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		trace_id TEXT,
		span_id TEXT,
		timestamp TIMESTAMP NOT NULL,
		category TEXT NOT NULL,
		type TEXT NOT NULL,
		level TEXT NOT NULL,
		thread_id TEXT,
		session_id TEXT,
		data TEXT DEFAULT '{}',
		metadata TEXT DEFAULT '{}',
		error TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (trace_id) REFERENCES traces(id) ON DELETE CASCADE,
		FOREIGN KEY (span_id) REFERENCES spans(id) ON DELETE CASCADE,
		FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE SET NULL
	);`

	tableCreateStart = time.Now()
	result, err = db.Exec(createEventsTable)
	if err != nil {
		log.Fatal("Failed to create events table:", err)
	}
	rowsAffected, _ = result.RowsAffected()

	createEvent = events.NewEvent(events.CategoryDatabase, "db_schema", events.LevelInfo).
		WithData("operation", "CREATE TABLE IF NOT EXISTS").
		WithData("table", "events").
		WithData("rows_affected", rowsAffected).
		WithDuration(tableCreateStart)
	eventBus.Publish(createEvent)

	// Create indexes for tracing tables
	tracingIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_traces_thread ON traces(thread_id)",
		"CREATE INDEX IF NOT EXISTS idx_traces_start_time ON traces(start_time DESC)",
		"CREATE INDEX IF NOT EXISTS idx_traces_status ON traces(status)",
		"CREATE INDEX IF NOT EXISTS idx_traces_service ON traces(service_name)",
		"CREATE INDEX IF NOT EXISTS idx_spans_trace ON spans(trace_id)",
		"CREATE INDEX IF NOT EXISTS idx_spans_parent ON spans(parent_span_id)",
		"CREATE INDEX IF NOT EXISTS idx_spans_start_time ON spans(start_time DESC)",
		"CREATE INDEX IF NOT EXISTS idx_spans_thread ON spans(thread_id)",
		"CREATE INDEX IF NOT EXISTS idx_spans_kind ON spans(kind)",
		"CREATE INDEX IF NOT EXISTS idx_spans_status ON spans(status)",
		"CREATE INDEX IF NOT EXISTS idx_spans_task ON spans(task_id)",
		"CREATE INDEX IF NOT EXISTS idx_events_trace ON events(trace_id)",
		"CREATE INDEX IF NOT EXISTS idx_events_span ON events(span_id)",
		"CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_events_category ON events(category)",
		"CREATE INDEX IF NOT EXISTS idx_events_thread ON events(thread_id)",
		"CREATE INDEX IF NOT EXISTS idx_events_type ON events(type)",
		"CREATE INDEX IF NOT EXISTS idx_events_level ON events(level)",
	}

	for _, indexSQL := range tracingIndexes {
		if _, err := db.Exec(indexSQL); err != nil {
			log.Printf("Warning: Failed to create tracing index: %v", err)
		}
	}

	// Create files table (for file system storage)
	createFilesTable := `
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		thread_id TEXT,
		message_id TEXT,
		filename TEXT NOT NULL,
		mime_type TEXT NOT NULL,
		file_type TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		checksum TEXT,
		data BLOB NOT NULL,
		width INTEGER,
		height INTEGER,
		duration INTEGER,
		metadata TEXT DEFAULT '{}',
		source TEXT DEFAULT 'unknown',
		source_tool TEXT,
		access_count INTEGER DEFAULT 0,
		last_accessed TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP,
		FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
		FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE SET NULL
	);`

	tableCreateStart = time.Now()
	result, err = db.Exec(createFilesTable)
	if err != nil {
		log.Fatal("Failed to create files table:", err)
	}
	rowsAffected, _ = result.RowsAffected()

	createEvent = events.NewEvent(events.CategoryDatabase, "db_schema", events.LevelInfo).
		WithData("operation", "CREATE TABLE IF NOT EXISTS").
		WithData("table", "files").
		WithData("rows_affected", rowsAffected).
		WithDuration(tableCreateStart)
	eventBus.Publish(createEvent)

	// Create indexes for files table
	filesIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_files_thread ON files(thread_id)",
		"CREATE INDEX IF NOT EXISTS idx_files_message ON files(message_id)",
		"CREATE INDEX IF NOT EXISTS idx_files_checksum ON files(checksum)",
		"CREATE INDEX IF NOT EXISTS idx_files_created ON files(created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_files_expires ON files(expires_at)",
		"CREATE INDEX IF NOT EXISTS idx_files_type ON files(file_type)",
		"CREATE INDEX IF NOT EXISTS idx_files_source ON files(source)",
	}

	for _, indexSQL := range filesIndexes {
		if _, err := db.Exec(indexSQL); err != nil {
			log.Printf("Warning: Failed to create files index: %v", err)
		}
	}

	// Create MCP subscriptions table for webhook subscriptions
	createSubscriptionsTable := `
	CREATE TABLE IF NOT EXISTS mcp_subscriptions (
		id TEXT PRIMARY KEY,
		server TEXT NOT NULL UNIQUE,
		events TEXT NOT NULL DEFAULT '[]',
		credential_id INTEGER,
		prompt TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	tableCreateStart = time.Now()
	result, err = db.Exec(createSubscriptionsTable)
	if err != nil {
		log.Fatal("Failed to create mcp_subscriptions table:", err)
	}
	rowsAffected, _ = result.RowsAffected()

	// Add credential_id column if it doesn't exist (migration for existing DBs)
	db.Exec("ALTER TABLE mcp_subscriptions ADD COLUMN credential_id INTEGER")

	createEvent = events.NewEvent(events.CategoryDatabase, "db_schema", events.LevelInfo).
		WithData("operation", "CREATE TABLE IF NOT EXISTS").
		WithData("table", "mcp_subscriptions").
		WithData("rows_affected", rowsAffected).
		WithDuration(tableCreateStart)
	eventBus.Publish(createEvent)

	// Create index for mcp_subscriptions
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_mcp_subscriptions_server ON mcp_subscriptions(server)"); err != nil {
		log.Printf("Warning: Failed to create mcp_subscriptions index: %v", err)
	}

	// Run migrations to ensure schema is up to date
	runMigrations(db)

	log.Println("Database initialized with threads, messages, tasks, memories, traces, spans, events, and files tables")
}

// DatabaseMessageSaver implementation



func handleHealth(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(startTime).String()
	response := HealthResponse{
		Status:  "healthy",
		Uptime:  uptime,
		Version: getVersion(),
	}
	sendJSON(w, http.StatusOK, response)
}

// handleDiscoveredAgents returns the list of discovered peer agents
func handleDiscoveredAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.GetConfig().Get()
	agentsConfig := cfg.Agents

	response := map[string]interface{}{
		"enabled":   agentsConfig != nil && agentsConfig.Enabled,
		"mode":      "",
		"group":     "",
		"running":   false,
		"agents":    []config.AgentInfo{},
	}

	if agentsConfig != nil {
		response["mode"] = agentsConfig.Mode
		response["group"] = agentsConfig.Group
	}

	if discoveryService != nil {
		response["running"] = discoveryService.IsRunning()
		if discoveryService.IsRunning() {
			agents := discoveryService.GetAgents()
			response["agents"] = agents
			response["count"] = len(agents)
		}
	}

	sendJSON(w, http.StatusOK, response)
}

// handleCustomTools handles GET /tools/custom - lists all available custom tools
func handleCustomTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	allTools := tools.ListAllTools()

	// Convert to response format with display name
	type ToolInfo struct {
		Name        string                 `json:"name"`
		DisplayName string                 `json:"display_name"`
		Description string                 `json:"description"`
		InputSchema map[string]interface{} `json:"input_schema"`
	}

	toolList := make([]ToolInfo, len(allTools))
	for i, t := range allTools {
		toolList[i] = ToolInfo{
			Name:        t.Name,
			DisplayName: t.DisplayName,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tools":   toolList,
		"count":   len(toolList),
	})
}

// handleSubscriptions handles GET /subscriptions - lists all webhook subscriptions
func handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Use the subscription list tool
	result, err := tools.SubscriptionsListTool(map[string]interface{}{})
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	sendJSON(w, http.StatusOK, result)
}

// handleWebhookReceive handles POST /webhook/receive - receives forwarded webhooks from the central receiver
func handleWebhookReceive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("⚠️ Webhook receive: failed to read body: %v", err)
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "failed to read request body",
		})
		return
	}

	// Parse webhook payload
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("⚠️ Webhook receive: failed to parse JSON: %v", err)
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "invalid JSON payload",
		})
		return
	}

	// Extract webhook details
	server, _ := payload["server"].(string)
	eventType, _ := payload["event_type"].(string)
	eventData := payload["data"]
	prompt, _ := payload["prompt"].(string)

	log.Printf("📥 Webhook received: server=%s, event=%s", server, eventType)

	// Build a message for the agent to process
	var userMessage string
	if prompt != "" {
		userMessage = prompt + "\n\n"
	}
	userMessage += fmt.Sprintf("Webhook event received from %s:\nEvent type: %s\n\nEvent data:\n%s",
		server, eventType, formatEventData(eventData))

	// Create a new thread for this webhook
	threadID := fmt.Sprintf("webhook_%s_%d", server, time.Now().UnixNano())

	// Publish event for tracking
	eventBus := events.GetEventBus()
	webhookEvent := events.NewEvent(events.CategorySystem, "webhook_received", events.LevelInfo).
		WithData("server", server).
		WithData("event_type", eventType).
		WithData("thread_id", threadID)
	eventBus.Publish(webhookEvent)

	// Process via internal chat endpoint - build request body
	chatPayload := map[string]interface{}{
		"message":   userMessage,
		"thread_id": threadID,
		"stream":    false,
	}

	// Process asynchronously by making internal HTTP call to /chat
	go func() {
		log.Printf("🔄 Processing webhook in thread %s", threadID)

		chatBody, _ := json.Marshal(chatPayload)

		// Get the port we're running on
		port := os.Getenv("PORT")
		if port == "" {
			port = "4015"
		}

		// Make internal request to chat endpoint
		chatURL := fmt.Sprintf("http://localhost:%s/chat", port)
		req, err := http.NewRequest("POST", chatURL, bytes.NewBuffer(chatBody))
		if err != nil {
			log.Printf("⚠️ Webhook processing failed to create request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		// Add API key for internal auth
		if apiKey := os.Getenv("AGENT_API_KEY"); apiKey != "" {
			req.Header.Set("X-API-Key", apiKey)
		}

		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("⚠️ Webhook processing failed: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			log.Printf("✅ Webhook processed successfully in thread %s", threadID)
		} else {
			responseBody, _ := io.ReadAll(resp.Body)
			log.Printf("⚠️ Webhook processing returned %d: %s", resp.StatusCode, string(responseBody))
		}
	}()

	// Return immediately
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"thread_id": threadID,
		"message":   "webhook received and processing",
	})
}

// formatEventData formats event data for display
func formatEventData(data interface{}) string {
	if data == nil {
		return "(no data)"
	}
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(jsonBytes)
}

// handleRequestCancel handles POST /requests/{id}/cancel
func handleRequestCancel(w http.ResponseWriter, r *http.Request) {
	// Only allow POST method
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract request ID from path: /requests/{id}/cancel
	path := strings.TrimPrefix(r.URL.Path, "/requests/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "cancel" {
		http.Error(w, "Invalid path. Use /requests/{id}/cancel", http.StatusBadRequest)
		return
	}

	requestID := parts[0]
	if requestID == "" {
		http.Error(w, "Request ID is required", http.StatusBadRequest)
		return
	}

	// Try to cancel the request
	if requestTracker == nil {
		sendJSON(w, http.StatusServiceUnavailable, agents.CancelResponse{
			Success:   false,
			RequestID: requestID,
			Cancelled: false,
			Error:     "Request tracker not initialized",
		})
		return
	}

	err := requestTracker.Cancel(requestID)
	if err != nil {
		sendJSON(w, http.StatusNotFound, agents.CancelResponse{
			Success:   false,
			RequestID: requestID,
			Cancelled: false,
			Error:     err.Error(),
		})
		return
	}

	sendJSON(w, http.StatusOK, agents.CancelResponse{
		Success:   true,
		RequestID: requestID,
		Cancelled: true,
	})
}

// processPDFBlocks handles PDF content blocks, converting URLs to base64
func processPDFBlocks(blocks []ContentBlock) ([]ContentBlock, error) {
	cfg := config.GetConfig()
	llmConfig := cfg.GetLLMConfig()

	// Check if vision and PDF support are enabled
	if llmConfig.Vision == nil || !llmConfig.Vision.Enabled {
		return blocks, nil // Vision not enabled, return as-is
	}

	pdfConfig := llmConfig.Vision.PDFConfig
	if pdfConfig == nil || !pdfConfig.Enabled {
		return blocks, nil // PDF not enabled, return as-is
	}

	var processedBlocks []ContentBlock
	for _, block := range blocks {
		// Only process document blocks with PDF media type
		if block.Type == "document" && block.Source != nil &&
		   block.Source.MediaType == "application/pdf" {

			// Create a copy to avoid modifying original
			processedBlock := block

			// Process the PDF block
			if err := processPDFBlock(&processedBlock, pdfConfig); err != nil {
				return nil, err
			}

			processedBlocks = append(processedBlocks, processedBlock)
		} else {
			// Keep other blocks as-is
			processedBlocks = append(processedBlocks, block)
		}
	}

	return processedBlocks, nil
}

// processPDFBlock handles a single PDF content block
func processPDFBlock(block *ContentBlock, pdfConfig *config.PDFConfig) error {
	if block.Source == nil {
		return fmt.Errorf("PDF block missing source")
	}

	validator := pdf.NewPDFValidator(pdfConfig)

	switch block.Source.Type {
	case "url":
		if !pdfConfig.AllowURLs {
			return fmt.Errorf("PDF URL fetching is disabled")
		}

		// Fetch PDF from URL
		pdfData, err := pdf.FetchPDFFromURL(block.Source.URL, pdfConfig.MaxFileSize)
		if err != nil {
			return fmt.Errorf("failed to fetch PDF from URL: %w", err)
		}

		// Validate PDF
		if err := validator.ValidatePDF(pdfData); err != nil {
			return err
		}

		// Convert to base64 for API
		block.Source.Type = "base64"
		block.Source.Data = base64.StdEncoding.EncodeToString(pdfData)
		block.Source.URL = "" // Clear URL field

	case "base64":
		// Decode and validate
		pdfData, err := base64.StdEncoding.DecodeString(block.Source.Data)
		if err != nil {
			return fmt.Errorf("invalid base64 PDF data: %w", err)
		}

		if err := validator.ValidatePDF(pdfData); err != nil {
			return err
		}

	case "file":
		// File API support for future
		return fmt.Errorf("file-based PDFs not yet supported")

	default:
		return fmt.Errorf("unsupported PDF source type: %s", block.Source.Type)
	}

	return nil
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read raw body first (for webhook fallback)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse request
	var chatReq ChatRequest
	if err := json.Unmarshal(rawBody, &chatReq); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Check for structured webhook event: { "type": "webhook", "server": "...", "event": "...", "data": {...}, "prompt": "..." }
	var webhookTrigger map[string]interface{} // Will be set if this is a webhook event
	var webhookEvent struct {
		Type   string                 `json:"type"`
		Server string                 `json:"server"`
		Event  string                 `json:"event"`
		Data   map[string]interface{} `json:"data"`
		Prompt string                 `json:"prompt"`
	}
	if err := json.Unmarshal(rawBody, &webhookEvent); err == nil && webhookEvent.Type == "webhook" {
		// Check if MCP is enabled and webhooks are configured
		currentCfg := config.GetConfig().Get()
		if currentCfg.MCP == nil || !currentCfg.MCP.Enabled {
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": "MCP is not enabled in agent configuration",
			})
			return
		}

		// Webhooks array must have servers configured - empty means no webhooks allowed
		if len(currentCfg.MCP.Webhooks) == 0 {
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": "No webhook servers configured in agent",
			})
			return
		}

		// Check if this server is in the allowed list
		serverEnabled := false
		for _, enabledServer := range currentCfg.MCP.Webhooks {
			if enabledServer == webhookEvent.Server {
				serverEnabled = true
				break
			}
		}
		if !serverEnabled {
			sendJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("Webhook server '%s' is not in the allowed webhooks list", webhookEvent.Server),
			})
			return
		}

		// Build message with event data (prompt comes from central receiver if set)
		dataJSON, _ := json.MarshalIndent(webhookEvent.Data, "", "  ")
		eventMessage := fmt.Sprintf("[Webhook Event - %s]\nEvent: %s\n\nData:\n%s",
			webhookEvent.Server, webhookEvent.Event, string(dataJSON))

		if webhookEvent.Prompt != "" {
			eventMessage += fmt.Sprintf("\n\nInstructions:\n%s", webhookEvent.Prompt)
		}

		chatReq.Message = eventMessage

		// Create new thread for this webhook (don't use provided thread_id)
		chatReq.ThreadID = nil

		// Disable streaming for webhook events
		streamFalse := false
		chatReq.Stream = &streamFalse

		// Store trigger info for thread metadata
		webhookTrigger = map[string]interface{}{
			"trigger": map[string]interface{}{
				"type":   "subscription",
				"server": webhookEvent.Server,
				"event":  webhookEvent.Event,
			},
		}

		log.Printf("📨 Webhook event: %s/%s (subscribed, processing)", webhookEvent.Server, webhookEvent.Event)
	}

	// Webhook fallback: if message is missing, use raw JSON as the message
	// This allows webhooks to POST directly without a wrapper
	messageEmpty := chatReq.Message == nil
	if !messageEmpty {
		if str, ok := chatReq.Message.(string); ok && str == "" {
			messageEmpty = true
		}
	}
	if messageEmpty {
		// Prefix with context so it's not parsed as JSON when loaded from DB
		chatReq.Message = "Incoming data:\n" + string(rawBody)
		// Disable streaming for webhook payloads (they expect a complete response)
		streamFalse := false
		chatReq.Stream = &streamFalse
		log.Printf("📨 Webhook fallback: using raw JSON as message (stream=false)")
	}

	// Generate request ID for cancellation support
	requestID := agents.GenerateRequestID()

	// Create trace for this chat request
	threadID := ""
	if chatReq.ThreadID != nil {
		threadID = *chatReq.ThreadID
	}
	trace := events.NewTrace("chat_request", threadID, db)

	// Set trace source - explicit source takes priority, then webhook detection, then default to chat
	if chatReq.Source != nil && *chatReq.Source != "" {
		trace.WithSource(events.TraceSource(*chatReq.Source))
	} else if webhookTrigger != nil {
		trace.WithSource(events.TraceSourceWebhook)
	} else {
		trace.WithSource(events.TraceSourceChat)
	}

	// Set parent trace ID if provided (for task executions spawned by scheduler)
	if chatReq.ParentTraceID != nil && *chatReq.ParentTraceID != "" {
		trace.WithParent(*chatReq.ParentTraceID)
		log.Printf("Trace %s linked to parent trace %s", trace.ID, *chatReq.ParentTraceID)
	}
	defer trace.Finish()

	eventBus := events.GetEventBus()

	// Register request with tracker for cancellation support
	var requestCtx context.Context
	if requestTracker != nil {
		var activeReq *agents.ActiveRequest
		var err error
		requestCtx, activeReq, err = requestTracker.RegisterWithParentContext(r.Context(), requestID, threadID)
		if err != nil {
			log.Printf("Failed to register request: %v", err)
			requestCtx = r.Context() // Fall back to request context
		} else {
			defer requestTracker.Complete(requestID)
			// Set request ID header early so client can capture it
			w.Header().Set("X-Request-ID", requestID)
			_ = activeReq // Suppress unused warning
		}
	} else {
		requestCtx = r.Context()
	}

	// Extract call context from incoming agent-to-agent request headers
	// This enables guard rails (circular call detection, max depth, etc.)
	if agentClient != nil {
		callCtx := agents.ExtractCallContext(r)
		agentClient.SetCallContext(callCtx)
		defer agentClient.ClearCallContext()

		// Set request context for cancellation propagation to other agents
		agentURL := r.Host
		if r.TLS == nil {
			agentURL = "http://" + agentURL
		} else {
			agentURL = "https://" + agentURL
		}
		agentClient.SetRequestContext(requestID, agentURL)
		defer agentClient.ClearRequestContext()
	}

	// Get current configuration to determine provider
	cfg := config.GetConfig()
	llmConfig := cfg.GetLLMConfig()

	// Create root span for chat handling
	rootSpan := trace.StartSpan("handle_chat", events.SpanKindServer).
		WithThreadID(threadID).
		WithAttribute("provider", llmConfig.Provider).
		WithAttribute("model", llmConfig.Model).
		WithCategory(events.CategoryChat)
	defer rootSpan.End()

	// Set as current span and trace so events auto-attach and we can create sibling spans
	events.SetCurrentSpan(rootSpan)
	defer events.SetCurrentSpan(nil)
	events.SetCurrentTrace(trace)
	defer events.SetCurrentTrace(nil)

	// Enrich request context with trace and span for context-based propagation
	requestCtx = events.WithTraceAndSpan(requestCtx, trace, rootSpan)

	// Parse message content - can be string or array of content blocks
	var messageContent interface{}
	var messageText string // For thread title extraction

	switch msg := chatReq.Message.(type) {
	case string:
		if msg == "" {
			http.Error(w, "Message is required", http.StatusBadRequest)
			return
		}
		messageContent = msg
		messageText = msg
	case []interface{}:
		// Convert to []ContentBlock
		var contentBlocks []ContentBlock
		for _, block := range msg {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				http.Error(w, "Invalid content block format", http.StatusBadRequest)
				return
			}

			// Parse each content block
			blockJSON, err := json.Marshal(blockMap)
			if err != nil {
				http.Error(w, "Invalid content block", http.StatusBadRequest)
				return
			}

			var contentBlock ContentBlock
			if err := json.Unmarshal(blockJSON, &contentBlock); err != nil {
				http.Error(w, "Invalid content block structure", http.StatusBadRequest)
				return
			}

			contentBlocks = append(contentBlocks, contentBlock)

			// Extract text for title
			if contentBlock.Type == "text" && messageText == "" {
				messageText = contentBlock.Text
			}
		}

		if len(contentBlocks) == 0 {
			http.Error(w, "At least one content block is required", http.StatusBadRequest)
			return
		}

		// Process PDF blocks if any
		processedBlocks, err := processPDFBlocks(contentBlocks)
		if err != nil {
			http.Error(w, fmt.Sprintf("PDF processing error: %v", err), http.StatusBadRequest)
			return
		}

		// Extract base64 files and store them (if filesystem enabled)
		if fileManager != nil && fileManager.IsEnabled() {
			// Convert to filesystem.ContentBlock format for processing
			fsBlocks := make([]filesystem.ContentBlock, len(processedBlocks))
			for i, block := range processedBlocks {
				fsBlocks[i] = filesystem.ContentBlock{
					Type: block.Type,
					Text: block.Text,
				}
				if block.Source != nil {
					fsBlocks[i].Source = &filesystem.ContentSource{
						Type:      block.Source.Type,
						MediaType: block.Source.MediaType,
						Data:      block.Source.Data,
						URL:       block.Source.URL,
						FileID:    block.Source.FileID,
					}
				}
			}

			// Process and extract files (this creates a message ID placeholder that will be updated)
			log.Printf("📁 Processing %d content blocks for file extraction", len(fsBlocks))
			extractedBlocks, err := fileManager.ProcessContentBlocks(fsBlocks, threadID, "", "user_upload")
			if err != nil {
				log.Printf("⚠️  File extraction error: %v", err)
			} else {
				// Convert back to ContentBlock format
				for i, fsBlock := range extractedBlocks {
					if fsBlock.Source != nil {
						log.Printf("📁 Block %d: type=%s, sourceType=%s, fileID=%s, mediaType=%s, URL=%s",
							i, fsBlock.Type, fsBlock.Source.Type, fsBlock.Source.FileID, fsBlock.Source.MediaType, fsBlock.Source.URL)
						if processedBlocks[i].Source == nil {
							processedBlocks[i].Source = &ContentSource{}
						}
						processedBlocks[i].Source.Type = fsBlock.Source.Type
						processedBlocks[i].Source.FileID = fsBlock.Source.FileID
						processedBlocks[i].Source.URL = fsBlock.Source.URL // Also preserve URL!
						processedBlocks[i].Source.MediaType = fsBlock.Source.MediaType // Preserve for LLM
						if fsBlock.Source.Type == "file" {
							// Clear the base64 data since it's now stored
							processedBlocks[i].Source.Data = ""
							log.Printf("📁 File stored: ID=%s, URL=%s", fsBlock.Source.FileID, fsBlock.Source.URL)
						}
					}
				}
			}
		}

		messageContent = processedBlocks
	default:
		http.Error(w, "Message must be a string or array of content blocks", http.StatusBadRequest)
		return
	}

	// Handle thread management (threadID already extracted above for trace)
	isNewThread := false
	if threadID == "" {
		isNewThread = true
		// Create new thread with title based on first message text
		title := messageText
		if title == "" {
			title = "New conversation"
		}
		if len(title) > 50 {
			title = title[:50] + "..."
		}

		var err error
		threadStartTime := time.Now()
		threadID, err = messageSaver.CreateThread(title)
		if err != nil {
			log.Printf("Error creating thread: %v", err)
			http.Error(w, "Error creating thread", http.StatusInternalServerError)
			return
		}

		// Update trace and span with new thread ID
		trace.UpdateThreadID(threadID)
		rootSpan.WithThreadID(threadID)

		// If this is a webhook event, update thread metadata with trigger info
		if webhookTrigger != nil {
			if err := messageSaver.MergeThreadMetadata(threadID, webhookTrigger); err != nil {
				log.Printf("⚠️ Failed to update thread metadata with webhook trigger: %v", err)
			}
		}

		// Publish thread creation event
		threadEvent := events.NewEvent(events.CategoryChat, "thread_created", events.LevelInfo).
			WithData("thread_id", threadID).
			WithData("title", title).
			WithDuration(threadStartTime)
		eventBus.Publish(threadEvent)
	}

	// Get task_id from thread metadata if present (for task execution context tracking)
	taskID := messageSaver.GetThreadTaskID(threadID)

	// Update root span with task_id for usage tracking
	if taskID != "" {
		rootSpan.WithTaskID(taskID)
	}

	// Publish chat message received event
	chatEvent := events.NewEvent(events.CategoryChat, events.TypeChatMessageReceived, events.LevelInfo).
		WithThread(threadID).
		WithData("message_preview", messageText).
		WithData("message_type", fmt.Sprintf("%T", chatReq.Message)).
		WithData("source", string(trace.Source)).
		WithMetadata("provider", llmConfig.Provider).
		WithMetadata("model", llmConfig.Model)
	if webhookTrigger != nil {
		if trigger, ok := webhookTrigger["trigger"].(map[string]interface{}); ok {
			chatEvent.WithData("webhook_server", trigger["server"]).
				WithData("webhook_event", trigger["event"])
		}
	}
	eventBus.Publish(chatEvent)

	// Return thread ID and trace ID in response headers
	w.Header().Set("X-Thread-ID", threadID)
	w.Header().Set("X-Trace-ID", trace.ID)

	// Save user message with the parsed content
	dbStartTime := time.Now()
	if err := messageSaver.SaveMessage(threadID, "user", messageContent, nil, nil); err != nil {
		log.Printf("Error saving user message: %v", err)
		// Publish database error event
		dbEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBError, events.LevelError).
			WithThread(threadID).
			WithError(err).
			WithDuration(dbStartTime)
		eventBus.Publish(dbEvent)
		http.Error(w, "Error saving message", http.StatusInternalServerError)
		return
	}

	// Publish database insert event
	dbEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBInsert, events.LevelInfo).
		WithThread(threadID).
		WithData("table", "messages").
		WithData("role", "user").
		WithDuration(dbStartTime)
	eventBus.Publish(dbEvent)

	// Get existing thread messages for context
	dbQueryStart := time.Now()
	existingMessages, err := messageSaver.GetThreadMessages(threadID)
	if err != nil {
		log.Printf("Error getting thread messages: %v", err)
		http.Error(w, "Error retrieving conversation history", http.StatusInternalServerError)
		return
	}

	// Publish database query event
	queryEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBQuery, events.LevelInfo).
		WithThread(threadID).
		WithData("table", "messages").
		WithData("rows_returned", len(existingMessages)).
		WithDuration(dbQueryStart)
	eventBus.Publish(queryEvent)

	// Convert to stream messages format with proper content handling
	var messages []stream.Message
	for _, msg := range existingMessages {
		// Convert database message content to proper format, including reasoning_content from metadata
		streamMsg, err := stream.ConvertDatabaseMessageToStreamMessageWithMetadata(msg.Role, msg.Content, msg.Metadata)
		if err != nil {
			log.Printf("Error converting message from database: %v", err)
			// Fall back to simple format
			messages = append(messages, stream.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		} else {
			messages = append(messages, streamMsg)
		}
	}

	// Get agent config
	agentConfig := cfg.Get()

	// Auto-derive effective max tokens from model context window if not explicitly set
	effectiveMaxTokens := 0
	if agentConfig.Context != nil {
		effectiveMaxTokens = agentConfig.Context.MaxTokens
	}
	if effectiveMaxTokens == 0 {
		contextWindow := config.GetModelContextWindow(llmConfig.Provider, llmConfig.Model)
		// Reserve 20% for output tokens, system prompt, and tool definitions
		effectiveMaxTokens = int(float64(contextWindow) * 0.8)
		log.Printf("📊 Auto-derived context budget: %d tokens (80%% of %d for %s/%s)",
			effectiveMaxTokens, contextWindow, llmConfig.Provider, llmConfig.Model)
	}

	// Check for existing compaction and filter already-compacted messages
	var compactionSummary string
	if agentConfig.Context != nil && agentConfig.Context.Compaction != nil && agentConfig.Context.Compaction.Enabled {
		compactor := stream.NewCompactor(
			agentConfig.Context.Compaction,
			effectiveMaxTokens,
			db,
			os.Getenv("ANTHROPIC_API_KEY"),
		)

		// Check for existing compaction summary and skip already-compacted messages
		if existing, err := compactor.GetExistingCompaction(threadID); err == nil && existing != nil {
			compactionSummary = existing.Summary
			// Skip messages already covered by the compaction summary
			if existing.MessagesCompacted > 0 && existing.MessagesCompacted < len(messages) {
				beforeCount := len(messages)
				messages = messages[existing.MessagesCompacted:]
				log.Printf("📦 Loaded compaction summary (%d chars), skipped %d already-compacted messages (%d → %d)",
					len(compactionSummary), existing.MessagesCompacted, beforeCount, len(messages))
			} else {
				log.Printf("📦 Loaded compaction summary (%d chars)", len(compactionSummary))
			}
		}
	}

	// Apply context management (truncate messages and strip old images) — hard safety net
	if agentConfig.Context != nil {
		contextMgr := stream.NewContextManager(
			agentConfig.Context.MaxMessages,
			effectiveMaxTokens,
			agentConfig.Context.KeepImages,
		)
		messages = contextMgr.TruncateMessages(messages)
	}

	// Expand file references to base64 for LLM (if filesystem enabled)
	if fileManager != nil && fileManager.IsEnabled() {
		log.Printf("📁 Expanding file references for %d messages", len(messages))
		for i := range messages {
			var fsBlocks []filesystem.ContentBlock
			var originalBlocks []stream.ContentBlock

			// Handle both []interface{} (raw JSON) and []stream.ContentBlock (converted)
			switch contentBlocks := messages[i].Content.(type) {
			case []interface{}:
				// Raw JSON format - convert to filesystem.ContentBlock
				for _, block := range contentBlocks {
					if blockMap, ok := block.(map[string]interface{}); ok {
						var fsBlock filesystem.ContentBlock
						if blockJSON, err := json.Marshal(blockMap); err == nil {
							if err := json.Unmarshal(blockJSON, &fsBlock); err == nil {
								fsBlocks = append(fsBlocks, fsBlock)
							}
						}
					}
				}

			case []stream.ContentBlock:
				// Already typed - convert stream.ContentBlock to filesystem.ContentBlock
				log.Printf("📁 Message %d has %d typed content blocks", i, len(contentBlocks))
				for j, block := range contentBlocks {
					if block.Source != nil {
						log.Printf("📁 Message %d Block %d: type=%s, sourceType=%s, fileID=%s, URL=%s",
							i, j, block.Type, block.Source.Type, block.Source.FileID, block.Source.URL)
					}
					fsBlock := filesystem.ContentBlock{
						Type: block.Type,
						Text: block.Text,
						ID:   block.ID,
						Name: block.Name,
					}
					if block.Source != nil {
						fsBlock.Source = &filesystem.ContentSource{
							Type:      block.Source.Type,
							MediaType: block.Source.MediaType,
							Data:      block.Source.Data,
							URL:       block.Source.URL,
							FileID:    block.Source.FileID,
						}
					}
					if block.Input != nil {
						fsBlock.Input = block.Input
					}
					if block.ToolUseID != "" {
						fsBlock.ToolUseID = block.ToolUseID
					}
					if block.Content != nil {
						fsBlock.Content = block.Content
					}
					fsBlock.IsError = block.IsError
					fsBlocks = append(fsBlocks, fsBlock)
				}
				originalBlocks = contentBlocks
			}

			// Expand file references
			if len(fsBlocks) > 0 {
				// FIRST: Capture file metadata BEFORE expansion (expansion clears FileID/URL)
				type fileMeta struct {
					index     int
					fileID    string
					url       string
					mediaType string
				}
				var fileMetadata []fileMeta
				for j, fsBlock := range fsBlocks {
					if fsBlock.Source != nil && fsBlock.Source.Type == "file" && fsBlock.Source.FileID != "" {
						fileMetadata = append(fileMetadata, fileMeta{
							index:     j,
							fileID:    fsBlock.Source.FileID,
							url:       fsBlock.Source.URL,
							mediaType: fsBlock.Source.MediaType,
						})
						log.Printf("📎 Captured metadata for block %d: fileID=%s, URL=%s", j, fsBlock.Source.FileID, fsBlock.Source.URL)
					}
				}

				// THEN: Expand file references (converts file refs to base64)
				expandedBlocks, err := fileManager.ExpandFileReferences(fsBlocks)
				if err != nil {
					log.Printf("⚠️  File expansion error: %v", err)
				} else {
					// Convert back to stream.ContentBlock for the LLM
					// We may add metadata text blocks, so use a slice we can append to
					var expandedContent []stream.ContentBlock

					// Track which blocks had file metadata
					metaByIndex := make(map[int]fileMeta)
					for _, m := range fileMetadata {
						metaByIndex[m.index] = m
					}

					for j, fsBlock := range expandedBlocks {
						// Check if this block had file metadata - inject text block BEFORE the content
						if meta, hasFile := metaByIndex[j]; hasFile {
							metadataText := fmt.Sprintf("[Uploaded file stored in filesystem]\n- File ID: %s\n- Type: %s",
								meta.fileID, meta.mediaType)
							if meta.url != "" {
								metadataText += fmt.Sprintf("\n- URL: %s", meta.url)
							}
							metadataText += "\n(You can reference this file by its ID in filesystem tools)"

							expandedContent = append(expandedContent, stream.ContentBlock{
								Type: "text",
								Text: metadataText,
							})
							log.Printf("📎 Injected file metadata for %s (%s)", meta.fileID, meta.mediaType)
						}

						// Add the actual content block
						contentBlock := stream.ContentBlock{
							Type:      fsBlock.Type,
							Text:      fsBlock.Text,
							ID:        fsBlock.ID,
							Name:      fsBlock.Name,
							ToolUseID: fsBlock.ToolUseID,
							IsError:   fsBlock.IsError,
						}
						if fsBlock.Source != nil {
							contentBlock.Source = &stream.ContentSource{
								Type:      fsBlock.Source.Type,
								MediaType: fsBlock.Source.MediaType,
								Data:      fsBlock.Source.Data,
								URL:       fsBlock.Source.URL,
								FileID:    fsBlock.Source.FileID,
							}
						}
						// Preserve input from original blocks
						if j < len(originalBlocks) && originalBlocks[j].Input != nil {
							contentBlock.Input = originalBlocks[j].Input
						} else if fsBlock.Input != nil {
							contentBlock.Input = fsBlock.Input
						}
						// Preserve content from original blocks (for tool_result)
						if j < len(originalBlocks) && originalBlocks[j].Content != nil {
							contentBlock.Content = originalBlocks[j].Content
						} else if fsBlock.Content != nil {
							contentBlock.Content = fsBlock.Content
						}
						expandedContent = append(expandedContent, contentBlock)
					}
					messages[i].Content = expandedContent
					log.Printf("✅ Expanded %d file references in message %d", len(expandedBlocks), i)
				}
			}
		}
	}

	// Retrieve relevant memories if memory manager is enabled
	if memoryManager != nil && agentConfig.Memory != nil && agentConfig.Memory.Enabled {
		// Get the user's message text for memory search
		var queryText string
		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			switch content := lastMsg.Content.(type) {
			case string:
				queryText = content
			case []interface{}:
				// Extract text from content blocks
				for _, block := range content {
					if blockMap, ok := block.(map[string]interface{}); ok {
						if blockMap["type"] == "text" {
							if text, ok := blockMap["text"].(string); ok {
								queryText += text + " "
							}
						}
					}
				}
			}
		}

		if queryText != "" && len(strings.TrimSpace(queryText)) > 10 { // Only for substantial queries
			// Skip memory retrieval for generic greetings
			lowerQuery := strings.ToLower(queryText)
			isGenericGreeting := strings.Contains(lowerQuery, "hello") ||
								strings.Contains(lowerQuery, "hi") ||
								strings.Contains(lowerQuery, "hey") ||
								strings.Contains(lowerQuery, "good morning") ||
								strings.Contains(lowerQuery, "good afternoon") ||
								strings.Contains(lowerQuery, "how are you")

			if !isGenericGreeting {
				log.Printf("[Memory] Searching for relevant memories, query: %q, thread: %s", queryText, threadID)
				memories, err := memoryManager.RetrieveRelevant(queryText, threadID, agentConfig.Memory.MaxMemoriesPerQuery)
				if err != nil {
					log.Printf("[Memory] Failed to retrieve memories: %v", err)
				} else if len(memories) > 0 {
					log.Printf("[Memory] Found %d memories for query", len(memories))
					// Use all memories that passed the similarity threshold
					relevantMemories := memories

					if len(relevantMemories) > 0 {
						// Log each memory being used
						for i, mem := range relevantMemories {
							preview := mem.Content
							if len(preview) > 100 {
								preview = preview[:100] + "..."
							}
							similarity := float32(0)
							if mem.Metadata != nil {
								if sim, ok := mem.Metadata["similarity"].(float32); ok {
									similarity = sim
								}
							}
							log.Printf("[Memory] #%d: similarity=%.3f, source=%s, category=%s, preview=%q", i+1, similarity, mem.Source, mem.Category, preview)
						}

						// Format and inject memories into the conversation
						memoryContext := memory.FormatMemoryContext(relevantMemories)
						log.Printf("[Memory] Injecting %d memories into chat context", len(relevantMemories))

						// Add memory context to the LAST user message (not the first!)
						// Injecting into the first user message would mutate early messages
						// on every request (different queries → different memories), breaking
						// Anthropic's prompt cache prefix matching for all subsequent messages.
						if len(messages) > 0 {
							for i := len(messages) - 1; i >= 0; i-- {
								if messages[i].Role == "user" {
									switch content := messages[i].Content.(type) {
									case string:
										messages[i].Content = memoryContext + "\n\n" + content
									}
									break
								}
							}
						}

						// Publish memory retrieval event
						eventBus.Publish(events.NewEvent(events.CategoryMemory, "memories_injected", events.LevelInfo).
							WithThread(threadID).
							WithData("count", len(relevantMemories)))
					}
				} else {
					log.Printf("[Memory] No relevant memories found for query")
				}
			}
		}
	}

	// Get available agents for system prompt injection (if enabled)
	var availableAgents []config.AgentInfo
	injectionEnabled := config.IsAgentInjectionEnabled(agentConfig.Agents)
	discoveryRunning := discoveryService != nil && discoveryService.IsRunning()

	// DEBUG: Log all the conditions
	log.Printf("🤝 DEBUG injection check: injectionEnabled=%v, discoveryService=%p, discoveryRunning=%v",
		injectionEnabled, discoveryService, discoveryRunning)
	if agentConfig.Agents != nil {
		log.Printf("🤝 DEBUG agents config: enabled=%v, mode=%s, group=%s, inject_into_prompt=%v",
			agentConfig.Agents.Enabled, agentConfig.Agents.Mode, agentConfig.Agents.Group, agentConfig.Agents.InjectIntoPrompt)
	} else {
		log.Printf("🤝 DEBUG agents config is NIL")
	}

	if injectionEnabled && discoveryRunning {
		log.Printf("🤝 DEBUG: calling discoveryService.GetAgents()...")
		availableAgents = discoveryService.GetAgents()
		log.Printf("🤝 Agent injection: %d agents available for prompt", len(availableAgents))
		for _, agent := range availableAgents {
			log.Printf("🤝 DEBUG: injecting agent %s (%s) at %s", agent.Name, agent.ID, agent.URL)
		}
	} else {
		log.Printf("🤝 Agent injection skipped: enabled=%v, discovery_running=%v", injectionEnabled, discoveryRunning)
	}

	// Combine compaction summary with request system context
	var systemContext interface{} = chatReq.System
	if compactionSummary != "" {
		compactionContext := fmt.Sprintf("## Previous Conversation Context\n\n%s", compactionSummary)
		switch existing := chatReq.System.(type) {
		case string:
			if existing != "" {
				systemContext = existing + "\n\n" + compactionContext
			} else {
				systemContext = compactionContext
			}
		case []string:
			systemContext = append(existing, compactionContext)
		case nil:
			systemContext = compactionContext
		default:
			systemContext = compactionContext
		}
	}

	// Inject setup mode prefix if enabled
	if agentConfig.SetupMode {
		setupPrefix := tools.GetSetupModeSystemPromptPrefix(&agentConfig)
		switch existing := systemContext.(type) {
		case string:
			systemContext = setupPrefix + existing
		case nil:
			systemContext = setupPrefix
		default:
			systemContext = setupPrefix
		}
		log.Printf("🔧 Setup mode: Injected config into system prompt")
	}

	// Inject all skills with full instructions if enabled
	skillsManager := skills.GetManager()
	if skillsManager.IsEnabled() && skillsManager.Count() > 0 {
		skillsSection := skillsManager.BuildAllSkillsPrompt()
		switch existing := systemContext.(type) {
		case string:
			systemContext = existing + skillsSection
		case nil:
			systemContext = skillsSection
		default:
			systemContext = skillsSection
		}
		log.Printf("🎯 Skills: Injected %d skills into system prompt", skillsManager.Count())
	}

	// Add system prompt with current context, MCP credentials, task management config, request system context, and available agents
	messages = stream.PrepareMessagesWithFullConfig(messages, llmConfig, agentConfig.Name, agentConfig.Description, agentConfig.MCP, agentConfig.Tasks, agentConfig.Scheduler, systemContext, availableAgents)

	// Store conversation for memory extraction (non-blocking)
	// Only extract if memory is enabled AND auto_extract_memories is enabled (defaults to true)
	if memoryManager != nil && config.IsAutoExtractMemoriesEnabled(agentConfig.Memory) {
		// Save initial messages for memory extraction after response
		initialMessages := make([]interface{}, len(messages))
		for i, msg := range messages {
			initialMessages[i] = msg
		}

		// Schedule memory extraction after response completes
		defer func() {
			go func() {
				// Wait a bit to ensure response is saved
				time.Sleep(2 * time.Second)

				// Get the updated thread messages including the assistant's response
				if threadMessages, err := messageSaver.GetThreadMessages(threadID); err == nil {
					// Convert to interface slice for memory extraction
					allMessages := make([]interface{}, len(threadMessages))
					for i, msg := range threadMessages {
						allMessages[i] = map[string]interface{}{
							"role":    msg.Role,
							"content": msg.Content,
						}
					}

					// Extract and store memories
					if candidates, err := memoryManager.ShouldRemember(allMessages, threadID); err == nil && len(candidates) > 0 {
						// Get the last message ID for reference
						var lastMessageID string
						if len(threadMessages) > 0 {
							lastMessageID = threadMessages[len(threadMessages)-1].ID
						}

						if err := memoryManager.StoreMemories(candidates, threadID, lastMessageID); err != nil {
							log.Printf("Failed to store memories: %v", err)
						} else {
							log.Printf("Stored %d new memories for thread %s", len(candidates), threadID)
						}
					}
				}
			}()
		}()
	}

	// Get custom tool definitions if configured
	var customTools []tools.ToolDefinition
	if len(llmConfig.Tools) > 0 {
		customTools = tools.GetToolsForNames(llmConfig.Tools)
		log.Printf("🔧 Tools configured: %v, resolved: %d tools", llmConfig.Tools, len(customTools))
		for _, t := range customTools {
			log.Printf("   - %s", t.Name)
		}
	}

	// Add config tools if setup mode is enabled
	if agentConfig.SetupMode {
		configTools := tools.GetToolsForNames([]string{"config_set"})
		customTools = append(customTools, configTools...)
		log.Printf("🔧 Setup mode: Added config tools (config_set)")
	}

	// Add operator session tools if operator mode is enabled (skip any already loaded from config)
	if agentConfig.Operator != nil && agentConfig.Operator.Enabled {
		existingToolNames := make(map[string]bool)
		for _, t := range customTools {
			existingToolNames[t.Name] = true
		}
		operatorToolNames := []string{"create_operator_session", "list_operator_sessions", "close_operator_session", "high_quality_screenshot"}
		var added int
		for _, name := range operatorToolNames {
			if !existingToolNames[name] {
				if resolved := tools.GetToolsForNames([]string{name}); len(resolved) > 0 {
					customTools = append(customTools, resolved[0])
					added++
				}
			}
		}
		if added > 0 {
			log.Printf("🖥️ Operator mode: Added %d session tools", added)
		}
	}

	// Get MCP tool definitions if configured
	mcpConfig := cfg.Get().MCP
	if mcpConfig != nil && mcpConfig.Enabled {
		enabledMCPTools := mcp.GetEnabledMCPTools(mcpConfig)
		log.Printf("🔧 MCP tools enabled: %d (external)", len(enabledMCPTools))
		for _, t := range enabledMCPTools {
			log.Printf("   - %s (%s)", t.Name, t.ServerName)
		}
		mcpToolDefs := mcp.ConvertMCPToolsToToolDefinitions(enabledMCPTools)
		customTools = append(customTools, mcpToolDefs...)

		// Auto-enable MCP tools referenced by skills
		if skillsManager.IsEnabled() && skillsManager.Count() > 0 {
			// Collect tool names from all enabled skills
			var skillToolNames []string
			toolSet := make(map[string]bool)
			for _, s := range skillsManager.GetAll() {
				for _, t := range s.Tools {
					if !toolSet[t] {
						toolSet[t] = true
						skillToolNames = append(skillToolNames, t)
					}
				}
			}
			if len(skillToolNames) > 0 {
				// Get additional tools from skills that aren't already loaded
				existingTools := make(map[string]bool)
				for _, t := range mcpToolDefs {
					existingTools[t.Name] = true
				}

				var additionalTools []string
				for _, toolName := range skillToolNames {
					if !existingTools[toolName] {
						additionalTools = append(additionalTools, toolName)
					}
				}

				if len(additionalTools) > 0 {
					// Create temporary config with skill tools added
					skillMCPConfig := *mcpConfig
					skillMCPConfig.Tools = append(skillMCPConfig.Tools, additionalTools...)
					skillToolDefs := mcp.ConvertMCPToolsToToolDefinitions(mcp.GetEnabledMCPTools(&skillMCPConfig))

					// Only add the new tools (not duplicates)
					for _, t := range skillToolDefs {
						if !existingTools[t.Name] {
							customTools = append(customTools, t)
							existingTools[t.Name] = true
						}
					}
					log.Printf("🎯 Skills: Auto-enabled %d additional MCP tools: %v", len(additionalTools), additionalTools)
				}
			}
		}
	}

	// Store all tools before filtering for manifest generation
	allAvailableTools := customTools
	log.Printf("🔧 Total tools available: %d", len(allAvailableTools))

	// Apply on-demand tool loading if configured
	toolLoadingConfig := llmConfig.ToolLoading
	if toolLoadingConfig != nil && toolLoadingConfig.Mode == "on_demand" {
		log.Printf("🔧 On-demand mode: strategy=%s, message=%q", toolLoadingConfig.Strategy, messageText)
		customTools = tools.LoadToolsForMessage(messageText, customTools)
		log.Printf("🔧 Tools after filtering: %d (from %d)", len(customTools), len(allAvailableTools))
		for _, t := range customTools {
			log.Printf("   - %s", t.Name)
		}

		// Generate tool manifest for system prompt so agent knows all available tools
		toolManifest := tools.GenerateToolManifest(allAvailableTools)
		if toolManifest != "" {
			log.Printf("🔧 Injecting tool manifest into system prompt (%d tools)", len(allAvailableTools))
			// Inject manifest into system context
			manifestContext := "\n\n## Available Tools\nYou have access to these tools. Only the most relevant ones are loaded for this request, but you can request others by name:\n" + toolManifest
			switch ctx := systemContext.(type) {
			case string:
				systemContext = ctx + manifestContext
			case nil:
				systemContext = manifestContext
			}
		}
	} else {
		log.Printf("🔧 Full mode: loading all %d tools", len(customTools))
	}

	// Get built-in tools if configured
	var builtinTools []interface{}
	for _, builtinTool := range llmConfig.BuiltinTools {
		builtinTools = append(builtinTools, builtinTool)
	}

	// Get provider and processor based on configuration
	var provider stream.Provider
	var processor stream.StreamProcessor
	var model *string

	switch llmConfig.Provider {
	case "anthropic":
		if !anthropicProvider.IsConfigured() {
			http.Error(w, "ANTHROPIC_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = anthropicProvider
		processor = &stream.AnthropicProcessor{}
		model = &llmConfig.Model
	case "openai":
		if !openaiProvider.IsConfigured() {
			http.Error(w, "OPENAI_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = openaiProvider
		processor = &stream.OpenAIProcessor{}
		model = &llmConfig.Model
	case "groq":
		if !groqProvider.IsConfigured() {
			http.Error(w, "GROQ_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = groqProvider
		processor = &stream.OpenAIProcessor{} // Groq uses OpenAI-compatible format
		model = &llmConfig.Model
	case "gemini":
		if !geminiProvider.IsConfigured() {
			http.Error(w, "GEMINI_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = geminiProvider
		processor = stream.NewGeminiStreamProcessor()
		model = &llmConfig.Model
	case "fireworks":
		if !fireworksProvider.IsConfigured() {
			http.Error(w, "FIREWORKS_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = fireworksProvider
		openaiProc := &stream.OpenAIProcessor{} // Fireworks uses OpenAI-compatible format
		// Log thinking model detection for debugging
		modelLower := strings.ToLower(llmConfig.Model)
		if strings.Contains(modelLower, "minimax") || strings.Contains(modelLower, "thinking") || strings.Contains(modelLower, "reasoning") {
			log.Printf("🧠 Fireworks: thinking model detected: %s (reasoning_history will be set)", llmConfig.Model)
		}
		processor = openaiProc
		model = &llmConfig.Model
	case "xai":
		if !xaiProvider.IsConfigured() {
			http.Error(w, "XAI_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = xaiProvider
		processor = &stream.OpenAIProcessor{} // xAI uses OpenAI-compatible format
		model = &llmConfig.Model
	case "zai":
		if !zaiProvider.IsConfigured() {
			http.Error(w, "ZAI_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = zaiProvider
		processor = &stream.OpenAIProcessor{} // Z.ai uses OpenAI-compatible format
		model = &llmConfig.Model
	case "novita":
		if !novitaProvider.IsConfigured() {
			http.Error(w, "NOVITA_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = novitaProvider
		processor = &stream.OpenAIProcessor{} // Novita uses OpenAI-compatible format
		model = &llmConfig.Model
	case "venice":
		if !veniceProvider.IsConfigured() {
			http.Error(w, "VENICE_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = veniceProvider
		processor = &stream.OpenAIProcessor{} // Venice uses OpenAI-compatible format
		model = &llmConfig.Model
	case "moonshot":
		if !moonshotProvider.IsConfigured() {
			http.Error(w, "MOONSHOT_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = moonshotProvider
		processor = &stream.OpenAIProcessor{} // Moonshot uses OpenAI-compatible format
		model = &llmConfig.Model
	case "together":
		if !togetherProvider.IsConfigured() {
			http.Error(w, "TOGETHER_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = togetherProvider
		openaiProc := &stream.OpenAIProcessor{}
		// Together AI K2.5 embeds thinking in content with </think> tags (no opening tag)
		// So we need to start in thinking mode for these models
		modelLower := strings.ToLower(llmConfig.Model)
		if strings.Contains(modelLower, "k2.5") || strings.Contains(modelLower, "k2-5") {
			openaiProc.SetStartInThinkMode(true)
			log.Printf("🧠 Together AI: Starting in think mode for model %s", llmConfig.Model)
		}
		processor = openaiProc
		model = &llmConfig.Model
	case "mistral":
		if !mistralProvider.IsConfigured() {
			http.Error(w, "MISTRAL_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = mistralProvider
		processor = &stream.OpenAIProcessor{} // Mistral uses OpenAI-compatible format
		model = &llmConfig.Model
	case "ollama":
		// Apply base_url from config if set (debug UI)
		if llmConfig.BaseURL != "" {
			ollamaProvider.SetBaseURL(llmConfig.BaseURL + "/v1")
		}
		if !ollamaProvider.IsConfigured() {
			http.Error(w, "OLLAMA_BASE_URL not set", http.StatusInternalServerError)
			return
		}
		provider = ollamaProvider
		processor = &stream.OpenAIProcessor{} // Ollama uses OpenAI-compatible format
		model = &llmConfig.Model
	case "cerebras":
		if !cerebrasProvider.IsConfigured() {
			http.Error(w, "CEREBRAS_API_KEY not set", http.StatusInternalServerError)
			return
		}
		provider = cerebrasProvider
		processor = &stream.OpenAIProcessor{} // Cerebras uses OpenAI-compatible format
		model = &llmConfig.Model
	default:
		http.Error(w, "Unsupported provider: "+llmConfig.Provider, http.StatusBadRequest)
		return
	}

	// Filter unsupported media (video) for non-Gemini providers
	filterVideoForProvider(messages, llmConfig.Provider)

	// Check if raw mode is requested
	if chatReq.RawMode != nil && *chatReq.RawMode {
		// Raw mode: bypass processor and stream raw LLM chunks directly
		rawStream, err := provider.GetRawStream(messages, customTools, builtinTools)
		if err != nil {
			log.Printf("Error getting raw stream: %v", err)
			http.Error(w, "Error getting stream", http.StatusInternalServerError)
			return
		}
		defer rawStream.Close()

		// Set headers for raw streaming
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

		// Send thread ID and request ID as first lines in raw mode
		fmt.Fprintf(w, "thread_id: %s\n", threadID)
		fmt.Fprintf(w, "request_id: %s\n", requestID)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// Stream raw chunks directly from LLM (bypass processor)
		scanner := bufio.NewScanner(rawStream)
		scanner.Buffer(make([]byte, 64*1024), 2*1024*1024) // Start 64KB, grow to 2MB max for large server tool results
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(w, "%s\n", line)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("Error reading raw stream: %v", err)
			return
		}
	} else {
		// Check if non-streaming mode is requested (for agent-to-agent communication)
		streamMode := true
		if chatReq.Stream != nil {
			streamMode = *chatReq.Stream
		}

		// Normal mode: use unified tool conversation handling with message saving
		// Publish LLM request event
		llmStartTime := time.Now()
		llmRequestEvent := events.NewEvent(events.CategoryLLM, events.TypeLLMRequest, events.LevelInfo).
			WithThread(threadID).
			WithData("provider", llmConfig.Provider).
			WithData("model", llmConfig.Model).
			WithData("max_tokens", llmConfig.MaxTokens).
			WithData("message_count", len(messages)).
			WithData("tools_count", len(customTools)+len(builtinTools)).
			WithData("stream_mode", streamMode)
		eventBus.Publish(llmRequestEvent)

		// Publish chat response started event
		responseStartEvent := events.NewEvent(events.CategoryChat, events.TypeChatResponseStarted, events.LevelInfo).
			WithThread(threadID)
		eventBus.Publish(responseStartEvent)

		if !streamMode {
			// Non-streaming mode: collect complete response and return JSON
			// Use a custom response writer to capture the response
			responseBuffer := &ResponseCapture{ResponseWriter: w}

			fileAdapter := &FileManagerAdapter{fm: fileManager}
		if err := stream.UnifiedToolConversationWithBuiltins(responseBuffer, provider, processor, messages, customTools, builtinTools, messageSaver, threadID, model, fileAdapter, taskID); err != nil {
				log.Printf("Error in unified tool conversation: %v", err)

				// Publish LLM error event
				llmErrorEvent := events.NewEvent(events.CategoryLLM, events.TypeLLMError, events.LevelError).
					WithThread(threadID).
					WithError(err).
					WithDuration(llmStartTime)
				eventBus.Publish(llmErrorEvent)

				// Return JSON error for non-streaming
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success":    false,
					"error":      err.Error(),
					"thread_id":  threadID,
					"request_id": requestID,
					"trace_id":   trace.ID,
				})
				return
			}

			// Extract the complete response text from captured events
			completeResponse := responseBuffer.ExtractCompleteResponse()
			contentBlocks := responseBuffer.ExtractContentBlocks()
			usage := responseBuffer.ExtractUsage()

			// Publish chat response complete event
			responseCompleteEvent := events.NewEvent(events.CategoryChat, events.TypeChatResponseComplete, events.LevelInfo).
				WithThread(threadID).
				WithData("source", string(trace.Source)).
				WithDuration(llmStartTime)
			eventBus.Publish(responseCompleteEvent)

			// Fetch messages from database for this thread
			threadMessages, err := messageSaver.GetThreadMessages(threadID)
			if err != nil {
				log.Printf("Error fetching thread messages: %v", err)
				// Don't fail the request, just continue without messages
			}

			// Return complete response as JSON
			w.Header().Set("Content-Type", "application/json")
			response := map[string]interface{}{
				"success":    true,
				"response":   completeResponse, // Backward compatible: text only
				"content":    contentBlocks,    // Full structured content with tool calls
				"thread_id":  threadID,
				"request_id": requestID,
				"trace_id":   trace.ID,
				"model":      llmConfig.Model,
			}
			if usage != nil {
				response["usage"] = usage
			}
			if threadMessages != nil {
				response["messages"] = threadMessages
			}
			json.NewEncoder(w).Encode(response)
		} else {
			// Streaming mode: normal SSE streaming with cancellation support
			fileAdapter := &FileManagerAdapter{fm: fileManager}
			if err := stream.UnifiedToolConversationWithContext(requestCtx, w, provider, processor, messages, customTools, builtinTools, messageSaver, threadID, requestID, model, fileAdapter, taskID); err != nil {
				// Check if error is due to cancellation
				if requestCtx.Err() != nil {
					log.Printf("Request cancelled: %v", err)
					return
				}
				log.Printf("Error in unified tool conversation: %v", err)

				// Publish LLM error event
				llmErrorEvent := events.NewEvent(events.CategoryLLM, events.TypeLLMError, events.LevelError).
					WithThread(threadID).
					WithError(err).
					WithDuration(llmStartTime)
				eventBus.Publish(llmErrorEvent)

				http.Error(w, "Error processing chat request", http.StatusInternalServerError)
				return
			}

			// Publish chat response complete event
			responseCompleteEvent := events.NewEvent(events.CategoryChat, events.TypeChatResponseComplete, events.LevelInfo).
				WithThread(threadID).
				WithData("source", string(trace.Source)).
				WithDuration(llmStartTime)
			eventBus.Publish(responseCompleteEvent)
		}

		// Generate thread activity summary in background
		{
			summaryThreadID := threadID
			summaryIsNew := isNewThread
			mainProvider := agentConfig.LLM.Provider
			go func() {
				time.Sleep(2 * time.Second)
				summarizer := stream.NewSummarizer(db, mainProvider)
				threadMessages, err := messageSaver.GetThreadMessages(summaryThreadID)
				if err != nil {
					log.Printf("Failed to get messages for summary: %v", err)
					return
				}
				// Convert to stream.Message
				streamMessages := make([]stream.Message, len(threadMessages))
				for i, msg := range threadMessages {
					streamMessages[i] = stream.Message{
						Role:    msg.Role,
						Content: msg.Content,
					}
				}
				result, err := summarizer.GenerateThreadSummary(summaryThreadID, streamMessages)
				if err != nil {
					log.Printf("Failed to generate thread summary: %v", err)
					return
				}
				var titlePtr *string
				if summaryIsNew && result.Title != "" {
					titlePtr = &result.Title
				}
				if err := messageSaver.UpdateThreadActivity(summaryThreadID, result.Activity, titlePtr); err != nil {
					log.Printf("❌ Failed to update thread activity: %v", err)
				} else {
					log.Printf("Updated thread %s activity: %s", summaryThreadID, result.Activity)

					// Emit thread activity event with type and parent info
					bus := events.GetEventBus()
					log.Printf("🔔 DEBUG thread_activity: emitting event for thread=%s, subscribers=%d, bus_running=%v",
						summaryThreadID, bus.SubscriberCount(), bus.IsRunning())
					activityEvent := events.NewEvent(events.CategoryChat, "thread_activity", events.LevelInfo).
						WithThread(summaryThreadID).
						WithData("activity", result.Activity)
					if titlePtr != nil {
						activityEvent.WithData("title", result.Title)
					}
					// Add thread type and parent_id
					var threadType, parentID sql.NullString
					if err := db.QueryRow("SELECT COALESCE(type, 'chat'), parent_id FROM threads WHERE id = ?", summaryThreadID).Scan(&threadType, &parentID); err == nil {
						activityEvent.WithData("thread_type", threadType.String)
						if parentID.Valid {
							activityEvent.WithData("parent_id", parentID.String)
						}
					}
					if err := bus.Publish(activityEvent); err != nil {
						log.Printf("❌ DEBUG thread_activity: publish failed: %v", err)
					} else {
						log.Printf("✅ DEBUG thread_activity: event published id=%s", activityEvent.ID)
					}
				}
			}()
		}

		// Background compaction: check if context needs compacting after response
		if agentConfig.Context != nil && agentConfig.Context.Compaction != nil && agentConfig.Context.Compaction.Enabled {
			compactThreadID := threadID
			compactMaxTokens := effectiveMaxTokens
			compactConfig := agentConfig.Context.Compaction
			go func() {
				// Small delay to let message saving complete
				time.Sleep(3 * time.Second)

				compactor := stream.NewCompactor(
					compactConfig,
					compactMaxTokens,
					db,
					os.Getenv("ANTHROPIC_API_KEY"),
				)

				// Reload all messages to check if compaction is needed
				allMessages, err := messageSaver.GetThreadMessages(compactThreadID)
				if err != nil {
					log.Printf("⚠️ Background compaction: failed to load messages: %v", err)
					return
				}

				// Convert to stream.Message
				var streamMsgs []stream.Message
				for _, msg := range allMessages {
					sm, err := stream.ConvertDatabaseMessageToStreamMessageWithMetadata(msg.Role, msg.Content, msg.Metadata)
					if err != nil {
						streamMsgs = append(streamMsgs, stream.Message{Role: msg.Role, Content: msg.Content})
					} else {
						streamMsgs = append(streamMsgs, sm)
					}
				}

				if compactor.ShouldCompact(streamMsgs) {
					summary, _, err := compactor.Compact(compactThreadID, streamMsgs)
					if err != nil {
						log.Printf("⚠️ Background compaction failed for thread %s: %v", compactThreadID, err)
					} else {
						log.Printf("📦 Background compaction complete for thread %s (%d chars summary)", compactThreadID, len(summary))
					}
				}
			}()
		}
	}
}

func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Delete all memories first (to avoid foreign key issues)
	memoryDeleteStart := time.Now()
	memoryResult, err := db.Exec("DELETE FROM memories")
	if err != nil {
		log.Printf("Error deleting memories: %v", err)
		// Continue anyway as this is not critical
	}
	memoryRows, _ := memoryResult.RowsAffected()

	// Publish database delete event for memories
	eventBus := events.GetEventBus()
	memoryDeleteEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBDelete, events.LevelInfo).
		WithData("table", "memories").
		WithData("operation", "DELETE").
		WithData("rows_affected", memoryRows).
		WithData("query", "DELETE FROM memories").
		WithDuration(memoryDeleteStart)
	eventBus.Publish(memoryDeleteEvent)

	// Delete all tasks (no foreign key constraints on tasks)
	taskDeleteStart := time.Now()
	taskResult, err := db.Exec("DELETE FROM tasks")
	if err != nil {
		log.Printf("Error deleting tasks: %v", err)
		http.Error(w, "Failed to clear tasks", http.StatusInternalServerError)
		return
	}
	taskRows, _ := taskResult.RowsAffected()

	// Publish database delete event for tasks
	taskDeleteEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBDelete, events.LevelInfo).
		WithData("table", "tasks").
		WithData("operation", "DELETE").
		WithData("rows_affected", taskRows).
		WithData("query", "DELETE FROM tasks").
		WithDuration(taskDeleteStart)
	eventBus.Publish(taskDeleteEvent)

	// Delete all messages (due to foreign key constraint with threads)
	deleteStart := time.Now()
	messageResult, err := db.Exec("DELETE FROM messages")
	if err != nil {
		log.Printf("Error deleting messages: %v", err)
		http.Error(w, "Failed to clear messages", http.StatusInternalServerError)
		return
	}
	messageRows, _ := messageResult.RowsAffected()

	// Publish database delete event for messages
	deleteEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBDelete, events.LevelInfo).
		WithData("table", "messages").
		WithData("operation", "DELETE").
		WithData("rows_affected", messageRows).
		WithData("query", "DELETE FROM messages").
		WithDuration(deleteStart)
	eventBus.Publish(deleteEvent)

	// Delete all threads
	threadDeleteStart := time.Now()
	threadResult, err := db.Exec("DELETE FROM threads")
	if err != nil {
		log.Printf("Error deleting threads: %v", err)
		http.Error(w, "Failed to clear threads", http.StatusInternalServerError)
		return
	}
	threadRows, _ := threadResult.RowsAffected()

	// Publish database delete event for threads
	threadDeleteEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBDelete, events.LevelInfo).
		WithData("table", "threads").
		WithData("operation", "DELETE").
		WithData("rows_affected", threadRows).
		WithData("query", "DELETE FROM threads").
		WithDuration(threadDeleteStart)
	eventBus.Publish(threadDeleteEvent)

	// Delete all events (before spans and traces due to foreign keys)
	eventsDeleteStart := time.Now()
	eventsResult, err := db.Exec("DELETE FROM events")
	if err != nil {
		log.Printf("Error deleting events: %v", err)
		// Continue anyway as this is not critical
	}
	eventsRows, _ := eventsResult.RowsAffected()

	// Publish database delete event for events
	eventsDeleteEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBDelete, events.LevelInfo).
		WithData("table", "events").
		WithData("operation", "DELETE").
		WithData("rows_affected", eventsRows).
		WithData("query", "DELETE FROM events").
		WithDuration(eventsDeleteStart)
	eventBus.Publish(eventsDeleteEvent)

	// Delete all spans (before traces due to foreign keys)
	spansDeleteStart := time.Now()
	spansResult, err := db.Exec("DELETE FROM spans")
	if err != nil {
		log.Printf("Error deleting spans: %v", err)
		// Continue anyway as this is not critical
	}
	spansRows, _ := spansResult.RowsAffected()

	// Publish database delete event for spans
	spansDeleteEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBDelete, events.LevelInfo).
		WithData("table", "spans").
		WithData("operation", "DELETE").
		WithData("rows_affected", spansRows).
		WithData("query", "DELETE FROM spans").
		WithDuration(spansDeleteStart)
	eventBus.Publish(spansDeleteEvent)

	// Delete all traces
	tracesDeleteStart := time.Now()
	tracesResult, err := db.Exec("DELETE FROM traces")
	if err != nil {
		log.Printf("Error deleting traces: %v", err)
		// Continue anyway as this is not critical
	}
	tracesRows, _ := tracesResult.RowsAffected()

	// Publish database delete event for traces
	tracesDeleteEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBDelete, events.LevelInfo).
		WithData("table", "traces").
		WithData("operation", "DELETE").
		WithData("rows_affected", tracesRows).
		WithData("query", "DELETE FROM traces").
		WithDuration(tracesDeleteStart)
	eventBus.Publish(tracesDeleteEvent)

	// Reinitialize memory manager if needed
	if memoryManager != nil {
		memoryManager, _ = memory.NewMemoryManager(db, config.GetConfig().Get().Memory, eventBus)
	}

	log.Printf("Database reset completed - cleared %d memories, %d tasks, %d messages, %d threads, %d events, %d spans, %d traces",
		memoryRows, taskRows, messageRows, threadRows, eventsRows, spansRows, tracesRows)
	sendJSON(w, http.StatusOK, map[string]string{
		"message": fmt.Sprintf("Database cleared successfully - removed %d memories, %d tasks, %d messages, %d threads, %d events, %d spans, %d traces",
			memoryRows, taskRows, messageRows, threadRows, eventsRows, spansRows, tracesRows),
		"status":  "completed",
	})
}

func handleWebInterface(w http.ResponseWriter, r *http.Request) {
	// Only serve on root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Serve embedded chat interface (always available, even in Docker)
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(web.EmbeddedChatHTML))
}

// handleDebugInterface serves the full debug UI (development mode only)
func handleDebugInterface(w http.ResponseWriter, r *http.Request) {
	// Check if web interface file exists (development mode)
	webFile := "./web/index.html"
	if _, err := os.Stat(webFile); os.IsNotExist(err) {
		// Web interface not available (production mode)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "Debug interface not available",
			"message": "The debug interface is only available in development mode",
			"hint":    "Use the chat interface at / instead",
		})
		return
	}

	// Serve the HTML file
	w.Header().Set("Content-Type", "text/html")
	http.ServeFile(w, r, webFile)
}

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}

// ============================================================================
// MCP (Model Context Protocol) Implementation
// ============================================================================

// MCP API handlers


// initScheduler initializes the scheduler if enabled in configuration
func initScheduler() {
	cfg := config.GetConfig()
	schedulerConfig := cfg.Get().Scheduler

	if schedulerConfig == nil || !schedulerConfig.Enabled {
		log.Println("Scheduler not enabled, skipping initialization")
		return
	}

	// Convert config.SchedulerConfig to scheduler.SchedulerConfig
	schedulerCfg := scheduler.SchedulerConfig{
		Enabled:  schedulerConfig.Enabled,
		Interval: schedulerConfig.Interval,
		MaxTasks: schedulerConfig.MaxTasks,
	}

	agentScheduler = scheduler.GetScheduler(schedulerCfg)
	agentScheduler.SetDatabase(db)  // Pass database to scheduler
	if err := agentScheduler.Start(); err != nil {
		log.Printf("Warning: Failed to start scheduler: %v", err)
		return
	}
}

// initReflection initializes the reflection scheduler if enabled in configuration
func initReflection() {
	cfg := config.GetConfig()
	reflectionConfig := cfg.Get().Reflection

	if reflectionConfig == nil || !reflectionConfig.Enabled {
		log.Println("Reflection not enabled, skipping initialization")
		return
	}

	agentReflection = reflection.GetReflectionScheduler(reflectionConfig)
	agentReflection.SetDatabase(db)
	if err := agentReflection.Start(); err != nil {
		log.Printf("Warning: Failed to start reflection: %v", err)
		return
	}
}

// getOutboundIP gets the preferred outbound IP address of this machine
// This is used for agent discovery to broadcast the correct URL
func getOutboundIP() string {
	// Try to get from environment variable first (Docker/K8s override)
	if envIP := os.Getenv("AGENT_IP"); envIP != "" {
		return envIP
	}

	// Get actual network IP by dialing (doesn't actually connect)
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Printf("Failed to determine outbound IP: %v", err)
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// getAgentURL constructs the agent's URL for discovery broadcasting
func getAgentURL() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4015"
	}

	// Check config for public URL first
	cfg := config.GetConfig()
	agentConfig := cfg.Get()
	if agentConfig.PublicURL != "" {
		log.Printf("Agent URL for discovery (from config): %s", agentConfig.PublicURL)
		return agentConfig.PublicURL
	}

	// Check if explicit URL is provided via env var
	if explicitURL := os.Getenv("AGENT_URL"); explicitURL != "" {
		log.Printf("Agent URL for discovery (from AGENT_URL): %s", explicitURL)
		return explicitURL
	}

	// Get actual network IP (works in Docker)
	ip := getOutboundIP()

	url := fmt.Sprintf("http://%s:%s", ip, port)
	log.Printf("Agent URL for discovery (auto-detected): %s", url)
	return url
}

func initAgentCommunication() {
	cfg := config.GetConfig()
	agentConfig := cfg.Get()

	if agentConfig.Agents == nil || !agentConfig.Agents.Enabled {
		log.Println("Agent communication not enabled, skipping initialization")
		return
	}

	log.Println("Initializing agent communication...")

	// Start discovery service if group is configured
	if agentConfig.Agents.Group != "" || agentConfig.Agents.GossipSeed != "" || len(agentConfig.Agents.AvailableAgents) > 0 {
		// Get agent URL with actual network IP (works in Docker)
		agentURL := getAgentURL()

		// Build features map for discovery announcement
		features := config.BuildFeatures(&agentConfig)

		// Get MCP server names for discovery announcement
		var mcpServerNames []string
		if agentConfig.MCP != nil && agentConfig.MCP.Enabled {
			for _, server := range agentConfig.MCP.Servers {
				if server.Name != "" {
					mcpServerNames = append(mcpServerNames, server.Name)
				}
			}
		}

		// Create discovery service
		var err error
		discoveryService, err = discovery.NewDiscoveryService(
			agentConfig.Agents,
			agentConfig.ID,
			agentConfig.Name,
			agentConfig.Description,
			agentURL,
			features,
			mcpServerNames,
		)

		if err != nil {
			log.Printf("Failed to create discovery service: %v", err)
		} else if discoveryService != nil {
			log.Printf("🔍 DEBUG: discoveryService created, pointer=%p", discoveryService)
			// Start discovery
			if err := discoveryService.Start(); err != nil {
				log.Printf("Failed to start discovery service: %v", err)
			} else {
				log.Printf("Discovery service started successfully")
				log.Printf("🔍 DEBUG: discoveryService after start, pointer=%p, running=%v", discoveryService, discoveryService.IsRunning())
			}
		}
	}

	// Create agent client (will use discovery service for agent list if available)
	agentClient = agents.NewAgentClient(agentConfig.Agents, events.GetEventBus(), db, agentConfig.ID)

	// Inject discovery service into agent client
	if discoveryService != nil {
		agentClient.SetDiscoveryService(discoveryService)
	}

	// Register agent tools
	registerAgentTools()

	// Count agents (from discovery or manual config)
	// For discovery mode, the count will update dynamically as agents are found
	var enabledCount int
	if discoveryService != nil && discoveryService.IsRunning() {
		// Discovery is async, agents will be found shortly
		log.Printf("Agent communication enabled (discovery active, agents will be discovered)")
	} else {
		for _, agent := range agentConfig.Agents.AvailableAgents {
			if agent.Enabled {
				enabledCount++
			}
		}
		log.Printf("Agent communication enabled with %d configured agents", enabledCount)
	}

	// Publish initialization event
	initEvent := events.NewEvent(events.CategoryAgent, "agent_comm_initialized", events.LevelInfo).
		WithData("available_agents", enabledCount).
		WithData("discovery_enabled", discoveryService != nil).
		WithData("settings", agentConfig.Agents.Settings)
	events.GetEventBus().Publish(initEvent)
}

// registerAgentTools registers the agent communication tools with the global tool registry
// This is called both at startup and when enabling agent communication dynamically
func registerAgentTools() {
	cfg := config.GetConfig().Get()

	// Only register agent tools if this agent can call others (peer or coordinator mode)
	if cfg.Agents != nil && !cfg.Agents.CanCallAgents() {
		log.Printf("🔧 Worker mode: skipping agent tool registration (receive-only)")
		return
	}

	if agentClient == nil {
		log.Printf("⚠️  Cannot register agent tools: agentClient is nil")
		return
	}

	registry := tools.GetGlobalRegistry()

	// Register call_agent tool
	registry.RegisterTool(&agents.CallAgentTool{
		Client: agentClient,
	})

	// Register delegation tools
	registry.RegisterTool(&agents.DelegateTaskTool{
		Client: agentClient,
	})
	registry.RegisterTool(&agents.CheckDelegatedTaskTool{
		Client: agentClient,
	})

	// Register activity tool
	registry.RegisterTool(&agents.GetAgentActivityTool{
		Client: agentClient,
	})

	// Only register list_available_agents tool when discovery is NOT active
	// When discovery is active, agent info is already injected into the system prompt,
	// so the list tool is redundant and can confuse the model
	if discoveryService == nil || !discoveryService.IsRunning() {
		registry.RegisterTool(&agents.ListAvailableAgentsTool{
			Client: agentClient,
		})
		log.Printf("✅ Agent tools registered: call_agent, delegate_task, check_delegated_task, get_agent_activity, list_available_agents")
	} else {
		log.Printf("✅ Agent tools registered: call_agent, delegate_task, check_delegated_task, get_agent_activity (list_available_agents skipped - discovery active)")
	}
}

// UpdateDiscoveryService dynamically starts or stops the discovery service based on config changes
// This allows agent communication to be enabled/disabled without restarting the container
func UpdateDiscoveryService(enabled bool, group string) error {
	log.Printf("🔄 UpdateDiscoveryService called: enabled=%v, group=%s", enabled, group)

	cfg := config.GetConfig()
	agentConfig := cfg.Get()

	// If disabling and service is running, stop it
	if !enabled || group == "" {
		log.Printf("🔄 Checking if discovery service needs to stop (enabled=%v, group=%s)", enabled, group)
		if discoveryService != nil && discoveryService.IsRunning() {
			log.Printf("🛑 Stopping discovery service (agents disabled or no group)")
			if err := discoveryService.Stop(); err != nil {
				log.Printf("❌ Error stopping discovery service: %v", err)
				return err
			}
			log.Printf("✅ Discovery service stopped successfully")
		} else {
			log.Printf("ℹ️  Discovery service not running, nothing to stop")
		}
		return nil
	}

	// If enabling and service is not running, start it
	if enabled && group != "" {
		log.Printf("🔄 Checking if discovery service needs to start (enabled=%v, group=%s)", enabled, group)

		// If already running with same group, nothing to do
		if discoveryService != nil && discoveryService.IsRunning() {
			log.Printf("ℹ️  Discovery service already running")
			return nil
		}

		// Create new discovery service if needed
		if discoveryService == nil {
			log.Printf("🔧 Creating new discovery service...")
			agentURL := getAgentURL()
			features := config.BuildFeatures(&agentConfig)

			// Get MCP server names for discovery announcement
			var mcpServerNames []string
			if agentConfig.MCP != nil && agentConfig.MCP.Enabled {
				for _, server := range agentConfig.MCP.Servers {
					if server.Name != "" {
						mcpServerNames = append(mcpServerNames, server.Name)
					}
				}
			}

			var err error
			discoveryService, err = discovery.NewDiscoveryService(
				agentConfig.Agents,
				agentConfig.ID,
				agentConfig.Name,
				agentConfig.Description,
				agentURL,
				features,
				mcpServerNames,
			)
			if err != nil {
				log.Printf("❌ Failed to create discovery service: %v", err)
				// Still register agent tools even if discovery fails (for manual agent communication)
				log.Printf("⚠️  Registering agent tools without discovery service...")
				if agentClient == nil {
					agentClient = agents.NewAgentClient(agentConfig.Agents, events.GetEventBus(), db, agentConfig.ID)
				}
				registerAgentTools()
				return err
			}
			log.Printf("✅ Discovery service created (nil check: %v)", discoveryService == nil)
		}

		// Start the discovery service
		log.Printf("🔍 About to start discovery service (nil: %v)", discoveryService == nil)
		if discoveryService != nil {
			log.Printf("🚀 Starting discovery service...")
			if err := discoveryService.Start(); err != nil {
				log.Printf("❌ Failed to start discovery service: %v", err)
				// Still register agent tools even if discovery start fails
				log.Printf("⚠️  Registering agent tools despite discovery start failure...")
				if agentClient == nil {
					agentClient = agents.NewAgentClient(agentConfig.Agents, events.GetEventBus(), db, agentConfig.ID)
				}
				registerAgentTools()
				return err
			}
			log.Printf("✅ Discovery service started dynamically (group: %s)", group)

			// Create agent client if not exists and register tools
			if agentClient == nil {
				log.Printf("🔧 Creating agent client for dynamic agent communication...")
				agentClient = agents.NewAgentClient(agentConfig.Agents, events.GetEventBus(), db, agentConfig.ID)
			}

			// Inject discovery service into agent client
			agentClient.SetDiscoveryService(discoveryService)

			// Register agent tools (call_agent only when discovery active)
			registerAgentTools()
		} else {
			log.Printf("⚠️  Discovery service is nil after creation, registering tools anyway...")
			if agentClient == nil {
				agentClient = agents.NewAgentClient(agentConfig.Agents, events.GetEventBus(), db, agentConfig.ID)
			}
			registerAgentTools()
		}
	}

	return nil
}

// GetDiscoveryService returns the current discovery service instance
func GetDiscoveryService() discovery.DiscoveryService {
	return discoveryService
}

// IsDiscoveryRunning returns true if the discovery service is currently running
func IsDiscoveryRunning() bool {
	return discoveryService != nil && discoveryService.IsRunning()
}

// UpdateMemoryManager dynamically enables or disables the memory manager
func UpdateMemoryManager(enabled bool) error {
	cfg := config.GetConfig()
	agentConfig := cfg.Get()

	if enabled {
		// Initialize memory manager if not already running
		if memoryManager == nil && agentConfig.Memory != nil {
			var err error
			memoryManager, err = memory.NewMemoryManager(db, agentConfig.Memory, events.GetEventBus())
			if err != nil {
				return fmt.Errorf("failed to initialize memory manager: %w", err)
			}
			log.Printf("✅ Memory manager enabled dynamically")

			// Wire up file ingestion callback if filesystem is already running
			if fileManager != nil && fileManager.IsEnabled() && config.IsMemoryAutoIngestEnabled(agentConfig.Memory) {
				ingestTypes := config.GetMemoryIngestTypes(agentConfig.Memory)
				fileManager.SetIngestionCallback(func(fileID, fileName, mimeType string, data []byte) {
					log.Printf("[Ingest] Callback triggered for: %s (mime: %s)", fileName, mimeType)
					shouldIngest := false
					for _, t := range ingestTypes {
						if strings.HasPrefix(mimeType, t) {
							shouldIngest = true
							break
						}
					}
					if !shouldIngest {
						log.Printf("[Ingest] ⏭️  Skipping %s - mime %s not in ingest_types", fileName, mimeType)
						return
					}
					result, err := memoryManager.IngestFile(fileID, fileName, mimeType, data)
					if err != nil {
						log.Printf("[Ingest] ⚠️ Failed: %v", err)
					} else {
						log.Printf("[Ingest] ✅ Done: %s (%d chunks)", fileName, result.ChunksCount)
					}
				})
				log.Printf("✅ File ingestion callback wired up for memory")
			}

			// Wire up deletion callback to clean up memories when files are deleted
			if fileManager != nil && fileManager.IsEnabled() {
				fileManager.SetDeletionCallback(func(fileID string) {
					if count, err := memoryManager.DeleteMemoriesBySourceID(fileID); err != nil {
						log.Printf("[Delete] ⚠️ Failed to delete memories for file %s: %v", fileID, err)
					} else if count > 0 {
						log.Printf("[Delete] ✅ Cleaned up %d memories for file: %s", count, fileID)
					}
				})
			}
		}
	} else {
		// Memory manager doesn't need explicit shutdown, just nil the reference
		if memoryManager != nil {
			memoryManager = nil
			// Clear the callbacks since memory is disabled
			if fileManager != nil {
				fileManager.SetIngestionCallback(nil)
				fileManager.SetDeletionCallback(nil)
			}
			log.Printf("✅ Memory manager disabled")
		}
	}
	return nil
}

// UpdateFileSystem dynamically enables or disables the filesystem
func UpdateFileSystem(enabled bool) error {
	cfg := config.GetConfig()
	agentConfig := cfg.Get()

	if enabled {
		// Initialize filesystem if not already running
		if fileManager == nil && agentConfig.FileSystem != nil {
			fileManager = filesystem.NewFileManager(db, agentConfig.FileSystem)
			if fileManager.IsEnabled() {
				tools.SetFileManager(fileManager)
				tools.RegisterFilesystemTools()

				// Wire up memory ingestion if memory is enabled
				if memoryManager != nil && config.IsMemoryAutoIngestEnabled(agentConfig.Memory) {
					ingestTypes := config.GetMemoryIngestTypes(agentConfig.Memory)
					fileManager.SetIngestionCallback(func(fileID, fileName, mimeType string, data []byte) {
						log.Printf("[Ingest] Callback triggered for: %s (mime: %s)", fileName, mimeType)
						shouldIngest := false
						for _, t := range ingestTypes {
							if strings.HasPrefix(mimeType, t) {
								shouldIngest = true
								break
							}
						}
						if !shouldIngest {
							log.Printf("[Ingest] ⏭️  Skipping %s - mime %s not in ingest_types", fileName, mimeType)
							return
						}
						result, err := memoryManager.IngestFile(fileID, fileName, mimeType, data)
						if err != nil {
							log.Printf("[Ingest] ⚠️ Failed: %v", err)
						} else {
							log.Printf("[Ingest] ✅ Done: %s (%d chunks)", fileName, result.ChunksCount)
						}
					})
				}

				// Wire up deletion callback to clean up memories when files are deleted
				if memoryManager != nil {
					fileManager.SetDeletionCallback(func(fileID string) {
						if count, err := memoryManager.DeleteMemoriesBySourceID(fileID); err != nil {
							log.Printf("[Delete] ⚠️ Failed to delete memories for file %s: %v", fileID, err)
						} else if count > 0 {
							log.Printf("[Delete] ✅ Cleaned up %d memories for file: %s", count, fileID)
						}
					})
				}

				// Start cleanup job
				if cleanupJob == nil {
					cleanupJob = filesystem.NewCleanupJob(fileManager, 1*time.Hour)
					cleanupJob.Start()
				}
				log.Printf("✅ Filesystem enabled dynamically")
			}
		}
	} else {
		// Stop cleanup job and disable filesystem
		if cleanupJob != nil {
			cleanupJob.Stop()
			cleanupJob = nil
		}
		if fileManager != nil {
			fileManager = nil
			log.Printf("✅ Filesystem disabled")
		}
	}
	return nil
}

// startTelemetry initializes and starts the telemetry subscriber
func startTelemetry(agentConfig config.AgentConfig, eventBus *events.EventBus) {
	if telemetrySubscriber != nil {
		log.Printf("📡 Telemetry already running")
		return
	}

	if agentConfig.Telemetry == nil || agentConfig.Telemetry.Endpoint == "" {
		log.Printf("⚠️  Telemetry endpoint not configured")
		return
	}

	telemetryCfg := events.TelemetryConfig{
		Endpoint:      agentConfig.Telemetry.Endpoint,
		APIKey:        agentConfig.Telemetry.APIKey,
		BatchSize:     agentConfig.Telemetry.BatchSize,
		FlushInterval: agentConfig.Telemetry.FlushInterval,
	}
	telemetrySubscriber = events.NewTelemetrySubscriber(telemetryCfg)

	// Subscribe to events - empty categories means all events
	filters := events.SubscriptionFilters{}
	if len(agentConfig.Telemetry.Categories) > 0 {
		for _, cat := range agentConfig.Telemetry.Categories {
			filters.Categories = append(filters.Categories, events.EventCategory(cat))
		}
	}

	var err error
	telemetrySubID, err = eventBus.SubscribeFunc(filters, telemetrySubscriber.OnEvent)
	if err != nil {
		log.Printf("⚠️  Failed to subscribe telemetry: %v", err)
		telemetrySubscriber.Stop()
		telemetrySubscriber = nil
		return
	}

	log.Printf("📡 Telemetry started: endpoint=%s", agentConfig.Telemetry.Endpoint)
}

// stopTelemetry stops the telemetry subscriber
func stopTelemetry() {
	if telemetrySubscriber == nil {
		return
	}

	// Unsubscribe from event bus
	if telemetrySubID != "" {
		eventBus := events.GetEventBus()
		eventBus.UnsubscribeFunc(telemetrySubID)
		telemetrySubID = ""
	}

	// Stop the subscriber (flushes remaining events)
	telemetrySubscriber.Stop()
	telemetrySubscriber = nil
	log.Printf("📡 Telemetry stopped")
}

// UpdateTelemetry is the callback for dynamic telemetry config changes
func UpdateTelemetry(enabled bool) error {
	cfg := config.GetConfig()
	agentConfig := cfg.Get()

	if enabled {
		if telemetrySubscriber == nil {
			eventBus := events.GetEventBus()
			startTelemetry(agentConfig, eventBus)
		}
	} else {
		stopTelemetry()
	}
	return nil
}

// UpdateTasks is the callback for dynamic tasks config changes
func UpdateTasks(enabled bool) error {
	if enabled {
		// Register task tools if not already registered
		tools.RegisterTaskTools()
		log.Printf("📋 Task tools enabled dynamically")
	} else {
		// Note: We don't unregister tools as they may be in use
		// Just log that tasks are disabled
		log.Printf("📋 Tasks disabled (tools remain registered)")
	}
	return nil
}

// ANSI color codes for terminal output
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[37m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// StartupStatus holds the status of all features for display
type StartupStatus struct {
	Memory       bool
	FileSystem   bool
	MCP          bool
	MCPResources bool
	Scheduler    bool
	Reflection   bool
	Realtime     bool
	Operator     bool
	Discovery    bool
	Agents       bool
}

// printStartupBanner prints a colorful startup banner with feature status
func printStartupBanner(status StartupStatus, port string, version string) {
	enabled := func(b bool) string {
		if b {
			return colorGreen + "●" + colorReset
		}
		return colorDim + "○" + colorReset
	}

	label := func(b bool, name string) string {
		if b {
			return colorGreen + name + colorReset
		}
		return colorDim + name + colorReset
	}

	fmt.Println()
	fmt.Printf("%s╔══════════════════════════════════════════════════════════════╗%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s  %s🤖 AGENT CORE%s                                      %sv%s%s  %s║%s\n",
		colorCyan, colorReset, colorBold+colorPurple, colorReset, colorYellow, version, colorReset, colorCyan, colorReset)
	fmt.Printf("%s╠══════════════════════════════════════════════════════════════╣%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s                                                              %s║%s\n", colorCyan, colorReset, colorCyan, colorReset)
	fmt.Printf("%s║%s  %sCORE FEATURES%s                                              %s║%s\n",
		colorCyan, colorReset, colorBold, colorReset, colorCyan, colorReset)
	fmt.Printf("%s║%s    %s %-12s   %s %-12s   %s %-12s   %s║%s\n",
		colorCyan, colorReset,
		enabled(status.Memory), label(status.Memory, "Memory"),
		enabled(status.FileSystem), label(status.FileSystem, "Files"),
		enabled(status.Scheduler), label(status.Scheduler, "Scheduler"),
		colorCyan, colorReset)
	fmt.Printf("%s║%s                                                              %s║%s\n", colorCyan, colorReset, colorCyan, colorReset)
	fmt.Printf("%s║%s  %sINTEGRATIONS%s                                               %s║%s\n",
		colorCyan, colorReset, colorBold, colorReset, colorCyan, colorReset)
	fmt.Printf("%s║%s    %s %-12s   %s %-12s   %s %-12s   %s║%s\n",
		colorCyan, colorReset,
		enabled(status.MCP), label(status.MCP, "MCP"),
		enabled(status.Reflection), label(status.Reflection, "Reflection"),
		enabled(status.Operator), label(status.Operator, "Operator"),
		colorCyan, colorReset)
	fmt.Printf("%s║%s                                                              %s║%s\n", colorCyan, colorReset, colorCyan, colorReset)
	fmt.Printf("%s║%s  %sCOMMUNICATION%s                                              %s║%s\n",
		colorCyan, colorReset, colorBold, colorReset, colorCyan, colorReset)
	fmt.Printf("%s║%s    %s %-12s   %s %-12s   %s %-12s   %s║%s\n",
		colorCyan, colorReset,
		enabled(status.Realtime), label(status.Realtime, "Voice"),
		enabled(status.Discovery), label(status.Discovery, "Discovery"),
		enabled(status.Agents), label(status.Agents, "Agents"),
		colorCyan, colorReset)
	fmt.Printf("%s║%s                                                              %s║%s\n", colorCyan, colorReset, colorCyan, colorReset)
	fmt.Printf("%s╠══════════════════════════════════════════════════════════════╣%s\n", colorCyan, colorReset)
	fmt.Printf("%s║%s  %s🌐 Server:%s http://localhost%s%-28s%s║%s\n",
		colorCyan, colorReset, colorBold, colorReset, colorGreen, port, colorCyan, colorReset)
	fmt.Printf("%s║%s  %s🔧 Debug:%s  http://localhost%s/debug%-21s%s║%s\n",
		colorCyan, colorReset, colorBold, colorReset, colorGreen, port, colorCyan, colorReset)
	fmt.Printf("%s╚══════════════════════════════════════════════════════════════╝%s\n", colorCyan, colorReset)
	fmt.Println()
}

// responseWriter wraps http.ResponseWriter to capture status code and bytes written
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += n
	return n, err
}

// Flush implements http.Flusher interface (required for SSE streaming)
func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker interface (required for WebSocket upgrades)
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("responseWriter does not implement http.Hijacker")
}

// authMiddleware checks for valid API key on protected endpoints
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get required API key from environment
		requiredKey := os.Getenv("AGENT_API_KEY")

		// If no API key configured, auth is disabled (development mode)
		if requiredKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Whitelist certain endpoints that don't require auth
		whitelistedPaths := []string{
			"/health",
			"/debug", // debug UI page
			"/",      // main UI page
			"/chat",  // TODO: implement proper auth for agent-to-agent calls
		}
		whitelistedWellKnown := []string{
			"/.well-known/agent.json", // A2A Agent Card (public for discovery)
		}
		whitelistedPrefixes := []string{
			"/web/",   // static assets (JS, CSS)
			"/files/", // file downloads (images displayed in chat, shared links)
		}

		for _, path := range whitelistedPaths {
			if r.URL.Path == path {
				next.ServeHTTP(w, r)
				return
			}
		}
		for _, path := range whitelistedWellKnown {
			if r.URL.Path == path {
				next.ServeHTTP(w, r)
				return
			}
		}
		for _, prefix := range whitelistedPrefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check API key from header (X-API-Key or X-Agent-Key for agent-to-agent calls)
		providedKey := r.Header.Get("X-API-Key")
		if providedKey == "" {
			providedKey = r.Header.Get("X-Agent-Key") // For agent-to-agent calls
		}
		if providedKey == "" {
			// Also check query parameter for WebSocket/SSE connections
			providedKey = r.URL.Query().Get("api_key")
		}

		if providedKey != requiredKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Unauthorized", "message": "Invalid or missing API key"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs HTTP requests with color-coded output
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status
		wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}

		// Read request body if present (for POST/PUT)
		var bodyPreview string
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			if r.Body != nil {
				bodyBytes, _ := io.ReadAll(r.Body)
				r.Body.Close()
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				// Show full body for debugging (limit to 2000 chars for safety)
				if len(bodyBytes) > 0 {
					bodyStr := string(bodyBytes)
					maxLen := 2000
					if len(bodyStr) > maxLen {
						bodyPreview = bodyStr[:maxLen] + "... (truncated)"
					} else {
						bodyPreview = bodyStr
					}
				}
			}
		}

		// Get query parameters
		queryStr := ""
		if len(r.URL.Query()) > 0 {
			queryStr = " " + colorGray + "?" + r.URL.RawQuery + colorReset
		}

		// Color code method
		methodColor := colorBlue
		switch r.Method {
		case "GET":
			methodColor = colorGreen
		case "POST":
			methodColor = colorYellow
		case "PUT":
			methodColor = colorPurple
		case "DELETE":
			methodColor = colorRed
		}

		// Log request
		log.Printf("%s%s%-6s%s %s%s%s%s",
			colorBold, methodColor, r.Method, colorReset,
			colorCyan, r.URL.Path, colorReset,
			queryStr,
		)

		// Log body preview if present
		if bodyPreview != "" {
			log.Printf("       %s└─ Body: %s%s", colorGray, bodyPreview, colorReset)
		}

		// Process request
		next.ServeHTTP(wrapped, r)

		// Calculate duration
		duration := time.Since(start)

		// Color code status
		statusColor := colorGreen
		if wrapped.statusCode >= 400 && wrapped.statusCode < 500 {
			statusColor = colorYellow
		} else if wrapped.statusCode >= 500 {
			statusColor = colorRed
		}

		// Log response
		log.Printf("       %s└─ %s%d%s %s%s%s | %d bytes | %s",
			colorGray,
			statusColor, wrapped.statusCode, colorReset,
			colorGray, http.StatusText(wrapped.statusCode), colorReset,
			wrapped.written,
			duration,
		)
	})
}

func main() {
	startupTime := time.Now()
	log.Printf("🚀 Agent starting...")

	// Set version globally so config.GetVersion() always returns the correct value
	config.SetVersion(getVersion())

	loadEnv()
	initDB()
	defer db.Close()
	defer func() {
		if agentScheduler != nil && agentScheduler.IsRunning() {
			agentScheduler.Stop()
		}
		if agentReflection != nil && agentReflection.IsRunning() {
			agentReflection.Stop()
		}
	}()

	log.Printf("⏱️  DB initialized in %v", time.Since(startupTime))

	// Initialize event bus
	eventBus := events.GetEventBus()
	defer eventBus.Stop()

	// Publish startup event
	startupEvent := events.NewEvent(events.CategorySystem, events.TypeSystemStartup, events.LevelInfo).
		WithData("version", getVersion()).
		WithData("port", ":4015")
	eventBus.Publish(startupEvent)

	// Initialize telemetry if enabled
	cfg := config.GetConfig()
	agentConfig := cfg.Get()
	if agentConfig.Telemetry != nil && agentConfig.Telemetry.Enabled && agentConfig.Telemetry.Endpoint != "" {
		startTelemetry(agentConfig, eventBus)
	}
	defer stopTelemetry() // Ensure telemetry flushes on shutdown

	// Initialize providers and message saver
	anthropicProvider = providers.NewAnthropicProvider()
	openaiProvider = providers.NewOpenAIProvider()
	groqProvider = providers.NewGroqProvider()
	geminiProvider = providers.NewGeminiProvider()
	fireworksProvider = providers.NewFireworksProvider()
	xaiProvider = providers.NewXAIProvider()
	zaiProvider = providers.NewZAIProvider()
	novitaProvider = providers.NewNovitaProvider()
	veniceProvider = providers.NewVeniceProvider()
	moonshotProvider = providers.NewMoonshotProvider()
	togetherProvider = providers.NewTogetherProvider()
	mistralProvider = providers.NewMistralProvider()
	ollamaProvider = providers.NewOllamaProvider()
	cerebrasProvider = providers.NewCerebrasProvider()
	messageSaver = threads.NewDatabaseMessageSaver(db)

	// Initialize request tracker for cancellation support
	requestTracker = agents.NewRequestTracker()

	// Initialize task management (only if tasks are enabled)
	tools.SetTaskDB(db)
	if agentConfig.Tasks != nil && agentConfig.Tasks.Enabled {
		tools.RegisterTaskTools()
		log.Printf("📋 Task tools enabled")
	}

	// Initialize subscription management tools (always register if MCP enabled - config controls visibility)
	if agentConfig.MCP != nil && agentConfig.MCP.Enabled {
		tools.RegisterSubscriptionTools()
		if len(agentConfig.MCP.Webhooks) > 0 {
			log.Printf("📡 Subscription tools enabled for webhooks: %v", agentConfig.MCP.Webhooks)
		} else {
			log.Printf("📡 Subscription tools registered (webhooks can be added via config)")
		}
	}

	// Register operator session tools (only if operator is enabled)
	if agentConfig.Operator != nil && agentConfig.Operator.Enabled {
		tools.RegisterOperatorSessionTools()
		// Always register browser tool — works with all providers.
		// Anthropic also gets the native computer tool via BuiltinTools, but
		// the browser tool (different name) serves as fallback and is needed
		// when provider is switched dynamically (e.g. Anthropic → Fireworks).
		tools.RegisterBrowserTool()
		log.Printf("🖥️ Operator tools enabled")
	}

	// Register wait tool (independent, always available)
	tools.RegisterWaitTool()

	// Register config tools (for setup mode)
	// These are always registered but only added to tool list when setup mode is enabled
	tools.RegisterConfigTools()

	// Initialize memory manager if enabled
	// Note: This is synchronous because filesystem callbacks depend on it
	// Memory loading from DB is fast (just reads, no embedding generation)
	// Re-fetch config in case it was updated
	agentConfig = cfg.Get()
	if agentConfig.Memory != nil && agentConfig.Memory.Enabled {
		memoryStart := time.Now()
		var err error
		memoryManager, err = memory.NewMemoryManager(db, agentConfig.Memory, eventBus)
		if err != nil {
			log.Printf("⚠️  Memory init failed: %v", err)
		} else {
			log.Printf("⏱️  Memory initialized in %v", time.Since(memoryStart))
		}
	}

	// Initialize file system if enabled
	if agentConfig.FileSystem != nil {
		fileManager = filesystem.NewFileManager(db, agentConfig.FileSystem)
		if fileManager.IsEnabled() {
			// Set file manager for filesystem tools and register them
			tools.SetFileManager(fileManager)
			tools.RegisterFilesystemTools()

			// Wire up memory ingestion callback if memory is enabled with auto-ingest
			// Callback checks memoryManager != nil at runtime for safety
			if config.IsMemoryAutoIngestEnabled(agentConfig.Memory) {
				ingestTypes := config.GetMemoryIngestTypes(agentConfig.Memory)

				fileManager.SetIngestionCallback(func(fileID, fileName, mimeType string, data []byte) {
					if memoryManager == nil {
						log.Printf("[Ingest] ⏭️  Skipping %s - memory manager not ready", fileName)
						return
					}
					log.Printf("[Ingest] Callback triggered for: %s (mime: %s)", fileName, mimeType)

					// Check if this MIME type should be ingested
					// Use HasPrefix to handle charset suffixes like "text/plain; charset=utf-8"
					shouldIngest := false
					matchedType := ""
					for _, t := range ingestTypes {
						if strings.HasPrefix(mimeType, t) {
							shouldIngest = true
							matchedType = t
							break
						}
					}
					if !shouldIngest {
						log.Printf("[Ingest] ⏭️  Skipping %s - mime %s not in ingest_types %v", fileName, mimeType, ingestTypes)
						return
					}
					log.Printf("[Ingest] ✓ Mime type matched: %s -> %s", mimeType, matchedType)

					// Ingest file into memory
					result, err := memoryManager.IngestFile(fileID, fileName, mimeType, data)
					if err != nil {
						log.Printf("[Ingest] ⚠️ Failed to ingest file %s: %v", fileName, err)
						return
					}
					log.Printf("[Ingest] ✅ File ingested: %s (%d chunks)", fileName, result.ChunksCount)
				})
			}

			// Wire up deletion callback to clean up memories when files are deleted
			// Callback checks memoryManager != nil at runtime for safety
			fileManager.SetDeletionCallback(func(fileID string) {
				if memoryManager == nil {
					return
				}
				if count, err := memoryManager.DeleteMemoriesBySourceID(fileID); err != nil {
					log.Printf("[Delete] ⚠️ Failed to delete memories for file %s: %v", fileID, err)
				} else if count > 0 {
					log.Printf("[Delete] ✅ Cleaned up %d memories for file: %s", count, fileID)
				}
			})

			// Start cleanup job
			cleanupJob = filesystem.NewCleanupJob(fileManager, 1*time.Hour)
			cleanupJob.Start()
		}
	}

	// Initialize tool loader for on-demand tool loading
	tools.InitToolLoader(cfg.GetLLMConfig().ToolLoading)

	// Initialize skills manager from config
	if agentConfig.Skills != nil && agentConfig.Skills.Enabled {
		skills.GetManager().Initialize(agentConfig.Skills)
		log.Printf("🎯 Skills: Initialized %d skills", skills.GetManager().Count())
	}

	// Initialize MCP in background (non-blocking to allow API to start quickly)
	if agentConfig.MCP != nil && agentConfig.MCP.Enabled {
		go func() {
			mcpStart := time.Now()
			log.Printf("🔌 MCP: Starting background initialization...")
			handlerMcp.InitMCP()
			log.Printf("⏱️  MCP initialized in %v (background)", time.Since(mcpStart))

			// Skills are loaded from config at startup via skills.GetManager().Initialize()
		}()
	}

	// Initialize scheduler if enabled (non-blocking)
	go initScheduler()

	// Initialize reflection scheduler if enabled (non-blocking)
	go initReflection()

	// Set database for system prompt queries (reflection context)
	stream.SetDatabase(db)

	// Initialize browser session if operator mode enabled (non-blocking)
	go operator.InitBrowserSession()

	// Initialize agent communication if enabled
	initAgentCommunication()

	// Initialize tracing system - enable event persistence
	events.EnablePersistence(db)

	// Initialize config handler with database access
	handlerConfig.SetDatabase(db)

	// Set callbacks for dynamic enable/disable of services
	handlerConfig.SetDiscoveryCallback(UpdateDiscoveryService)
	handlerConfig.SetDiscoveryRunningCallback(IsDiscoveryRunning)
	handlerConfig.SetMemoryCallback(UpdateMemoryManager)
	handlerConfig.SetFilesystemCallback(UpdateFileSystem)
	handlerConfig.SetTelemetryCallback(UpdateTelemetry)
	handlerConfig.SetTasksCallback(UpdateTasks)
	handlerConfig.SetOperatorCallback(func() error {
		cfg := config.GetConfig()
		operatorConfig := cfg.Get().Operator
		if operatorConfig != nil && operatorConfig.Enabled {
			operator.InitProvider(operatorConfig)
			// Ensure operator tools are registered (idempotent — skips if already present)
			tools.RegisterOperatorSessionTools()
			tools.RegisterBrowserTool()
			log.Printf("🖥️  Browser provider re-initialized via config update")
		}
		return nil
	})

	mux := http.NewServeMux()

	// Core routes
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/chat", handleChat)
	mux.HandleFunc("/requests/", handleRequestCancel) // Cancel endpoint: POST /requests/{id}/cancel
	mux.HandleFunc("/config", handlerConfig.HandleConfig)
	mux.HandleFunc("/providers", handlerConfig.HandleProviders)
	mux.HandleFunc("/providers/ollama/models", handlerConfig.HandleOllamaModels)
	mux.HandleFunc("/reset", handleReset)
	mux.HandleFunc("/operator/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(operator.GetStatus())
	})

	// A2A protocol endpoints
	a2aPort := os.Getenv("PORT")
	if a2aPort == "" {
		a2aPort = "4015"
	}
	if !strings.HasPrefix(a2aPort, ":") {
		a2aPort = ":" + a2aPort
	}
	a2aHandler := a2a.NewHandler(cfg, db, events.GetEventBus(), a2aPort)
	mux.HandleFunc("/.well-known/agent.json", a2aHandler.HandleAgentCard)
	mux.HandleFunc("/a2a", a2aHandler.HandleJSONRPC)

	// Realtime voice endpoints (always registered, handler checks if enabled)
	realtimeServer := realtime.NewServer(db, messageSaver, eventBus)
	mux.HandleFunc("/voice", realtimeServer.HandleWebSocket)
	mux.HandleFunc("/realtime/sessions", realtime.HandleSessions(realtimeServer))
	mux.HandleFunc("/voice.html", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/voice.html")
	})

	// Serve static web assets (js, css) - always enabled for debug interface
	webFS := http.FileServer(http.Dir("./web"))
	mux.Handle("/web/", http.StripPrefix("/web/", webFS))

	// Scheduler routes
	mux.HandleFunc("/scheduler/status", scheduler.HandleSchedulerStatus)
	mux.HandleFunc("/scheduler/trigger", scheduler.HandleSchedulerTrigger)
	mux.HandleFunc("/scheduler/start", scheduler.HandleSchedulerStart)
	mux.HandleFunc("/scheduler/stop", scheduler.HandleSchedulerStop)
	mux.HandleFunc("/scheduler/jobs", scheduler.HandleSchedulerJobs)

	// Reflection routes
	reflection.RegisterRoutes(mux)

	// Discovery endpoint - returns discovered peer agents
	mux.HandleFunc("/discovery/agents", handleDiscoveredAgents)

	// Custom tools endpoint
	mux.HandleFunc("/tools/custom", handleCustomTools)

	// Subscriptions endpoint
	mux.HandleFunc("/subscriptions", handleSubscriptions)

	// Webhook receive endpoint for forwarded webhooks from central receiver
	mux.HandleFunc("/webhook/receive", handleWebhookReceive)

	// Event streaming endpoints (partial - query moved to observability)
	mux.HandleFunc("/events", events.SSEHandler)
	mux.HandleFunc("/events/stats", events.EventStatsHandler)

	// Register handler package routes
	threads.RegisterRoutes(mux, db)
	tasks.RegisterRoutes(mux)

	handlerMcp.RegisterRoutes(mux)
	handlerSkills.RegisterRoutes(mux)
	memories.RegisterRoutes(mux, func() *memory.MemoryManager { return memoryManager })
	observability.RegisterRoutes(mux, db)

	// Activity endpoint - agent activity summary
	mux.HandleFunc("/activity", activity.HandleActivity(db, config.GetConfig().Get().ID))

	// File management endpoints - always register, check IsEnabled() dynamically in handlers
	if fileManager != nil {
		mux.HandleFunc("/files", filesystem.HandleFilesCreateOrList(fileManager))
		mux.HandleFunc("/files/stats", filesystem.HandleFilesStats(fileManager))
		mux.HandleFunc("/files/cleanup", filesystem.HandleFilesCleanup(fileManager))
		mux.Handle("/files/", http.StripPrefix("/files", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Route based on path and method
			if strings.HasSuffix(r.URL.Path, "/download") {
				filesystem.HandleFilesDownload(fileManager)(w, r)
			} else if r.Method == http.MethodDelete {
				filesystem.HandleFilesDelete(fileManager)(w, r)
			} else if r.Method == http.MethodGet {
				filesystem.HandleFilesGet(fileManager)(w, r)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		})))
	}

	// Serve web interface (development only)
	mux.HandleFunc("/debug", handleDebugInterface)
	mux.HandleFunc("/", handleWebInterface)

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "4015"
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	// Print startup banner with feature status
	status := StartupStatus{
		Memory:       memoryManager != nil,
		FileSystem:   fileManager != nil && fileManager.IsEnabled(),
		MCP:          agentConfig.MCP != nil && agentConfig.MCP.Enabled,
		MCPResources: false,
		Scheduler:    agentScheduler != nil && agentScheduler.IsRunning(),
		Reflection:   agentReflection != nil && agentReflection.IsRunning(),
		Realtime:     agentConfig.Realtime != nil && agentConfig.Realtime.Enabled,
		Operator:     agentConfig.Operator != nil && agentConfig.Operator.Enabled,
		Discovery:    discoveryService != nil,
		Agents:       agentConfig.Agents != nil && agentConfig.Agents.Enabled,
	}
	printStartupBanner(status, port, getVersion())
	if BuildTime != "" {
		log.Printf("📦 Built: %s", BuildTime)
	}

	log.Printf("✅ HTTP server ready in %v - listening on %s", time.Since(startupTime), port)

	// Chain middleware: auth -> logging -> handler
	handler := authMiddleware(loggingMiddleware(mux))

	if err := http.ListenAndServe(port, handler); err != nil {
		log.Fatal(err)
	}
}

// providerSupportsVideo returns true if the provider can handle video content blocks natively
func providerSupportsVideo(provider string) bool {
	return provider == "gemini"
}

// filterVideoForProvider strips video content blocks for providers that don't support video.
// When filesystem is enabled, the file metadata text block (with URL) was already injected
// during expansion — we keep that and only remove the base64 video block.
// When filesystem is disabled, we replace the video block with a text placeholder.
func filterVideoForProvider(messages []stream.Message, provider string) {
	if providerSupportsVideo(provider) {
		return
	}

	for i := range messages {
		switch content := messages[i].Content.(type) {
		case []stream.ContentBlock:
			filtered := filterVideoBlocks(content)
			if len(filtered) != len(content) {
				messages[i].Content = filtered
				log.Printf("🎬 Filtered video blocks from message %d for provider %s", i, provider)
			}
		case []interface{}:
			filtered := filterVideoBlocksRaw(content)
			if len(filtered) != len(content) {
				messages[i].Content = filtered
				log.Printf("🎬 Filtered video blocks (raw) from message %d for provider %s", i, provider)
			}
		}
	}
}

// filterVideoBlocks removes video content blocks from typed content blocks.
// If the preceding block is a filesystem metadata text block, it's kept as context.
// If not, a placeholder text block is inserted.
func filterVideoBlocks(blocks []stream.ContentBlock) []stream.ContentBlock {
	var filtered []stream.ContentBlock
	hasVideo := false

	for _, block := range blocks {
		if block.Type == "video" {
			hasVideo = true
			// Check if the previous block is a filesystem metadata text block
			if len(filtered) > 0 && filtered[len(filtered)-1].Type == "text" &&
				strings.Contains(filtered[len(filtered)-1].Text, "[Uploaded file stored in filesystem]") {
				// Filesystem metadata already provides context — skip the video block
				continue
			}
			// No filesystem metadata — add a placeholder
			filtered = append(filtered, stream.ContentBlock{
				Type: "text",
				Text: "[Video content provided but not supported by the current AI provider. The video was not processed.]",
			})
			continue
		}
		filtered = append(filtered, block)
	}

	// If no video was found, return original slice (no allocation)
	if !hasVideo {
		return blocks
	}
	return filtered
}

// filterVideoBlocksRaw removes video content blocks from raw JSON content blocks ([]interface{}).
func filterVideoBlocksRaw(blocks []interface{}) []interface{} {
	var filtered []interface{}
	hasVideo := false

	for _, item := range blocks {
		blockMap, ok := item.(map[string]interface{})
		if !ok {
			filtered = append(filtered, item)
			continue
		}

		blockType, _ := blockMap["type"].(string)
		if blockType == "video" {
			hasVideo = true
			// Check if the previous block is a filesystem metadata text block
			if len(filtered) > 0 {
				if prevMap, ok := filtered[len(filtered)-1].(map[string]interface{}); ok {
					if prevType, _ := prevMap["type"].(string); prevType == "text" {
						if prevText, _ := prevMap["text"].(string); strings.Contains(prevText, "[Uploaded file stored in filesystem]") {
							// Filesystem metadata already provides context — skip the video block
							continue
						}
					}
				}
			}
			// No filesystem metadata — add a placeholder
			filtered = append(filtered, map[string]interface{}{
				"type": "text",
				"text": "[Video content provided but not supported by the current AI provider. The video was not processed.]",
			})
			continue
		}
		filtered = append(filtered, item)
	}

	if !hasVideo {
		return blocks
	}
	return filtered
}
