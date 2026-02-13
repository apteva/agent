package reflection

import (
	"encoding/json"
	"net/http"
)

// HandleReflectionStatus returns the current reflection scheduler status
func HandleReflectionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var status ReflectionStatus
	if globalReflection != nil {
		status = globalReflection.GetStatus()
	} else {
		status = ReflectionStatus{
			Enabled: false,
			Running: false,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// HandleReflectionTrigger manually triggers a reflection session
func HandleReflectionTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if globalReflection == nil {
		http.Error(w, "Reflection not initialized", http.StatusServiceUnavailable)
		return
	}

	if err := globalReflection.TriggerReflection(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"message":      "Reflection triggered",
		"trigger_type": "manual",
	})
}

// RegisterRoutes registers reflection HTTP handlers on the given mux
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/reflection/status", HandleReflectionStatus)
	mux.HandleFunc("/reflection/trigger", HandleReflectionTrigger)
}
