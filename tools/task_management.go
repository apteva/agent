package tools

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/apteva/agent/events"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// Task represents a task in the database
type Task struct {
	ID          string     `json:"id"`
	ThreadID    *string    `json:"thread_id,omitempty"`
	Title       string     `json:"title"`
	Description *string    `json:"description,omitempty"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	Priority    int        `json:"priority"`
	ExecuteAt   *time.Time `json:"execute_at,omitempty"`
	ExecutedAt  *time.Time `json:"executed_at,omitempty"`
	Recurrence  *string    `json:"recurrence,omitempty"`
	NextRun     *time.Time `json:"next_run,omitempty"`
	Result      *string    `json:"result,omitempty"`
	Source      string     `json:"source"`              // 'local' or 'delegated'
	DelegatorID *string    `json:"delegator_id,omitempty"` // Agent ID that delegated this task
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

var taskDB *sql.DB

// SetTaskDB sets the database connection for task operations
func SetTaskDB(db *sql.DB) {
	taskDB = db
}

// CreateTaskTool creates a new task
func CreateTaskTool(input map[string]interface{}) (map[string]interface{}, error) {
	if taskDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	taskID := "task_" + uuid.New().String()[:8]
	eventBus := events.GetEventBus()

	// Extract required fields
	title, ok := input["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required")
	}

	// Extract optional fields
	description, _ := input["description"].(string)
	priority := 5
	if p, ok := input["priority"].(float64); ok {
		priority = int(p)
	}

	taskType := "once"
	var recurrence, executeAt, nextRun *string

	// Handle recurrence
	if rec, ok := input["recurrence"].(string); ok && rec != "" {
		// Validate recurrence pattern
		if err := ValidateRecurrence(rec); err != nil {
			return nil, fmt.Errorf("invalid recurrence: %w", err)
		}

		taskType = "recurring"
		recurrence = &rec
		// Calculate next run based on recurrence
		nextRun = calculateNextRun(rec, executeAt)
	}

	// Handle scheduled execution
	if execAt, ok := input["execute_at"].(string); ok && execAt != "" {
		executeAt = &execAt
		if taskType == "recurring" && nextRun == nil {
			nextRun = &execAt
		}
	}

	// Get thread ID from context if available
	var threadID *string
	if tid, ok := input["_thread_id"].(string); ok {
		threadID = &tid
	}

	// Handle delegation fields
	source := "local"
	var delegatorID *string
	if src, ok := input["source"].(string); ok && src == "delegated" {
		source = "delegated"
	}
	if did, ok := input["delegator_id"].(string); ok && did != "" {
		delegatorID = &did
		source = "delegated" // Automatically set source if delegator_id is provided
	}

	// Insert into database
	_, err := taskDB.Exec(`
		INSERT INTO tasks (id, thread_id, title, description, type, priority, execute_at, recurrence, next_run, status, source, delegator_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		taskID, threadID, title, description, taskType, priority, executeAt, recurrence, nextRun, source, delegatorID,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	// Publish task created event
	taskEvent := events.NewEvent(events.CategoryTask, events.TypeTaskCreated, events.LevelInfo).
		WithTask(taskID).
		WithData("title", title).
		WithData("type", taskType).
		WithData("priority", priority)

	if threadID != nil {
		taskEvent = taskEvent.WithThread(*threadID)
	}

	eventBus.Publish(taskEvent)

	response := map[string]interface{}{
		"success":    true,
		"task_id":    taskID,
		"message":    fmt.Sprintf("Task '%s' created successfully", title),
		"type":       taskType,
		"source":     source,
		"execute_at": executeAt,
	}
	if delegatorID != nil {
		response["delegator_id"] = *delegatorID
	}
	return response, nil
}

// ListTasksTool lists tasks with filters
func ListTasksTool(input map[string]interface{}) (map[string]interface{}, error) {
	if taskDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	// Default status to "all" if not provided
	if _, ok := input["status"]; !ok {
		input["status"] = "all"
	}

	query := `SELECT id, thread_id, title, description, type, status, priority,
	          execute_at, executed_at, recurrence, next_run, result,
	          COALESCE(source, 'user') as source, delegator_id,
	          created_at, updated_at
	          FROM tasks WHERE 1=1`
	args := []interface{}{}

	// Add filters (skip if status is "all")
	if status, ok := input["status"].(string); ok && status != "" && status != "all" {
		query += " AND status = ?"
		args = append(args, status)
	}

	if taskType, ok := input["type"].(string); ok && taskType != "" {
		query += " AND type = ?"
		args = append(args, taskType)
	}

	if upcoming, ok := input["upcoming"].(bool); ok && upcoming {
		query += " AND (execute_at > datetime('now') OR next_run > datetime('now'))"
	}

	// Add thread filter if provided
	if threadID, ok := input["_thread_id"].(string); ok {
		query += " AND thread_id = ?"
		args = append(args, threadID)
	}

	query += " ORDER BY COALESCE(execute_at, next_run, created_at) ASC"

	limit := 20
	if l, ok := input["limit"].(float64); ok {
		limit = int(l)
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := taskDB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []map[string]interface{}
	for rows.Next() {
		var task Task
		var threadID, description, recurrence, result, delegatorID sql.NullString
		var executeAt, executedAt, nextRun sql.NullTime

		err := rows.Scan(&task.ID, &threadID, &task.Title, &description,
			&task.Type, &task.Status, &task.Priority,
			&executeAt, &executedAt, &recurrence, &nextRun, &result,
			&task.Source, &delegatorID,
			&task.CreatedAt, &task.UpdatedAt)
		if err != nil {
			continue
		}

		taskMap := map[string]interface{}{
			"id":         task.ID,
			"title":      task.Title,
			"type":       task.Type,
			"status":     task.Status,
			"priority":   task.Priority,
			"source":     task.Source,
			"created_at": task.CreatedAt.Format(time.RFC3339),
		}

		if threadID.Valid {
			taskMap["thread_id"] = threadID.String
		}
		if description.Valid {
			taskMap["description"] = description.String
		}
		if executeAt.Valid {
			taskMap["execute_at"] = executeAt.Time.Format(time.RFC3339)
		}
		if executedAt.Valid {
			taskMap["executed_at"] = executedAt.Time.Format(time.RFC3339)
		}
		if recurrence.Valid {
			taskMap["recurrence"] = recurrence.String
		}
		if nextRun.Valid {
			taskMap["next_run"] = nextRun.Time.Format(time.RFC3339)
		}
		if result.Valid {
			taskMap["result"] = result.String
		}
		if delegatorID.Valid {
			taskMap["delegator_id"] = delegatorID.String
		}

		tasks = append(tasks, taskMap)
	}

	// Publish task listed event (optional - for debugging)
	eventBus := events.GetEventBus()
	listEvent := events.NewEvent(events.CategoryTask, events.TypeTaskListed, events.LevelDebug).
		WithData("count", len(tasks)).
		WithData("filters", input)
	eventBus.Publish(listEvent)

	return map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	}, nil
}

// GetTaskTool gets detailed information about a task
func GetTaskTool(input map[string]interface{}) (map[string]interface{}, error) {
	if taskDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	var task Task
	var threadID, description, recurrence, result, source, delegatorID sql.NullString
	var executeAt, executedAt, nextRun sql.NullTime

	err := taskDB.QueryRow(`
		SELECT id, thread_id, title, description, type, status, priority,
		       execute_at, executed_at, recurrence, next_run, result,
		       COALESCE(source, 'local'), delegator_id, created_at, updated_at
		FROM tasks WHERE id = ?`, taskID).
		Scan(&task.ID, &threadID, &task.Title, &description,
			&task.Type, &task.Status, &task.Priority,
			&executeAt, &executedAt, &recurrence, &nextRun, &result,
			&source, &delegatorID, &task.CreatedAt, &task.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	taskMap := map[string]interface{}{
		"success":    true,
		"id":         task.ID,
		"title":      task.Title,
		"type":       task.Type,
		"status":     task.Status,
		"priority":   task.Priority,
		"source":     source.String,
		"created_at": task.CreatedAt.Format(time.RFC3339),
		"updated_at": task.UpdatedAt.Format(time.RFC3339),
	}

	if threadID.Valid {
		taskMap["thread_id"] = threadID.String

		// Fetch trajectory (messages from the thread)
		trajectory, err := getTaskTrajectory(threadID.String)
		if err != nil {
			log.Printf("Warning: failed to get trajectory for task %s: %v", taskID, err)
		} else if len(trajectory) > 0 {
			taskMap["trajectory"] = trajectory
		}
	}
	if description.Valid {
		taskMap["description"] = description.String
	}
	if executeAt.Valid {
		taskMap["execute_at"] = executeAt.Time.Format(time.RFC3339)
	}
	if executedAt.Valid {
		taskMap["executed_at"] = executedAt.Time.Format(time.RFC3339)
	}
	if recurrence.Valid {
		taskMap["recurrence"] = recurrence.String
	}
	if nextRun.Valid {
		taskMap["next_run"] = nextRun.Time.Format(time.RFC3339)
	}
	if result.Valid {
		// Try to parse result as JSON
		var resultData interface{}
		if err := json.Unmarshal([]byte(result.String), &resultData); err == nil {
			taskMap["result"] = resultData
		} else {
			taskMap["result"] = result.String
		}
	}
	if delegatorID.Valid {
		taskMap["delegator_id"] = delegatorID.String
	}

	return taskMap, nil
}

// getTaskTrajectory fetches the messages (trajectory) for a task's thread
func getTaskTrajectory(threadID string) ([]map[string]interface{}, error) {
	if taskDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := taskDB.Query(`
		SELECT id, role, content, model, created_at, metadata
		FROM messages
		WHERE thread_id = ?
		ORDER BY created_at ASC`, threadID)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var trajectory []map[string]interface{}
	for rows.Next() {
		var id, role, contentStr string
		var model sql.NullString
		var createdAt time.Time
		var metadataStr string

		err := rows.Scan(&id, &role, &contentStr, &model, &createdAt, &metadataStr)
		if err != nil {
			continue
		}

		msg := map[string]interface{}{
			"id":         id,
			"role":       role,
			"created_at": createdAt.Format(time.RFC3339),
		}

		if model.Valid {
			msg["model"] = model.String
		}

		// Parse content - could be string or JSON
		trimmedContent := strings.TrimSpace(contentStr)
		if strings.HasPrefix(trimmedContent, "{") || strings.HasPrefix(trimmedContent, "[") {
			var content interface{}
			if err := json.Unmarshal([]byte(contentStr), &content); err == nil {
				msg["content"] = content
			} else {
				msg["content"] = contentStr
			}
		} else {
			msg["content"] = contentStr
		}

		// Parse metadata
		if metadataStr != "" && metadataStr != "{}" {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(metadataStr), &metadata); err == nil && len(metadata) > 0 {
				msg["metadata"] = metadata
			}
		}

		trajectory = append(trajectory, msg)
	}

	return trajectory, nil
}

// ExecuteTaskTool executes a task and stores result
func ExecuteTaskTool(input map[string]interface{}) (map[string]interface{}, error) {
	if taskDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	eventBus := events.GetEventBus()

	// Check task exists and is pending
	var status, taskType, recurrence string
	err := taskDB.QueryRow("SELECT status, type, COALESCE(recurrence, '') FROM tasks WHERE id = ?", taskID).
		Scan(&status, &taskType, &recurrence)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to check task: %w", err)
	}

	if status != "pending" && status != "failed" {
		return nil, fmt.Errorf("task is not in pending or failed state (current: %s)", status)
	}

	// Update status to running
	_, err = taskDB.Exec("UPDATE tasks SET status = 'running', executed_at = ?, updated_at = ? WHERE id = ?",
		time.Now(), time.Now(), taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to update task status: %w", err)
	}

	// Publish task execution started event
	execEvent := events.NewEvent(events.CategoryTask, events.TypeTaskExecuted, events.LevelInfo).
		WithTask(taskID).
		WithData("status", "running")
	eventBus.Publish(execEvent)

	// Get task details for agent execution
	var title, description string
	err = taskDB.QueryRow("SELECT title, COALESCE(description, '') FROM tasks WHERE id = ?", taskID).
		Scan(&title, &description)
	if err != nil {
		return nil, fmt.Errorf("failed to get task details: %w", err)
	}

	// Generate thread ID that will be used for execution
	threadID := fmt.Sprintf("task_%s_%d", taskID, time.Now().Unix())

	// Check if sync execution is requested
	sync := false
	if syncVal, ok := input["sync"].(bool); ok {
		sync = syncVal
	}

	if sync {
		// Execute synchronously and wait for completion
		log.Printf("Starting synchronous execution for task %s with thread %s", taskID, threadID)
		err := executeTask(taskID, title, description, threadID, true)
		if err != nil {
			// Task execution failed
			return map[string]interface{}{
				"status":    "failed",
				"message":   fmt.Sprintf("Task '%s' execution failed: %v", title, err),
				"task_id":   taskID,
				"thread_id": threadID,
			}, nil
		}

		// Get the updated task status and result
		var finalStatus, resultStr string
		err = taskDB.QueryRow("SELECT status, COALESCE(result, '{}') FROM tasks WHERE id = ?", taskID).
			Scan(&finalStatus, &resultStr)
		if err != nil {
			finalStatus = "unknown"
			resultStr = "{}"
		}

		// Try to parse result as JSON
		var resultData interface{}
		if err := json.Unmarshal([]byte(resultStr), &resultData); err == nil {
			return map[string]interface{}{
				"status":    finalStatus,
				"message":   fmt.Sprintf("Task '%s' completed synchronously", title),
				"task_id":   taskID,
				"thread_id": threadID,
				"result":    resultData,
			}, nil
		}

		return map[string]interface{}{
			"status":    finalStatus,
			"message":   fmt.Sprintf("Task '%s' completed synchronously", title),
			"task_id":   taskID,
			"thread_id": threadID,
			"result":    resultStr,
		}, nil
	}

	// Execute task asynchronously using goroutine
	log.Printf("Starting goroutine for task %s execution with thread %s", taskID, threadID)
	go func() {
		log.Printf("Goroutine started for task %s", taskID)
		executeTask(taskID, title, description, threadID, false)
	}()

	// Return immediate response
	result := map[string]interface{}{
		"status":    "running",
		"message":   fmt.Sprintf("Task '%s' is now being executed in a new thread. The task will run independently and update its own status when complete. You can check the task status later or view the execution in the Threads page.", title),
		"task_id":   taskID,
		"thread_id": threadID,
	}

	// Note: Task completion and recurring task scheduling will be handled by the agent via update_task

	return result, nil
}

// ExecuteTaskToolStreaming executes a task with streaming output
func ExecuteTaskToolStreaming(input map[string]interface{}, callback StreamCallback) (map[string]interface{}, error) {
	if taskDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	eventBus := events.GetEventBus()

	// Check task exists and is pending
	var status, taskType, recurrence string
	err := taskDB.QueryRow("SELECT status, type, COALESCE(recurrence, '') FROM tasks WHERE id = ?", taskID).
		Scan(&status, &taskType, &recurrence)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to check task: %w", err)
	}

	if status != "pending" && status != "failed" {
		return nil, fmt.Errorf("task is not in pending or failed state (current: %s)", status)
	}

	// Update status to running
	_, err = taskDB.Exec("UPDATE tasks SET status = 'running', executed_at = ?, updated_at = ? WHERE id = ?",
		time.Now(), time.Now(), taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to update task status: %w", err)
	}

	// Send progress event
	callback(NewProgressEvent("Starting task execution...", 0.1))

	// Publish task execution started event
	execEvent := events.NewEvent(events.CategoryTask, events.TypeTaskExecuted, events.LevelInfo).
		WithTask(taskID).
		WithData("status", "running").
		WithData("streaming", true)
	eventBus.Publish(execEvent)

	// Get task details for agent execution
	var title, description string
	err = taskDB.QueryRow("SELECT title, COALESCE(description, '') FROM tasks WHERE id = ?", taskID).
		Scan(&title, &description)
	if err != nil {
		return nil, fmt.Errorf("failed to get task details: %w", err)
	}

	// Generate thread ID for execution
	threadID := fmt.Sprintf("task_%s_%d", taskID, time.Now().Unix())

	callback(NewLogEvent(fmt.Sprintf("Executing task: %s", title)))

	// Execute with streaming
	result, err := executeTaskStreaming(taskID, title, description, threadID, callback)
	if err != nil {
		callback(NewErrorEvent(err.Error()))
		return map[string]interface{}{
			"status":    "failed",
			"message":   fmt.Sprintf("Task '%s' execution failed: %v", title, err),
			"task_id":   taskID,
			"thread_id": threadID,
		}, nil
	}

	callback(NewProgressEvent("Task completed", 1.0))

	// Get the updated task status and result
	var finalStatus, resultStr string
	err = taskDB.QueryRow("SELECT status, COALESCE(result, '{}') FROM tasks WHERE id = ?", taskID).
		Scan(&finalStatus, &resultStr)
	if err != nil {
		finalStatus = "unknown"
		resultStr = "{}"
	}

	// Try to parse result as JSON
	var resultData interface{}
	if err := json.Unmarshal([]byte(resultStr), &resultData); err == nil {
		result["result"] = resultData
	} else {
		result["result"] = resultStr
	}

	result["status"] = finalStatus
	result["message"] = fmt.Sprintf("Task '%s' completed", title)
	result["task_id"] = taskID
	result["thread_id"] = threadID

	return result, nil
}

// executeTaskStreaming executes a task with streaming output via SSE
func executeTaskStreaming(taskID, title, description, threadID string, callback StreamCallback) (map[string]interface{}, error) {
	log.Printf("executeTaskStreaming called for task %s with thread %s", taskID, threadID)

	// Create a new thread in the database for this task execution
	threadTitle := fmt.Sprintf("Task Execution: %s", title)

	// Insert the thread into the database with task_id in metadata
	threadMetadata := fmt.Sprintf(`{"task_id":"%s"}`, taskID)
	_, err := taskDB.Exec(
		"INSERT INTO threads (id, title, created_at, updated_at, metadata) VALUES (?, ?, ?, ?, ?)",
		threadID, threadTitle, time.Now(), time.Now(), threadMetadata,
	)
	if err != nil {
		log.Printf("Failed to create thread for task %s: %v", taskID, err)
		updateTaskFailed(taskID, fmt.Sprintf("Failed to create thread: %v", err))
		return nil, fmt.Errorf("failed to create thread: %w", err)
	}
	log.Printf("Created thread %s for task %s execution", threadID, taskID)

	callback(NewProgressEvent("Thread created, starting agent...", 0.2))

	// Update the task with the thread_id
	_, err = taskDB.Exec("UPDATE tasks SET thread_id = ? WHERE id = ?", threadID, taskID)
	if err != nil {
		log.Printf("Failed to update task %s with thread_id: %v", taskID, err)
	}

	// Build the prompt for the agent
	prompt := fmt.Sprintf(`You are executing a scheduled task autonomously. No user is present.

Task ID: %s
Title: %s
Description: %s

IMPORTANT: You ARE the task execution — do NOT call execute_task or get_task.
Your job is to perform the work described above using your available tools (search, fetch, generate content, etc.).

INSTRUCTIONS:
1. Run autonomously - DO NOT ask questions. Make your best judgment.
2. Perform the work described in the task title and description using available tools.
3. Keep your text responses SHORT - just status updates like "Generating article...", "Fetching data...", "Processing complete."
4. NO verbose explanations or full sentences - save tokens. Only output brief progress indicators.
5. When done: call update_task with task_id="%s" and status="completed" with brief results.
6. On failure: call update_task with task_id="%s" and status="failed" with error.

Begin.`,
		taskID, title, description, taskID, taskID)

	// Get current trace for parent linkage
	var parentTraceID string
	if currentTrace := events.GetCurrentTrace(); currentTrace != nil {
		parentTraceID = currentTrace.ID
		log.Printf("Task %s (streaming) will link to parent trace %s", taskID, parentTraceID)
	}

	// Prepare the chat request with streaming
	chatRequest := map[string]interface{}{
		"message":   prompt,
		"stream":    true, // STREAMING MODE
		"thread_id": threadID,
		"source":    "task", // This is the actual task execution
	}

	// Add parent trace linkage if available
	if parentTraceID != "" {
		chatRequest["parent_trace_id"] = parentTraceID
	}

	requestBody, err := json.Marshal(chatRequest)
	if err != nil {
		log.Printf("Failed to marshal chat request for task %s: %v", taskID, err)
		updateTaskFailed(taskID, fmt.Sprintf("Failed to marshal request: %v", err))
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Get the port from environment variable
	port := os.Getenv("PORT")
	if port == "" {
		port = "4015"
	}
	chatURL := fmt.Sprintf("http://localhost:%s/chat", port)

	callback(NewProgressEvent("Connecting to agent...", 0.3))

	log.Printf("Calling chat endpoint for task %s with streaming at %s", taskID, chatURL)

	// Make streaming request
	req, err := http.NewRequest("POST", chatURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	// Add API key for internal auth
	if apiKey := os.Getenv("AGENT_API_KEY"); apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Minute} // Long timeout for task execution
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to call chat endpoint for task %s: %v", taskID, err)
		updateTaskFailed(taskID, fmt.Sprintf("Failed to call chat endpoint: %v", err))
		return nil, fmt.Errorf("failed to call chat endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Task %s chat endpoint returned status %d: %s", taskID, resp.StatusCode, string(body))
		updateTaskFailed(taskID, fmt.Sprintf("Chat endpoint returned status %d", resp.StatusCode))
		return nil, fmt.Errorf("chat endpoint returned status %d", resp.StatusCode)
	}

	callback(NewProgressEvent("Agent executing task...", 0.4))

	// Parse SSE stream
	var fullResponse strings.Builder
	scanner := bufio.NewScanner(resp.Body)

	// Increase buffer size for large responses
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)

		switch eventType {
		case "content":
			// Stream content chunk
			content, _ := event["content"].(string)
			fullResponse.WriteString(content)
			callback(NewChunkEvent(content))

		case "tool_use", "tool_call":
			// Agent is using a tool
			toolName, _ := event["tool_name"].(string)
			callback(NewLogEvent(fmt.Sprintf("Using tool: %s", toolName)))

		case "tool_result":
			// Tool completed
			callback(NewLogEvent("Tool completed"))

		case "stop", "end":
			// Stream complete
			break

		case "error":
			// Error occurred
			errorMsg, _ := event["error"].(string)
			if errorMsg == "" {
				errorMsg, _ = event["content"].(string)
			}
			callback(NewErrorEvent(errorMsg))
		}
	}

	log.Printf("Task %s execution completed, response length: %d", taskID, fullResponse.Len())

	return map[string]interface{}{
		"response": fullResponse.String(),
	}, nil
}

// UpdateTaskTool updates task properties
func UpdateTaskTool(input map[string]interface{}) (map[string]interface{}, error) {
	if taskDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	// Build update query dynamically
	updates := []string{}
	args := []interface{}{}

	if title, ok := input["title"].(string); ok {
		updates = append(updates, "title = ?")
		args = append(args, title)
	}

	if description, ok := input["description"].(string); ok {
		updates = append(updates, "description = ?")
		args = append(args, description)
	}

	if executeAt, ok := input["execute_at"].(string); ok {
		updates = append(updates, "execute_at = ?")
		args = append(args, executeAt)
	}

	if recurrence, ok := input["recurrence"].(string); ok {
		// Validate recurrence pattern
		if recurrence != "" {
			if err := ValidateRecurrence(recurrence); err != nil {
				return nil, fmt.Errorf("invalid recurrence: %w", err)
			}
			updates = append(updates, "recurrence = ?")
			args = append(args, recurrence)
			updates = append(updates, "type = ?")
			args = append(args, "recurring")
			// Recalculate next_run
			nextRun := calculateNextRun(recurrence, nil)
			if nextRun != nil {
				updates = append(updates, "next_run = ?")
				args = append(args, *nextRun)
			}
		} else {
			// Clear recurrence
			updates = append(updates, "recurrence = NULL")
			updates = append(updates, "next_run = NULL")
			updates = append(updates, "type = ?")
			args = append(args, "once")
		}
	}

	if priority, ok := input["priority"].(float64); ok {
		updates = append(updates, "priority = ?")
		args = append(args, int(priority))
	}

	var newStatus string
	if status, ok := input["status"].(string); ok {
		if status == "pending" || status == "completed" || status == "failed" || status == "cancelled" || status == "running" {
			updates = append(updates, "status = ?")
			args = append(args, status)
			newStatus = status
		}
	}

	if result, ok := input["result"]; ok {
		resultJSON, _ := json.Marshal(result)
		updates = append(updates, "result = ?")
		args = append(args, string(resultJSON))
	}

	// If marking as completed, set executed_at
	if newStatus == "completed" {
		updates = append(updates, "executed_at = ?")
		args = append(args, time.Now())
	}

	if len(updates) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	// Always update updated_at
	updates = append(updates, "updated_at = ?")
	args = append(args, time.Now())

	// Add task ID for WHERE clause
	args = append(args, taskID)

	query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", joinStrings(updates, ", "))
	result, err := taskDB.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, fmt.Errorf("task not found")
	}

	// Publish task updated event
	eventBus := events.GetEventBus()
	updateEvent := events.NewEvent(events.CategoryTask, events.TypeTaskUpdated, events.LevelInfo).
		WithTask(taskID).
		WithData("status", newStatus).
		WithData("updates", len(updates)-1) // Exclude updated_at
	eventBus.Publish(updateEvent)

	return map[string]interface{}{
		"message": "Task updated successfully",
		"task_id": taskID,
	}, nil
}

// DeleteTaskTool deletes a task
func DeleteTaskTool(input map[string]interface{}) (map[string]interface{}, error) {
	if taskDB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	result, err := taskDB.Exec("DELETE FROM tasks WHERE id = ?", taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete task: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, fmt.Errorf("task not found")
	}

	// Publish task deleted event
	eventBus := events.GetEventBus()
	deleteEvent := events.NewEvent(events.CategoryTask, events.TypeTaskDeleted, events.LevelInfo).
		WithTask(taskID)
	eventBus.Publish(deleteEvent)

	return map[string]interface{}{
		"message": "Task deleted successfully",
		"task_id": taskID,
	}, nil
}

// Helper functions

func calculateNextRun(pattern string, from *string) *string {
	baseTime := time.Now()
	if from != nil {
		if t, err := time.Parse(time.RFC3339, *from); err == nil {
			baseTime = t
		}
	}

	var next time.Time

	// Check if it's a cron expression (auto-detect!)
	if IsCronExpression(pattern) {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		schedule, err := parser.Parse(pattern)
		if err != nil {
			log.Printf("Error parsing cron expression '%s': %v", pattern, err)
			return nil
		}

		next = schedule.Next(baseTime)
		result := next.Format(time.RFC3339)
		return &result
	}

	// Handle simple patterns (backward compatibility)
	switch pattern {
	case "daily":
		next = baseTime.AddDate(0, 0, 1)
	case "weekly":
		next = baseTime.AddDate(0, 0, 7)
	case "monthly":
		next = baseTime.AddDate(0, 1, 0)
	default:
		return nil
	}

	result := next.Format(time.RFC3339)
	return &result
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// executeTask executes a task by calling the agent's chat endpoint
// sync=true: waits for completion and returns error if any
// sync=false: executes in background, no error return (logs errors instead)
func executeTask(taskID, title, description, threadID string, sync bool) error {
	mode := "async"
	if sync {
		mode = "sync"
	}
	log.Printf("executeTask called for task %s with thread %s (mode: %s)", taskID, threadID, mode)

	// Create a new thread in the database for this task execution
	threadTitle := fmt.Sprintf("Task Execution: %s", title)

	// Insert the thread into the database with task_id in metadata
	threadMetadata := fmt.Sprintf(`{"task_id":"%s"}`, taskID)
	_, err := taskDB.Exec(
		"INSERT INTO threads (id, title, created_at, updated_at, metadata) VALUES (?, ?, ?, ?, ?)",
		threadID, threadTitle, time.Now(), time.Now(), threadMetadata,
	)
	if err != nil {
		log.Printf("Failed to create thread for task %s: %v", taskID, err)
		updateTaskFailed(taskID, fmt.Sprintf("Failed to create thread: %v", err))
		if sync {
			return fmt.Errorf("failed to create thread: %w", err)
		}
		return nil
	}
	log.Printf("Created thread %s for task %s execution", threadID, taskID)

	// Update the task with the thread_id
	_, err = taskDB.Exec("UPDATE tasks SET thread_id = ? WHERE id = ?", threadID, taskID)
	if err != nil {
		log.Printf("Failed to update task %s with thread_id: %v", taskID, err)
		// Continue anyway - not critical
	}

	// Get task type and recurrence info
	var taskType, recurrence string
	err = taskDB.QueryRow("SELECT type, COALESCE(recurrence, '') FROM tasks WHERE id = ?", taskID).
		Scan(&taskType, &recurrence)
	if err != nil {
		log.Printf("Error getting task type/recurrence for %s: %v", taskID, err)
		// Continue anyway with defaults
		taskType = "once"
		recurrence = ""
	}
	log.Printf("Task %s type=%s, recurrence=%s", taskID, taskType, recurrence)

	// Build the prompt for the agent
	prompt := fmt.Sprintf(`You are executing a scheduled task autonomously. No user is present.

Task ID: %s
Title: %s
Description: %s

IMPORTANT: You ARE the task execution — do NOT call execute_task or get_task.
Your job is to perform the work described above using your available tools (search, fetch, generate content, etc.).

INSTRUCTIONS:
1. Run autonomously - DO NOT ask questions. Make your best judgment.
2. Perform the work described in the task title and description using available tools.
3. Keep your text responses SHORT - just status updates like "Generating article...", "Fetching data...", "Processing complete."
4. NO verbose explanations or full sentences - save tokens. Only output brief progress indicators.
5. When done: call update_task with task_id="%s" and status="completed" with brief results.
6. On failure: call update_task with task_id="%s" and status="failed" with error.

Begin.`,
		taskID, title, description, taskID, taskID)

	// Get current trace for parent linkage
	var parentTraceID string
	if currentTrace := events.GetCurrentTrace(); currentTrace != nil {
		parentTraceID = currentTrace.ID
		log.Printf("Task %s will link to parent trace %s", taskID, parentTraceID)
	}

	// Prepare the chat request
	chatRequest := map[string]interface{}{
		"message":   prompt,
		"stream":    false,
		"thread_id": threadID,
		"source":    "task", // This is the actual task execution
	}

	// Add parent trace linkage if available
	if parentTraceID != "" {
		chatRequest["parent_trace_id"] = parentTraceID
	}

	// Call the chat endpoint
	requestBody, err := json.Marshal(chatRequest)
	if err != nil {
		log.Printf("Failed to marshal chat request for task %s: %v", taskID, err)
		updateTaskFailed(taskID, fmt.Sprintf("Failed to marshal request: %v", err))
		if sync {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		return nil
	}

	// Get the port from environment variable (same port the server is running on)
	port := os.Getenv("PORT")
	if port == "" {
		port = "4015"
	}
	chatURL := fmt.Sprintf("http://localhost:%s/chat", port)

	log.Printf("Calling chat endpoint for task %s with thread %s (%s) at %s", taskID, threadID, mode, chatURL)
	req, err := http.NewRequest("POST", chatURL, bytes.NewReader(requestBody))
	if err != nil {
		log.Printf("Failed to create request for task %s: %v", taskID, err)
		updateTaskFailed(taskID, fmt.Sprintf("Failed to create request: %v", err))
		if sync {
			return fmt.Errorf("failed to create request: %w", err)
		}
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	// Add API key for internal auth
	if apiKey := os.Getenv("AGENT_API_KEY"); apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to call chat endpoint for task %s: %v", taskID, err)
		updateTaskFailed(taskID, fmt.Sprintf("Failed to call chat endpoint: %v", err))
		if sync {
			return fmt.Errorf("failed to call chat endpoint: %w", err)
		}
		return nil
	}
	defer resp.Body.Close()

	// Read the response
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("Task %s chat endpoint returned status %d: %s", taskID, resp.StatusCode, string(body))
		updateTaskFailed(taskID, fmt.Sprintf("Chat endpoint returned status %d", resp.StatusCode))
		if sync {
			return fmt.Errorf("chat endpoint returned status %d", resp.StatusCode)
		}
		return nil
	}

	log.Printf("Task %s execution response received (status %d). Body: %s", taskID, resp.StatusCode, string(body))

	// The agent will handle updating the task status via update_task tool
	return nil
}

// updateTaskFailed is a helper to update task status to failed
func updateTaskFailed(taskID, errorMsg string) {
	if taskDB == nil {
		return
	}

	result := map[string]interface{}{
		"error": errorMsg,
	}
	resultJSON, _ := json.Marshal(result)
	_, err := taskDB.Exec("UPDATE tasks SET status = 'failed', result = ?, updated_at = ? WHERE id = ?",
		string(resultJSON), time.Now(), taskID)
	if err != nil {
		log.Printf("Failed to update task %s status to failed: %v", taskID, err)
	}
}