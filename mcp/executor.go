package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

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

// ExecuteToolStreamingWithContext executes an MCP tool with notification streaming support.
// For external MCP tools, notifications are streamed via the callback.
// For gateway/internal tools, falls through to the standard sync path.
func ExecuteToolStreamingWithContext(ctx context.Context, toolName string, params map[string]interface{}, cfg *config.MCPConfig, sessionID string, callback MCPNotificationCallback) (interface{}, error) {
	executor := GetMCPExecutor(cfg)
	if executor == nil {
		return nil, fmt.Errorf("MCP executor not available")
	}

	// External MCP tools support streaming notifications
	if IsExternalTool(toolName) && callback != nil {
		return executor.executeExternalToolStreaming(toolName, params, callback)
	}

	// Non-external tools (gateway) use the standard path
	return executor.ExecuteMCPToolWithContext(ctx, toolName, params, cfg, sessionID)
}

// processExternalToolResult converts a standard MCP ToolCallResult into our internal format.
// Shared by executeExternalTool and executeExternalToolStreaming.
func processExternalToolResult(toolName string, result *ToolCallResult) (interface{}, error) {
	// Log what we got back
	log.Printf("📋 MCP External: tool=%s, isError=%v, content_blocks=%d",
		toolName, result.IsError, len(result.Content))
	for i, block := range result.Content {
		dataLen := 0
		if block.Data != "" {
			dataLen = len(block.Data)
		}
		textPreview := ""
		if block.Text != "" {
			textPreview = truncateForLog(block.Text, 200)
		}
		log.Printf("  📦 Block[%d]: type=%q, mimeType=%q, text_len=%d, data_len=%d, text_preview=%q",
			i, block.Type, block.MimeType, len(block.Text), dataLen, textPreview)
	}

	// Convert standard MCP result to our format
	if result.IsError {
		for _, block := range result.Content {
			if block.Type == "text" && block.Text != "" {
				return nil, fmt.Errorf("%s", block.Text)
			}
		}
		return nil, fmt.Errorf("tool execution failed")
	}

	// Extract content from response — handle both text and image blocks
	var textContent []string
	var parsedContent []interface{}
	var allContent []interface{}
	hasNonText := false

	for _, block := range result.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textContent = append(textContent, block.Text)
				allContent = append(allContent, map[string]interface{}{
					"type": "text",
					"text": block.Text,
				})

				if len(block.Text) > 2 && (block.Text[0] == '{' || block.Text[0] == '[') {
					var parsed interface{}
					if err := json.Unmarshal([]byte(block.Text), &parsed); err == nil {
						parsedContent = append(parsedContent, parsed)
						log.Printf("  📋 Text block[%d] parsed as JSON structure (%d bytes)", len(parsedContent)-1, len(block.Text))
					}
				}
			}
		case "image":
			hasNonText = true
			imgBlock := map[string]interface{}{
				"type": "image",
				"source": map[string]interface{}{
					"type":       "base64",
					"media_type": block.MimeType,
					"data":       block.Data,
				},
			}
			allContent = append(allContent, imgBlock)
			log.Printf("  🖼️  Image block preserved: mimeType=%s, data_len=%d", block.MimeType, len(block.Data))
		default:
			hasNonText = true
			allContent = append(allContent, map[string]interface{}{
				"type":     block.Type,
				"text":     block.Text,
				"mimeType": block.MimeType,
				"data":     block.Data,
			})
			log.Printf("  ❓ Unknown block type=%q preserved as-is", block.Type)
		}
	}

	if hasNonText {
		return map[string]interface{}{
			"content": allContent,
		}, nil
	}

	if len(parsedContent) == 1 && len(textContent) == 1 {
		return parsedContent[0], nil
	}

	if len(textContent) == 1 {
		return textContent[0], nil
	}
	if len(textContent) > 1 {
		return map[string]interface{}{
			"content": allContent,
		}, nil
	}

	return map[string]interface{}{
		"content": result.Content,
	}, nil
}

// executeExternalTool executes a tool on an external standard MCP server
func (e *MCPToolExecutor) executeExternalTool(toolName string, params map[string]interface{}) (interface{}, error) {
	manager := GetExternalServerManager()

	log.Printf("🔧 MCP External Execute: tool=%s, params_keys=%v", toolName, mapKeys(params))
	startTime := time.Now()

	result, err := manager.CallTool(toolName, params)
	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("❌ MCP External Execute: tool=%s FAILED after %v: %v", toolName, elapsed, err)
		return nil, err
	}

	log.Printf("✅ MCP External Execute: tool=%s completed in %v", toolName, elapsed)
	return processExternalToolResult(toolName, result)
}

// executeExternalToolStreaming executes a tool on an external MCP server with notification streaming.
func (e *MCPToolExecutor) executeExternalToolStreaming(toolName string, params map[string]interface{}, callback MCPNotificationCallback) (interface{}, error) {
	manager := GetExternalServerManager()

	log.Printf("🔧 MCP External Execute (streaming): tool=%s, params_keys=%v", toolName, mapKeys(params))
	startTime := time.Now()

	result, err := manager.CallToolStreaming(toolName, params, callback)
	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("❌ MCP External Execute (streaming): tool=%s FAILED after %v: %v", toolName, elapsed, err)
		return nil, err
	}

	log.Printf("✅ MCP External Execute (streaming): tool=%s completed in %v", toolName, elapsed)
	return processExternalToolResult(toolName, result)
}

// mapKeys returns the keys of a map for logging
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// truncateForLog truncates a string for log output
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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