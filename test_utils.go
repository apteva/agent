package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/handlers/threads"
	"github.com/apteva/agent/providers"
	"github.com/apteva/agent/stream"
	"github.com/apteva/agent/tools"

	_ "github.com/mattn/go-sqlite3"
)

// TestDatabase provides a test database for testing
type TestDatabase struct {
	DB *sql.DB
}

// NewTestDatabase creates a new in-memory SQLite database for testing
func NewTestDatabase(t *testing.T) *TestDatabase {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Create tables
	createThreadsTable := `
	CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		title TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		metadata TEXT DEFAULT '{}'
	);`

	_, err = db.Exec(createThreadsTable)
	if err != nil {
		t.Fatalf("Failed to create threads table: %v", err)
	}

	createMessagesTable := `
	CREATE TABLE messages (
		id TEXT PRIMARY KEY,
		thread_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		model TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		metadata TEXT DEFAULT '{}',
		FOREIGN KEY (thread_id) REFERENCES threads (id) ON DELETE CASCADE
	);`

	_, err = db.Exec(createMessagesTable)
	if err != nil {
		t.Fatalf("Failed to create messages table: %v", err)
	}

	return &TestDatabase{DB: db}
}

// Close closes the test database
func (td *TestDatabase) Close() {
	td.DB.Close()
}

// GetMessageSaver returns a DatabaseMessageSaver for testing
func (td *TestDatabase) GetMessageSaver() *threads.DatabaseMessageSaver {
	return threads.NewDatabaseMessageSaver(td.DB)
}

// MockProvider implements the Provider interface for testing
type MockProvider struct {
	StreamResponse string
	Error         error
}

func (m *MockProvider) GetRawStream(messages []stream.Message, customTools []tools.ToolDefinition, builtinTools []interface{}) (io.ReadCloser, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return io.NopCloser(strings.NewReader(m.StreamResponse)), nil
}

// MockStreamProcessor implements StreamProcessor for testing
type MockStreamProcessor struct {
	Events []stream.StreamEvent
	Index  int
}

func (m *MockStreamProcessor) ProcessLine(line string) (*stream.StreamEvent, error) {
	if m.Index >= len(m.Events) {
		return nil, nil
	}

	event := &m.Events[m.Index]
	m.Index++
	return event, nil
}

func (m *MockStreamProcessor) IsComplete(line string) bool {
	return m.Index >= len(m.Events)
}

// TestServer provides a test HTTP server
type TestServer struct {
	Server     *httptest.Server
	TestDB     *TestDatabase
	messageSaver threads.MessageSaver
}

// NewTestServer creates a new test server
func NewTestServer(t *testing.T) *TestServer {
	testDB := NewTestDatabase(t)
	messageSaver := testDB.GetMessageSaver()

	// Initialize providers
	anthropicProvider = providers.NewAnthropicProvider()
	openaiProvider = providers.NewOpenAIProvider()

	// Set test environment variables
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	os.Setenv("OPENAI_API_KEY", "test-key")

	mux := http.NewServeMux()
	// handleHealth and handleConfig still in main.go
	// mux.HandleFunc("/health", handleHealth)
	// mux.HandleFunc("/config", handleConfig)

	// Mock chat handler for testing
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request body for basic validation
		var chatReq ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Check for empty message
		if msg, ok := chatReq.Message.(string); ok && msg == "" {
			http.Error(w, "Message is required", http.StatusBadRequest)
			return
		}

		// Check for invalid content blocks
		if blocks, ok := chatReq.Message.([]interface{}); ok {
			for _, block := range blocks {
				if _, ok := block.(map[string]interface{}); !ok {
					http.Error(w, "Invalid content block format", http.StatusBadRequest)
					return
				}
			}
		}

		// Simple mock response for testing
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Thread-ID", "test-thread")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		fmt.Fprintf(w, "data: {\"type\":\"content\",\"content\":\"Test response\"}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"stop\",\"content\":\"\"}\n\n")
	})

	server := httptest.NewServer(mux)

	return &TestServer{
		Server:       server,
		TestDB:       testDB,
		messageSaver: messageSaver,
	}
}

// Close closes the test server and database
func (ts *TestServer) Close() {
	ts.Server.Close()
	ts.TestDB.Close()
}

// PostJSON makes a POST request with JSON body
func (ts *TestServer) PostJSON(path string, body interface{}) (*http.Response, error) {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	return http.Post(ts.Server.URL+path, "application/json", bytes.NewBuffer(jsonData))
}

// Get makes a GET request
func (ts *TestServer) Get(path string) (*http.Response, error) {
	return http.Get(ts.Server.URL + path)
}

// Helper functions for testing

// AssertEqual asserts that two values are equal
func AssertEqual(t *testing.T, expected, actual interface{}) {
	if expected != actual {
		t.Errorf("Expected %v, got %v", expected, actual)
	}
}

// AssertNotNil asserts that a value is not nil
func AssertNotNil(t *testing.T, value interface{}) {
	if value == nil {
		t.Error("Expected value to not be nil")
	}
}

// AssertNil asserts that a value is nil
func AssertNil(t *testing.T, value interface{}) {
	if value != nil {
		t.Errorf("Expected nil, got %v", value)
	}
}

// AssertContains asserts that a string contains a substring
func AssertContains(t *testing.T, haystack, needle string) {
	if !strings.Contains(haystack, needle) {
		t.Errorf("Expected '%s' to contain '%s'", haystack, needle)
	}
}

// ReadResponseBody reads and returns the response body as a string
func ReadResponseBody(t *testing.T, resp *http.Response) string {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	return string(body)
}

// ParseJSONResponse parses a JSON response into a map
func ParseJSONResponse(t *testing.T, resp *http.Response) map[string]interface{} {
	body := ReadResponseBody(t, resp)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}
	return result
}

// CreateTestThread creates a test thread
func CreateTestThread(t *testing.T, saver threads.MessageSaver) string {
	threadID, err := saver.CreateThread("Test Thread")
	if err != nil {
		t.Fatalf("Failed to create test thread: %v", err)
	}
	return threadID
}

// CreateTestMessage creates a test message
func CreateTestMessage(t *testing.T, saver threads.MessageSaver, threadID, role, content string) {
	err := saver.SaveMessage(threadID, role, content, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create test message: %v", err)
	}
}

// StreamEvent helper for creating test stream events
func NewStreamEvent(eventType, content string) stream.StreamEvent {
	return stream.StreamEvent{
		Type:    eventType,
		Content: content,
	}
}

// ToolUseEvent helper for creating tool use events
func NewToolUseEvent(toolID, toolName string, input map[string]interface{}) stream.StreamEvent {
	return stream.StreamEvent{
		Type:      "tool_use",
		ToolID:    toolID,
		ToolName:  toolName,
		ToolInput: input,
		Content:   fmt.Sprintf("Using tool %s", toolName),
	}
}

// ToolResultEvent helper for creating tool result events
func NewToolResultEvent(toolID, content string) stream.StreamEvent {
	return stream.StreamEvent{
		Type:    "tool_result",
		ToolID:  toolID,
		Content: content,
	}
}

// MockTool for testing tool functionality
type MockTool struct {
	ToolName        string
	ToolDescription string
	Schema          map[string]interface{}
	Result          interface{}
	ExecuteError    error
}

func (m *MockTool) Name() string {
	return m.ToolName
}

func (m *MockTool) DisplayName() string {
	return m.ToolName
}

func (m *MockTool) Description() string {
	return m.ToolDescription
}

func (m *MockTool) InputSchema() map[string]interface{} {
	return m.Schema
}

func (m *MockTool) Execute(params map[string]interface{}) (interface{}, error) {
	if m.ExecuteError != nil {
		return nil, m.ExecuteError
	}
	return m.Result, nil
}

// RegisterMockTool registers a mock tool for testing
func RegisterMockTool(name, description string, result interface{}) *MockTool {
	mockTool := &MockTool{
		ToolName:        name,
		ToolDescription: description,
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{
					"type":        "string",
					"description": "Test input",
				},
			},
		},
		Result: result,
	}

	registry := tools.GetGlobalRegistry()
	registry.RegisterTool(mockTool)
	return mockTool
}

// SetupTestConfig sets up a test configuration
func SetupTestConfig(t *testing.T) {
	cfg := config.GetConfig()
	testConfig := cfg.Get()
	testConfig.LLM.Provider = "anthropic"
	testConfig.LLM.Model = "claude-sonnet-4-20250514"
	testConfig.LLM.Tools = []string{"test_tool"}
	cfg.Update(testConfig)
}

// MockHandler creates a mock HTTP handler for testing
func MockHandler(responseBody string, statusCode int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(responseBody))
	}
}

// CreateTempDB creates a temporary database file for testing
func CreateTempDB(t *testing.T) string {
	tmpfile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpfile.Close()
	return tmpfile.Name()
}

// CleanupTempDB removes a temporary database file
func CleanupTempDB(filename string) {
	os.Remove(filename)
}