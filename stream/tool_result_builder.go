package stream

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// ExtractedImage holds image data found in an MCP tool result
type ExtractedImage struct {
	MimeType   string
	Base64Data string
}

// ProcessMCPToolResult processes an MCP tool result and handles embedded images
// based on vision and filesystem configuration.
//
// Behavior matrix:
//
//	Filesystem OFF + Vision OFF  → Strip base64 data, replace with "[image removed, N bytes]"
//	Filesystem OFF + Vision ON   → Keep base64, build image content blocks for LLM vision
//	Filesystem ON  + Vision OFF  → Store images via filesystem, replace with file references
//	Filesystem ON  + Vision ON   → Store images AND build vision content blocks
//
// Returns:
//   - content: for DB storage and LLM conversation (string, or []interface{} with content blocks when vision is on)
//   - contentString: always a plain string for SSE output and logging
//   - extractedFileIDs: file IDs from filesystem storage (empty if filesystem disabled)
func ProcessMCPToolResult(
	result interface{},
	visionEnabled bool,
	fileProcessor FileProcessor,
	threadID string,
	toolName string,
) (content interface{}, contentString string, extractedFileIDs []string) {
	// Step 1: Detect embedded images before any modification
	images := findImagesInValue(result)

	// No images — fast path, return as JSON string (current behavior)
	if len(images) == 0 {
		resultJSON, _ := json.Marshal(result)
		s := string(resultJSON)
		return s, s, nil
	}

	log.Printf("🖼️  Found %d embedded image(s) in MCP tool %s result", len(images), toolName)

	// Step 2: Filesystem extraction (if enabled) — stores images, replaces with file references
	processedResult := result
	if fileProcessor != nil && fileProcessor.IsEnabled() {
		extracted, fileIDs, err := fileProcessor.ExtractImagesFromToolResult(result, threadID, toolName)
		if err != nil {
			log.Printf("⚠️  Failed to extract images from MCP tool result: %v", err)
		} else if len(fileIDs) > 0 {
			processedResult = extracted
			extractedFileIDs = fileIDs
			log.Printf("📁 Stored %d file(s) from MCP tool %s", len(fileIDs), toolName)
		}
	}

	// Step 3: Build output based on vision setting
	if visionEnabled {
		// Build the text portion: use filesystem-processed result (has file refs) or strip base64
		textResult := processedResult
		if len(extractedFileIDs) == 0 {
			// Filesystem didn't extract — strip base64 manually for the text portion
			textResult = stripBase64FromValue(processedResult)
		}
		textJSON, _ := json.Marshal(textResult)

		// Build content blocks: text + image(s) in Anthropic-compatible format
		blocks := []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": string(textJSON),
			},
		}
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

		log.Printf("👁️  Built vision content: 1 text block + %d image block(s) for tool %s", len(images), toolName)
		return blocks, string(textJSON), extractedFileIDs
	}

	// Step 4: Vision disabled — strip base64 to save tokens (unless filesystem already replaced them)
	if len(extractedFileIDs) == 0 {
		processedResult = stripBase64FromValue(processedResult)
	}
	resultJSON, _ := json.Marshal(processedResult)
	s := string(resultJSON)
	return s, s, extractedFileIDs
}

// findImagesInValue recursively walks a value and collects all embedded base64 images
func findImagesInValue(value interface{}) []ExtractedImage {
	var images []ExtractedImage

	switch v := value.(type) {
	case map[string]interface{}:
		// Check if this map itself is an image object
		if img, ok := extractImageData(v); ok {
			images = append(images, img)
			return images // Don't recurse into an image object's children
		}
		// Recurse into values
		for _, val := range v {
			images = append(images, findImagesInValue(val)...)
		}

	case []interface{}:
		for _, item := range v {
			images = append(images, findImagesInValue(item)...)
		}
	}

	return images
}

// extractImageData checks if a map contains base64 image data and extracts it.
// Returns the image and true if found, zero value and false otherwise.
func extractImageData(m map[string]interface{}) (ExtractedImage, bool) {
	// Pattern 1: Anthropic format — {"type": "image", "source": {"type": "base64", "data": "...", "media_type": "..."}}
	if t, ok := m["type"].(string); ok && t == "image" {
		if source, ok := m["source"].(map[string]interface{}); ok {
			if sourceType, ok := source["type"].(string); ok && sourceType == "base64" {
				if data, ok := source["data"].(string); ok && len(data) > 100 {
					mimeType, _ := source["media_type"].(string)
					if mimeType == "" {
						mimeType = "image/png"
					}
					return ExtractedImage{MimeType: mimeType, Base64Data: data}, true
				}
			}
		}
	}

	// Pattern 2: Simple format — {"data": "<base64>", "mimeType": "image/..."} (Gemini inlineData)
	if data, hasData := m["data"].(string); hasData && len(data) > 100 {
		for _, field := range []string{"mimeType", "mime_type", "media_type", "content_type"} {
			if mimeType, ok := m[field].(string); ok && strings.HasPrefix(mimeType, "image/") {
				return ExtractedImage{MimeType: mimeType, Base64Data: data}, true
			}
		}
	}

	// Pattern 3: base64 field — {"base64": "<data>", "mimeType": "image/..."}
	if data, hasData := m["base64"].(string); hasData && len(data) > 100 {
		for _, field := range []string{"mimeType", "mime_type", "media_type"} {
			if mimeType, ok := m[field].(string); ok && strings.HasPrefix(mimeType, "image/") {
				return ExtractedImage{MimeType: mimeType, Base64Data: data}, true
			}
		}
	}

	// Pattern 4: Named image fields — {"image_data": "<base64>"} or {"screenshot": "<base64>"}
	for _, field := range []string{"image_data", "screenshot"} {
		if data, ok := m[field].(string); ok && len(data) > 100 {
			return ExtractedImage{MimeType: "image/png", Base64Data: data}, true
		}
	}

	// Pattern 5: image field with media_type — {"image": "<base64>", "media_type": "image/png"}
	if data, ok := m["image"].(string); ok && len(data) > 100 {
		mimeType := "image/png"
		for _, field := range []string{"media_type", "mimeType", "mime_type"} {
			if mt, ok := m[field].(string); ok && strings.HasPrefix(mt, "image/") {
				mimeType = mt
				break
			}
		}
		return ExtractedImage{MimeType: mimeType, Base64Data: data}, true
	}

	// Pattern 6: Data URL — {"url": "data:image/png;base64,..."}
	if url, ok := m["url"].(string); ok && strings.HasPrefix(url, "data:image/") && strings.Contains(url, ";base64,") {
		parts := strings.SplitN(url, ";base64,", 2)
		if len(parts) == 2 && len(parts[1]) > 100 {
			mimeType := strings.TrimPrefix(parts[0], "data:")
			return ExtractedImage{MimeType: mimeType, Base64Data: parts[1]}, true
		}
	}

	return ExtractedImage{}, false
}

// stripBase64FromValue recursively walks a value and replaces base64 image data with placeholders
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

// stripBase64FromMap processes a map, replacing base64 image data with a placeholder
func stripBase64FromMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Check if this map is an image object — replace the whole thing
	if _, isImage := extractImageData(m); isImage {
		// Figure out the mime type for the placeholder
		mimeType := "image"
		for _, field := range []string{"mimeType", "mime_type", "media_type", "content_type"} {
			if mt, ok := m[field].(string); ok {
				mimeType = mt
				break
			}
		}
		// Find the data size
		dataSize := 0
		for _, field := range []string{"data", "base64", "image_data", "screenshot", "image"} {
			if data, ok := m[field].(string); ok && len(data) > 100 {
				dataSize = len(data)
				break
			}
		}
		// Check data URL
		if dataSize == 0 {
			if url, ok := m["url"].(string); ok && strings.Contains(url, ";base64,") {
				parts := strings.SplitN(url, ";base64,", 2)
				if len(parts) == 2 {
					dataSize = len(parts[1])
				}
			}
		}
		// Check Anthropic source format
		if dataSize == 0 {
			if source, ok := m["source"].(map[string]interface{}); ok {
				if data, ok := source["data"].(string); ok {
					dataSize = len(data)
				}
				if mt, ok := source["media_type"].(string); ok {
					mimeType = mt
				}
			}
		}

		return map[string]interface{}{
			"type":    "image_removed",
			"message": fmt.Sprintf("[%s image removed, %d bytes base64]", mimeType, dataSize),
		}
	}

	// Not an image — recurse into values
	for key, val := range m {
		result[key] = stripBase64FromValue(val)
	}
	return result
}
