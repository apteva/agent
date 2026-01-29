package filesystem

import (
	"context"
	"database/sql"
	"encoding/base64"
	"testing"
	"time"

	"agent-server/config"

	_ "github.com/mattn/go-sqlite3"
)

// setupTestDB creates a test database
func setupTestDB(t *testing.T) *sql.DB {
	// Create temp database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create files table
	schema := `
	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		thread_id TEXT,
		message_id TEXT,
		filename TEXT NOT NULL,
		mime_type TEXT NOT NULL,
		file_type TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		checksum TEXT,
		data BLOB NOT NULL,
		width INTEGER,
		height INTEGER,
		duration INTEGER,
		metadata TEXT DEFAULT '{}',
		source TEXT DEFAULT 'unknown',
		source_tool TEXT,
		access_count INTEGER DEFAULT 0,
		last_accessed TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_files_checksum ON files(checksum);
	CREATE INDEX IF NOT EXISTS idx_files_created ON files(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_files_expires ON files(expires_at);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("Failed to create test schema: %v", err)
	}

	return db
}

// generateTestImageData generates a simple PNG image as bytes
func generateTestImageData(sizeKB int) []byte {
	// Generate random data that represents an image
	size := sizeKB * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

func TestFileManager_Store(t *testing.T) {
	cfg := &config.FileSystemConfig{
		Enabled:        true,
		MaxFileSize:    10 * 1024 * 1024, // 10MB
		MaxTotalSize:   100 * 1024 * 1024,
		AutoExtract:    true,
		MinExtractSize: 1024,
		Deduplication:  true,
		AutoCleanup:    true,
		RetentionDays:  7,
		CleanupOrphans: true,
		AllowedTypes:   []string{"image", "document"},
	}

	t.Run("Store basic file", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()
		fm := NewFileManager(db, cfg)
		ctx := context.Background()
		data := generateTestImageData(50) // 50KB
		file := &File{
			ThreadID:  "thread123",
			MessageID: "msg123",
			Filename:  "test.jpg",
			MimeType:  "image/jpeg",
			FileType:  "image",
			SizeBytes: int64(len(data)),
			Data:      data,
			Source:    "user_upload",
		}

		fileID, err := fm.Store(ctx, file)
		if err != nil {
			t.Fatalf("Failed to store file: %v", err)
		}

		if fileID == "" {
			t.Fatal("Expected file ID, got empty string")
		}

		// Verify file was stored
		stored, err := fm.Get(ctx, fileID)
		if err != nil {
			t.Fatalf("Failed to retrieve stored file: %v", err)
		}

		if stored.Filename != "test.jpg" {
			t.Errorf("Expected filename 'test.jpg', got '%s'", stored.Filename)
		}
		if stored.SizeBytes != int64(len(data)) {
			t.Errorf("Expected size %d, got %d", len(data), stored.SizeBytes)
		}
	})

	t.Run("Deduplication", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()
		fm := NewFileManager(db, cfg)
		ctx := context.Background()

		data := generateTestImageData(50)

		file1 := &File{
			ThreadID:  "thread123",
			Filename:  "duplicate1.jpg",
			MimeType:  "image/jpeg",
			FileType:  "image",
			SizeBytes: int64(len(data)),
			Data:      data,
			Source:    "user_upload",
		}

		fileID1, err := fm.Store(ctx, file1)
		if err != nil {
			t.Fatalf("Failed to store first file: %v", err)
		}

		// Store same data again (should be deduplicated)
		file2 := &File{
			ThreadID:  "thread456",
			Filename:  "duplicate2.jpg",
			MimeType:  "image/jpeg",
			FileType:  "image",
			SizeBytes: int64(len(data)),
			Data:      data,
			Source:    "user_upload",
		}

		fileID2, err := fm.Store(ctx, file2)
		if err != nil {
			t.Fatalf("Failed to store second file: %v", err)
		}

		// Should return same file ID (deduplicated)
		if fileID1 != fileID2 {
			t.Errorf("Expected deduplication: got different IDs %s and %s", fileID1, fileID2)
		}
	})

	t.Run("Size limit enforcement", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()
		fm := NewFileManager(db, cfg)
		ctx := context.Background()

		// Try to store file larger than max
		data := generateTestImageData(11 * 1024) // 11MB (exceeds 10MB limit)
		file := &File{
			Filename:  "toolarge.jpg",
			MimeType:  "image/jpeg",
			FileType:  "image",
			SizeBytes: int64(len(data)),
			Data:      data,
			Source:    "user_upload",
		}

		_, err := fm.Store(ctx, file)
		if err == nil {
			t.Fatal("Expected error for file exceeding size limit, got nil")
		}
	})

	t.Run("Disallowed file type", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()
		fm := NewFileManager(db, cfg)
		ctx := context.Background()

		data := generateTestImageData(50)
		file := &File{
			Filename:  "test.mp4",
			MimeType:  "video/mp4",
			FileType:  "video", // Not in allowed_types
			SizeBytes: int64(len(data)),
			Data:      data,
			Source:    "user_upload",
		}

		_, err := fm.Store(ctx, file)
		if err == nil {
			t.Fatal("Expected error for disallowed file type, got nil")
		}
	})
}

func TestFileManager_GetAndDelete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.FileSystemConfig{
		Enabled:      true,
		MaxFileSize:  10 * 1024 * 1024,
		MaxTotalSize: 100 * 1024 * 1024,
		AllowedTypes: []string{"image"},
	}

	fm := NewFileManager(db, cfg)
	ctx := context.Background()

	// Store a file
	data := generateTestImageData(50)
	file := &File{
		Filename:  "test.jpg",
		MimeType:  "image/jpeg",
		FileType:  "image",
		SizeBytes: int64(len(data)),
		Data:      data,
		Source:    "user_upload",
	}

	fileID, err := fm.Store(ctx, file)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	t.Run("Get existing file", func(t *testing.T) {
		retrieved, err := fm.Get(ctx, fileID)
		if err != nil {
			t.Fatalf("Failed to get file: %v", err)
		}

		if retrieved.ID != fileID {
			t.Errorf("Expected ID %s, got %s", fileID, retrieved.ID)
		}
		if len(retrieved.Data) != len(data) {
			t.Errorf("Expected data length %d, got %d", len(data), len(retrieved.Data))
		}
	})

	t.Run("Get as base64", func(t *testing.T) {
		base64Str, err := fm.GetAsBase64(ctx, fileID)
		if err != nil {
			t.Fatalf("Failed to get file as base64: %v", err)
		}

		// Decode and verify
		decoded, err := base64.StdEncoding.DecodeString(base64Str)
		if err != nil {
			t.Fatalf("Failed to decode base64: %v", err)
		}

		if len(decoded) != len(data) {
			t.Errorf("Expected decoded length %d, got %d", len(data), len(decoded))
		}
	})

	t.Run("Delete file", func(t *testing.T) {
		err := fm.Delete(ctx, fileID)
		if err != nil {
			t.Fatalf("Failed to delete file: %v", err)
		}

		// Try to get deleted file (should fail)
		_, err = fm.Get(ctx, fileID)
		if err == nil {
			t.Fatal("Expected error getting deleted file, got nil")
		}
	})

	t.Run("Get non-existent file", func(t *testing.T) {
		_, err := fm.Get(ctx, "nonexistent")
		if err == nil {
			t.Fatal("Expected error for non-existent file, got nil")
		}
	})
}

func TestFileManager_Cleanup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.FileSystemConfig{
		Enabled:        true,
		MaxFileSize:    10 * 1024 * 1024,
		MaxTotalSize:   100 * 1024 * 1024,
		AutoCleanup:    true,
		RetentionDays:  7,
		CleanupOrphans: true,
		AllowedTypes:   []string{"image"},
	}

	fm := NewFileManager(db, cfg)
	ctx := context.Background()

	t.Run("Cleanup expired files", func(t *testing.T) {
		data := generateTestImageData(50)

		// Create file with past expiration
		expiredTime := time.Now().Add(-1 * time.Hour)
		file := &File{
			Filename:  "expired.jpg",
			MimeType:  "image/jpeg",
			FileType:  "image",
			SizeBytes: int64(len(data)),
			Data:      data,
			Source:    "user_upload",
			ExpiresAt: &expiredTime,
		}

		fileID, err := fm.Store(ctx, file)
		if err != nil {
			t.Fatalf("Failed to store file: %v", err)
		}

		// Run cleanup
		cleaned, err := fm.CleanupExpired(ctx)
		if err != nil {
			t.Fatalf("Failed to cleanup: %v", err)
		}

		if cleaned != 1 {
			t.Errorf("Expected 1 file cleaned, got %d", cleaned)
		}

		// Verify file is gone
		_, err = fm.Get(ctx, fileID)
		if err == nil {
			t.Fatal("Expected file to be deleted after cleanup")
		}
	})
}

func TestFileManager_Stats(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.FileSystemConfig{
		Enabled:      true,
		MaxFileSize:  10 * 1024 * 1024,
		MaxTotalSize: 100 * 1024 * 1024,
		AllowedTypes: []string{"image", "document"},
	}

	fm := NewFileManager(db, cfg)
	ctx := context.Background()

	// Store multiple files
	for i := 0; i < 3; i++ {
		data := generateTestImageData(50)
		file := &File{
			Filename:  "test.jpg",
			MimeType:  "image/jpeg",
			FileType:  "image",
			SizeBytes: int64(len(data)),
			Data:      data,
			Source:    "user_upload",
		}
		_, err := fm.Store(ctx, file)
		if err != nil {
			t.Fatalf("Failed to store file %d: %v", i, err)
		}
	}

	// Get stats
	stats, err := fm.GetStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats.TotalFiles < 3 {
		t.Errorf("Expected at least 3 files, got %d", stats.TotalFiles)
	}

	if stats.TotalSizeBytes == 0 {
		t.Error("Expected non-zero total size")
	}

	if len(stats.FilesByType) == 0 {
		t.Error("Expected files by type breakdown")
	}
}

func TestContentProcessor_ExtractAndExpand(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.FileSystemConfig{
		Enabled:        true,
		MaxFileSize:    10 * 1024 * 1024,
		MaxTotalSize:   100 * 1024 * 1024,
		AutoExtract:    true,
		MinExtractSize: 1024, // 1KB
		Deduplication:  true,
		AllowedTypes:   []string{"image"},
	}

	fm := NewFileManager(db, cfg)
	ctx := context.Background()

	t.Run("Extract base64 to file reference", func(t *testing.T) {
		// Generate test image data
		data := generateTestImageData(50) // 50KB
		base64Str := base64.StdEncoding.EncodeToString(data)

		// Create content block with base64
		blocks := []ContentBlock{
			{
				Type: "text",
				Text: "Here's an image:",
			},
			{
				Type: "image",
				Source: &ContentSource{
					Type:      "base64",
					MediaType: "image/jpeg",
					Data:      base64Str,
				},
			},
		}

		// Process blocks (should extract file)
		processed, err := fm.ProcessContentBlocks(blocks, "thread123", "msg123", "user_upload")
		if err != nil {
			t.Fatalf("Failed to process blocks: %v", err)
		}

		// Check that image was converted to file reference
		if len(processed) != 2 {
			t.Fatalf("Expected 2 blocks, got %d", len(processed))
		}

		imageBlock := processed[1]
		if imageBlock.Source == nil {
			t.Fatal("Expected source in image block")
		}

		if imageBlock.Source.Type != "file" {
			t.Errorf("Expected source type 'file', got '%s'", imageBlock.Source.Type)
		}

		if imageBlock.Source.FileID == "" {
			t.Error("Expected file ID, got empty string")
		}

		// Verify file was stored
		_, err = fm.Get(ctx, imageBlock.Source.FileID)
		if err != nil {
			t.Fatalf("Failed to retrieve stored file: %v", err)
		}
	})

	t.Run("Expand file reference to base64", func(t *testing.T) {
		db := setupTestDB(t)
		defer db.Close()
		fm := NewFileManager(db, cfg)
		ctx := context.Background()

		// First store a file
		data := generateTestImageData(50)
		file := &File{
			Filename:  "test.jpg",
			MimeType:  "image/jpeg",
			FileType:  "image",
			SizeBytes: int64(len(data)),
			Data:      data,
			Source:    "user_upload",
		}

		fileID, err := fm.Store(ctx, file)
		if err != nil {
			t.Fatalf("Failed to store file: %v", err)
		}

		// Create content block with file reference
		blocks := []ContentBlock{
			{
				Type: "image",
				Source: &ContentSource{
					Type:      "file",
					FileID:    fileID,
					MediaType: "image/jpeg",
				},
			},
		}

		// Expand file references
		expanded, err := fm.ExpandFileReferences(blocks)
		if err != nil {
			t.Fatalf("Failed to expand references: %v", err)
		}

		if len(expanded) != 1 {
			t.Fatalf("Expected 1 block, got %d", len(expanded))
		}

		expandedBlock := expanded[0]
		if expandedBlock.Source == nil {
			t.Fatal("Expected source in expanded block")
		}

		if expandedBlock.Source.Type != "base64" {
			t.Errorf("Expected source type 'base64', got '%s'", expandedBlock.Source.Type)
		}

		if expandedBlock.Source.Data == "" {
			t.Error("Expected base64 data, got empty string")
		}

		// Verify base64 data matches original
		decoded, err := base64.StdEncoding.DecodeString(expandedBlock.Source.Data)
		if err != nil {
			t.Fatalf("Failed to decode base64: %v", err)
		}

		if len(decoded) != len(data) {
			t.Errorf("Expected decoded length %d, got %d", len(data), len(decoded))
		}
	})

	t.Run("Skip small base64 content", func(t *testing.T) {
		// Generate small data (less than min_extract_size)
		data := []byte("small image data")
		base64Str := base64.StdEncoding.EncodeToString(data)

		blocks := []ContentBlock{
			{
				Type: "image",
				Source: &ContentSource{
					Type:      "base64",
					MediaType: "image/jpeg",
					Data:      base64Str,
				},
			},
		}

		// Process blocks (should NOT extract)
		processed, err := fm.ProcessContentBlocks(blocks, "thread123", "msg123", "user_upload")
		if err != nil {
			t.Fatalf("Failed to process blocks: %v", err)
		}

		// Should still be base64 (not extracted)
		if processed[0].Source.Type != "base64" {
			t.Errorf("Expected type to remain 'base64', got '%s'", processed[0].Source.Type)
		}
	})
}

func TestFileManager_Disabled(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.FileSystemConfig{
		Enabled: false, // Disabled
	}

	fm := NewFileManager(db, cfg)
	ctx := context.Background()

	if fm.IsEnabled() {
		t.Error("Expected filesystem to be disabled")
	}

	// All operations should fail gracefully
	_, err := fm.Store(ctx, &File{})
	if err == nil {
		t.Error("Expected error when storing with filesystem disabled")
	}

	_, err = fm.Get(ctx, "test")
	if err == nil {
		t.Error("Expected error when getting with filesystem disabled")
	}
}

func TestExtractImagesFromToolResult(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.FileSystemConfig{
		Enabled:        true,
		MaxFileSize:    10 * 1024 * 1024,
		MaxTotalSize:   100 * 1024 * 1024,
		AutoExtract:    true,
		MinExtractSize: 100, // Low threshold for testing
		Deduplication:  true,
		AllowedTypes:   []string{"image"},
	}

	fm := NewFileManager(db, cfg)

	// Generate test image data
	data := generateTestImageData(5) // 5KB
	base64Str := base64.StdEncoding.EncodeToString(data)

	t.Run("Extract from nested structure with mimeType", func(t *testing.T) {
		// Simulate tool result like: {success: true, data: {images: [{data: "base64...", mimeType: "image/png"}]}}
		toolResult := map[string]interface{}{
			"success": true,
			"message": "Generated test image",
			"data": map[string]interface{}{
				"metadata": map[string]interface{}{
					"width":  800,
					"height": 600,
				},
				"images": []interface{}{
					map[string]interface{}{
						"data":     base64Str,
						"mimeType": "image/png",
					},
				},
			},
		}

		processed, fileIDs, err := fm.ExtractImagesFromToolResult(toolResult, "thread123", "test_tool")
		if err != nil {
			t.Fatalf("Failed to extract images: %v", err)
		}

		if len(fileIDs) != 1 {
			t.Fatalf("Expected 1 extracted file, got %d", len(fileIDs))
		}

		// Verify structure was modified
		processedMap := processed.(map[string]interface{})
		dataMap := processedMap["data"].(map[string]interface{})
		imagesArr := dataMap["images"].([]interface{})
		imageRef := imagesArr[0].(map[string]interface{})

		if imageRef["type"] != "file_reference" {
			t.Errorf("Expected type 'file_reference', got '%v'", imageRef["type"])
		}
		if imageRef["file_id"] == nil || imageRef["file_id"] == "" {
			t.Error("Expected file_id to be set")
		}
		if imageRef["extracted"] != true {
			t.Error("Expected extracted to be true")
		}
	})

	t.Run("Extract from Anthropic format", func(t *testing.T) {
		// Simulate Anthropic image format
		toolResult := map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": "image/png",
				"data":       base64Str,
			},
		}

		processed, fileIDs, err := fm.ExtractImagesFromToolResult(toolResult, "thread123", "test_tool")
		if err != nil {
			t.Fatalf("Failed to extract images: %v", err)
		}

		if len(fileIDs) != 1 {
			t.Fatalf("Expected 1 extracted file, got %d", len(fileIDs))
		}

		// Verify it was converted to file reference
		processedMap := processed.(map[string]interface{})
		if processedMap["type"] != "file_reference" {
			t.Errorf("Expected type 'file_reference', got '%v'", processedMap["type"])
		}
	})

	t.Run("Extract from format with base64 indicator", func(t *testing.T) {
		// Format: {data: "base64...", format: "base64"}
		toolResult := map[string]interface{}{
			"data":   base64Str,
			"format": "base64",
		}

		processed, fileIDs, err := fm.ExtractImagesFromToolResult(toolResult, "thread123", "test_tool")
		if err != nil {
			t.Fatalf("Failed to extract images: %v", err)
		}

		if len(fileIDs) != 1 {
			t.Fatalf("Expected 1 extracted file, got %d", len(fileIDs))
		}

		processedMap := processed.(map[string]interface{})
		if processedMap["type"] != "file_reference" {
			t.Errorf("Expected type 'file_reference', got '%v'", processedMap["type"])
		}
	})

	t.Run("Extract screenshot field", func(t *testing.T) {
		toolResult := map[string]interface{}{
			"success":    true,
			"screenshot": base64Str,
		}

		processed, fileIDs, err := fm.ExtractImagesFromToolResult(toolResult, "thread123", "test_tool")
		if err != nil {
			t.Fatalf("Failed to extract images: %v", err)
		}

		if len(fileIDs) != 1 {
			t.Fatalf("Expected 1 extracted file, got %d", len(fileIDs))
		}

		processedMap := processed.(map[string]interface{})
		// Screenshot should be replaced with file reference
		if _, hasScreenshot := processedMap["screenshot"]; hasScreenshot {
			t.Error("Expected screenshot field to be removed")
		}
	})

	t.Run("Skip small data", func(t *testing.T) {
		// Data smaller than MinExtractSize
		smallData := base64.StdEncoding.EncodeToString([]byte("tiny"))

		toolResult := map[string]interface{}{
			"data":     smallData,
			"mimeType": "image/png",
		}

		_, fileIDs, err := fm.ExtractImagesFromToolResult(toolResult, "thread123", "test_tool")
		if err != nil {
			t.Fatalf("Failed to process: %v", err)
		}

		if len(fileIDs) != 0 {
			t.Errorf("Expected 0 extracted files for small data, got %d", len(fileIDs))
		}
	})

	t.Run("No extraction when disabled", func(t *testing.T) {
		disabledCfg := &config.FileSystemConfig{
			Enabled:     true,
			AutoExtract: false, // Disabled
		}
		disabledFM := NewFileManager(db, disabledCfg)

		toolResult := map[string]interface{}{
			"data":     base64Str,
			"mimeType": "image/png",
		}

		_, fileIDs, err := disabledFM.ExtractImagesFromToolResult(toolResult, "thread123", "test_tool")
		if err != nil {
			t.Fatalf("Failed to process: %v", err)
		}

		if len(fileIDs) != 0 {
			t.Errorf("Expected 0 extracted files when disabled, got %d", len(fileIDs))
		}
	})

	t.Run("Multiple images in array", func(t *testing.T) {
		toolResult := map[string]interface{}{
			"images": []interface{}{
				map[string]interface{}{
					"data":     base64Str,
					"mimeType": "image/png",
				},
				map[string]interface{}{
					"data":     base64Str,
					"mimeType": "image/jpeg",
				},
			},
		}

		_, fileIDs, err := fm.ExtractImagesFromToolResult(toolResult, "thread123", "test_tool")
		if err != nil {
			t.Fatalf("Failed to extract images: %v", err)
		}

		// With deduplication, both will have same ID since data is identical
		if len(fileIDs) < 1 {
			t.Errorf("Expected at least 1 extracted file, got %d", len(fileIDs))
		}
	})
}

// Benchmark tests
func BenchmarkFileManager_Store(b *testing.B) {
	db := setupTestDB(&testing.T{})
	defer db.Close()

	cfg := &config.FileSystemConfig{
		Enabled:       true,
		MaxFileSize:   10 * 1024 * 1024,
		MaxTotalSize:  100 * 1024 * 1024,
		Deduplication: true,
		AllowedTypes:  []string{"image"},
	}

	fm := NewFileManager(db, cfg)
	ctx := context.Background()

	data := generateTestImageData(50) // 50KB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file := &File{
			Filename:  "test.jpg",
			MimeType:  "image/jpeg",
			FileType:  "image",
			SizeBytes: int64(len(data)),
			Data:      data,
			Source:    "benchmark",
		}
		fm.Store(ctx, file)
	}
}

func BenchmarkFileManager_Get(b *testing.B) {
	db := setupTestDB(&testing.T{})
	defer db.Close()

	cfg := &config.FileSystemConfig{
		Enabled:      true,
		MaxFileSize:  10 * 1024 * 1024,
		MaxTotalSize: 100 * 1024 * 1024,
		AllowedTypes: []string{"image"},
	}

	fm := NewFileManager(db, cfg)
	ctx := context.Background()

	// Store a file to retrieve
	data := generateTestImageData(50)
	file := &File{
		Filename:  "test.jpg",
		MimeType:  "image/jpeg",
		FileType:  "image",
		SizeBytes: int64(len(data)),
		Data:      data,
		Source:    "benchmark",
	}
	fileID, _ := fm.Store(ctx, file)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fm.Get(ctx, fileID)
	}
}
