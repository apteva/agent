package activity

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// HandleActivity - GET /activity - Returns a structured summary of recent agent activity
// Aggregates data from threads, messages, traces, and spans tables.
// Query params:
//   - since: duration string (e.g. "1h", "24h", "7d") - default "1h"
//   - limit: max recent threads to return (default 20, max 100)
func HandleActivity(db *sql.DB, agentID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()

		// Parse "since" duration
		sinceStr := query.Get("since")
		if sinceStr == "" {
			sinceStr = "1h"
		}
		since, err := parseDuration(sinceStr)
		if err != nil {
			since = 1 * time.Hour
		}
		cutoff := time.Now().Add(-since)

		// Parse limit
		limit := 20
		if l := query.Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}

		// Build response
		summary := querySummary(db, cutoff)
		recentThreads := queryRecentThreads(db, cutoff, limit)

		response := map[string]interface{}{
			"agent_id": agentID,
			"period": map[string]interface{}{
				"since": sinceStr,
				"from":  cutoff.Format(time.RFC3339),
				"to":    time.Now().Format(time.RFC3339),
			},
			"summary":        summary,
			"recent_threads": recentThreads,
		}

		sendJSON(w, http.StatusOK, response)
	}
}

// querySummary aggregates high-level stats from threads, messages, traces, and spans.
func querySummary(db *sql.DB, cutoff time.Time) map[string]interface{} {
	summary := map[string]interface{}{
		"threads_active":      0,
		"threads_created":     0,
		"messages_sent":       0,
		"messages_received":   0,
		"tools_invoked":       0,
		"tasks_completed":     0,
		"total_tokens":        0,
		"total_input_tokens":  0,
		"total_output_tokens": 0,
		"errors":              0,
	}

	cutoffStr := cutoff.Format(time.RFC3339)

	// Threads active (updated since cutoff)
	var threadsActive int
	if err := db.QueryRow("SELECT COUNT(*) FROM threads WHERE updated_at >= ?", cutoffStr).Scan(&threadsActive); err == nil {
		summary["threads_active"] = threadsActive
	}

	// Threads created since cutoff
	var threadsCreated int
	if err := db.QueryRow("SELECT COUNT(*) FROM threads WHERE created_at >= ?", cutoffStr).Scan(&threadsCreated); err == nil {
		summary["threads_created"] = threadsCreated
	}

	// Messages sent (role=assistant) and received (role=user) since cutoff
	var msgSent, msgReceived int
	if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE role = 'assistant' AND created_at >= ?", cutoffStr).Scan(&msgSent); err == nil {
		summary["messages_sent"] = msgSent
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE role = 'user' AND created_at >= ?", cutoffStr).Scan(&msgReceived); err == nil {
		summary["messages_received"] = msgReceived
	}

	// Token usage and tool calls from spans
	var inputTokens, outputTokens, toolCalls int
	err := db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN kind = 'llm' THEN token_usage_input ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN kind = 'llm' THEN token_usage_output ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN kind = 'tool' THEN 1 ELSE 0 END), 0)
		FROM spans
		WHERE start_time >= ?
	`, cutoffStr).Scan(&inputTokens, &outputTokens, &toolCalls)
	if err == nil {
		summary["total_input_tokens"] = inputTokens
		summary["total_output_tokens"] = outputTokens
		summary["total_tokens"] = inputTokens + outputTokens
		summary["tools_invoked"] = toolCalls
	}

	// Tasks completed since cutoff
	var tasksCompleted int
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks WHERE status = 'completed' AND executed_at >= ?", cutoffStr).Scan(&tasksCompleted); err == nil {
		summary["tasks_completed"] = tasksCompleted
	}

	// Errors (traces with status=error)
	var errors int
	if err := db.QueryRow("SELECT COUNT(*) FROM traces WHERE status = 'error' AND start_time >= ?", cutoffStr).Scan(&errors); err == nil {
		summary["errors"] = errors
	}

	// Average response time from traces
	var avgDuration sql.NullFloat64
	if err := db.QueryRow("SELECT AVG(duration_ms) FROM traces WHERE start_time >= ? AND duration_ms IS NOT NULL", cutoffStr).Scan(&avgDuration); err == nil && avgDuration.Valid {
		summary["avg_response_time_ms"] = int(avgDuration.Float64)
	}

	return summary
}

// queryRecentThreads returns the most recently active threads with their activity summaries.
func queryRecentThreads(db *sql.DB, cutoff time.Time, limit int) []map[string]interface{} {
	cutoffStr := cutoff.Format(time.RFC3339)

	rows, err := db.Query(`
		SELECT t.id, t.title, t.activity, t.created_at, t.updated_at,
		       COALESCE(COUNT(m.id), 0) as message_count,
		       (SELECT source FROM traces tr WHERE tr.thread_id = t.id ORDER BY tr.start_time ASC LIMIT 1) as source
		FROM threads t
		LEFT JOIN messages m ON t.id = m.thread_id
		WHERE t.updated_at >= ?
		GROUP BY t.id
		ORDER BY t.updated_at DESC
		LIMIT ?
	`, cutoffStr, limit)
	if err != nil {
		log.Printf("Error querying recent threads for activity: %v", err)
		return []map[string]interface{}{}
	}
	defer rows.Close()

	threads := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id string
		var title, activity sql.NullString
		var createdAt, updatedAt string
		var messageCount int
		var source sql.NullString

		if err := rows.Scan(&id, &title, &activity, &createdAt, &updatedAt, &messageCount, &source); err != nil {
			log.Printf("Activity thread scan error: %v", err)
			continue
		}

		thread := map[string]interface{}{
			"thread_id":     id,
			"messages":      messageCount,
			"last_active":   updatedAt,
		}

		if title.Valid && title.String != "" {
			thread["title"] = title.String
		}
		if activity.Valid && activity.String != "" {
			thread["activity"] = activity.String
		}
		if source.Valid && source.String != "" {
			thread["source"] = source.String
		} else {
			thread["source"] = "chat"
		}

		// Parse created_at to check if this thread started within the period
		thread["started"] = createdAt

		threads = append(threads, thread)
	}

	return threads
}

// parseDuration extends time.ParseDuration to support "d" (days) suffix.
func parseDuration(s string) (time.Duration, error) {
	// Handle day suffix
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}
