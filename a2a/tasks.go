package a2a

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// handleTasksGet handles the A2A tasks/get method.
// It retrieves a task by ID and returns it in A2A Task format.
func (h *Handler) handleTasksGet(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	var params TaskGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, ErrCodeInvalidParams, "Invalid params: "+err.Error())
		return
	}

	if params.TaskID == "" {
		writeRPCError(w, req.ID, ErrCodeInvalidParams, "Task ID is required")
		return
	}

	task, err := h.getA2ATask(params.TaskID, params.HistoryLength)
	if err != nil {
		if err == sql.ErrNoRows {
			writeRPCError(w, req.ID, ErrCodeServerError, "Task not found: "+params.TaskID)
			return
		}
		writeRPCError(w, req.ID, ErrCodeInternalError, "Failed to retrieve task: "+err.Error())
		return
	}

	writeRPCResponse(w, req.ID, task)
}

// handleTasksCancel handles the A2A tasks/cancel method.
// It cancels a running task.
func (h *Handler) handleTasksCancel(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	var params TaskCancelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, ErrCodeInvalidParams, "Invalid params: "+err.Error())
		return
	}

	if params.TaskID == "" {
		writeRPCError(w, req.ID, ErrCodeInvalidParams, "Task ID is required")
		return
	}

	// Check task exists and get current status
	task, err := h.getA2ATask(params.TaskID, nil)
	if err != nil {
		if err == sql.ErrNoRows {
			writeRPCError(w, req.ID, ErrCodeServerError, "Task not found: "+params.TaskID)
			return
		}
		writeRPCError(w, req.ID, ErrCodeInternalError, "Failed to retrieve task: "+err.Error())
		return
	}

	// Only running/pending tasks can be cancelled
	if task.Status.State != TaskStateWorking && task.Status.State != TaskStateSubmitted {
		writeRPCError(w, req.ID, ErrCodeServerError, "Task cannot be cancelled in state: "+task.Status.State)
		return
	}

	// Update task status to cancelled
	h.updateA2ATaskStatus(params.TaskID, TaskStateCanceled)

	// Return updated task
	task.Status = TaskStatus{
		State:     TaskStateCanceled,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	writeRPCResponse(w, req.ID, task)
}

// getA2ATask retrieves a task from the database and maps it to A2A Task format.
func (h *Handler) getA2ATask(taskID string, historyLength *int) (*Task, error) {
	if h.db == nil {
		return nil, sql.ErrNoRows
	}

	var (
		id        string
		title     string
		status    string
		contextID sql.NullString
		artifacts sql.NullString
		history   sql.NullString
		createdAt time.Time
		updatedAt time.Time
	)

	err := h.db.QueryRow(`
		SELECT id, title, status, context_id, artifacts, history, created_at, updated_at
		FROM tasks WHERE id = ?`, taskID).
		Scan(&id, &title, &status, &contextID, &artifacts, &history, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	// Map internal status to A2A state
	a2aState := mapInternalToA2AState(status)

	task := &Task{
		Kind: "task",
		ID:   id,
		Status: TaskStatus{
			State:     a2aState,
			Timestamp: updatedAt.UTC().Format(time.RFC3339),
		},
		Metadata: map[string]interface{}{
			"title":      title,
			"created_at": createdAt.UTC().Format(time.RFC3339),
		},
	}

	if contextID.Valid {
		task.ContextID = contextID.String
	}

	// Parse artifacts
	if artifacts.Valid && artifacts.String != "" {
		var arts []Artifact
		if err := json.Unmarshal([]byte(artifacts.String), &arts); err == nil {
			task.Artifacts = arts
		} else {
			log.Printf("⚠️ Failed to parse artifacts for task %s: %v", taskID, err)
		}
	}

	// Parse history with optional length limit
	if history.Valid && history.String != "" {
		var msgs []Message
		if err := json.Unmarshal([]byte(history.String), &msgs); err == nil {
			if historyLength != nil && *historyLength > 0 && len(msgs) > *historyLength {
				// Return only the last N messages
				msgs = msgs[len(msgs)-*historyLength:]
			}
			task.History = msgs
		} else {
			log.Printf("⚠️ Failed to parse history for task %s: %v", taskID, err)
		}
	}

	return task, nil
}

// mapInternalToA2AState maps our internal task status to A2A state constants.
func mapInternalToA2AState(internalStatus string) string {
	switch internalStatus {
	case "pending":
		return TaskStateSubmitted
	case "running":
		return TaskStateWorking
	case "completed":
		return TaskStateCompleted
	case "failed":
		return TaskStateFailed
	case "cancelled":
		return TaskStateCanceled
	default:
		// Pass through for A2A-native states already stored
		return internalStatus
	}
}
