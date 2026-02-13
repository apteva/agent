package mcp

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/mcp"
)

// HandleMCPTools - GET /mcp/tools
func HandleMCPTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Optional query parameter to filter by server
	serverName := r.URL.Query().Get("server")

	externalManager := mcp.GetExternalServerManager()
	externalTools := externalManager.GetTools()

	// Convert external tools to MCPTool format
	var allTools []mcp.MCPTool
	for _, t := range externalTools {
		if serverName != "" && t.ServerName != serverName {
			continue
		}
		allTools = append(allTools, mcp.MCPTool{
			Name:        t.FullName,
			DisplayName: t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			ServerName:  t.ServerName,
		})
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tools":   allTools,
		"count":   len(allTools),
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

	status := map[string]interface{}{
		"enabled":     false,
		"tools_count": 0,
	}

	if mcpConfig != nil {
		status["enabled"] = mcpConfig.Enabled

		if mcpConfig.Enabled {
			enabledTools := mcp.GetEnabledMCPTools(mcpConfig)
			status["tools_count"] = len(enabledTools)
			status["enabled_tools"] = enabledTools
		}

		// External MCP servers info
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

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}
