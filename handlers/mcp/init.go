package mcp

import (
	"log"
	"time"

	"agent-server/config"
	"agent-server/mcp"
)

// InitMCP initializes MCP connection and auto-refreshes cache if enabled
func InitMCP() {
	cfg := config.GetConfig()
	mcpConfig := cfg.Get().MCP

	if mcpConfig == nil || !mcpConfig.Enabled {
		log.Println("MCP not enabled, skipping initialization")
		return
	}

	log.Printf("🔌 Initializing MCP from %s...", mcpConfig.BaseURL)

	// Initialize external/standard MCP servers if configured
	if len(mcpConfig.Servers) > 0 {
		log.Printf("🔌 MCP: Initializing %d external servers...", len(mcpConfig.Servers))
		if err := mcp.InitializeExternalServers(mcpConfig.Servers); err != nil {
			log.Printf("⚠️  MCP External: Failed to initialize some servers: %v", err)
		}
		externalManager := mcp.GetExternalServerManager()
		externalTools := externalManager.GetTools()
		log.Printf("🔧 MCP External: Loaded %d tools from %d servers",
			len(externalTools), len(externalManager.GetServerNames()))
	}

	// Attempt to refresh MCP cache with retries
	// Re-read config on each retry in case it was updated via POST /config
	maxRetries := 3
	var err error
	for i := 0; i < maxRetries; i++ {
		// Get fresh config on each attempt (may have been updated via /config endpoint)
		currentConfig := config.GetConfig().Get().MCP
		if currentConfig == nil || !currentConfig.Enabled {
			log.Println("MCP disabled during initialization, stopping")
			return
		}
		err = mcp.RefreshMCPCache(currentConfig)
		if err == nil {
			mcpConfig = currentConfig // Update for periodic refresh
			break
		}
		if i < maxRetries-1 {
			log.Printf("⚠️  MCP refresh attempt %d/%d failed: %v (retrying...)", i+1, maxRetries, err)
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
	}

	if err != nil {
		log.Printf("❌ Failed to initialize MCP cache after %d attempts: %v", maxRetries, err)
		log.Printf("💡 MCP is enabled but not connected. You can:")
		log.Printf("   1. Check if MCP server is running at %s", mcpConfig.BaseURL)
		log.Printf("   2. Manually refresh via: POST /mcp/refresh")
		log.Printf("   3. Check status via: GET /mcp/status")
		// Continue anyway - MCP routes will still work for manual refresh
		go StartMCPPeriodicRefresh(mcpConfig)
		return
	}

	cache := mcp.GetMCPCache()
	servers := cache.GetServers()
	tools := cache.GetTools("")

	log.Printf("✅ MCP initialized: %d servers, %d tools loaded from %s", len(servers), len(tools), mcpConfig.BaseURL)

	// Optionally set up periodic refresh
	go StartMCPPeriodicRefresh(mcpConfig)
}

// StartMCPPeriodicRefresh starts a background goroutine that periodically refreshes MCP cache
func StartMCPPeriodicRefresh(mcpConfig *config.MCPConfig) {
	if mcpConfig.CacheTTL == "" {
		return // No periodic refresh if TTL not set
	}

	ttl, err := time.ParseDuration(mcpConfig.CacheTTL)
	if err != nil {
		log.Printf("Invalid MCP cache TTL: %v", err)
		return
	}

	ticker := time.NewTicker(ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Get fresh config on each tick (may have been updated via /config endpoint)
			currentConfig := config.GetConfig().Get().MCP
			if currentConfig == nil || !currentConfig.Enabled {
				log.Println("MCP disabled, stopping periodic refresh")
				return
			}
			log.Println("Refreshing MCP cache (periodic)")
			err := mcp.RefreshMCPCache(currentConfig)
			if err != nil {
				log.Printf("Failed to refresh MCP cache: %v", err)
			} else {
				cache := mcp.GetMCPCache()
				log.Printf("MCP cache refreshed: %d servers, %d tools",
					len(cache.GetServers()), len(cache.GetTools("")))
			}
		}
	}
}
