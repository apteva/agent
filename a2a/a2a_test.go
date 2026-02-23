package a2a

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"

	_ "github.com/mattn/go-sqlite3"
)

// =============================================================================
// Test helpers
// =============================================================================

// testDB creates an in-memory SQLite database with the required tables.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Tasks table matching production schema
	_, err = db.Exec(`CREATE TABLE tasks (
		id TEXT PRIMARY KEY,
		thread_id TEXT,
		title TEXT NOT NULL,
		description TEXT,
		type TEXT DEFAULT 'once',
		status TEXT DEFAULT 'pending',
		priority INTEGER DEFAULT 5,
		execute_at DATETIME,
		executed_at DATETIME,
		recurrence TEXT,
		next_run DATETIME,
		result TEXT,
		source TEXT DEFAULT 'local',
		delegator_id TEXT,
		context_id TEXT,
		a2a_task_id TEXT,
		artifacts TEXT,
		history TEXT,
		push_config TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("Failed to create tasks table: %v", err)
	}

	return db
}

// mockChatServer creates a test HTTP server that simulates the /chat endpoint.
// For stream=false: returns a JSON response.
// For stream=true: returns SSE events.
func mockChatServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		message, _ := req["message"].(string)
		streaming, _ := req["stream"].(bool)

		if streaming {
			// SSE streaming mode
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher, _ := w.(http.Flusher)

			// Send content chunks
			chunks := []string{"Hello", " from", " Agent", "!"}
			for _, chunk := range chunks {
				fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]interface{}{
					"type":    "content",
					"content": chunk,
				}))
				if flusher != nil {
					flusher.Flush()
				}
			}

			// Send done
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]interface{}{
				"type": "done",
			}))
			if flusher != nil {
				flusher.Flush()
			}
		} else {
			// Non-streaming JSON response
			response := fmt.Sprintf("Echo: %s", message)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":   true,
				"response":  response,
				"thread_id": "thread_test_123",
				"model":     "test-model",
			})
		}
	}))
}

// testHandler creates an A2A handler wired to a mock chat server.
func testHandler(t *testing.T) (*Handler, *sql.DB, *httptest.Server) {
	t.Helper()
	db := testDB(t)
	chatServer := mockChatServer(t)

	// Extract port from chat server URL (e.g., "http://127.0.0.1:12345" → ":12345")
	parts := strings.Split(chatServer.URL, ":")
	port := ":" + parts[len(parts)-1]

	cfg := config.GetConfig()
	agentCfg := cfg.Get()
	agentCfg.Name = "Test Agent"
	agentCfg.Description = "A test agent for A2A protocol testing"
	agentCfg.A2A = &config.A2AConfig{Enabled: true, PushNotifications: false}
	cfg.Update(agentCfg)

	eventBus := events.GetEventBus()
	handler := NewHandler(cfg, db, eventBus, port)

	return handler, db, chatServer
}

func mustJSON(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func rpcRequest(method string, id interface{}, params interface{}) []byte {
	paramsJSON, _ := json.Marshal(params)
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}
	data, _ := json.Marshal(req)
	return data
}

// =============================================================================
// Agent Card Tests
// =============================================================================

func TestAgentCard_ReturnsValidCard(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent.json", nil)
	w := httptest.NewRecorder()

	handler.HandleAgentCard(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var card AgentCard
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &card); err != nil {
		t.Fatalf("Failed to parse agent card: %v", err)
	}

	if card.Name != "Test Agent" {
		t.Errorf("Expected name 'Test Agent', got '%s'", card.Name)
	}
	if card.Description != "A test agent for A2A protocol testing" {
		t.Errorf("Expected description mismatch, got '%s'", card.Description)
	}
	if !card.Capabilities.Streaming {
		t.Error("Expected streaming capability to be true")
	}
	if card.Capabilities.StateTransitionHistory != true {
		t.Error("Expected stateTransitionHistory to be true")
	}
	if len(card.DefaultInputModes) == 0 {
		t.Error("Expected at least one input mode")
	}
	if len(card.DefaultOutputModes) == 0 {
		t.Error("Expected at least one output mode")
	}
	if len(card.Skills) == 0 {
		t.Error("Expected at least one skill")
	}

	// Check security scheme
	if _, ok := card.SecuritySchemes["apiKey"]; !ok {
		t.Error("Expected apiKey security scheme")
	} else {
		scheme := card.SecuritySchemes["apiKey"]
		if scheme.Type != "apiKey" {
			t.Errorf("Expected type 'apiKey', got '%s'", scheme.Type)
		}
		if scheme.In != "header" {
			t.Errorf("Expected in 'header', got '%s'", scheme.In)
		}
		if scheme.Name != "X-API-Key" {
			t.Errorf("Expected name 'X-API-Key', got '%s'", scheme.Name)
		}
	}

	t.Logf("Agent card: name=%s, url=%s, skills=%d, inputModes=%v",
		card.Name, card.URL, len(card.Skills), card.DefaultInputModes)
}

func TestAgentCard_MethodNotAllowed(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	req := httptest.NewRequest(http.MethodPost, "/.well-known/agent.json", nil)
	w := httptest.NewRecorder()

	handler.HandleAgentCard(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", w.Code)
	}
}

// =============================================================================
// JSON-RPC Dispatcher Tests
// =============================================================================

func TestJSONRPC_RejectsNonPost(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	req := httptest.NewRequest(http.MethodGet, "/a2a", nil)
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", w.Code)
	}
}

func TestJSONRPC_ParseError(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("Expected error response")
	}
	if rpcResp.Error.Code != ErrCodeParseError {
		t.Errorf("Expected error code %d, got %d", ErrCodeParseError, rpcResp.Error.Code)
	}
}

func TestJSONRPC_InvalidVersion(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	body := `{"jsonrpc":"1.0","id":1,"method":"message/send","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("Expected error for invalid jsonrpc version")
	}
	if rpcResp.Error.Code != ErrCodeInvalidRequest {
		t.Errorf("Expected code %d, got %d", ErrCodeInvalidRequest, rpcResp.Error.Code)
	}
}

func TestJSONRPC_MethodNotFound(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"unknown/method"}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("Expected error for unknown method")
	}
	if rpcResp.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("Expected code %d, got %d", ErrCodeMethodNotFound, rpcResp.Error.Code)
	}
}

func TestJSONRPC_EchoesA2AVersion(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"unknown/method"}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("A2A-Version", "0.3")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	if v := w.Header().Get("A2A-Version"); v != "0.3" {
		t.Errorf("Expected A2A-Version header '0.3', got '%s'", v)
	}
}

// =============================================================================
// message/send Tests
// =============================================================================

func TestMessageSend_Success(t *testing.T) {
	handler, db, chatServer := testHandler(t)
	defer chatServer.Close()
	defer db.Close()

	params := MessageSendParams{
		Message: Message{
			Role: "user",
			Parts: []Part{
				{Kind: "text", Text: "Hello, Agent!"},
			},
		},
	}

	body := rpcRequest("message/send", 1, params)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		t.Fatalf("Failed to parse response: %v\nBody: %s", err, string(respBody))
	}

	if rpcResp.Error != nil {
		t.Fatalf("Unexpected error: code=%d, message=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	// Parse result as A2A Task
	resultJSON, _ := json.Marshal(rpcResp.Result)
	var task Task
	if err := json.Unmarshal(resultJSON, &task); err != nil {
		t.Fatalf("Failed to parse task: %v", err)
	}

	if task.Kind != "task" {
		t.Errorf("Expected kind 'task', got '%s'", task.Kind)
	}
	if task.ID == "" {
		t.Error("Expected non-empty task ID")
	}
	if !strings.HasPrefix(task.ID, "task_a2a_") {
		t.Errorf("Expected task ID to start with 'task_a2a_', got '%s'", task.ID)
	}
	if task.ContextID == "" {
		t.Error("Expected non-empty context ID")
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("Expected state 'completed', got '%s'", task.Status.State)
	}
	if task.Status.Timestamp == "" {
		t.Error("Expected non-empty timestamp")
	}
	if len(task.Artifacts) == 0 {
		t.Fatal("Expected at least one artifact")
	}

	// Check artifact contains response
	artifact := task.Artifacts[0]
	if len(artifact.Parts) == 0 {
		t.Fatal("Expected artifact to have parts")
	}
	if artifact.Parts[0].Kind != "text" {
		t.Errorf("Expected text part, got '%s'", artifact.Parts[0].Kind)
	}
	if !strings.Contains(artifact.Parts[0].Text, "Echo: Hello, Agent!") {
		t.Errorf("Expected response to contain echoed message, got '%s'", artifact.Parts[0].Text)
	}

	// Check history
	if len(task.History) != 2 {
		t.Errorf("Expected 2 history messages, got %d", len(task.History))
	}
	if task.History[0].Role != "user" {
		t.Errorf("Expected first history message to be 'user', got '%s'", task.History[0].Role)
	}
	if task.History[1].Role != "agent" {
		t.Errorf("Expected second history message to be 'agent', got '%s'", task.History[1].Role)
	}

	// Verify task was persisted in database
	var dbStatus, dbSource string
	err := db.QueryRow("SELECT status, source FROM tasks WHERE id = ?", task.ID).Scan(&dbStatus, &dbSource)
	if err != nil {
		t.Fatalf("Task not found in database: %v", err)
	}
	if dbStatus != "completed" {
		t.Errorf("Expected db status 'completed', got '%s'", dbStatus)
	}
	if dbSource != "a2a" {
		t.Errorf("Expected db source 'a2a', got '%s'", dbSource)
	}

	t.Logf("message/send: taskID=%s, state=%s, response=%s",
		task.ID, task.Status.State, artifact.Parts[0].Text)
}

func TestMessageSend_WithContextID(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	params := MessageSendParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "Hello"}},
		},
		ContextID: "my-context-123",
	}

	body := rpcRequest("message/send", 2, params)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&rpcResp)

	if rpcResp.Error != nil {
		t.Fatalf("Unexpected error: %s", rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var task Task
	json.Unmarshal(resultJSON, &task)

	if task.ContextID != "my-context-123" {
		t.Errorf("Expected contextID 'my-context-123', got '%s'", task.ContextID)
	}
}

func TestMessageSend_EmptyMessage(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	params := MessageSendParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{}, // No parts
		},
	}

	body := rpcRequest("message/send", 3, params)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("Expected error for empty message")
	}
	if rpcResp.Error.Code != ErrCodeInvalidParams {
		t.Errorf("Expected code %d, got %d", ErrCodeInvalidParams, rpcResp.Error.Code)
	}
}

func TestMessageSend_MultipleTextParts(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	params := MessageSendParams{
		Message: Message{
			Role: "user",
			Parts: []Part{
				{Kind: "text", Text: "Part one."},
				{Kind: "text", Text: "Part two."},
			},
		},
	}

	body := rpcRequest("message/send", 4, params)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&rpcResp)

	if rpcResp.Error != nil {
		t.Fatalf("Unexpected error: %s", rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var task Task
	json.Unmarshal(resultJSON, &task)

	// The response should contain both parts concatenated
	if len(task.Artifacts) == 0 || len(task.Artifacts[0].Parts) == 0 {
		t.Fatal("Expected artifact with parts")
	}
	responseText := task.Artifacts[0].Parts[0].Text
	if !strings.Contains(responseText, "Part one.") || !strings.Contains(responseText, "Part two.") {
		t.Errorf("Expected response to contain both parts, got '%s'", responseText)
	}
}

// =============================================================================
// message/stream Tests
// =============================================================================

func TestMessageStream_Success(t *testing.T) {
	handler, db, chatServer := testHandler(t)
	defer chatServer.Close()
	defer db.Close()

	params := MessageSendParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "Stream test"}},
		},
	}

	body := rpcRequest("message/stream", 10, params)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	resp := w.Result()

	// Check SSE content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", contentType)
	}

	// Parse SSE events
	respBody, _ := io.ReadAll(resp.Body)
	events := parseSSEEvents(t, string(respBody))

	if len(events) == 0 {
		t.Fatal("Expected at least one SSE event")
	}

	// Verify event sequence
	var statusUpdates []TaskStatusUpdateEvent
	var artifactUpdates []TaskArtifactUpdateEvent
	var taskID string
	var finalEvent *TaskStatusUpdateEvent

	for _, event := range events {
		kind, _ := event["kind"].(string)
		switch kind {
		case "status-update":
			var su TaskStatusUpdateEvent
			eventJSON, _ := json.Marshal(event)
			json.Unmarshal(eventJSON, &su)
			statusUpdates = append(statusUpdates, su)
			if taskID == "" {
				taskID = su.TaskID
			}
			if su.Final {
				finalEvent = &su
			}
		case "artifact-update":
			var au TaskArtifactUpdateEvent
			eventJSON, _ := json.Marshal(event)
			json.Unmarshal(eventJSON, &au)
			artifactUpdates = append(artifactUpdates, au)
		}
	}

	// Must have status updates
	if len(statusUpdates) < 3 {
		t.Errorf("Expected at least 3 status updates (submitted, working, completed), got %d", len(statusUpdates))
	}

	// First status should be submitted
	if statusUpdates[0].Status.State != TaskStateSubmitted {
		t.Errorf("Expected first status to be 'submitted', got '%s'", statusUpdates[0].Status.State)
	}

	// Second should be working
	if statusUpdates[1].Status.State != TaskStateWorking {
		t.Errorf("Expected second status to be 'working', got '%s'", statusUpdates[1].Status.State)
	}

	// Must have artifact updates (content chunks)
	if len(artifactUpdates) == 0 {
		t.Error("Expected at least one artifact update")
	}

	// Must end with final completed event
	if finalEvent == nil {
		t.Fatal("Expected a final status update event")
	}
	if finalEvent.Status.State != TaskStateCompleted {
		t.Errorf("Expected final state 'completed', got '%s'", finalEvent.Status.State)
	}
	if !finalEvent.Final {
		t.Error("Expected final event to have final=true")
	}

	// Verify task persisted in DB as completed
	var dbStatus string
	err := db.QueryRow("SELECT status FROM tasks WHERE id = ?", taskID).Scan(&dbStatus)
	if err != nil {
		t.Fatalf("Task not found in database: %v", err)
	}
	if dbStatus != "completed" {
		t.Errorf("Expected db status 'completed', got '%s'", dbStatus)
	}

	t.Logf("message/stream: taskID=%s, statusUpdates=%d, artifactUpdates=%d",
		taskID, len(statusUpdates), len(artifactUpdates))
}

// =============================================================================
// tasks/get Tests
// =============================================================================

func TestTasksGet_Success(t *testing.T) {
	handler, db, chatServer := testHandler(t)
	defer chatServer.Close()
	defer db.Close()

	// First, create a task via message/send
	params := MessageSendParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "Create a task for retrieval"}},
		},
	}

	body := rpcRequest("message/send", 20, params)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.HandleJSONRPC(w, req)

	var sendResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&sendResp)
	if sendResp.Error != nil {
		t.Fatalf("message/send failed: %s", sendResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(sendResp.Result)
	var createdTask Task
	json.Unmarshal(resultJSON, &createdTask)

	// Now retrieve the task via tasks/get
	getParams := TaskGetParams{TaskID: createdTask.ID}
	getBody := rpcRequest("tasks/get", 21, getParams)
	getReq := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(getBody))
	getReq.Header.Set("Content-Type", "application/json")
	getW := httptest.NewRecorder()

	handler.HandleJSONRPC(getW, getReq)

	var getResp JSONRPCResponse
	json.NewDecoder(getW.Body).Decode(&getResp)

	if getResp.Error != nil {
		t.Fatalf("tasks/get failed: %s", getResp.Error.Message)
	}

	getResultJSON, _ := json.Marshal(getResp.Result)
	var retrievedTask Task
	json.Unmarshal(getResultJSON, &retrievedTask)

	if retrievedTask.ID != createdTask.ID {
		t.Errorf("Expected task ID '%s', got '%s'", createdTask.ID, retrievedTask.ID)
	}
	if retrievedTask.Status.State != TaskStateCompleted {
		t.Errorf("Expected state 'completed', got '%s'", retrievedTask.Status.State)
	}
	if len(retrievedTask.Artifacts) == 0 {
		t.Error("Expected artifacts to be returned")
	}
	if len(retrievedTask.History) == 0 {
		t.Error("Expected history to be returned")
	}

	t.Logf("tasks/get: taskID=%s, state=%s, artifacts=%d, history=%d",
		retrievedTask.ID, retrievedTask.Status.State,
		len(retrievedTask.Artifacts), len(retrievedTask.History))
}

func TestTasksGet_NotFound(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	getParams := TaskGetParams{TaskID: "nonexistent_task_id"}
	body := rpcRequest("tasks/get", 22, getParams)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !strings.Contains(rpcResp.Error.Message, "not found") {
		t.Errorf("Expected 'not found' in error message, got '%s'", rpcResp.Error.Message)
	}
}

func TestTasksGet_WithHistoryLength(t *testing.T) {
	handler, db, chatServer := testHandler(t)
	defer chatServer.Close()
	defer db.Close()

	// Create a task
	params := MessageSendParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "History length test"}},
		},
	}
	body := rpcRequest("message/send", 23, params)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.HandleJSONRPC(w, req)

	var sendResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&sendResp)
	resultJSON, _ := json.Marshal(sendResp.Result)
	var createdTask Task
	json.Unmarshal(resultJSON, &createdTask)

	// Retrieve with historyLength=1
	histLen := 1
	getParams := TaskGetParams{TaskID: createdTask.ID, HistoryLength: &histLen}
	getBody := rpcRequest("tasks/get", 24, getParams)
	getReq := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(getBody))
	getReq.Header.Set("Content-Type", "application/json")
	getW := httptest.NewRecorder()
	handler.HandleJSONRPC(getW, getReq)

	var getResp JSONRPCResponse
	json.NewDecoder(getW.Body).Decode(&getResp)
	getResultJSON, _ := json.Marshal(getResp.Result)
	var retrievedTask Task
	json.Unmarshal(getResultJSON, &retrievedTask)

	if len(retrievedTask.History) > 1 {
		t.Errorf("Expected at most 1 history message, got %d", len(retrievedTask.History))
	}
}

// =============================================================================
// tasks/cancel Tests
// =============================================================================

func TestTasksCancel_PendingTask(t *testing.T) {
	handler, db, chatServer := testHandler(t)
	defer chatServer.Close()
	defer db.Close()

	// Insert a pending task directly
	taskID := "task_a2a_cancel1"
	_, err := db.Exec(`INSERT INTO tasks (id, title, type, status, source, created_at, updated_at)
		VALUES (?, 'Cancel test', 'once', 'pending', 'a2a', datetime('now'), datetime('now'))`, taskID)
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	cancelParams := TaskCancelParams{TaskID: taskID}
	body := rpcRequest("tasks/cancel", 30, cancelParams)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&rpcResp)

	if rpcResp.Error != nil {
		t.Fatalf("Unexpected error: %s", rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var task Task
	json.Unmarshal(resultJSON, &task)

	if task.Status.State != TaskStateCanceled {
		t.Errorf("Expected state 'canceled', got '%s'", task.Status.State)
	}

	// Verify in DB
	var dbStatus string
	db.QueryRow("SELECT status FROM tasks WHERE id = ?", taskID).Scan(&dbStatus)
	if dbStatus != "cancelled" {
		t.Errorf("Expected db status 'cancelled', got '%s'", dbStatus)
	}
}

func TestTasksCancel_CompletedTask(t *testing.T) {
	handler, db, chatServer := testHandler(t)
	defer chatServer.Close()
	defer db.Close()

	// Insert a completed task
	taskID := "task_a2a_cancel2"
	db.Exec(`INSERT INTO tasks (id, title, type, status, source, created_at, updated_at)
		VALUES (?, 'Completed task', 'once', 'completed', 'a2a', datetime('now'), datetime('now'))`, taskID)

	cancelParams := TaskCancelParams{TaskID: taskID}
	body := rpcRequest("tasks/cancel", 31, cancelParams)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	json.NewDecoder(w.Body).Decode(&rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("Expected error when cancelling completed task")
	}
	if !strings.Contains(rpcResp.Error.Message, "cannot be cancelled") {
		t.Errorf("Expected 'cannot be cancelled' error, got '%s'", rpcResp.Error.Message)
	}
}

// =============================================================================
// State Mapping Tests
// =============================================================================

func TestStateMapping_InternalToA2A(t *testing.T) {
	tests := []struct {
		internal string
		expected string
	}{
		{"pending", TaskStateSubmitted},
		{"running", TaskStateWorking},
		{"completed", TaskStateCompleted},
		{"failed", TaskStateFailed},
		{"cancelled", TaskStateCanceled},
		{"input-required", TaskStateInputRequired}, // A2A-native state
	}

	for _, tt := range tests {
		result := mapInternalToA2AState(tt.internal)
		if result != tt.expected {
			t.Errorf("mapInternalToA2AState(%q) = %q, want %q", tt.internal, result, tt.expected)
		}
	}
}

// =============================================================================
// End-to-End: Agent-to-Agent Communication
// =============================================================================

func TestE2E_AgentToAgentViaA2A(t *testing.T) {
	// This test simulates two agents communicating via A2A:
	// 1. "Agent B" is a real A2A server (our handler + mock /chat)
	// 2. We act as "Agent A" making A2A calls to Agent B

	handler, db, chatServer := testHandler(t)
	defer chatServer.Close()
	defer db.Close()

	// Start Agent B's A2A server
	agentB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent.json":
			handler.HandleAgentCard(w, r)
		case "/a2a":
			handler.HandleJSONRPC(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer agentB.Close()

	t.Logf("Agent B running at %s", agentB.URL)

	// Step 1: Agent A discovers Agent B via Agent Card
	cardResp, err := http.Get(agentB.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("Failed to fetch agent card: %v", err)
	}
	defer cardResp.Body.Close()

	var card AgentCard
	cardBody, _ := io.ReadAll(cardResp.Body)
	if err := json.Unmarshal(cardBody, &card); err != nil {
		t.Fatalf("Failed to parse agent card: %v", err)
	}

	t.Logf("Discovered Agent B: name=%s, skills=%d, streaming=%v",
		card.Name, len(card.Skills), card.Capabilities.Streaming)

	if card.Name == "" {
		t.Error("Agent card missing name")
	}
	if !card.Capabilities.Streaming {
		t.Error("Agent B should support streaming")
	}

	// Step 2: Agent A sends a message to Agent B via message/send
	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "agent-a-req-1",
		Method:  "message/send",
	}
	msgParams := MessageSendParams{
		Message: Message{
			Role:      "user",
			MessageID: "msg_from_agent_a",
			Parts:     []Part{{Kind: "text", Text: "What is 2 + 2?"}},
		},
		ContextID: "conversation_between_a_and_b",
	}
	paramsJSON, _ := json.Marshal(msgParams)
	rpcReq.Params = paramsJSON

	rpcBody, _ := json.Marshal(rpcReq)
	sendResp, err := http.Post(agentB.URL+"/a2a", "application/json", bytes.NewReader(rpcBody))
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
	defer sendResp.Body.Close()

	sendBody, _ := io.ReadAll(sendResp.Body)
	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(sendBody, &rpcResp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if rpcResp.Error != nil {
		t.Fatalf("A2A call failed: %s", rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var task Task
	json.Unmarshal(resultJSON, &task)

	if task.Status.State != TaskStateCompleted {
		t.Errorf("Expected completed, got %s", task.Status.State)
	}
	if task.ContextID != "conversation_between_a_and_b" {
		t.Errorf("Expected context ID preserved, got '%s'", task.ContextID)
	}

	// Extract response text
	responseText := ""
	if len(task.Artifacts) > 0 && len(task.Artifacts[0].Parts) > 0 {
		responseText = task.Artifacts[0].Parts[0].Text
	}
	t.Logf("Agent B responded: %s", responseText)

	if responseText == "" {
		t.Error("Agent B returned empty response")
	}

	// Step 3: Agent A retrieves the task status
	getReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "agent-a-req-2",
		Method:  "tasks/get",
	}
	getParamsJSON, _ := json.Marshal(TaskGetParams{TaskID: task.ID})
	getReq.Params = getParamsJSON

	getBody, _ := json.Marshal(getReq)
	getResp, err := http.Post(agentB.URL+"/a2a", "application/json", bytes.NewReader(getBody))
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}
	defer getResp.Body.Close()

	getRespBody, _ := io.ReadAll(getResp.Body)
	var getRpcResp JSONRPCResponse
	json.Unmarshal(getRespBody, &getRpcResp)

	if getRpcResp.Error != nil {
		t.Fatalf("tasks/get failed: %s", getRpcResp.Error.Message)
	}

	getResultJSON, _ := json.Marshal(getRpcResp.Result)
	var retrievedTask Task
	json.Unmarshal(getResultJSON, &retrievedTask)

	if retrievedTask.ID != task.ID {
		t.Errorf("Expected same task ID, got '%s'", retrievedTask.ID)
	}
	if retrievedTask.Status.State != TaskStateCompleted {
		t.Errorf("Expected completed, got '%s'", retrievedTask.Status.State)
	}

	t.Log("End-to-end A2A communication: SUCCESS")
}

func TestE2E_AgentToAgentStreaming(t *testing.T) {
	// Test streaming A2A communication between two agents

	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	agentB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a2a":
			handler.HandleJSONRPC(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer agentB.Close()

	// Agent A sends a streaming request to Agent B
	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "stream-1",
		Method:  "message/stream",
	}
	msgParams := MessageSendParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "Stream me a response"}},
		},
	}
	paramsJSON, _ := json.Marshal(msgParams)
	rpcReq.Params = paramsJSON

	rpcBody, _ := json.Marshal(rpcReq)
	resp, err := http.Post(agentB.URL+"/a2a", "application/json", bytes.NewReader(rpcBody))
	if err != nil {
		t.Fatalf("Failed to send stream request: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected text/event-stream, got %s", resp.Header.Get("Content-Type"))
	}

	// Read SSE events
	body, _ := io.ReadAll(resp.Body)
	events := parseSSEEvents(t, string(body))

	if len(events) == 0 {
		t.Fatal("Expected SSE events")
	}

	// Collect all text from artifact updates
	var streamedText strings.Builder
	hasSubmitted := false
	hasWorking := false
	hasCompleted := false

	for _, event := range events {
		kind, _ := event["kind"].(string)
		switch kind {
		case "status-update":
			state, _ := event["status"].(map[string]interface{})
			if state != nil {
				s, _ := state["state"].(string)
				switch s {
				case TaskStateSubmitted:
					hasSubmitted = true
				case TaskStateWorking:
					hasWorking = true
				case TaskStateCompleted:
					hasCompleted = true
				}
			}
		case "artifact-update":
			artifact, _ := event["artifact"].(map[string]interface{})
			if artifact != nil {
				parts, _ := artifact["parts"].([]interface{})
				for _, part := range parts {
					p, _ := part.(map[string]interface{})
					if text, ok := p["text"].(string); ok {
						streamedText.WriteString(text)
					}
				}
			}
		}
	}

	if !hasSubmitted {
		t.Error("Missing 'submitted' status event")
	}
	if !hasWorking {
		t.Error("Missing 'working' status event")
	}
	if !hasCompleted {
		t.Error("Missing 'completed' status event")
	}
	if streamedText.Len() == 0 {
		t.Error("No text received from stream")
	}

	t.Logf("Streamed response (%d chars): %s", streamedText.Len(), streamedText.String())
}

// =============================================================================
// Protocol Compliance Tests
// =============================================================================

func TestProtocol_JSONRPCResponseFormat(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	params := MessageSendParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "Protocol check"}},
		},
	}

	body := rpcRequest("message/send", 100, params)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	respBody, _ := io.ReadAll(w.Result().Body)

	// Verify it's valid JSON
	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		t.Fatalf("Response is not valid JSON: %v", err)
	}

	// Must have jsonrpc field
	if raw["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc='2.0', got '%v'", raw["jsonrpc"])
	}

	// Must have id matching request
	if raw["id"] != float64(100) { // JSON numbers are float64
		t.Errorf("Expected id=100, got '%v'", raw["id"])
	}

	// Must have result (not error)
	if raw["result"] == nil {
		t.Error("Expected result field in response")
	}
	if raw["error"] != nil {
		t.Error("Did not expect error field in success response")
	}
}

func TestProtocol_ErrorResponseFormat(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	body := `{"jsonrpc":"2.0","id":"err-1","method":"nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var raw map[string]interface{}
	json.NewDecoder(w.Body).Decode(&raw)

	if raw["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc='2.0' in error response")
	}
	if raw["id"] != "err-1" {
		t.Errorf("Expected id='err-1' in error response, got '%v'", raw["id"])
	}
	if raw["error"] == nil {
		t.Fatal("Expected error field")
	}

	errObj, ok := raw["error"].(map[string]interface{})
	if !ok {
		t.Fatal("Error should be an object")
	}
	if errObj["code"] == nil {
		t.Error("Error must have 'code' field")
	}
	if errObj["message"] == nil {
		t.Error("Error must have 'message' field")
	}
}

func TestProtocol_TaskStructure(t *testing.T) {
	handler, _, chatServer := testHandler(t)
	defer chatServer.Close()

	params := MessageSendParams{
		Message: Message{
			Role:  "user",
			Parts: []Part{{Kind: "text", Text: "Structure check"}},
		},
	}

	body := rpcRequest("message/send", 101, params)
	req := httptest.NewRequest(http.MethodPost, "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleJSONRPC(w, req)

	var rpcResp JSONRPCResponse
	respBody, _ := io.ReadAll(w.Body)
	json.Unmarshal(respBody, &rpcResp)

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var raw map[string]interface{}
	json.Unmarshal(resultJSON, &raw)

	// Verify A2A Task structure
	requiredFields := []string{"kind", "id", "status"}
	for _, field := range requiredFields {
		if raw[field] == nil {
			t.Errorf("Task missing required field '%s'", field)
		}
	}

	if raw["kind"] != "task" {
		t.Errorf("Expected kind='task', got '%v'", raw["kind"])
	}

	// Check status structure
	status, ok := raw["status"].(map[string]interface{})
	if !ok {
		t.Fatal("Status should be an object")
	}
	if status["state"] == nil {
		t.Error("Status missing 'state' field")
	}
	if status["timestamp"] == nil {
		t.Error("Status missing 'timestamp' field")
	}

	// Verify timestamp is valid RFC3339
	ts, _ := status["timestamp"].(string)
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("Timestamp '%s' is not valid RFC3339: %v", ts, err)
	}
}

// =============================================================================
// Helper: Parse SSE events
// =============================================================================

func parseSSEEvents(t *testing.T, body string) []map[string]interface{} {
	t.Helper()
	var events []map[string]interface{}

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// SSE events from our handler are wrapped in a JSON-RPC response
		var rpcResp JSONRPCResponse
		if err := json.Unmarshal([]byte(data), &rpcResp); err != nil {
			continue
		}

		if rpcResp.Result == nil {
			continue
		}

		resultJSON, _ := json.Marshal(rpcResp.Result)
		var event map[string]interface{}
		if err := json.Unmarshal(resultJSON, &event); err != nil {
			continue
		}

		events = append(events, event)
	}

	return events
}
