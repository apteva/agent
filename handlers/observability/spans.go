package observability

import (
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"agent-server/events"
)

// HandleSpans - GET /spans - List spans with filters
func HandleSpans(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()
		traceID := query.Get("trace_id")
		kind := query.Get("kind")
		status := query.Get("status")
		limit := query.Get("limit")
		if limit == "" {
			limit = "100"
		}

		// Build SQL query
		sqlQuery := "SELECT id, trace_id, parent_span_id, name, kind, start_time, end_time, duration_ms, status FROM spans WHERE 1=1"
		args := []interface{}{}

		if traceID != "" {
			sqlQuery += " AND trace_id = ?"
			args = append(args, traceID)
		}
		if kind != "" {
			sqlQuery += " AND kind = ?"
			args = append(args, kind)
		}
		if status != "" {
			sqlQuery += " AND status = ?"
			args = append(args, status)
		}

		sqlQuery += " ORDER BY start_time DESC LIMIT ?"
		args = append(args, limit)

		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			log.Printf("Error querying spans: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		spans := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, traceID, parentSpanID, name, kind, status string
			var startTime time.Time
			var endTime sql.NullString
			var durationMS sql.NullInt64

			err := rows.Scan(&id, &traceID, &parentSpanID, &name, &kind, &startTime, &endTime, &durationMS, &status)
			if err != nil {
				continue
			}

			span := map[string]interface{}{
				"id":             id,
				"trace_id":       traceID,
				"parent_span_id": parentSpanID,
				"name":           name,
				"kind":           kind,
				"start_time":     startTime,
				"status":         status,
			}

			if endTime.Valid {
				t, _ := time.Parse(time.RFC3339, endTime.String)
				span["end_time"] = t
			}
			if durationMS.Valid {
				span["duration_ms"] = durationMS.Int64
			}

			spans = append(spans, span)
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"spans": spans,
			"total": len(spans),
		})
	}
}

// HandleSpan - GET /spans/:id - Get span details
func HandleSpan(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract span ID from path
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) < 3 {
			http.Error(w, "Span ID required", http.StatusBadRequest)
			return
		}
		spanID := pathParts[2]

		span, err := events.GetSpan(db, spanID)
		if err != nil {
			http.Error(w, "Span not found", http.StatusNotFound)
			return
		}

		sendJSON(w, http.StatusOK, span)
	}
}
