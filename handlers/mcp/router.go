package mcp

import (
	"net/http"
)

// RegisterRoutes registers all MCP-related routes
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/tools", HandleMCPTools)
	mux.HandleFunc("/mcp/status", HandleMCPStatus)

	// External MCP server routes (standard protocol servers)
	mux.HandleFunc("/mcp/external/servers", HandleMCPExternalServers)
	mux.HandleFunc("/mcp/external/tools", HandleMCPExternalTools)
}
