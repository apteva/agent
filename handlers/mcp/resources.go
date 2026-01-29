package mcp

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"agent-server/config"
	"agent-server/mcp"
)

// ResourceSyncerGetter is a function type for getting the resource syncer
type ResourceSyncerGetter func() *mcp.ResourceSyncer

var getResourceSyncer ResourceSyncerGetter

// SetResourceSyncerGetter sets the function to get the resource syncer
func SetResourceSyncerGetter(getter ResourceSyncerGetter) {
	getResourceSyncer = getter
}

// HandleResources handles GET /mcp/resources
func HandleResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	syncer := getResourceSyncer()
	if syncer == nil {
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"enabled":   false,
			"resources": []interface{}{},
			"message":   "MCP resource syncer not initialized",
		})
		return
	}

	resources := syncer.GetSyncedResources()
	status := syncer.GetSyncStatus()

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"enabled":   true,
		"resources": resources,
		"status":    status,
	})
}

// HandleResourcesSync handles POST /mcp/resources/sync
func HandleResourcesSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	syncer := getResourceSyncer()
	if syncer == nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "MCP resource syncer not initialized",
		})
		return
	}

	stats, err := syncer.SyncAll()
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"stats":   stats,
	})
}

// HandleResourcesStatus handles GET /mcp/resources/status
func HandleResourcesStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	syncer := getResourceSyncer()
	if syncer == nil {
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"enabled": false,
			"status":  nil,
		})
		return
	}

	status := syncer.GetSyncStatus()
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"enabled": true,
		"status":  status,
	})
}

// HandleServerResources handles GET /mcp/resources/:server_id
func HandleServerResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract server ID from path
	path := strings.TrimPrefix(r.URL.Path, "/mcp/resources/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	serverID, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	// Get MCP client from cache
	cache := mcp.GetMCPCache()
	servers := cache.GetServers()

	var serverName string
	for _, s := range servers {
		if s.ID == serverID {
			serverName = s.Name
			break
		}
	}

	if serverName == "" {
		sendJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false,
			"error":   "Server not found",
		})
		return
	}

	// List resources from this server
	cfg := config.GetConfig().Get().MCP
	mcpClient := mcp.NewMCPClient(cfg)
	resources, err := mcpClient.ListResources(serverID)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Set server name
	for i := range resources {
		resources[i].ServerName = serverName
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"server_id": serverID,
		"server":    serverName,
		"resources": resources,
	})
}

// HandleServerSync handles POST /mcp/resources/:server_id/sync
func HandleServerSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	syncer := getResourceSyncer()
	if syncer == nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "MCP resource syncer not initialized",
		})
		return
	}

	// Extract server ID from path
	path := strings.TrimPrefix(r.URL.Path, "/mcp/resources/")
	path = strings.TrimSuffix(path, "/sync")
	serverID, err := strconv.Atoi(path)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	// Get server name
	cache := mcp.GetMCPCache()
	servers := cache.GetServers()

	var serverName string
	for _, s := range servers {
		if s.ID == serverID {
			serverName = s.Name
			break
		}
	}

	if serverName == "" {
		sendJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false,
			"error":   "Server not found",
		})
		return
	}

	stats, err := syncer.SyncServer(serverID, serverName)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"server_id": serverID,
		"server":    serverName,
		"stats":     stats,
	})
}

// HandleAvailableResources handles GET /mcp/resources/available
// Returns all discovered resources from the cache (not just synced ones)
func HandleAvailableResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cache := mcp.GetMCPCache()

	// Get optional server filter from query
	serverFilter := r.URL.Query().Get("server")

	// Build response with resources grouped by server
	resourcesByServer := make(map[string]interface{})
	var totalCount int

	resourceServers := cache.GetResourceServers()

	if serverFilter != "" {
		// Get resources for specific server
		resources := cache.GetResources(serverFilter)
		resourcesByServer[serverFilter] = resources
		totalCount = len(resources)
	} else {
		// Get all resources grouped by server
		for _, serverName := range resourceServers {
			resources := cache.GetResources(serverName)
			resourcesByServer[serverName] = resources
			totalCount += len(resources)
		}
	}

	// Log for debugging
	log.Printf("📚 MCP /resources/available: %d servers with resources, %d total resources", len(resourceServers), totalCount)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"servers":   resourceServers,
		"resources": resourcesByServer,
		"total":     totalCount,
	})
}

// RegisterResourceRoutes registers MCP resource routes
func RegisterResourceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/resources", HandleResources)
	mux.HandleFunc("/mcp/resources/sync", HandleResourcesSync)
	mux.HandleFunc("/mcp/resources/status", HandleResourcesStatus)
	mux.HandleFunc("/mcp/resources/available", HandleAvailableResources)
	// Dynamic routes for specific servers handled by pattern matching
}
