package tools

// Task tool wrappers that implement the Tool interface

// CreateTaskToolWrapper wraps the create task function
type CreateTaskToolWrapper struct{}

func (t *CreateTaskToolWrapper) Name() string {
	return "create_task"
}

func (t *CreateTaskToolWrapper) DisplayName() string {
	return "Create Task"
}

func (t *CreateTaskToolWrapper) Description() string {
	return `Create a scheduled or recurring task. Tasks are automatically executed by the scheduler when their time comes.

SCHEDULING:
- One-time task: Set execute_at to a future ISO datetime
- Recurring task: Set recurrence to a cron expression (next_run is auto-calculated from now)
- Immediate task: Omit both execute_at and recurrence, then call execute_task

RECURRENCE PATTERNS (cron format - 5 fields):
  ┌─ minute (0-59)
  │ ┌─ hour (0-23)
  │ │ ┌─ day of month (1-31)
  │ │ │ ┌─ month (1-12)
  │ │ │ │ ┌─ day of week (0-6, Sun=0)
  │ │ │ │ │
  * * * * *

Examples:
- "0 9 * * *" = Daily at 9am
- "0 9 * * 1-5" = Weekdays at 9am
- "0 9 * * 1,3,5" = Mon/Wed/Fri at 9am
- "0 9 * * 4,5" = Thu/Fri at 9am
- "0 */2 * * *" = Every 2 hours
- "0 9 1 * *" = 1st of month at 9am
- "0 0 * * 0" = Every Sunday at midnight

Legacy patterns: "daily", "weekly", "monthly" (prefer cron for flexibility)`
}

func (t *CreateTaskToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Task title (required)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Task instructions for the executing agent. Keep concise - describe WHAT to do, not background context the agent can look up itself.",
			},
			"execute_at": map[string]interface{}{
				"type":        "string",
				"description": "ISO datetime for one-time tasks (e.g., '2026-01-20T09:00:00Z'). Optional for recurring tasks - if omitted, next_run is calculated from now based on recurrence pattern.",
			},
			"recurrence": map[string]interface{}{
				"type":        "string",
				"description": "Cron expression for recurring tasks. Format: 'minute hour day-of-month month day-of-week'. When set, the task automatically repeats. Does NOT require execute_at - next run is calculated from current time.",
				"examples": []string{
					"0 9 * * *",
					"0 9 * * 1-5",
					"0 9 * * 1,3,5",
					"0 9 * * 4,5",
					"0 */2 * * *",
					"0 9 1 * *",
					"daily",
					"weekly",
				},
			},
			"priority": map[string]interface{}{
				"type":        "integer",
				"description": "Task priority 1-10 (higher = more important)",
				"minimum":     1,
				"maximum":     10,
				"default":     5,
			},
		},
		"required": []string{"title"},
	}
}

func (t *CreateTaskToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return CreateTaskTool(params)
}

// ListTasksToolWrapper wraps the list tasks function
type ListTasksToolWrapper struct{}

func (t *ListTasksToolWrapper) Name() string {
	return "list_tasks"
}

func (t *ListTasksToolWrapper) DisplayName() string {
	return "List Tasks"
}

func (t *ListTasksToolWrapper) Description() string {
	return "List tasks filtered by status (use 'all' to see all tasks)"
}

func (t *ListTasksToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"status": map[string]interface{}{
				"type": "string",
				"enum": []string{"all", "pending", "running", "completed", "failed", "cancelled"},
				"description": "Task status filter (required - use 'all' to see all tasks)",
				"default": "all",
			},
			"type": map[string]interface{}{
				"type": "string",
				"enum": []string{"once", "recurring"},
			},
			"upcoming": map[string]interface{}{
				"type":        "boolean",
				"description": "Only show future tasks",
				"default":     false,
			},
			"limit": map[string]interface{}{
				"type":    "integer",
				"default": 20,
			},
		},
		"required": []string{"status"},
	}
}

func (t *ListTasksToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return ListTasksTool(params)
}

// GetTaskToolWrapper wraps the get task function
type GetTaskToolWrapper struct{}

func (t *GetTaskToolWrapper) Name() string {
	return "get_task"
}

func (t *GetTaskToolWrapper) DisplayName() string {
	return "Get Task"
}

func (t *GetTaskToolWrapper) Description() string {
	return "Get detailed information about a task"
}

func (t *GetTaskToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "Task ID to retrieve",
			},
		},
		"required": []string{"task_id"},
	}
}

func (t *GetTaskToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return GetTaskTool(params)
}

// ExecuteTaskToolWrapper wraps the execute task function with streaming support
type ExecuteTaskToolWrapper struct{}

func (t *ExecuteTaskToolWrapper) Name() string {
	return "execute_task"
}

func (t *ExecuteTaskToolWrapper) DisplayName() string {
	return "Execute Task"
}

func (t *ExecuteTaskToolWrapper) Description() string {
	return "Execute a task immediately. When sync=true, you can see the task execution progress in real-time."
}

func (t *ExecuteTaskToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "Task ID to execute",
			},
			"sync": map[string]interface{}{
				"type":        "boolean",
				"description": "Execute synchronously and wait for completion with streaming output (default: false)",
				"default":     false,
			},
		},
		"required": []string{"task_id"},
	}
}

// SupportsStreaming returns true - this tool can stream task execution output
func (t *ExecuteTaskToolWrapper) SupportsStreaming() bool {
	return true
}

// ExecuteStreaming executes a task with streaming output (only when sync=true)
func (t *ExecuteTaskToolWrapper) ExecuteStreaming(params map[string]interface{}, callback StreamCallback) (interface{}, error) {
	// Only stream when sync=true, otherwise fall back to regular async execution
	sync := false
	if syncVal, ok := params["sync"].(bool); ok {
		sync = syncVal
	}

	if !sync {
		// Async execution - use regular non-streaming path
		return ExecuteTaskTool(params)
	}

	// Sync execution with streaming output
	return ExecuteTaskToolStreaming(params, callback)
}

// Execute falls back to non-streaming for backwards compatibility
func (t *ExecuteTaskToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return ExecuteTaskTool(params)
}

// UpdateTaskToolWrapper wraps the update task function
type UpdateTaskToolWrapper struct{}

func (t *UpdateTaskToolWrapper) Name() string {
	return "update_task"
}

func (t *UpdateTaskToolWrapper) DisplayName() string {
	return "Update Task"
}

func (t *UpdateTaskToolWrapper) Description() string {
	return `Update task properties, status, or store execution results.

COMMON USES:
- Mark task completed: status="completed" with result containing output
- Mark task failed: status="failed" with result containing error details
- Reschedule: Update execute_at to a new datetime
- Change recurrence: Update recurrence pattern (cron expression)
- Cancel task: status="cancelled"

NOTE: After a recurring task completes, create a new task with the same recurrence to schedule the next occurrence.`
}

func (t *UpdateTaskToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "Task ID to update (required)",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "New task title",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "New task instructions. Keep concise - describe WHAT to do, not background context.",
			},
			"execute_at": map[string]interface{}{
				"type":        "string",
				"description": "New execution time (ISO datetime). Use to reschedule a task.",
			},
			"recurrence": map[string]interface{}{
				"type":        "string",
				"description": "New recurrence pattern (cron expression). See create_task for format details.",
			},
			"priority": map[string]interface{}{
				"type":        "integer",
				"description": "New priority 1-10",
				"minimum":     1,
				"maximum":     10,
			},
			"status": map[string]interface{}{
				"type":        "string",
				"description": "New status. Use 'completed' when task succeeds, 'failed' on error, 'cancelled' to stop.",
				"enum":        []string{"pending", "completed", "failed", "cancelled"},
			},
			"result": map[string]interface{}{
				"type":        "object",
				"description": "Task execution result. Store output data, error messages, or summary here.",
			},
		},
		"required": []string{"task_id"},
	}
}

func (t *UpdateTaskToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return UpdateTaskTool(params)
}

// DeleteTaskToolWrapper wraps the delete task function
type DeleteTaskToolWrapper struct{}

func (t *DeleteTaskToolWrapper) Name() string {
	return "delete_task"
}

func (t *DeleteTaskToolWrapper) DisplayName() string {
	return "Delete Task"
}

func (t *DeleteTaskToolWrapper) Description() string {
	return "Delete a task permanently"
}

func (t *DeleteTaskToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "Task ID to delete",
			},
		},
		"required": []string{"task_id"},
	}
}

func (t *DeleteTaskToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return DeleteTaskTool(params)
}

// RegisterTaskTools registers all task management tools
func RegisterTaskTools() {
	registry := GetGlobalRegistry()
	registry.RegisterTool(&CreateTaskToolWrapper{})
	registry.RegisterTool(&ListTasksToolWrapper{})
	registry.RegisterTool(&GetTaskToolWrapper{})
	registry.RegisterTool(&ExecuteTaskToolWrapper{})
	registry.RegisterTool(&UpdateTaskToolWrapper{})
	registry.RegisterTool(&DeleteTaskToolWrapper{})
}