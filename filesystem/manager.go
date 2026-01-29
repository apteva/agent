package filesystem

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"agent-server/config"
	"agent-server/events"
)

// File represents a stored file
type File struct {
	ID         string                 `json:"id"`
	ThreadID   string                 `json:"thread_id,omitempty"`
	MessageID  string                 `json:"message_id,omitempty"`
	Filename   string                 `json:"filename"`
	MimeType   string                 `json:"mime_type"`
	FileType   string                 `json:"file_type"`
	SizeBytes  int64                  `json:"size_bytes"`
	Checksum   string                 `json:"checksum"`
	Data       []byte                 `json:"-"` // Exclude from JSON
	Width      *int                   `json:"width,omitempty"`
	Height     *int                   `json:"height,omitempty"`
	Duration   *int                   `json:"duration,omitempty"`
	Metadata   map[string]interface{} `json:"metadata"`
	Source     string                 `json:"source"`
	SourceTool string                 `json:"source_tool,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	ExpiresAt  *time.Time             `json:"expires_at,omitempty"`
}

// StorageStats represents storage statistics
type StorageStats struct {
	TotalFiles     int                    `json:"total_files"`
	TotalSizeBytes int64                  `json:"total_size_bytes"`
	FilesByType    map[string]int         `json:"files_by_type"`
	BytesByType    map[string]int64       `json:"bytes_by_type"`
	OldestFile     *time.Time             `json:"oldest_file,omitempty"`
	NewestFile     *time.Time             `json:"newest_file,omitempty"`
}

// ThreadStorageStats represents per-thread storage statistics
type ThreadStorageStats struct {
	ThreadID       string         `json:"thread_id"`
	TotalFiles     int            `json:"total_files"`
	TotalSizeBytes int64          `json:"total_size_bytes"`
	FilesByType    map[string]int `json:"files_by_type"`
	BytesByType    map[string]int64 `json:"bytes_by_type"`
}

// GlobalStorageStats represents storage statistics with per-thread breakdown
type GlobalStorageStats struct {
	TotalFiles     int                          `json:"total_files"`
	TotalSizeBytes int64                        `json:"total_size_bytes"`
	FilesByType    map[string]int               `json:"files_by_type"`
	BytesByType    map[string]int64             `json:"bytes_by_type"`
	ByThread       map[string]*ThreadStorageStats `json:"by_thread"`
	OldestFile     *time.Time                   `json:"oldest_file,omitempty"`
	NewestFile     *time.Time                   `json:"newest_file,omitempty"`
}

// IngestionCallback is called after a file is stored to trigger memory ingestion
type IngestionCallback func(fileID, fileName, mimeType string, data []byte)

// DeletionCallback is called before a file is deleted to clean up associated data (e.g., memories)
type DeletionCallback func(fileID string)

// FileManager handles file storage operations
type FileManager struct {
	db                *sql.DB
	config            *config.FileSystemConfig
	ingestionCallback IngestionCallback
	deletionCallback  DeletionCallback
}

// NewFileManager creates a new FileManager instance
func NewFileManager(db *sql.DB, cfg *config.FileSystemConfig) *FileManager {
	return &FileManager{
		db:     db,
		config: cfg,
	}
}

// SetIngestionCallback sets the callback for memory ingestion after file storage
func (fm *FileManager) SetIngestionCallback(callback IngestionCallback) {
	fm.ingestionCallback = callback
}

// SetDeletionCallback sets the callback for cleanup when a file is deleted
func (fm *FileManager) SetDeletionCallback(callback DeletionCallback) {
	fm.deletionCallback = callback
}

// IsEnabled returns whether the filesystem is enabled
func (fm *FileManager) IsEnabled() bool {
	return fm.config != nil && fm.config.Enabled
}

// Store saves a file and returns its ID
func (fm *FileManager) Store(ctx context.Context, file *File) (string, error) {
	if !fm.IsEnabled() {
		return "", fmt.Errorf("filesystem is disabled")
	}

	// Check if file type is allowed
	if !fm.isAllowedType(file.FileType) {
		return "", fmt.Errorf("file type '%s' not allowed", file.FileType)
	}

	// Validate file size
	if file.SizeBytes > fm.config.MaxFileSize {
		return "", fmt.Errorf("file size %d exceeds maximum %d", file.SizeBytes, fm.config.MaxFileSize)
	}

	// Check total storage limit
	stats, err := fm.GetStats(ctx)
	if err == nil && stats.TotalSizeBytes+file.SizeBytes > fm.config.MaxTotalSize {
		return "", fmt.Errorf("storage limit exceeded: current %d + new %d > max %d",
			stats.TotalSizeBytes, file.SizeBytes, fm.config.MaxTotalSize)
	}

	// Calculate checksum if not provided
	if file.Checksum == "" {
		hash := sha256.Sum256(file.Data)
		file.Checksum = hex.EncodeToString(hash[:])
	}

	// Check for deduplication
	if fm.config.Deduplication {
		existing, err := fm.FindByChecksum(ctx, file.Checksum)
		if err == nil && existing != nil {
			log.Printf("📎 File deduplicated: reusing existing file %s (checksum: %s)", existing.ID, file.Checksum[:8])
			return existing.ID, nil
		}
	}

	// Generate ID if not provided
	if file.ID == "" {
		file.ID = generateID()
	}

	// Set expiration based on retention policy (only if not already set)
	if fm.config.RetentionDays > 0 && file.ExpiresAt == nil {
		expiresAt := time.Now().AddDate(0, 0, fm.config.RetentionDays)
		file.ExpiresAt = &expiresAt
	}

	// Set creation time
	file.CreatedAt = time.Now()

	// Serialize metadata
	metadataJSON, err := json.Marshal(file.Metadata)
	if err != nil {
		metadataJSON = []byte("{}")
	}

	// Insert into database
	query := `
		INSERT INTO files (
			id, thread_id, message_id, filename, mime_type, file_type,
			size_bytes, checksum, data, width, height, duration, metadata,
			source, source_tool, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = fm.db.ExecContext(ctx, query,
		file.ID, file.ThreadID, file.MessageID, file.Filename, file.MimeType, file.FileType,
		file.SizeBytes, file.Checksum, file.Data, file.Width, file.Height, file.Duration,
		string(metadataJSON), file.Source, file.SourceTool, file.CreatedAt, file.ExpiresAt,
	)

	if err != nil {
		return "", fmt.Errorf("failed to store file: %w", err)
	}

	log.Printf("📁 Stored file: %s (%s, %d bytes, checksum: %s)", file.ID, file.FileType, file.SizeBytes, file.Checksum[:8])

	// Emit file_uploaded event with trace context from the request
	uploadEvent := events.NewEvent(events.CategoryFile, events.TypeFileUploaded, events.LevelInfo).
		WithData("file_id", file.ID).
		WithData("filename", file.Filename).
		WithData("mime_type", file.MimeType).
		WithData("file_type", file.FileType).
		WithData("size_bytes", file.SizeBytes).
		WithData("source", file.Source).
		WithData("checksum", file.Checksum)
	if file.ThreadID != "" {
		uploadEvent = uploadEvent.WithThread(file.ThreadID)
	}
	events.GetEventBus().PublishWithContext(ctx, uploadEvent)

	// Trigger memory ingestion callback if set (runs async)
	if fm.ingestionCallback != nil {
		go fm.ingestionCallback(file.ID, file.Filename, file.MimeType, file.Data)
	}

	return file.ID, nil
}

// Get retrieves a file by ID
func (fm *FileManager) Get(ctx context.Context, fileID string) (*File, error) {
	if !fm.IsEnabled() {
		return nil, fmt.Errorf("filesystem is disabled")
	}

	query := `
		SELECT id, thread_id, message_id, filename, mime_type, file_type,
		       size_bytes, checksum, data, width, height, duration, metadata,
		       source, source_tool, created_at, expires_at, access_count, last_accessed
		FROM files WHERE id = ?
	`

	var file File
	var threadID, messageID, sourceToolStr sql.NullString
	var width, height, duration, accessCount sql.NullInt64
	var metadataJSON string
	var lastAccessed sql.NullTime
	var expiresAt sql.NullTime

	err := fm.db.QueryRowContext(ctx, query, fileID).Scan(
		&file.ID, &threadID, &messageID, &file.Filename, &file.MimeType, &file.FileType,
		&file.SizeBytes, &file.Checksum, &file.Data, &width, &height, &duration,
		&metadataJSON, &file.Source, &sourceToolStr, &file.CreatedAt, &expiresAt,
		&accessCount, &lastAccessed,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("file not found: %s", fileID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	// Handle nullable fields
	if threadID.Valid {
		file.ThreadID = threadID.String
	}
	if messageID.Valid {
		file.MessageID = messageID.String
	}
	if sourceToolStr.Valid {
		file.SourceTool = sourceToolStr.String
	}
	if width.Valid {
		w := int(width.Int64)
		file.Width = &w
	}
	if height.Valid {
		h := int(height.Int64)
		file.Height = &h
	}
	if duration.Valid {
		d := int(duration.Int64)
		file.Duration = &d
	}
	if expiresAt.Valid {
		file.ExpiresAt = &expiresAt.Time
	}

	// Parse metadata
	if err := json.Unmarshal([]byte(metadataJSON), &file.Metadata); err != nil {
		file.Metadata = make(map[string]interface{})
	}

	// Update access tracking
	go fm.updateAccessTracking(fileID)

	return &file, nil
}

// GetAsBase64 retrieves a file and returns it as base64 encoded string
func (fm *FileManager) GetAsBase64(ctx context.Context, fileID string) (string, error) {
	file, err := fm.Get(ctx, fileID)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(file.Data), nil
}

// Delete removes a file by ID
func (fm *FileManager) Delete(ctx context.Context, fileID string) error {
	if !fm.IsEnabled() {
		return fmt.Errorf("filesystem is disabled")
	}

	// Get file info before deletion for event data
	var threadID sql.NullString
	var sizeBytes int64
	var filename string
	err := fm.db.QueryRowContext(ctx, "SELECT thread_id, size_bytes, filename FROM files WHERE id = ?", fileID).
		Scan(&threadID, &sizeBytes, &filename)
	if err == sql.ErrNoRows {
		return fmt.Errorf("file not found: %s", fileID)
	}
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// Call deletion callback first to clean up associated data (e.g., memories)
	if fm.deletionCallback != nil {
		fm.deletionCallback(fileID)
	}

	query := `DELETE FROM files WHERE id = ?`
	_, err = fm.db.ExecContext(ctx, query, fileID)
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	log.Printf("🗑️  Deleted file: %s", fileID)

	// Emit file_deleted event with trace context
	deleteEvent := events.NewEvent(events.CategoryFile, events.TypeFileDeleted, events.LevelInfo).
		WithData("file_id", fileID).
		WithData("filename", filename).
		WithData("size_bytes", sizeBytes).
		WithData("reason", "user_request")
	if threadID.Valid {
		deleteEvent = deleteEvent.WithThread(threadID.String)
	}
	events.GetEventBus().PublishWithContext(ctx, deleteEvent)

	return nil
}

// FindByChecksum finds a file by its checksum (for deduplication)
func (fm *FileManager) FindByChecksum(ctx context.Context, checksum string) (*File, error) {
	if !fm.IsEnabled() {
		return nil, fmt.Errorf("filesystem is disabled")
	}

	query := `
		SELECT id, thread_id, message_id, filename, mime_type, file_type,
		       size_bytes, checksum, width, height, duration, metadata,
		       source, source_tool, created_at, expires_at
		FROM files WHERE checksum = ? LIMIT 1
	`

	var file File
	var threadID, messageID, sourceToolStr sql.NullString
	var width, height, duration sql.NullInt64
	var metadataJSON string
	var expiresAt sql.NullTime

	err := fm.db.QueryRowContext(ctx, query, checksum).Scan(
		&file.ID, &threadID, &messageID, &file.Filename, &file.MimeType, &file.FileType,
		&file.SizeBytes, &file.Checksum, &width, &height, &duration,
		&metadataJSON, &file.Source, &sourceToolStr, &file.CreatedAt, &expiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found, but not an error
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find file by checksum: %w", err)
	}

	// Handle nullable fields
	if threadID.Valid {
		file.ThreadID = threadID.String
	}
	if messageID.Valid {
		file.MessageID = messageID.String
	}
	if sourceToolStr.Valid {
		file.SourceTool = sourceToolStr.String
	}
	if width.Valid {
		w := int(width.Int64)
		file.Width = &w
	}
	if height.Valid {
		h := int(height.Int64)
		file.Height = &h
	}
	if duration.Valid {
		d := int(duration.Int64)
		file.Duration = &d
	}
	if expiresAt.Valid {
		file.ExpiresAt = &expiresAt.Time
	}

	// Parse metadata
	if err := json.Unmarshal([]byte(metadataJSON), &file.Metadata); err != nil {
		file.Metadata = make(map[string]interface{})
	}

	return &file, nil
}

// CleanupExpired removes expired files based on retention policy
func (fm *FileManager) CleanupExpired(ctx context.Context) (int, error) {
	if !fm.IsEnabled() || !fm.config.AutoCleanup {
		return 0, nil
	}

	// First, get the files that will be deleted for event emission
	selectQuery := `SELECT id, thread_id, size_bytes, filename FROM files WHERE expires_at IS NOT NULL AND expires_at < ?`
	rows, err := fm.db.QueryContext(ctx, selectQuery, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to query expired files: %w", err)
	}

	type expiredFile struct {
		id        string
		threadID  sql.NullString
		sizeBytes int64
		filename  string
	}
	var expiredFiles []expiredFile

	for rows.Next() {
		var f expiredFile
		if err := rows.Scan(&f.id, &f.threadID, &f.sizeBytes, &f.filename); err != nil {
			continue
		}
		expiredFiles = append(expiredFiles, f)
	}
	rows.Close()

	if len(expiredFiles) == 0 {
		return 0, nil
	}

	// Call deletion callbacks for each file
	for _, f := range expiredFiles {
		if fm.deletionCallback != nil {
			fm.deletionCallback(f.id)
		}
	}

	// Delete the expired files
	deleteQuery := `DELETE FROM files WHERE expires_at IS NOT NULL AND expires_at < ?`
	result, err := fm.db.ExecContext(ctx, deleteQuery, time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired files: %w", err)
	}

	deletedCount, _ := result.RowsAffected()

	// Emit file_expired events (cleanup runs in background, no trace context)
	eventBus := events.GetEventBus()
	for _, f := range expiredFiles {
		expireEvent := events.NewEvent(events.CategoryFile, events.TypeFileExpired, events.LevelInfo).
			WithData("file_id", f.id).
			WithData("filename", f.filename).
			WithData("size_bytes", f.sizeBytes).
			WithData("reason", "expiry")
		if f.threadID.Valid {
			expireEvent = expireEvent.WithThread(f.threadID.String)
		}
		eventBus.PublishWithContext(ctx, expireEvent)
	}

	if deletedCount > 0 {
		log.Printf("🧹 Cleaned up %d expired files", deletedCount)
	}

	return int(deletedCount), nil
}

// CleanupOrphans is disabled - files are never automatically deleted
func (fm *FileManager) CleanupOrphans(ctx context.Context) (int, error) {
	return 0, nil
}

// GetStats returns storage statistics
func (fm *FileManager) GetStats(ctx context.Context) (*StorageStats, error) {
	if !fm.IsEnabled() {
		return nil, fmt.Errorf("filesystem is disabled")
	}

	stats := &StorageStats{
		FilesByType: make(map[string]int),
		BytesByType: make(map[string]int64),
	}

	// Get total files and size
	query := `SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files`
	err := fm.db.QueryRowContext(ctx, query).Scan(&stats.TotalFiles, &stats.TotalSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage stats: %w", err)
	}

	// Get files and bytes by type
	query = `SELECT file_type, COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files GROUP BY file_type`
	rows, err := fm.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get files by type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var fileType string
		var count int
		var bytes int64
		if err := rows.Scan(&fileType, &count, &bytes); err != nil {
			continue
		}
		stats.FilesByType[fileType] = count
		stats.BytesByType[fileType] = bytes
	}

	// Get oldest and newest files
	query = `SELECT MIN(created_at), MAX(created_at) FROM files`
	var oldest, newest sql.NullTime
	err = fm.db.QueryRowContext(ctx, query).Scan(&oldest, &newest)
	if err == nil {
		if oldest.Valid {
			stats.OldestFile = &oldest.Time
		}
		if newest.Valid {
			stats.NewestFile = &newest.Time
		}
	}

	return stats, nil
}

// GetStatsByThread returns storage statistics for a specific thread
func (fm *FileManager) GetStatsByThread(ctx context.Context, threadID string) (*ThreadStorageStats, error) {
	if !fm.IsEnabled() {
		return nil, fmt.Errorf("filesystem is disabled")
	}

	stats := &ThreadStorageStats{
		ThreadID:    threadID,
		FilesByType: make(map[string]int),
		BytesByType: make(map[string]int64),
	}

	// Get total files and size for thread
	query := `SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files WHERE thread_id = ?`
	err := fm.db.QueryRowContext(ctx, query, threadID).Scan(&stats.TotalFiles, &stats.TotalSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to get thread storage stats: %w", err)
	}

	// Get files and bytes by type for thread
	query = `SELECT file_type, COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files WHERE thread_id = ? GROUP BY file_type`
	rows, err := fm.db.QueryContext(ctx, query, threadID)
	if err != nil {
		return nil, fmt.Errorf("failed to get files by type for thread: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var fileType string
		var count int
		var bytes int64
		if err := rows.Scan(&fileType, &count, &bytes); err != nil {
			continue
		}
		stats.FilesByType[fileType] = count
		stats.BytesByType[fileType] = bytes
	}

	return stats, nil
}

// GetGlobalStats returns storage statistics with per-thread breakdown
func (fm *FileManager) GetGlobalStats(ctx context.Context) (*GlobalStorageStats, error) {
	if !fm.IsEnabled() {
		return nil, fmt.Errorf("filesystem is disabled")
	}

	stats := &GlobalStorageStats{
		FilesByType: make(map[string]int),
		BytesByType: make(map[string]int64),
		ByThread:    make(map[string]*ThreadStorageStats),
	}

	// Get total files and size
	query := `SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files`
	err := fm.db.QueryRowContext(ctx, query).Scan(&stats.TotalFiles, &stats.TotalSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage stats: %w", err)
	}

	// Get files and bytes by type
	query = `SELECT file_type, COUNT(*), COALESCE(SUM(size_bytes), 0) FROM files GROUP BY file_type`
	rows, err := fm.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get files by type: %w", err)
	}

	for rows.Next() {
		var fileType string
		var count int
		var bytes int64
		if err := rows.Scan(&fileType, &count, &bytes); err != nil {
			continue
		}
		stats.FilesByType[fileType] = count
		stats.BytesByType[fileType] = bytes
	}
	rows.Close()

	// Get per-thread stats
	query = `SELECT COALESCE(thread_id, ''), file_type, COUNT(*), COALESCE(SUM(size_bytes), 0)
	         FROM files GROUP BY thread_id, file_type`
	rows, err = fm.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get thread stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var threadID, fileType string
		var count int
		var bytes int64
		if err := rows.Scan(&threadID, &fileType, &count, &bytes); err != nil {
			continue
		}

		if threadID == "" {
			threadID = "_no_thread"
		}

		if stats.ByThread[threadID] == nil {
			stats.ByThread[threadID] = &ThreadStorageStats{
				ThreadID:    threadID,
				FilesByType: make(map[string]int),
				BytesByType: make(map[string]int64),
			}
		}

		stats.ByThread[threadID].FilesByType[fileType] = count
		stats.ByThread[threadID].BytesByType[fileType] = bytes
		stats.ByThread[threadID].TotalFiles += count
		stats.ByThread[threadID].TotalSizeBytes += bytes
	}

	// Get oldest and newest files
	query = `SELECT MIN(created_at), MAX(created_at) FROM files`
	var oldest, newest sql.NullTime
	err = fm.db.QueryRowContext(ctx, query).Scan(&oldest, &newest)
	if err == nil {
		if oldest.Valid {
			stats.OldestFile = &oldest.Time
		}
		if newest.Valid {
			stats.NewestFile = &newest.Time
		}
	}

	return stats, nil
}

// ListFiles returns a list of files with optional filtering
func (fm *FileManager) ListFiles(ctx context.Context, threadID string, limit int) ([]*File, error) {
	if !fm.IsEnabled() {
		return nil, fmt.Errorf("filesystem is disabled")
	}

	var query string
	var args []interface{}

	if threadID != "" {
		query = `
			SELECT id, thread_id, message_id, filename, mime_type, file_type,
			       size_bytes, checksum, width, height, duration, source, created_at
			FROM files WHERE thread_id = ? ORDER BY created_at DESC LIMIT ?
		`
		args = []interface{}{threadID, limit}
	} else {
		query = `
			SELECT id, thread_id, message_id, filename, mime_type, file_type,
			       size_bytes, checksum, width, height, duration, source, created_at
			FROM files ORDER BY created_at DESC LIMIT ?
		`
		args = []interface{}{limit}
	}

	rows, err := fm.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		var file File
		var threadIDStr, messageID sql.NullString
		var width, height, duration sql.NullInt64

		err := rows.Scan(
			&file.ID, &threadIDStr, &messageID, &file.Filename, &file.MimeType, &file.FileType,
			&file.SizeBytes, &file.Checksum, &width, &height, &duration, &file.Source, &file.CreatedAt,
		)
		if err != nil {
			continue
		}

		if threadIDStr.Valid {
			file.ThreadID = threadIDStr.String
		}
		if messageID.Valid {
			file.MessageID = messageID.String
		}
		if width.Valid {
			w := int(width.Int64)
			file.Width = &w
		}
		if height.Valid {
			h := int(height.Int64)
			file.Height = &h
		}
		if duration.Valid {
			d := int(duration.Int64)
			file.Duration = &d
		}

		files = append(files, &file)
	}

	return files, nil
}

// updateAccessTracking updates the access count and last accessed timestamp
func (fm *FileManager) updateAccessTracking(fileID string) {
	query := `
		UPDATE files
		SET access_count = access_count + 1, last_accessed = ?
		WHERE id = ?
	`
	_, err := fm.db.Exec(query, time.Now(), fileID)
	if err != nil {
		log.Printf("Failed to update access tracking for file %s: %v", fileID, err)
	}
}

// generateID generates a random file ID
func generateID() string {
	return strings.ToLower(fmt.Sprintf("%016x", time.Now().UnixNano()))
}

// isAllowedType checks if a file type is in the allowed list
func (fm *FileManager) isAllowedType(fileType string) bool {
	if len(fm.config.AllowedTypes) == 0 {
		return true // No restrictions
	}

	for _, allowed := range fm.config.AllowedTypes {
		if strings.EqualFold(fileType, allowed) {
			return true
		}
	}

	return false
}

// DetermineFileType determines the file type from MIME type
func DetermineFileType(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "application/pdf"):
		return "document"
	case strings.HasPrefix(mimeType, "text/"):
		return "document"
	default:
		return "other"
	}
}

// GetPublicURL generates a public download URL for a file
func (fm *FileManager) GetPublicURL(fileID string) (string, error) {
	if !fm.IsEnabled() {
		return "", fmt.Errorf("filesystem disabled")
	}

	// Get agent's public URL from config
	cfg := config.GetConfig()
	agentConfig := cfg.Get()
	baseURL := agentConfig.PublicURL

	// Require explicit public URL (don't fall back to auto-detection)
	if baseURL == "" {
		return "", fmt.Errorf("no public URL configured - set PUBLIC_URL environment variable or public_url in config")
	}

	// Remove trailing slash if present
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Construct download URL
	return fmt.Sprintf("%s/files/%s/download", baseURL, fileID), nil
}

// GetMetadata retrieves file metadata by ID (without the data blob)
func (fm *FileManager) GetMetadata(fileID string) (*File, error) {
	if !fm.IsEnabled() {
		return nil, fmt.Errorf("filesystem is disabled")
	}

	query := `
		SELECT id, thread_id, message_id, filename, mime_type, file_type,
		       size_bytes, checksum, width, height, duration, metadata,
		       source, source_tool, created_at, expires_at
		FROM files WHERE id = ?
	`

	var file File
	var threadID, messageID, sourceToolStr sql.NullString
	var width, height, duration sql.NullInt64
	var metadataJSON string
	var expiresAt sql.NullTime

	err := fm.db.QueryRow(query, fileID).Scan(
		&file.ID, &threadID, &messageID, &file.Filename, &file.MimeType, &file.FileType,
		&file.SizeBytes, &file.Checksum, &width, &height, &duration,
		&metadataJSON, &file.Source, &sourceToolStr, &file.CreatedAt, &expiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("file not found: %s", fileID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	// Handle nullable fields
	if threadID.Valid {
		file.ThreadID = threadID.String
	}
	if messageID.Valid {
		file.MessageID = messageID.String
	}
	if sourceToolStr.Valid {
		file.SourceTool = sourceToolStr.String
	}
	if width.Valid {
		w := int(width.Int64)
		file.Width = &w
	}
	if height.Valid {
		h := int(height.Int64)
		file.Height = &h
	}
	if duration.Valid {
		d := int(duration.Int64)
		file.Duration = &d
	}
	if expiresAt.Valid {
		file.ExpiresAt = &expiresAt.Time
	}

	// Parse metadata JSON
	if metadataJSON != "" {
		json.Unmarshal([]byte(metadataJSON), &file.Metadata)
	}
	if file.Metadata == nil {
		file.Metadata = make(map[string]interface{})
	}

	return &file, nil
}

// ListFilesFiltered returns a list of files with optional filtering by thread and file type
func (fm *FileManager) ListFilesFiltered(threadID, fileType string, limit int) ([]*File, error) {
	if !fm.IsEnabled() {
		return nil, fmt.Errorf("filesystem is disabled")
	}

	// Build query dynamically based on filters
	query := `
		SELECT id, thread_id, message_id, filename, mime_type, file_type,
		       size_bytes, checksum, width, height, duration, source, created_at
		FROM files WHERE 1=1
	`
	var args []interface{}

	if threadID != "" {
		query += " AND thread_id = ?"
		args = append(args, threadID)
	}

	if fileType != "" {
		query += " AND file_type = ?"
		args = append(args, fileType)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := fm.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		var file File
		var threadIDStr, messageID sql.NullString
		var width, height, duration sql.NullInt64

		err := rows.Scan(
			&file.ID, &threadIDStr, &messageID, &file.Filename, &file.MimeType, &file.FileType,
			&file.SizeBytes, &file.Checksum, &width, &height, &duration, &file.Source, &file.CreatedAt,
		)
		if err != nil {
			continue
		}

		if threadIDStr.Valid {
			file.ThreadID = threadIDStr.String
		}
		if messageID.Valid {
			file.MessageID = messageID.String
		}
		if width.Valid {
			w := int(width.Int64)
			file.Width = &w
		}
		if height.Valid {
			h := int(height.Int64)
			file.Height = &h
		}
		if duration.Valid {
			d := int(duration.Int64)
			file.Duration = &d
		}

		files = append(files, &file)
	}

	return files, nil
}

// SearchFiles searches files by filename pattern
func (fm *FileManager) SearchFiles(query, fileType string, limit int) ([]*File, error) {
	if !fm.IsEnabled() {
		return nil, fmt.Errorf("filesystem is disabled")
	}

	// Build SQL query with LIKE for filename search
	sqlQuery := `
		SELECT id, thread_id, message_id, filename, mime_type, file_type,
		       size_bytes, checksum, width, height, duration, source, created_at
		FROM files WHERE filename LIKE ?
	`
	searchPattern := "%" + query + "%"
	args := []interface{}{searchPattern}

	if fileType != "" {
		sqlQuery += " AND file_type = ?"
		args = append(args, fileType)
	}

	sqlQuery += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := fm.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search files: %w", err)
	}
	defer rows.Close()

	var files []*File
	for rows.Next() {
		var file File
		var threadIDStr, messageID sql.NullString
		var width, height, duration sql.NullInt64

		err := rows.Scan(
			&file.ID, &threadIDStr, &messageID, &file.Filename, &file.MimeType, &file.FileType,
			&file.SizeBytes, &file.Checksum, &width, &height, &duration, &file.Source, &file.CreatedAt,
		)
		if err != nil {
			continue
		}

		if threadIDStr.Valid {
			file.ThreadID = threadIDStr.String
		}
		if messageID.Valid {
			file.MessageID = messageID.String
		}
		if width.Valid {
			w := int(width.Int64)
			file.Width = &w
		}
		if height.Valid {
			h := int(height.Int64)
			file.Height = &h
		}
		if duration.Valid {
			d := int(duration.Int64)
			file.Duration = &d
		}

		files = append(files, &file)
	}

	return files, nil
}

// DeleteByID removes a file by ID (without context for simpler tool usage)
func (fm *FileManager) DeleteByID(fileID string) error {
	return fm.Delete(context.Background(), fileID)
}
