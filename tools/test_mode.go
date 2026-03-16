package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/apteva/agent/config"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

// TestModeContextKey is used to carry per-request test mode (e.g. from A2A callers).
const TestModeContextKey contextKey = "test_mode"

// agentCallTools are the ONLY tools that execute for real in test mode.
// These enable multi-agent orchestration testing.
var agentCallTools = map[string]bool{
	"call_agent":           true,
	"delegate_task":        true,
	"check_delegated_task": true,
}

// ShouldInterceptInTestMode returns true if this tool should be mocked in test mode.
// Only agent call tools are exempt.
func ShouldInterceptInTestMode(toolName string) bool {
	return !agentCallTools[toolName]
}

// IsTestModeActive checks whether test mode is active, either via per-request
// context (from A2A caller header) or global config.
func IsTestModeActive(ctx context.Context) bool {
	if val, ok := ctx.Value(TestModeContextKey).(bool); ok && val {
		return true
	}
	if cfg := config.GetConfig(); cfg != nil {
		return cfg.Get().TestMode
	}
	return false
}

// SimulateToolResult returns a plausible mock result for a tool call in test mode.
func SimulateToolResult(toolName string, input map[string]interface{}) (interface{}, error) {
	base := map[string]interface{}{
		"test_mode": true,
		"success":   true,
	}

	switch toolName {
	case "create_task":
		title, _ := input["title"].(string)
		base["task_id"] = fmt.Sprintf("test_task_%d", time.Now().UnixMilli()%100000)
		base["message"] = fmt.Sprintf("[TEST] Task '%s' created (simulated)", title)

	case "list_tasks":
		base["tasks"] = []interface{}{}
		base["message"] = "[TEST] No tasks (simulated)"

	case "get_task":
		taskID, _ := input["task_id"].(string)
		base["task"] = map[string]interface{}{
			"id":     taskID,
			"title":  "Simulated task",
			"status": "pending",
		}
		base["message"] = "[TEST] Task retrieved (simulated)"

	case "execute_task":
		base["status"] = "completed"
		base["message"] = "[TEST] Task executed (simulated)"

	case "update_task":
		base["message"] = "[TEST] Task updated (simulated)"

	case "delete_task":
		base["message"] = "[TEST] Task deleted (simulated)"

	case "send_notification":
		base["status"] = "sent"
		base["notification_id"] = fmt.Sprintf("test_notif_%d", time.Now().UnixMilli()%100000)
		base["message"] = "[TEST] Notification sent (simulated)"

	case "subscribe":
		base["message"] = "[TEST] Subscription created (simulated)"

	case "unsubscribe":
		base["message"] = "[TEST] Subscription removed (simulated)"

	case "update_subscription":
		base["message"] = "[TEST] Subscription updated (simulated)"

	case "list_subscriptions":
		base["subscriptions"] = []interface{}{}
		base["message"] = "[TEST] No subscriptions (simulated)"

	case "list_files":
		base["files"] = []interface{}{}
		base["message"] = "[TEST] No files (simulated)"

	case "get_file":
		base["content"] = "[TEST] Simulated file content"
		base["message"] = "[TEST] File retrieved (simulated)"

	case "search_files":
		base["results"] = []interface{}{}
		base["message"] = "[TEST] No results (simulated)"

	case "delete_file":
		base["message"] = "[TEST] File deleted (simulated)"

	case "config_set":
		base["message"] = "[TEST] Config updated (simulated)"

	case "create_operator_session":
		base["session_id"] = fmt.Sprintf("test_session_%d", time.Now().UnixMilli()%100000)
		base["message"] = "[TEST] Operator session created (simulated)"

	case "connect_operator_session":
		base["session_id"] = input["session_id"]
		base["status"] = "connected"
		base["message"] = "[TEST] Connected to existing session (simulated)"

	case "close_operator_session":
		base["message"] = "[TEST] Operator session closed (simulated)"

	case "list_operator_sessions":
		base["sessions"] = []interface{}{}
		base["message"] = "[TEST] No sessions (simulated)"

	case "get_time":
		base["time"] = time.Now().Format(time.RFC3339)
		base["message"] = "[TEST] Current time (simulated)"

	case "ping":
		base["message"] = "[TEST] Pong (simulated)"

	case "wait":
		base["message"] = "[TEST] Wait completed (simulated)"

	case "high_quality_screenshot":
		base["message"] = "[TEST] Screenshot captured (simulated)"

	case "document_search":
		base["results"] = []interface{}{}
		base["message"] = "[TEST] No documents found (simulated)"

	default:
		base["tool"] = toolName
		base["message"] = fmt.Sprintf("[TEST] Tool '%s' executed (simulated)", toolName)
	}

	return base, nil
}

// SimulateMCPToolResult returns a mock result for an MCP tool call in test mode.
func SimulateMCPToolResult(toolName string, input map[string]interface{}) string {
	return fmt.Sprintf(`{"test_mode":true,"success":true,"tool":"%s","message":"[TEST] MCP tool '%s' executed (simulated)"}`, toolName, toolName)
}
