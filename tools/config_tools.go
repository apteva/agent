package tools

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/apteva/agent/config"
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

	// Update skills enabled state
	if skillsEnabled, ok := params["skills_enabled"].(bool); ok {
		if agentConfig.Skills == nil {
			agentConfig.Skills = &config.SkillsConfig{Enabled: skillsEnabled, Definitions: []config.Skill{}}
		} else {
			agentConfig.Skills.Enabled = skillsEnabled
		}
		updated = true
		log.Printf("🎯 Config: Updated skills.enabled: %v", skillsEnabled)
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
		for _, skill := range agentConfig.Skills.Definitions {
			if skill.Enabled {
				skillNames = append(skillNames, skill.Name)
			}
		}
	}
	return map[string]interface{}{
		"success": true,
		"message": "Configuration updated successfully",
		"config": map[string]interface{}{
			"system_prompt":  agentConfig.LLM.SystemPrompt,
			"tools":          agentConfig.LLM.Tools,
			"builtin_tools":  len(agentConfig.LLM.BuiltinTools),
			"mcp_tools":      mcpToolsCount,
			"skills":         skillNames,
			"skills_enabled": agentConfig.Skills != nil && agentConfig.Skills.Enabled,
			"setup_mode":     agentConfig.SetupMode,
		},
	}, nil
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
		var skillNames []string
		for _, skill := range cfg.Skills.Definitions {
			if skill.Enabled {
				skillNames = append(skillNames, skill.Name)
			}
		}
		configSummary["skills"] = skillNames
		configSummary["skills_enabled"] = true
	} else {
		configSummary["skills"] = []string{}
		configSummary["skills_enabled"] = false
	}

	configJSON, _ := json.MarshalIndent(configSummary, "", "  ")

	return fmt.Sprintf(`=== SETUP MODE ===
You are a setup wizard. Help the user configure you through conversation.

CURRENT CONFIG:
%s

RULES:
- Ask one question at a time. Keep it conversational.
- ACTIVELY USE YOUR TOOLS to explore what's available — list MCP servers, call tools to discover accounts/pages/channels, browse connected services. Show the user what you find and let them pick.
- Based on answers, use config_set to update system_prompt, tools, and skills.
- When done, call config_set({"setup_mode": false}) to finish.

=== END SETUP MODE ===

`, string(configJSON))
}

// RegisterConfigTools registers the config tools with the global registry
func RegisterConfigTools() {
	registry := GetGlobalRegistry()
	registry.RegisterTool(&ConfigSetTool{})
	log.Printf("✅ Config tools registered: config_set")
}
