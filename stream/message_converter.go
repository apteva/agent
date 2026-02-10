package stream

import (
	"encoding/json"
	"fmt"
	"log"
)

// ConvertDatabaseMessageToStreamMessage converts a message loaded from the database
// into the proper format for the LLM provider
func ConvertDatabaseMessageToStreamMessage(role string, content interface{}) (Message, error) {
	return ConvertDatabaseMessageToStreamMessageWithMetadata(role, content, nil)
}

// ConvertDatabaseMessageToStreamMessageWithMetadata converts a message loaded from the database
// into the proper format for the LLM provider, including reasoning_content from metadata
func ConvertDatabaseMessageToStreamMessageWithMetadata(role string, content interface{}, metadata map[string]interface{}) (Message, error) {
	msg := Message{
		Role: role,
	}

	// Extract reasoning_content from metadata (for thinking models like Kimi K2)
	if metadata != nil {
		if reasoning, ok := metadata["reasoning_content"].(string); ok && reasoning != "" {
			msg.ReasoningContent = reasoning
			log.Printf("🧠 Loaded reasoning_content from metadata: %d chars", len(reasoning))
		}
	}

	switch v := content.(type) {
	case string:
		// Simple string content - pass through as-is
		msg.Content = v

	case []interface{}:
		// Array of content blocks - need to properly parse
		var blocks []ContentBlock
		for _, item := range v {
			if blockMap, ok := item.(map[string]interface{}); ok {
				block := ContentBlock{}

				// Parse type
				if blockType, ok := blockMap["type"].(string); ok {
					block.Type = blockType
				}

				// Parse text (for text blocks)
				if text, ok := blockMap["text"].(string); ok {
					block.Text = text
				}

				// Parse tool use fields
				if id, ok := blockMap["id"].(string); ok {
					block.ID = id
				}
				if name, ok := blockMap["name"].(string); ok {
					block.Name = name
				}

				// Parse input - Anthropic requires this field to be present
				// IMPORTANT: Even empty objects {} must be preserved
				if input, ok := blockMap["input"]; ok {
					switch v := input.(type) {
					case map[string]interface{}:
						if v != nil {
							block.Input = v
							// DEBUG: Log computer tool input when converting
							if block.Name == "computer" {
								inputJSON, _ := json.Marshal(v)
								log.Printf("🖥️  COMPUTER TOOL - Converting from DB: input=%s", string(inputJSON))
							}
						} else {
							// nil map becomes empty map
							block.Input = map[string]interface{}{}
							if block.Name == "computer" {
								log.Printf("🖥️  COMPUTER TOOL - Converting from DB: input was nil map, using empty map")
							}
						}
					case nil:
						// Anthropic requires input field, use empty map if nil
						block.Input = map[string]interface{}{}
						if block.Name == "computer" {
							log.Printf("🖥️  COMPUTER TOOL - Converting from DB: input was nil, using empty map")
						}
					default:
						// Try to handle other types - default to empty map
						log.Printf("Unexpected input type for tool_use %s: %T", block.Name, v)
						block.Input = map[string]interface{}{}
					}
				} else if block.Type == "tool_use" {
					// For tool_use blocks, ensure input is always present
					block.Input = map[string]interface{}{}
					if block.Name == "computer" {
						log.Printf("🖥️  COMPUTER TOOL - Converting from DB: no input field found, adding empty map")
					}
					log.Printf("Tool use %s had no input field, adding empty map", block.Name)
				}

				// Parse tool result fields
				if toolUseID, ok := blockMap["tool_use_id"].(string); ok {
					block.ToolUseID = toolUseID
				}
				if content, ok := blockMap["content"]; ok {
					// Convert server tool result types for Anthropic API compatibility
					// web_search_result -> search_result, web_fetch_result -> text
					block.Content = convertServerToolResultContent(content)

					// Debug: log tool_result content structure for history loading diagnostics
					if block.Type == "tool_result" {
						logToolResultContentDebug(block.ToolUseID, block.Content)
					}
				}
				if isError, ok := blockMap["is_error"].(bool); ok {
					block.IsError = isError
				}

				// Parse image/document source fields
				if source, ok := blockMap["source"].(map[string]interface{}); ok {
					// Convert to ContentSource
					contentSource := &ContentSource{}
					if sourceType, ok := source["type"].(string); ok {
						contentSource.Type = sourceType
					}
					if mediaType, ok := source["media_type"].(string); ok {
						contentSource.MediaType = mediaType
					}
					if data, ok := source["data"].(string); ok {
						contentSource.Data = data
					}
					// Parse file reference fields (for stored files)
					if fileID, ok := source["file_id"].(string); ok {
						contentSource.FileID = fileID
					}
					if url, ok := source["url"].(string); ok {
						contentSource.URL = url
					}
					block.Source = contentSource
				}

				blocks = append(blocks, block)
			}
		}

		// Log tool use blocks for debugging
		for _, block := range blocks {
			if block.Type == "tool_use" {
				log.Printf("Tool use block: name=%s, id=%s, input=%v", block.Name, block.ID, block.Input)
			}
		}

		// For assistant messages, Anthropic expects content blocks as an array
		// For user messages, we can use blocks as well
		msg.Content = blocks

	case []ContentBlock:
		// Already properly formatted
		msg.Content = v

	default:
		// Try to parse as JSON string (from database)
		if jsonStr, ok := v.(string); ok {
			// Try parsing as array of content blocks
			var blocks []ContentBlock
			if err := json.Unmarshal([]byte(jsonStr), &blocks); err == nil {
				msg.Content = blocks
			} else {
				// Try parsing as single content block
				var block ContentBlock
				if err := json.Unmarshal([]byte(jsonStr), &block); err == nil {
					msg.Content = []ContentBlock{block}
				} else {
					// If all else fails, treat as text
					msg.Content = jsonStr
				}
			}
		} else {
			return msg, fmt.Errorf("unsupported content type: %T", v)
		}
	}

	// Log for debugging
	log.Printf("Converted %s message: content type %T", role, msg.Content)

	return msg, nil
}

// ConvertMessagesFromDatabase converts a slice of database messages to stream messages
func ConvertMessagesFromDatabase(dbMessages []interface{}) ([]Message, error) {
	var messages []Message

	for _, dbMsg := range dbMessages {
		if msg, ok := dbMsg.(map[string]interface{}); ok {
			role, _ := msg["role"].(string)
			content := msg["content"]

			streamMsg, err := ConvertDatabaseMessageToStreamMessage(role, content)
			if err != nil {
				log.Printf("Error converting message: %v", err)
				continue
			}

			messages = append(messages, streamMsg)
		}
	}

	return messages, nil
}

// convertServerToolResultContent converts server tool result content types for Anthropic API compatibility
// When Anthropic returns web_search results, they have type "web_search_result", but when sending
// them back in a subsequent request, Anthropic expects type "search_result"
func convertServerToolResultContent(content interface{}) interface{} {
	switch v := content.(type) {
	case []interface{}:
		// Array of content blocks - convert each one
		converted := make([]interface{}, len(v))
		for i, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
				converted[i] = convertServerToolResultItem(itemMap)
			} else {
				converted[i] = item
			}
		}
		return converted
	case map[string]interface{}:
		// Single content block - must wrap in array for Anthropic API
		converted := convertServerToolResultItem(v)
		return []interface{}{converted}
	default:
		// String or other type - pass through
		return content
	}
}

// convertServerToolResultItem converts a single server tool result item
func convertServerToolResultItem(item map[string]interface{}) map[string]interface{} {
	itemType, hasType := item["type"].(string)
	if !hasType {
		return item
	}

	// Convert server tool result types to standard Anthropic text blocks
	// The raw web_search_result/web_fetch_result formats can't be re-sent as-is
	switch itemType {
	case "web_search_result":
		// Convert to text block with formatted result
		title, _ := item["title"].(string)
		url, _ := item["url"].(string)
		pageAge, _ := item["page_age"].(string)

		text := fmt.Sprintf("**%s**\nURL: %s", title, url)
		if pageAge != "" {
			text += fmt.Sprintf("\nDate: %s", pageAge)
		}

		log.Printf("📝 Converting web_search_result to text: %s", title)
		return map[string]interface{}{
			"type": "text",
			"text": text,
		}

	case "web_fetch_result":
		// Convert to text block
		var text string
		if content, ok := item["content"].(string); ok {
			text = content
		} else if title, ok := item["title"].(string); ok {
			text = title
		} else {
			text = "[web fetch result]"
		}

		log.Printf("📝 Converting web_fetch_result to text")
		return map[string]interface{}{
			"type": "text",
			"text": text,
		}

	case "web_fetch_tool_result_error":
		// Convert error result to text block
		var text string
		if errorMsg, ok := item["error"].(string); ok {
			text = fmt.Sprintf("[Web fetch error: %s]", errorMsg)
		} else if message, ok := item["message"].(string); ok {
			text = fmt.Sprintf("[Web fetch error: %s]", message)
		} else {
			text = "[Web fetch failed]"
		}

		log.Printf("📝 Converting web_fetch_tool_result_error to text")
		return map[string]interface{}{
			"type": "text",
			"text": text,
		}
	}

	return item
}

// logToolResultContentDebug logs a concise summary of tool_result content loaded from history
func logToolResultContentDebug(toolUseID string, content interface{}) {
	switch v := content.(type) {
	case string:
		log.Printf("📋 History tool_result (id=%s): string, %d chars", toolUseID, len(v))
	case []interface{}:
		imageCount, textCount := 0, 0
		totalImageBytes := 0
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				t, _ := m["type"].(string)
				if t == "image" {
					imageCount++
					if src, ok := m["source"].(map[string]interface{}); ok {
						if d, ok := src["data"].(string); ok {
							totalImageBytes += len(d)
						}
					}
				} else if t == "text" {
					textCount++
				}
			}
		}
		if imageCount > 0 {
			log.Printf("📋 History tool_result (id=%s): %d blocks (%d text, %d images, ~%s image data)",
				toolUseID, len(v), textCount, imageCount, formatDataSize(totalImageBytes))
		}
	}
}

// formatDataSize formats byte count as human-readable string
func formatDataSize(bytes int) string {
	if bytes >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
	if bytes >= 1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%dB", bytes)
}