package mcp

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/apteva/agent/config"
)

// ExternalServerManager manages connections to external standard MCP servers
type ExternalServerManager struct {
	clients map[string]*StandardMCPClient // keyed by server name
	tools   map[string]ExternalMCPTool    // keyed by "server__tool"
	mu      sync.RWMutex
}

// ExternalMCPTool represents a tool from an external MCP server
type ExternalMCPTool struct {
	ServerName  string                 `json:"server_name"`
	Name        string                 `json:"name"`
	FullName    string                 `json:"full_name"` // "server__tool"
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// Global external server manager
var globalExternalManager *ExternalServerManager
var externalManagerOnce sync.Once

// GetExternalServerManager returns the global external server manager
func GetExternalServerManager() *ExternalServerManager {
	externalManagerOnce.Do(func() {
		globalExternalManager = &ExternalServerManager{
			clients: make(map[string]*StandardMCPClient),
			tools:   make(map[string]ExternalMCPTool),
		}
	})
	return globalExternalManager
}

// AddServer adds and initializes a new external MCP server
func (m *ExternalServerManager) AddServer(cfg StandardMCPServerConfig) error {
	if !cfg.Enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.clients[cfg.Name]; exists {
		log.Printf("🔌 MCP External [%s]: Already connected, skipping", cfg.Name)
		return nil
	}

	// Create client
	client := NewStandardMCPClient(cfg)

	// Initialize connection
	if err := client.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize server %s: %w", cfg.Name, err)
	}

	m.clients[cfg.Name] = client

	// Load tools from this server
	tools, err := client.ListTools()
	if err != nil {
		log.Printf("⚠️ MCP External [%s]: Failed to list tools: %v", cfg.Name, err)
	} else {
		for _, tool := range tools {
			fullName := MakeExternalToolName(cfg.Name, tool.Name)
			m.tools[fullName] = ExternalMCPTool{
				ServerName:  cfg.Name,
				Name:        tool.Name,
				FullName:    fullName,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			}
		}
		log.Printf("🔧 MCP External [%s]: Loaded %d tools", cfg.Name, len(tools))
	}

	return nil
}

// RemoveServer removes an external MCP server
func (m *ExternalServerManager) RemoveServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.clients[name]
	if !exists {
		return nil
	}

	// Close client
	client.Close()
	delete(m.clients, name)

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
		m.mu.RUnlock()
		return nil, fmt.Errorf("tool '%s' not found", fullName)
	}

	client, clientExists := m.clients[tool.ServerName]
	m.mu.RUnlock()

	if !clientExists {
		return nil, fmt.Errorf("server '%s' not connected", tool.ServerName)
	}

	return client.CallTool(tool.Name, arguments)
}

// GetServerNames returns list of connected server names
func (m *ExternalServerManager) GetServerNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

// RefreshServer refreshes tools from a specific server
func (m *ExternalServerManager) RefreshServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.clients[name]
	if !exists {
		return fmt.Errorf("server '%s' not connected", name)
	}

	// Remove old tools from this server
	for key, tool := range m.tools {
		if tool.ServerName == name {
			delete(m.tools, key)
		}
	}

	// Reload tools
	tools, err := client.ListTools()
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	for _, tool := range tools {
		fullName := MakeExternalToolName(name, tool.Name)
		m.tools[fullName] = ExternalMCPTool{
			ServerName:  name,
			Name:        tool.Name,
			FullName:    fullName,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}
	}

	log.Printf("🔧 MCP External [%s]: Refreshed %d tools", name, len(tools))
	return nil
}

// InitializeFromConfig loads all external servers from config
func InitializeExternalServers(servers []config.ExternalMCPServer) error {
	manager := GetExternalServerManager()

	for _, server := range servers {
		if !server.Enabled {
			continue
		}

		cfg := StandardMCPServerConfig{
			Name:    server.Name,
			URL:     server.URL,
			Headers: server.Headers,
			Timeout: 30 * time.Second,
			Enabled: server.Enabled,
		}

		if err := manager.AddServer(cfg); err != nil {
			log.Printf("⚠️ MCP External [%s]: Failed to connect: %v", server.Name, err)
			// Continue with other servers
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
