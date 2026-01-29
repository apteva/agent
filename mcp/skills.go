package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"agent-server/config"
)

// Skill represents a skill fetched from MCP server
type Skill struct {
	Name         string                 `json:"name"`
	DisplayName  string                 `json:"display_name"`
	Description  string                 `json:"description"`
	Version      string                 `json:"version"`
	Instructions string                 `json:"instructions"`
	Tools        []string               `json:"tools"`
	ClaudeNative map[string]interface{} `json:"claude_native,omitempty"`
	Category     string                 `json:"category"`
	Tags         []string               `json:"tags"`
	Icon         string                 `json:"icon"`
	LoadedAt     time.Time              `json:"loaded_at"`
}

// SkillsResponse represents the API response from skill-list
type SkillsResponse struct {
	Success bool    `json:"success"`
	Skills  []Skill `json:"skills"`
	Count   int     `json:"count"`
}

// SkillsCache caches skills in memory
type SkillsCache struct {
	Skills      map[string]Skill // key: skill name
	LastRefresh time.Time
	mu          sync.RWMutex
}

// Global skills cache
var globalSkillsCache *SkillsCache
var skillsCacheMutex sync.Once

// GetSkillsCache returns the global skills cache instance
func GetSkillsCache() *SkillsCache {
	skillsCacheMutex.Do(func() {
		globalSkillsCache = &SkillsCache{
			Skills: make(map[string]Skill),
		}
	})
	return globalSkillsCache
}

// FetchSkills fetches skills by names from MCP server
func (c *MCPClient) FetchSkills(names []string) ([]Skill, error) {
	if len(names) == 0 {
		return []Skill{}, nil
	}

	// Build URL with names parameter
	url := fmt.Sprintf("%s/skills?names=%s", c.config.BaseURL, strings.Join(names, ","))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	var lastErr error

	// Retry logic
	for i := 0; i <= c.config.RetryCount; i++ {
		resp, lastErr = c.httpClient.Do(req)
		if lastErr == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if i < c.config.RetryCount {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to fetch skills after %d retries: %w", c.config.RetryCount, lastErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var skillsResp SkillsResponse
	if err := json.NewDecoder(resp.Body).Decode(&skillsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !skillsResp.Success {
		return nil, fmt.Errorf("server returned success=false")
	}

	// Set loaded timestamp
	now := time.Now()
	for i := range skillsResp.Skills {
		skillsResp.Skills[i].LoadedAt = now
	}

	return skillsResp.Skills, nil
}

// GetSkills returns cached skills by names
func (cache *SkillsCache) GetSkills(names []string) []Skill {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	if len(names) == 0 {
		// Return all cached skills
		skills := make([]Skill, 0, len(cache.Skills))
		for _, skill := range cache.Skills {
			skills = append(skills, skill)
		}
		return skills
	}

	// Return skills matching names
	skills := make([]Skill, 0, len(names))
	for _, name := range names {
		if skill, exists := cache.Skills[name]; exists {
			skills = append(skills, skill)
		}
	}
	return skills
}

// UpdateSkills updates the skills cache
func (cache *SkillsCache) UpdateSkills(skills []Skill) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	for _, skill := range skills {
		cache.Skills[skill.Name] = skill
	}
	cache.LastRefresh = time.Now()
}

// LoadSkills loads skills from MCP server and updates cache
func LoadSkills(cfg *config.MCPConfig, skillsCfg *config.SkillsConfig) ([]Skill, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("MCP not enabled")
	}

	if skillsCfg == nil || !skillsCfg.Enabled || len(skillsCfg.Names) == 0 {
		return []Skill{}, nil
	}

	client := NewMCPClient(cfg)
	cache := GetSkillsCache()

	skills, err := client.FetchSkills(skillsCfg.Names)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch skills: %w", err)
	}

	cache.UpdateSkills(skills)
	log.Printf("🎯 Skills: Loaded %d skills: %v", len(skills), skillsCfg.Names)

	return skills, nil
}

// GetEnabledSkills returns the currently enabled skills from cache
func GetEnabledSkills(skillsCfg *config.SkillsConfig) []Skill {
	if skillsCfg == nil || !skillsCfg.Enabled || len(skillsCfg.Names) == 0 {
		return []Skill{}
	}

	cache := GetSkillsCache()
	return cache.GetSkills(skillsCfg.Names)
}

// GetSkillTools returns all MCP tools referenced by enabled skills
func GetSkillTools(skills []Skill) []string {
	toolSet := make(map[string]bool)

	for _, skill := range skills {
		for _, tool := range skill.Tools {
			toolSet[tool] = true
		}
	}

	tools := make([]string, 0, len(toolSet))
	for tool := range toolSet {
		tools = append(tools, tool)
	}
	return tools
}

// BuildSkillsPromptSection builds the system prompt section for skills (for non-Claude providers)
func BuildSkillsPromptSection(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n## Active Skills\n")

	for _, skill := range skills {
		sb.WriteString(fmt.Sprintf("\n### %s", skill.DisplayName))
		if skill.Icon != "" {
			sb.WriteString(fmt.Sprintf(" %s", skill.Icon))
		}
		sb.WriteString("\n")
		sb.WriteString(skill.Instructions)
		sb.WriteString("\n")
	}

	return sb.String()
}

// BuildClaudeNativeSkills converts skills to Claude native skills format
func BuildClaudeNativeSkills(skills []Skill) []map[string]interface{} {
	nativeSkills := make([]map[string]interface{}, 0, len(skills))

	for _, skill := range skills {
		// Use claude_native if provided, otherwise build from skill data
		if skill.ClaudeNative != nil && len(skill.ClaudeNative) > 0 {
			nativeSkills = append(nativeSkills, skill.ClaudeNative)
		} else {
			// Build native skill format from skill data
			nativeSkill := map[string]interface{}{
				"name":         skill.Name,
				"description":  skill.Description,
				"instructions": skill.Instructions,
			}
			nativeSkills = append(nativeSkills, nativeSkill)
		}
	}

	return nativeSkills
}
