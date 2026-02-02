package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apteva/agent/config"
)

// StdioMCPClient connects to MCP servers using stdio transport (stdin/stdout)
type StdioMCPClient struct {
	name         string
	command      []string
	env          []string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *bufio.Reader
	stderr       io.ReadCloser
	initialized  bool
	serverInfo   *ServerInfo
	capabilities *ServerCapabilities
	requestID    int64
	mu           sync.Mutex
	responseChan chan *JSONRPCResponse
	done         chan struct{}
}

// StdioMCPServerConfig configures a stdio MCP server
type StdioMCPServerConfig struct {
	Name    string            `json:"name"`              // Unique name for this server
	Command []string          `json:"command"`           // Command to run (e.g., ["npx", "-y", "@dangahagan/weather-mcp"])
	Env     map[string]string `json:"env,omitempty"`     // Environment variables
	Enabled bool              `json:"enabled"`
}

// NewStdioMCPClient creates a new stdio MCP client
func NewStdioMCPClient(cfg StdioMCPServerConfig) *StdioMCPClient {
	// Convert env map to slice
	var env []string
	for k, v := range cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return &StdioMCPClient{
		name:         cfg.Name,
		command:      cfg.Command,
		env:          env,
		responseChan: make(chan *JSONRPCResponse, 100),
		done:         make(chan struct{}),
	}
}

// Start starts the MCP server process
func (c *StdioMCPClient) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.command) == 0 {
		return fmt.Errorf("no command specified")
	}

	c.cmd = exec.Command(c.command[0], c.command[1:]...)

	// Set up environment
	if len(c.env) > 0 {
		c.cmd.Env = append(c.cmd.Environ(), c.env...)
	}

	// Set up pipes
	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	c.stdout = bufio.NewReader(stdout)

	c.stderr, err = c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	log.Printf("🔌 MCP Stdio [%s]: Process started (pid: %d)", c.name, c.cmd.Process.Pid)

	// Start reading responses in background
	go c.readResponses()

	// Log stderr in background
	go c.logStderr()

	return nil
}

// readResponses reads JSON-RPC responses from stdout
func (c *StdioMCPClient) readResponses() {
	defer close(c.done)

	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("⚠️ MCP Stdio [%s]: Error reading stdout: %v", c.name, err)
			}
			return
		}

		// Skip empty lines
		if len(line) <= 1 {
			continue
		}

		// Try to parse as JSON-RPC response
		var response JSONRPCResponse
		if err := json.Unmarshal(line, &response); err != nil {
			// Not a valid JSON-RPC response, might be a log line
			log.Printf("📝 MCP Stdio [%s]: %s", c.name, string(line))
			continue
		}

		// Send to response channel
		select {
		case c.responseChan <- &response:
		default:
			log.Printf("⚠️ MCP Stdio [%s]: Response channel full, dropping response", c.name)
		}
	}
}

// logStderr logs stderr output
func (c *StdioMCPClient) logStderr() {
	reader := bufio.NewReader(c.stderr)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if line != "" {
			log.Printf("📝 MCP Stdio [%s] stderr: %s", c.name, line)
		}
	}
}

// Stop stops the MCP server process
func (c *StdioMCPClient) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil && c.cmd.Process != nil {
		log.Printf("🔌 MCP Stdio [%s]: Stopping process", c.name)
		c.stdin.Close()
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}

	return nil
}

// nextID generates the next request ID
func (c *StdioMCPClient) nextID() int {
	return int(atomic.AddInt64(&c.requestID, 1))
}

// sendRequest sends a JSON-RPC request and waits for response
func (c *StdioMCPClient) sendRequest(method string, params interface{}) (*JSONRPCResponse, error) {
	c.mu.Lock()

	id := c.nextID()
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Write request
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	c.mu.Unlock()

	// Wait for response with timeout
	timeout := time.After(30 * time.Second)
	for {
		select {
		case response := <-c.responseChan:
			if response.ID == id {
				return response, nil
			}
			// Not our response, put it back (simplified - in production use a map)
			// For now, just log and continue
			log.Printf("⚠️ MCP Stdio [%s]: Got response for different ID: %d (expected %d)", c.name, response.ID, id)
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for response")
		case <-c.done:
			return nil, fmt.Errorf("process exited")
		}
	}
}

// sendNotification sends a JSON-RPC notification (no response expected)
func (c *StdioMCPClient) sendNotification(method string, params interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	notification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		notification["params"] = params
	}

	data, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write notification: %w", err)
	}

	return nil
}

// Initialize initializes the MCP connection
func (c *StdioMCPClient) Initialize() error {
	c.mu.Lock()
	if c.initialized {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	// Start the process if not already started
	if c.cmd == nil {
		if err := c.Start(); err != nil {
			return err
		}
	}

	// Give process a moment to start
	time.Sleep(500 * time.Millisecond)

	params := InitializeParams{
		ProtocolVersion: MCPProtocolVersion,
		Capabilities: ClientCapabilities{
			Roots: &RootsCapability{ListChanged: true},
		},
		ClientInfo: ClientInfo{
			Name:    "apteva-agent",
			Version: config.GetVersion(),
		},
	}

	resp, err := c.sendRequest("initialize", params)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
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

	c.mu.Lock()
	c.serverInfo = &result.ServerInfo
	c.capabilities = &result.Capabilities
	c.initialized = true
	c.mu.Unlock()

	log.Printf("🔌 MCP Stdio [%s]: Initialized - server=%s version=%s",
		c.name, result.ServerInfo.Name, result.ServerInfo.Version)

	// Send initialized notification
	c.sendNotification("notifications/initialized", nil)

	return nil
}

// ListTools fetches available tools from the server
func (c *StdioMCPClient) ListTools() ([]MCPToolDefinition, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	resp, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error: %s", resp.Error.Message)
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

	log.Printf("🔧 MCP Stdio [%s]: Loaded %d tools", c.name, len(result.Tools))
	return result.Tools, nil
}

// CallTool executes a tool on the server
func (c *StdioMCPClient) CallTool(name string, arguments map[string]interface{}) (*ToolCallResult, error) {
	if !c.initialized {
		if err := c.Initialize(); err != nil {
			return nil, err
		}
	}

	params := ToolCallParams{
		Name:      name,
		Arguments: arguments,
	}

	resp, err := c.sendRequest("tools/call", params)
	if err != nil {
		return nil, fmt.Errorf("tools/call failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/call error: %s", resp.Error.Message)
	}

	// Parse result
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var result ToolCallResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool result: %w", err)
	}

	return &result, nil
}

// GetServerInfo returns server info
func (c *StdioMCPClient) GetServerInfo() *ServerInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverInfo
}

// ServerInfo returns server info (alias for GetServerInfo for interface consistency)
func (c *StdioMCPClient) ServerInfo() *ServerInfo {
	return c.GetServerInfo()
}

// IsInitialized returns whether the client is initialized
func (c *StdioMCPClient) IsInitialized() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initialized
}

// Name returns the server name
func (c *StdioMCPClient) Name() string {
	return c.name
}
