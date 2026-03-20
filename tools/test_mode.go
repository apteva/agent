package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apteva/agent/config"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

// TestModeContextKey is used to carry per-request test mode (e.g. from A2A callers).
const TestModeContextKey contextKey = "test_mode"

// realExecuteTools always execute for real in test mode (no mocking).
// Agent calls enable multi-agent orchestration testing.
// Read-only tools are safe — they return real data without side effects.
var realExecuteTools = map[string]bool{
	// Agent calls
	"call_agent":           true,
	"delegate_task":        true,
	"check_delegated_task": true,
	// Read-only built-in tools
	"list_tasks":             true,
	"get_task":               true,
	"list_files":             true,
	"get_file":               true,
	"search_files":           true,
	"list_subscriptions":     true,
	"list_operator_sessions": true,
	"get_time":               true,
	"ping":                   true,
	"document_search":        true,
	"high_quality_screenshot": true,
	"recall":                 true,
}

// ShouldInterceptInTestMode returns true if this tool should be mocked in test mode.
// Read-only tools and agent calls execute for real.
func ShouldInterceptInTestMode(toolName string) bool {
	return !realExecuteTools[toolName]
}

// readOnlyMCPPrefixes are prefixes that indicate a read-only MCP tool.
// Tools matching these prefixes execute for real in test mode.
var readOnlyMCPPrefixes = []string{
	"get_", "list_", "search_", "find_", "query_", "fetch_",
	"read_", "describe_", "count_", "check_", "show_", "lookup_",
	"view_", "inspect_", "status_", "info_",
}

// readOnlyMCPContains are substrings that indicate a read-only MCP tool.
var readOnlyMCPContains = []string{
	"_list", "_get", "_search", "_find", "_query", "_fetch",
	"_read", "_describe", "_count", "_check", "_status", "_info",
}

// IsMCPReadOnly determines if an MCP tool name looks like a read-only operation.
// Read-only MCP tools execute for real in test mode, returning actual data.
// Write operations (create, update, delete, send, etc.) are mocked.
func IsMCPReadOnly(toolName string) bool {
	lower := strings.ToLower(toolName)

	for _, prefix := range readOnlyMCPPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, sub := range readOnlyMCPContains {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
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
// Only called for write/mutating tools — read-only tools execute for real.
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

	case "wait":
		base["message"] = "[TEST] Wait completed (simulated)"

	default:
		base["tool"] = toolName
		base["message"] = fmt.Sprintf("[TEST] Tool '%s' executed (simulated)", toolName)
	}

	return base, nil
}

// SimulateMCPToolResult returns a mock result for a write/mutating MCP tool in test mode.
// Read-only MCP tools should not reach here — they execute for real.
func SimulateMCPToolResult(toolName string, input map[string]interface{}) string {
	return fmt.Sprintf(`{"test_mode":true,"success":true,"tool":"%s","message":"[TEST] MCP tool '%s' write blocked in test mode (simulated)"}`, toolName, toolName)
}
