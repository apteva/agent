package discovery

import (
	"log"

	"github.com/apteva/agent/config"
)

// ManualDiscovery uses the manually configured available_agents array (legacy mode)
type ManualDiscovery struct {
	cfg        *config.AgentsConfig
	running    bool
	cardProber *CardProber
}

// NewManualDiscovery creates a manual discovery service (uses available_agents from config)
func NewManualDiscovery(cfg *config.AgentsConfig) (*ManualDiscovery, error) {
	log.Printf("📋 Manual discovery mode: using %d configured agents", len(cfg.AvailableAgents))

	return &ManualDiscovery{
		cfg:        cfg,
		running:    true,
		cardProber: NewCardProber(),
	}, nil
}

// Start begins manual discovery (no-op, agents are already configured)
func (d *ManualDiscovery) Start() error {
	d.running = true
	log.Printf("📋 Manual discovery: %d agents configured", len(d.cfg.AvailableAgents))
	return nil
}

// Stop stops manual discovery
func (d *ManualDiscovery) Stop() error {
	d.running = false
	return nil
}

// GetAgents returns the manually configured agents
func (d *ManualDiscovery) GetAgents() []config.AgentInfo {
	// Filter to only enabled agents
	var enabled []config.AgentInfo
	for _, agent := range d.cfg.AvailableAgents {
		if agent.Enabled {
			enabled = append(enabled, agent)
		}
	}

	// Enrich A2A agents with Agent Card skills (cached)
	if d.cardProber != nil && len(enabled) > 0 {
		ptrs := make([]*config.AgentInfo, len(enabled))
		for i := range enabled {
			ptrs[i] = &enabled[i]
		}
		d.cardProber.EnrichAgents(ptrs)
	}

	return enabled
}

// IsRunning returns whether discovery is active
func (d *ManualDiscovery) IsRunning() bool {
	return d.running
}
