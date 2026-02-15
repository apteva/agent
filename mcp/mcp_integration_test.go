package mcp

import (
	"context"
	"testing"
	"time"
)

// Integration tests against a real MCP server (AgentDojo pushover).
// These tests verify the full HTTP client flow: initialize → list tools → call tool → error handling.

const (
	testMCPURL    = "https://api.agentdojo.dev/srv-wn7gn84x"
	testMCPAPIKey = "key_489b04b77debb696cb3002b34f62a07f1c4fae4586a5e71e"
	testMCPName   = "test-pushover"
)

func newTestClient() *StandardMCPClient {
	return NewStandardMCPClient(StandardMCPServerConfig{
		Name:    testMCPName,
		URL:     testMCPURL,
		Headers: map[string]string{"X-API-Key": testMCPAPIKey},
		Timeout: 15 * time.Second,
		Enabled: true,
	})
}

func TestMCPInitialize(t *testing.T) {
	client := newTestClient()
	defer client.Close()

	err := client.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if !client.IsInitialized() {
		t.Fatal("Client should be initialized")
	}

	info := client.ServerInfo()
	if info == nil {
		t.Fatal("ServerInfo should not be nil after init")
	}
	t.Logf("Server: %s v%s", info.Name, info.Version)
}

func TestMCPListTools(t *testing.T) {
	client := newTestClient()
	defer client.Close()

	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}

	if len(tools) == 0 {
		t.Fatal("Expected at least one tool")
	}

	foundNotify := false
	for _, tool := range tools {
		t.Logf("Tool: %s - %s", tool.Name, tool.Description)
		if tool.Name == "pushover-notifications-send-notification" {
			foundNotify = true
			if tool.InputSchema == nil {
				t.Error("Expected InputSchema for send-notification")
			}
		}
	}

	if !foundNotify {
		t.Error("Expected to find pushover-notifications-send-notification tool")
	}
}

func TestMCPCallToolSuccess(t *testing.T) {
	client := newTestClient()
	defer client.Close()

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Call the notification tool — this should succeed
	result, err := client.CallTool("pushover-notifications-send-notification", map[string]interface{}{
		"message": "MCP integration test from apteva agent",
		"title":   "Test Notification",
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	t.Logf("Result: isError=%v, blocks=%d", result.IsError, len(result.Content))
	for i, block := range result.Content {
		t.Logf("  Block[%d]: type=%s, text=%s", i, block.Type, truncateString(block.Text, 200))
	}
}

func TestMCPCallToolWithContext(t *testing.T) {
	client := newTestClient()
	defer client.Close()

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	ctx := context.Background()
	result, err := client.CallToolWithContext(ctx, "pushover-notifications-send-notification", map[string]interface{}{
		"message": "MCP context test from apteva agent",
		"title":   "Context Test",
	})
	if err != nil {
		t.Fatalf("CallToolWithContext failed: %v", err)
	}

	t.Logf("Result: isError=%v, blocks=%d", result.IsError, len(result.Content))
}

func TestMCPCallToolWithContextCancellation(t *testing.T) {
	client := newTestClient()
	defer client.Close()

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Cancel the context immediately — should fail fast
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling

	_, err := client.CallToolWithContext(ctx, "pushover-notifications-send-notification", map[string]interface{}{
		"message": "This should be cancelled",
	})
	if err == nil {
		t.Fatal("Expected error from cancelled context")
	}
	t.Logf("Got expected error: %v", err)
}

func TestMCPCallToolWithContextTimeout(t *testing.T) {
	client := newTestClient()
	defer client.Close()

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Very short timeout — should timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond) // Ensure timeout fires

	_, err := client.CallToolWithContext(ctx, "pushover-notifications-send-notification", map[string]interface{}{
		"message": "This should timeout",
	})
	if err == nil {
		t.Fatal("Expected error from timed-out context")
	}
	t.Logf("Got expected timeout error: %v", err)
}

func TestMCPCallToolStreamingSuccess(t *testing.T) {
	client := newTestClient()
	defer client.Close()

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var notifications []MCPNotification
	callback := func(n MCPNotification) {
		notifications = append(notifications, n)
		t.Logf("Notification: type=%s, message=%s", n.Type, n.Message)
	}

	result, err := client.CallToolStreaming("pushover-notifications-send-notification", map[string]interface{}{
		"message": "MCP streaming test from apteva agent",
		"title":   "Streaming Test",
	}, callback)
	if err != nil {
		t.Fatalf("CallToolStreaming failed: %v", err)
	}

	t.Logf("Result: isError=%v, blocks=%d, notifications=%d", result.IsError, len(result.Content), len(notifications))
}

func TestMCPCallToolStreamingWithContextCancellation(t *testing.T) {
	client := newTestClient()
	defer client.Close()

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling

	_, err := client.CallToolStreamingWithContext(ctx, "pushover-notifications-send-notification", map[string]interface{}{
		"message": "This should be cancelled",
	}, func(n MCPNotification) {})
	if err == nil {
		t.Fatal("Expected error from cancelled context")
	}
	t.Logf("Got expected error: %v", err)
}

func TestMCPCallToolNotFound(t *testing.T) {
	client := newTestClient()
	defer client.Close()

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Call a non-existent tool — server should return an error
	_, err := client.CallTool("nonexistent-tool-xyz", map[string]interface{}{
		"message": "test",
	})
	if err == nil {
		t.Fatal("Expected error for non-existent tool")
	}
	t.Logf("Got expected error: %v", err)
}

func TestMCPCallToolInvalidURL(t *testing.T) {
	// Test with a server that doesn't exist — should fail with connection error
	client := NewStandardMCPClient(StandardMCPServerConfig{
		Name:    "bad-server",
		URL:     "https://nonexistent.invalid/mcp",
		Headers: map[string]string{},
		Timeout: 5 * time.Second,
		Enabled: true,
	})
	defer client.Close()

	_, err := client.CallTool("some-tool", map[string]interface{}{})
	if err == nil {
		t.Fatal("Expected error for invalid server URL")
	}
	t.Logf("Got expected connection error: %v", err)
}

func TestMCPServerManagerFlow(t *testing.T) {
	// Test the full server manager flow: add server → get tools → call tool
	manager := &ExternalServerManager{
		httpClients:  make(map[string]*StandardMCPClient),
		stdioClients: make(map[string]*StdioMCPClient),
		tools:        make(map[string]ExternalMCPTool),
	}

	cfg := StandardMCPServerConfig{
		Name:    testMCPName,
		URL:     testMCPURL,
		Headers: map[string]string{"X-API-Key": testMCPAPIKey},
		Timeout: 15 * time.Second,
		Enabled: true,
	}

	// Create and initialize client
	client := NewStandardMCPClient(cfg)
	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	manager.httpClients[cfg.Name] = client

	// Load tools
	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	displayName := cfg.Name
	if info := client.ServerInfo(); info != nil && info.Name != "" {
		displayName = info.Name
	}
	manager.addToolsFromServer(cfg.Name, displayName, tools)

	// Verify tools are registered
	allTools := manager.GetTools()
	if len(allTools) == 0 {
		t.Fatal("Expected tools to be registered")
	}
	t.Logf("Registered %d tools", len(allTools))

	// Find the notification tool
	notifyToolName := MakeExternalToolName(testMCPName, "pushover-notifications-send-notification")
	tool := manager.GetTool(notifyToolName)
	if tool == nil {
		t.Fatalf("Expected to find tool %s", notifyToolName)
	}

	// Call tool through manager with context
	ctx := context.Background()
	result, err := manager.CallToolWithContext(ctx, notifyToolName, map[string]interface{}{
		"message": "MCP manager flow test",
		"title":   "Manager Test",
	})
	if err != nil {
		t.Fatalf("CallToolWithContext via manager failed: %v", err)
	}

	t.Logf("Manager call result: isError=%v, blocks=%d", result.IsError, len(result.Content))

	// Test error case: call non-existent tool through manager
	_, err = manager.CallToolWithContext(ctx, "nonexistent__tool", map[string]interface{}{})
	if err == nil {
		t.Fatal("Expected error for non-existent tool via manager")
	}
	t.Logf("Manager error (expected): %v", err)

	// Test cancellation through manager
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = manager.CallToolWithContext(cancelCtx, notifyToolName, map[string]interface{}{
		"message": "cancelled",
	})
	if err == nil {
		t.Fatal("Expected error from cancelled context via manager")
	}
	t.Logf("Manager cancellation error (expected): %v", err)

	// Cleanup
	client.Close()
}
