package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"
	"time"

	"agent-server/config"
	"agent-server/events"
	"agent-server/memory"
)

// ResourceSyncer manages syncing MCP resources to memory
type ResourceSyncer struct {
	mcpClient     *MCPClient
	memoryManager *memory.MemoryManager
	config        *config.MCPResourceConfig
	resources     []string // Resource URIs to sync (explicit opt-in)
	eventBus      *events.EventBus

	// Sync state
	checksums     map[string]string    // uri -> content hash
	subscriptions map[string]int       // uri -> serverID
	syncedAt      map[string]time.Time // uri -> last sync time
	mu            sync.RWMutex

	// Background sync
	stopChan chan struct{}
	running  bool
}

// SyncStats contains statistics about a sync operation
type SyncStats struct {
	TotalResources   int       `json:"total_resources"`
	NewResources     int       `json:"new_resources"`
	UpdatedResources int       `json:"updated_resources"`
	DeletedResources int       `json:"deleted_resources"`
	SkippedResources int       `json:"skipped_resources"`
	Errors           int       `json:"errors"`
	ErrorMessages    []string  `json:"error_messages,omitempty"`
	Duration         string    `json:"duration"`
	SyncedAt         time.Time `json:"synced_at"`
}

// SyncedResource represents a resource that has been synced
type SyncedResource struct {
	URI        string    `json:"uri"`
	Name       string    `json:"name"`
	ServerID   int       `json:"server_id"`
	ServerName string    `json:"server_name"`
	MimeType   string    `json:"mime_type"`
	Checksum   string    `json:"checksum"`
	ChunkCount int       `json:"chunk_count"`
	SyncedAt   time.Time `json:"synced_at"`
}

// NewResourceSyncer creates a new resource syncer
// resources: list of resource URIs to sync (e.g., ["notion://page/123", "github://repo/abc"])
// cfg: resource sync settings (auto_sync, interval, filters, etc.)
func NewResourceSyncer(mcpClient *MCPClient, memoryManager *memory.MemoryManager, resources []string, cfg *config.MCPResourceConfig, eventBus *events.EventBus) *ResourceSyncer {
	return &ResourceSyncer{
		mcpClient:     mcpClient,
		memoryManager: memoryManager,
		config:        cfg,
		resources:     resources,
		eventBus:      eventBus,
		checksums:     make(map[string]string),
		subscriptions: make(map[string]int),
		syncedAt:      make(map[string]time.Time),
		stopChan:      make(chan struct{}),
	}
}

// Start begins background sync if configured
func (s *ResourceSyncer) Start() error {
	log.Printf("🚀 MCP Resource Syncer: Starting...")
	log.Printf("🚀 MCP Resource Syncer: Config enabled=%v, %d resources configured",
		s.config != nil && s.config.Enabled, len(s.resources))

	if s.config == nil || !s.config.Enabled {
		log.Printf("🚀 MCP Resource Syncer: Disabled (config nil or not enabled)")
		return nil
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		log.Printf("🚀 MCP Resource Syncer: Already running")
		return nil
	}
	s.running = true
	s.mu.Unlock()

	// Load existing checksums from database to avoid re-embedding unchanged resources
	if s.memoryManager != nil {
		checksums, err := s.memoryManager.LoadMCPChecksums()
		if err != nil {
			log.Printf("⚠️ MCP Resource Syncer: Failed to load checksums from DB: %v", err)
		} else {
			s.mu.Lock()
			for uri, checksum := range checksums {
				s.checksums[uri] = checksum
			}
			s.mu.Unlock()
			log.Printf("🚀 MCP Resource Syncer: Loaded %d checksums from database", len(checksums))
		}
	}

	log.Printf("🚀 MCP Resource Syncer: auto_sync=%v, interval=%s, subscribe=%v",
		s.config.AutoSync, s.config.SyncInterval, s.config.Subscribe)

	// Initial sync if auto_sync enabled
	if s.config.AutoSync {
		log.Printf("🚀 MCP Resource Syncer: Starting initial sync (auto_sync=true)...")
		go func() {
			stats, err := s.SyncAll()
			if err != nil {
				log.Printf("⚠️ MCP Resource Syncer: Initial sync error: %v", err)
			} else {
				log.Printf("✅ MCP Resource Syncer: Initial sync complete - %d new, %d updated, %d errors",
					stats.NewResources, stats.UpdatedResources, stats.Errors)
			}
		}()
	} else {
		log.Printf("🚀 MCP Resource Syncer: Skipping initial sync (auto_sync=false)")
	}

	// Start periodic sync if interval configured
	if s.config.SyncInterval != "" {
		interval, err := time.ParseDuration(s.config.SyncInterval)
		if err != nil {
			log.Printf("⚠️ MCP Resource Syncer: Invalid sync interval '%s', using 6h", s.config.SyncInterval)
			interval = 6 * time.Hour
		}
		log.Printf("🚀 MCP Resource Syncer: Starting periodic sync every %s", interval)
		go s.periodicSync(interval)
	}

	return nil
}

// Stop stops background sync and cleans up subscriptions
func (s *ResourceSyncer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.running = false
	close(s.stopChan)

	// Unsubscribe from all resources
	for uri, serverID := range s.subscriptions {
		s.mcpClient.Unsubscribe(serverID, uri)
	}
	s.subscriptions = make(map[string]int)
}

// periodicSync runs sync at regular intervals
func (s *ResourceSyncer) periodicSync(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Printf("📚 MCP Resources: Running periodic sync...")
			stats, err := s.SyncAll()
			if err != nil {
				log.Printf("⚠️ MCP Resources: Periodic sync error: %v", err)
			} else {
				log.Printf("📚 MCP Resources: Periodic sync complete - %d new, %d updated, %d deleted",
					stats.NewResources, stats.UpdatedResources, stats.DeletedResources)
			}
		case <-s.stopChan:
			return
		}
	}
}

// SyncAll syncs all configured resource URIs
func (s *ResourceSyncer) SyncAll() (*SyncStats, error) {
	if s.memoryManager == nil {
		return nil, fmt.Errorf("memory manager not initialized")
	}

	startTime := time.Now()
	stats := &SyncStats{
		SyncedAt: startTime,
	}

	log.Printf("📚 MCP Resources Sync: Starting sync...")

	// Check if specific resources are configured (explicit opt-in required)
	if len(s.resources) == 0 {
		log.Printf("📚 MCP Resources Sync: No resources configured in mcp.resources - skipping sync")
		stats.Duration = time.Since(startTime).String()
		return stats, nil
	}

	log.Printf("📚 MCP Resources Sync: %d resources configured for sync:", len(s.resources))
	for i, uri := range s.resources {
		log.Printf("   %d. %s", i+1, uri)
	}

	// Get all available resources from cache
	cache := GetMCPCache()
	allServers := cache.GetServers()

	// Build server ID to name mapping
	serverNames := make(map[int]string)
	for _, server := range allServers {
		serverNames[server.ID] = server.Name
	}

	log.Printf("📚 MCP Resources Sync: %d servers available, %d have resources",
		len(allServers), len(cache.GetResourceServers()))

	// Track which URIs we've seen
	seenURIs := make(map[string]bool)

	// Sync only the configured resources
	for _, uri := range s.resources {
		log.Printf("📚 MCP Resources Sync: Looking for resource: %s", uri)

		// Find this resource in the cache
		var foundResource *MCPResource
		for _, serverName := range cache.GetResourceServers() {
			resources := cache.GetResources(serverName)
			for i := range resources {
				if resources[i].URI == uri {
					foundResource = &resources[i]
					log.Printf("📚 MCP Resources Sync: Found resource '%s' on server '%s' (id: %d)",
						foundResource.Name, serverName, foundResource.ServerID)
					break
				}
			}
			if foundResource != nil {
				break
			}
		}

		if foundResource == nil {
			errMsg := fmt.Sprintf("Resource not found in cache: %s", uri)
			log.Printf("⚠️ MCP Resources Sync: %s", errMsg)
			stats.Errors++
			stats.ErrorMessages = append(stats.ErrorMessages, errMsg)
			continue
		}

		seenURIs[uri] = true
		stats.TotalResources++

		// Sync the resource
		isNew, err := s.SyncResource(foundResource.ServerID, foundResource.ServerName, *foundResource)
		if err != nil {
			errMsg := fmt.Sprintf("Failed to sync %s: %v", uri, err)
			log.Printf("⚠️ MCP Resources Sync: %s", errMsg)
			stats.Errors++
			stats.ErrorMessages = append(stats.ErrorMessages, errMsg)
			continue
		}

		if isNew {
			stats.NewResources++
		} else {
			stats.UpdatedResources++
		}

		// Subscribe if enabled
		if s.config != nil && s.config.Subscribe {
			s.subscribeToResource(foundResource.ServerID, uri)
		}
	}

	// Delete memories for resources that are no longer in the config
	deleted, err := s.cleanupDeletedResources(seenURIs)
	if err != nil {
		log.Printf("⚠️ MCP Resources Sync: Cleanup error: %v", err)
	}
	stats.DeletedResources = deleted

	stats.Duration = time.Since(startTime).String()

	log.Printf("✅ MCP Resources Sync: Complete in %s - %d total, %d new, %d updated, %d deleted, %d errors",
		stats.Duration, stats.TotalResources, stats.NewResources, stats.UpdatedResources, stats.DeletedResources, stats.Errors)

	// Publish sync event
	if s.eventBus != nil {
		event := events.NewEvent(events.CategoryMemory, "mcp_resources_synced", events.LevelInfo).
			WithData("stats", stats)
		s.eventBus.Publish(event)
	}

	return stats, nil
}

// SyncServer syncs all resources from a specific server
func (s *ResourceSyncer) SyncServer(serverID int, serverName string) (*SyncStats, error) {
	if s.memoryManager == nil {
		return nil, fmt.Errorf("memory manager not initialized")
	}

	startTime := time.Now()
	stats := &SyncStats{
		SyncedAt: startTime,
	}

	resources, err := s.mcpClient.ListResources(serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to list resources: %w", err)
	}

	for _, resource := range resources {
		resource.ServerName = serverName

		if !s.matchesFilters(resource.URI) {
			stats.SkippedResources++
			continue
		}

		stats.TotalResources++

		isNew, err := s.SyncResource(serverID, serverName, resource)
		if err != nil {
			stats.Errors++
			continue
		}

		if isNew {
			stats.NewResources++
		} else {
			stats.UpdatedResources++
		}
	}

	stats.Duration = time.Since(startTime).String()
	return stats, nil
}

// SyncResource syncs a single resource to memory
// Returns true if this is a new resource, false if updated
func (s *ResourceSyncer) SyncResource(serverID int, serverName string, resource MCPResource) (bool, error) {
	log.Printf("🔄 MCP Sync: Starting sync for resource '%s' (uri: %s) from server '%s' (id: %d)",
		resource.Name, resource.URI, serverName, serverID)

	// Read resource content
	content, err := s.mcpClient.ReadResource(serverID, resource.URI)
	if err != nil {
		return false, fmt.Errorf("failed to read resource: %w", err)
	}

	// Get text content
	textContent := content.Text
	if textContent == "" && content.Blob != "" {
		// Skip binary content for now
		return false, fmt.Errorf("binary resources not supported yet")
	}

	if textContent == "" {
		return false, fmt.Errorf("empty content")
	}

	log.Printf("🔄 MCP Sync: Read %d bytes of content for '%s'", len(textContent), resource.Name)

	// Check size limit
	maxSize := 10485760 // 10MB default
	if s.config != nil && s.config.MaxSize > 0 {
		maxSize = s.config.MaxSize
	}
	if len(textContent) > maxSize {
		return false, fmt.Errorf("resource too large: %d bytes (max %d)", len(textContent), maxSize)
	}

	// Compute checksum
	hash := sha256.Sum256([]byte(textContent))
	checksum := hex.EncodeToString(hash[:])

	// Check if content changed (checksums loaded from DB on startup)
	s.mu.RLock()
	existingChecksum := s.checksums[resource.URI]
	s.mu.RUnlock()

	isNew := existingChecksum == ""
	if existingChecksum == checksum {
		// Content unchanged, skip entirely
		log.Printf("🔄 MCP Sync: Content unchanged for '%s' (checksum match), skipping", resource.Name)
		return false, nil
	}

	// Content changed or new - delete existing memories if any
	if !isNew {
		log.Printf("🔄 MCP Sync: Content changed for '%s', deleting old memories", resource.Name)
		deletedCount, err := s.memoryManager.DeleteMCPResource(resource.URI)
		if err != nil {
			log.Printf("⚠️ MCP Resources: Failed to delete old memories for %s: %v", resource.URI, err)
		} else if deletedCount > 0 {
			log.Printf("🔄 MCP Sync: Deleted %d existing memory chunks for '%s'", deletedCount, resource.Name)
		}
	}

	// Determine MIME type
	mimeType := content.MimeType
	if mimeType == "" {
		mimeType = resource.MimeType
	}
	if mimeType == "" {
		mimeType = guessMimeType(resource.URI, resource.Name)
	}

	log.Printf("🧠 MCP Sync: Ingesting '%s' into memory (mime: %s, size: %d bytes, new: %v)",
		resource.Name, mimeType, len(textContent), isNew)

	// Ingest into memory with checksum
	result, err := s.memoryManager.IngestMCPResource(
		serverID,
		serverName,
		resource.URI,
		resource.Name,
		textContent,
		mimeType,
		checksum, // Store checksum with memories for persistence
	)
	if err != nil {
		return false, fmt.Errorf("failed to ingest resource: %w", err)
	}

	// Update state
	s.mu.Lock()
	s.checksums[resource.URI] = checksum
	s.syncedAt[resource.URI] = time.Now()
	s.mu.Unlock()

	log.Printf("✅ MCP Sync: Created %d memory chunks for '%s' from server '%s'",
		result.ChunksCount, resource.Name, serverName)

	return isNew, nil
}

// HandleResourceUpdate handles a resource update notification
func (s *ResourceSyncer) HandleResourceUpdate(serverID int, uri string) error {
	s.mu.RLock()
	_, subscribed := s.subscriptions[uri]
	s.mu.RUnlock()

	if !subscribed {
		return nil
	}

	// Get server name from cache
	cache := GetMCPCache()
	cache.mu.RLock()
	server, exists := cache.Servers[serverID]
	cache.mu.RUnlock()

	serverName := ""
	if exists {
		serverName = server.Name
	}

	// Re-sync the resource
	resource := MCPResource{
		URI:        uri,
		Name:       path.Base(uri),
		ServerID:   serverID,
		ServerName: serverName,
	}

	_, err := s.SyncResource(serverID, serverName, resource)
	if err != nil {
		return fmt.Errorf("failed to re-sync resource %s: %w", uri, err)
	}

	log.Printf("📚 MCP Resources: Re-synced %s after update notification", uri)
	return nil
}

// GetSyncedResources returns list of synced resources
func (s *ResourceSyncer) GetSyncedResources() []SyncedResource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var resources []SyncedResource
	for uri, checksum := range s.checksums {
		syncedAt := s.syncedAt[uri]
		resources = append(resources, SyncedResource{
			URI:      uri,
			Name:     path.Base(uri),
			Checksum: checksum,
			SyncedAt: syncedAt,
		})
	}
	return resources
}

// GetSyncStatus returns current sync status
func (s *ResourceSyncer) GetSyncStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"enabled":            s.config != nil && s.config.Enabled,
		"running":            s.running,
		"synced_count":       len(s.checksums),
		"subscription_count": len(s.subscriptions),
		"config":             s.config,
	}
}

// matchesFilters checks if a URI matches the configured filters
func (s *ResourceSyncer) matchesFilters(uri string) bool {
	if len(s.config.Filters) == 0 {
		return true // No filters means accept all
	}

	for _, pattern := range s.config.Filters {
		if matchPattern(pattern, uri) {
			return true
		}
	}
	return false
}

// matchPattern checks if a URI matches a glob-like pattern
func matchPattern(pattern, uri string) bool {
	// Simple glob matching: * matches anything
	if pattern == "*" || pattern == "" {
		return true
	}

	// Handle prefix patterns like "notion://*"
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(uri, prefix)
	}

	// Handle suffix patterns like "*/docs/*"
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(uri, suffix) || strings.Contains(uri, suffix)
	}

	// Exact match
	return pattern == uri
}

// subscribeToResource subscribes to updates for a resource
func (s *ResourceSyncer) subscribeToResource(serverID int, uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subscriptions[uri]; exists {
		return // Already subscribed
	}

	if err := s.mcpClient.Subscribe(serverID, uri); err != nil {
		log.Printf("⚠️ MCP Resources: Failed to subscribe to %s: %v", uri, err)
		return
	}

	s.subscriptions[uri] = serverID
}

// cleanupDeletedResources removes memories for resources that no longer exist
func (s *ResourceSyncer) cleanupDeletedResources(currentURIs map[string]bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	toDelete := []string{}

	for uri := range s.checksums {
		if !currentURIs[uri] {
			toDelete = append(toDelete, uri)
		}
	}

	if len(toDelete) > 0 {
		log.Printf("🗑️ MCP Cleanup: Found %d resources to delete (removed from config)", len(toDelete))
	}

	for _, uri := range toDelete {
		log.Printf("🗑️ MCP Cleanup: Deleting memories for removed resource: %s", uri)
		count, err := s.memoryManager.DeleteMCPResource(uri)
		if err != nil {
			log.Printf("⚠️ MCP Cleanup: Failed to delete memories for %s: %v", uri, err)
			continue
		}

		delete(s.checksums, uri)
		delete(s.syncedAt, uri)

		// Unsubscribe if subscribed
		if serverID, exists := s.subscriptions[uri]; exists {
			s.mcpClient.Unsubscribe(serverID, uri)
			delete(s.subscriptions, uri)
		}

		log.Printf("✅ MCP Cleanup: Deleted %d memories for %s", count, uri)
		deleted++
	}

	return deleted, nil
}

// guessMimeType guesses MIME type from URI or name
func guessMimeType(uri, name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ext == "" {
		ext = strings.ToLower(path.Ext(uri))
	}

	switch ext {
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".html", ".htm":
		return "text/html"
	case ".xml":
		return "application/xml"
	case ".csv":
		return "text/csv"
	case ".yaml", ".yml":
		return "application/yaml"
	default:
		return "text/plain"
	}
}
