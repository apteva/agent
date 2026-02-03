package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apteva/agent/config"
)

func TestFileDiscovery_GetAgentsReturnsDiscoveredAgents(t *testing.T) {
	// Create a temporary registry directory
	tmpDir, err := os.MkdirTemp("", "apteva-agents-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create agent config
	cfg := &config.AgentsConfig{
		Enabled:          true,
		Mode:             "coordinator",
		Group:            "test-group",
		FileRegistryPath: tmpDir,
	}

	// Create the discovery service for "coordinator"
	coordDiscovery, err := NewFileDiscovery(cfg, "coordinator-id", "Coordinator", "http://localhost:4106", nil)
	if err != nil {
		t.Fatalf("Failed to create coordinator discovery: %v", err)
	}

	// Start coordinator discovery
	if err := coordDiscovery.Start(); err != nil {
		t.Fatalf("Failed to start coordinator discovery: %v", err)
	}
	defer coordDiscovery.Stop()

	// Verify coordinator registered itself
	coordFile := filepath.Join(tmpDir, "coordinator-id.json")
	if _, err := os.Stat(coordFile); os.IsNotExist(err) {
		t.Errorf("Coordinator did not create its registration file")
	}

	// Initially, GetAgents should return 0 (only self, which is excluded)
	agents := coordDiscovery.GetAgents()
	if len(agents) != 0 {
		t.Errorf("Expected 0 agents initially, got %d", len(agents))
	}

	// Now simulate another agent registering by creating its file
	workerEntry := FileAgentEntry{
		ID:        "worker-id",
		Name:      "Worker Agent",
		URL:       "http://localhost:4105",
		Group:     "test-group",
		PID:       os.Getpid(), // Use current PID so it passes alive check
		StartedAt: time.Now(),
	}
	workerData, _ := json.MarshalIndent(workerEntry, "", "  ")
	workerFile := filepath.Join(tmpDir, "worker-id.json")
	if err := os.WriteFile(workerFile, workerData, 0644); err != nil {
		t.Fatalf("Failed to write worker file: %v", err)
	}

	// Manually trigger discovery (simulating what the periodic tick does)
	coordDiscovery.discoverPeers()

	// Now GetAgents should return 1 agent
	agents = coordDiscovery.GetAgents()
	if len(agents) != 1 {
		t.Errorf("Expected 1 agent after discovery, got %d", len(agents))
	}

	if len(agents) > 0 {
		if agents[0].ID != "worker-id" {
			t.Errorf("Expected agent ID 'worker-id', got '%s'", agents[0].ID)
		}
		if agents[0].Name != "Worker Agent" {
			t.Errorf("Expected agent name 'Worker Agent', got '%s'", agents[0].Name)
		}
	}
}

func TestFileDiscovery_SameInstanceUsedForDiscoveryAndGetAgents(t *testing.T) {
	// This test verifies that the same instance is used for discovery and GetAgents
	tmpDir, err := os.MkdirTemp("", "apteva-agents-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.AgentsConfig{
		Enabled:          true,
		Mode:             "coordinator",
		Group:            "test-group",
		FileRegistryPath: tmpDir,
	}

	discovery, err := NewFileDiscovery(cfg, "test-agent", "Test Agent", "http://localhost:4000", nil)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	if err := discovery.Start(); err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}
	defer discovery.Stop()

	// Create a peer agent file
	peerEntry := FileAgentEntry{
		ID:        "peer-id",
		Name:      "Peer Agent",
		URL:       "http://localhost:4001",
		Group:     "test-group",
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	peerData, _ := json.MarshalIndent(peerEntry, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "peer-id.json"), peerData, 0644); err != nil {
		t.Fatalf("Failed to write peer file: %v", err)
	}

	// Run discovery
	discovery.discoverPeers()

	// Check registry directly
	discovery.mu.RLock()
	registryCount := len(discovery.registry)
	discovery.mu.RUnlock()

	// Get agents via GetAgents
	agents := discovery.GetAgents()

	// Both should match
	if registryCount != len(agents) {
		t.Errorf("Registry count (%d) doesn't match GetAgents count (%d)", registryCount, len(agents))
	}

	if registryCount != 1 {
		t.Errorf("Expected 1 agent in registry, got %d", registryCount)
	}
}

func TestFileDiscovery_WorkerModeDoesNotDiscover(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apteva-agents-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.AgentsConfig{
		Enabled:          true,
		Mode:             "worker", // Worker mode
		Group:            "test-group",
		FileRegistryPath: tmpDir,
	}

	workerDiscovery, err := NewFileDiscovery(cfg, "worker-id", "Worker", "http://localhost:4105", nil)
	if err != nil {
		t.Fatalf("Failed to create worker discovery: %v", err)
	}

	if err := workerDiscovery.Start(); err != nil {
		t.Fatalf("Failed to start worker discovery: %v", err)
	}
	defer workerDiscovery.Stop()

	// Worker should register itself
	workerFile := filepath.Join(tmpDir, "worker-id.json")
	if _, err := os.Stat(workerFile); os.IsNotExist(err) {
		t.Errorf("Worker did not create its registration file")
	}

	// Create another agent file
	otherEntry := FileAgentEntry{
		ID:        "other-id",
		Name:      "Other Agent",
		URL:       "http://localhost:4106",
		Group:     "test-group",
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	otherData, _ := json.MarshalIndent(otherEntry, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "other-id.json"), otherData, 0644); err != nil {
		t.Fatalf("Failed to write other file: %v", err)
	}

	// Worker should NOT discover other agents (GetAgents should be empty)
	// Note: In worker mode, discoverPeers is never called, so registry stays empty
	agents := workerDiscovery.GetAgents()
	if len(agents) != 0 {
		t.Errorf("Worker mode should not discover agents, but got %d", len(agents))
	}
}

func TestFileDiscovery_GroupFiltering(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apteva-agents-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.AgentsConfig{
		Enabled:          true,
		Mode:             "coordinator",
		Group:            "group-a",
		FileRegistryPath: tmpDir,
	}

	discovery, err := NewFileDiscovery(cfg, "agent-a", "Agent A", "http://localhost:4000", nil)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	if err := discovery.Start(); err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}
	defer discovery.Stop()

	// Create agent in same group
	sameGroupEntry := FileAgentEntry{
		ID:        "same-group",
		Name:      "Same Group Agent",
		URL:       "http://localhost:4001",
		Group:     "group-a",
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	sameData, _ := json.MarshalIndent(sameGroupEntry, "", "  ")
	os.WriteFile(filepath.Join(tmpDir, "same-group.json"), sameData, 0644)

	// Create agent in different group
	diffGroupEntry := FileAgentEntry{
		ID:        "diff-group",
		Name:      "Different Group Agent",
		URL:       "http://localhost:4002",
		Group:     "group-b",
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	diffData, _ := json.MarshalIndent(diffGroupEntry, "", "  ")
	os.WriteFile(filepath.Join(tmpDir, "diff-group.json"), diffData, 0644)

	// Run discovery
	discovery.discoverPeers()

	// Should only find the agent in the same group
	agents := discovery.GetAgents()
	if len(agents) != 1 {
		t.Errorf("Expected 1 agent (same group only), got %d", len(agents))
	}

	if len(agents) > 0 && agents[0].ID != "same-group" {
		t.Errorf("Expected 'same-group' agent, got '%s'", agents[0].ID)
	}
}

func TestFileDiscovery_ConcurrentAccessDuringDiscovery(t *testing.T) {
	// This test simulates the race condition: discovery running while GetAgents is called
	tmpDir, err := os.MkdirTemp("", "apteva-agents-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.AgentsConfig{
		Enabled:          true,
		Mode:             "coordinator",
		Group:            "test-group",
		FileRegistryPath: tmpDir,
		RefreshInterval:  "100ms", // Fast refresh for testing
	}

	discovery, err := NewFileDiscovery(cfg, "test-agent", "Test Agent", "http://localhost:4000", nil)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	// Create peer file BEFORE starting
	peerEntry := FileAgentEntry{
		ID:        "peer-id",
		Name:      "Peer Agent",
		URL:       "http://localhost:4001",
		Group:     "test-group",
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	peerData, _ := json.MarshalIndent(peerEntry, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "peer-id.json"), peerData, 0644); err != nil {
		t.Fatalf("Failed to write peer file: %v", err)
	}

	if err := discovery.Start(); err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}
	defer discovery.Stop()

	// Immediately after start, should find the agent
	agents := discovery.GetAgents()
	if len(agents) != 1 {
		t.Errorf("Expected 1 agent immediately after start, got %d", len(agents))
	}

	// Repeatedly call GetAgents while discovery is running
	// This simulates concurrent chat requests
	failures := 0
	for i := 0; i < 20; i++ {
		agents := discovery.GetAgents()
		if len(agents) != 1 {
			failures++
			t.Logf("Iteration %d: GetAgents returned %d agents (expected 1)", i, len(agents))
		}
		time.Sleep(50 * time.Millisecond)
	}

	if failures > 0 {
		t.Errorf("GetAgents returned wrong count %d times out of 20", failures)
	}
}

func TestFileDiscovery_ViaInterface(t *testing.T) {
	// This test simulates how main.go uses the discovery service via interface
	tmpDir, err := os.MkdirTemp("", "apteva-agents-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Pre-create a peer agent file
	peerEntry := FileAgentEntry{
		ID:        "peer-via-interface",
		Name:      "Peer Agent",
		URL:       "http://localhost:4001",
		Group:     "interface-test-group",
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	peerData, _ := json.MarshalIndent(peerEntry, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "peer-via-interface.json"), peerData, 0644); err != nil {
		t.Fatalf("Failed to write peer file: %v", err)
	}

	cfg := &config.AgentsConfig{
		Enabled:          true,
		Mode:             "coordinator",
		Group:            "interface-test-group",
		FileRegistryPath: tmpDir,
		DiscoveryMethod:  "file",
	}

	// Create via interface (like main.go does)
	var discoveryService DiscoveryService
	discoveryService, err = NewDiscoveryService(cfg, "interface-agent", "Interface Agent", "http://localhost:4000", nil)
	if err != nil {
		t.Fatalf("Failed to create discovery service: %v", err)
	}

	if discoveryService == nil {
		t.Fatal("Discovery service is nil")
	}

	t.Logf("discoveryService type: %T, pointer: %p", discoveryService, discoveryService)

	// Start via interface
	if err := discoveryService.Start(); err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}
	defer discoveryService.Stop()

	t.Logf("After Start - IsRunning: %v", discoveryService.IsRunning())

	// Get agents via interface - should find the pre-existing peer
	agents := discoveryService.GetAgents()
	t.Logf("GetAgents returned %d agents", len(agents))

	if len(agents) != 1 {
		t.Errorf("Expected 1 agent via interface, got %d", len(agents))
	}

	if len(agents) > 0 && agents[0].ID != "peer-via-interface" {
		t.Errorf("Expected 'peer-via-interface', got '%s'", agents[0].ID)
	}
}

func TestFileDiscovery_InitialDiscoveryRunsBeforeStartReturns(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apteva-agents-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Pre-create another agent file BEFORE starting discovery
	peerEntry := FileAgentEntry{
		ID:        "pre-existing",
		Name:      "Pre-existing Agent",
		URL:       "http://localhost:4001",
		Group:     "test-group",
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	peerData, _ := json.MarshalIndent(peerEntry, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "pre-existing.json"), peerData, 0644); err != nil {
		t.Fatalf("Failed to write peer file: %v", err)
	}

	cfg := &config.AgentsConfig{
		Enabled:          true,
		Mode:             "coordinator",
		Group:            "test-group",
		FileRegistryPath: tmpDir,
	}

	discovery, err := NewFileDiscovery(cfg, "new-agent", "New Agent", "http://localhost:4000", nil)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	// Start discovery - this should run initial discovery before returning
	if err := discovery.Start(); err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}
	defer discovery.Stop()

	// IMMEDIATELY after Start() returns, GetAgents should find the pre-existing agent
	// This tests that initial discovery runs synchronously in Start()
	agents := discovery.GetAgents()
	if len(agents) != 1 {
		t.Errorf("Expected 1 agent immediately after Start(), got %d (race condition?)", len(agents))
	}

	if len(agents) > 0 && agents[0].ID != "pre-existing" {
		t.Errorf("Expected 'pre-existing' agent, got '%s'", agents[0].ID)
	}
}
