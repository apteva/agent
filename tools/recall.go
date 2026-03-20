package tools

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/apteva/agent/memory"
)

var recallDB *sql.DB
var recallMemoryManager *memory.MemoryManager

// SetRecallDeps sets the database and memory manager for the recall tool.
func SetRecallDeps(db *sql.DB, mm *memory.MemoryManager) {
	recallDB = db
	recallMemoryManager = mm
}

// RecallToolWrapper implements the Tool interface for recall.
type RecallToolWrapper struct{}

func (t *RecallToolWrapper) Name() string        { return "recall" }
func (t *RecallToolWrapper) DisplayName() string  { return "Recall" }

func (t *RecallToolWrapper) Description() string {
	return `Search your memory and past conversations. Use this to look up things you've learned, find previous discussions, or remember context from earlier threads.

Scopes:
- "all" (default): search both memories and conversations
- "memories": only search stored knowledge (facts, preferences, documents)
- "conversations": only search past chat threads and messages

Examples:
- recall(query: "deployment process") — find anything related to deployment
- recall(query: "user preferences", scope: "memories") — search only stored memories
- recall(query: "database migration", scope: "conversations") — find past discussions`
}

func (t *RecallToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "What to search for — a topic, keyword, or natural language question",
			},
			"scope": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"all", "memories", "conversations"},
				"description": "Where to search: 'all' (default), 'memories' (stored knowledge), or 'conversations' (past threads)",
				"default":     "all",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max results per source (default 5)",
				"default":     5,
				"minimum":     1,
				"maximum":     20,
			},
		},
		"required": []string{"query"},
	}
}

func (t *RecallToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return RecallTool(params)
}

// RegisterRecallTool registers the recall tool in the global registry.
func RegisterRecallTool() {
	registry := GetGlobalRegistry()
	registry.RegisterTool(&RecallToolWrapper{})
}

// RecallTool searches memories and/or past conversations.
func RecallTool(input map[string]interface{}) (interface{}, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	scope, _ := input["scope"].(string)
	if scope == "" {
		scope = "all"
	}

	limit := 5
	if l, ok := input["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 20 {
			limit = 20
		}
	}

	result := map[string]interface{}{
		"query": query,
		"scope": scope,
	}

	// Search memories
	if scope == "all" || scope == "memories" {
		memories := searchMemories(query, limit)
		result["memories"] = memories
		result["memories_count"] = len(memories)
	}

	// Search conversations
	if scope == "all" || scope == "conversations" {
		conversations := searchConversations(query, limit)
		result["conversations"] = conversations
		result["conversations_count"] = len(conversations)
	}

	return result, nil
}

type memoryResult struct {
	Content    string  `json:"content"`
	Category   string  `json:"category,omitempty"`
	Source     string  `json:"source,omitempty"`
	SourceName string  `json:"source_name,omitempty"`
	Similarity float64 `json:"similarity,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

func searchMemories(query string, limit int) []memoryResult {
	if recallMemoryManager == nil {
		return []memoryResult{}
	}

	memories, err := recallMemoryManager.RetrieveRelevant(query, "", limit)
	if err != nil {
		log.Printf("[Recall] Memory search error: %v", err)
		return []memoryResult{}
	}

	results := make([]memoryResult, 0, len(memories))
	for _, mem := range memories {
		r := memoryResult{
			Content:    mem.Content,
			Category:   mem.Category,
			Source:     string(mem.Source),
			SourceName: mem.SourceName,
			CreatedAt:  mem.CreatedAt.Format(time.RFC3339),
		}
		if mem.Metadata != nil {
			if sim, ok := mem.Metadata["similarity"].(float32); ok {
				r.Similarity = float64(sim)
			}
		}
		results = append(results, r)
	}

	return results
}

type conversationResult struct {
	ThreadID    string           `json:"thread_id"`
	Title       string           `json:"title"`
	UpdatedAt   string           `json:"updated_at"`
	MessageCount int             `json:"message_count"`
	Snippets    []messageSnippet `json:"snippets,omitempty"`
}

type messageSnippet struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

func searchConversations(query string, limit int) []conversationResult {
	if recallDB == nil {
		return []conversationResult{}
	}

	// Build search terms for SQL LIKE
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return []conversationResult{}
	}

	// Search threads by title match + message content match
	// First find threads with matching titles or messages
	var conditions []string
	var args []interface{}
	for _, word := range words {
		pattern := "%" + word + "%"
		conditions = append(conditions, "(LOWER(t.title) LIKE ? OR LOWER(m.content) LIKE ?)")
		args = append(args, pattern, pattern)
	}

	searchQuery := fmt.Sprintf(`
		SELECT DISTINCT t.id, t.title, t.updated_at,
		       (SELECT COUNT(*) FROM messages WHERE thread_id = t.id) as msg_count
		FROM threads t
		LEFT JOIN messages m ON t.id = m.thread_id
		WHERE (%s)
		  AND COALESCE(t.type, 'chat') = 'chat'
		ORDER BY t.updated_at DESC
		LIMIT ?
	`, strings.Join(conditions, " AND "))
	args = append(args, limit)

	rows, err := recallDB.Query(searchQuery, args...)
	if err != nil {
		log.Printf("[Recall] Conversation search error: %v", err)
		return []conversationResult{}
	}
	defer rows.Close()

	var results []conversationResult
	for rows.Next() {
		var r conversationResult
		var updatedAt time.Time
		if err := rows.Scan(&r.ThreadID, &r.Title, &updatedAt, &r.MessageCount); err != nil {
			log.Printf("[Recall] Scan error: %v", err)
			continue
		}
		r.UpdatedAt = updatedAt.Format(time.RFC3339)

		// Fetch matching message snippets from this thread
		r.Snippets = findMatchingSnippets(r.ThreadID, words, 3)

		results = append(results, r)
	}

	return results
}

// findMatchingSnippets returns message snippets from a thread that match the search words.
func findMatchingSnippets(threadID string, words []string, limit int) []messageSnippet {
	if recallDB == nil {
		return nil
	}

	// Build conditions for message content matching
	var conditions []string
	var args []interface{}
	args = append(args, threadID)
	for _, word := range words {
		conditions = append(conditions, "LOWER(content) LIKE ?")
		args = append(args, "%"+word+"%")
	}

	query := fmt.Sprintf(`
		SELECT role, content, created_at FROM messages
		WHERE thread_id = ? AND (%s)
		ORDER BY created_at DESC LIMIT ?
	`, strings.Join(conditions, " OR "))
	args = append(args, limit)

	rows, err := recallDB.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var snippets []messageSnippet
	for rows.Next() {
		var s messageSnippet
		var contentStr string
		var createdAt time.Time
		if err := rows.Scan(&s.Role, &contentStr, &createdAt); err != nil {
			continue
		}
		s.CreatedAt = createdAt.Format(time.RFC3339)
		s.Content = extractSnippet(contentStr, words, 200)
		snippets = append(snippets, s)
	}

	return snippets
}

// extractSnippet extracts a relevant portion of content around matching words.
func extractSnippet(content string, words []string, maxLen int) string {
	// Try to parse JSON content blocks (assistant messages store structured content)
	var textContent string
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "[") {
		var blocks []map[string]interface{}
		if err := json.Unmarshal([]byte(content), &blocks); err == nil {
			var texts []string
			for _, block := range blocks {
				if block["type"] == "text" {
					if text, ok := block["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
			textContent = strings.Join(texts, " ")
		}
	}
	if textContent == "" {
		textContent = content
	}

	lower := strings.ToLower(textContent)

	// Find earliest match position
	bestPos := -1
	for _, word := range words {
		pos := strings.Index(lower, word)
		if pos >= 0 && (bestPos < 0 || pos < bestPos) {
			bestPos = pos
		}
	}

	if bestPos < 0 || len(textContent) <= maxLen {
		if len(textContent) > maxLen {
			return textContent[:maxLen] + "..."
		}
		return textContent
	}

	// Center the snippet around the match
	start := bestPos - maxLen/3
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(textContent) {
		end = len(textContent)
		start = end - maxLen
		if start < 0 {
			start = 0
		}
	}

	snippet := textContent[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(textContent) {
		snippet = snippet + "..."
	}
	return snippet
}
