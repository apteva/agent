package stream

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// ExtractedMedia holds image or document data found in a tool result
type ExtractedMedia struct {
	MimeType   string
	Base64Data string
	Kind       string // "image" or "document"
}

// ExtractedImage is an alias for backward compatibility
type ExtractedImage = ExtractedMedia

// ProcessMCPToolResult processes a tool result and handles embedded images and documents
// based on vision, document support, and filesystem configuration.
//
// Behavior matrix:
//
//	Filesystem OFF + Vision OFF  → Strip base64 data, replace with "[media removed, N bytes]"
//	Filesystem OFF + Vision ON   → Keep base64, build image/document content blocks for LLM
//	Filesystem ON  + Vision OFF  → Store files via filesystem, replace with file references
//	Filesystem ON  + Vision ON   → Store files AND build content blocks
//
// The documentSupport flag controls whether PDF/document content blocks are built.
// Only Anthropic supports document content blocks natively.
//
// Returns:
//   - content: for DB storage and LLM conversation (string, or []interface{} with content blocks when vision is on)
//   - contentString: always a plain string for SSE output and logging
//   - extractedFileIDs: file IDs from filesystem storage (empty if filesystem disabled)
func ProcessMCPToolResult(
	result interface{},
	visionEnabled bool,
	documentSupport bool,
	fileProcessor FileProcessor,
	threadID string,
	toolName string,
) (content interface{}, contentString string, extractedFileIDs []string) {
	// Step 1: Detect embedded media (images + documents) before any modification
	media := findMediaInValue(result)

	// No media — fast path, return as JSON string (current behavior)
	if len(media) == 0 {
		resultJSON, _ := json.Marshal(result)
		s := string(resultJSON)
		return s, s, nil
	}

	// Separate images and documents
	var images, documents []ExtractedMedia
	for _, m := range media {
		if m.Kind == "document" {
			documents = append(documents, m)
		} else {
			images = append(images, m)
		}
	}

	if len(images) > 0 {
		log.Printf("🖼️  Found %d embedded image(s) in tool %s result", len(images), toolName)
	}
	if len(documents) > 0 {
		log.Printf("📄 Found %d embedded document(s) in tool %s result", len(documents), toolName)
	}

	// Step 2: Filesystem extraction (if enabled) — stores files, replaces with file references
	processedResult := result
	if fileProcessor != nil && fileProcessor.IsEnabled() {
		extracted, fileIDs, err := fileProcessor.ExtractImagesFromToolResult(result, threadID, toolName)
		if err != nil {
			log.Printf("⚠️  Failed to extract files from tool result: %v", err)
		} else if len(fileIDs) > 0 {
			processedResult = extracted
			extractedFileIDs = fileIDs
			log.Printf("📁 Stored %d file(s) from tool %s", len(fileIDs), toolName)
		}
	}

	// Step 3: Build output based on vision/document support settings
	if visionEnabled || (documentSupport && len(documents) > 0) {
		// Build the text portion: use filesystem-processed result (has file refs) or strip base64
		textResult := processedResult
		if len(extractedFileIDs) == 0 {
			// Filesystem didn't extract — strip base64 manually for the text portion
			textResult = stripBase64FromValue(processedResult)
		}
		textJSON, _ := json.Marshal(textResult)

		// Build content blocks: text + image(s) + document(s) in Anthropic-compatible format
		blocks := []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": string(textJSON),
			},
		}

		// Add image blocks (only if vision is enabled)
		if visionEnabled {
			for _, img := range images {
				blocks = append(blocks, map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": img.MimeType,
						"data":       img.Base64Data,
					},
				})
			}
		}

		// Add document blocks (only if document support is enabled)
		if documentSupport {
			for _, doc := range documents {
				blocks = append(blocks, map[string]interface{}{
					"type": "document",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": doc.MimeType,
						"data":       doc.Base64Data,
					},
				})
			}
		}

		imgCount := 0
		docCount := 0
		if visionEnabled {
			imgCount = len(images)
		}
		if documentSupport {
			docCount = len(documents)
		}
		log.Printf("📦 Built content: 1 text block + %d image block(s) + %d document block(s) for tool %s", imgCount, docCount, toolName)
		return blocks, string(textJSON), extractedFileIDs
	}

	// Step 4: No vision/document support — strip base64 to save tokens (unless filesystem already replaced them)
	if len(extractedFileIDs) == 0 {
		processedResult = stripBase64FromValue(processedResult)
	}
	resultJSON, _ := json.Marshal(processedResult)
	s := string(resultJSON)
	return s, s, extractedFileIDs
}

// findMediaInValue recursively walks a value and collects all embedded base64 images and documents
func findMediaInValue(value interface{}) []ExtractedMedia {
	var media []ExtractedMedia

	switch v := value.(type) {
	case map[string]interface{}:
		// Check if this map itself is a media object (image or document)
		if m, ok := extractMediaData(v); ok {
			media = append(media, m)
			return media // Don't recurse into a media object's children
		}
		// Recurse into values
		for _, val := range v {
			media = append(media, findMediaInValue(val)...)
		}

	case []interface{}:
		for _, item := range v {
			media = append(media, findMediaInValue(item)...)
		}
	}

	return media
}

// findImagesInValue is kept for backward compatibility — returns only images
func findImagesInValue(value interface{}) []ExtractedImage {
	all := findMediaInValue(value)
	var images []ExtractedImage
	for _, m := range all {
		if m.Kind == "image" {
			images = append(images, m)
		}
	}
	return images
}

// isDocumentMimeType checks if a MIME type represents a document (PDF, etc.)
func isDocumentMimeType(mimeType string) bool {
	return mimeType == "application/pdf"
}

// extractMediaData checks if a map contains base64 image or document data and extracts it.
// Returns the media and true if found, zero value and false otherwise.
func extractMediaData(m map[string]interface{}) (ExtractedMedia, bool) {
	// Pattern 1: Anthropic format — {"type": "image"|"document", "source": {"type": "base64", "data": "...", "media_type": "..."}}
	if t, ok := m["type"].(string); ok && (t == "image" || t == "document") {
		if source, ok := m["source"].(map[string]interface{}); ok {
			if sourceType, ok := source["type"].(string); ok && sourceType == "base64" {
				if data, ok := source["data"].(string); ok && len(data) > 100 {
					mimeType, _ := source["media_type"].(string)
					kind := t
					if kind == "image" && mimeType == "" {
						mimeType = "image/png"
					}
					if kind == "document" && mimeType == "" {
						mimeType = "application/pdf"
					}
					return ExtractedMedia{MimeType: mimeType, Base64Data: data, Kind: kind}, true
				}
			}
		}
	}

	// Pattern 2: Simple format — {"data": "<base64>", "mimeType": "image/..." or "application/pdf"}
	if data, hasData := m["data"].(string); hasData && len(data) > 100 {
		for _, field := range []string{"mimeType", "mime_type", "media_type", "content_type"} {
			if mimeType, ok := m[field].(string); ok {
				if strings.HasPrefix(mimeType, "image/") {
					return ExtractedMedia{MimeType: mimeType, Base64Data: data, Kind: "image"}, true
				}
				if isDocumentMimeType(mimeType) {
					return ExtractedMedia{MimeType: mimeType, Base64Data: data, Kind: "document"}, true
				}
			}
		}
	}

	// Pattern 3: base64 field — {"base64": "<data>", "mimeType": "image/..." or "application/pdf"}
	if data, hasData := m["base64"].(string); hasData && len(data) > 100 {
		for _, field := range []string{"mimeType", "mime_type", "media_type"} {
			if mimeType, ok := m[field].(string); ok {
				if strings.HasPrefix(mimeType, "image/") {
					return ExtractedMedia{MimeType: mimeType, Base64Data: data, Kind: "image"}, true
				}
				if isDocumentMimeType(mimeType) {
					return ExtractedMedia{MimeType: mimeType, Base64Data: data, Kind: "document"}, true
				}
			}
		}
	}

	// Pattern 4: Named image fields — {"image_data": "<base64>"} or {"screenshot": "<base64>"}
	for _, field := range []string{"image_data", "screenshot"} {
		if data, ok := m[field].(string); ok && len(data) > 100 {
			return ExtractedMedia{MimeType: "image/png", Base64Data: data, Kind: "image"}, true
		}
	}

	// Pattern 5: Named document fields — {"pdf_data": "<base64>"} or {"document_data": "<base64>"}
	for _, field := range []string{"pdf_data", "document_data"} {
		if data, ok := m[field].(string); ok && len(data) > 100 {
			mimeType := "application/pdf"
			if field == "document_data" {
				// Try to get specific mime type
				for _, mf := range []string{"mimeType", "mime_type", "media_type"} {
					if mt, ok := m[mf].(string); ok && isDocumentMimeType(mt) {
						mimeType = mt
						break
					}
				}
			}
			return ExtractedMedia{MimeType: mimeType, Base64Data: data, Kind: "document"}, true
		}
	}

	// Pattern 6: image field with media_type — {"image": "<base64>", "media_type": "image/png"}
	if data, ok := m["image"].(string); ok && len(data) > 100 {
		mimeType := "image/png"
		for _, field := range []string{"media_type", "mimeType", "mime_type"} {
			if mt, ok := m[field].(string); ok && strings.HasPrefix(mt, "image/") {
				mimeType = mt
				break
			}
		}
		return ExtractedMedia{MimeType: mimeType, Base64Data: data, Kind: "image"}, true
	}

	// Pattern 7: Data URL — {"url": "data:image/png;base64,..."} or {"url": "data:application/pdf;base64,..."}
	if url, ok := m["url"].(string); ok && strings.HasPrefix(url, "data:") && strings.Contains(url, ";base64,") {
		parts := strings.SplitN(url, ";base64,", 2)
		if len(parts) == 2 && len(parts[1]) > 100 {
			mimeType := strings.TrimPrefix(parts[0], "data:")
			kind := "image"
			if isDocumentMimeType(mimeType) {
				kind = "document"
			}
			return ExtractedMedia{MimeType: mimeType, Base64Data: parts[1], Kind: kind}, true
		}
	}

	return ExtractedMedia{}, false
}

// extractImageData is kept for backward compatibility — wraps extractMediaData
func extractImageData(m map[string]interface{}) (ExtractedImage, bool) {
	media, ok := extractMediaData(m)
	if !ok {
		return ExtractedImage{}, false
	}
	return media, true
}

// stripBase64FromValue recursively walks a value and replaces base64 media data with placeholders
func stripBase64FromValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return stripBase64FromMap(v)
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = stripBase64FromValue(item)
		}
		return result
	default:
		return value
	}
}

// stripBase64FromMap processes a map, replacing base64 media data with a placeholder
func stripBase64FromMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Check if this map is a media object (image or document) — replace the whole thing
	if media, isMedia := extractMediaData(m); isMedia {
		// Find the data size
		dataSize := len(media.Base64Data)
		label := media.Kind
		if media.MimeType != "" {
			label = media.MimeType
		}

		return map[string]interface{}{
			"type":    media.Kind + "_removed",
			"message": fmt.Sprintf("[%s removed, %d bytes base64]", label, dataSize),
		}
	}

	// Not a media object — recurse into values
	for key, val := range m {
		result[key] = stripBase64FromValue(val)
	}
	return result
}
