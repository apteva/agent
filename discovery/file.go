package discovery

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/apteva/agent/config"
)

// DefaultRegistryPath is the default location for file-based agent registry
const DefaultRegistryPath = "/tmp/apteva-agents"

// FileDiscovery implements agent discovery using filesystem
// This is the most reliable method for localhost/same-machine scenarios
type FileDiscovery struct {
	cfg          *config.AgentsConfig
	agentID      string
	agentName    string
	agentURL     string
	group        string
	features     map[string]bool
	capabilities []string

	registryPath string
	registry     map[string]*config.AgentInfo
	mu           sync.RWMutex
	running      bool
	stopCh       chan struct{}
}

// FileAgentEntry is the structure written to each agent's file
type FileAgentEntry struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	URL          string          `json:"url"`
	Group        string          `json:"group"`
	Features     map[string]bool `json:"features,omitempty"`
	Capabilities []string        `json:"capabilities,omitempty"`
	PID          int             `json:"pid"`
	StartedAt    time.Time       `json:"started_at"`
}

// NewFileDiscovery creates a new file-based discovery service
func NewFileDiscovery(cfg *config.AgentsConfig, agentID, agentName, agentURL string, features map[string]bool) (*FileDiscovery, error) {
	// Use configured path or default
	registryPath := DefaultRegistryPath
	if cfg.FileRegistryPath != "" {
		registryPath = cfg.FileRegistryPath
	}

	// Ensure registry directory exists
	if err := os.MkdirAll(registryPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create registry directory %s: %w", registryPath, err)
	}

	return &FileDiscovery{
		cfg:          cfg,
		agentID:      agentID,
		agentName:    agentName,
		agentURL:     agentURL,
		group:        cfg.Group,
		features:     features,
		registryPath: registryPath,
		registry:     make(map[string]*config.AgentInfo),
		stopCh:       make(chan struct{}),
	}, nil
}

// Start begins file-based discovery
func (d *FileDiscovery) Start() error {
	if d.running {
		return fmt.Errorf("discovery already running")
	}

	// Recreate stopCh in case Stop() was called before
	d.stopCh = make(chan struct{})

	// Register ourselves
	if err := d.registerSelf(); err != nil {
		return fmt.Errorf("failed to register self: %w", err)
	}

	d.running = true
	log.Printf("🔍 File discovery started (path: %s)", d.registryPath)
	log.Printf("   This agent: %s (%s) at %s", d.agentName, d.agentID, d.agentURL)
	log.Printf("🔍 DEBUG: FileDiscovery instance pointer=%p", d)

	// In worker mode, only register (no discovery of other agents)
	if d.cfg.IsWorkerMode() {
		log.Printf("🔍 File worker mode: registered but not discovering")
		return nil
	}

	// Run initial discovery IMMEDIATELY so agents are available right away
	// This prevents race conditions where chat requests come in before discovery runs
	d.discoverPeers()

	// Start continuous discovery in background for periodic updates
	go d.continuousDiscovery()

	return nil
}

// registerSelf writes this agent's info to the registry
func (d *FileDiscovery) registerSelf() error {
	entry := FileAgentEntry{
		ID:           d.agentID,
		Name:         d.agentName,
		URL:          d.agentURL,
		Group:        d.group,
		Features:     d.features,
		Capabilities: d.capabilities,
		PID:          os.Getpid(),
		StartedAt:    time.Now(),
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent entry: %w", err)
	}

	filePath := filepath.Join(d.registryPath, d.agentID+".json")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write registry file: %w", err)
	}

	log.Printf("🔍 File discovery: registered at %s", filePath)
	return nil
}

// unregisterSelf removes this agent's file from the registry
func (d *FileDiscovery) unregisterSelf() error {
	filePath := filepath.Join(d.registryPath, d.agentID+".json")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove registry file: %w", err)
	}
	log.Printf("🔍 File discovery: unregistered from %s", filePath)
	return nil
}

// continuousDiscovery periodically scans the registry directory
func (d *FileDiscovery) continuousDiscovery() {
	log.Printf("🔍 File: continuous discovery loop started")

	// Note: Initial discovery is now done in Start() before this goroutine starts
	// This ensures agents are available immediately when Start() returns

	// Get configurable refresh interval (default: 5s for file-based, faster than mDNS)
	refreshInterval := 5 * time.Second
	if intervalStr := config.GetAgentRefreshInterval(d.cfg); intervalStr != "" {
		if parsed, err := time.ParseDuration(intervalStr); err == nil {
			refreshInterval = parsed
		}
	}

	log.Printf("🔍 File: periodic discovery every %s", refreshInterval)

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	tickCount := 0
	for {
		select {
		case <-ticker.C:
			tickCount++
			log.Printf("🔍 File: periodic discovery tick #%d", tickCount)
			d.discoverPeers()
		case <-d.stopCh:
			log.Printf("🔍 File: continuous discovery loop stopped")
			return
		}
	}
}

// discoverPeers reads all agent files from the registry directory
func (d *FileDiscovery) discoverPeers() {
	files, err := filepath.Glob(filepath.Join(d.registryPath, "*.json"))
	if err != nil {
		log.Printf("🔍 File discovery: error reading registry: %v", err)
		return
	}

	discovered := make(map[string]*config.AgentInfo)

	for _, filePath := range files {
		// Read and parse the file
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("🔍 File discovery: error reading %s: %v", filePath, err)
			continue
		}

		var entry FileAgentEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			log.Printf("🔍 File discovery: error parsing %s: %v", filePath, err)
			continue
		}

		// Skip self
		if entry.ID == d.agentID {
			continue
		}

		// Skip agents in different groups (if group is set)
		if d.group != "" && entry.Group != "" && entry.Group != d.group {
			continue
		}

		// Check if the process is still alive (cleanup stale entries)
		if !d.isProcessAlive(entry.PID) {
			log.Printf("🔍 File discovery: removing stale entry %s (PID %d not running)", entry.ID, entry.PID)
			os.Remove(filePath)
			continue
		}

		// Add to discovered
		agent := &config.AgentInfo{
			ID:           entry.ID,
			Name:         entry.Name,
			URL:          entry.URL,
			Features:     entry.Features,
			Capabilities: entry.Capabilities,
			Enabled:      true,
			Timeout:      d.cfg.Settings.DefaultTimeout,
		}
		discovered[entry.ID] = agent
	}

	// Update registry
	d.mu.Lock()
	oldCount := len(d.registry)
	d.registry = discovered
	newCount := len(d.registry)
	d.mu.Unlock()

	log.Printf("🔍 File discovery: found %d agents (registry: %d -> %d)", len(discovered), oldCount, newCount)
	for _, agent := range discovered {
		log.Printf("   - %s (%s) at %s", agent.Name, agent.ID, agent.URL)
	}
	log.Printf("🔍 DEBUG: registry pointer=%p, running=%v, agentID=%s", d, d.running, d.agentID)
}

// isProcessAlive checks if a process with the given PID is still running
func (d *FileDiscovery) isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// On Unix, sending signal 0 checks if process exists without actually sending a signal
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Signal 0 doesn't send anything, just checks if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// Stop stops file-based discovery and unregisters this agent
func (d *FileDiscovery) Stop() error {
	if !d.running {
		return nil
	}

	close(d.stopCh)
	d.running = false

	// Unregister self
	if err := d.unregisterSelf(); err != nil {
		log.Printf("🔍 File discovery: warning during unregister: %v", err)
	}

	log.Printf("🔍 File discovery stopped")
	return nil
}

// GetAgents returns the list of currently discovered agents
func (d *FileDiscovery) GetAgents() []config.AgentInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	log.Printf("🔍 DEBUG GetAgents called: registry has %d agents, running=%v", len(d.registry), d.running)

	agents := make([]config.AgentInfo, 0, len(d.registry))
	for id, agent := range d.registry {
		log.Printf("🔍 DEBUG GetAgents returning: %s (%s) at %s", agent.Name, id, agent.URL)
		agents = append(agents, *agent)
	}

	log.Printf("🔍 DEBUG GetAgents returning %d agents total", len(agents))
	return agents
}

// IsRunning returns whether discovery is active
func (d *FileDiscovery) IsRunning() bool {
	return d.running
}
