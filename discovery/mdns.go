package discovery

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/apteva/agent/config"

	"github.com/hashicorp/mdns"
)

// mdnsLogFilter filters out noisy mdns library logs
type mdnsLogFilter struct {
	original io.Writer
}

func (f *mdnsLogFilter) Write(p []byte) (n int, err error) {
	s := string(p)
	// Filter out noisy mdns messages
	if strings.Contains(s, "[INFO] mdns:") {
		return len(p), nil // Discard but pretend we wrote it
	}
	return f.original.Write(p)
}

// getDockerInterface finds the docker0 bridge interface for mDNS binding
// This is needed on macOS/Linux where Docker runs in a VM or uses a bridge network
func getDockerInterface() *net.Interface {
	// Try common Docker bridge interface names
	interfaceNames := []string{"docker0", "br-", "podman"}

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		for _, name := range interfaceNames {
			if strings.HasPrefix(iface.Name, name) {
				log.Printf("🔍 mDNS: found Docker interface %s", iface.Name)
				return &iface
			}
		}
	}

	return nil
}

// getInterfaceIP gets the first IPv4 address of an interface
func getInterfaceIP(iface *net.Interface) net.IP {
	if iface == nil {
		return nil
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			if ipv4 := ipnet.IP.To4(); ipv4 != nil {
				return ipv4
			}
		}
	}

	return nil
}

// MDNSDiscovery implements agent discovery using mDNS (Multicast DNS)
type MDNSDiscovery struct {
	cfg       *config.AgentsConfig
	agentID   string
	agentName string
	agentURL  string
	group     string
	features  map[string]bool // Agent's enabled features

	server    *mdns.Server
	iface     *net.Interface // Network interface for mDNS (auto-detected or specified)
	registry  map[string]*config.AgentInfo
	mu        sync.RWMutex
	running   bool
	stopCh    chan struct{}
}

// AgentMetadata is broadcast via mDNS TXT records
type AgentMetadata struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	URL          string          `json:"url"`
	Capabilities []string        `json:"capabilities,omitempty"`
	Tags         []string        `json:"tags,omitempty"`
	Features     map[string]bool `json:"features,omitempty"`
}

// NewMDNSDiscovery creates a new mDNS-based discovery service
func NewMDNSDiscovery(cfg *config.AgentsConfig, agentID, agentName, agentURL string, features map[string]bool) (*MDNSDiscovery, error) {
	if cfg.Group == "" {
		return nil, fmt.Errorf("group name required for mDNS discovery")
	}

	// Auto-detect Docker bridge interface for container environments
	iface := getDockerInterface()
	if iface != nil {
		if ip := getInterfaceIP(iface); ip != nil {
			log.Printf("🔍 mDNS: binding to interface %s (%s)", iface.Name, ip.String())
		}
	}

	return &MDNSDiscovery{
		cfg:       cfg,
		agentID:   agentID,
		agentName: agentName,
		agentURL:  agentURL,
		group:     cfg.Group,
		features:  features,
		iface:     iface,
		registry:  make(map[string]*config.AgentInfo),
		stopCh:    make(chan struct{}),
	}, nil
}

// Start begins mDNS broadcasting and discovery
func (d *MDNSDiscovery) Start() error {
	if d.running {
		return fmt.Errorf("discovery already running")
	}

	// Create metadata for TXT records
	metadata := AgentMetadata{
		ID:       d.agentID,
		Name:     d.agentName,
		URL:      d.agentURL,
		Features: d.features,
	}
	metaJSON, _ := json.Marshal(metadata)

	// Extract IP and port from URL for mDNS service
	// URL format: http://192.168.0.154:4015
	port := 4015 // default
	var hostIP string
	if parts := splitURL(d.agentURL); len(parts) == 2 {
		hostIP = parts[0]
		fmt.Sscanf(parts[1], "%d", &port)
	}

	// Construct service type: _agent-{group}._tcp
	serviceType := fmt.Sprintf("_agent-%s._tcp", d.group)

	// Parse IP address explicitly for Docker compatibility
	var ips []net.IP
	if hostIP != "" {
		if ip := net.ParseIP(hostIP); ip != nil {
			ips = []net.IP{ip}
		}
	}

	// Build TXT records - each must be < 255 bytes (DNS limit)
	// Helper to safely add a TXT record, truncating if needed
	const maxTXTLen = 254 // Leave 1 byte margin
	safeTXT := func(record string) string {
		if len(record) > maxTXTLen {
			log.Printf("⚠️ mDNS: truncating TXT record from %d to %d bytes: %s...", len(record), maxTXTLen, record[:50])
			return record[:maxTXTLen]
		}
		return record
	}

	txtRecords := []string{
		safeTXT(fmt.Sprintf("url=%s", d.agentURL)),
		safeTXT(fmt.Sprintf("id=%s", d.agentID)),
		safeTXT(fmt.Sprintf("name=%s", d.agentName)),
	}

	// Add features as individual TXT records (more compact than JSON)
	// Format: f:feature_name=1 or f:feature_name=0
	for feature, enabled := range d.features {
		val := "0"
		if enabled {
			val = "1"
		}
		record := fmt.Sprintf("f:%s=%s", feature, val)
		// Skip features with names so long they'd be truncated badly
		if len(record) <= maxTXTLen {
			txtRecords = append(txtRecords, record)
		} else {
			log.Printf("⚠️ mDNS: skipping feature with too-long name: %s", feature[:50])
		}
	}

	// Only add metadata JSON if it fits in 255 bytes
	// This is for backwards compatibility with older agents
	if len(metaJSON) < 240 { // Leave room for "metadata=" prefix
		txtRecords = append(txtRecords, fmt.Sprintf("metadata=%s", string(metaJSON)))
	} else {
		log.Printf("⚠️ mDNS: metadata JSON too large (%d bytes), using individual TXT records", len(metaJSON))
	}

	// Create mDNS service
	service, err := mdns.NewMDNSService(
		d.agentID,         // Instance name
		serviceType,       // Service type
		"",                // Domain (local)
		"",                // Host name (auto)
		port,              // Port extracted from URL
		ips,               // IPs (explicit for Docker)
		txtRecords,
	)
	if err != nil {
		return fmt.Errorf("failed to create mDNS service: %w", err)
	}

	// Filter out noisy mdns library logs during server creation
	originalOutput := log.Writer()
	log.SetOutput(&mdnsLogFilter{original: originalOutput})
	d.server, err = mdns.NewServer(&mdns.Config{
		Zone:  service,
		Iface: d.iface, // Bind to Docker bridge interface if detected
	})
	log.SetOutput(originalOutput)

	if err != nil {
		return fmt.Errorf("failed to start mDNS server: %w", err)
	}

	d.running = true

	// In worker mode, only broadcast (no discovery of other agents)
	if d.cfg.IsWorkerMode() {
		log.Printf("🔍 mDNS worker mode: broadcasting on group '%s' (no discovery)", d.group)
		return nil
	}

	log.Printf("🔍 mDNS coordinator mode: broadcasting and discovering on group '%s'", d.group)

	// Start continuous discovery in background (coordinator mode only)
	go d.continuousDiscovery(serviceType)

	return nil
}

// splitURL splits URL into host:port parts
func splitURL(url string) []string {
	// Remove http:// or https://
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	// Split on :
	return strings.Split(url, ":")
}

// continuousDiscovery runs periodic mDNS lookups to discover peers
func (d *MDNSDiscovery) continuousDiscovery(serviceType string) {
	// Initial discovery (immediate)
	d.discoverPeers(serviceType)

	// Quick follow-up after 3 seconds (catch agents that started at same time)
	time.Sleep(3 * time.Second)
	d.discoverPeers(serviceType)

	// Get configurable refresh interval (default: 10s)
	refreshInterval := 10 * time.Second
	if intervalStr := config.GetAgentRefreshInterval(d.cfg); intervalStr != "" {
		if parsed, err := time.ParseDuration(intervalStr); err == nil {
			refreshInterval = parsed
		}
	}

	// Periodic re-discovery to catch new agents and remove stale ones
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.discoverPeers(serviceType)
		case <-d.stopCh:
			return
		}
	}
}

// discoverPeers performs mDNS lookup to find peer agents
func (d *MDNSDiscovery) discoverPeers(serviceType string) {
	entriesCh := make(chan *mdns.ServiceEntry, 100)

	// Create query params - use Docker interface if detected
	params := &mdns.QueryParam{
		Service:     serviceType,
		Domain:      "local",
		Timeout:     3 * time.Second,
		Entries:     entriesCh,
		Interface:   d.iface, // Use Docker bridge interface if detected
		DisableIPv6: true,    // Disable IPv6 to avoid errors in Docker
	}

	// Start lookup in background with filtered logging
	go func() {
		originalOutput := log.Writer()
		log.SetOutput(&mdnsLogFilter{original: originalOutput})
		err := mdns.Query(params)
		log.SetOutput(originalOutput)

		if err != nil {
			log.Printf("mDNS lookup error: %v", err)
		}
		close(entriesCh)
	}()

	discovered := make(map[string]*config.AgentInfo)

	// Collect discovered agents
	timeout := time.After(3 * time.Second)
	for {
		select {
		case entry, ok := <-entriesCh:
			if !ok {
				// Channel closed, we're done
				goto UPDATE_REGISTRY
			}

			// Skip self
			if entry.Name == d.agentID {
				continue
			}

			// Parse metadata from TXT records
			var agentURL, agentID, agentName string
			var metadata AgentMetadata
			features := make(map[string]bool)

			for _, info := range entry.InfoFields {
				if len(info) > 9 && info[:9] == "metadata=" {
					json.Unmarshal([]byte(info[9:]), &metadata)
				} else if len(info) > 4 && info[:4] == "url=" {
					agentURL = info[4:]
				} else if len(info) > 3 && info[:3] == "id=" {
					agentID = info[3:]
				} else if len(info) > 5 && info[:5] == "name=" {
					agentName = info[5:]
				} else if len(info) > 2 && info[:2] == "f:" {
					// Parse individual feature: f:feature_name=1
					featurePart := info[2:]
					if eqIdx := strings.Index(featurePart, "="); eqIdx > 0 {
						featureName := featurePart[:eqIdx]
						featureVal := featurePart[eqIdx+1:]
						features[featureName] = featureVal == "1"
					}
				}
			}

			// Use metadata if available, otherwise use TXT fields
			if metadata.ID != "" {
				agentID = metadata.ID
				agentName = metadata.Name
				agentURL = metadata.URL
			}

			if agentID == "" || agentURL == "" {
				log.Printf("Skipping agent with incomplete info: %s", entry.Name)
				continue
			}

			// Merge features: start with metadata, then overlay individual TXT records
			finalFeatures := metadata.Features
			if finalFeatures == nil {
				finalFeatures = make(map[string]bool)
			}
			for k, v := range features {
				finalFeatures[k] = v
			}

			// Create agent info
			agent := &config.AgentInfo{
				ID:           agentID,
				Name:         agentName,
				URL:          agentURL,
				Capabilities: metadata.Capabilities,
				Tags:         metadata.Tags,
				Enabled:      true,
				Timeout:      d.cfg.Settings.DefaultTimeout,
				Features:     finalFeatures,
			}

			discovered[agentID] = agent

		case <-timeout:
			// Discovery timeout, use what we have
			goto UPDATE_REGISTRY
		}
	}

UPDATE_REGISTRY:
	// Update registry with discovered agents
	d.mu.Lock()
	d.registry = discovered
	d.mu.Unlock()

	log.Printf("🔍 mDNS discovery: found %d agents in group '%s'", len(discovered), d.group)

	// Log discovered agents
	for _, agent := range discovered {
		log.Printf("  - %s (%s) at %s", agent.Name, agent.ID, agent.URL)
	}
}

// Stop stops the mDNS discovery service
func (d *MDNSDiscovery) Stop() error {
	if !d.running {
		return nil
	}

	close(d.stopCh)

	if d.server != nil {
		if err := d.server.Shutdown(); err != nil {
			return fmt.Errorf("failed to shutdown mDNS server: %w", err)
		}
	}

	d.running = false
	log.Printf("🔍 mDNS discovery stopped for group '%s'", d.group)
	return nil
}

// GetAgents returns the list of currently discovered agents
func (d *MDNSDiscovery) GetAgents() []config.AgentInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	agents := make([]config.AgentInfo, 0, len(d.registry))
	for _, agent := range d.registry {
		agents = append(agents, *agent)
	}
	return agents
}

// IsRunning returns whether discovery is active
func (d *MDNSDiscovery) IsRunning() bool {
	return d.running
}
