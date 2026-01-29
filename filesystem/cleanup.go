package filesystem

import (
	"context"
	"fmt"
	"log"
	"time"
)

// CleanupJob runs periodic cleanup tasks
type CleanupJob struct {
	fm       *FileManager
	interval time.Duration
	stopChan chan bool
}

// NewCleanupJob creates a new cleanup job
func NewCleanupJob(fm *FileManager, interval time.Duration) *CleanupJob {
	return &CleanupJob{
		fm:       fm,
		interval: interval,
		stopChan: make(chan bool),
	}
}

// Start begins the cleanup job
func (cj *CleanupJob) Start() {
	if !cj.fm.IsEnabled() || !cj.fm.config.AutoCleanup {
		return
	}

	go func() {
		// Wait 5 minutes before first cleanup to avoid race conditions on startup
		select {
		case <-time.After(5 * time.Minute):
			cj.runCleanup()
		case <-cj.stopChan:
			return
		}

		ticker := time.NewTicker(cj.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				cj.runCleanup()
			case <-cj.stopChan:
				return
			}
		}
	}()
}

// Stop stops the cleanup job
func (cj *CleanupJob) Stop() {
	close(cj.stopChan)
}

// runCleanup executes cleanup tasks
func (cj *CleanupJob) runCleanup() {
	ctx := context.Background()
	totalCleaned := 0

	// Cleanup expired files
	if cj.fm.config.RetentionDays > 0 {
		expired, err := cj.fm.CleanupExpired(ctx)
		if err == nil && expired > 0 {
			totalCleaned += expired
		}
	}

	// Cleanup orphaned files (currently disabled)
	if cj.fm.config.CleanupOrphans {
		orphans, err := cj.fm.CleanupOrphans(ctx)
		if err == nil && orphans > 0 {
			totalCleaned += orphans
		}
	}

	// Only log if something was cleaned
	if totalCleaned > 0 {
		log.Printf("🧹 Cleaned up %d expired files", totalCleaned)
	}
}

// formatBytes formats bytes into human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
