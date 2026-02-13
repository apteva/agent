package mcp

import (
	"github.com/apteva/agent/config"
	"github.com/apteva/agent/tools"
)

// MCPTool represents an MCP tool in our internal format
type MCPTool struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"display_name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	ServerName  string                 `json:"server_name"`
}

// GetEnabledMCPTools returns MCP tools from external servers
func GetEnabledMCPTools(mcpConfig *config.MCPConfig) []MCPTool {
	var enabledTools []MCPTool

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
		return ConvertMCPToolsToToolDefinitions(enabledTools)
	}
}

// ConvertMCPToolsToAnthropicFormat converts MCP tools to Anthropic's tool format
func ConvertMCPToolsToAnthropicFormat(mcpTools []MCPTool) []map[string]interface{} {
	var anthropicTools []map[string]interface{}

	for _, mcpTool := range mcpTools {
		anthropicTool := map[string]interface{}{
			"name":         mcpTool.Name,
			"description":  mcpTool.Description,
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
	if IsExternalTool(toolName) {
		return GetExternalServerManager().GetTool(toolName) != nil
	}
	return false
}

// GetMCPToolByName retrieves a specific MCP tool by name
func GetMCPToolByName(toolName string) *MCPTool {
	if IsExternalTool(toolName) {
		externalTool := GetExternalServerManager().GetTool(toolName)
		if externalTool != nil {
			return &MCPTool{
				Name:        externalTool.FullName,
				DisplayName: externalTool.Name,
				Description: externalTool.Description,
				InputSchema: externalTool.InputSchema,
				ServerName:  externalTool.ServerName,
			}
		}
	}
	return nil
}
