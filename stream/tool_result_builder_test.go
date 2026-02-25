package stream

import (
	"encoding/json"
	"strings"
	"testing"
)

// fakeBase64 generates a fake base64 string of at least 101 chars (above the 100 min threshold)
func fakeBase64(size int) string {
	if size < 101 {
		size = 101
	}
	b := make([]byte, size)
	for i := range b {
		b[i] = 'A' + byte(i%26)
	}
	return string(b)
}

// --- Mock FileProcessor ---

type mockFileProcessor struct {
	enabled    bool
	storedIDs  []string
	calledWith interface{}
}

func (m *mockFileProcessor) IsEnabled() bool { return m.enabled }

func (m *mockFileProcessor) ProcessContentBlocks(blocks []ContentBlock, threadID, messageID, source string) ([]ContentBlock, error) {
	return blocks, nil
}

func (m *mockFileProcessor) ExpandFileReferences(blocks []ContentBlock) ([]ContentBlock, error) {
	return blocks, nil
}

func (m *mockFileProcessor) ExtractImagesFromToolResult(result interface{}, threadID, source string) (interface{}, []string, error) {
	m.calledWith = result
	// Walk the result and replace image data with file references
	replaced := replaceImagesWithRefs(result, m.storedIDs, 0)
	return replaced, m.storedIDs, nil
}

// replaceImagesWithRefs recursively replaces image data with file references (mock behavior)
func replaceImagesWithRefs(value interface{}, fileIDs []string, idx int) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		// Check if this is an image object (has "data" + "mimeType" like Gemini inlineData)
		if data, ok := v["data"].(string); ok && len(data) > 100 {
			for _, field := range []string{"mimeType", "mime_type", "media_type"} {
				if mt, ok := v[field].(string); ok && strings.HasPrefix(mt, "image/") {
					fileID := "file-001"
					if idx < len(fileIDs) {
						fileID = fileIDs[idx]
					}
					return map[string]interface{}{
						"type":       "file_reference",
						"file_id":    fileID,
						"url":        "https://example.com/files/" + fileID,
						"media_type": mt,
						"extracted":  true,
					}
				}
			}
		}
		// Check Anthropic format
		if t, ok := v["type"].(string); ok && t == "image" {
			if source, ok := v["source"].(map[string]interface{}); ok {
				if data, ok := source["data"].(string); ok && len(data) > 100 {
					fileID := "file-001"
					if idx < len(fileIDs) {
						fileID = fileIDs[idx]
					}
					mt, _ := source["media_type"].(string)
					return map[string]interface{}{
						"type":       "file_reference",
						"file_id":    fileID,
						"url":        "https://example.com/files/" + fileID,
						"media_type": mt,
						"extracted":  true,
					}
				}
			}
		}
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = replaceImagesWithRefs(val, fileIDs, idx)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = replaceImagesWithRefs(item, fileIDs, i)
		}
		return result
	default:
		return value
	}
}

// --- Test helpers ---

func mustParseJSON(t *testing.T, s string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("failed to parse JSON: %v\ninput: %s", err, s)
	}
	return v
}

func assertContentString(t *testing.T, contentString string, checks ...string) {
	t.Helper()
	for _, check := range checks {
		if !strings.Contains(contentString, check) {
			t.Errorf("contentString missing %q\ngot: %s", check, truncateForTest(contentString, 500))
		}
	}
}

func assertContentStringExcludes(t *testing.T, contentString string, excludes ...string) {
	t.Helper()
	for _, excl := range excludes {
		if strings.Contains(contentString, excl) {
			t.Errorf("contentString should NOT contain %q\ngot: %s", excl, truncateForTest(contentString, 500))
		}
	}
}

func truncateForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ============================================================
// Test: No images — fast path
// ============================================================

func TestProcessMCPToolResult_NoImages(t *testing.T) {
	result := map[string]interface{}{
		"success": true,
		"message": "Notification sent",
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}
	// Content should be a string (JSON-encoded)
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	if s != contentStr {
		t.Errorf("content and contentString should match for no-image case")
	}
	assertContentString(t, contentStr, `"success":true`, `"message":"Notification sent"`)
}

func TestProcessMCPToolResult_NoImages_StringInput(t *testing.T) {
	// When input is a raw string (not a map), should return it as JSON-encoded string
	result := "plain text result"

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	if s != contentStr {
		t.Errorf("content and contentString should match")
	}
	// json.Marshal wraps strings in quotes
	if s != `"plain text result"` {
		t.Errorf("expected quoted string, got: %s", s)
	}
}

// ============================================================
// Matrix: Filesystem OFF + Vision OFF → strip base64
// ============================================================

func TestProcessMCPToolResult_NoVision_NoFS_GeminiInlineData(t *testing.T) {
	// Pattern 2: Gemini inlineData format
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"success": true,
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{
							"inlineData": map[string]interface{}{
								"data":     b64,
								"mimeType": "image/png",
							},
						},
					},
				},
			},
		},
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "gemini-generate")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}
	// Content should be a string (vision off → no content blocks)
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	if s != contentStr {
		t.Errorf("content and contentString should match when vision is off")
	}
	// Base64 data should be stripped
	assertContentStringExcludes(t, contentStr, b64)
	// Should contain the removal placeholder
	assertContentString(t, contentStr, "image_removed", "image/png")
}

func TestProcessMCPToolResult_NoVision_NoFS_AnthropicFormat(t *testing.T) {
	// Pattern 1: Anthropic format
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"type": "image",
		"source": map[string]interface{}{
			"type":       "base64",
			"media_type": "image/jpeg",
			"data":       b64,
		},
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	assertContentStringExcludes(t, s, b64)
	assertContentString(t, contentStr, "image_removed")
}

func TestProcessMCPToolResult_NoVision_NoFS_Base64Field(t *testing.T) {
	// Pattern 3: base64 field
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"base64":   b64,
		"mimeType": "image/webp",
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	assertContentStringExcludes(t, s, b64)
	assertContentString(t, contentStr, "image_removed", "image/webp")
}

func TestProcessMCPToolResult_NoVision_NoFS_ImageDataField(t *testing.T) {
	// Pattern 4: image_data field
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"image_data": b64,
		"width":      512,
		"height":     512,
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	assertContentStringExcludes(t, s, b64)
	assertContentString(t, contentStr, "image_removed")
}

func TestProcessMCPToolResult_NoVision_NoFS_ScreenshotField(t *testing.T) {
	// Pattern 4: screenshot field
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"screenshot": b64,
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	assertContentStringExcludes(t, s, b64)
	assertContentString(t, contentStr, "image_removed")
}

func TestProcessMCPToolResult_NoVision_NoFS_ImageField(t *testing.T) {
	// Pattern 5: image field with media_type
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"image":      b64,
		"media_type": "image/gif",
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	assertContentStringExcludes(t, s, b64)
	assertContentString(t, contentStr, "image_removed", "image/gif")
}

func TestProcessMCPToolResult_NoVision_NoFS_DataURL(t *testing.T) {
	// Pattern 6: data URL
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"url": "data:image/png;base64," + b64,
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	assertContentStringExcludes(t, s, b64)
	assertContentString(t, contentStr, "image_removed")
}

// ============================================================
// Matrix: Filesystem OFF + Vision ON → keep base64, build image blocks
// ============================================================

func TestProcessMCPToolResult_Vision_NoFS_GeminiInlineData(t *testing.T) {
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"success": true,
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{
							"inlineData": map[string]interface{}{
								"data":     b64,
								"mimeType": "image/png",
							},
						},
					},
				},
			},
		},
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, true, false, nil, "thread-1", "gemini-generate")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs with no filesystem, got %v", fileIDs)
	}

	// Content should be []interface{} (content blocks) when vision is on
	blocks, ok := content.([]interface{})
	if !ok {
		t.Fatalf("expected content to be []interface{} (content blocks), got %T", content)
	}

	// Should have at least 2 blocks: 1 text + 1 image
	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 content blocks, got %d", len(blocks))
	}

	// First block should be text
	textBlock, ok := blocks[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected text block to be map, got %T", blocks[0])
	}
	if textBlock["type"] != "text" {
		t.Errorf("expected first block type=text, got %v", textBlock["type"])
	}
	// Text block should have base64 STRIPPED (it goes in the image block instead)
	textContent, _ := textBlock["text"].(string)
	if strings.Contains(textContent, b64) {
		t.Error("text block should NOT contain raw base64 data")
	}

	// Second block should be image with base64 data
	imgBlock, ok := blocks[1].(map[string]interface{})
	if !ok {
		t.Fatalf("expected image block to be map, got %T", blocks[1])
	}
	if imgBlock["type"] != "image" {
		t.Errorf("expected second block type=image, got %v", imgBlock["type"])
	}
	source, ok := imgBlock["source"].(map[string]interface{})
	if !ok {
		t.Fatal("expected image block to have source")
	}
	if source["type"] != "base64" {
		t.Errorf("expected source type=base64, got %v", source["type"])
	}
	if source["data"] != b64 {
		t.Error("image block should contain the original base64 data")
	}
	if source["media_type"] != "image/png" {
		t.Errorf("expected media_type=image/png, got %v", source["media_type"])
	}

	// contentString should NOT contain the base64 data (it's the text portion)
	assertContentStringExcludes(t, contentStr, b64)
}

func TestProcessMCPToolResult_Vision_NoFS_AnthropicFormat(t *testing.T) {
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"type": "image",
		"source": map[string]interface{}{
			"type":       "base64",
			"media_type": "image/jpeg",
			"data":       b64,
		},
	}

	content, _, fileIDs := ProcessMCPToolResult(result, true, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}

	blocks, ok := content.([]interface{})
	if !ok {
		t.Fatalf("expected content blocks, got %T", content)
	}
	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 blocks (text + image), got %d", len(blocks))
	}

	// Verify image block has the data
	imgBlock := blocks[1].(map[string]interface{})
	source := imgBlock["source"].(map[string]interface{})
	if source["data"] != b64 {
		t.Error("image block should contain base64 data")
	}
	if source["media_type"] != "image/jpeg" {
		t.Errorf("expected media_type=image/jpeg, got %v", source["media_type"])
	}
}

func TestProcessMCPToolResult_Vision_NoFS_MultipleImages(t *testing.T) {
	b64a := fakeBase64(200)
	b64b := fakeBase64(300)
	result := map[string]interface{}{
		"images": []interface{}{
			map[string]interface{}{"data": b64a, "mimeType": "image/png"},
			map[string]interface{}{"data": b64b, "mimeType": "image/jpeg"},
		},
	}

	content, _, fileIDs := ProcessMCPToolResult(result, true, false, nil, "thread-1", "test-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}

	blocks, ok := content.([]interface{})
	if !ok {
		t.Fatalf("expected content blocks, got %T", content)
	}
	// 1 text + 2 images
	if len(blocks) != 3 {
		t.Fatalf("expected 3 content blocks (1 text + 2 images), got %d", len(blocks))
	}

	// Verify both image blocks
	for i, expected := range []struct {
		data     string
		mimeType string
	}{
		{b64a, "image/png"},
		{b64b, "image/jpeg"},
	} {
		imgBlock := blocks[i+1].(map[string]interface{})
		source := imgBlock["source"].(map[string]interface{})
		if source["data"] != expected.data {
			t.Errorf("image block %d: wrong data", i)
		}
		if source["media_type"] != expected.mimeType {
			t.Errorf("image block %d: expected media_type=%s, got %v", i, expected.mimeType, source["media_type"])
		}
	}
}

// ============================================================
// Matrix: Filesystem ON + Vision OFF → store images, replace with file refs
// ============================================================

func TestProcessMCPToolResult_NoVision_FS_GeminiInlineData(t *testing.T) {
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"success": true,
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{
							"inlineData": map[string]interface{}{
								"data":     b64,
								"mimeType": "image/png",
							},
						},
					},
				},
			},
		},
	}

	fp := &mockFileProcessor{
		enabled:   true,
		storedIDs: []string{"file-abc-123"},
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, fp, "thread-1", "gemini-generate")

	// Should have extracted file IDs
	if len(fileIDs) != 1 {
		t.Fatalf("expected 1 fileID, got %v", fileIDs)
	}
	if fileIDs[0] != "file-abc-123" {
		t.Errorf("expected fileID=file-abc-123, got %s", fileIDs[0])
	}

	// Content should be string (vision off)
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected content to be string, got %T", content)
	}
	if s != contentStr {
		t.Errorf("content and contentString should match when vision is off")
	}

	// Should contain file reference, NOT base64 data
	assertContentStringExcludes(t, contentStr, b64)
	assertContentString(t, contentStr, "file_reference", "file-abc-123")

	// FileProcessor should have been called
	if fp.calledWith == nil {
		t.Error("expected FileProcessor.ExtractImagesFromToolResult to be called")
	}
}

func TestProcessMCPToolResult_NoVision_FS_DisabledProcessor(t *testing.T) {
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"data":     b64,
		"mimeType": "image/png",
	}

	fp := &mockFileProcessor{
		enabled:   false, // disabled
		storedIDs: []string{"file-xyz"},
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, fp, "thread-1", "test-tool")

	// Disabled processor should not produce file IDs
	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs with disabled processor, got %v", fileIDs)
	}

	// Should strip base64 (like no-FS case)
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", content)
	}
	assertContentStringExcludes(t, s, b64)
	assertContentString(t, contentStr, "image_removed")
}

// ============================================================
// Matrix: Filesystem ON + Vision ON → store images AND build vision blocks
// ============================================================

func TestProcessMCPToolResult_Vision_FS_GeminiInlineData(t *testing.T) {
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"success": true,
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{
							"inlineData": map[string]interface{}{
								"data":     b64,
								"mimeType": "image/png",
							},
						},
					},
				},
			},
		},
	}

	fp := &mockFileProcessor{
		enabled:   true,
		storedIDs: []string{"file-both-123"},
	}

	content, contentStr, fileIDs := ProcessMCPToolResult(result, true, false, fp, "thread-1", "gemini-generate")

	// Should have file IDs from filesystem
	if len(fileIDs) != 1 {
		t.Fatalf("expected 1 fileID, got %v", fileIDs)
	}
	if fileIDs[0] != "file-both-123" {
		t.Errorf("expected fileID=file-both-123, got %s", fileIDs[0])
	}

	// Content should be []interface{} (content blocks) — vision is on
	blocks, ok := content.([]interface{})
	if !ok {
		t.Fatalf("expected content blocks, got %T", content)
	}
	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 blocks (text + image), got %d", len(blocks))
	}

	// Text block should contain file references (filesystem processed result)
	textBlock := blocks[0].(map[string]interface{})
	textContent, _ := textBlock["text"].(string)
	assertContentString(t, textContent, "file_reference", "file-both-123")
	assertContentStringExcludes(t, textContent, b64)

	// Image block should still have the ORIGINAL base64 for vision
	imgBlock := blocks[1].(map[string]interface{})
	source := imgBlock["source"].(map[string]interface{})
	if source["data"] != b64 {
		t.Error("image block should contain original base64 data for vision")
	}
	if source["media_type"] != "image/png" {
		t.Errorf("expected media_type=image/png, got %v", source["media_type"])
	}

	// contentString should have file references, not base64
	assertContentStringExcludes(t, contentStr, b64)
	assertContentString(t, contentStr, "file_reference")
}

// ============================================================
// Test: findImagesInValue — pattern detection
// ============================================================

func TestFindImagesInValue_Pattern1_Anthropic(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"type": "image",
		"source": map[string]interface{}{
			"type":       "base64",
			"media_type": "image/png",
			"data":       b64,
		},
	}

	images := findImagesInValue(value)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MimeType != "image/png" {
		t.Errorf("expected image/png, got %s", images[0].MimeType)
	}
	if images[0].Base64Data != b64 {
		t.Error("base64 data mismatch")
	}
}

func TestFindImagesInValue_Pattern2_GeminiInlineData(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"data":     b64,
		"mimeType": "image/jpeg",
	}

	images := findImagesInValue(value)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MimeType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", images[0].MimeType)
	}
}

func TestFindImagesInValue_Pattern3_Base64Field(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"base64":   b64,
		"mimeType": "image/webp",
	}

	images := findImagesInValue(value)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MimeType != "image/webp" {
		t.Errorf("expected image/webp, got %s", images[0].MimeType)
	}
}

func TestFindImagesInValue_Pattern4_ImageData(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"image_data": b64,
	}

	images := findImagesInValue(value)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MimeType != "image/png" {
		t.Errorf("expected default image/png, got %s", images[0].MimeType)
	}
}

func TestFindImagesInValue_Pattern4_Screenshot(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"screenshot": b64,
	}

	images := findImagesInValue(value)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
}

func TestFindImagesInValue_Pattern5_ImageField(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"image":      b64,
		"media_type": "image/gif",
	}

	images := findImagesInValue(value)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MimeType != "image/gif" {
		t.Errorf("expected image/gif, got %s", images[0].MimeType)
	}
}

func TestFindImagesInValue_Pattern6_DataURL(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"url": "data:image/svg+xml;base64," + b64,
	}

	images := findImagesInValue(value)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MimeType != "image/svg+xml" {
		t.Errorf("expected image/svg+xml, got %s", images[0].MimeType)
	}
}

func TestFindImagesInValue_Nested(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"result": map[string]interface{}{
			"nested": map[string]interface{}{
				"deep": map[string]interface{}{
					"data":     b64,
					"mimeType": "image/png",
				},
			},
		},
	}

	images := findImagesInValue(value)
	if len(images) != 1 {
		t.Fatalf("expected 1 image in nested structure, got %d", len(images))
	}
}

func TestFindImagesInValue_NoImages(t *testing.T) {
	value := map[string]interface{}{
		"success": true,
		"data":    "short string", // too short to be base64 image
		"models":  []interface{}{"gemini-flash", "gemini-pro"},
	}

	images := findImagesInValue(value)
	if len(images) != 0 {
		t.Errorf("expected 0 images, got %d", len(images))
	}
}

func TestFindImagesInValue_StringInput(t *testing.T) {
	// Strings should not match any pattern
	images := findImagesInValue("just a string with no images")
	if len(images) != 0 {
		t.Errorf("expected 0 images from string input, got %d", len(images))
	}
}

// ============================================================
// Test: stripBase64FromValue
// ============================================================

func TestStripBase64FromValue_GeminiInlineData(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"result": map[string]interface{}{
			"inlineData": map[string]interface{}{
				"data":     b64,
				"mimeType": "image/png",
			},
		},
	}

	stripped := stripBase64FromValue(value)
	strippedJSON, _ := json.Marshal(stripped)
	s := string(strippedJSON)

	if strings.Contains(s, b64) {
		t.Error("stripped result should NOT contain base64 data")
	}
	if !strings.Contains(s, "image_removed") {
		t.Error("stripped result should contain image_removed placeholder")
	}
}

func TestStripBase64FromValue_PreservesNonImageData(t *testing.T) {
	value := map[string]interface{}{
		"success": true,
		"message": "hello",
		"count":   float64(42),
	}

	stripped := stripBase64FromValue(value)
	strippedMap, ok := stripped.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", stripped)
	}
	if strippedMap["success"] != true {
		t.Error("success field should be preserved")
	}
	if strippedMap["message"] != "hello" {
		t.Error("message field should be preserved")
	}
	if strippedMap["count"] != float64(42) {
		t.Error("count field should be preserved")
	}
}

func TestStripBase64FromValue_Array(t *testing.T) {
	b64 := fakeBase64(200)
	value := []interface{}{
		map[string]interface{}{"data": b64, "mimeType": "image/png"},
		map[string]interface{}{"text": "keep this"},
	}

	stripped := stripBase64FromValue(value)
	arr, ok := stripped.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", stripped)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 items, got %d", len(arr))
	}

	// First item should be stripped
	first, _ := json.Marshal(arr[0])
	if strings.Contains(string(first), b64) {
		t.Error("first item should have base64 stripped")
	}

	// Second item should be preserved
	second := arr[1].(map[string]interface{})
	if second["text"] != "keep this" {
		t.Error("second item text should be preserved")
	}
}

// ============================================================
// Document (PDF) detection tests
// ============================================================

func TestFindMediaInValue_PDF_SimpleFormat(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"data":     b64,
		"mimeType": "application/pdf",
	}

	media := findMediaInValue(value)
	if len(media) != 1 {
		t.Fatalf("expected 1 media, got %d", len(media))
	}
	if media[0].Kind != "document" {
		t.Errorf("expected kind=document, got %s", media[0].Kind)
	}
	if media[0].MimeType != "application/pdf" {
		t.Errorf("expected application/pdf, got %s", media[0].MimeType)
	}
}

func TestFindMediaInValue_PDF_AnthropicFormat(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"type": "document",
		"source": map[string]interface{}{
			"type":       "base64",
			"media_type": "application/pdf",
			"data":       b64,
		},
	}

	media := findMediaInValue(value)
	if len(media) != 1 {
		t.Fatalf("expected 1 media, got %d", len(media))
	}
	if media[0].Kind != "document" {
		t.Errorf("expected kind=document, got %s", media[0].Kind)
	}
}

func TestFindMediaInValue_PDF_PdfDataField(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"pdf_data": b64,
		"title":    "Test",
	}

	media := findMediaInValue(value)
	if len(media) != 1 {
		t.Fatalf("expected 1 media, got %d", len(media))
	}
	if media[0].Kind != "document" {
		t.Errorf("expected kind=document, got %s", media[0].Kind)
	}
	if media[0].MimeType != "application/pdf" {
		t.Errorf("expected application/pdf, got %s", media[0].MimeType)
	}
}

func TestFindMediaInValue_PDF_DataURL(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"url": "data:application/pdf;base64," + b64,
	}

	media := findMediaInValue(value)
	if len(media) != 1 {
		t.Fatalf("expected 1 media, got %d", len(media))
	}
	if media[0].Kind != "document" {
		t.Errorf("expected kind=document, got %s", media[0].Kind)
	}
}

func TestFindMediaInValue_MixedImageAndPDF(t *testing.T) {
	b64img := fakeBase64(200)
	b64pdf := fakeBase64(300)
	value := map[string]interface{}{
		"files": []interface{}{
			map[string]interface{}{"data": b64img, "mimeType": "image/png"},
			map[string]interface{}{"data": b64pdf, "mimeType": "application/pdf"},
		},
	}

	media := findMediaInValue(value)
	if len(media) != 2 {
		t.Fatalf("expected 2 media items, got %d", len(media))
	}

	var images, docs int
	for _, m := range media {
		if m.Kind == "image" {
			images++
		}
		if m.Kind == "document" {
			docs++
		}
	}
	if images != 1 || docs != 1 {
		t.Errorf("expected 1 image + 1 document, got %d images + %d documents", images, docs)
	}
}

// ============================================================
// ProcessMCPToolResult with document support
// ============================================================

func TestProcessMCPToolResult_DocumentSupport_PDF(t *testing.T) {
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"success": true,
		"document": map[string]interface{}{
			"data":     b64,
			"mimeType": "application/pdf",
			"title":    "Test PDF",
		},
	}

	// With document support enabled
	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, true, nil, "thread-1", "pdf-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}

	// Content should be []interface{} (content blocks) when document support is on
	blocks, ok := content.([]interface{})
	if !ok {
		t.Fatalf("expected content blocks, got %T", content)
	}

	// Should have 2 blocks: 1 text + 1 document
	if len(blocks) != 2 {
		t.Fatalf("expected 2 content blocks (1 text + 1 document), got %d", len(blocks))
	}

	// First block should be text
	textBlock := blocks[0].(map[string]interface{})
	if textBlock["type"] != "text" {
		t.Errorf("expected first block type=text, got %v", textBlock["type"])
	}
	// Text should not contain base64
	textContent, _ := textBlock["text"].(string)
	assertContentStringExcludes(t, textContent, b64)

	// Second block should be document
	docBlock := blocks[1].(map[string]interface{})
	if docBlock["type"] != "document" {
		t.Errorf("expected second block type=document, got %v", docBlock["type"])
	}
	source := docBlock["source"].(map[string]interface{})
	if source["type"] != "base64" {
		t.Errorf("expected source type=base64, got %v", source["type"])
	}
	if source["data"] != b64 {
		t.Error("document block should contain base64 data")
	}
	if source["media_type"] != "application/pdf" {
		t.Errorf("expected media_type=application/pdf, got %v", source["media_type"])
	}

	// contentString should not contain base64
	assertContentStringExcludes(t, contentStr, b64)
}

func TestProcessMCPToolResult_NoDocumentSupport_PDF_Stripped(t *testing.T) {
	b64 := fakeBase64(200)
	result := map[string]interface{}{
		"document": map[string]interface{}{
			"data":     b64,
			"mimeType": "application/pdf",
		},
	}

	// Without document support — should strip base64
	content, contentStr, fileIDs := ProcessMCPToolResult(result, false, false, nil, "thread-1", "pdf-tool")

	if len(fileIDs) != 0 {
		t.Errorf("expected no fileIDs, got %v", fileIDs)
	}

	// Content should be string (no content blocks)
	s, ok := content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", content)
	}
	assertContentStringExcludes(t, s, b64)
	assertContentString(t, contentStr, "document_removed", "application/pdf")
}

func TestProcessMCPToolResult_VisionAndDocument_MixedContent(t *testing.T) {
	b64img := fakeBase64(200)
	b64pdf := fakeBase64(300)
	result := map[string]interface{}{
		"files": []interface{}{
			map[string]interface{}{"data": b64img, "mimeType": "image/png"},
			map[string]interface{}{"data": b64pdf, "mimeType": "application/pdf"},
		},
	}

	// Both vision and document support enabled
	content, _, _ := ProcessMCPToolResult(result, true, true, nil, "thread-1", "mixed-tool")

	blocks, ok := content.([]interface{})
	if !ok {
		t.Fatalf("expected content blocks, got %T", content)
	}

	// Should have 3 blocks: 1 text + 1 image + 1 document
	if len(blocks) != 3 {
		t.Fatalf("expected 3 content blocks (1 text + 1 image + 1 document), got %d", len(blocks))
	}

	// Check types
	textBlock := blocks[0].(map[string]interface{})
	if textBlock["type"] != "text" {
		t.Errorf("block 0: expected type=text, got %v", textBlock["type"])
	}
	imgBlock := blocks[1].(map[string]interface{})
	if imgBlock["type"] != "image" {
		t.Errorf("block 1: expected type=image, got %v", imgBlock["type"])
	}
	docBlock := blocks[2].(map[string]interface{})
	if docBlock["type"] != "document" {
		t.Errorf("block 2: expected type=document, got %v", docBlock["type"])
	}
}

func TestStripBase64FromValue_PDF(t *testing.T) {
	b64 := fakeBase64(200)
	value := map[string]interface{}{
		"data":     b64,
		"mimeType": "application/pdf",
	}

	stripped := stripBase64FromValue(value)
	strippedJSON, _ := json.Marshal(stripped)
	s := string(strippedJSON)

	if strings.Contains(s, b64) {
		t.Error("stripped result should NOT contain base64 data")
	}
	if !strings.Contains(s, "document_removed") {
		t.Error("stripped result should contain document_removed placeholder")
	}
	if !strings.Contains(s, "application/pdf") {
		t.Error("stripped result should mention application/pdf")
	}
}
