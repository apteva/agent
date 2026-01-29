package tasks

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"agent-server/tools"
)

// HandleTasks - GET/POST /tasks
func HandleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// List tasks
		params := map[string]interface{}{}

		// Parse query parameters
		if status := r.URL.Query().Get("status"); status != "" {
			params["status"] = status
		}
		if taskType := r.URL.Query().Get("type"); taskType != "" {
			params["type"] = taskType
		}
		if upcoming := r.URL.Query().Get("upcoming"); upcoming == "true" {
			params["upcoming"] = true
		}
		if limit := r.URL.Query().Get("limit"); limit != "" {
			if l, err := strconv.Atoi(limit); err == nil {
				params["limit"] = float64(l)
			}
		}

		result, err := tools.ListTasksTool(params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sendJSON(w, http.StatusOK, result)

	case http.MethodPost:
		// Create task
		var input map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		result, err := tools.CreateTaskTool(input)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sendJSON(w, http.StatusCreated, result)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleTaskOperations - GET/PUT/DELETE /tasks/:id, POST /tasks/:id/execute
func HandleTaskOperations(w http.ResponseWriter, r *http.Request) {
	// Extract task ID from path: /tasks/{taskId} or /tasks/{taskId}/{operation}
	path := strings.TrimPrefix(r.URL.Path, "/tasks/")
	parts := strings.Split(path, "/")

	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "Task ID required", http.StatusBadRequest)
		return
	}

	taskID := parts[0]
	operation := ""
	if len(parts) > 1 {
		operation = parts[1]
	}

	switch operation {
	case "execute":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		result, err := tools.ExecuteTaskTool(map[string]interface{}{"task_id": taskID})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sendJSON(w, http.StatusOK, result)

	case "":
		// Operations on the task itself
		switch r.Method {
		case http.MethodGet:
			// Get task details
			result, err := tools.GetTaskTool(map[string]interface{}{"task_id": taskID})
			if err != nil {
				if err.Error() == "task not found" {
					http.Error(w, err.Error(), http.StatusNotFound)
				} else {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}
			sendJSON(w, http.StatusOK, result)

		case http.MethodPut:
			// Update task
			var input map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, "Invalid JSON", http.StatusBadRequest)
				return
			}
			input["task_id"] = taskID

			result, err := tools.UpdateTaskTool(input)
			if err != nil {
				if err.Error() == "task not found" {
					http.Error(w, err.Error(), http.StatusNotFound)
				} else {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
				return
			}
			sendJSON(w, http.StatusOK, result)

		case http.MethodDelete:
			// Delete task
			result, err := tools.DeleteTaskTool(map[string]interface{}{"task_id": taskID})
			if err != nil {
				if err.Error() == "task not found" {
					http.Error(w, err.Error(), http.StatusNotFound)
				} else {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}
			sendJSON(w, http.StatusOK, result)

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}

	default:
		http.Error(w, "Invalid operation", http.StatusBadRequest)
	}
}

// HandleAvailableTools - GET /tools/available
func HandleAvailableTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	registry := tools.GetGlobalRegistry()
	allTools := registry.ListTools()

	// Organize tools by category
	taskTools := []tools.ToolDefinition{}
	otherTools := []tools.ToolDefinition{}

	taskToolNames := map[string]bool{
		"create_task":  true,
		"list_tasks":   true,
		"get_task":     true,
		"execute_task": true,
		"update_task":  true,
		"delete_task":  true,
	}

	for _, tool := range allTools {
		if taskToolNames[tool.Name] {
			taskTools = append(taskTools, tool)
		} else {
			otherTools = append(otherTools, tool)
		}
	}

	response := map[string]interface{}{
		"local_tools": otherTools,
		"task_tools":  taskTools,
		"total_count": len(allTools),
	}

	sendJSON(w, http.StatusOK, response)
}

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
