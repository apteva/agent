package discovery

import (
	"agent-server/config"
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
func NewDiscoveryService(cfg *config.AgentsConfig, agentID, agentName, agentURL string, features map[string]bool) (DiscoveryService, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	// Determine discovery method based on config
	if cfg.GossipSeed != "" {
		// Gossip mode: GossipSeed is set
		return NewGossipDiscovery(cfg, agentID, agentName, agentURL)
	} else if cfg.Group != "" {
		// mDNS mode: Group is set but no GossipSeed
		return NewMDNSDiscovery(cfg, agentID, agentName, agentURL, features)
	} else if len(cfg.AvailableAgents) > 0 {
		// Manual mode: Available agents explicitly configured (legacy)
		return NewManualDiscovery(cfg)
	}

	// No discovery method configured
	return nil, nil
}
