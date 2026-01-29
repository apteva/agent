package scheduler

import (
	"encoding/json"
	"net/http"
)

// HandleSchedulerStatus returns the current scheduler status
func HandleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get the global scheduler instance (will be nil if not initialized)
	var status SchedulerStatus
	if globalScheduler != nil {
		status = globalScheduler.GetStatus()
	} else {
		// Scheduler not initialized
		status = SchedulerStatus{
			Running:   false,
			Uptime:    "0s",
			TickCount: 0,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// HandleSchedulerTrigger manually triggers a scheduler tick
func HandleSchedulerTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if scheduler is initialized
	if globalScheduler == nil {
		http.Error(w, "Scheduler not initialized", http.StatusServiceUnavailable)
		return
	}

	// Trigger the tick
	if err := globalScheduler.TriggerTick(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Scheduler tick triggered successfully",
		"status":  globalScheduler.GetStatus(),
	})
}

// HandleSchedulerStart starts the scheduler
func HandleSchedulerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if scheduler is initialized
	if globalScheduler == nil {
		http.Error(w, "Scheduler not initialized", http.StatusServiceUnavailable)
		return
	}

	// Start the scheduler
	if err := globalScheduler.Start(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Scheduler started successfully",
		"status":  globalScheduler.GetStatus(),
	})
}

// HandleSchedulerStop stops the scheduler
func HandleSchedulerStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if scheduler is initialized
	if globalScheduler == nil {
		http.Error(w, "Scheduler not initialized", http.StatusServiceUnavailable)
		return
	}

	// Stop the scheduler
	globalScheduler.Stop()

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Scheduler stopped successfully",
		"status":  globalScheduler.GetStatus(),
	})
}

// HandleSchedulerJobs returns the list of scheduled jobs/tasks
func HandleSchedulerJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if scheduler is initialized and has database
	if globalScheduler == nil {
		http.Error(w, "Scheduler not initialized", http.StatusServiceUnavailable)
		return
	}

	jobs, err := globalScheduler.GetJobs()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"jobs": jobs,
	})
}