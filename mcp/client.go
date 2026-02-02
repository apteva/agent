package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/apteva/agent/config"
)

// MCPServer represents a cached MCP server
type MCPServer struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description"`
	IconURL     string    `json:"icon_url,omitempty"`
	Version     string    `json:"version"`
	Status      string    `json:"status"`
	LoadedAt    time.Time `json:"loaded_at"`
}

// MCPTool represents a cached MCP tool
type MCPTool struct {
	Name                 string                 `json:"name"`
	DisplayName          string                 `json:"display_name"`
	Description          string                 `json:"description"`
	InputSchema          map[string]interface{} `json:"inputSchema"`
	RequiresCredentials  bool                   `json:"requires_credentials"`
	ServerID             int                    `json:"server_id"`
	ServerName           string                 `json:"server_name"`
	CredentialProviderID int                    `json:"credential_provider_id,omitempty"`
	LoadedAt             time.Time              `json:"loaded_at"`
	LastUsed             time.Time              `json:"last_used,omitempty"`
}

// MCPCache represents the in-memory cache
type MCPCache struct {
	Servers     map[int]MCPServer          `json:"servers"`
	Tools       map[string]MCPTool         `json:"tools"`     // key: "server_name:tool_name"
	Resources   map[string][]MCPResource   `json:"resources"` // key: server_name, value: list of resources
	LastRefresh time.Time                  `json:"last_refresh"`
	mu          sync.RWMutex
}

// MCPClient handles communication with MCP servers
type MCPClient struct {
	config     *config.MCPConfig
	httpClient *http.Client
}

// API Response structures
type MCPServerResponse struct {
	Success bool        `json:"success"`
	Servers []MCPServer `json:"servers"`
}

type MCPToolResponse struct {
	Success bool      `json:"success"`
	Data    []MCPTool `json:"data"`
}

// Global MCP cache instance
var globalMCPCache *MCPCache
var cacheMutex sync.Once

// GetMCPCache returns the global MCP cache instance
func GetMCPCache() *MCPCache {
	cacheMutex.Do(func() {
		globalMCPCache = &MCPCache{
			Servers:   make(map[int]MCPServer),
			Tools:     make(map[string]MCPTool),
			Resources: make(map[string][]MCPResource),
		}
	})
	return globalMCPCache
}

// NewMCPClient creates a new MCP client
func NewMCPClient(cfg *config.MCPConfig) *MCPClient {
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		timeout = 120 * time.Second
	}

	return &MCPClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// LoadServers fetches available servers from MCP endpoint
func (c *MCPClient) LoadServers() ([]MCPServer, error) {
	url := fmt.Sprintf("%s/servers", c.config.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	var lastErr error

	// Retry logic
	for i := 0; i <= c.config.RetryCount; i++ {
		resp, lastErr = c.httpClient.Do(req)
		if lastErr == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if i < c.config.RetryCount {
			time.Sleep(time.Second * time.Duration(i+1)) // exponential backoff
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to fetch servers after %d retries: %w", c.config.RetryCount, lastErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var serverResp MCPServerResponse
	if err := json.NewDecoder(resp.Body).Decode(&serverResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !serverResp.Success {
		return nil, fmt.Errorf("server returned success=false")
	}

	// Set loaded timestamp
	now := time.Now()
	for i := range serverResp.Servers {
		serverResp.Servers[i].LoadedAt = now
	}

	return serverResp.Servers, nil
}

// LoadTools fetches available tools from MCP endpoint
func (c *MCPClient) LoadTools() ([]MCPTool, error) {
	url := fmt.Sprintf("%s/tools/list", c.config.BaseURL)

	// Create request body (empty for now, but can be extended)
	reqBody := map[string]interface{}{}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	var lastErr error

	// Retry logic
	for i := 0; i <= c.config.RetryCount; i++ {
		resp, lastErr = c.httpClient.Do(req)
		if lastErr == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if i < c.config.RetryCount {
			time.Sleep(time.Second * time.Duration(i+1)) // exponential backoff
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to fetch tools after %d retries: %w", c.config.RetryCount, lastErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var toolResp MCPToolResponse
	if err := json.NewDecoder(resp.Body).Decode(&toolResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !toolResp.Success {
		return nil, fmt.Errorf("server returned success=false")
	}

	// Set loaded timestamp
	now := time.Now()
	for i := range toolResp.Data {
		toolResp.Data[i].LoadedAt = now
		if toolResp.Data[i].InputSchema == nil {
			toolResp.Data[i].InputSchema = make(map[string]interface{})
		}
	}

	return toolResp.Data, nil
}

// HealthCheck verifies connection to MCP server
func (c *MCPClient) HealthCheck() error {
	url := fmt.Sprintf("%s/servers", c.config.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	return nil
}

// Cache methods

// GetServers returns all cached servers
func (cache *MCPCache) GetServers() []MCPServer {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	servers := make([]MCPServer, 0, len(cache.Servers))
	for _, server := range cache.Servers {
		servers = append(servers, server)
	}
	return servers
}

// GetTools returns cached tools, optionally filtered by server name
func (cache *MCPCache) GetTools(serverName string) []MCPTool {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	tools := make([]MCPTool, 0, len(cache.Tools))
	for _, tool := range cache.Tools {
		if serverName == "" || tool.ServerName == serverName {
			tools = append(tools, tool)
		}
	}
	return tools
}

// UpdateCache updates the cache with new servers and tools
func (cache *MCPCache) UpdateCache(servers []MCPServer, tools []MCPTool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Update servers
	cache.Servers = make(map[int]MCPServer)
	for _, server := range servers {
		cache.Servers[server.ID] = server
	}

	// Create server ID to name mapping
	serverNames := make(map[int]string)
	for _, server := range servers {
		serverNames[server.ID] = server.Name
	}

	// Update tools
	cache.Tools = make(map[string]MCPTool)
	for _, tool := range tools {
		if serverName, exists := serverNames[tool.ServerID]; exists {
			tool.ServerName = serverName
			key := fmt.Sprintf("%s:%s", serverName, tool.Name)
			cache.Tools[key] = tool
		}
	}

	cache.LastRefresh = time.Now()
}

// UpdateResources updates the cached resources for servers
func (cache *MCPCache) UpdateResources(resourcesByServer map[string][]MCPResource) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.Resources = resourcesByServer
}

// GetResources returns cached resources, optionally filtered by server name
func (cache *MCPCache) GetResources(serverName string) []MCPResource {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	if serverName != "" {
		return cache.Resources[serverName]
	}

	// Return all resources
	var all []MCPResource
	for _, resources := range cache.Resources {
		all = append(all, resources...)
	}
	return all
}

// GetResourceServers returns list of server names that have resources
func (cache *MCPCache) GetResourceServers() []string {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	var servers []string
	for serverName, resources := range cache.Resources {
		if len(resources) > 0 {
			servers = append(servers, serverName)
		}
	}
	return servers
}

// RefreshMCPCache refreshes the MCP cache
func RefreshMCPCache(cfg *config.MCPConfig) error {
	if cfg == nil || !cfg.Enabled {
		return fmt.Errorf("MCP not enabled")
	}

	client := NewMCPClient(cfg)
	cache := GetMCPCache()

	// Load servers
	servers, err := client.LoadServers()
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}

	// Load tools
	tools, err := client.LoadTools()
	if err != nil {
		return fmt.Errorf("failed to load tools: %w", err)
	}

	// Update cache with servers and tools
	cache.UpdateCache(servers, tools)
	log.Printf("🔧 MCP: Loaded %d servers, %d tools", len(servers), len(tools))

	// Build server ID to name mapping
	serverNames := make(map[int]string)
	for _, s := range servers {
		serverNames[s.ID] = s.Name
	}

	// Load all resources in a single call
	log.Printf("📚 MCP: Loading resources from /resources/list...")
	allResources, err := client.ListAllResourcesFromAPI()
	if err != nil {
		log.Printf("📚 MCP: Failed to load resources: %v", err)
		// Don't fail the whole refresh, just skip resources
		return nil
	}

	// Group resources by server
	resourcesByServer := make(map[string][]MCPResource)
	unmappedCount := 0
	for i := range allResources {
		// Set server name from our mapping
		if name, ok := serverNames[allResources[i].ServerID]; ok {
			allResources[i].ServerName = name
			resourcesByServer[name] = append(resourcesByServer[name], allResources[i])
		} else {
			unmappedCount++
			log.Printf("📚 MCP: Resource '%s' has unknown server_id=%d", allResources[i].Name, allResources[i].ServerID)
		}
	}
	cache.UpdateResources(resourcesByServer)

	// Verify cache was updated
	cachedServers := cache.GetResourceServers()
	log.Printf("📚 MCP: Loaded %d resources from %d servers (unmapped: %d, cached servers: %v)",
		len(allResources), len(resourcesByServer), unmappedCount, cachedServers)

	return nil
}

// CheckMCPConnection checks if MCP server is reachable
func CheckMCPConnection(cfg *config.MCPConfig) error {
	if cfg == nil || !cfg.Enabled {
		return fmt.Errorf("MCP not enabled")
	}

	client := NewMCPClient(cfg)
	return client.HealthCheck()
}

// MCPWebhookEvent represents a single webhook event
type MCPWebhookEvent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MCPWebhookServer represents webhooks available from an MCP server
type MCPWebhookServer struct {
	Server      string            `json:"server"`
	DisplayName string            `json:"display_name"`
	Events      []MCPWebhookEvent `json:"events"`
}

// MCPWebhooksResponse is the response from /webhooks/list
type MCPWebhooksResponse struct {
	Success  bool               `json:"success"`
	Webhooks []MCPWebhookServer `json:"webhooks"`
	Count    int                `json:"count"`
}

// ListWebhooks fetches available webhook events from MCP servers
func (c *MCPClient) ListWebhooks() ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/webhooks/list", c.config.BaseURL)

	// Create request body
	reqBody := map[string]interface{}{}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch webhooks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var webhooksResp MCPWebhooksResponse
	if err := json.NewDecoder(resp.Body).Decode(&webhooksResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !webhooksResp.Success {
		return nil, fmt.Errorf("server returned success=false")
	}

	// Convert to []map[string]interface{} for compatibility
	result := make([]map[string]interface{}, len(webhooksResp.Webhooks))
	for i, ws := range webhooksResp.Webhooks {
		events := make([]map[string]interface{}, len(ws.Events))
		for j, e := range ws.Events {
			events[j] = map[string]interface{}{
				"name":        e.Name,
				"description": e.Description,
			}
		}
		result[i] = map[string]interface{}{
			"server":       ws.Server,
			"display_name": ws.DisplayName,
			"events":       events,
		}
	}

	return result, nil
}

// GetToolDisplayName returns the display name for an MCP tool
// Returns empty string if tool not found
func GetToolDisplayName(toolName string) string {
	// Check external tools first (format: serverName__toolName)
	if IsExternalTool(toolName) {
		externalTool := GetExternalServerManager().GetTool(toolName)
		if externalTool != nil {
			// Priority: Title > Cleaned tool name
			// Don't use description (too long) or server name prefix (clutters UI)
			if externalTool.Title != "" {
				return externalTool.Title
			}
			// Clean up tool name: GITHUB_LIST_REPOSITORIES -> "List Repositories"
			return cleanToolName(externalTool.Name)
		}
	}

	cache := GetMCPCache()
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	// Search for the tool by name (tools are keyed as "server:name")
	for key, tool := range cache.Tools {
		// Check if the key ends with the tool name
		if len(key) > len(toolName) && key[len(key)-len(toolName):] == toolName {
			// Verify it's the right format (server:toolName)
			if key[len(key)-len(toolName)-1] == ':' {
				if tool.DisplayName != "" {
					return tool.DisplayName
				}
				// Fall back to converting name to title case if no display name
				return toTitleCase(toolName)
			}
		}
	}

	// Tool not found, return empty string
	return ""
}

// toTitleCase converts "TOOL_NAME" or "tool-name" to "Tool Name"
func toTitleCase(s string) string {
	words := make([]byte, 0, len(s))
	capitalizeNext := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '-' || c == '_' {
			words = append(words, ' ')
			capitalizeNext = true
		} else if capitalizeNext {
			// Capitalize: uppercase if lowercase, keep if already upper
			if c >= 'a' && c <= 'z' {
				words = append(words, c-32)
			} else {
				words = append(words, c)
			}
			capitalizeNext = false
		} else {
			// Lowercase: convert uppercase to lowercase
			if c >= 'A' && c <= 'Z' {
				words = append(words, c+32)
			} else {
				words = append(words, c)
			}
		}
	}
	return string(words)
}

// getShortDescription extracts a short display name from a tool description
// Returns first sentence or truncated string (max 50 chars)
// cleanToolName converts ugly tool names to readable format
// GITHUB_LIST_REPOSITORIES_FOR_THE_AUTHENTICATED_USER -> "List Repositories"
// SLACK_SEND_MESSAGE -> "Send Message"
func cleanToolName(name string) string {
	if name == "" {
		return ""
	}

	// Remove common prefixes (GITHUB_, SLACK_, etc.)
	prefixes := []string{
		"GITHUB_", "SLACK_", "GMAIL_", "GOOGLE_", "DISCORD_",
		"NOTION_", "JIRA_", "TRELLO_", "ASANA_", "LINEAR_",
		"TWITTER_", "FACEBOOK_", "INSTAGRAM_", "LINKEDIN_",
		"DROPBOX_", "ONEDRIVE_", "DRIVE_", "SHEETS_", "DOCS_",
		"CALENDAR_", "ZOOM_", "TEAMS_", "HUBSPOT_", "SALESFORCE_",
	}

	cleanName := name
	for _, prefix := range prefixes {
		if len(cleanName) > len(prefix) && cleanName[:len(prefix)] == prefix {
			cleanName = cleanName[len(prefix):]
			break
		}
	}

	// Also remove common suffixes that add noise
	suffixes := []string{
		"_FOR_THE_AUTHENTICATED_USER",
		"_FOR_AUTHENTICATED_USER",
		"_FOR_A_USER",
		"_FOR_USER",
	}
	for _, suffix := range suffixes {
		if len(cleanName) > len(suffix) && cleanName[len(cleanName)-len(suffix):] == suffix {
			cleanName = cleanName[:len(cleanName)-len(suffix)]
			break
		}
	}

	// Convert to title case
	return toTitleCase(cleanName)
}