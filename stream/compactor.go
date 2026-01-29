package stream

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"agent-server/config"
)

// Compactor handles automatic context compaction via summarization
type Compactor struct {
	config       *config.CompactionConfig
	maxTokens    int
	db           *sql.DB
	anthropicKey string
}

// CompactionResult holds the result of a compaction operation
type CompactionResult struct {
	Summary              string    `json:"summary"`
	MessagesCompacted    int       `json:"messages_compacted"`
	OriginalTokens       int       `json:"original_tokens"`
	SummaryTokens        int       `json:"summary_tokens"`
	CompactedAt          time.Time `json:"compacted_at"`
	LastCompactedMsgID   string    `json:"last_compacted_message_id"`
	FirstCompactedMsgID  string    `json:"first_compacted_message_id"`
}

// NewCompactor creates a new compactor instance
func NewCompactor(cfg *config.CompactionConfig, maxTokens int, db *sql.DB, anthropicKey string) *Compactor {
	// Apply defaults
	if cfg.TriggerRatio == 0 {
		cfg.TriggerRatio = 0.7
	}
	if cfg.KeepRecent == 0 {
		cfg.KeepRecent = 20
	}
	if cfg.SummaryMaxTokens == 0 {
		cfg.SummaryMaxTokens = 1500
	}
	if cfg.Model == "" {
		cfg.Model = "claude-haiku-4"
	}

	return &Compactor{
		config:       cfg,
		maxTokens:    maxTokens,
		db:           db,
		anthropicKey: anthropicKey,
	}
}

// ShouldCompact checks if compaction is needed based on current token count
func (c *Compactor) ShouldCompact(messages []Message) bool {
	if !c.config.Enabled || c.maxTokens == 0 {
		return false
	}

	// Need at least keep_recent + some messages to compact
	if len(messages) <= c.config.KeepRecent+5 {
		return false
	}

	currentTokens := c.estimateTotalTokens(messages)
	threshold := int(float64(c.maxTokens) * c.config.TriggerRatio)

	shouldCompact := currentTokens > threshold
	if shouldCompact {
		log.Printf("📦 Compaction needed: %d tokens > %d threshold (%.0f%% of %d)",
			currentTokens, threshold, c.config.TriggerRatio*100, c.maxTokens)
	}

	return shouldCompact
}

// Compact performs compaction on messages, returning summary and recent messages
func (c *Compactor) Compact(threadID string, messages []Message) (string, []Message, error) {
	if len(messages) <= c.config.KeepRecent {
		return "", messages, nil
	}

	// Split messages
	cutoff := len(messages) - c.config.KeepRecent
	toCompact := messages[:cutoff]
	recent := messages[cutoff:]

	log.Printf("📦 Compacting %d messages, keeping %d recent", len(toCompact), len(recent))

	// Generate summary
	summary, err := c.generateSummary(toCompact)
	if err != nil {
		return "", messages, fmt.Errorf("failed to generate summary: %w", err)
	}

	// Get message IDs for storage
	firstMsgID := c.getMessageID(toCompact, 0)
	lastMsgID := c.getMessageID(toCompact, len(toCompact)-1)

	// Store compaction in thread metadata
	result := CompactionResult{
		Summary:             summary,
		MessagesCompacted:   len(toCompact),
		OriginalTokens:      c.estimateTotalTokens(toCompact),
		SummaryTokens:       len(summary) / 4,
		CompactedAt:         time.Now(),
		FirstCompactedMsgID: firstMsgID,
		LastCompactedMsgID:  lastMsgID,
	}

	if err := c.storeCompaction(threadID, result); err != nil {
		log.Printf("⚠️ Failed to store compaction metadata: %v", err)
		// Continue anyway - summary still works
	}

	log.Printf("📦 Compaction complete: %d messages → %d token summary",
		len(toCompact), result.SummaryTokens)

	return summary, recent, nil
}

// GetExistingCompaction retrieves any existing compaction summary for a thread
func (c *Compactor) GetExistingCompaction(threadID string) (*CompactionResult, error) {
	if c.db == nil {
		return nil, nil
	}

	var metadataStr string
	err := c.db.QueryRow("SELECT COALESCE(metadata, '{}') FROM threads WHERE id = ?", threadID).Scan(&metadataStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
		return nil, nil
	}

	compactionData, ok := metadata["compaction"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	result := &CompactionResult{}
	if summary, ok := compactionData["summary"].(string); ok {
		result.Summary = summary
	}
	if lastMsgID, ok := compactionData["last_compacted_message_id"].(string); ok {
		result.LastCompactedMsgID = lastMsgID
	}
	if compactedAt, ok := compactionData["compacted_at"].(string); ok {
		result.CompactedAt, _ = time.Parse(time.RFC3339, compactedAt)
	}

	if result.Summary == "" {
		return nil, nil
	}

	return result, nil
}

// generateSummary calls the LLM to generate a summary of messages
func (c *Compactor) generateSummary(messages []Message) (string, error) {
	// Build conversation text for summarization
	var conversationText strings.Builder
	for _, msg := range messages {
		role := strings.ToUpper(msg.Role)
		content := c.extractTextContent(msg)
		if content != "" {
			conversationText.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
		}
	}

	prompt := fmt.Sprintf(`Summarize this conversation segment for context continuity. The summary will be prepended to future messages so the AI maintains awareness of what happened.

EXTRACT AND PRESERVE:
1. Key decisions made (with brief reasoning)
2. Important facts, data, or values mentioned
3. File paths, IDs, or code references
4. Errors encountered and their solutions
5. User preferences expressed
6. Current state of any ongoing work
7. Tools used and their outcomes

FORMAT:
- Use bullet points, be concise but complete
- Preserve exact values (IDs, paths, numbers, names)
- Note any unfinished tasks or pending items
- Keep under %d tokens

CONVERSATION TO SUMMARIZE:
%s`, c.config.SummaryMaxTokens, conversationText.String())

	// Call Anthropic API for summary
	summary, err := c.callAnthropicAPI(prompt)
	if err != nil {
		return "", err
	}

	return summary, nil
}

// callAnthropicAPI makes a request to Anthropic for summarization
func (c *Compactor) callAnthropicAPI(prompt string) (string, error) {
	if c.anthropicKey == "" {
		return "", fmt.Errorf("no Anthropic API key configured")
	}

	requestBody := map[string]interface{}{
		"model":      c.config.Model,
		"max_tokens": c.config.SummaryMaxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.anthropicKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Anthropic API error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}

	if len(response.Content) == 0 {
		return "", fmt.Errorf("empty response from Anthropic")
	}

	return response.Content[0].Text, nil
}

// storeCompaction saves compaction result to thread metadata
func (c *Compactor) storeCompaction(threadID string, result CompactionResult) error {
	if c.db == nil {
		return nil
	}

	// Get existing metadata
	var metadataStr string
	err := c.db.QueryRow("SELECT COALESCE(metadata, '{}') FROM threads WHERE id = ?", threadID).Scan(&metadataStr)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
		metadata = make(map[string]interface{})
	}

	// Update compaction data
	metadata["compaction"] = map[string]interface{}{
		"summary":                     result.Summary,
		"messages_compacted":          result.MessagesCompacted,
		"original_tokens":             result.OriginalTokens,
		"summary_tokens":              result.SummaryTokens,
		"compacted_at":                result.CompactedAt.Format(time.RFC3339),
		"first_compacted_message_id":  result.FirstCompactedMsgID,
		"last_compacted_message_id":   result.LastCompactedMsgID,
	}

	newMetadata, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	_, err = c.db.Exec("UPDATE threads SET metadata = ?, updated_at = ? WHERE id = ?",
		string(newMetadata), time.Now(), threadID)
	return err
}

// estimateTotalTokens estimates token count for all messages
func (c *Compactor) estimateTotalTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += c.estimateMessageTokens(&msg)
	}
	return total
}

// estimateMessageTokens estimates tokens for a single message
func (c *Compactor) estimateMessageTokens(msg *Message) int {
	tokens := 4 // Base overhead

	switch content := msg.Content.(type) {
	case string:
		tokens += len(content) / 4
	case []ContentBlock:
		for _, block := range content {
			tokens += c.estimateBlockTokens(&block)
		}
	case []interface{}:
		for _, block := range content {
			if blockMap, ok := block.(map[string]interface{}); ok {
				tokens += c.estimateMapBlockTokens(blockMap)
			}
		}
	}

	return tokens
}

// estimateBlockTokens estimates tokens for a ContentBlock
func (c *Compactor) estimateBlockTokens(block *ContentBlock) int {
	tokens := 3

	switch block.Type {
	case "text":
		tokens += len(block.Text) / 4
	case "tool_use":
		tokens += len(block.Name) / 4
		if block.Input != nil {
			inputJSON, _ := json.Marshal(block.Input)
			tokens += len(inputJSON) / 4
		}
	case "tool_result":
		if contentStr, ok := block.Content.(string); ok {
			tokens += len(contentStr) / 4
		} else if block.Content != nil {
			contentJSON, _ := json.Marshal(block.Content)
			tokens += len(contentJSON) / 4
		}
	case "image":
		tokens += 1000
	}

	return tokens
}

// estimateMapBlockTokens estimates tokens for a map-based block
func (c *Compactor) estimateMapBlockTokens(blockMap map[string]interface{}) int {
	tokens := 3
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
		if input, ok := blockMap["input"]; ok {
			inputJSON, _ := json.Marshal(input)
			tokens += len(inputJSON) / 4
		}
	case "tool_result":
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

// extractTextContent extracts readable text from a message for summarization
func (c *Compactor) extractTextContent(msg Message) string {
	var parts []string

	switch content := msg.Content.(type) {
	case string:
		return content
	case []ContentBlock:
		for _, block := range content {
			switch block.Type {
			case "text":
				parts = append(parts, block.Text)
			case "tool_use":
				inputJSON, _ := json.Marshal(block.Input)
				parts = append(parts, fmt.Sprintf("[Tool: %s, Input: %s]", block.Name, string(inputJSON)))
			case "tool_result":
				if contentStr, ok := block.Content.(string); ok {
					// Truncate long tool results
					if len(contentStr) > 500 {
						contentStr = contentStr[:500] + "..."
					}
					parts = append(parts, fmt.Sprintf("[Tool Result: %s]", contentStr))
				}
			}
		}
	case []interface{}:
		for _, block := range content {
			if blockMap, ok := block.(map[string]interface{}); ok {
				blockType, _ := blockMap["type"].(string)
				switch blockType {
				case "text":
					if text, ok := blockMap["text"].(string); ok {
						parts = append(parts, text)
					}
				case "tool_use":
					name, _ := blockMap["name"].(string)
					input, _ := blockMap["input"]
					inputJSON, _ := json.Marshal(input)
					parts = append(parts, fmt.Sprintf("[Tool: %s, Input: %s]", name, string(inputJSON)))
				case "tool_result":
					if content, ok := blockMap["content"]; ok {
						contentStr := fmt.Sprintf("%v", content)
						if len(contentStr) > 500 {
							contentStr = contentStr[:500] + "..."
						}
						parts = append(parts, fmt.Sprintf("[Tool Result: %s]", contentStr))
					}
				}
			}
		}
	}

	return strings.Join(parts, " ")
}

// getMessageID extracts message ID from a message at given index
func (c *Compactor) getMessageID(messages []Message, index int) string {
	if index < 0 || index >= len(messages) {
		return ""
	}
	// Messages loaded from DB should have IDs in metadata or we generate a placeholder
	// For now, use index-based ID
	return fmt.Sprintf("msg_%d", index)
}
