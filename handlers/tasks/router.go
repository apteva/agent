package tasks

import (
	"net/http"
)

// RegisterRoutes registers all task-related routes
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/tasks", HandleTasks)
	mux.HandleFunc("/tasks/", HandleTaskOperations)
	mux.HandleFunc("/tools/available", HandleAvailableTools)
}
