package a2a

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/apteva/agent/events"
	"github.com/google/uuid"
)

// handleMessageStream handles the A2A message/stream method (SSE streaming).
// It receives an A2A message, calls the internal /chat endpoint with streaming,
// and forwards the response as A2A SSE events.
func (h *Handler) handleMessageStream(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
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
	if params.TaskID != "" {
		taskID = params.TaskID
	}

	contextID := params.ContextID
	if contextID == "" {
		contextID = "ctx_" + uuid.New().String()[:8]
	}

	log.Printf("📨 A2A message/stream: taskID=%s contextID=%s", taskID, contextID)

	// Create task in database
	if err := h.createA2ATask(taskID, contextID, messageText); err != nil {
		log.Printf("⚠️ Failed to create A2A task: %v", err)
	}

	// Publish event
	if h.eventBus != nil {
		event := events.NewEvent(events.CategoryAgent, "a2a_message_stream", events.LevelInfo).
			WithData("task_id", taskID).
			WithData("context_id", contextID)
		h.eventBus.Publish(event)
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRPCError(w, req.ID, ErrCodeInternalError, "Streaming not supported")
		return
	}

	// Emit submitted status
	writeSSEEvent(w, TaskStatusUpdateEvent{
		Kind:   "status-update",
		TaskID: taskID,
		Status: TaskStatus{
			State:     TaskStateSubmitted,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: false,
	})
	flusher.Flush()

	// Emit working status
	h.updateA2ATaskStatus(taskID, TaskStateWorking)
	writeSSEEvent(w, TaskStatusUpdateEvent{
		Kind:   "status-update",
		TaskID: taskID,
		Status: TaskStatus{
			State:     TaskStateWorking,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: false,
	})
	flusher.Flush()

	// Call internal /chat endpoint with streaming
	chatReq := map[string]interface{}{
		"message":   messageText,
		"stream":    true,
		"source":    "a2a",
		"thread_id": contextID,
	}

	chatJSON, err := json.Marshal(chatReq)
	if err != nil {
		h.updateA2ATaskStatus(taskID, TaskStateFailed)
		writeSSEEvent(w, TaskStatusUpdateEvent{
			Kind:   "status-update",
			TaskID: taskID,
			Status: TaskStatus{
				State:     TaskStateFailed,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Message:   &Message{Role: "agent", Parts: []Part{{Kind: "text", Text: "Failed to build request"}}},
			},
			Final: true,
		})
		flusher.Flush()
		return
	}

	chatURL := fmt.Sprintf("http://localhost%s/chat", h.port)
	httpReq, err := http.NewRequestWithContext(r.Context(), "POST", chatURL, bytes.NewReader(chatJSON))
	if err != nil {
		h.updateA2ATaskStatus(taskID, TaskStateFailed)
		writeSSEEvent(w, TaskStatusUpdateEvent{
			Kind:   "status-update",
			TaskID: taskID,
			Status: TaskStatus{
				State:     TaskStateFailed,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Message:   &Message{Role: "agent", Parts: []Part{{Kind: "text", Text: "Failed to create request"}}},
			},
			Final: true,
		})
		flusher.Flush()
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Forward auth headers
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		httpReq.Header.Set("X-API-Key", apiKey)
	}
	if apiKey := r.Header.Get("X-Agent-Key"); apiKey != "" {
		httpReq.Header.Set("X-Agent-Key", apiKey)
	}

	// Forward call chain guard rail headers so /chat can propagate them
	for _, header := range []string{
		"X-Agent-Call-Chain", "X-Agent-Call-Depth", "X-Agent-Origin",
		"X-Agent-Call-ID", "X-Agent-TTL", "X-Agent-Start-Time",
		"X-Test-Mode",
	} {
		if v := r.Header.Get(header); v != "" {
			httpReq.Header.Set(header, v)
		}
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		h.updateA2ATaskStatus(taskID, TaskStateFailed)
		writeSSEEvent(w, TaskStatusUpdateEvent{
			Kind:   "status-update",
			TaskID: taskID,
			Status: TaskStatus{
				State:     TaskStateFailed,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Message:   &Message{Role: "agent", Parts: []Part{{Kind: "text", Text: "Chat request failed: " + err.Error()}}},
			},
			Final: true,
		})
		flusher.Flush()
		return
	}
	defer resp.Body.Close()

	// Parse SSE events from internal /chat and forward as A2A events
	var allText strings.Builder
	artifactIndex := 0

	scanner := bufio.NewScanner(resp.Body)
	// Increase scanner buffer for large SSE events
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		dataJSON := strings.TrimPrefix(line, "data: ")
		var chatEvent map[string]interface{}
		if err := json.Unmarshal([]byte(dataJSON), &chatEvent); err != nil {
			continue
		}

		eventType, _ := chatEvent["type"].(string)

		switch eventType {
		case "content":
			// Text content chunk — emit as artifact update
			text, _ := chatEvent["content"].(string)
			if text != "" {
				allText.WriteString(text)

				writeSSEEvent(w, TaskArtifactUpdateEvent{
					Kind:   "artifact-update",
					TaskID: taskID,
					Artifact: Artifact{
						ArtifactID: "artifact_" + taskID,
						Parts:      []Part{{Kind: "text", Text: text}},
						Index:      artifactIndex,
						Append:     artifactIndex > 0,
						LastChunk:  false,
					},
				})
				flusher.Flush()
				artifactIndex++
			}

		case "tool_use":
			// Tool invocation — emit as status update with metadata
			toolName, _ := chatEvent["tool_name"].(string)
			writeSSEEvent(w, TaskStatusUpdateEvent{
				Kind:   "status-update",
				TaskID: taskID,
				Status: TaskStatus{
					State:     TaskStateWorking,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					Message: &Message{
						Role:  "agent",
						Parts: []Part{{Kind: "text", Text: fmt.Sprintf("Using tool: %s", toolName)}},
					},
				},
				Final: false,
			})
			flusher.Flush()

		case "tool_result":
			// Tool completed — could emit metadata but keep it simple
			// Just a working status update
			writeSSEEvent(w, TaskStatusUpdateEvent{
				Kind:   "status-update",
				TaskID: taskID,
				Status: TaskStatus{
					State:     TaskStateWorking,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				},
				Final: false,
			})
			flusher.Flush()

		case "done":
			// Stream complete — handled after loop
		}
	}

	// Emit final artifact chunk marker
	if allText.Len() > 0 {
		writeSSEEvent(w, TaskArtifactUpdateEvent{
			Kind:   "artifact-update",
			TaskID: taskID,
			Artifact: Artifact{
				ArtifactID: "artifact_" + taskID,
				Parts:      []Part{{Kind: "text", Text: ""}},
				Index:      artifactIndex,
				Append:     true,
				LastChunk:  true,
			},
		})
		flusher.Flush()
	}

	// Store final artifact in DB
	finalArtifact := Artifact{
		ArtifactID: "artifact_" + taskID,
		Name:       "response",
		Parts:      []Part{{Kind: "text", Text: allText.String()}},
		Index:      0,
		LastChunk:  true,
	}
	artifactsJSON, _ := json.Marshal([]Artifact{finalArtifact})
	h.storeA2AArtifacts(taskID, string(artifactsJSON))

	// Store history
	history := []Message{
		params.Message,
		{
			Role:      "agent",
			MessageID: "msg_" + uuid.New().String()[:8],
			Parts:     []Part{{Kind: "text", Text: allText.String()}},
		},
	}
	historyJSON, _ := json.Marshal(history)
	h.storeA2AHistory(taskID, string(historyJSON))

	// Emit completed status (final)
	h.updateA2ATaskStatus(taskID, TaskStateCompleted)
	writeSSEEvent(w, TaskStatusUpdateEvent{
		Kind:   "status-update",
		TaskID: taskID,
		Status: TaskStatus{
			State:     TaskStateCompleted,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Message: &Message{
				Role:      "agent",
				MessageID: "msg_" + uuid.New().String()[:8],
				Parts:     []Part{{Kind: "text", Text: allText.String()}},
			},
		},
		Final: true,
	})
	flusher.Flush()
}
