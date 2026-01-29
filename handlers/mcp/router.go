package mcp

import (
	"net/http"
)

// RegisterRoutes registers all MCP-related routes
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/servers", HandleMCPServers)
	mux.HandleFunc("/mcp/tools", HandleMCPTools)
	mux.HandleFunc("/mcp/refresh", HandleMCPRefresh)
	mux.HandleFunc("/mcp/status", HandleMCPStatus)
	mux.HandleFunc("/mcp/webhooks", HandleMCPWebhooks)

	// External MCP server routes (standard protocol servers)
	mux.HandleFunc("/mcp/external/servers", HandleMCPExternalServers)
	mux.HandleFunc("/mcp/external/tools", HandleMCPExternalTools)

	// Resource routes
	mux.HandleFunc("/mcp/resources", HandleResources)
	mux.HandleFunc("/mcp/resources/sync", HandleResourcesSync)
	mux.HandleFunc("/mcp/resources/status", HandleResourcesStatus)
	mux.HandleFunc("/mcp/resources/available", HandleAvailableResources)
}
