package mcp

import (
	"log"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/mcp"
)

// InitMCP initializes MCP connection for external standard servers
func InitMCP() {
	cfg := config.GetConfig()
	mcpConfig := cfg.Get().MCP

	if mcpConfig == nil || !mcpConfig.Enabled {
		log.Println("MCP not enabled, skipping initialization")
		return
	}

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
	} else {
		log.Println("MCP enabled but no servers configured")
	}
}
