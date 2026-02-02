package mcp

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/apteva/agent/config"
)

// ExternalClientInterface for both HTTP and stdio external MCP clients
type ExternalClientInterface interface {
	Initialize() error
	ListTools() ([]MCPToolDefinition, error)
	CallTool(name string, arguments map[string]interface{}) (*ToolCallResult, error)
	Name() string
}

// ExternalServerManager manages connections to external standard MCP servers
type ExternalServerManager struct {
	httpClients  map[string]*StandardMCPClient // HTTP clients keyed by server name
	stdioClients map[string]*StdioMCPClient    // Stdio clients keyed by server name
	tools        map[string]ExternalMCPTool    // keyed by "server__tool"
	mu           sync.RWMutex
}

// ExternalMCPTool represents a tool from an external MCP server
type ExternalMCPTool struct {
	ServerName        string                 `json:"server_name"`
	ServerDisplayName string                 `json:"server_display_name"` // From MCP server's ServerInfo.Name
	Name              string                 `json:"name"`
	Title             string                 `json:"title"`     // Human-readable title (e.g., "Add email for auth user")
	FullName          string                 `json:"full_name"` // "server__tool"
	Description       string                 `json:"description"`
	InputSchema       map[string]interface{} `json:"inputSchema"`
}

// Global external server manager
var globalExternalManager *ExternalServerManager
var externalManagerOnce sync.Once

// GetExternalServerManager returns the global external server manager
func GetExternalServerManager() *ExternalServerManager {
	externalManagerOnce.Do(func() {
		globalExternalManager = &ExternalServerManager{
			httpClients:  make(map[string]*StandardMCPClient),
			stdioClients: make(map[string]*StdioMCPClient),
			tools:        make(map[string]ExternalMCPTool),
		}
	})
	return globalExternalManager
}

// AddServer adds and initializes a new HTTP-based external MCP server
func (m *ExternalServerManager) AddServer(cfg StandardMCPServerConfig) error {
	if !cfg.Enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.httpClients[cfg.Name]; exists {
		log.Printf("🔌 MCP HTTP [%s]: Already connected, skipping", cfg.Name)
		return nil
	}

	// Create client
	client := NewStandardMCPClient(cfg)

	// Initialize connection
	if err := client.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize server %s: %w", cfg.Name, err)
	}

	m.httpClients[cfg.Name] = client

	// Get display name from server's self-reported info
	displayName := cfg.Name
	if serverInfo := client.ServerInfo(); serverInfo != nil && serverInfo.Name != "" {
		displayName = serverInfo.Name
	}

	// Load tools from this server
	tools, err := client.ListTools()
	if err != nil {
		log.Printf("⚠️ MCP HTTP [%s]: Failed to list tools: %v", cfg.Name, err)
	} else {
		m.addToolsFromServer(cfg.Name, displayName, tools)
	}

	return nil
}

// AddStdioServer adds and initializes a new stdio-based MCP server
func (m *ExternalServerManager) AddStdioServer(cfg StdioMCPServerConfig) error {
	if !cfg.Enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.stdioClients[cfg.Name]; exists {
		log.Printf("🔌 MCP Stdio [%s]: Already connected, skipping", cfg.Name)
		return nil
	}

	// Create client
	client := NewStdioMCPClient(cfg)

	// Initialize connection (starts process and handshakes)
	if err := client.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize stdio server %s: %w", cfg.Name, err)
	}

	m.stdioClients[cfg.Name] = client

	// Get display name from server's self-reported info
	displayName := cfg.Name
	if serverInfo := client.ServerInfo(); serverInfo != nil && serverInfo.Name != "" {
		displayName = serverInfo.Name
	}

	// Load tools from this server
	tools, err := client.ListTools()
	if err != nil {
		log.Printf("⚠️ MCP Stdio [%s]: Failed to list tools: %v", cfg.Name, err)
	} else {
		m.addToolsFromServer(cfg.Name, displayName, tools)
	}

	return nil
}

// addToolsFromServer adds tools from a server to the tools map (must be called with lock held)
func (m *ExternalServerManager) addToolsFromServer(serverName, displayName string, tools []MCPToolDefinition) {
	for i, tool := range tools {
		fullName := MakeExternalToolName(serverName, tool.Name)
		// Use Title, DisplayName, or empty (will fall back to description later)
		title := tool.Title
		if title == "" {
			title = tool.DisplayName
		}
		// Debug: log first few tools to see what fields are being returned
		if i < 3 {
			log.Printf("🔍 DEBUG Tool[%d]: name=%s, title=%q, displayName=%q, desc=%q",
				i, tool.Name, tool.Title, tool.DisplayName, truncateString(tool.Description, 50))
		}
		m.tools[fullName] = ExternalMCPTool{
			ServerName:        serverName,
			ServerDisplayName: displayName,
			Name:              tool.Name,
			Title:             title,
			FullName:          fullName,
			Description:       tool.Description,
			InputSchema:       tool.InputSchema,
		}
	}
	log.Printf("🔧 MCP External [%s] (%s): Loaded %d tools", serverName, displayName, len(tools))
}

// truncateString truncates a string to max length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RemoveServer removes an external MCP server
func (m *ExternalServerManager) RemoveServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check HTTP clients
	if client, exists := m.httpClients[name]; exists {
		client.Close()
		delete(m.httpClients, name)
	}

	// Check stdio clients
	if client, exists := m.stdioClients[name]; exists {
		client.Stop()
		delete(m.stdioClients, name)
	}

	// Remove tools from this server
	for key, tool := range m.tools {
		if tool.ServerName == name {
			delete(m.tools, key)
		}
	}

	log.Printf("🔌 MCP External [%s]: Disconnected", name)
	return nil
}

// GetTools returns all tools from all external servers
func (m *ExternalServerManager) GetTools() []ExternalMCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tools := make([]ExternalMCPTool, 0, len(m.tools))
	for _, tool := range m.tools {
		tools = append(tools, tool)
	}
	return tools
}

// GetTool returns a specific tool by full name (server:tool)
func (m *ExternalServerManager) GetTool(fullName string) *ExternalMCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if tool, exists := m.tools[fullName]; exists {
		return &tool
	}
	return nil
}

// CallTool executes a tool on the appropriate external server
func (m *ExternalServerManager) CallTool(fullName string, arguments map[string]interface{}) (*ToolCallResult, error) {
	m.mu.RLock()
	tool, exists := m.tools[fullName]
	if !exists {
		// Try reverse lookup: Claude may have received a sanitized name
		// Look for a tool whose sanitized name matches the requested name
		for originalName, t := range m.tools {
			if sanitizeToolName(originalName) == fullName {
				tool = t
				exists = true
				log.Printf("🔧 MCP: Matched sanitized tool name '%s' to original '%s'", fullName, originalName)
				break
			}
		}
	}
	if !exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("tool '%s' not found", fullName)
	}

	// Try HTTP client first
	if client, ok := m.httpClients[tool.ServerName]; ok {
		m.mu.RUnlock()
		return client.CallTool(tool.Name, arguments)
	}

	// Try stdio client
	if client, ok := m.stdioClients[tool.ServerName]; ok {
		m.mu.RUnlock()
		return client.CallTool(tool.Name, arguments)
	}

	m.mu.RUnlock()
	return nil, fmt.Errorf("server '%s' not connected", tool.ServerName)
}

// GetServerNames returns list of connected server names
func (m *ExternalServerManager) GetServerNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.httpClients)+len(m.stdioClients))
	for name := range m.httpClients {
		names = append(names, name)
	}
	for name := range m.stdioClients {
		names = append(names, name)
	}
	return names
}

// RefreshServer refreshes tools from a specific server
func (m *ExternalServerManager) RefreshServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove old tools from this server
	for key, tool := range m.tools {
		if tool.ServerName == name {
			delete(m.tools, key)
		}
	}

	var tools []MCPToolDefinition
	var err error
	displayName := name

	// Try HTTP client
	if client, exists := m.httpClients[name]; exists {
		tools, err = client.ListTools()
		if serverInfo := client.ServerInfo(); serverInfo != nil && serverInfo.Name != "" {
			displayName = serverInfo.Name
		}
	} else if client, exists := m.stdioClients[name]; exists {
		tools, err = client.ListTools()
		if serverInfo := client.ServerInfo(); serverInfo != nil && serverInfo.Name != "" {
			displayName = serverInfo.Name
		}
	} else {
		return fmt.Errorf("server '%s' not connected", name)
	}

	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	for _, tool := range tools {
		fullName := MakeExternalToolName(name, tool.Name)
		title := tool.Title
		if title == "" {
			title = tool.DisplayName
		}
		m.tools[fullName] = ExternalMCPTool{
			ServerName:        name,
			ServerDisplayName: displayName,
			Name:              tool.Name,
			Title:             title,
			FullName:          fullName,
			Description:       tool.Description,
			InputSchema:       tool.InputSchema,
		}
	}

	log.Printf("🔧 MCP External [%s] (%s): Refreshed %d tools", name, displayName, len(tools))
	return nil
}

// InitializeFromConfig loads all external servers from config
func InitializeExternalServers(servers []config.ExternalMCPServer) error {
	manager := GetExternalServerManager()

	for _, server := range servers {
		if !server.Enabled {
			continue
		}

		// Determine server type (default to http for backwards compatibility)
		serverType := server.Type
		if serverType == "" {
			if len(server.Command) > 0 {
				serverType = "stdio"
			} else {
				serverType = "http"
			}
		}

		switch serverType {
		case "stdio":
			// Build command with args
			command := server.Command
			if len(server.Args) > 0 {
				command = append(command, server.Args...)
			}

			cfg := StdioMCPServerConfig{
				Name:    server.Name,
				Command: command,
				Env:     server.Env,
				Enabled: server.Enabled,
			}

			if err := manager.AddStdioServer(cfg); err != nil {
				log.Printf("⚠️ MCP Stdio [%s]: Failed to start: %v", server.Name, err)
				// Continue with other servers
			}

		case "http":
			cfg := StandardMCPServerConfig{
				Name:    server.Name,
				URL:     server.URL,
				Headers: server.Headers,
				Timeout: 30 * time.Second,
				Enabled: server.Enabled,
			}

			if err := manager.AddServer(cfg); err != nil {
				log.Printf("⚠️ MCP HTTP [%s]: Failed to connect: %v", server.Name, err)
				// Continue with other servers
			}

		default:
			log.Printf("⚠️ MCP External [%s]: Unknown server type: %s", server.Name, serverType)
		}
	}

	return nil
}

// ConvertExternalToolsToToolDefinitions converts external MCP tools to our internal format
func ConvertExternalToolsToToolDefinitions(tools []ExternalMCPTool) []MCPTool {
	result := make([]MCPTool, len(tools))
	now := time.Now()

	for i, tool := range tools {
		result[i] = MCPTool{
			Name:        tool.FullName, // Use full name (server__tool)
			DisplayName: tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			ServerName:  tool.ServerName,
			LoadedAt:    now,
		}
	}

	return result
}

// ExternalToolSeparator is the character used to separate server name from tool name
// Using double underscore to avoid conflicts with tool names that might contain single underscore
const ExternalToolSeparator = "__"

// IsExternalTool checks if a tool name is from an external server (contains __)
func IsExternalTool(toolName string) bool {
	return strings.Contains(toolName, ExternalToolSeparator)
}

// ParseExternalToolName splits "server__tool" into parts
func ParseExternalToolName(fullName string) (serverName, toolName string) {
	idx := strings.Index(fullName, ExternalToolSeparator)
	if idx >= 0 {
		return fullName[:idx], fullName[idx+len(ExternalToolSeparator):]
	}
	return "", fullName
}

// MakeExternalToolName creates a full tool name from server and tool names
func MakeExternalToolName(serverName, toolName string) string {
	return serverName + ExternalToolSeparator + toolName
}

// sanitizeToolName ensures a tool name matches Anthropic's pattern ^[a-zA-Z0-9_-]{1,128}$
// Invalid characters are replaced with underscores
func sanitizeToolName(name string) string {
	sanitized := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			sanitized = append(sanitized, c)
		} else {
			sanitized = append(sanitized, '_')
		}
	}
	if len(sanitized) > 128 {
		sanitized = sanitized[:128]
	}
	if len(sanitized) == 0 {
		return "unnamed_tool"
	}
	return string(sanitized)
}
