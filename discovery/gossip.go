package discovery

import (
	"fmt"
	"log"

	"github.com/apteva/agent/config"
)

// GossipDiscovery implements agent discovery using gossip protocol (memberlist)
// This is a stub for future implementation
type GossipDiscovery struct {
	cfg       *config.AgentsConfig
	agentID   string
	agentName string
	agentURL  string
	running   bool
}

// NewGossipDiscovery creates a new gossip-based discovery service
func NewGossipDiscovery(cfg *config.AgentsConfig, agentID, agentName, agentURL string) (*GossipDiscovery, error) {
	if cfg.GossipSeed == "" {
		return nil, fmt.Errorf("gossip_seed required for gossip discovery")
	}

	log.Printf("📡 Gossip discovery initialized (STUB - not yet implemented)")
	log.Printf("   Seed: %s", cfg.GossipSeed)
	log.Printf("   Group: %s", cfg.Group)
	log.Printf("   To enable gossip, implement discovery/gossip.go with hashicorp/memberlist")

	return &GossipDiscovery{
		cfg:       cfg,
		agentID:   agentID,
		agentName: agentName,
		agentURL:  agentURL,
	}, nil
}

// Start begins gossip discovery (stub)
func (d *GossipDiscovery) Start() error {
	log.Printf("⚠️  Gossip discovery not yet implemented")
	log.Printf("   Falling back to manual mode")
	log.Printf("   To implement: see AGENT-DISCOVERY-PROPOSALS.md")

	d.running = false
	return fmt.Errorf("gossip discovery not yet implemented - use mDNS (remove gossip_seed) or manual mode")
}

// Stop stops gossip discovery
func (d *GossipDiscovery) Stop() error {
	d.running = false
	return nil
}

// GetAgents returns discovered agents (stub returns empty)
func (d *GossipDiscovery) GetAgents() []config.AgentInfo {
	return []config.AgentInfo{}
}

// IsRunning returns whether discovery is active
func (d *GossipDiscovery) IsRunning() bool {
	return d.running
}

// TODO: Full gossip implementation
// When ready to implement:
// 1. Import "github.com/hashicorp/memberlist"
// 2. Create memberlist config with group-based cluster name
// 3. Join cluster via GossipSeed
// 4. Parse member metadata to build agent registry
// 5. Handle member join/leave events
// 6. Periodic sync to update registry
