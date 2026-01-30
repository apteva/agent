package memories

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/memory"
)

// HandleMemories - GET/DELETE /memories
func HandleMemories(getMemoryManager MemoryManagerGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get current memory manager (may have been enabled/disabled dynamically)
		memoryManager := getMemoryManager()

		switch r.Method {
		case http.MethodGet:
			// Check config to see if memory is supposed to be enabled
			cfg := config.GetConfig().Get()
			configEnabled := cfg.Memory != nil && cfg.Memory.Enabled

			if memoryManager == nil {
				// Memory manager not initialized - could be config disabled or init failure
				response := map[string]interface{}{
					"memories":       []*memory.Memory{},
					"enabled":        false,
					"config_enabled": configEnabled,
					"count":          0,
				}
				if configEnabled {
					response["error"] = "Memory system failed to initialize (check OPENAI_API_KEY)"
				}
				sendJSON(w, http.StatusOK, response)
				return
			}

			// Get thread ID from query params (optional)
			threadID := r.URL.Query().Get("thread_id")

			memories, err := memoryManager.GetAllMemories(threadID)
			if err != nil {
				http.Error(w, "Failed to retrieve memories", http.StatusInternalServerError)
				return
			}

			sendJSON(w, http.StatusOK, map[string]interface{}{
				"memories":       memories,
				"enabled":        true,
				"config_enabled": configEnabled,
				"count":          len(memories),
			})

		case http.MethodDelete:
			if memoryManager == nil {
				http.Error(w, "Memory system not enabled", http.StatusBadRequest)
				return
			}

			if err := memoryManager.ClearAllMemories(); err != nil {
				http.Error(w, "Failed to clear memories", http.StatusInternalServerError)
				return
			}

			sendJSON(w, http.StatusOK, map[string]string{
				"message": "All memories cleared successfully",
			})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// HandleMemoryOperations - DELETE /memories/:id
func HandleMemoryOperations(getMemoryManager MemoryManagerGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get current memory manager (may have been enabled/disabled dynamically)
		memoryManager := getMemoryManager()

		if memoryManager == nil {
			http.Error(w, "Memory system not enabled", http.StatusBadRequest)
			return
		}

		// Extract memory ID from path
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/memories/"), "/")
		if len(pathParts) < 1 || pathParts[0] == "" {
			http.Error(w, "Memory ID required", http.StatusBadRequest)
			return
		}
		memoryID := pathParts[0]

		switch r.Method {
		case http.MethodDelete:
			if err := memoryManager.DeleteMemory(memoryID); err != nil {
				http.Error(w, "Failed to delete memory", http.StatusInternalServerError)
				return
			}

			sendJSON(w, http.StatusOK, map[string]string{
				"message": "Memory deleted successfully",
				"id":      memoryID,
			})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
