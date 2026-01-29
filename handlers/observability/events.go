package observability

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// HandleEventsQuery - GET /events/query - Query events from database
func HandleEventsQuery(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()
		category := query.Get("category")
		level := query.Get("level")
		traceID := query.Get("trace_id")
		spanID := query.Get("span_id")
		limit := query.Get("limit")
		if limit == "" {
			limit = "200"
		}

		// Build SQL query
		sqlQuery := "SELECT id, trace_id, span_id, timestamp, category, type, level, thread_id, data, metadata, error FROM events WHERE 1=1"
		args := []interface{}{}

		if category != "" {
			sqlQuery += " AND category = ?"
			args = append(args, category)
		}
		if level != "" {
			sqlQuery += " AND level = ?"
			args = append(args, level)
		}
		if traceID != "" {
			sqlQuery += " AND trace_id = ?"
			args = append(args, traceID)
		}
		if spanID != "" {
			sqlQuery += " AND span_id = ?"
			args = append(args, spanID)
		}

		sqlQuery += " ORDER BY timestamp DESC LIMIT ?"
		args = append(args, limit)

		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			log.Printf("Error querying events: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		eventsData := make([]map[string]interface{}, 0)
		for rows.Next() {
			var (
				id, category, typeStr, level string
				timestamp                    time.Time
				traceID, spanID, threadID    sql.NullString
				dataJSON, metadataJSON, errorStr sql.NullString
			)

			err := rows.Scan(&id, &traceID, &spanID, &timestamp, &category, &typeStr, &level, &threadID, &dataJSON, &metadataJSON, &errorStr)
			if err != nil {
				continue
			}

			event := map[string]interface{}{
				"id":        id,
				"timestamp": timestamp,
				"category":  category,
				"type":      typeStr,
				"level":     level,
			}

			if traceID.Valid {
				event["trace_id"] = traceID.String
			}
			if spanID.Valid {
				event["span_id"] = spanID.String
			}
			if threadID.Valid {
				event["thread_id"] = threadID.String
			}
			if dataJSON.Valid && dataJSON.String != "" && dataJSON.String != "{}" {
				var data map[string]interface{}
				json.Unmarshal([]byte(dataJSON.String), &data)
				event["data"] = data
			}
			if metadataJSON.Valid && metadataJSON.String != "" && metadataJSON.String != "{}" {
				var metadata map[string]interface{}
				json.Unmarshal([]byte(metadataJSON.String), &metadata)
				event["metadata"] = metadata
			}
			if errorStr.Valid {
				event["error"] = errorStr.String
			}

			eventsData = append(eventsData, event)
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"events": eventsData,
			"total":  len(eventsData),
		})
	}
}

// HandleEventsForTrace - GET /events/trace/:trace_id - Get events for trace
func HandleEventsForTrace(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract trace ID from path
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) < 4 {
			http.Error(w, "Trace ID required", http.StatusBadRequest)
			return
		}
		traceID := pathParts[3]

		rows, err := db.Query(`
		SELECT id, span_id, timestamp, category, type, level, data, metadata, error
		FROM events
		WHERE trace_id = ?
		ORDER BY timestamp`, traceID)

		if err != nil {
			log.Printf("Error querying events: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		eventsData := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, spanID, category, typeStr, level string
			var timestamp time.Time
			var dataJSON, metadataJSON, errorStr sql.NullString

			err := rows.Scan(&id, &spanID, &timestamp, &category, &typeStr, &level, &dataJSON, &metadataJSON, &errorStr)
			if err != nil {
				continue
			}

			event := map[string]interface{}{
				"id":        id,
				"span_id":   spanID,
				"timestamp": timestamp,
				"category":  category,
				"type":      typeStr,
				"level":     level,
			}

			if dataJSON.Valid {
				var data map[string]interface{}
				json.Unmarshal([]byte(dataJSON.String), &data)
				event["data"] = data
			}
			if metadataJSON.Valid {
				var metadata map[string]interface{}
				json.Unmarshal([]byte(metadataJSON.String), &metadata)
				event["metadata"] = metadata
			}
			if errorStr.Valid {
				event["error"] = errorStr.String
			}

			eventsData = append(eventsData, event)
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"trace_id": traceID,
			"events":   eventsData,
			"total":    len(eventsData),
		})
	}
}
