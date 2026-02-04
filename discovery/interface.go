package discovery

import (
	"log"

	"github.com/apteva/agent/config"
)

// DiscoveryService handles automatic agent discovery
type DiscoveryService interface {
	// Start begins the discovery process
	Start() error

	// Stop stops the discovery service
	Stop() error

	// GetAgents returns currently discovered agents
	GetAgents() []config.AgentInfo

	// IsRunning returns whether discovery is active
	IsRunning() bool
}

// NewDiscoveryService creates the appropriate discovery service based on config
func NewDiscoveryService(cfg *config.AgentsConfig, agentID, agentName, agentDescription, agentURL string, features map[string]bool) (DiscoveryService, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	// Determine discovery method
	method := cfg.DiscoveryMethod

	// If no explicit method, determine based on config
	if method == "" {
		if cfg.GossipSeed != "" {
			// Gossip seed explicitly configured
			method = "gossip"
		} else if len(cfg.AvailableAgents) > 0 {
			// Manual agents list configured
			method = "manual"
		} else {
			// Default to file-based discovery (most reliable, works on localhost)
			method = "file"
		}
	}

	log.Printf("🔍 Discovery method: %s", method)

	switch method {
	case "file":
		return NewFileDiscovery(cfg, agentID, agentName, agentDescription, agentURL, features)
	case "mdns":
		if cfg.Group == "" {
			cfg.Group = "default" // Ensure group is set for mDNS
		}
		return NewMDNSDiscovery(cfg, agentID, agentName, agentURL, features)
	case "ssdp":
		if cfg.Group == "" {
			cfg.Group = "default"
		}
		return NewSSDPDiscovery(cfg, agentID, agentName, agentURL)
	case "gossip":
		return NewGossipDiscovery(cfg, agentID, agentName, agentURL)
	case "manual":
		return NewManualDiscovery(cfg)
	default:
		log.Printf("⚠️  Unknown discovery method '%s', falling back to file-based", method)
		return NewFileDiscovery(cfg, agentID, agentName, agentDescription, agentURL, features)
	}
}
