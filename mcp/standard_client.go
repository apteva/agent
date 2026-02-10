package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apteva/agent/config"
)

// StandardMCPClient connects to standard MCP servers using Streamable HTTP transport
type StandardMCPClient struct {
	name        string
	url         string
	headers     map[string]string
	httpClient  *http.Client
	sessionID   string
	initialized bool
	serverInfo  *ServerInfo
	capabilities *ServerCapabilities
	requestID   int64
	mu          sync.RWMutex
}

// StandardMCPServerConfig configures a standard MCP server connection
type StandardMCPServerConfig struct {
	Name     string            `json:"name"`              // Unique name for this server
	URL      string            `json:"url"`               // Server endpoint URL
	Headers  map[string]string `json:"headers,omitempty"` // Optional headers (for auth, etc.)
	Timeout  time.Duration     `json:"timeout,omitempty"` // Request timeout
	Enabled  bool              `json:"enabled"`
}

// NewStandardMCPClient creates a new standard MCP client
func NewStandardMCPClient(cfg StandardMCPServerConfig) *StandardMCPClient {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &StandardMCPClient{
		name:    cfg.Name,
		url:     cfg.URL,
		headers: cfg.Headers,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		requestID: 0,
	}
}

// nextID generates the next request ID
func (c *StandardMCPClient) nextID() int {
	return int(atomic.AddInt64(&c.requestID, 1))
}

// doRequest sends a JSON-RPC request and returns the response
func (c *StandardMCPClient) doRequest(method string, params interface{}) (*JSONRPCResponse, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  method,
		Params:  params,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// Add session ID if we have one
	c.mu.RLock()
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.RUnlock()

	// Add custom headers
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for session ID in response
	if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID != "" {
		c.mu.Lock()
		c.sessionID = sessionID
		c.mu.Unlock()
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	// Handle SSE response format (event: message\ndata: {...})
	jsonBody := body
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") || strings.HasPrefix(string(body), "event:") {
		jsonBody = parseSSEResponse(body)
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(jsonBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w (body: %s)", err, string(body))
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return &rpcResp, nil
}

// Initialize performs the MCP handshake with the server
func (c *StandardMCPClient) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	params := InitializeParams{
		ProtocolVersion: MCPProtocolVersion,
		Capabilities:    ClientCapabilities{},
		ClientInfo: ClientInfo{
			Name:    "apteva-agent",
			Version: config.GetVersion(),
		},
	}

	// Temporarily unlock for the request
	c.mu.Unlock()
	resp, err := c.doRequest("initialize", params)
	c.mu.Lock()

	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	// Parse result
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	var result InitializeResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return fmt.Errorf("failed to parse initialize result: %w", err)
	}

	c.serverInfo = &result.ServerInfo
	c.capabilities = &result.Capabilities
	c.initialized = true

	log.Printf("🔌 MCP [%s]: Initialized - server=%s version=%s",
		c.name, result.ServerInfo.Name, result.ServerInfo.Version)

	// Send initialized notification (no response expected)
	c.mu.Unlock()
	c.sendNotification("notifications/initialized", nil)
	c.mu.Lock()

	return nil
}

// sendNotification sends a JSON-RPC notification (no ID, no response)
func (c *StandardMCPClient) sendNotification(method string, params interface{}) {
	notification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		notification["params"] = params
	}

	jsonData, err := json.Marshal(notification)
	if err != nil {
		log.Printf("⚠️ MCP [%s]: Failed to marshal notification: %v", c.name, err)
		return
	}

	httpReq, err := http.NewRequest("POST", c.url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("⚠️ MCP [%s]: Failed to create notification request: %v", c.name, err)
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("⚠️ MCP [%s]: Notification failed: %v", c.name, err)
		return
	}
	resp.Body.Close()
}

// ListTools fetches available tools from the server
func (c *StandardMCPClient) ListTools() ([]MCPToolDefinition, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	resp, err := c.doRequest("tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}

	// Parse result
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var result ToolsListResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools list: %w", err)
	}

	log.Printf("🔧 MCP [%s]: Loaded %d tools", c.name, len(result.Tools))
	return result.Tools, nil
}

// CallTool executes a tool on the server
func (c *StandardMCPClient) CallTool(name string, arguments map[string]interface{}) (*ToolCallResult, error) {
	if !c.initialized {
		log.Printf("🔌 MCP HTTP [%s]: Auto-initializing for tool call '%s'", c.name, name)
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	params := ToolCallParams{
		Name:      name,
		Arguments: arguments,
	}

	log.Printf("🔧 MCP HTTP [%s]: Calling tool '%s' → POST %s (timeout=%v)",
		c.name, name, c.url, c.httpClient.Timeout)
	startTime := time.Now()

	resp, err := c.doRequest("tools/call", params)
	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("❌ MCP HTTP [%s]: Tool '%s' request FAILED after %v: %v", c.name, name, elapsed, err)
		return nil, fmt.Errorf("tools/call failed: %w", err)
	}

	log.Printf("✅ MCP HTTP [%s]: Tool '%s' got response in %v", c.name, name, elapsed)

	// Parse result
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	log.Printf("📋 MCP HTTP [%s]: Tool '%s' response size=%d bytes", c.name, name, len(resultBytes))

	var result ToolCallResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		log.Printf("❌ MCP HTTP [%s]: Tool '%s' failed to parse result: %v (raw: %s)",
			c.name, name, err, truncateString(string(resultBytes), 500))
		return nil, fmt.Errorf("failed to parse tool result: %w", err)
	}

	log.Printf("📋 MCP HTTP [%s]: Tool '%s' parsed: isError=%v, content_blocks=%d",
		c.name, name, result.IsError, len(result.Content))

	return &result, nil
}

// ListResources fetches available resources from the server
func (c *StandardMCPClient) ListResources() ([]MCPResourceDefinition, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	// Check if server supports resources
	c.mu.RLock()
	hasResources := c.capabilities != nil && c.capabilities.Resources != nil
	c.mu.RUnlock()

	if !hasResources {
		return []MCPResourceDefinition{}, nil
	}

	resp, err := c.doRequest("resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("resources/list failed: %w", err)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var result ResourcesListResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse resources list: %w", err)
	}

	return result.Resources, nil
}

// ListPrompts fetches available prompts from the server
func (c *StandardMCPClient) ListPrompts() ([]MCPPromptDefinition, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	// Check if server supports prompts
	c.mu.RLock()
	hasPrompts := c.capabilities != nil && c.capabilities.Prompts != nil
	c.mu.RUnlock()

	if !hasPrompts {
		return []MCPPromptDefinition{}, nil
	}

	resp, err := c.doRequest("prompts/list", nil)
	if err != nil {
		return nil, fmt.Errorf("prompts/list failed: %w", err)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var result PromptsListResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse prompts list: %w", err)
	}

	return result.Prompts, nil
}

// Name returns the server name
func (c *StandardMCPClient) Name() string {
	return c.name
}

// ServerInfo returns the server info (after initialization)
func (c *StandardMCPClient) ServerInfo() *ServerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverInfo
}

// IsInitialized returns true if the client has been initialized
func (c *StandardMCPClient) IsInitialized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialized
}

// Close closes the client connection
func (c *StandardMCPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initialized = false
	c.sessionID = ""
	return nil
}

// parseSSEResponse extracts JSON from SSE format
// SSE format: "event: message\ndata: {...}\n\n"
func parseSSEResponse(body []byte) []byte {
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			// Extract JSON after "data: "
			jsonStr := strings.TrimPrefix(line, "data:")
			jsonStr = strings.TrimSpace(jsonStr)
			return []byte(jsonStr)
		}
	}
	// If no data line found, return original body
	return body
}
