package a2a

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/apteva/agent/events"
	"github.com/google/uuid"
)

// handleMessageSend handles the A2A message/send method (synchronous).
// It receives an A2A message, calls the internal /chat endpoint, and returns
// an A2A Task with the response as an artifact.
func (h *Handler) handleMessageSend(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	// Parse params
	var params MessageSendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, ErrCodeInvalidParams, "Invalid params: "+err.Error())
		return
	}

	// Extract text from A2A message parts
	messageText := extractTextFromParts(params.Message.Parts)
	if messageText == "" {
		writeRPCError(w, req.ID, ErrCodeInvalidParams, "Message must contain at least one text part")
		return
	}

	// Generate task ID
	taskID := "task_a2a_" + uuid.New().String()[:8]

	// Use provided task ID if resuming an existing task
	if params.TaskID != "" {
		taskID = params.TaskID
	}

	// Context ID for conversation grouping
	contextID := params.ContextID
	if contextID == "" {
		contextID = "ctx_" + uuid.New().String()[:8]
	}

	log.Printf("📨 A2A message/send: taskID=%s contextID=%s", taskID, contextID)

	// Create task in database
	if err := h.createA2ATask(taskID, contextID, messageText); err != nil {
		log.Printf("⚠️ Failed to create A2A task: %v", err)
		// Continue anyway - the chat will still work
	}

	// Update task to working
	h.updateA2ATaskStatus(taskID, TaskStateWorking)

	// Publish event
	if h.eventBus != nil {
		event := events.NewEvent(events.CategoryAgent, "a2a_message_send", events.LevelInfo).
			WithData("task_id", taskID).
			WithData("context_id", contextID)
		h.eventBus.Publish(event)
	}

	// Call internal /chat endpoint
	chatReq := map[string]interface{}{
		"message":   messageText,
		"stream":    false,
		"source":    "a2a",
		"thread_id": contextID,
	}

	chatJSON, err := json.Marshal(chatReq)
	if err != nil {
		h.updateA2ATaskStatus(taskID, TaskStateFailed)
		writeRPCError(w, req.ID, ErrCodeInternalError, "Failed to build chat request")
		return
	}

	// POST to internal /chat
	chatURL := fmt.Sprintf("http://localhost%s/chat", h.port)
	httpReq, err := http.NewRequestWithContext(r.Context(), "POST", chatURL, bytes.NewReader(chatJSON))
	if err != nil {
		h.updateA2ATaskStatus(taskID, TaskStateFailed)
		writeRPCError(w, req.ID, ErrCodeInternalError, "Failed to create chat request")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Forward API key from original request
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		httpReq.Header.Set("X-API-Key", apiKey)
	}
	if apiKey := r.Header.Get("X-Agent-Key"); apiKey != "" {
		httpReq.Header.Set("X-Agent-Key", apiKey)
	}

	// Forward call chain guard rail headers and caller identity so /chat can use them
	for _, header := range []string{
		"X-Agent-Call-Chain", "X-Agent-Call-Depth", "X-Agent-Origin",
		"X-Agent-Call-ID", "X-Agent-TTL", "X-Agent-Start-Time",
		"X-Agent-Caller-ID", "X-Agent-Caller-Name",
		"X-Test-Mode",
	} {
		if v := r.Header.Get(header); v != "" {
			httpReq.Header.Set(header, v)
		}
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		h.updateA2ATaskStatus(taskID, TaskStateFailed)
		writeRPCError(w, req.ID, ErrCodeInternalError, "Chat request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.updateA2ATaskStatus(taskID, TaskStateFailed)
		writeRPCError(w, req.ID, ErrCodeInternalError, "Failed to read chat response")
		return
	}

	// Parse chat response
	var chatResp map[string]interface{}
	if err := json.Unmarshal(body, &chatResp); err != nil {
		h.updateA2ATaskStatus(taskID, TaskStateFailed)
		writeRPCError(w, req.ID, ErrCodeInternalError, "Invalid chat response")
		return
	}

	// Check for chat error
	if success, ok := chatResp["success"].(bool); ok && !success {
		errMsg := "Chat processing failed"
		if e, ok := chatResp["error"].(string); ok {
			errMsg = e
		}
		h.updateA2ATaskStatus(taskID, TaskStateFailed)
		writeRPCError(w, req.ID, ErrCodeServerError, errMsg)
		return
	}

	// Extract response text
	responseText := ""
	if r, ok := chatResp["response"].(string); ok {
		responseText = r
	}

	// Build A2A artifact from response
	artifact := Artifact{
		ArtifactID: "artifact_" + uuid.New().String()[:8],
		Name:       "response",
		Parts: []Part{
			{Kind: "text", Text: responseText},
		},
		Index:     0,
		LastChunk: true,
	}

	// Store artifacts in DB
	artifactsJSON, _ := json.Marshal([]Artifact{artifact})
	h.storeA2AArtifacts(taskID, string(artifactsJSON))

	// Build A2A history
	history := []Message{
		params.Message,
		{
			Role:      "agent",
			MessageID: "msg_" + uuid.New().String()[:8],
			Parts:     []Part{{Kind: "text", Text: responseText}},
		},
	}
	historyJSON, _ := json.Marshal(history)
	h.storeA2AHistory(taskID, string(historyJSON))

	// Update task to completed
	h.updateA2ATaskStatus(taskID, TaskStateCompleted)

	// Build A2A Task response
	task := Task{
		Kind:      "task",
		ID:        taskID,
		ContextID: contextID,
		Status: TaskStatus{
			State:     TaskStateCompleted,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Message: &Message{
				Role:      "agent",
				MessageID: "msg_" + uuid.New().String()[:8],
				Parts:     []Part{{Kind: "text", Text: responseText}},
			},
		},
		Artifacts: []Artifact{artifact},
		History:   history,
	}

	writeRPCResponse(w, req.ID, task)
}

// extractTextFromParts concatenates text from all text parts in a message.
func extractTextFromParts(parts []Part) string {
	var texts []string
	for _, part := range parts {
		if part.Kind == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// createA2ATask creates a task entry in the database for an A2A request.
func (h *Handler) createA2ATask(taskID, contextID, message string) error {
	if h.db == nil {
		return fmt.Errorf("database not available")
	}

	title := message
	if len(title) > 100 {
		title = title[:100] + "..."
	}

	_, err := h.db.Exec(`
		INSERT INTO tasks (id, title, type, status, priority, source, context_id, created_at, updated_at)
		VALUES (?, ?, 'once', 'pending', 5, 'a2a', ?, datetime('now'), datetime('now'))`,
		taskID, title, contextID)
	return err
}

// updateA2ATaskStatus updates the status of an A2A task.
func (h *Handler) updateA2ATaskStatus(taskID, status string) {
	if h.db == nil {
		return
	}
	// Map A2A states to internal states where needed
	internalStatus := status
	switch status {
	case TaskStateSubmitted:
		internalStatus = "pending"
	case TaskStateWorking:
		internalStatus = "running"
	case TaskStateCanceled:
		internalStatus = "cancelled"
	}
	_, err := h.db.Exec(`UPDATE tasks SET status = ?, updated_at = datetime('now') WHERE id = ?`,
		internalStatus, taskID)
	if err != nil {
		log.Printf("⚠️ Failed to update A2A task status: %v", err)
	}
}

// storeA2AArtifacts stores artifacts JSON on a task.
func (h *Handler) storeA2AArtifacts(taskID, artifactsJSON string) {
	if h.db == nil {
		return
	}
	_, err := h.db.Exec(`UPDATE tasks SET artifacts = ?, updated_at = datetime('now') WHERE id = ?`,
		artifactsJSON, taskID)
	if err != nil {
		log.Printf("⚠️ Failed to store A2A artifacts: %v", err)
	}
}

// storeA2AHistory stores message history JSON on a task.
func (h *Handler) storeA2AHistory(taskID, historyJSON string) {
	if h.db == nil {
		return
	}
	_, err := h.db.Exec(`UPDATE tasks SET history = ?, updated_at = datetime('now') WHERE id = ?`,
		historyJSON, taskID)
	if err != nil {
		log.Printf("⚠️ Failed to store A2A history: %v", err)
	}
}
