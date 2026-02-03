package skills

import (
	"net/http"
)

// RegisterRoutes registers all skills-related routes
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/skills", HandleSkills)
	mux.HandleFunc("/skills/status", handleSkillsStatusRouter)
	mux.HandleFunc("/skills/match", HandleSkillsMatch)
}

// handleSkillsStatusRouter routes GET to status, POST to enable/disable
func handleSkillsStatusRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		HandleSkillsStatus(w, r)
	case http.MethodPost:
		HandleSkillsEnable(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
