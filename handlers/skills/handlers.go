package skills

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/skills"
)

// sendJSON sends a JSON response
func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// HandleSkillsStatus - GET /skills/status
func HandleSkillsStatus(w http.ResponseWriter, r *http.Request) {
	manager := skills.GetManager()
	cfg := config.GetConfig().Get()

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"enabled": manager.IsEnabled(),
			"count":   manager.Count(),
			"summary": manager.GetSkillsSummary(),
			"config":  cfg.Skills,
		},
	})
}

// HandleSkills - GET/POST/PUT/DELETE /skills
func HandleSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetSkills(w, r)
	case http.MethodPost:
		handleCreateSkill(w, r)
	case http.MethodPut:
		handleUpdateSkill(w, r)
	case http.MethodDelete:
		handleDeleteSkill(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetSkills returns all skills
func handleGetSkills(w http.ResponseWriter, r *http.Request) {
	cfg := config.GetConfig().Get()

	var allSkills []config.Skill
	if cfg.Skills != nil {
		allSkills = cfg.Skills.Definitions
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"skills":  allSkills,
		"count":   len(allSkills),
		"enabled": cfg.Skills != nil && cfg.Skills.Enabled,
	})
}

// handleCreateSkill creates a new skill
func handleCreateSkill(w http.ResponseWriter, r *http.Request) {
	var skill config.Skill
	if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid JSON: " + err.Error(),
		})
		return
	}

	// Validate required fields
	if skill.Name == "" {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "name is required",
		})
		return
	}
	if skill.Description == "" {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "description is required",
		})
		return
	}
	if skill.Instructions == "" {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "instructions is required",
		})
		return
	}

	// Get current config
	globalCfg := config.GetConfig()
	cfg := globalCfg.Get()

	// Initialize skills config if needed
	if cfg.Skills == nil {
		cfg.Skills = &config.SkillsConfig{
			Enabled:     true,
			Definitions: []config.Skill{},
		}
	}

	// Check for duplicate name
	for _, existing := range cfg.Skills.Definitions {
		if existing.Name == skill.Name {
			sendJSON(w, http.StatusConflict, map[string]interface{}{
				"success": false,
				"error":   "skill with name '" + skill.Name + "' already exists",
			})
			return
		}
	}

	// Default enabled to true for new skills
	skill.Enabled = true

	// Add skill
	cfg.Skills.Definitions = append(cfg.Skills.Definitions, skill)

	// Save config
	if err := globalCfg.Update(cfg); err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to save config: " + err.Error(),
		})
		return
	}

	// Reinitialize skills manager
	skills.GetManager().Initialize(cfg.Skills)

	sendJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"skill":   skill,
	})
}

// handleUpdateSkill updates an existing skill
func handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	var skill config.Skill
	if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid JSON: " + err.Error(),
		})
		return
	}

	if skill.Name == "" {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "name is required",
		})
		return
	}

	// Get current config
	globalCfg := config.GetConfig()
	cfg := globalCfg.Get()

	if cfg.Skills == nil {
		sendJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false,
			"error":   "no skills configured",
		})
		return
	}

	// Find and update skill
	found := false
	for i, existing := range cfg.Skills.Definitions {
		if existing.Name == skill.Name {
			cfg.Skills.Definitions[i] = skill
			found = true
			break
		}
	}

	if !found {
		sendJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false,
			"error":   "skill not found: " + skill.Name,
		})
		return
	}

	// Save config
	if err := globalCfg.Update(cfg); err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to save config: " + err.Error(),
		})
		return
	}

	// Reinitialize skills manager
	skills.GetManager().Initialize(cfg.Skills)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"skill":   skill,
	})
}

// handleDeleteSkill deletes a skill
func handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "name query parameter is required",
		})
		return
	}

	// Get current config
	globalCfg := config.GetConfig()
	cfg := globalCfg.Get()

	if cfg.Skills == nil || len(cfg.Skills.Definitions) == 0 {
		sendJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false,
			"error":   "no skills configured",
		})
		return
	}

	// Find and remove skill
	found := false
	newDefinitions := make([]config.Skill, 0, len(cfg.Skills.Definitions)-1)
	for _, skill := range cfg.Skills.Definitions {
		if skill.Name == name {
			found = true
			continue
		}
		newDefinitions = append(newDefinitions, skill)
	}

	if !found {
		sendJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false,
			"error":   "skill not found: " + name,
		})
		return
	}

	cfg.Skills.Definitions = newDefinitions

	// Save config
	if err := globalCfg.Update(cfg); err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to save config: " + err.Error(),
		})
		return
	}

	// Reinitialize skills manager
	skills.GetManager().Initialize(cfg.Skills)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "skill deleted: " + name,
	})
}

// HandleSkillsMatch - POST /skills/match
// Tests trigger matching against input text
func HandleSkillsMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid JSON: " + err.Error(),
		})
		return
	}

	manager := skills.GetManager()
	matched := manager.MatchSkills(req.Input)

	// Extract names for response
	var matchedNames []string
	for _, skill := range matched {
		matchedNames = append(matchedNames, skill.Name)
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"input":   req.Input,
		"matched": matchedNames,
		"skills":  matched,
	})
}

// HandleSkillsEnable - POST /skills/status
// Enables or disables the skills system
func HandleSkillsEnable(w http.ResponseWriter, r *http.Request) {
	log.Printf("🎯 Skills: HandleSkillsEnable called")

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ Skills: Failed to decode request: %v", err)
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid JSON: " + err.Error(),
		})
		return
	}

	log.Printf("🎯 Skills: Setting enabled to %v", req.Enabled)

	// Get current config
	globalCfg := config.GetConfig()
	cfg := globalCfg.Get()

	if cfg.Skills == nil {
		cfg.Skills = &config.SkillsConfig{
			Enabled:     req.Enabled,
			Definitions: []config.Skill{},
		}
	} else {
		cfg.Skills.Enabled = req.Enabled
	}

	// Save config
	if err := globalCfg.Update(cfg); err != nil {
		log.Printf("❌ Skills: Failed to save config: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to save config: " + err.Error(),
		})
		return
	}

	log.Printf("✅ Skills: Config saved, enabled=%v", req.Enabled)

	// Reinitialize skills manager
	skills.GetManager().Initialize(cfg.Skills)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"enabled": req.Enabled,
	})
}
