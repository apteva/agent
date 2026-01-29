# Multipart & Chunked Upload Support - Complete Proposal

## Executive Summary

Add comprehensive file upload capabilities to the agent:
1. **Standard Multipart** - Single-request uploads for typical files (< 50MB)
2. **Chunked Upload** - Multi-request uploads for large files with resume support

**Benefits:**
- Support files of any size
- Resume interrupted uploads
- Better progress tracking
- Native browser support
- 25-33% bandwidth savings vs base64

**Effort:**
- Phase 1 (Multipart): 4-5 hours
- Phase 2 (Chunked): 15-20 hours
- Total: 20-25 hours

---

## Table of Contents

1. [Current State](#1-current-state)
2. [Proposed Architecture](#2-proposed-architecture)
3. [Phase 1: Standard Multipart](#3-phase-1-standard-multipart)
4. [Phase 2: Chunked Upload](#4-phase-2-chunked-upload)
5. [Implementation Details](#5-implementation-details)
6. [API Specifications](#6-api-specifications)
7. [Client Examples](#7-client-examples)
8. [Security & Performance](#8-security--performance)
9. [Testing Strategy](#9-testing-strategy)
10. [Migration & Rollout](#10-migration--rollout)

---

## 1. Current State

### ✅ What Works
```bash
# JSON with base64 (single request)
curl -X POST http://localhost:8080/files \
  -H "Content-Type: application/json" \
  -d '{"filename": "image.png", "data": "iVBORw0KGgo..."}'
```

**Limitations:**
- ❌ 33% bandwidth overhead (base64 encoding)
- ❌ Entire file in memory
- ❌ No native browser support
- ❌ No resume capability
- ❌ Practical limit ~10MB

### ❌ What Doesn't Work
```bash
# Multipart form data
curl -F "file=@large_video.mp4" http://localhost:8080/files

# Chunked upload
curl -X POST http://localhost:8080/files/chunks \
  -H "X-Upload-ID: abc123" \
  -H "X-Chunk-Index: 0" \
  --data-binary @chunk_0.bin
```

---

## 2. Proposed Architecture

### Upload Flow Decision Tree

```
User wants to upload file
    ↓
Check file size
    ↓
    ├─→ < 10 KB          → Inline base64 in message (existing)
    ├─→ 10 KB - 50 MB    → Standard multipart (Phase 1)
    └─→ > 50 MB          → Chunked upload (Phase 2)
```

### Three Upload Methods

| Method | Size Range | Requests | Resume | Use Case |
|--------|-----------|----------|--------|----------|
| **JSON base64** | < 10 KB | 1 | ❌ | Chat messages with images |
| **Multipart** | 10 KB - 50 MB | 1 | ❌ | Standard file uploads |
| **Chunked** | > 50 MB | Many | ✅ | Large files, videos |

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    POST /files                              │
│                         ↓                                   │
│              Check Content-Type                             │
│                         ↓                                   │
│    ┌───────────────────┼───────────────────┐              │
│    │                   │                   │              │
│    ↓                   ↓                   ↓              │
│ application/json   multipart/form    (chunked uses       │
│    (base64)           (binary)       separate endpoints) │
│    EXISTING            NEW                                │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│              Chunked Upload Flow (NEW)                      │
│                                                             │
│  1. POST /files/uploads/init                               │
│     → Returns upload_id                                    │
│                                                             │
│  2. POST /files/uploads/:id/chunks  (multiple times)       │
│     → Upload chunk_0, chunk_1, ... chunk_N                │
│     → Each chunk stored temporarily                        │
│                                                             │
│  3. POST /files/uploads/:id/complete                       │
│     → Assemble chunks → Store final file                   │
│     → Returns file_id                                      │
│                                                             │
│  Optional:                                                 │
│  - GET /files/uploads/:id/status  → Check progress        │
│  - DELETE /files/uploads/:id      → Cancel upload         │
└─────────────────────────────────────────────────────────────┘
```

---

## 3. Phase 1: Standard Multipart

### 3.1 Overview

Single-request upload using standard `multipart/form-data`.

**Perfect for:**
- Images (screenshots, photos)
- Documents (PDFs, text files)
- Audio files
- Files up to 50MB

### 3.2 Implementation

**File:** `filesystem/handlers.go`

```go
// HandleFilesCreateOrList - Updated to support multipart
func HandleFilesCreateOrList(fm *FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			contentType := r.Header.Get("Content-Type")

			if strings.HasPrefix(contentType, "multipart/form-data") {
				handleMultipartUpload(fm, w, r)
			} else {
				handleJSONUpload(fm, w, r) // Existing
			}
			return
		}

		if r.Method == http.MethodGet {
			HandleFilesList(fm)(w, r)
			return
		}

		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMultipartUpload - NEW
func handleMultipartUpload(fm *FileManager, w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (32MB max in memory)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Failed to parse multipart form",
		})
		return
	}
	defer r.MultipartForm.RemoveAll()

	// Get file from form
	file, header, err := r.FormFile("file")
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "No file provided (use 'file' field)",
		})
		return
	}
	defer file.Close()

	// Read file data
	fileData, err := io.ReadAll(file)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to read file",
		})
		return
	}

	// Auto-detect MIME type
	mimeType := r.FormValue("mime_type")
	if mimeType == "" {
		mimeType = header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = http.DetectContentType(fileData)
		}
	}

	// Parse metadata
	var metadata map[string]interface{}
	if metadataStr := r.FormValue("metadata"); metadataStr != "" {
		json.Unmarshal([]byte(metadataStr), &metadata)
	} else {
		metadata = make(map[string]interface{})
	}

	// Create file object
	fileObj := &File{
		Filename:   header.Filename,
		MimeType:   mimeType,
		FileType:   determineFileType(mimeType),
		SizeBytes:  int64(len(fileData)),
		Data:       fileData,
		ThreadID:   r.FormValue("thread_id"),
		MessageID:  r.FormValue("message_id"),
		Source:     r.FormValue("source"),
		SourceTool: r.FormValue("source_tool"),
		Metadata:   metadata,
	}

	// Check size limit
	if fm.config.MaxFileSize > 0 && fileObj.SizeBytes > fm.config.MaxFileSize {
		sendJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("File too large (max %d bytes)", fm.config.MaxFileSize),
		})
		return
	}

	// Store file
	fileID, err := fm.Store(r.Context(), fileObj)
	if err != nil {
		log.Printf("Failed to store file: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to store file",
		})
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"file_id":   fileID,
		"filename":  header.Filename,
		"size":      fileObj.SizeBytes,
		"mime_type": mimeType,
		"message":   fmt.Sprintf("File %s uploaded successfully", header.Filename),
	})
}

func determineFileType(mimeType string) string {
	if strings.HasPrefix(mimeType, "image/") {
		return "image"
	}
	if strings.HasPrefix(mimeType, "video/") {
		return "video"
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return "audio"
	}
	if mimeType == "application/pdf" || strings.HasPrefix(mimeType, "text/") {
		return "document"
	}
	return "other"
}
```

### 3.3 Usage Example

```bash
# Simple upload
curl -X POST http://localhost:8080/files \
  -F "file=@document.pdf"

# With metadata
curl -X POST http://localhost:8080/files \
  -F "file=@screenshot.png" \
  -F "thread_id=thread_123" \
  -F "source=user_upload" \
  -F "metadata={\"description\":\"Bug screenshot\"}"
```

---

## 4. Phase 2: Chunked Upload

### 4.1 Overview

Multi-request upload for large files with resume capability.

**Perfect for:**
- Videos
- Large datasets
- Backup files
- Files > 50MB

### 4.2 Data Model

**New Table:** `upload_sessions`

```sql
CREATE TABLE upload_sessions (
    id TEXT PRIMARY KEY,                    -- upload_xyz123
    filename TEXT NOT NULL,
    mime_type TEXT,
    file_type TEXT,
    total_size INTEGER NOT NULL,
    chunk_size INTEGER NOT NULL,
    total_chunks INTEGER NOT NULL,
    uploaded_chunks INTEGER DEFAULT 0,

    -- Metadata
    thread_id TEXT,
    source TEXT,
    metadata TEXT DEFAULT '{}',

    -- State
    status TEXT CHECK (status IN ('pending', 'uploading', 'assembling', 'completed', 'failed')) DEFAULT 'pending',

    -- Timing
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,                   -- Auto-cleanup incomplete uploads
    completed_at TIMESTAMP,

    -- Result
    file_id TEXT,                           -- Final file ID after assembly
    error TEXT,

    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE SET NULL
);

CREATE INDEX idx_upload_sessions_status ON upload_sessions(status);
CREATE INDEX idx_upload_sessions_expires ON upload_sessions(expires_at);
CREATE INDEX idx_upload_sessions_thread ON upload_sessions(thread_id);
```

**New Table:** `upload_chunks`

```sql
CREATE TABLE upload_chunks (
    upload_id TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,
    chunk_size INTEGER NOT NULL,
    chunk_data BLOB NOT NULL,
    checksum TEXT,                          -- For integrity verification
    uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (upload_id, chunk_index),
    FOREIGN KEY (upload_id) REFERENCES upload_sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_upload_chunks_upload ON upload_chunks(upload_id);
```

### 4.3 API Endpoints

#### **1. Initialize Upload**

```
POST /files/uploads/init
```

**Request:**
```json
{
  "filename": "large_video.mp4",
  "mime_type": "video/mp4",
  "total_size": 524288000,
  "chunk_size": 5242880,
  "thread_id": "thread_123",
  "metadata": {
    "description": "Conference recording"
  }
}
```

**Response:**
```json
{
  "success": true,
  "upload_id": "upload_abc123",
  "chunk_size": 5242880,
  "total_chunks": 100,
  "expires_at": "2024-11-12T10:00:00Z"
}
```

#### **2. Upload Chunk**

```
POST /files/uploads/:upload_id/chunks
Content-Type: application/octet-stream
X-Chunk-Index: 0
X-Chunk-Checksum: sha256:abc123...
```

**Request Body:** Binary chunk data

**Response:**
```json
{
  "success": true,
  "upload_id": "upload_abc123",
  "chunk_index": 0,
  "uploaded_chunks": 1,
  "total_chunks": 100,
  "progress_percent": 1.0
}
```

#### **3. Get Upload Status**

```
GET /files/uploads/:upload_id/status
```

**Response:**
```json
{
  "success": true,
  "upload_id": "upload_abc123",
  "status": "uploading",
  "filename": "large_video.mp4",
  "total_size": 524288000,
  "uploaded_chunks": 45,
  "total_chunks": 100,
  "progress_percent": 45.0,
  "missing_chunks": [23, 31, 44],
  "created_at": "2024-11-11T10:00:00Z",
  "updated_at": "2024-11-11T10:05:23Z",
  "expires_at": "2024-11-12T10:00:00Z"
}
```

#### **4. Complete Upload**

```
POST /files/uploads/:upload_id/complete
```

**Request:** (optional)
```json
{
  "verify_checksums": true
}
```

**Response:**
```json
{
  "success": true,
  "upload_id": "upload_abc123",
  "file_id": "file_xyz789",
  "filename": "large_video.mp4",
  "size": 524288000,
  "message": "File assembled and stored successfully"
}
```

#### **5. Cancel Upload**

```
DELETE /files/uploads/:upload_id
```

**Response:**
```json
{
  "success": true,
  "message": "Upload cancelled and chunks deleted"
}
```

#### **6. List Active Uploads**

```
GET /files/uploads?status=uploading&thread_id=thread_123
```

**Response:**
```json
{
  "success": true,
  "count": 2,
  "uploads": [
    {
      "upload_id": "upload_abc123",
      "filename": "video1.mp4",
      "progress_percent": 45.0,
      "status": "uploading"
    },
    {
      "upload_id": "upload_def456",
      "filename": "video2.mp4",
      "progress_percent": 78.0,
      "status": "uploading"
    }
  ]
}
```

### 4.4 Implementation

**File:** `filesystem/chunked_upload.go` (NEW)

```go
package filesystem

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

// UploadSession represents a chunked upload session
type UploadSession struct {
	ID             string                 `json:"upload_id"`
	Filename       string                 `json:"filename"`
	MimeType       string                 `json:"mime_type"`
	FileType       string                 `json:"file_type"`
	TotalSize      int64                  `json:"total_size"`
	ChunkSize      int64                  `json:"chunk_size"`
	TotalChunks    int                    `json:"total_chunks"`
	UploadedChunks int                    `json:"uploaded_chunks"`
	ThreadID       string                 `json:"thread_id,omitempty"`
	Source         string                 `json:"source,omitempty"`
	Metadata       map[string]interface{} `json:"metadata"`
	Status         string                 `json:"status"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	ExpiresAt      time.Time              `json:"expires_at"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	FileID         string                 `json:"file_id,omitempty"`
	Error          string                 `json:"error,omitempty"`
}

// ChunkData represents a single chunk
type Chunk struct {
	UploadID   string
	ChunkIndex int
	ChunkSize  int64
	ChunkData  []byte
	Checksum   string
	UploadedAt time.Time
}

// ChunkedUploadManager manages chunked uploads
type ChunkedUploadManager struct {
	db          *sql.DB
	fm          *FileManager
	maxChunkAge time.Duration // Auto-expire old chunks
}

func NewChunkedUploadManager(db *sql.DB, fm *FileManager) *ChunkedUploadManager {
	return &ChunkedUploadManager{
		db:          db,
		fm:          fm,
		maxChunkAge: 24 * time.Hour, // Expire after 24 hours
	}
}

// InitUpload creates a new upload session
func (cum *ChunkedUploadManager) InitUpload(ctx context.Context, req UploadInitRequest) (*UploadSession, error) {
	// Calculate chunks
	totalChunks := int((req.TotalSize + req.ChunkSize - 1) / req.ChunkSize)

	session := &UploadSession{
		ID:             "upload_" + uuid.New().String()[:12],
		Filename:       req.Filename,
		MimeType:       req.MimeType,
		FileType:       determineFileType(req.MimeType),
		TotalSize:      req.TotalSize,
		ChunkSize:      req.ChunkSize,
		TotalChunks:    totalChunks,
		UploadedChunks: 0,
		ThreadID:       req.ThreadID,
		Source:         req.Source,
		Metadata:       req.Metadata,
		Status:         "pending",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(cum.maxChunkAge),
	}

	// Store session in database
	metadataJSON, _ := json.Marshal(session.Metadata)

	_, err := cum.db.ExecContext(ctx, `
		INSERT INTO upload_sessions (
			id, filename, mime_type, file_type, total_size, chunk_size,
			total_chunks, thread_id, source, metadata, status, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Filename, session.MimeType, session.FileType,
		session.TotalSize, session.ChunkSize, session.TotalChunks,
		session.ThreadID, session.Source, string(metadataJSON),
		session.Status, session.ExpiresAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create upload session: %w", err)
	}

	return session, nil
}

// UploadChunk stores a single chunk
func (cum *ChunkedUploadManager) UploadChunk(ctx context.Context, uploadID string, chunkIndex int, data []byte, checksum string) error {
	// Verify session exists and is valid
	session, err := cum.GetUploadSession(ctx, uploadID)
	if err != nil {
		return fmt.Errorf("upload session not found: %w", err)
	}

	if session.Status == "completed" {
		return fmt.Errorf("upload already completed")
	}

	if session.Status == "failed" {
		return fmt.Errorf("upload failed: %s", session.Error)
	}

	if time.Now().After(session.ExpiresAt) {
		return fmt.Errorf("upload session expired")
	}

	// Verify chunk index
	if chunkIndex < 0 || chunkIndex >= session.TotalChunks {
		return fmt.Errorf("invalid chunk index: %d (must be 0-%d)", chunkIndex, session.TotalChunks-1)
	}

	// Verify checksum if provided
	if checksum != "" {
		hash := sha256.Sum256(data)
		actualChecksum := "sha256:" + hex.EncodeToString(hash[:])
		if checksum != actualChecksum {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", checksum, actualChecksum)
		}
	} else {
		// Generate checksum
		hash := sha256.Sum256(data)
		checksum = "sha256:" + hex.EncodeToString(hash[:])
	}

	// Check if chunk already uploaded
	var exists bool
	err = cum.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM upload_chunks WHERE upload_id = ? AND chunk_index = ?)",
		uploadID, chunkIndex,
	).Scan(&exists)

	if err != nil {
		return fmt.Errorf("failed to check chunk existence: %w", err)
	}

	if exists {
		// Chunk already uploaded, update it
		_, err = cum.db.ExecContext(ctx, `
			UPDATE upload_chunks
			SET chunk_size = ?, chunk_data = ?, checksum = ?, uploaded_at = CURRENT_TIMESTAMP
			WHERE upload_id = ? AND chunk_index = ?`,
			len(data), data, checksum, uploadID, chunkIndex,
		)
	} else {
		// Insert new chunk
		_, err = cum.db.ExecContext(ctx, `
			INSERT INTO upload_chunks (upload_id, chunk_index, chunk_size, chunk_data, checksum)
			VALUES (?, ?, ?, ?, ?)`,
			uploadID, chunkIndex, len(data), data, checksum,
		)
	}

	if err != nil {
		return fmt.Errorf("failed to store chunk: %w", err)
	}

	// Update session status
	_, err = cum.db.ExecContext(ctx, `
		UPDATE upload_sessions
		SET uploaded_chunks = (SELECT COUNT(*) FROM upload_chunks WHERE upload_id = ?),
		    status = 'uploading',
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		uploadID, uploadID,
	)

	return err
}

// GetUploadSession retrieves session details
func (cum *ChunkedUploadManager) GetUploadSession(ctx context.Context, uploadID string) (*UploadSession, error) {
	var session UploadSession
	var metadataJSON, completedAt sql.NullString

	err := cum.db.QueryRowContext(ctx, `
		SELECT id, filename, mime_type, file_type, total_size, chunk_size,
		       total_chunks, uploaded_chunks, thread_id, source, metadata,
		       status, created_at, updated_at, expires_at, completed_at,
		       COALESCE(file_id, ''), COALESCE(error, '')
		FROM upload_sessions
		WHERE id = ?`,
		uploadID,
	).Scan(
		&session.ID, &session.Filename, &session.MimeType, &session.FileType,
		&session.TotalSize, &session.ChunkSize, &session.TotalChunks,
		&session.UploadedChunks, &session.ThreadID, &session.Source,
		&metadataJSON, &session.Status, &session.CreatedAt, &session.UpdatedAt,
		&session.ExpiresAt, &completedAt, &session.FileID, &session.Error,
	)

	if err != nil {
		return nil, err
	}

	if metadataJSON.Valid {
		json.Unmarshal([]byte(metadataJSON.String), &session.Metadata)
	}

	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		session.CompletedAt = &t
	}

	return &session, nil
}

// GetMissingChunks returns list of missing chunk indices
func (cum *ChunkedUploadManager) GetMissingChunks(ctx context.Context, uploadID string) ([]int, error) {
	session, err := cum.GetUploadSession(ctx, uploadID)
	if err != nil {
		return nil, err
	}

	// Get uploaded chunk indices
	rows, err := cum.db.QueryContext(ctx,
		"SELECT chunk_index FROM upload_chunks WHERE upload_id = ? ORDER BY chunk_index",
		uploadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	uploaded := make(map[int]bool)
	for rows.Next() {
		var idx int
		if err := rows.Scan(&idx); err != nil {
			return nil, err
		}
		uploaded[idx] = true
	}

	// Find missing chunks
	var missing []int
	for i := 0; i < session.TotalChunks; i++ {
		if !uploaded[i] {
			missing = append(missing, i)
		}
	}

	return missing, nil
}

// CompleteUpload assembles chunks into final file
func (cum *ChunkedUploadManager) CompleteUpload(ctx context.Context, uploadID string, verifyChecksums bool) (string, error) {
	session, err := cum.GetUploadSession(ctx, uploadID)
	if err != nil {
		return "", err
	}

	if session.Status == "completed" {
		return session.FileID, nil
	}

	// Check all chunks uploaded
	if session.UploadedChunks != session.TotalChunks {
		missing, _ := cum.GetMissingChunks(ctx, uploadID)
		return "", fmt.Errorf("incomplete upload: %d/%d chunks (missing: %v)",
			session.UploadedChunks, session.TotalChunks, missing)
	}

	// Update status to assembling
	_, err = cum.db.ExecContext(ctx,
		"UPDATE upload_sessions SET status = 'assembling', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		uploadID,
	)
	if err != nil {
		return "", err
	}

	// Assemble chunks
	rows, err := cum.db.QueryContext(ctx, `
		SELECT chunk_index, chunk_data, checksum
		FROM upload_chunks
		WHERE upload_id = ?
		ORDER BY chunk_index`,
		uploadID,
	)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve chunks: %w", err)
	}
	defer rows.Close()

	var assembledData []byte
	chunkIdx := 0

	for rows.Next() {
		var idx int
		var data []byte
		var checksum string

		if err := rows.Scan(&idx, &data, &checksum); err != nil {
			return "", err
		}

		// Verify order
		if idx != chunkIdx {
			return "", fmt.Errorf("chunk order mismatch: expected %d, got %d", chunkIdx, idx)
		}

		// Verify checksum if requested
		if verifyChecksums && checksum != "" {
			hash := sha256.Sum256(data)
			actualChecksum := "sha256:" + hex.EncodeToString(hash[:])
			if checksum != actualChecksum {
				return "", fmt.Errorf("chunk %d checksum mismatch", idx)
			}
		}

		assembledData = append(assembledData, data...)
		chunkIdx++
	}

	// Verify total size
	if int64(len(assembledData)) != session.TotalSize {
		return "", fmt.Errorf("size mismatch: expected %d bytes, got %d bytes",
			session.TotalSize, len(assembledData))
	}

	// Store as file
	file := &File{
		Filename:   session.Filename,
		MimeType:   session.MimeType,
		FileType:   session.FileType,
		SizeBytes:  int64(len(assembledData)),
		Data:       assembledData,
		ThreadID:   session.ThreadID,
		Source:     session.Source,
		SourceTool: "chunked_upload",
		Metadata:   session.Metadata,
	}

	fileID, err := cum.fm.Store(ctx, file)
	if err != nil {
		// Mark as failed
		cum.db.ExecContext(ctx, `
			UPDATE upload_sessions
			SET status = 'failed', error = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			err.Error(), uploadID,
		)
		return "", fmt.Errorf("failed to store file: %w", err)
	}

	// Mark as completed
	now := time.Now()
	_, err = cum.db.ExecContext(ctx, `
		UPDATE upload_sessions
		SET status = 'completed', file_id = ?, completed_at = ?, updated_at = ?
		WHERE id = ?`,
		fileID, now, now, uploadID,
	)

	if err != nil {
		return "", err
	}

	// Delete chunks (optional - can keep for debugging)
	_, err = cum.db.ExecContext(ctx,
		"DELETE FROM upload_chunks WHERE upload_id = ?",
		uploadID,
	)

	return fileID, err
}

// CancelUpload deletes upload session and chunks
func (cum *ChunkedUploadManager) CancelUpload(ctx context.Context, uploadID string) error {
	// Delete chunks first (cascade will handle it, but explicit is better)
	_, err := cum.db.ExecContext(ctx,
		"DELETE FROM upload_chunks WHERE upload_id = ?",
		uploadID,
	)
	if err != nil {
		return err
	}

	// Delete session
	_, err = cum.db.ExecContext(ctx,
		"DELETE FROM upload_sessions WHERE id = ?",
		uploadID,
	)

	return err
}

// CleanupExpired removes expired upload sessions
func (cum *ChunkedUploadManager) CleanupExpired(ctx context.Context) (int, error) {
	result, err := cum.db.ExecContext(ctx, `
		DELETE FROM upload_sessions
		WHERE status IN ('pending', 'uploading')
		  AND expires_at < CURRENT_TIMESTAMP`,
	)

	if err != nil {
		return 0, err
	}

	count, _ := result.RowsAffected()
	return int(count), nil
}

// UploadInitRequest for initializing upload
type UploadInitRequest struct {
	Filename  string                 `json:"filename"`
	MimeType  string                 `json:"mime_type"`
	TotalSize int64                  `json:"total_size"`
	ChunkSize int64                  `json:"chunk_size"`
	ThreadID  string                 `json:"thread_id,omitempty"`
	Source    string                 `json:"source,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}
```

**File:** `filesystem/chunked_handlers.go` (NEW)

```go
package filesystem

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// HandleUploadInit - POST /files/uploads/init
func HandleUploadInit(cum *ChunkedUploadManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req UploadInitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "Invalid request body",
			})
			return
		}

		// Validate request
		if req.Filename == "" {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "filename is required",
			})
			return
		}

		if req.TotalSize <= 0 {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "total_size must be positive",
			})
			return
		}

		if req.ChunkSize <= 0 {
			// Default to 5MB chunks
			req.ChunkSize = 5 * 1024 * 1024
		}

		// Initialize upload
		session, err := cum.InitUpload(r.Context(), req)
		if err != nil {
			log.Printf("Failed to init upload: %v", err)
			sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   "Failed to initialize upload",
			})
			return
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":      true,
			"upload_id":    session.ID,
			"chunk_size":   session.ChunkSize,
			"total_chunks": session.TotalChunks,
			"expires_at":   session.ExpiresAt,
		})
	}
}

// HandleUploadChunk - POST /files/uploads/:id/chunks
func HandleUploadChunk(cum *ChunkedUploadManager) http.handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract upload ID from path
		path := strings.TrimPrefix(r.URL.Path, "/files/uploads/")
		uploadID := strings.Split(path, "/")[0]

		// Get chunk index from header
		chunkIndexStr := r.Header.Get("X-Chunk-Index")
		if chunkIndexStr == "" {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "X-Chunk-Index header required",
			})
			return
		}

		chunkIndex, err := strconv.Atoi(chunkIndexStr)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "Invalid X-Chunk-Index",
			})
			return
		}

		// Get optional checksum
		checksum := r.Header.Get("X-Chunk-Checksum")

		// Read chunk data
		chunkData, err := io.ReadAll(r.Body)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   "Failed to read chunk data",
			})
			return
		}

		// Upload chunk
		if err := cum.UploadChunk(r.Context(), uploadID, chunkIndex, chunkData, checksum); err != nil {
			log.Printf("Failed to upload chunk: %v", err)
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		// Get updated session
		session, _ := cum.GetUploadSession(r.Context(), uploadID)

		progressPercent := 0.0
		if session.TotalChunks > 0 {
			progressPercent = float64(session.UploadedChunks) / float64(session.TotalChunks) * 100
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":          true,
			"upload_id":        uploadID,
			"chunk_index":      chunkIndex,
			"uploaded_chunks":  session.UploadedChunks,
			"total_chunks":     session.TotalChunks,
			"progress_percent": progressPercent,
		})
	}
}

// HandleUploadStatus - GET /files/uploads/:id/status
func HandleUploadStatus(cum *ChunkedUploadManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract upload ID
		uploadID := strings.TrimPrefix(r.URL.Path, "/files/uploads/")
		uploadID = strings.TrimSuffix(uploadID, "/status")

		session, err := cum.GetUploadSession(r.Context(), uploadID)
		if err != nil {
			sendJSON(w, http.StatusNotFound, map[string]interface{}{
				"success": false,
				"error":   "Upload session not found",
			})
			return
		}

		// Get missing chunks
		missingChunks, _ := cum.GetMissingChunks(r.Context(), uploadID)

		progressPercent := 0.0
		if session.TotalChunks > 0 {
			progressPercent = float64(session.UploadedChunks) / float64(session.TotalChunks) * 100
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":          true,
			"upload_id":        session.ID,
			"status":           session.Status,
			"filename":         session.Filename,
			"total_size":       session.TotalSize,
			"uploaded_chunks":  session.UploadedChunks,
			"total_chunks":     session.TotalChunks,
			"progress_percent": progressPercent,
			"missing_chunks":   missingChunks,
			"created_at":       session.CreatedAt,
			"updated_at":       session.UpdatedAt,
			"expires_at":       session.ExpiresAt,
			"file_id":          session.FileID,
		})
	}
}

// HandleUploadComplete - POST /files/uploads/:id/complete
func HandleUploadComplete(cum *ChunkedUploadManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract upload ID
		uploadID := strings.TrimPrefix(r.URL.Path, "/files/uploads/")
		uploadID = strings.TrimSuffix(uploadID, "/complete")

		// Parse options
		var opts struct {
			VerifyChecksums bool `json:"verify_checksums"`
		}
		json.NewDecoder(r.Body).Decode(&opts)

		// Complete upload
		fileID, err := cum.CompleteUpload(r.Context(), uploadID, opts.VerifyChecksums)
		if err != nil {
			log.Printf("Failed to complete upload: %v", err)
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		// Get session for response
		session, _ := cum.GetUploadSession(r.Context(), uploadID)

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":  true,
			"upload_id": uploadID,
			"file_id":  fileID,
			"filename": session.Filename,
			"size":     session.TotalSize,
			"message":  "File assembled and stored successfully",
		})
	}
}

// HandleUploadCancel - DELETE /files/uploads/:id
func HandleUploadCancel(cum *ChunkedUploadManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		uploadID := strings.TrimPrefix(r.URL.Path, "/files/uploads/")

		if err := cum.CancelUpload(r.Context(), uploadID); err != nil {
			log.Printf("Failed to cancel upload: %v", err)
			sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   "Failed to cancel upload",
			})
			return
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Upload cancelled and chunks deleted",
		})
	}
}
```

---

## 5. Implementation Details

(Content continues - see next sections...)

---

## File Structure

**New Files:**
- `filesystem/chunked_upload.go` (~500 lines)
- `filesystem/chunked_handlers.go` (~300 lines)
- `migrations/007_chunked_uploads.sql` (~50 lines)

**Modified Files:**
- `filesystem/handlers.go` (~150 lines changed)
- `main.go` (~50 lines - register routes)

**Total:** ~1050 lines of code

---

## Timeline

**Phase 1 (Multipart):** 4-5 hours
- Handler implementation: 2 hours
- Testing: 1 hour
- Documentation: 1 hour

**Phase 2 (Chunked):** 15-20 hours
- Database schema: 1 hour
- Core logic: 6 hours
- Handlers: 4 hours
- Testing: 3 hours
- Documentation: 2 hours
- Integration: 2 hours

**Total: 20-25 hours**

---

**(Due to message length limits, I've provided the core structure. Would you like me to continue with sections 6-10 covering API specs, client examples, security, testing, and migration?)**
