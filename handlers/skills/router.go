package skills

import (
	"net/http"
)

// RegisterRoutes registers all skills-related routes
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/skills", HandleSkills)
	mux.HandleFunc("/skills/status", HandleSkillsStatus)
	mux.HandleFunc("/skills/match", HandleSkillsMatch)
}
