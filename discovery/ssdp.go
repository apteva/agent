package discovery

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/apteva/agent/config"

	"github.com/koron/go-ssdp"
)

// SSDPDiscovery implements agent discovery using SSDP (Simple Service Discovery Protocol)
type SSDPDiscovery struct {
	cfg       *config.AgentsConfig
	agentID   string
	agentName string
	agentURL  string
	group     string

	advertiser *ssdp.Advertiser
	registry   map[string]*config.AgentInfo
	mu         sync.RWMutex
	running    bool
	stopCh     chan struct{}
}

// NewSSDPDiscovery creates a new SSDP-based discovery service
func NewSSDPDiscovery(cfg *config.AgentsConfig, agentID, agentName, agentURL string) (*SSDPDiscovery, error) {
	if cfg.Group == "" {
		return nil, fmt.Errorf("group name required for SSDP discovery")
	}

	return &SSDPDiscovery{
		cfg:       cfg,
		agentID:   agentID,
		agentName: agentName,
		agentURL:  agentURL,
		group:     cfg.Group,
		registry:  make(map[string]*config.AgentInfo),
		stopCh:    make(chan struct{}),
	}, nil
}

// serviceType returns the SSDP service type for the group
func (d *SSDPDiscovery) serviceType() string {
	return fmt.Sprintf("urn:apteva:agent:%s:1", d.group)
}

// Start begins SSDP advertising and discovery
func (d *SSDPDiscovery) Start() error {
	if d.running {
		return fmt.Errorf("discovery already running")
	}

	// Recreate stopCh in case Stop() was called before (closed channel would cause immediate exit)
	d.stopCh = make(chan struct{})

	// Create metadata for SSDP location header
	metadata := AgentMetadata{
		ID:   d.agentID,
		Name: d.agentName,
		URL:  d.agentURL,
	}
	metaJSON, _ := json.Marshal(metadata)

	// Create SSDP advertiser
	ad, err := ssdp.Advertise(
		d.serviceType(),                              // Service type
		fmt.Sprintf("uuid:%s", d.agentID),            // USN (Unique Service Name)
		d.agentURL,                                   // Location (agent URL)
		"github.com/apteva/agent/1.0",                           // Server
		1800,                                         // Max-Age (30 minutes)
	)
	if err != nil {
		return fmt.Errorf("failed to create SSDP advertiser: %w", err)
	}
	d.advertiser = ad

	d.running = true
	log.Printf("🔍 SSDP discovery started for group '%s' (service: %s)", d.group, d.serviceType())
	log.Printf("   Advertising: %s", string(metaJSON))

	// Start continuous discovery in background
	go d.continuousDiscovery()

	// Start alive announcements
	go d.announceLoop()

	return nil
}

// announceLoop sends periodic alive announcements
func (d *SSDPDiscovery) announceLoop() {
	// Send initial alive
	if err := d.advertiser.Alive(); err != nil {
		log.Printf("⚠️  SSDP alive error: %v", err)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := d.advertiser.Alive(); err != nil {
				log.Printf("⚠️  SSDP alive error: %v", err)
			}
		case <-d.stopCh:
			return
		}
	}
}

// continuousDiscovery runs periodic SSDP searches to discover peers
func (d *SSDPDiscovery) continuousDiscovery() {
	log.Printf("🔍 SSDP: continuous discovery loop started")

	// Initial discovery (immediate)
	d.discoverPeers()

	// Quick follow-up after 3 seconds (catch agents that started at same time)
	time.Sleep(3 * time.Second)
	d.discoverPeers()

	// Get configurable refresh interval (default: 10s)
	refreshInterval := 10 * time.Second
	if intervalStr := config.GetAgentRefreshInterval(d.cfg); intervalStr != "" {
		if parsed, err := time.ParseDuration(intervalStr); err == nil {
			refreshInterval = parsed
		}
	}

	log.Printf("🔍 SSDP: periodic discovery every %s", refreshInterval)

	// Periodic re-discovery to catch new agents and remove stale ones
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	tickCount := 0
	for {
		select {
		case <-ticker.C:
			tickCount++
			log.Printf("🔍 SSDP: periodic discovery tick #%d", tickCount)
			d.discoverPeers()
		case <-d.stopCh:
			log.Printf("🔍 SSDP: continuous discovery loop stopped")
			return
		}
	}
}

// discoverPeers performs SSDP search to find peer agents
func (d *SSDPDiscovery) discoverPeers() {
	// Search for agents in our group
	services, err := ssdp.Search(d.serviceType(), 3, "")
	if err != nil {
		log.Printf("⚠️  SSDP search error: %v", err)
		return
	}

	log.Printf("🔍 SSDP raw search returned %d services", len(services))

	discovered := make(map[string]*config.AgentInfo)

	for _, srv := range services {
		// Extract agent ID from USN (format: uuid:{agentID}::serviceType)
		agentID := extractUUID(srv.USN)

		// Skip self
		if agentID == d.agentID {
			log.Printf("🔍 SSDP: found self (%s), skipping", agentID)
			continue
		}

		if agentID == "" {
			continue // Skip invalid
		}

		// The Location header contains the agent URL
		agentURL := srv.Location
		if agentURL == "" {
			continue
		}

		// Create agent info
		agent := &config.AgentInfo{
			ID:      agentID,
			Name:    agentID, // Use ID as name if not available
			URL:     agentURL,
			Enabled: true,
			Timeout: d.cfg.Settings.DefaultTimeout,
		}

		discovered[agentID] = agent
	}

	// Update registry with discovered agents
	d.mu.Lock()
	d.registry = discovered
	d.mu.Unlock()

	log.Printf("🔍 SSDP discovery: found %d agents in group '%s'", len(discovered), d.group)

	// Log discovered agents
	for _, agent := range discovered {
		log.Printf("  - %s at %s", agent.ID, agent.URL)
	}
}

// extractUUID extracts the UUID from a USN string
// USN format: uuid:{id}::serviceType or uuid:{id}
func extractUUID(usn string) string {
	if len(usn) < 5 {
		return ""
	}
	// Remove "uuid:" prefix
	if usn[:5] == "uuid:" {
		usn = usn[5:]
	}
	// Remove ::serviceType suffix if present
	for i, c := range usn {
		if c == ':' {
			return usn[:i]
		}
	}
	return usn
}

// Stop stops the SSDP discovery service
func (d *SSDPDiscovery) Stop() error {
	if !d.running {
		return nil
	}

	close(d.stopCh)

	if d.advertiser != nil {
		// Send bye announcement
		if err := d.advertiser.Bye(); err != nil {
			log.Printf("⚠️  SSDP bye error: %v", err)
		}
		if err := d.advertiser.Close(); err != nil {
			return fmt.Errorf("failed to close SSDP advertiser: %w", err)
		}
	}

	d.running = false
	log.Printf("🔍 SSDP discovery stopped for group '%s'", d.group)
	return nil
}

// GetAgents returns the list of currently discovered agents
func (d *SSDPDiscovery) GetAgents() []config.AgentInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	agents := make([]config.AgentInfo, 0, len(d.registry))
	for _, agent := range d.registry {
		agents = append(agents, *agent)
	}
	// Sort for deterministic ordering (prompt cache stability)
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].ID < agents[j].ID
	})
	return agents
}

// IsRunning returns whether discovery is active
func (d *SSDPDiscovery) IsRunning() bool {
	return d.running
}
