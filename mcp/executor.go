package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/apteva/agent/config"
)

// MCPToolExecutor handles execution of MCP tools
type MCPToolExecutor struct {
	client *MCPClient
}

// NewMCPToolExecutor creates a new MCP tool executor
func NewMCPToolExecutor(cfg *config.MCPConfig) *MCPToolExecutor {
	return &MCPToolExecutor{
		client: NewMCPClient(cfg),
	}
}

// ExecuteMCPTool executes an MCP tool with the given parameters
func (e *MCPToolExecutor) ExecuteMCPTool(toolName string, params map[string]interface{}, sessionID string) (interface{}, error) {
	return e.ExecuteMCPToolWithConfig(toolName, params, e.client.config, sessionID)
}

// ExecuteMCPToolWithConfig executes an MCP tool with the given parameters and config
func (e *MCPToolExecutor) ExecuteMCPToolWithConfig(toolName string, params map[string]interface{}, cfg *config.MCPConfig, sessionID string) (interface{}, error) {
	return e.ExecuteMCPToolWithContext(context.Background(), toolName, params, cfg, sessionID)
}

// ExecuteMCPToolWithContext executes an MCP tool with cancellation support
func (e *MCPToolExecutor) ExecuteMCPToolWithContext(ctx context.Context, toolName string, params map[string]interface{}, cfg *config.MCPConfig, sessionID string) (interface{}, error) {
	// Check if this is an external MCP tool (format: "server:tool")
	if IsExternalTool(toolName) {
		return e.executeExternalTool(toolName, params)
	}

	// Get global config to check test mode
	globalCfg := config.GetConfig()
	agentConfig := globalCfg.Get()

	// Determine effective test mode (subsystem override or global)
	testMode := config.IsTestMode(agentConfig.TestMode, cfg.TestMode)

	// Find the tool in cache to verify it exists
	// Skip cache validation in test mode - let MCP server handle it
	tool := GetMCPToolByName(toolName)
	if tool == nil && !testMode {
		return nil, fmt.Errorf("MCP tool '%s' not found in cache", toolName)
	}

	// Create execution request using the MCP standard format
	requestBody := map[string]interface{}{
		"name":       toolName,
		"arguments":  params,
		"session_id": sessionID,  // Always include session_id for tracking
		"test_mode":  testMode,   // Pass effective test_mode (global or subsystem override)
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Execute tool via MCP server using the /tools/call endpoint
	url := fmt.Sprintf("%s/tools/call", e.client.config.BaseURL)

	// Create request with context for cancellation support
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use X-API-Key header format as specified
	if e.client.config.APIKey != "" {
		req.Header.Set("X-API-Key", e.client.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute tool: %w", err)
	}
	defer resp.Body.Close()

	// Parse response body (always read it, even for non-200 status)
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Handle non-200 status codes - extract error from body
	if resp.StatusCode != http.StatusOK {
		errorMsg := extractErrorMessage(result)
		if errorMsg != "" {
			return nil, fmt.Errorf("%s", errorMsg)
		}
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	// Check if execution was successful (for 200 responses with success=false)
	if success, ok := result["success"].(bool); !ok || !success {
		errorMsg := extractErrorMessage(result)
		if errorMsg != "" {
			return nil, fmt.Errorf("%s", errorMsg)
		}
		return nil, fmt.Errorf("tool execution failed")
	}

	// Return the result data
	if data, ok := result["data"]; ok {
		return data, nil
	}

	return result, nil
}

// Global MCP tool executor instance
var globalMCPExecutor *MCPToolExecutor

// GetMCPExecutor returns the global MCP tool executor
func GetMCPExecutor(cfg *config.MCPConfig) *MCPToolExecutor {
	if globalMCPExecutor == nil && cfg != nil && cfg.Enabled {
		globalMCPExecutor = NewMCPToolExecutor(cfg)
	}
	return globalMCPExecutor
}

// ExecuteTool is a convenience function to execute an MCP tool
func ExecuteTool(toolName string, params map[string]interface{}, cfg *config.MCPConfig, sessionID string) (interface{}, error) {
	return ExecuteToolWithContext(context.Background(), toolName, params, cfg, sessionID)
}

// ExecuteToolWithContext executes an MCP tool with cancellation support
func ExecuteToolWithContext(ctx context.Context, toolName string, params map[string]interface{}, cfg *config.MCPConfig, sessionID string) (interface{}, error) {
	executor := GetMCPExecutor(cfg)
	if executor == nil {
		return nil, fmt.Errorf("MCP executor not available")
	}
	return executor.ExecuteMCPToolWithContext(ctx, toolName, params, cfg, sessionID)
}

// executeExternalTool executes a tool on an external standard MCP server
func (e *MCPToolExecutor) executeExternalTool(toolName string, params map[string]interface{}) (interface{}, error) {
	manager := GetExternalServerManager()

	result, err := manager.CallTool(toolName, params)
	if err != nil {
		return nil, err
	}

	// Convert standard MCP result to our format
	if result.IsError {
		// Extract error message from content
		for _, block := range result.Content {
			if block.Type == "text" && block.Text != "" {
				return nil, fmt.Errorf("%s", block.Text)
			}
		}
		return nil, fmt.Errorf("tool execution failed")
	}

	// Extract text content from response
	var textContent []string
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			textContent = append(textContent, block.Text)
		}
	}

	// Return as structured data
	if len(textContent) == 1 {
		return textContent[0], nil
	}
	return map[string]interface{}{
		"content": result.Content,
	}, nil
}

// extractErrorMessage extracts error details from MCP response
// It checks multiple locations: error field, content[].text, data.error
func extractErrorMessage(result map[string]interface{}) string {
	// 1. Try top-level "error" field first
	if errorMsg, ok := result["error"].(string); ok && errorMsg != "" {
		return errorMsg
	}

	// 2. Try content array (MCP standard format)
	if content, ok := result["content"].([]interface{}); ok {
		for _, block := range content {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, _ := blockMap["type"].(string); blockType == "text" {
					if text, ok := blockMap["text"].(string); ok {
						// Check if it's an error message
						if strings.HasPrefix(text, "Error:") {
							return strings.TrimSpace(strings.TrimPrefix(text, "Error:"))
						}
						// If isError is set, use the text as-is
						if isError, _ := result["isError"].(bool); isError {
							return text
						}
					}
				}
			}
		}
	}

	// 3. Try nested data.error
	if data, ok := result["data"].(map[string]interface{}); ok {
		if errorMsg, ok := data["error"].(string); ok && errorMsg != "" {
			return errorMsg
		}
		// Try data.message for some error formats
		if msg, ok := data["message"].(string); ok && msg != "" {
			if success, _ := data["success"].(bool); !success {
				return msg
			}
		}
	}

	// 4. Try metadata for additional context
	if metadata, ok := result["metadata"].(map[string]interface{}); ok {
		if toolName, _ := metadata["tool_name"].(string); toolName != "" {
			if reqID, _ := metadata["request_id"].(string); reqID != "" {
				// Return empty - let caller use generic message, but we could log reqID
				return ""
			}
		}
	}

	return ""
}