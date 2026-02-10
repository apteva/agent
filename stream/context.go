package stream

import (
	"encoding/json"
	"log"
)

// ContextManager handles context window management
type ContextManager struct {
	maxMessages int
	maxTokens   int
	keepImages  int
}

// NewContextManager creates a new context manager
// maxTokens takes precedence over maxMessages if set (> 0)
func NewContextManager(maxMessages, maxTokens, keepImages int) *ContextManager {
	return &ContextManager{
		maxMessages: maxMessages,
		maxTokens:   maxTokens,
		keepImages:  keepImages,
	}
}

// TruncateMessages applies context limits to messages
// 1. If maxTokens is set, limits by estimated token count
// 2. Otherwise, limits by message count (maxMessages)
// 3. Strips images from older messages, keeping only last N with images
// IMPORTANT: Ensures tool_use/tool_result pairs stay together
func (cm *ContextManager) TruncateMessages(messages []Message) []Message {
	originalCount := len(messages)

	// Step 0a: ALWAYS strip base64 images from ALL tool_result content blocks.
	// Tool_result images are redundant in history — the text block already has
	// file_id/URL references. These images can be 1-2MB and inflate token estimates
	// causing important messages to be truncated away.
	toolResultImageCount := stripImagesFromAllToolResults(messages)
	if toolResultImageCount > 0 {
		log.Printf("🖼️  Context: Stripped %d images from tool_result content (all messages)", toolResultImageCount)
	}

	// Step 0b: Strip top-level images from old messages (respects keepImages threshold)
	if cm.keepImages > 0 && len(messages) > 0 {
		imageCount := cm.stripOldImages(messages)
		if imageCount > 0 {
			log.Printf("🖼️  Context: Pre-truncation stripped %d top-level images, keeping last %d messages with images",
				imageCount, cm.keepImages)
		}
	}

	// Step 1: Apply truncation (token-based or message-based)
	if cm.maxTokens > 0 {
		// Token-based truncation (preferred)
		messages = cm.truncateByTokens(messages, originalCount)
	} else if cm.maxMessages > 0 && len(messages) > cm.maxMessages {
		// Message-based truncation (fallback)
		cutIndex := len(messages) - cm.maxMessages

		// Adjust cut index to preserve tool_use/tool_result pairs
		cutIndex = cm.findSafeCutIndex(messages, cutIndex)

		messages = messages[cutIndex:]
		log.Printf("📊 Context: Truncated %d → %d messages (limit: %d, safe cut at %d)",
			originalCount, len(messages), cm.maxMessages, cutIndex)
	}

	return messages
}

// truncateByTokens removes oldest messages until estimated token count is under maxTokens
// Always preserves tool_use/tool_result pairs
func (cm *ContextManager) truncateByTokens(messages []Message, originalCount int) []Message {
	// Calculate total tokens
	totalTokens := 0
	tokenCounts := make([]int, len(messages))
	for i, msg := range messages {
		tokens := cm.estimateMessageTokens(&msg)
		tokenCounts[i] = tokens
		totalTokens += tokens
	}

	if totalTokens <= cm.maxTokens {
		log.Printf("📊 Context: %d messages, ~%d tokens (under limit of %d)", len(messages), totalTokens, cm.maxTokens)
		return messages
	}

	// Find cut index by accumulating tokens from the end
	cutIndex := 0
	keptTokens := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if keptTokens+tokenCounts[i] > cm.maxTokens {
			cutIndex = i + 1
			break
		}
		keptTokens += tokenCounts[i]
	}

	// Adjust cut index to preserve tool_use/tool_result pairs
	cutIndex = cm.findSafeCutIndex(messages, cutIndex)

	// Recalculate kept tokens after adjustment
	keptTokens = 0
	for i := cutIndex; i < len(messages); i++ {
		keptTokens += tokenCounts[i]
	}

	messages = messages[cutIndex:]
	log.Printf("📊 Context: Truncated %d → %d messages, ~%d → ~%d tokens (limit: %d)",
		originalCount, len(messages), totalTokens, keptTokens, cm.maxTokens)

	return messages
}

// estimateMessageTokens estimates the token count for a message
// Uses a rough approximation: ~4 characters per token for text, fixed costs for structure
func (cm *ContextManager) estimateMessageTokens(msg *Message) int {
	tokens := 4 // Base overhead for message structure (role, etc.)

	switch content := msg.Content.(type) {
	case string:
		tokens += len(content) / 4

	case []ContentBlock:
		for _, block := range content {
			tokens += cm.estimateBlockTokens(&block)
		}

	case []interface{}:
		for _, block := range content {
			if blockMap, ok := block.(map[string]interface{}); ok {
				tokens += cm.estimateMapBlockTokens(blockMap)
			}
		}
	}

	return tokens
}

// estimateBlockTokens estimates tokens for a ContentBlock
func (cm *ContextManager) estimateBlockTokens(block *ContentBlock) int {
	tokens := 3 // Block structure overhead

	switch block.Type {
	case "text":
		tokens += len(block.Text) / 4

	case "tool_use":
		tokens += len(block.Name) / 4
		tokens += len(block.ID) / 4
		if block.Input != nil {
			inputJSON, _ := json.Marshal(block.Input)
			tokens += len(inputJSON) / 4
		}

	case "tool_result":
		tokens += len(block.ToolUseID) / 4
		if contentStr, ok := block.Content.(string); ok {
			tokens += len(contentStr) / 4
		} else if block.Content != nil {
			contentJSON, _ := json.Marshal(block.Content)
			tokens += len(contentJSON) / 4
		}

	case "image":
		// Images use ~1000 tokens on average (varies by size)
		tokens += 1000
	}

	return tokens
}

// estimateMapBlockTokens estimates tokens for a map-based block
func (cm *ContextManager) estimateMapBlockTokens(blockMap map[string]interface{}) int {
	tokens := 3 // Block structure overhead

	blockType, _ := blockMap["type"].(string)

	switch blockType {
	case "text":
		if text, ok := blockMap["text"].(string); ok {
			tokens += len(text) / 4
		}

	case "tool_use":
		if name, ok := blockMap["name"].(string); ok {
			tokens += len(name) / 4
		}
		if id, ok := blockMap["id"].(string); ok {
			tokens += len(id) / 4
		}
		if input, ok := blockMap["input"]; ok {
			inputJSON, _ := json.Marshal(input)
			tokens += len(inputJSON) / 4
		}

	case "tool_result":
		if toolUseID, ok := blockMap["tool_use_id"].(string); ok {
			tokens += len(toolUseID) / 4
		}
		if content, ok := blockMap["content"]; ok {
			if contentStr, ok := content.(string); ok {
				tokens += len(contentStr) / 4
			} else {
				contentJSON, _ := json.Marshal(content)
				tokens += len(contentJSON) / 4
			}
		}

	case "image":
		tokens += 1000
	}

	return tokens
}

// findSafeCutIndex adjusts the cut index to ensure we don't break tool_use/tool_result pairs
// If the first message after cut contains a tool_result, we need to include its tool_use
func (cm *ContextManager) findSafeCutIndex(messages []Message, proposedCut int) int {
	if proposedCut <= 0 || proposedCut >= len(messages) {
		return proposedCut
	}

	// Collect all tool_use IDs that would be kept (from proposedCut onwards)
	toolResultIDs := make(map[string]bool)
	for i := proposedCut; i < len(messages); i++ {
		ids := cm.extractToolResultIDs(&messages[i])
		for _, id := range ids {
			toolResultIDs[id] = true
		}
	}

	if len(toolResultIDs) == 0 {
		return proposedCut // No tool_results, safe to cut here
	}

	// Collect all tool_use IDs that would be kept (from proposedCut onwards)
	toolUseIDsKept := make(map[string]bool)
	for i := proposedCut; i < len(messages); i++ {
		ids := cm.extractToolUseIDs(&messages[i])
		for _, id := range ids {
			toolUseIDsKept[id] = true
		}
	}

	// Find which tool_result IDs are missing their tool_use
	missingToolUseIDs := make(map[string]bool)
	for id := range toolResultIDs {
		if !toolUseIDsKept[id] {
			missingToolUseIDs[id] = true
		}
	}

	if len(missingToolUseIDs) == 0 {
		return proposedCut // All tool_results have their tool_use, safe to cut here
	}

	// Search backwards from proposedCut to find messages with required tool_use blocks
	for i := proposedCut - 1; i >= 0; i-- {
		ids := cm.extractToolUseIDs(&messages[i])
		for _, id := range ids {
			delete(missingToolUseIDs, id)
		}

		if len(missingToolUseIDs) == 0 {
			log.Printf("📊 Context: Adjusted cut index %d → %d to preserve tool pairs", proposedCut, i)
			return i
		}
	}

	// If we get here, we couldn't find all tool_use blocks - return 0 to keep everything
	log.Printf("⚠️  Context: Could not find all tool_use blocks, keeping all messages")
	return 0
}

// extractToolResultIDs extracts all tool_use_id values from tool_result blocks in a message
func (cm *ContextManager) extractToolResultIDs(msg *Message) []string {
	var ids []string

	// Handle []ContentBlock type (from message converter)
	if blocks, ok := msg.Content.([]ContentBlock); ok {
		for _, block := range blocks {
			if block.Type == "tool_result" && block.ToolUseID != "" {
				ids = append(ids, block.ToolUseID)
			}
		}
		return ids
	}

	// Handle []interface{} type (raw JSON)
	if blocks, ok := msg.Content.([]interface{}); ok {
		for _, block := range blocks {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}

			blockType, _ := blockMap["type"].(string)
			if blockType == "tool_result" {
				if toolUseID, ok := blockMap["tool_use_id"].(string); ok && toolUseID != "" {
					ids = append(ids, toolUseID)
				}
			}
		}
	}

	return ids
}

// extractToolUseIDs extracts all id values from tool_use blocks in a message
func (cm *ContextManager) extractToolUseIDs(msg *Message) []string {
	var ids []string

	// Handle []ContentBlock type (from message converter)
	if blocks, ok := msg.Content.([]ContentBlock); ok {
		for _, block := range blocks {
			if block.Type == "tool_use" && block.ID != "" {
				ids = append(ids, block.ID)
			}
		}
		return ids
	}

	// Handle []interface{} type (raw JSON)
	if blocks, ok := msg.Content.([]interface{}); ok {
		for _, block := range blocks {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}

			blockType, _ := blockMap["type"].(string)
			if blockType == "tool_use" {
				if id, ok := blockMap["id"].(string); ok && id != "" {
					ids = append(ids, id)
				}
			}
		}
	}

	return ids
}

// stripOldImages removes images from messages beyond the keepImages threshold
// Returns count of images stripped
// stripImagesFromAllToolResults strips base64 images from tool_result content in ALL messages.
// Tool_result images are always redundant in history because the text block has the file_id/URL.
// This must run before token estimation to prevent 1-2MB images from inflating token counts.
func stripImagesFromAllToolResults(messages []Message) int {
	strippedCount := 0
	for i := range messages {
		if blocks, ok := messages[i].Content.([]ContentBlock); ok {
			for j := range blocks {
				if blocks[j].Type == "tool_result" {
					count := stripImagesFromToolResultContent(&blocks[j])
					strippedCount += count
				}
			}
			if strippedCount > 0 {
				messages[i].Content = blocks
			}
		} else if rawBlocks, ok := messages[i].Content.([]interface{}); ok {
			for _, block := range rawBlocks {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if blockType, _ := blockMap["type"].(string); blockType == "tool_result" {
						count := stripImagesFromToolResultMap(blockMap)
						strippedCount += count
					}
				}
			}
		}
	}
	return strippedCount
}

func (cm *ContextManager) stripOldImages(messages []Message) int {
	if len(messages) <= cm.keepImages {
		return 0 // All messages within keep threshold
	}

	strippedCount := 0
	cutoffIdx := len(messages) - cm.keepImages

	for i := 0; i < cutoffIdx; i++ {
		count := stripImagesFromMessage(&messages[i])
		strippedCount += count
	}

	return strippedCount
}

// stripImagesFromMessage removes image content blocks from a single message
// Replaces images with text placeholder
// Also strips images nested inside tool_result content blocks
// Returns count of images stripped
func stripImagesFromMessage(msg *Message) int {
	strippedCount := 0

	// Handle []ContentBlock type (from message converter)
	if blocks, ok := msg.Content.([]ContentBlock); ok {
		newBlocks := make([]ContentBlock, 0, len(blocks))

		for _, block := range blocks {
			if block.Type == "image" {
				strippedCount++
				// Replace with text placeholder
				newBlocks = append(newBlocks, ContentBlock{
					Type: "text",
					Text: "[Image removed from context]",
				})
			} else if block.Type == "tool_result" {
				// Also strip images inside tool_result content
				innerCount := stripImagesFromToolResultContent(&block)
				strippedCount += innerCount
				newBlocks = append(newBlocks, block)
			} else {
				newBlocks = append(newBlocks, block)
			}
		}

		if strippedCount > 0 {
			msg.Content = newBlocks
		}
		return strippedCount
	}

	// Handle []interface{} type (raw JSON)
	if blocks, ok := msg.Content.([]interface{}); ok {
		newBlocks := []interface{}{}

		for _, block := range blocks {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				newBlocks = append(newBlocks, block)
				continue
			}

			blockType, hasType := blockMap["type"].(string)
			if !hasType {
				newBlocks = append(newBlocks, block)
				continue
			}

			// Strip image blocks
			if blockType == "image" {
				strippedCount++
				// Replace with text placeholder
				newBlocks = append(newBlocks, map[string]interface{}{
					"type": "text",
					"text": "[Image removed from context]",
				})
			} else if blockType == "tool_result" {
				// Also strip images inside tool_result content
				innerCount := stripImagesFromToolResultMap(blockMap)
				strippedCount += innerCount
				newBlocks = append(newBlocks, blockMap)
			} else {
				// Keep all non-image blocks
				newBlocks = append(newBlocks, block)
			}
		}

		if strippedCount > 0 {
			msg.Content = newBlocks
		}
	}

	return strippedCount
}

// stripImagesFromToolResultContent strips image blocks from a ContentBlock's Content field
// This handles tool_result blocks that contain embedded images (e.g., from vision-enabled MCP results)
func stripImagesFromToolResultContent(block *ContentBlock) int {
	switch content := block.Content.(type) {
	case []interface{}:
		strippedCount := 0
		newContent := make([]interface{}, 0, len(content))
		for _, item := range content {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if itemType, _ := itemMap["type"].(string); itemType == "image" {
					strippedCount++
					newContent = append(newContent, map[string]interface{}{
						"type": "text",
						"text": "[Image removed from context]",
					})
					continue
				}
			}
			newContent = append(newContent, item)
		}
		if strippedCount > 0 {
			block.Content = newContent
			log.Printf("🖼️  Stripped %d image(s) from tool_result content (tool_use_id=%s)", strippedCount, block.ToolUseID)
		}
		return strippedCount

	case []ContentBlock:
		strippedCount := 0
		newContent := make([]ContentBlock, 0, len(content))
		for _, cb := range content {
			if cb.Type == "image" {
				strippedCount++
				newContent = append(newContent, ContentBlock{
					Type: "text",
					Text: "[Image removed from context]",
				})
			} else {
				newContent = append(newContent, cb)
			}
		}
		if strippedCount > 0 {
			block.Content = newContent
			log.Printf("🖼️  Stripped %d image(s) from tool_result content (tool_use_id=%s)", strippedCount, block.ToolUseID)
		}
		return strippedCount
	}

	return 0
}

// stripImagesFromToolResultMap strips image blocks from a tool_result map's "content" field
// This handles the []interface{} (raw JSON) representation of tool_result blocks
func stripImagesFromToolResultMap(blockMap map[string]interface{}) int {
	content, ok := blockMap["content"]
	if !ok {
		return 0
	}

	contentArr, ok := content.([]interface{})
	if !ok {
		return 0
	}

	strippedCount := 0
	newContent := make([]interface{}, 0, len(contentArr))
	for _, item := range contentArr {
		if itemMap, ok := item.(map[string]interface{}); ok {
			if itemType, _ := itemMap["type"].(string); itemType == "image" {
				strippedCount++
				newContent = append(newContent, map[string]interface{}{
					"type": "text",
					"text": "[Image removed from context]",
				})
				continue
			}
		}
		newContent = append(newContent, item)
	}

	if strippedCount > 0 {
		blockMap["content"] = newContent
		toolUseID, _ := blockMap["tool_use_id"].(string)
		log.Printf("🖼️  Stripped %d image(s) from tool_result map content (tool_use_id=%s)", strippedCount, toolUseID)
	}
	return strippedCount
}
