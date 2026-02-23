package threads

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// HandleThreads - GET /threads - List all threads
// Query params:
//   - all=true: show all threads including children
//   - type=task|scheduler|reflection|chat: filter by type
func HandleThreads(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		showAll := r.URL.Query().Get("all") == "true"
		filterType := r.URL.Query().Get("type")

		// Query all threads with message count
		query := `
			SELECT t.id, t.title, COALESCE(t.type, 'chat'), t.parent_id, t.activity,
			       t.created_at, t.updated_at, t.metadata,
			       COALESCE(COUNT(m.id), 0) as message_count
			FROM threads t
			LEFT JOIN messages m ON t.id = m.thread_id
		`

		var args []interface{}
		var conditions []string

		if filterType != "" {
			conditions = append(conditions, "t.type = ?")
			args = append(args, filterType)
		}

		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}

		query += " GROUP BY t.id ORDER BY t.updated_at DESC"

		rows, err := db.Query(query, args...)
		if err != nil {
			log.Printf("Database error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		allThreads := make([]Thread, 0)
		for rows.Next() {
			var thread Thread
			var metadataStr string
			var threadType sql.NullString
			err := rows.Scan(&thread.ID, &thread.Title, &threadType, &thread.ParentID, &thread.Activity,
				&thread.CreatedAt, &thread.UpdatedAt, &metadataStr, &thread.MessageCount)
			if err != nil {
				log.Printf("Database scan error: %v", err)
				http.Error(w, "Database scan error", http.StatusInternalServerError)
				return
			}

			thread.Type = "chat"
			if threadType.Valid && threadType.String != "" {
				thread.Type = threadType.String
			}

			// Parse metadata JSON
			if err := json.Unmarshal([]byte(metadataStr), &thread.Metadata); err != nil {
				log.Printf("Metadata parse error: %v", err)
				thread.Metadata = make(map[string]interface{})
			}

			allThreads = append(allThreads, thread)
		}

		if err := rows.Err(); err != nil {
			log.Printf("Rows iteration error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		var result []Thread
		if showAll {
			result = allThreads
		} else {
			// Build parent→children map and return only top-level threads
			// with child threads nested inside
			childMap := make(map[string][]Thread) // parentID → children
			for _, t := range allThreads {
				if t.ParentID != nil && *t.ParentID != "" {
					childMap[*t.ParentID] = append(childMap[*t.ParentID], t)
				}
			}

			for _, t := range allThreads {
				if t.ParentID != nil && *t.ParentID != "" {
					continue // skip children at top level
				}
				// Attach children if any
				if children, ok := childMap[t.ID]; ok {
					t.Children = children
				}
				result = append(result, t)
			}
		}

		if result == nil {
			result = []Thread{}
		}

		response := map[string]interface{}{
			"threads": result,
		}
		sendJSON(w, http.StatusOK, response)
	}
}

// HandleThreadMessages - GET /threads/:id/messages - Get messages for a thread
func HandleThreadMessages(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract thread ID from path: /threads/{threadId}/messages
		path := strings.TrimPrefix(r.URL.Path, "/threads/")
		if !strings.HasSuffix(path, "/messages") {
			http.Error(w, "Invalid path. Use /threads/{threadId}/messages", http.StatusBadRequest)
			return
		}

		threadID := strings.TrimSuffix(path, "/messages")
		if threadID == "" {
			http.Error(w, "Thread ID required", http.StatusBadRequest)
			return
		}

		rows, err := db.Query("SELECT id, thread_id, role, content, model, created_at, metadata FROM messages WHERE thread_id = ? ORDER BY created_at ASC", threadID)
		if err != nil {
			log.Printf("Database error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		messages := make([]Message, 0)
		for rows.Next() {
			var message Message
			var contentStr, metadataStr string
			err := rows.Scan(&message.ID, &message.ThreadID, &message.Role, &contentStr, &message.Model, &message.CreatedAt, &metadataStr)
			if err != nil {
				log.Printf("Database scan error: %v", err)
				http.Error(w, "Database scan error", http.StatusInternalServerError)
				return
			}

			// Parse content - could be string or JSON content blocks
			var content interface{}

			// First check if it looks like JSON (starts with { or [)
			trimmedContent := strings.TrimSpace(contentStr)
			if strings.HasPrefix(trimmedContent, "{") || strings.HasPrefix(trimmedContent, "[") {
				// Try to parse as JSON
				if err := json.Unmarshal([]byte(contentStr), &content); err != nil {
					// If JSON parsing fails, treat as plain string
					content = contentStr
				} else {
					// Successfully parsed as JSON - keep the parsed structure
					// This will be a map[string]interface{} for tool_use/tool_result objects
					// or []interface{} for arrays
				}
			} else {
				// Plain string content
				content = contentStr
			}
			message.Content = content

			// Parse metadata JSON
			if err := json.Unmarshal([]byte(metadataStr), &message.Metadata); err != nil {
				log.Printf("Metadata parse error: %v", err)
				message.Metadata = make(map[string]interface{})
			}

			messages = append(messages, message)
		}

		if err := rows.Err(); err != nil {
			log.Printf("Rows iteration error: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		// Wrap in expected format for frontend
		response := map[string]interface{}{
			"messages": messages,
		}
		sendJSON(w, http.StatusOK, response)
	}
}

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}
