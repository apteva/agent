package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/mcp"
)

// HandleMCPServers - GET /mcp/servers
func HandleMCPServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cache := mcp.GetMCPCache()
	servers := cache.GetServers()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"servers":      servers,
		"count":        len(servers),
		"last_refresh": cache.LastRefresh,
	})
}

// HandleMCPTools - GET /mcp/tools
func HandleMCPTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Optional query parameter to filter by server
	serverName := r.URL.Query().Get("server")

	cache := mcp.GetMCPCache()
	tools := cache.GetTools(serverName)

	// Also include external MCP server tools
	externalManager := mcp.GetExternalServerManager()
	externalTools := externalManager.GetTools()

	// Convert external tools to the same format and append
	convertedExternal := mcp.ConvertExternalToolsToToolDefinitions(externalTools)

	// Filter external tools by server name if specified
	if serverName != "" {
		var filtered []mcp.MCPTool
		for _, t := range convertedExternal {
			if t.ServerName == serverName {
				filtered = append(filtered, t)
			}
		}
		convertedExternal = filtered
	}

	// Combine all tools
	allTools := append(tools, convertedExternal...)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"tools":        allTools,
		"count":        len(allTools),
		"last_refresh": cache.LastRefresh,
	})
}

// HandleMCPRefresh - POST /mcp/refresh
func HandleMCPRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.GetConfig()
	mcpConfig := cfg.Get().MCP

	if mcpConfig == nil || !mcpConfig.Enabled {
		http.Error(w, "MCP not enabled", http.StatusBadRequest)
		return
	}

	err := mcp.RefreshMCPCache(mcpConfig)
	if err != nil {
		log.Printf("Failed to refresh MCP cache: %v", err)
		http.Error(w, fmt.Sprintf("Failed to refresh MCP cache: %v", err), http.StatusInternalServerError)
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"message":   "MCP cache refreshed successfully",
		"timestamp": time.Now(),
	})
}

// HandleMCPStatus - GET /mcp/status
func HandleMCPStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.GetConfig()
	mcpConfig := cfg.Get().MCP
	cache := mcp.GetMCPCache()

	status := map[string]interface{}{
		"enabled":       false,
		"connected":     false,
		"servers_count": 0,
		"tools_count":   0,
	}

	if mcpConfig != nil {
		status["enabled"] = mcpConfig.Enabled
		status["base_url"] = mcpConfig.BaseURL
		status["timeout"] = mcpConfig.Timeout
		status["retry_count"] = mcpConfig.RetryCount
		status["cache_ttl"] = mcpConfig.CacheTTL

		if mcpConfig.Enabled {
			// Check connection
			if err := mcp.CheckMCPConnection(mcpConfig); err == nil {
				status["connected"] = true
			} else {
				status["connection_error"] = err.Error()
			}

			servers := cache.GetServers()
			tools := cache.GetTools("")

			status["servers_count"] = len(servers)
			status["tools_count"] = len(tools)
			status["last_refresh"] = cache.LastRefresh

			// Add available tools list
			availableTools := []string{}
			for _, tool := range tools {
				availableTools = append(availableTools, tool.Name)
			}
			status["available_tools"] = availableTools

			// Add enabled tools
			status["enabled_tools"] = mcp.GetEnabledMCPTools(mcpConfig)
		}

		// Add config info
		status["config"] = mcpConfig

		// Add external MCP servers info
		externalManager := mcp.GetExternalServerManager()
		externalTools := externalManager.GetTools()
		externalServerNames := externalManager.GetServerNames()
		status["external_servers"] = map[string]interface{}{
			"count":        len(externalServerNames),
			"server_names": externalServerNames,
			"tools_count":  len(externalTools),
		}
	}

	sendJSON(w, http.StatusOK, status)
}

// HandleMCPExternalServers - GET /mcp/external/servers
func HandleMCPExternalServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	externalManager := mcp.GetExternalServerManager()
	serverNames := externalManager.GetServerNames()

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"servers": serverNames,
		"count":   len(serverNames),
	})
}

// HandleMCPExternalTools - GET /mcp/external/tools
func HandleMCPExternalTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	externalManager := mcp.GetExternalServerManager()
	tools := externalManager.GetTools()

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tools":   tools,
		"count":   len(tools),
	})
}

// HandleMCPWebhooks - GET /mcp/webhooks
// Returns available webhooks from MCP servers, filtered by enabled webhook servers in config
func HandleMCPWebhooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.GetConfig()
	mcpConfig := cfg.Get().MCP

	if mcpConfig == nil || !mcpConfig.Enabled {
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"webhooks":        []interface{}{},
			"enabled_servers": []string{},
			"message":         "MCP not enabled",
		})
		return
	}

	// Get enabled webhook servers from config
	enabledServers := mcpConfig.Webhooks
	if enabledServers == nil {
		enabledServers = []string{}
	}

	// Create MCP client and fetch available webhooks
	client := mcp.NewMCPClient(mcpConfig)
	allWebhooks, err := client.ListWebhooks()
	if err != nil {
		log.Printf("Failed to fetch webhooks from MCP: %v", err)
		// Return empty list with error info
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"webhooks":        []interface{}{},
			"enabled_servers": enabledServers,
			"error":           err.Error(),
		})
		return
	}

	// Filter webhooks to only include enabled servers
	var filteredWebhooks []map[string]interface{}
	for _, wh := range allWebhooks {
		serverName, _ := wh["server"].(string)
		for _, enabled := range enabledServers {
			if serverName == enabled {
				filteredWebhooks = append(filteredWebhooks, wh)
				break
			}
		}
	}

	// Also return all available servers for the config UI
	var availableServers []string
	for _, wh := range allWebhooks {
		if serverName, ok := wh["server"].(string); ok {
			// Check if not already in list
			found := false
			for _, s := range availableServers {
				if s == serverName {
					found = true
					break
				}
			}
			if !found {
				availableServers = append(availableServers, serverName)
			}
		}
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"webhooks":          filteredWebhooks,
		"enabled_servers":   enabledServers,
		"available_servers": availableServers,
		"count":             len(filteredWebhooks),
	})
}

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}
