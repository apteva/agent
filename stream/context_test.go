package stream

import (
	"testing"
)

// --- stripImagesFromMessage: top-level image stripping ---

func TestStripImagesFromMessage_TopLevelImage_ContentBlock(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "text", Text: "Here is an image"},
			{Type: "image", Source: &ContentSource{Type: "base64", MediaType: "image/png", Data: "iVBOR..."}},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 1 {
		t.Fatalf("expected 1 stripped, got %d", count)
	}

	blocks := msg.Content.([]ContentBlock)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[1].Type != "text" || blocks[1].Text != "[Image removed from context]" {
		t.Errorf("expected placeholder, got type=%s text=%q", blocks[1].Type, blocks[1].Text)
	}
}

func TestStripImagesFromMessage_TopLevelImage_InterfaceMap(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{"type": "text", "text": "Hello"},
			map[string]interface{}{
				"type": "image",
				"source": map[string]interface{}{
					"type":       "base64",
					"media_type": "image/png",
					"data":       "iVBOR...",
				},
			},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 1 {
		t.Fatalf("expected 1 stripped, got %d", count)
	}

	blocks := msg.Content.([]interface{})
	replaced := blocks[1].(map[string]interface{})
	if replaced["type"] != "text" || replaced["text"] != "[Image removed from context]" {
		t.Errorf("expected placeholder, got %v", replaced)
	}
}

func TestStripImagesFromMessage_NoImages(t *testing.T) {
	msg := Message{
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: "Just text"},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 0 {
		t.Fatalf("expected 0 stripped, got %d", count)
	}
}

// --- stripImagesFromMessage: images inside tool_result content ---

func TestStripImagesFromMessage_ToolResult_ContentBlock_WithImage(t *testing.T) {
	// Simulates a tool_result saved with vision ON: has image blocks in Content
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: "toolu_123",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "Generated image of a cat"},
					map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": "image/png",
							"data":       "iVBORw0KGgoAAAANSUhEUgAAAAUA",
						},
					},
				},
			},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 1 {
		t.Fatalf("expected 1 stripped, got %d", count)
	}

	blocks := msg.Content.([]ContentBlock)
	toolResult := blocks[0]
	content := toolResult.Content.([]interface{})
	if len(content) != 2 {
		t.Fatalf("expected 2 items in tool_result content, got %d", len(content))
	}
	// First item should still be text
	first := content[0].(map[string]interface{})
	if first["type"] != "text" {
		t.Errorf("expected first item type=text, got %v", first["type"])
	}
	// Second item should now be placeholder
	second := content[1].(map[string]interface{})
	if second["type"] != "text" || second["text"] != "[Image removed from context]" {
		t.Errorf("expected image placeholder, got %v", second)
	}
}

func TestStripImagesFromMessage_ToolResult_ContentBlock_MultipleImages(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: "toolu_456",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "Two images"},
					map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type": "base64", "media_type": "image/png", "data": "img1data",
						},
					},
					map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type": "base64", "media_type": "image/jpeg", "data": "img2data",
						},
					},
				},
			},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 2 {
		t.Fatalf("expected 2 stripped, got %d", count)
	}

	blocks := msg.Content.([]ContentBlock)
	content := blocks[0].Content.([]interface{})
	if len(content) != 3 {
		t.Fatalf("expected 3 items, got %d", len(content))
	}
	for i := 1; i <= 2; i++ {
		item := content[i].(map[string]interface{})
		if item["type"] != "text" || item["text"] != "[Image removed from context]" {
			t.Errorf("expected placeholder at index %d, got %v", i, item)
		}
	}
}

func TestStripImagesFromMessage_ToolResult_ContentBlock_NoImages(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: "toolu_789",
				Content:   "Just a text result, no images",
			},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 0 {
		t.Fatalf("expected 0 stripped, got %d", count)
	}

	// Content should be unchanged
	blocks := msg.Content.([]ContentBlock)
	if blocks[0].Content != "Just a text result, no images" {
		t.Errorf("content was modified unexpectedly: %v", blocks[0].Content)
	}
}

func TestStripImagesFromMessage_ToolResult_InterfaceMap_WithImage(t *testing.T) {
	// Same scenario but with raw []interface{} message format
	msg := Message{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "toolu_abc",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "Image result"},
					map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": "image/png",
							"data":       "hugeb64data",
						},
					},
				},
			},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 1 {
		t.Fatalf("expected 1 stripped, got %d", count)
	}

	blocks := msg.Content.([]interface{})
	toolResult := blocks[0].(map[string]interface{})
	content := toolResult["content"].([]interface{})
	second := content[1].(map[string]interface{})
	if second["type"] != "text" || second["text"] != "[Image removed from context]" {
		t.Errorf("expected placeholder, got %v", second)
	}
}

func TestStripImagesFromMessage_ToolResult_InterfaceMap_StringContent(t *testing.T) {
	// tool_result with string content (no array) - should not fail
	msg := Message{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "toolu_def",
				"content":     "Simple string result",
			},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 0 {
		t.Fatalf("expected 0 stripped, got %d", count)
	}
}

func TestStripImagesFromMessage_ToolResult_InterfaceMap_NoContent(t *testing.T) {
	// tool_result with no content field
	msg := Message{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "toolu_ghi",
			},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 0 {
		t.Fatalf("expected 0 stripped, got %d", count)
	}
}

// --- Mixed: top-level images + tool_result images ---

func TestStripImagesFromMessage_Mixed_TopLevel_And_ToolResult(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "image", Source: &ContentSource{Type: "base64", MediaType: "image/png", Data: "toplevel"}},
			{
				Type:      "tool_result",
				ToolUseID: "toolu_mix",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "Result with image"},
					map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type": "base64", "media_type": "image/png", "data": "nested",
						},
					},
				},
			},
			{Type: "text", Text: "Some text"},
		},
	}

	count := stripImagesFromMessage(&msg)
	if count != 2 {
		t.Fatalf("expected 2 stripped (1 top-level + 1 nested), got %d", count)
	}

	blocks := msg.Content.([]ContentBlock)

	// Top-level image replaced
	if blocks[0].Type != "text" || blocks[0].Text != "[Image removed from context]" {
		t.Errorf("expected top-level placeholder, got type=%s text=%q", blocks[0].Type, blocks[0].Text)
	}

	// Nested image in tool_result replaced
	toolResult := blocks[1]
	content := toolResult.Content.([]interface{})
	second := content[1].(map[string]interface{})
	if second["type"] != "text" || second["text"] != "[Image removed from context]" {
		t.Errorf("expected nested placeholder, got %v", second)
	}

	// Text block unchanged
	if blocks[2].Type != "text" || blocks[2].Text != "Some text" {
		t.Errorf("text block was modified: %v", blocks[2])
	}
}

// --- stripOldImages integration ---

func TestStripOldImages_StripsToolResultImages(t *testing.T) {
	cm := NewContextManager(0, 0, 2) // keep images in last 2 messages

	messages := []Message{
		{
			Role: "user",
			Content: []ContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: "toolu_old",
					Content: []interface{}{
						map[string]interface{}{"type": "text", "text": "Old image"},
						map[string]interface{}{
							"type": "image",
							"source": map[string]interface{}{
								"type": "base64", "media_type": "image/png", "data": "oldbase64",
							},
						},
					},
				},
			},
		},
		{Role: "assistant", Content: "Response 1"},
		{Role: "user", Content: "Next question"},
		{
			Role: "user",
			Content: []ContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: "toolu_new",
					Content: []interface{}{
						map[string]interface{}{"type": "text", "text": "New image"},
						map[string]interface{}{
							"type": "image",
							"source": map[string]interface{}{
								"type": "base64", "media_type": "image/png", "data": "newbase64",
							},
						},
					},
				},
			},
		},
	}

	count := cm.stripOldImages(messages)
	if count != 1 {
		t.Fatalf("expected 1 stripped from old message, got %d", count)
	}

	// Old message (index 0): image should be stripped
	oldBlocks := messages[0].Content.([]ContentBlock)
	oldContent := oldBlocks[0].Content.([]interface{})
	oldSecond := oldContent[1].(map[string]interface{})
	if oldSecond["type"] != "text" || oldSecond["text"] != "[Image removed from context]" {
		t.Errorf("expected old image stripped, got %v", oldSecond)
	}

	// New message (index 3): image should be preserved (within keepImages threshold)
	newBlocks := messages[3].Content.([]ContentBlock)
	newContent := newBlocks[0].Content.([]interface{})
	newSecond := newContent[1].(map[string]interface{})
	if newSecond["type"] != "image" {
		t.Errorf("expected new image preserved, got type=%v", newSecond["type"])
	}
}

// --- stripImagesFromToolResultContent with []ContentBlock content ---

func TestStripImagesFromToolResultContent_ContentBlockSlice(t *testing.T) {
	block := ContentBlock{
		Type:      "tool_result",
		ToolUseID: "toolu_cb",
		Content: []ContentBlock{
			{Type: "text", Text: "Image result"},
			{Type: "image", Source: &ContentSource{Type: "base64", MediaType: "image/png", Data: "data123"}},
		},
	}

	count := stripImagesFromToolResultContent(&block)
	if count != 1 {
		t.Fatalf("expected 1 stripped, got %d", count)
	}

	content := block.Content.([]ContentBlock)
	if content[1].Type != "text" || content[1].Text != "[Image removed from context]" {
		t.Errorf("expected placeholder, got type=%s text=%q", content[1].Type, content[1].Text)
	}
}
