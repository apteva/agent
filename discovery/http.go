package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/apteva/agent/config"
)

// HTTPDiscovery implements agent discovery using direct HTTP polling
// This works in Docker and any environment without multicast support
type HTTPDiscovery struct {
	cfg       *config.AgentsConfig
	agentID   string
	agentName string
	agentURL  string
	group     string

	registry map[string]*config.AgentInfo
	mu       sync.RWMutex
	running  bool
	stopCh   chan struct{}
	client   *http.Client
}

// NewHTTPDiscovery creates a new HTTP-based discovery service
func NewHTTPDiscovery(cfg *config.AgentsConfig, agentID, agentName, agentURL string) (*HTTPDiscovery, error) {
	if cfg.Group == "" {
		return nil, fmt.Errorf("group name required for HTTP discovery")
	}

	return &HTTPDiscovery{
		cfg:       cfg,
		agentID:   agentID,
		agentName: agentName,
		agentURL:  agentURL,
		group:     cfg.Group,
		registry:  make(map[string]*config.AgentInfo),
		stopCh:    make(chan struct{}),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}, nil
}

// Start begins HTTP discovery
func (d *HTTPDiscovery) Start() error {
	if d.running {
		return fmt.Errorf("discovery already running")
	}

	// Recreate stopCh in case Stop() was called before (closed channel would cause immediate exit)
	d.stopCh = make(chan struct{})

	d.running = true
	log.Printf("🔍 HTTP discovery started for group '%s'", d.group)
	log.Printf("   This agent: %s (%s) at %s", d.agentName, d.agentID, d.agentURL)

	// Start continuous discovery in background
	go d.continuousDiscovery()

	return nil
}

// continuousDiscovery polls known agents to check if they're alive
func (d *HTTPDiscovery) continuousDiscovery() {
	log.Printf("🔍 HTTP: continuous discovery loop started")

	// Initial discovery
	d.discoverPeers()

	// Get configurable refresh interval (default: 10s)
	refreshInterval := 10 * time.Second
	if intervalStr := config.GetAgentRefreshInterval(d.cfg); intervalStr != "" {
		if parsed, err := time.ParseDuration(intervalStr); err == nil {
			refreshInterval = parsed
		}
	}

	log.Printf("🔍 HTTP: periodic discovery every %s", refreshInterval)

	// Periodic re-discovery
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	tickCount := 0
	for {
		select {
		case <-ticker.C:
			tickCount++
			log.Printf("🔍 HTTP: periodic discovery tick #%d", tickCount)
			d.discoverPeers()
		case <-d.stopCh:
			log.Printf("🔍 HTTP: continuous discovery loop stopped")
			return
		}
	}
}

// discoverPeers checks configured agents via HTTP
func (d *HTTPDiscovery) discoverPeers() {
	discovered := make(map[string]*config.AgentInfo)

	// Check each configured agent
	for _, agent := range d.cfg.AvailableAgents {
		if !agent.Enabled {
			continue
		}

		// Skip self
		if agent.ID == d.agentID || agent.URL == d.agentURL {
			continue
		}

		// Ping the agent's health endpoint
		if d.pingAgent(agent.URL) {
			discovered[agent.ID] = &config.AgentInfo{
				ID:           agent.ID,
				Name:         agent.Name,
				URL:          agent.URL,
				Capabilities: agent.Capabilities,
				Tags:         agent.Tags,
				Enabled:      true,
				Timeout:      d.cfg.Settings.DefaultTimeout,
			}
		}
	}

	// Update registry
	d.mu.Lock()
	d.registry = discovered
	d.mu.Unlock()

	if len(discovered) > 0 {
		log.Printf("🔍 HTTP discovery: found %d agents in group '%s'", len(discovered), d.group)
		for _, agent := range discovered {
			log.Printf("  - %s (%s) at %s", agent.Name, agent.ID, agent.URL)
		}
	}
}

// pingAgent checks if an agent is alive via HTTP
func (d *HTTPDiscovery) pingAgent(url string) bool {
	resp, err := d.client.Get(url + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// RegisterSelf announces this agent to a central registry (if configured)
func (d *HTTPDiscovery) RegisterSelf(registryURL string) error {
	data := map[string]interface{}{
		"id":    d.agentID,
		"name":  d.agentName,
		"url":   d.agentURL,
		"group": d.group,
	}

	body, _ := json.Marshal(data)
	resp, err := d.client.Post(registryURL+"/agents/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed: %s", string(body))
	}

	return nil
}

// Stop stops the HTTP discovery service
func (d *HTTPDiscovery) Stop() error {
	if !d.running {
		return nil
	}

	close(d.stopCh)
	d.running = false
	log.Printf("🔍 HTTP discovery stopped for group '%s'", d.group)
	return nil
}

// GetAgents returns the list of currently discovered agents
func (d *HTTPDiscovery) GetAgents() []config.AgentInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	agents := make([]config.AgentInfo, 0, len(d.registry))
	for _, agent := range d.registry {
		agents = append(agents, *agent)
	}
	return agents
}

// IsRunning returns whether discovery is active
func (d *HTTPDiscovery) IsRunning() bool {
	return d.running
}
