package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/apteva/agent/config"
)

// MCPToolExecutor handles execution of MCP tools via external standard MCP servers
type MCPToolExecutor struct{}

// NewMCPToolExecutor creates a new MCP tool executor
func NewMCPToolExecutor() *MCPToolExecutor {
	return &MCPToolExecutor{}
}

// Global MCP tool executor instance
var globalMCPExecutor *MCPToolExecutor

// GetMCPExecutor returns the global MCP tool executor
func GetMCPExecutor(cfg *config.MCPConfig) *MCPToolExecutor {
	if globalMCPExecutor == nil && cfg != nil && cfg.Enabled {
		globalMCPExecutor = NewMCPToolExecutor()
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
	return executor.executeExternalToolWithContext(ctx, toolName, params)
}

// ExecuteToolStreamingWithContext executes an MCP tool with notification streaming support.
func ExecuteToolStreamingWithContext(ctx context.Context, toolName string, params map[string]interface{}, cfg *config.MCPConfig, sessionID string, callback MCPNotificationCallback) (interface{}, error) {
	executor := GetMCPExecutor(cfg)
	if executor == nil {
		return nil, fmt.Errorf("MCP executor not available")
	}

	if callback != nil {
		return executor.executeExternalToolStreamingWithContext(ctx, toolName, params, callback)
	}

	return executor.executeExternalToolWithContext(ctx, toolName, params)
}

// processExternalToolResult converts a standard MCP ToolCallResult into our internal format.
func processExternalToolResult(toolName string, result *ToolCallResult) (interface{}, error) {
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

	if result.IsError {
		for _, block := range result.Content {
			if block.Type == "text" && block.Text != "" {
				return nil, fmt.Errorf("%s", block.Text)
			}
		}
		return nil, fmt.Errorf("tool execution failed")
	}

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
		default:
			hasNonText = true
			allContent = append(allContent, map[string]interface{}{
				"type":     block.Type,
				"text":     block.Text,
				"mimeType": block.MimeType,
				"data":     block.Data,
			})
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

// executeExternalTool executes a tool on an external standard MCP server (no context)
func (e *MCPToolExecutor) executeExternalTool(toolName string, params map[string]interface{}) (interface{}, error) {
	return e.executeExternalToolWithContext(context.Background(), toolName, params)
}

// executeExternalToolWithContext executes a tool with context for cancellation support
func (e *MCPToolExecutor) executeExternalToolWithContext(ctx context.Context, toolName string, params map[string]interface{}) (interface{}, error) {
	manager := GetExternalServerManager()

	log.Printf("🔧 MCP Execute: tool=%s, params_keys=%v", toolName, mapKeys(params))
	startTime := time.Now()

	result, err := manager.CallToolWithContext(ctx, toolName, params)
	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("❌ MCP Execute: tool=%s FAILED after %v: %v", toolName, elapsed, err)
		return nil, err
	}

	log.Printf("✅ MCP Execute: tool=%s completed in %v", toolName, elapsed)
	return processExternalToolResult(toolName, result)
}

// executeExternalToolStreaming executes a tool with notification streaming (no context)
func (e *MCPToolExecutor) executeExternalToolStreaming(toolName string, params map[string]interface{}, callback MCPNotificationCallback) (interface{}, error) {
	return e.executeExternalToolStreamingWithContext(context.Background(), toolName, params, callback)
}

// executeExternalToolStreamingWithContext executes a tool with streaming and context
func (e *MCPToolExecutor) executeExternalToolStreamingWithContext(ctx context.Context, toolName string, params map[string]interface{}, callback MCPNotificationCallback) (interface{}, error) {
	manager := GetExternalServerManager()

	log.Printf("🔧 MCP Execute (streaming): tool=%s, params_keys=%v", toolName, mapKeys(params))
	startTime := time.Now()

	result, err := manager.CallToolStreamingWithContext(ctx, toolName, params, callback)
	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("❌ MCP Execute (streaming): tool=%s FAILED after %v: %v", toolName, elapsed, err)
		return nil, err
	}

	log.Printf("✅ MCP Execute (streaming): tool=%s completed in %v", toolName, elapsed)
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
func extractErrorMessage(result map[string]interface{}) string {
	if errorMsg, ok := result["error"].(string); ok && errorMsg != "" {
		return errorMsg
	}

	if content, ok := result["content"].([]interface{}); ok {
		for _, block := range content {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if blockType, _ := blockMap["type"].(string); blockType == "text" {
					if text, ok := blockMap["text"].(string); ok {
						if strings.HasPrefix(text, "Error:") {
							return strings.TrimSpace(strings.TrimPrefix(text, "Error:"))
						}
						if isError, _ := result["isError"].(bool); isError {
							return text
						}
					}
				}
			}
		}
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		if errorMsg, ok := data["error"].(string); ok && errorMsg != "" {
			return errorMsg
		}
	}

	return ""
}
