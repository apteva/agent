package filesystem

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"agent-server/events"
)

// HandleFilesCreateOrList handles POST /files (upload) and returns list metadata
// Supports both multipart form upload and JSON with base64 data
func HandleFilesCreateOrList(fm *FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			contentType := r.Header.Get("Content-Type")

			// Check if multipart form upload
			if strings.HasPrefix(contentType, "multipart/form-data") {
				handleMultipartUpload(fm, w, r)
				return
			}

			// Default: JSON with base64 data
			handleJSONUpload(fm, w, r)

		} else if r.Method == http.MethodGet {
			// Handle file list (delegate to HandleFilesList)
			HandleFilesList(fm)(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// handleMultipartUpload handles multipart form file uploads
func handleMultipartUpload(fm *FileManager, w http.ResponseWriter, r *http.Request) {
	// Create trace and span for file upload operation
	trace := events.NewTrace("file_upload", "", nil)
	defer trace.Finish()

	span := trace.StartSpan("multipart_upload", events.SpanKindServer).
		WithCategory(events.CategoryFile)
	defer span.End()

	// Add trace context to request context for propagation
	ctx := events.WithTraceAndSpan(r.Context(), trace, span)

	// Parse multipart form (32MB max memory)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to parse multipart form: %s", err.Error()),
		})
		return
	}

	// Get file from form
	uploadedFile, header, err := r.FormFile("file")
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "No file provided. Use form field 'file'",
		})
		return
	}
	defer uploadedFile.Close()

	// Read file data
	fileData, err := io.ReadAll(uploadedFile)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to read file: %s", err.Error()),
		})
		return
	}

	// Get filename
	filename := header.Filename
	if customFilename := r.FormValue("filename"); customFilename != "" {
		filename = customFilename
	}

	// Detect MIME type from header or use provided
	mimeType := header.Header.Get("Content-Type")
	if customMimeType := r.FormValue("mime_type"); customMimeType != "" {
		mimeType = customMimeType
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		// Try to detect from filename extension
		mimeType = detectMimeType(filename)
	}

	// Determine file type
	fileType := r.FormValue("file_type")
	if fileType == "" {
		fileType = DetermineFileType(mimeType)
	}

	// Get optional fields from form
	threadID := r.FormValue("thread_id")
	messageID := r.FormValue("message_id")
	source := r.FormValue("source")
	if source == "" {
		source = "upload"
	}
	sourceTool := r.FormValue("source_tool")

	// Create file object
	file := &File{
		Filename:   filename,
		MimeType:   mimeType,
		FileType:   fileType,
		SizeBytes:  int64(len(fileData)),
		Data:       fileData,
		ThreadID:   threadID,
		MessageID:  messageID,
		Source:     source,
		SourceTool: sourceTool,
		Metadata:   make(map[string]interface{}),
	}

	// Store file with trace context
	fileID, err := fm.Store(ctx, file)
	if err != nil {
		log.Printf("Failed to store file: %v", err)
		span.RecordError(err)
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to store file: %s", err.Error()),
		})
		return
	}

	// Record file info in span
	span.WithAttribute("file_id", fileID).
		WithAttribute("filename", filename).
		WithAttribute("size_bytes", len(fileData))

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"file_id":   fileID,
		"filename":  filename,
		"mime_type": mimeType,
		"file_type": fileType,
		"size":      len(fileData),
		"message":   fmt.Sprintf("File %s uploaded successfully", filename),
	})
}

// handleJSONUpload handles JSON with base64 encoded data
func handleJSONUpload(fm *FileManager, w http.ResponseWriter, r *http.Request) {
	// Create trace and span for file upload operation
	trace := events.NewTrace("file_upload", "", nil)
	defer trace.Finish()

	span := trace.StartSpan("json_upload", events.SpanKindServer).
		WithCategory(events.CategoryFile)
	defer span.End()

	// Add trace context to request context for propagation
	ctx := events.WithTraceAndSpan(r.Context(), trace, span)

	var uploadData struct {
		Filename   string                 `json:"filename"`
		MimeType   string                 `json:"mime_type"`
		FileType   string                 `json:"file_type"`
		SizeBytes  int64                  `json:"size_bytes"`
		Data       string                 `json:"data"` // Base64 encoded
		ThreadID   string                 `json:"thread_id,omitempty"`
		MessageID  string                 `json:"message_id,omitempty"`
		Source     string                 `json:"source,omitempty"`
		SourceTool string                 `json:"source_tool,omitempty"`
		Metadata   map[string]interface{} `json:"metadata,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&uploadData); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid request body",
		})
		return
	}

	// Decode base64 data
	fileData, err := base64.StdEncoding.DecodeString(uploadData.Data)
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Invalid base64 data",
		})
		return
	}

	// Determine file type from mime_type if not provided
	fileType := uploadData.FileType
	if fileType == "" && uploadData.MimeType != "" {
		fileType = DetermineFileType(uploadData.MimeType)
	}

	// Create file object
	file := &File{
		Filename:   uploadData.Filename,
		MimeType:   uploadData.MimeType,
		FileType:   fileType,
		SizeBytes:  int64(len(fileData)),
		Data:       fileData,
		ThreadID:   uploadData.ThreadID,
		MessageID:  uploadData.MessageID,
		Source:     uploadData.Source,
		SourceTool: uploadData.SourceTool,
		Metadata:   uploadData.Metadata,
	}

	if file.Metadata == nil {
		file.Metadata = make(map[string]interface{})
	}

	// Store file with trace context
	fileID, err := fm.Store(ctx, file)
	if err != nil {
		log.Printf("Failed to store file: %v", err)
		span.RecordError(err)
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to store file: %s", err.Error()),
		})
		return
	}

	// Record file info in span
	span.WithAttribute("file_id", fileID).
		WithAttribute("filename", uploadData.Filename).
		WithAttribute("size_bytes", len(fileData))

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"file_id": fileID,
		"message": fmt.Sprintf("File %s uploaded successfully", uploadData.Filename),
	})
}

// detectMimeType detects MIME type from filename extension
func detectMimeType(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".txt"):
		return "text/plain"
	case strings.HasSuffix(lower, ".html"), strings.HasSuffix(lower, ".htm"):
		return "text/html"
	case strings.HasSuffix(lower, ".css"):
		return "text/css"
	case strings.HasSuffix(lower, ".js"):
		return "application/javascript"
	case strings.HasSuffix(lower, ".json"):
		return "application/json"
	case strings.HasSuffix(lower, ".xml"):
		return "application/xml"
	case strings.HasSuffix(lower, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(lower, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(lower, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(lower, ".mp4"):
		return "video/mp4"
	case strings.HasSuffix(lower, ".webm"):
		return "video/webm"
	case strings.HasSuffix(lower, ".md"):
		return "text/markdown"
	case strings.HasSuffix(lower, ".csv"):
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}

// HandleFilesList handles GET /files
func HandleFilesList(fm *FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !fm.IsEnabled() {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "Filesystem is disabled",
			})
			return
		}

		// Parse query parameters
		threadID := r.URL.Query().Get("thread_id")
		limitStr := r.URL.Query().Get("limit")
		limit := 50 // default
		if limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
				limit = parsed
			}
		}

		// List files
		files, err := fm.ListFiles(r.Context(), threadID, limit)
		if err != nil {
			log.Printf("Failed to list files: %v", err)
			sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   "Failed to list files",
			})
			return
		}

		// Add public URLs to each file
		filesWithURLs := make([]map[string]interface{}, len(files))
		for i, file := range files {
			fileMap := map[string]interface{}{
				"id":         file.ID,
				"thread_id":  file.ThreadID,
				"filename":   file.Filename,
				"mime_type":  file.MimeType,
				"file_type":  file.FileType,
				"size_bytes": file.SizeBytes,
				"checksum":   file.Checksum,
				"metadata":   file.Metadata,
				"source":     file.Source,
				"created_at": file.CreatedAt,
			}
			if file.MessageID != "" {
				fileMap["message_id"] = file.MessageID
			}
			if file.SourceTool != "" {
				fileMap["source_tool"] = file.SourceTool
			}
			// Add public URL if available
			if url, err := fm.GetPublicURL(file.ID); err == nil {
				fileMap["url"] = url
			}
			filesWithURLs[i] = fileMap
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"count":   len(files),
			"files":   filesWithURLs,
		})
	}
}

// HandleFilesGet handles GET /files/{id}
func HandleFilesGet(fm *FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !fm.IsEnabled() {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "Filesystem is disabled",
			})
			return
		}

		// Extract file ID from path (StripPrefix already removed /files)
		fileID := strings.TrimPrefix(r.URL.Path, "/")
		if fileID == "" {
			http.Error(w, "File ID required", http.StatusBadRequest)
			return
		}

		// Get file
		file, err := fm.Get(r.Context(), fileID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				sendJSON(w, http.StatusNotFound, map[string]interface{}{
					"success": false,
					"error":   "File not found",
				})
				return
			}
			log.Printf("Failed to get file: %v", err)
			sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   "Failed to get file",
			})
			return
		}

		// Build response with public URL
		response := map[string]interface{}{
			"success": true,
			"file":    file,
		}
		if url, err := fm.GetPublicURL(file.ID); err == nil {
			response["url"] = url
		}

		sendJSON(w, http.StatusOK, response)
	}
}

// HandleFilesDownload handles GET /files/{id}/download
func HandleFilesDownload(fm *FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !fm.IsEnabled() {
			http.Error(w, "Filesystem is disabled", http.StatusBadRequest)
			return
		}

		// Extract file ID from path (StripPrefix already removed /files)
		path := strings.TrimPrefix(r.URL.Path, "/")
		fileID := strings.TrimSuffix(path, "/download")
		if fileID == "" || fileID == path {
			http.Error(w, "File ID required", http.StatusBadRequest)
			return
		}

		// Get file
		file, err := fm.Get(r.Context(), fileID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "File not found", http.StatusNotFound)
				return
			}
			log.Printf("Failed to get file: %v", err)
			http.Error(w, "Failed to get file", http.StatusInternalServerError)
			return
		}

		// Set headers for download
		w.Header().Set("Content-Type", file.MimeType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Filename))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", file.SizeBytes))

		// Write file data
		if _, err := w.Write(file.Data); err != nil {
			log.Printf("Failed to write file data: %v", err)
		}
	}
}

// HandleFilesDelete handles DELETE /files/{id}
func HandleFilesDelete(fm *FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !fm.IsEnabled() {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "Filesystem is disabled",
			})
			return
		}

		// Extract file ID from path (StripPrefix already removed /files)
		fileID := strings.TrimPrefix(r.URL.Path, "/")
		if fileID == "" {
			http.Error(w, "File ID required", http.StatusBadRequest)
			return
		}

		// Delete file
		err := fm.Delete(r.Context(), fileID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				sendJSON(w, http.StatusNotFound, map[string]interface{}{
					"success": false,
					"error":   "File not found",
				})
				return
			}
			log.Printf("Failed to delete file: %v", err)
			sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   "Failed to delete file",
			})
			return
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "File deleted successfully",
		})
	}
}

// HandleFilesStats handles GET /files/stats
// Query params:
//   - thread_id: Get stats for a specific thread
//   - global=true: Get global stats with per-thread breakdown
func HandleFilesStats(fm *FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !fm.IsEnabled() {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "Filesystem is disabled",
			})
			return
		}

		// Check for thread_id parameter
		threadID := r.URL.Query().Get("thread_id")
		// Check for global breakdown parameter
		global := r.URL.Query().Get("global") == "true"

		if threadID != "" {
			// Get stats for specific thread
			stats, err := fm.GetStatsByThread(r.Context(), threadID)
			if err != nil {
				log.Printf("Failed to get thread stats: %v", err)
				sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
					"success": false,
					"error":   "Failed to get thread stats",
				})
				return
			}

			sendJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"stats":   stats,
			})
			return
		}

		if global {
			// Get global stats with per-thread breakdown
			stats, err := fm.GetGlobalStats(r.Context())
			if err != nil {
				log.Printf("Failed to get global stats: %v", err)
				sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
					"success": false,
					"error":   "Failed to get global stats",
				})
				return
			}

			sendJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"stats":   stats,
			})
			return
		}

		// Get basic stats (default)
		stats, err := fm.GetStats(r.Context())
		if err != nil {
			log.Printf("Failed to get stats: %v", err)
			sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   "Failed to get stats",
			})
			return
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"stats":   stats,
		})
	}
}

// HandleFilesCleanup handles POST /files/cleanup
func HandleFilesCleanup(fm *FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !fm.IsEnabled() {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "Filesystem is disabled",
			})
			return
		}

		totalCleaned := 0

		// Cleanup expired files
		if fm.config.RetentionDays > 0 {
			expired, err := fm.CleanupExpired(r.Context())
			if err != nil {
				log.Printf("Failed to cleanup expired files: %v", err)
			} else {
				totalCleaned += expired
			}
		}

		// Cleanup orphaned files
		if fm.config.CleanupOrphans {
			orphans, err := fm.CleanupOrphans(r.Context())
			if err != nil {
				log.Printf("Failed to cleanup orphaned files: %v", err)
			} else {
				totalCleaned += orphans
			}
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"success":       true,
			"files_cleaned": totalCleaned,
			"message":       fmt.Sprintf("Cleaned up %d files", totalCleaned),
		})
	}
}

// sendJSON sends a JSON response
func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}
