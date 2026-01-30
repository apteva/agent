package mcp

import (
	"github.com/apteva/agent/config"
	"github.com/apteva/agent/tools"
)

// GetEnabledMCPTools returns MCP tools that are enabled in the configuration
func GetEnabledMCPTools(mcpConfig *config.MCPConfig) []MCPTool {
	var enabledTools []MCPTool

	// Create map for quick lookup of explicitly enabled tools
	enabledToolsMap := make(map[string]bool)
	if mcpConfig != nil {
		for _, toolName := range mcpConfig.Tools {
			enabledToolsMap[toolName] = true
		}
	}

	// Filter tools from our gateway (require explicit enabling)
	if mcpConfig != nil && mcpConfig.Enabled && len(mcpConfig.Tools) > 0 {
		cache := GetMCPCache()
		allTools := cache.GetTools("")
		for _, tool := range allTools {
			if enabledToolsMap[tool.Name] {
				enabledTools = append(enabledTools, tool)
			}
		}
	}

	// External MCP server tools are ALL enabled by default (standard MCP behavior)
	// Users connect to external servers expecting all their tools to be available
	externalManager := GetExternalServerManager()
	externalTools := externalManager.GetTools()
	for _, extTool := range externalTools {
		enabledTools = append(enabledTools, MCPTool{
			Name:        extTool.FullName,
			DisplayName: extTool.Name,
			Description: extTool.Description,
			InputSchema: extTool.InputSchema,
			ServerName:  extTool.ServerName,
		})
	}

	return enabledTools
}

// ConvertMCPToolsToToolDefinitions converts MCP tools to the format expected by providers
func ConvertMCPToolsToToolDefinitions(mcpTools []MCPTool) []tools.ToolDefinition {
	var toolDefs []tools.ToolDefinition

	for _, mcpTool := range mcpTools {
		toolDef := tools.ToolDefinition{
			Name:        mcpTool.Name,
			Description: mcpTool.Description,
			InputSchema: mcpTool.InputSchema,
		}
		toolDefs = append(toolDefs, toolDef)
	}

	return toolDefs
}

// GetMCPToolsForProvider returns MCP tools formatted for the specified provider
func GetMCPToolsForProvider(mcpConfig *config.MCPConfig, provider string) interface{} {
	enabledTools := GetEnabledMCPTools(mcpConfig)

	switch provider {
	case "anthropic":
		return ConvertMCPToolsToAnthropicFormat(enabledTools)
	case "openai":
		return ConvertMCPToolsToOpenAIFormat(enabledTools)
	default:
		// Return generic tool definitions
		return ConvertMCPToolsToToolDefinitions(enabledTools)
	}
}

// ConvertMCPToolsToAnthropicFormat converts MCP tools to Anthropic's tool format
func ConvertMCPToolsToAnthropicFormat(mcpTools []MCPTool) []map[string]interface{} {
	var anthropicTools []map[string]interface{}

	for _, mcpTool := range mcpTools {
		anthropicTool := map[string]interface{}{
			"name":        mcpTool.Name,
			"description": mcpTool.Description,
			"input_schema": mcpTool.InputSchema,
		}
		anthropicTools = append(anthropicTools, anthropicTool)
	}

	return anthropicTools
}

// ConvertMCPToolsToOpenAIFormat converts MCP tools to OpenAI's function format
func ConvertMCPToolsToOpenAIFormat(mcpTools []MCPTool) []map[string]interface{} {
	var openaiTools []map[string]interface{}

	for _, mcpTool := range mcpTools {
		openaiTool := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        mcpTool.Name,
				"description": mcpTool.Description,
				"parameters":  mcpTool.InputSchema,
			},
		}
		openaiTools = append(openaiTools, openaiTool)
	}

	return openaiTools
}

// IsMCPTool checks if a tool name corresponds to an MCP tool
func IsMCPTool(toolName string, mcpConfig *config.MCPConfig) bool {
	// External tools are always valid if they exist (standard MCP behavior)
	if IsExternalTool(toolName) {
		return GetExternalServerManager().GetTool(toolName) != nil
	}

	// Gateway tools require MCP to be enabled and tool to be in enabled list
	if mcpConfig == nil || !mcpConfig.Enabled {
		return false
	}

	enabledTools := GetEnabledMCPTools(mcpConfig)
	for _, tool := range enabledTools {
		if tool.Name == toolName {
			return true
		}
	}

	return false
}

// GetMCPToolByName retrieves a specific MCP tool by name
func GetMCPToolByName(toolName string) *MCPTool {
	// Check if it's an external tool first
	if IsExternalTool(toolName) {
		externalTool := GetExternalServerManager().GetTool(toolName)
		if externalTool != nil {
			// Convert to MCPTool format
			return &MCPTool{
				Name:        externalTool.FullName,
				DisplayName: externalTool.Name,
				Description: externalTool.Description,
				InputSchema: externalTool.InputSchema,
				ServerName:  externalTool.ServerName,
			}
		}
		return nil
	}

	cache := GetMCPCache()
	allTools := cache.GetTools("")

	for _, tool := range allTools {
		if tool.Name == toolName {
			return &tool
		}
	}
	return nil
}

// GetAllMCPTools returns all tools including external MCP server tools
func GetAllMCPTools(mcpConfig *config.MCPConfig) []MCPTool {
	var allTools []MCPTool

	// Get tools from our gateway
	if mcpConfig != nil && mcpConfig.Enabled {
		cache := GetMCPCache()
		allTools = append(allTools, cache.GetTools("")...)
	}

	// Get tools from external servers
	externalManager := GetExternalServerManager()
	externalTools := externalManager.GetTools()
	converted := ConvertExternalToolsToToolDefinitions(externalTools)
	allTools = append(allTools, converted...)

	return allTools
}