package tools

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"agent-server/config"
)

// ConfigSetTool allows the agent to modify its own configuration
type ConfigSetTool struct{}

func (t *ConfigSetTool) Name() string {
	return "config_set"
}

func (t *ConfigSetTool) DisplayName() string {
	return "Set Configuration"
}

func (t *ConfigSetTool) Description() string {
	return `Update agent configuration. Provide any fields to update. Available fields:
- system_prompt: The agent's system prompt/instructions
- tools: Array of tool names to enable (e.g., ["file_read", "web_search"])
- builtin_tools: Array of builtin tool configs
- mcp_tools: Array of MCP tool names to enable
- skills: Array of skill names to enable (e.g., ["slack-workflows", "code-review"])
- setup_mode: Boolean to enable/disable setup mode (set to false when done configuring)

Example: config_set({"system_prompt": "You are a helpful code assistant.", "skills": ["slack-workflows"]})`
}

func (t *ConfigSetTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"system_prompt": map[string]interface{}{
				"type":        "string",
				"description": "The agent's system prompt/instructions",
			},
			"tools": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Array of tool names to enable",
			},
			"builtin_tools": map[string]interface{}{
				"type":        "array",
				"description": "Array of builtin tool configurations",
			},
			"mcp_tools": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Array of MCP tool names to enable",
			},
			"skills": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Array of skill names to enable (e.g., [\"slack-workflows\", \"code-review\"])",
			},
			"setup_mode": map[string]interface{}{
				"type":        "boolean",
				"description": "Enable/disable setup mode (set to false when done configuring)",
			},
		},
	}
}

func (t *ConfigSetTool) Execute(params map[string]interface{}) (interface{}, error) {
	cfg := config.GetConfig()
	agentConfig := cfg.Get()
	updated := false

	// Update system prompt
	if prompt, ok := params["system_prompt"].(string); ok {
		agentConfig.LLM.SystemPrompt = prompt
		updated = true
		log.Printf("🔧 Config: Updated system_prompt (%d chars)", len(prompt))
	}

	// Update tools
	if toolsRaw, ok := params["tools"].([]interface{}); ok {
		var tools []string
		for _, t := range toolsRaw {
			if toolName, ok := t.(string); ok {
				tools = append(tools, toolName)
			}
		}
		agentConfig.LLM.Tools = tools
		updated = true
		log.Printf("🔧 Config: Updated tools: %v", tools)
	}

	// Update builtin tools
	if builtinRaw, ok := params["builtin_tools"].([]interface{}); ok {
		var builtinTools []config.BuiltinToolConfig
		for _, bt := range builtinRaw {
			if btMap, ok := bt.(map[string]interface{}); ok {
				btConfig := config.BuiltinToolConfig{}
				if name, ok := btMap["name"].(string); ok {
					btConfig.Name = name
				}
				if typ, ok := btMap["type"].(string); ok {
					btConfig.Type = typ
				}
				builtinTools = append(builtinTools, btConfig)
			}
		}
		agentConfig.LLM.BuiltinTools = builtinTools
		updated = true
		log.Printf("🔧 Config: Updated builtin_tools: %d tools", len(builtinTools))
	}

	// Update MCP tools
	if mcpToolsRaw, ok := params["mcp_tools"].([]interface{}); ok {
		var mcpTools []string
		for _, t := range mcpToolsRaw {
			if toolName, ok := t.(string); ok {
				mcpTools = append(mcpTools, toolName)
			}
		}
		if agentConfig.MCP != nil {
			agentConfig.MCP.Tools = mcpTools
			updated = true
			log.Printf("🔧 Config: Updated mcp_tools: %v", mcpTools)
		}
	}

	// Update skills
	if skillsRaw, ok := params["skills"].([]interface{}); ok {
		var skillNames []string
		for _, s := range skillsRaw {
			if skillName, ok := s.(string); ok {
				skillNames = append(skillNames, skillName)
			}
		}
		if agentConfig.Skills == nil {
			agentConfig.Skills = &config.SkillsConfig{Enabled: true}
		}
		agentConfig.Skills.Names = skillNames
		agentConfig.Skills.Enabled = len(skillNames) > 0
		updated = true
		log.Printf("🎯 Config: Updated skills: %v", skillNames)
	}

	// Update setup mode
	if setupMode, ok := params["setup_mode"].(bool); ok {
		agentConfig.SetupMode = setupMode
		updated = true
		log.Printf("🔧 Config: Updated setup_mode: %v", setupMode)
	}

	if !updated {
		return map[string]interface{}{
			"success": false,
			"error":   "No valid configuration fields provided",
		}, nil
	}

	// Save the updated config
	cfg.Set(agentConfig)
	if err := cfg.Save(); err != nil {
		log.Printf("❌ Config: Failed to save: %v", err)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to save config: %v", err),
		}, nil
	}

	log.Printf("✅ Config: Saved successfully")

	// Return the updated config summary
	mcpToolsCount := 0
	if agentConfig.MCP != nil {
		mcpToolsCount = len(agentConfig.MCP.Tools)
	}
	var skillNames []string
	if agentConfig.Skills != nil && agentConfig.Skills.Enabled {
		skillNames = agentConfig.Skills.Names
	}
	return map[string]interface{}{
		"success": true,
		"message": "Configuration updated successfully",
		"config": map[string]interface{}{
			"system_prompt": agentConfig.LLM.SystemPrompt,
			"tools":         agentConfig.LLM.Tools,
			"builtin_tools": len(agentConfig.LLM.BuiltinTools),
			"mcp_tools":     mcpToolsCount,
			"skills":        skillNames,
			"setup_mode":    agentConfig.SetupMode,
		},
	}, nil
}

// ToolsSearchTool allows the agent to discover available tools and skills
type ToolsSearchTool struct {
	MCPToolsProvider func() []ToolInfo   // Function to get MCP tools
	SkillsProvider   func() []SkillInfo  // Function to get available skills
}

// ToolInfo represents a tool's metadata for search results
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      string `json:"source"` // "builtin", "mcp", "registered"
}

// SkillInfo represents a skill's metadata for search results
type SkillInfo struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tools       []string `json:"tools"` // MCP tools this skill references
	Icon        string   `json:"icon,omitempty"`
}

func (t *ToolsSearchTool) Name() string {
	return "tools_search"
}

func (t *ToolsSearchTool) DisplayName() string {
	return "Search Tools"
}

func (t *ToolsSearchTool) Description() string {
	return `Search available tools and skills. Use this to discover what tools and skills can be enabled.
- query: Search term to filter by name or description
- category: Filter by category ("builtin", "mcp", "skill", "file", "web", "data", etc.)

Example: tools_search({"query": "slack"}) or tools_search({"category": "skill"})`
}

func (t *ToolsSearchTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search term to filter tools and skills",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"description": "Filter by category: builtin, mcp, skill, file, web, data",
			},
		},
	}
}

func (t *ToolsSearchTool) Execute(params map[string]interface{}) (interface{}, error) {
	query := ""
	if q, ok := params["query"].(string); ok {
		query = strings.ToLower(q)
	}
	category := ""
	if c, ok := params["category"].(string); ok {
		category = strings.ToLower(c)
	}

	var results []ToolInfo

	// Get registered tools from global registry
	registry := GetGlobalRegistry()
	for _, toolDef := range registry.ListTools() {
		info := ToolInfo{
			Name:        toolDef.Name,
			Description: toolDef.Description,
			Category:    categorizeBuiltinTool(toolDef.Name),
			Source:      "builtin",
		}

		if matchesSearch(info, query, category) {
			results = append(results, info)
		}
	}

	// Get MCP tools if provider is set
	if t.MCPToolsProvider != nil {
		mcpTools := t.MCPToolsProvider()
		for _, tool := range mcpTools {
			if matchesSearch(tool, query, category) {
				results = append(results, tool)
			}
		}
	}

	// Add common builtin tools that might not be registered
	commonBuiltins := []ToolInfo{
		{Name: "web_search", Description: "Search the web for information", Category: "web", Source: "builtin"},
		{Name: "web_fetch", Description: "Fetch content from a URL", Category: "web", Source: "builtin"},
		{Name: "file_read", Description: "Read file contents", Category: "file", Source: "builtin"},
		{Name: "file_write", Description: "Write content to a file", Category: "file", Source: "builtin"},
		{Name: "file_list", Description: "List directory contents", Category: "file", Source: "builtin"},
		{Name: "grep", Description: "Search file contents with pattern matching", Category: "file", Source: "builtin"},
		{Name: "sql_query", Description: "Execute SQL queries on databases", Category: "data", Source: "builtin"},
	}

	for _, tool := range commonBuiltins {
		// Check if already in results
		found := false
		for _, r := range results {
			if r.Name == tool.Name {
				found = true
				break
			}
		}
		if !found && matchesSearch(tool, query, category) {
			results = append(results, tool)
		}
	}

	// Get skills if provider is set and category allows
	var skillResults []SkillInfo
	if t.SkillsProvider != nil && (category == "" || category == "skill") {
		skills := t.SkillsProvider()
		for _, skill := range skills {
			if matchesSkillSearch(skill, query) {
				skillResults = append(skillResults, skill)
			}
		}
	}

	return map[string]interface{}{
		"tools":       results,
		"skills":      skillResults,
		"tools_count": len(results),
		"skills_count": len(skillResults),
		"query":       query,
	}, nil
}

func matchesSkillSearch(skill SkillInfo, query string) bool {
	if query == "" {
		return true
	}
	nameLower := strings.ToLower(skill.Name)
	displayLower := strings.ToLower(skill.DisplayName)
	descLower := strings.ToLower(skill.Description)
	return strings.Contains(nameLower, query) ||
		strings.Contains(displayLower, query) ||
		strings.Contains(descLower, query)
}

func matchesSearch(tool ToolInfo, query, category string) bool {
	// Filter by category first
	if category != "" {
		if category == "mcp" && tool.Source != "mcp" {
			return false
		}
		if category == "builtin" && tool.Source != "builtin" {
			return false
		}
		if category != "mcp" && category != "builtin" {
			if !strings.Contains(strings.ToLower(tool.Category), category) {
				return false
			}
		}
	}

	// Then filter by query
	if query != "" {
		nameLower := strings.ToLower(tool.Name)
		descLower := strings.ToLower(tool.Description)
		if !strings.Contains(nameLower, query) && !strings.Contains(descLower, query) {
			return false
		}
	}

	return true
}

func categorizeBuiltinTool(name string) string {
	name = strings.ToLower(name)
	if strings.Contains(name, "file") || strings.Contains(name, "read") || strings.Contains(name, "write") {
		return "file"
	}
	if strings.Contains(name, "web") || strings.Contains(name, "http") || strings.Contains(name, "fetch") {
		return "web"
	}
	if strings.Contains(name, "sql") || strings.Contains(name, "database") || strings.Contains(name, "query") {
		return "data"
	}
	if strings.Contains(name, "task") {
		return "task"
	}
	if strings.Contains(name, "memory") {
		return "memory"
	}
	if strings.Contains(name, "agent") || strings.Contains(name, "call") {
		return "agent"
	}
	if strings.Contains(name, "browser") || strings.Contains(name, "operator") {
		return "browser"
	}
	return "general"
}

// GetSetupModeSystemPromptPrefix returns the prefix to inject when in setup mode
func GetSetupModeSystemPromptPrefix(cfg *config.AgentConfig) string {
	// Marshal config to JSON for display
	configSummary := map[string]interface{}{
		"name":          cfg.Name,
		"description":   cfg.Description,
		"system_prompt": cfg.LLM.SystemPrompt,
		"model":         cfg.LLM.Model,
		"provider":      cfg.LLM.Provider,
		"tools":         cfg.LLM.Tools,
		"builtin_tools": len(cfg.LLM.BuiltinTools),
	}
	if cfg.MCP != nil {
		configSummary["mcp_tools"] = cfg.MCP.Tools
	}
	if cfg.Skills != nil && cfg.Skills.Enabled {
		configSummary["skills"] = cfg.Skills.Names
	} else {
		configSummary["skills"] = []string{}
	}

	configJSON, _ := json.MarshalIndent(configSummary, "", "  ")

	return fmt.Sprintf(`=== SETUP MODE ===
You are in SETUP MODE. You can configure yourself based on user requests.

YOUR CURRENT CONFIGURATION:
%s

AVAILABLE TOOLS:
- config_set: Update your configuration (system_prompt, tools, builtin_tools, mcp_tools, skills, setup_mode)
- tools_search: Search for available tools and skills to enable

INSTRUCTIONS:
1. Use tools_search to discover available tools and skills
2. Use config_set to update your configuration (e.g., enable skills: {"skills": ["slack-workflows"]})
3. When done configuring, use config_set({"setup_mode": false}) to exit setup mode

=== END SETUP MODE ===

`, string(configJSON))
}

// RegisterConfigTools registers the config tools with the global registry
func RegisterConfigTools(mcpToolsProvider func() []ToolInfo, skillsProvider func() []SkillInfo) {
	registry := GetGlobalRegistry()
	registry.RegisterTool(&ConfigSetTool{})
	registry.RegisterTool(&ToolsSearchTool{
		MCPToolsProvider: mcpToolsProvider,
		SkillsProvider:   skillsProvider,
	})
	log.Printf("✅ Config tools registered: config_set, tools_search")
}
