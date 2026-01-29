package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
)

// ContentSource represents a source for image/document content
type ContentSource struct {
	Type      string `json:"type"`                // "base64", "url", "file"
	MediaType string `json:"media_type,omitempty"` // "image/jpeg", "application/pdf", etc.
	Data      string `json:"data,omitempty"`      // For base64
	URL       string `json:"url,omitempty"`       // For URL
	FileID    string `json:"file_id,omitempty"`   // For file reference
}

// ContentBlock represents a content block in a message
type ContentBlock struct {
	Type      string                 `json:"type"`                    // "text", "image", "document", "tool_use", "tool_result"
	Text      string                 `json:"text,omitempty"`          // for text blocks
	Source    *ContentSource         `json:"source,omitempty"`        // for image and document blocks
	ID        string                 `json:"id,omitempty"`            // for tool_use
	Name      string                 `json:"name,omitempty"`          // for tool_use
	Input     map[string]interface{} `json:"input,omitempty"`         // for tool_use
	ToolUseID string                 `json:"tool_use_id,omitempty"`   // for tool_result
	Content   interface{}            `json:"content,omitempty"`       // for tool_result
	IsError   bool                   `json:"is_error,omitempty"`      // for tool_result
}

// ProcessContentBlocks extracts base64 content and stores as files
func (fm *FileManager) ProcessContentBlocks(blocks []ContentBlock, threadID, messageID, source string) ([]ContentBlock, error) {
	if !fm.IsEnabled() || !fm.config.AutoExtract {
		return blocks, nil
	}

	processed := make([]ContentBlock, 0, len(blocks))

	for _, block := range blocks {
		// Check if this is a base64 image/document
		if block.Source != nil && block.Source.Type == "base64" {
			base64Data := block.Source.Data

			// Only extract if above minimum size
			if len(base64Data) > fm.config.MinExtractSize {
				// Decode and store file
				fileID, err := fm.storeBase64Content(base64Data, block.Source.MediaType, block.Type, threadID, messageID, source)
				if err != nil {
					log.Printf("⚠️  Failed to store file: %v", err)
					processed = append(processed, block) // Keep original
					continue
				}

				// Generate public URL for the file
				publicURL, urlErr := fm.GetPublicURL(fileID)
				if urlErr != nil {
					log.Printf("⚠️  Could not generate public URL for file %s: %v", fileID, urlErr)
					publicURL = "" // Continue without URL
				}

				// Replace with file reference + public URL
				block.Source = &ContentSource{
					Type:      "file",
					FileID:    fileID,
					URL:       publicURL, // Add public URL
					MediaType: block.Source.MediaType,
				}

				if publicURL != "" {
					log.Printf("✅ Extracted file: %s (was %d bytes base64, now file reference with URL: %s)", fileID, len(base64Data), publicURL)
				} else {
					log.Printf("✅ Extracted file: %s (was %d bytes base64, now file reference)", fileID, len(base64Data))
				}
			}
		}

		processed = append(processed, block)
	}

	return processed, nil
}

// ExpandFileReferences converts file references back to base64 for LLM calls
func (fm *FileManager) ExpandFileReferences(blocks []ContentBlock) ([]ContentBlock, error) {
	if !fm.IsEnabled() {
		return blocks, nil
	}

	expanded := make([]ContentBlock, 0, len(blocks))

	for _, block := range blocks {
		// Check if this is a file reference
		if block.Source != nil && block.Source.Type == "file" && block.Source.FileID != "" {
			// Retrieve file record to get mime_type as fallback
			file, err := fm.Get(context.Background(), block.Source.FileID)
			if err != nil {
				log.Printf("⚠️  Failed to retrieve file %s: %v", block.Source.FileID, err)
				// Skip block or return placeholder
				continue
			}

			// Get base64 data
			base64Data := base64.StdEncoding.EncodeToString(file.Data)

			// Use stored MediaType, fallback to file record's MimeType
			mediaType := block.Source.MediaType
			if mediaType == "" && file != nil {
				mediaType = file.MimeType
			}

			// Replace with base64 for LLM
			block.Source = &ContentSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      base64Data,
			}
		}

		expanded = append(expanded, block)
	}

	return expanded, nil
}

// StripFileReferences removes file references from content blocks
// Used for context management when stripping old images
func (fm *FileManager) StripFileReferences(blocks []ContentBlock) []ContentBlock {
	if !fm.IsEnabled() {
		return blocks
	}

	stripped := make([]ContentBlock, 0)

	for _, block := range blocks {
		// Skip image/document blocks that reference files
		if (block.Type == "image" || block.Type == "document") &&
			block.Source != nil && block.Source.Type == "file" {
			// Replace with text placeholder
			stripped = append(stripped, ContentBlock{
				Type: "text",
				Text: fmt.Sprintf("[%s removed from context]", block.Type),
			})
			continue
		}

		stripped = append(stripped, block)
	}

	return stripped
}

// storeBase64Content decodes base64 and stores the file
func (fm *FileManager) storeBase64Content(base64Data, mimeType, contentType, threadID, messageID, source string) (string, error) {
	// Decode base64
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("invalid base64 data: %w", err)
	}

	// Calculate checksum
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	// Determine file type
	fileType := DetermineFileType(mimeType)

	// Check if allowed type
	if !fm.isAllowedType(fileType) {
		return "", fmt.Errorf("file type '%s' not allowed (mime: %s)", fileType, mimeType)
	}

	// Generate filename
	filename := generateFilename(mimeType, checksum)

	// Create file object
	file := &File{
		ThreadID:  threadID,
		MessageID: messageID,
		Filename:  filename,
		MimeType:  mimeType,
		FileType:  fileType,
		SizeBytes: int64(len(data)),
		Checksum:  checksum,
		Data:      data,
		Metadata:  make(map[string]interface{}),
		Source:    source,
	}

	// Add content type to metadata
	if contentType != "" {
		file.Metadata["content_type"] = contentType
	}

	// Store file
	return fm.Store(context.Background(), file)
}

// generateFilename generates a filename from MIME type and checksum
func generateFilename(mimeType, checksum string) string {
	ext := getExtensionFromMimeType(mimeType)
	return fmt.Sprintf("%s%s", checksum[:12], ext)
}

// getExtensionFromMimeType returns file extension for MIME type
func getExtensionFromMimeType(mimeType string) string {
	switch mimeType {
	// Images
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "image/bmp":
		return ".bmp"
	case "image/tiff":
		return ".tiff"
	// Videos
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "video/x-msvideo":
		return ".avi"
	case "video/x-matroska":
		return ".mkv"
	// Audio
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/aac":
		return ".aac"
	case "audio/flac":
		return ".flac"
	case "audio/webm":
		return ".weba"
	// Documents
	case "application/pdf":
		return ".pdf"
	case "application/msword":
		return ".doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.ms-excel":
		return ".xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "application/vnd.ms-powerpoint":
		return ".ppt"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return ".pptx"
	// Text
	case "text/plain":
		return ".txt"
	case "text/html":
		return ".html"
	case "text/css":
		return ".css"
	case "text/javascript", "application/javascript":
		return ".js"
	case "application/json":
		return ".json"
	case "text/xml", "application/xml":
		return ".xml"
	case "text/csv":
		return ".csv"
	case "text/markdown":
		return ".md"
	// Archives
	case "application/zip":
		return ".zip"
	case "application/gzip":
		return ".gz"
	case "application/x-tar":
		return ".tar"
	default:
		// Try to extract from mime type
		parts := strings.Split(mimeType, "/")
		if len(parts) == 2 {
			return "." + parts[1]
		}
		return ""
	}
}

// HasFileReferences checks if content blocks contain file references
func HasFileReferences(blocks []ContentBlock) bool {
	for _, block := range blocks {
		if block.Source != nil && block.Source.Type == "file" {
			return true
		}
	}
	return false
}

// ExtractImagesFromToolResult recursively finds and extracts base64 images from any nested structure
// Returns the modified result with images replaced by file references, and a list of extracted file IDs
func (fm *FileManager) ExtractImagesFromToolResult(result interface{}, threadID, source string) (interface{}, []string, error) {
	if !fm.IsEnabled() || !fm.config.AutoExtract {
		return result, nil, nil
	}

	var extractedFiles []string
	processed, err := fm.processNestedValue(result, threadID, source, &extractedFiles)
	if err != nil {
		return result, nil, err
	}

	return processed, extractedFiles, nil
}

// processNestedValue recursively processes a value looking for base64 images
func (fm *FileManager) processNestedValue(value interface{}, threadID, source string, extractedFiles *[]string) (interface{}, error) {
	switch v := value.(type) {
	case map[string]interface{}:
		return fm.processNestedMap(v, threadID, source, extractedFiles)
	case []interface{}:
		return fm.processNestedArray(v, threadID, source, extractedFiles)
	default:
		return value, nil
	}
}

// processNestedMap processes a map looking for image patterns
func (fm *FileManager) processNestedMap(m map[string]interface{}, threadID, source string, extractedFiles *[]string) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// Check if this map looks like an image object
	if fm.looksLikeImageObject(m) {
		// Try to extract and store the image
		fileRef, err := fm.extractImageFromMap(m, threadID, source)
		if err != nil {
			log.Printf("⚠️  Failed to extract image from map: %v", err)
			// Return original on error
			for k, v := range m {
				result[k] = v
			}
			return result, nil
		}

		if fileRef != nil {
			*extractedFiles = append(*extractedFiles, fileRef["file_id"].(string))
			return fileRef, nil
		}
	}

	// Recursively process all values
	for key, val := range m {
		processed, err := fm.processNestedValue(val, threadID, source, extractedFiles)
		if err != nil {
			result[key] = val // Keep original on error
		} else {
			result[key] = processed
		}
	}

	return result, nil
}

// processNestedArray processes an array looking for image objects
func (fm *FileManager) processNestedArray(arr []interface{}, threadID, source string, extractedFiles *[]string) ([]interface{}, error) {
	result := make([]interface{}, 0, len(arr))

	for _, item := range arr {
		processed, err := fm.processNestedValue(item, threadID, source, extractedFiles)
		if err != nil {
			result = append(result, item) // Keep original on error
		} else {
			result = append(result, processed)
		}
	}

	return result, nil
}

// looksLikeBase64FileObject checks if a map contains base64-encoded file data (images, videos, audio, documents, etc.)
func (fm *FileManager) looksLikeImageObject(m map[string]interface{}) bool {
	// Pattern 1: Anthropic format - {"type": "image", "source": {"type": "base64", "data": "..."}}
	if t, ok := m["type"].(string); ok && (t == "image" || t == "video" || t == "audio" || t == "document") {
		if source, ok := m["source"].(map[string]interface{}); ok {
			if sourceType, ok := source["type"].(string); ok && sourceType == "base64" {
				if data, ok := source["data"].(string); ok && len(data) > 100 {
					return true
				}
			}
		}
	}

	// Pattern 2: Simple format - {"data": "<base64>", "mimeType": "..."}
	// Now supports all media types, not just images
	if data, hasData := m["data"].(string); hasData && len(data) > 100 {
		if mimeType, hasMime := m["mimeType"].(string); hasMime && isExtractableMimeType(mimeType) {
			return true
		}
		// Also check mime_type (snake_case)
		if mimeType, hasMime := m["mime_type"].(string); hasMime && isExtractableMimeType(mimeType) {
			return true
		}
		// Also check media_type
		if mimeType, hasMime := m["media_type"].(string); hasMime && isExtractableMimeType(mimeType) {
			return true
		}
		// Also check content_type
		if mimeType, hasMime := m["content_type"].(string); hasMime && isExtractableMimeType(mimeType) {
			return true
		}
	}

	// Pattern 3: Format indicator - {"data": "<base64>", "format": "base64"}
	if data, hasData := m["data"].(string); hasData && len(data) > 100 {
		if format, hasFormat := m["format"].(string); hasFormat && format == "base64" {
			return true
		}
	}

	// Pattern 4: base64 field directly - {"base64": "<data>", "mimeType": "..."}
	if data, hasData := m["base64"].(string); hasData && len(data) > 100 {
		if mimeType, hasMime := m["mimeType"].(string); hasMime && isExtractableMimeType(mimeType) {
			return true
		}
		if mimeType, hasMime := m["mime_type"].(string); hasMime && isExtractableMimeType(mimeType) {
			return true
		}
	}

	// Pattern 5: Specific field names for different media types
	// image_data, video_data, audio_data, file_data, document_data
	for _, field := range []string{"image_data", "video_data", "audio_data", "file_data", "document_data", "pdf_data"} {
		if data, hasData := m[field].(string); hasData && len(data) > 100 {
			return true
		}
	}

	// Pattern 6: screenshot field - {"screenshot": "<base64>", ...}
	if data, hasData := m["screenshot"].(string); hasData && len(data) > 100 {
		return true
	}

	// Pattern 7: Data URL format - {"format": "dataUrl", "url": "data:image/png;base64,..."}
	if format, hasFormat := m["format"].(string); hasFormat && format == "dataUrl" {
		if url, hasURL := m["url"].(string); hasURL && strings.HasPrefix(url, "data:") && strings.Contains(url, ";base64,") {
			return true
		}
	}

	// Pattern 8: Just a url field with data URL - {"url": "data:image/png;base64,..."}
	if url, hasURL := m["url"].(string); hasURL && strings.HasPrefix(url, "data:") && strings.Contains(url, ";base64,") && len(url) > 100 {
		return true
	}

	// Pattern 9: image field with media_type - {"image": "<base64>", "media_type": "image/png"}
	// Used by high_quality_screenshot tool
	if data, hasData := m["image"].(string); hasData && len(data) > 100 {
		if mimeType, hasMime := m["media_type"].(string); hasMime && isExtractableMimeType(mimeType) {
			return true
		}
	}

	return false
}

// isExtractableMimeType checks if a MIME type should be extracted
func isExtractableMimeType(mimeType string) bool {
	// Images
	if strings.HasPrefix(mimeType, "image/") {
		return true
	}
	// Videos
	if strings.HasPrefix(mimeType, "video/") {
		return true
	}
	// Audio
	if strings.HasPrefix(mimeType, "audio/") {
		return true
	}
	// PDFs and documents
	if mimeType == "application/pdf" ||
		mimeType == "application/msword" ||
		strings.Contains(mimeType, "officedocument") ||
		strings.Contains(mimeType, "spreadsheet") ||
		strings.Contains(mimeType, "presentation") {
		return true
	}
	// Archives
	if mimeType == "application/zip" ||
		mimeType == "application/gzip" ||
		mimeType == "application/x-tar" {
		return true
	}
	return false
}

// extractFileFromMap extracts base64 file data and stores it, returning a file reference
// Supports images, videos, audio, PDFs, documents, and other binary files
func (fm *FileManager) extractImageFromMap(m map[string]interface{}, threadID, source string) (map[string]interface{}, error) {
	var base64Data string
	var mimeType string = "application/octet-stream" // Default to binary

	// Extract data based on different patterns

	// Pattern 1: Anthropic format
	if sourceMap, ok := m["source"].(map[string]interface{}); ok {
		if data, ok := sourceMap["data"].(string); ok {
			base64Data = data
		}
		if mt, ok := sourceMap["media_type"].(string); ok {
			mimeType = mt
		}
	}

	// Pattern 2-4: Direct data field
	if base64Data == "" {
		if data, ok := m["data"].(string); ok && len(data) > 100 {
			base64Data = data
		}
	}

	// Pattern 4: base64 field
	if base64Data == "" {
		if data, ok := m["base64"].(string); ok && len(data) > 100 {
			base64Data = data
		}
	}

	// Pattern 5: Various *_data fields for different media types
	if base64Data == "" {
		for _, field := range []string{"image_data", "video_data", "audio_data", "file_data", "document_data", "pdf_data"} {
			if data, ok := m[field].(string); ok && len(data) > 100 {
				base64Data = data
				// Infer mime type from field name if not set
				if mimeType == "application/octet-stream" {
					switch field {
					case "image_data":
						mimeType = "image/png"
					case "video_data":
						mimeType = "video/mp4"
					case "audio_data":
						mimeType = "audio/mpeg"
					case "pdf_data":
						mimeType = "application/pdf"
					}
				}
				break
			}
		}
	}

	// Pattern 6: screenshot field
	if base64Data == "" {
		if data, ok := m["screenshot"].(string); ok && len(data) > 100 {
			base64Data = data
			if mimeType == "application/octet-stream" {
				mimeType = "image/png"
			}
		}
	}

	// Pattern 7 & 8: Data URL format - {"url": "data:image/png;base64,..."}
	if base64Data == "" {
		if url, ok := m["url"].(string); ok && strings.HasPrefix(url, "data:") && strings.Contains(url, ";base64,") {
			// Parse data URL: data:image/png;base64,iVBORw0KGgo...
			parts := strings.SplitN(url, ";base64,", 2)
			if len(parts) == 2 {
				base64Data = parts[1]
				// Extract mime type from data URL (e.g., "data:image/png" -> "image/png")
				mimeFromURL := strings.TrimPrefix(parts[0], "data:")
				if mimeFromURL != "" {
					mimeType = mimeFromURL
				}
			}
		}
	}

	// Pattern 9: image field - {"image": "<base64>", "media_type": "image/png"}
	// Used by high_quality_screenshot tool
	if base64Data == "" {
		if data, ok := m["image"].(string); ok && len(data) > 100 {
			base64Data = data
			if mimeType == "application/octet-stream" {
				mimeType = "image/png"
			}
		}
	}

	// Extract mime type from various fields (override default if present)
	if mt, ok := m["mimeType"].(string); ok && mt != "" {
		mimeType = mt
	} else if mt, ok := m["mime_type"].(string); ok && mt != "" {
		mimeType = mt
	} else if mt, ok := m["media_type"].(string); ok && mt != "" {
		mimeType = mt
	} else if mt, ok := m["content_type"].(string); ok && mt != "" {
		mimeType = mt
	} else if mt, ok := m["type"].(string); ok && (strings.HasPrefix(mt, "image/") ||
		strings.HasPrefix(mt, "video/") ||
		strings.HasPrefix(mt, "audio/") ||
		strings.HasPrefix(mt, "application/")) {
		mimeType = mt
	}

	// Check minimum size
	if len(base64Data) < fm.config.MinExtractSize {
		return nil, nil // Not big enough to extract
	}

	// Determine content type label for logging
	contentTypeLabel := "file"
	if strings.HasPrefix(mimeType, "image/") {
		contentTypeLabel = "image"
	} else if strings.HasPrefix(mimeType, "video/") {
		contentTypeLabel = "video"
	} else if strings.HasPrefix(mimeType, "audio/") {
		contentTypeLabel = "audio"
	} else if mimeType == "application/pdf" {
		contentTypeLabel = "PDF"
	} else if strings.Contains(mimeType, "document") || strings.Contains(mimeType, "spreadsheet") {
		contentTypeLabel = "document"
	}

	// Store the file
	fileID, err := fm.storeBase64Content(base64Data, mimeType, "tool_result_"+contentTypeLabel, threadID, "", source)
	if err != nil {
		return nil, err
	}

	// Generate public URL
	publicURL, urlErr := fm.GetPublicURL(fileID)
	if urlErr != nil {
		log.Printf("⚠️  Could not generate public URL for extracted %s %s: %v", contentTypeLabel, fileID, urlErr)
		publicURL = ""
	}

	log.Printf("✅ Extracted %s from tool result: %s (was %d bytes base64, mime: %s)", contentTypeLabel, fileID, len(base64Data), mimeType)

	// Return file reference in a format that's useful for the conversation
	fileRef := map[string]interface{}{
		"type":       "file_reference",
		"file_id":    fileID,
		"media_type": mimeType,
		"extracted":  true,
	}
	if publicURL != "" {
		fileRef["url"] = publicURL
	}

	return fileRef, nil
}
