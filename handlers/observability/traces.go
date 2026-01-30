package observability

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apteva/agent/events"
)

// HandleTraces - GET /traces - List traces with filters
// Supports group_by=thread to aggregate traces by thread_id
func HandleTraces(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()
		groupBy := query.Get("group_by")
		status := query.Get("status")
		threadID := query.Get("thread_id")
		source := query.Get("source")
		topLevel := query.Get("top_level") == "true"
		startTime := query.Get("start_time")
		endTime := query.Get("end_time")

		limit := 50
		if l := query.Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		offset := 0
		if o := query.Get("offset"); o != "" {
			if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
				offset = parsed
			}
		}

		// Check for trajectory inclusion
		includeTrajectory := query.Get("include_trajectory") == "true"

		// Handle group_by=thread aggregation
		if groupBy == "thread" {
			handleTracesGroupedByThread(db, w, query, limit, offset, includeTrajectory)
			return
		}

		// Build SQL query with token aggregation
		sqlQuery := `
			SELECT t.id, t.name, t.thread_id, t.service_name, t.start_time, t.end_time,
			       t.duration_ms, t.status, t.span_count, t.error_count,
			       t.parent_trace_id, t.source,
			       COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN s.token_usage_input ELSE 0 END), 0) as input_tokens,
			       COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN s.token_usage_output ELSE 0 END), 0) as output_tokens,
			       COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN 1 ELSE 0 END), 0) as llm_calls,
			       COALESCE(SUM(CASE WHEN s.kind = 'tool' THEN 1 ELSE 0 END), 0) as tool_calls
			FROM traces t
			LEFT JOIN spans s ON s.trace_id = t.id
			WHERE 1=1
		`
		args := []interface{}{}

		if topLevel {
			sqlQuery += " AND t.parent_trace_id IS NULL"
		}
		if status != "" {
			sqlQuery += " AND t.status = ?"
			args = append(args, status)
		}
		if threadID != "" {
			sqlQuery += " AND t.thread_id = ?"
			args = append(args, threadID)
		}
		if source != "" {
			sqlQuery += " AND t.source = ?"
			args = append(args, source)
		}
		if startTime != "" {
			sqlQuery += " AND t.start_time >= ?"
			args = append(args, startTime)
		}
		if endTime != "" {
			sqlQuery += " AND t.end_time <= ?"
			args = append(args, endTime)
		}

		sqlQuery += " GROUP BY t.id ORDER BY t.start_time DESC LIMIT ? OFFSET ?"
		args = append(args, limit, offset)

		rows, err := db.Query(sqlQuery, args...)
		if err != nil {
			log.Printf("Error querying traces: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		traces := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, name, serviceName, status string
			var threadIDVal, parentTraceID, sourceVal sql.NullString
			var startTimeVal time.Time
			var endTimeVal sql.NullString
			var durationMS, spanCount, errorCount sql.NullInt64
			var inputTokens, outputTokens, llmCalls, toolCalls int

			err := rows.Scan(&id, &name, &threadIDVal, &serviceName, &startTimeVal, &endTimeVal,
				&durationMS, &status, &spanCount, &errorCount, &parentTraceID, &sourceVal,
				&inputTokens, &outputTokens, &llmCalls, &toolCalls)
			if err != nil {
				log.Printf("Scan error: %v", err)
				continue
			}

			trace := map[string]interface{}{
				"id":           id,
				"name":         name,
				"service_name": serviceName,
				"started_at":   startTimeVal,
				"status":       status,
				"usage": map[string]interface{}{
					"input_tokens":  inputTokens,
					"output_tokens": outputTokens,
					"total_tokens":  inputTokens + outputTokens,
					"llm_calls":     llmCalls,
					"tool_calls":    toolCalls,
				},
			}

			if threadIDVal.Valid {
				trace["thread_id"] = threadIDVal.String
			}
			if parentTraceID.Valid {
				trace["parent_trace_id"] = parentTraceID.String
			}
			if sourceVal.Valid {
				trace["source"] = sourceVal.String
			} else {
				trace["source"] = "chat"
			}
			if endTimeVal.Valid {
				t, _ := time.Parse(time.RFC3339, endTimeVal.String)
				trace["ended_at"] = t
			}
			if durationMS.Valid {
				trace["duration_ms"] = durationMS.Int64
			}
			if spanCount.Valid {
				trace["span_count"] = spanCount.Int64
			}
			if errorCount.Valid {
				trace["error_count"] = errorCount.Int64
			}

			traces = append(traces, trace)
		}

		// Optionally fetch trajectory for each trace
		if includeTrajectory {
			for i, trace := range traces {
				traceID := trace["id"].(string)
				trajectory := getTrajectoryForTrace(db, traceID)
				traces[i]["trajectory"] = trajectory
			}
		}

		// Get total count
		countQuery := "SELECT COUNT(*) FROM traces WHERE 1=1"
		countArgs := []interface{}{}
		if topLevel {
			countQuery += " AND parent_trace_id IS NULL"
		}
		if status != "" {
			countQuery += " AND status = ?"
			countArgs = append(countArgs, status)
		}
		if threadID != "" {
			countQuery += " AND thread_id = ?"
			countArgs = append(countArgs, threadID)
		}
		if source != "" {
			countQuery += " AND source = ?"
			countArgs = append(countArgs, source)
		}

		var total int
		db.QueryRow(countQuery, countArgs...).Scan(&total)

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"traces": traces,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

// HandleTrace - GET /traces/:id - Get trace details with spans
// Also handles /traces/:id/usage for usage rollup
func HandleTrace(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract trace ID and check for /usage suffix
		path := strings.TrimPrefix(r.URL.Path, "/traces/")
		if strings.HasSuffix(path, "/usage") {
			handleTraceUsage(db, w, r, strings.TrimSuffix(path, "/usage"))
			return
		}

		traceID := path
		if traceID == "" || strings.Contains(traceID, "/") {
			http.Error(w, "Invalid trace ID", http.StatusBadRequest)
			return
		}

		trace, err := events.GetTrace(db, traceID)
		if err != nil {
			http.Error(w, "Trace not found", http.StatusNotFound)
			return
		}

		// Get all spans for this trace
		spans, err := events.GetSpansForTrace(db, traceID)
		if err != nil {
			log.Printf("Error getting spans for trace %s: %v", traceID, err)
			spans = []*events.Span{}
		}

		// Calculate usage from spans
		var inputTokens, outputTokens, llmCalls, toolCalls int
		for _, span := range spans {
			if span.Kind == "llm" {
				inputTokens += span.TokenUsageIn
				outputTokens += span.TokenUsageOut
				llmCalls++
			} else if span.Kind == "tool" {
				toolCalls++
			}
		}

		// Get child traces
		childRows, err := db.Query(`
			SELECT id, name, thread_id, source, status, duration_ms, span_count, error_count, start_time, end_time
			FROM traces WHERE parent_trace_id = ? ORDER BY start_time
		`, traceID)

		children := make([]map[string]interface{}, 0)
		if err == nil {
			defer childRows.Close()
			for childRows.Next() {
				var id, name, status string
				var threadID, source, endTime sql.NullString
				var durationMS, spanCount, errorCount sql.NullInt64
				var startTime time.Time

				if err := childRows.Scan(&id, &name, &threadID, &source, &status, &durationMS, &spanCount, &errorCount, &startTime, &endTime); err == nil {
					child := map[string]interface{}{
						"id":         id,
						"name":       name,
						"status":     status,
						"started_at": startTime,
					}
					if threadID.Valid {
						child["thread_id"] = threadID.String
					}
					if source.Valid {
						child["source"] = source.String
					}
					if durationMS.Valid {
						child["duration_ms"] = durationMS.Int64
					}
					if spanCount.Valid {
						child["span_count"] = spanCount.Int64
					}
					if errorCount.Valid {
						child["error_count"] = errorCount.Int64
					}
					if endTime.Valid {
						if t, err := time.Parse(time.RFC3339, endTime.String); err == nil {
							child["ended_at"] = t
						}
					}
					children = append(children, child)
				}
			}
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"trace": trace,
			"spans": spans,
			"children": children,
			"usage": map[string]interface{}{
				"input_tokens":  inputTokens,
				"output_tokens": outputTokens,
				"total_tokens":  inputTokens + outputTokens,
				"llm_calls":     llmCalls,
				"tool_calls":    toolCalls,
			},
		})
	}
}

// handleTraceUsage - GET /traces/:id/usage - Get recursive usage rollup
func handleTraceUsage(db *sql.DB, w http.ResponseWriter, r *http.Request, traceID string) {
	if traceID == "" {
		http.Error(w, "Invalid trace ID", http.StatusBadRequest)
		return
	}

	// Get direct usage
	var directIn, directOut, directLLM, directTool int
	db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN kind = 'llm' THEN token_usage_input ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN kind = 'llm' THEN token_usage_output ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN kind = 'llm' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN kind = 'tool' THEN 1 ELSE 0 END), 0)
		FROM spans WHERE trace_id = ?
	`, traceID).Scan(&directIn, &directOut, &directLLM, &directTool)

	// Get children usage recursively using CTE
	var childIn, childOut, childLLM, childTool int
	db.QueryRow(`
		WITH RECURSIVE trace_tree AS (
			SELECT id FROM traces WHERE parent_trace_id = ?
			UNION ALL
			SELECT t.id FROM traces t JOIN trace_tree tt ON t.parent_trace_id = tt.id
		)
		SELECT
			COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN s.token_usage_input ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN s.token_usage_output ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN s.kind = 'tool' THEN 1 ELSE 0 END), 0)
		FROM spans s
		WHERE s.trace_id IN (SELECT id FROM trace_tree)
	`, traceID).Scan(&childIn, &childOut, &childLLM, &childTool)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"trace_id": traceID,
		"direct": map[string]interface{}{
			"input_tokens":  directIn,
			"output_tokens": directOut,
			"total_tokens":  directIn + directOut,
			"llm_calls":     directLLM,
			"tool_calls":    directTool,
		},
		"children": map[string]interface{}{
			"input_tokens":  childIn,
			"output_tokens": childOut,
			"total_tokens":  childIn + childOut,
			"llm_calls":     childLLM,
			"tool_calls":    childTool,
		},
		"total": map[string]interface{}{
			"input_tokens":  directIn + childIn,
			"output_tokens": directOut + childOut,
			"total_tokens":  directIn + directOut + childIn + childOut,
			"llm_calls":     directLLM + childLLM,
			"tool_calls":    directTool + childTool,
		},
	})
}

// HandleTraceTree - GET /traces/:id/tree - Get trace with spans as tree
func HandleTraceTree(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract trace ID from path
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) < 3 {
			http.Error(w, "Trace ID required", http.StatusBadRequest)
			return
		}
		traceID := pathParts[2]

		trace, err := events.GetTrace(db, traceID)
		if err != nil {
			http.Error(w, "Trace not found", http.StatusNotFound)
			return
		}

		spans, err := events.GetSpansForTrace(db, traceID)
		if err != nil {
			http.Error(w, "Failed to get spans", http.StatusInternalServerError)
			return
		}

		// Build tree structure
		spanMap := make(map[string]*events.Span)
		for _, span := range spans {
			spanMap[span.ID] = span
		}

		// Find root span and build hierarchy
		var rootSpan *events.Span
		for _, span := range spans {
			if span.ParentSpanID == "" {
				rootSpan = span
			} else if parent, ok := spanMap[span.ParentSpanID]; ok {
				if parent.ChildSpans == nil {
					parent.ChildSpans = make([]*events.Span, 0)
				}
				parent.ChildSpans = append(parent.ChildSpans, span)
			}
		}

		// Get events for each span
		for _, span := range spans {
			eventRows, err := db.Query(`
			SELECT id, timestamp, category, type, level, data, metadata, error
			FROM events
			WHERE span_id = ?
			ORDER BY timestamp`, span.ID)

			if err == nil {
				span.Events = make([]*events.Event, 0)
				for eventRows.Next() {
					var event events.Event
					var dataJSON, metadataJSON, errorStr sql.NullString

					err := eventRows.Scan(&event.ID, &event.Timestamp, &event.Category, &event.Type, &event.Level, &dataJSON, &metadataJSON, &errorStr)
					if err == nil {
						if dataJSON.Valid {
							json.Unmarshal([]byte(dataJSON.String), &event.Data)
						}
						if metadataJSON.Valid {
							json.Unmarshal([]byte(metadataJSON.String), &event.Metadata)
						}
						if errorStr.Valid {
							event.Error = errorStr.String
						}
						span.Events = append(span.Events, &event)
					}
				}
				eventRows.Close()
			}
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"trace":     trace,
			"root_span": rootSpan,
		})
	}
}

// HandleTracesForThread - GET /traces/thread/:thread_id - Get all traces for a thread
func HandleTracesForThread(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract thread ID from path
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) < 4 {
			http.Error(w, "Thread ID required", http.StatusBadRequest)
			return
		}
		threadID := pathParts[3]

		rows, err := db.Query(`
		SELECT id, name, service_name, start_time, end_time, duration_ms, status, span_count, error_count
		FROM traces
		WHERE thread_id = ?
		ORDER BY start_time DESC`, threadID)

		if err != nil {
			log.Printf("Error querying traces for thread: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		traces := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, name, serviceName, status string
			var startTime time.Time
			var (
				endTime                      sql.NullString
				durationMS, spanCount, errorCount sql.NullInt64
			)

			err := rows.Scan(&id, &name, &serviceName, &startTime, &endTime, &durationMS, &status, &spanCount, &errorCount)
			if err != nil {
				continue
			}

			trace := map[string]interface{}{
				"id":           id,
				"name":         name,
				"service_name": serviceName,
				"start_time":   startTime,
				"status":       status,
			}

			if endTime.Valid {
				t, _ := time.Parse(time.RFC3339, endTime.String)
				trace["end_time"] = t
			}
			if durationMS.Valid {
				trace["duration_ms"] = durationMS.Int64
			}
			if spanCount.Valid {
				trace["span_count"] = spanCount.Int64
			}
			if errorCount.Valid {
				trace["error_count"] = errorCount.Int64
			}

			traces = append(traces, trace)
		}

		sendJSON(w, http.StatusOK, map[string]interface{}{
			"thread_id": threadID,
			"traces":    traces,
			"total":     len(traces),
		})
	}
}

// HandleTraceStats - GET /traces/stats - Aggregated stats
func HandleTraceStats(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get time range from query params
		query := r.URL.Query()
		since := query.Get("since")
		if since == "" {
			since = "24h"
		}

		// Parse duration
		duration, err := time.ParseDuration(since)
		if err != nil {
			duration = 24 * time.Hour
		}
		startTime := time.Now().Add(-duration)

		// Query stats
		var totalTraces, okTraces, errorTraces int
		var avgDuration, p50, p95, p99 sql.NullFloat64

		db.QueryRow(`
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN status = 'ok' THEN 1 ELSE 0 END) as ok_count,
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) as error_count,
			AVG(duration_ms) as avg_duration
		FROM traces
		WHERE start_time >= ?`, startTime).Scan(&totalTraces, &okTraces, &errorTraces, &avgDuration)

		// Calculate percentiles (simplified - would need proper percentile calculation for production)
		db.QueryRow(`SELECT duration_ms FROM traces WHERE start_time >= ? AND duration_ms IS NOT NULL ORDER BY duration_ms LIMIT 1 OFFSET ?`,
			startTime, int(float64(totalTraces)*0.50)).Scan(&p50)
		db.QueryRow(`SELECT duration_ms FROM traces WHERE start_time >= ? AND duration_ms IS NOT NULL ORDER BY duration_ms LIMIT 1 OFFSET ?`,
			startTime, int(float64(totalTraces)*0.95)).Scan(&p95)
		db.QueryRow(`SELECT duration_ms FROM traces WHERE start_time >= ? AND duration_ms IS NOT NULL ORDER BY duration_ms LIMIT 1 OFFSET ?`,
			startTime, int(float64(totalTraces)*0.99)).Scan(&p99)

		stats := map[string]interface{}{
			"period":       since,
			"total_traces": totalTraces,
			"ok_traces":    okTraces,
			"error_traces": errorTraces,
		}

		if avgDuration.Valid {
			stats["avg_duration_ms"] = avgDuration.Float64
		}
		if p50.Valid {
			stats["p50_duration_ms"] = p50.Float64
		}
		if p95.Valid {
			stats["p95_duration_ms"] = p95.Float64
		}
		if p99.Valid {
			stats["p99_duration_ms"] = p99.Float64
		}

		sendJSON(w, http.StatusOK, stats)
	}
}

// handleTracesGroupedByThread aggregates traces by thread_id
// By default, only shows top-level threads (where traces have no parent in a different thread)
func handleTracesGroupedByThread(db *sql.DB, w http.ResponseWriter, query map[string][]string, limit, offset int, includeTrajectory bool) {
	getParam := func(key string) string {
		if vals, ok := query[key]; ok && len(vals) > 0 {
			return vals[0]
		}
		return ""
	}

	status := getParam("status")
	source := getParam("source")
	startTime := getParam("start_time")
	endTime := getParam("end_time")
	showAll := getParam("show_all") == "true" // If true, show all threads including children

	// Build SQL query that aggregates by thread_id
	// Filter to top-level threads: threads where the first trace has no parent_trace_id
	// OR the parent trace is in the same thread (not cross-thread parent)
	sqlQuery := `
		SELECT
			t.thread_id,
			COUNT(DISTINCT t.id) as trace_count,
			MIN(t.start_time) as first_trace_at,
			MAX(t.start_time) as last_trace_at,
			SUM(t.duration_ms) as total_duration_ms,
			SUM(t.span_count) as total_spans,
			SUM(t.error_count) as total_errors,
			COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN s.token_usage_input ELSE 0 END), 0) as total_input_tokens,
			COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN s.token_usage_output ELSE 0 END), 0) as total_output_tokens,
			COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN 1 ELSE 0 END), 0) as total_llm_calls,
			COALESCE(SUM(CASE WHEN s.kind = 'tool' THEN 1 ELSE 0 END), 0) as total_tool_calls,
			(SELECT source FROM traces t2 WHERE t2.thread_id = t.thread_id ORDER BY t2.start_time ASC LIMIT 1) as source
		FROM traces t
		LEFT JOIN spans s ON s.trace_id = t.id
		WHERE t.thread_id IS NOT NULL AND t.thread_id != ''
	`
	args := []interface{}{}

	// Filter to top-level threads only (unless show_all=true)
	if !showAll {
		// A thread is top-level if its first trace has no parent_trace_id pointing to a different thread
		sqlQuery += ` AND NOT EXISTS (
			SELECT 1 FROM traces first_trace
			JOIN traces parent_trace ON first_trace.parent_trace_id = parent_trace.id
			WHERE first_trace.thread_id = t.thread_id
			AND first_trace.start_time = (SELECT MIN(start_time) FROM traces WHERE thread_id = t.thread_id)
			AND parent_trace.thread_id != t.thread_id
		)`
	}

	if status != "" {
		sqlQuery += " AND t.status = ?"
		args = append(args, status)
	}
	if source != "" {
		sqlQuery += " AND t.source = ?"
		args = append(args, source)
	}
	if startTime != "" {
		sqlQuery += " AND t.start_time >= ?"
		args = append(args, startTime)
	}
	if endTime != "" {
		sqlQuery += " AND t.end_time <= ?"
		args = append(args, endTime)
	}

	sqlQuery += " GROUP BY t.thread_id ORDER BY last_trace_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		log.Printf("Error querying traces grouped by thread: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	threads := make([]map[string]interface{}, 0)
	for rows.Next() {
		var threadID string
		var traceCount int
		var firstTraceAtStr, lastTraceAtStr string
		var totalDurationMS, totalSpans, totalErrors sql.NullInt64
		var totalInputTokens, totalOutputTokens, totalLLMCalls, totalToolCalls int
		var sourceVal sql.NullString

		err := rows.Scan(&threadID, &traceCount, &firstTraceAtStr, &lastTraceAtStr,
			&totalDurationMS, &totalSpans, &totalErrors,
			&totalInputTokens, &totalOutputTokens, &totalLLMCalls, &totalToolCalls, &sourceVal)
		if err != nil {
			log.Printf("Scan error: %v", err)
			continue
		}

		// Parse time strings
		firstTraceAt := parseTimeString(firstTraceAtStr)
		lastTraceAt := parseTimeString(lastTraceAtStr)

		// Get source, default to chat
		source := "chat"
		if sourceVal.Valid && sourceVal.String != "" {
			source = sourceVal.String
		}

		thread := map[string]interface{}{
			"thread_id":      threadID,
			"trace_count":    traceCount,
			"first_trace_at": firstTraceAt,
			"last_trace_at":  lastTraceAt,
			"source":         source,
			"usage": map[string]interface{}{
				"input_tokens":  totalInputTokens,
				"output_tokens": totalOutputTokens,
				"total_tokens":  totalInputTokens + totalOutputTokens,
				"llm_calls":     totalLLMCalls,
				"tool_calls":    totalToolCalls,
			},
		}

		if totalDurationMS.Valid {
			thread["total_duration_ms"] = totalDurationMS.Int64
		}
		if totalSpans.Valid {
			thread["total_spans"] = totalSpans.Int64
		}
		if totalErrors.Valid {
			thread["total_errors"] = totalErrors.Int64
		}

		threads = append(threads, thread)
	}

	// Aggregate child thread usage and add to each top-level thread
	for i, thread := range threads {
		threadID := thread["thread_id"].(string)
		childUsage := getChildThreadsUsage(db, threadID)
		if childUsage != nil {
			// Add child usage to the thread's usage
			currentUsage := thread["usage"].(map[string]interface{})
			threads[i]["usage"] = map[string]interface{}{
				"input_tokens":  currentUsage["input_tokens"].(int) + childUsage["input_tokens"].(int),
				"output_tokens": currentUsage["output_tokens"].(int) + childUsage["output_tokens"].(int),
				"total_tokens":  currentUsage["total_tokens"].(int) + childUsage["total_tokens"].(int),
				"llm_calls":     currentUsage["llm_calls"].(int) + childUsage["llm_calls"].(int),
				"tool_calls":    currentUsage["tool_calls"].(int) + childUsage["tool_calls"].(int),
			}
			// Also store child thread IDs for reference
			if childThreadIDs, ok := childUsage["child_thread_ids"].([]string); ok && len(childThreadIDs) > 0 {
				threads[i]["child_thread_ids"] = childThreadIDs
			}
		}
	}

	// Optionally fetch trajectory for each thread (including child threads)
	if includeTrajectory {
		for i, thread := range threads {
			threadID := thread["thread_id"].(string)
			trajectory := getTrajectoryForThreadWithChildren(db, threadID)
			threads[i]["trajectory"] = trajectory
		}
	}

	// Get total count of distinct threads (applying same filters)
	countQuery := `SELECT COUNT(DISTINCT t.thread_id) FROM traces t WHERE t.thread_id IS NOT NULL AND t.thread_id != ''`
	countArgs := []interface{}{}

	// Apply top-level filter to count query too
	if !showAll {
		countQuery += ` AND NOT EXISTS (
			SELECT 1 FROM traces first_trace
			JOIN traces parent_trace ON first_trace.parent_trace_id = parent_trace.id
			WHERE first_trace.thread_id = t.thread_id
			AND first_trace.start_time = (SELECT MIN(start_time) FROM traces WHERE thread_id = t.thread_id)
			AND parent_trace.thread_id != t.thread_id
		)`
	}

	if status != "" {
		countQuery += " AND t.status = ?"
		countArgs = append(countArgs, status)
	}
	if source != "" {
		countQuery += " AND t.source = ?"
		countArgs = append(countArgs, source)
	}

	var total int
	db.QueryRow(countQuery, countArgs...).Scan(&total)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"threads":  threads,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"group_by": "thread",
	})
}

// getTrajectoryForTrace returns the trajectory (messages) for a single trace
// Uses thread_id from trace to get messages
func getTrajectoryForTrace(db *sql.DB, traceID string) []map[string]interface{} {
	// First get the thread_id for this trace
	var threadID sql.NullString
	err := db.QueryRow("SELECT thread_id FROM traces WHERE id = ?", traceID).Scan(&threadID)
	if err != nil || !threadID.Valid || threadID.String == "" {
		return []map[string]interface{}{}
	}

	return getMessagesForThread(db, threadID.String)
}

// getChildThreadsUsage returns aggregated usage from child threads
func getChildThreadsUsage(db *sql.DB, parentThreadID string) map[string]interface{} {
	// Find child thread IDs
	childRows, err := db.Query(`
		SELECT DISTINCT child_trace.thread_id
		FROM traces child_trace
		JOIN traces parent_trace ON child_trace.parent_trace_id = parent_trace.id
		WHERE parent_trace.thread_id = ?
		AND child_trace.thread_id IS NOT NULL
		AND child_trace.thread_id != ?
		AND child_trace.thread_id != ''
	`, parentThreadID, parentThreadID)
	if err != nil {
		return nil
	}
	defer childRows.Close()

	var childThreadIDs []string
	for childRows.Next() {
		var childThreadID string
		if err := childRows.Scan(&childThreadID); err == nil {
			childThreadIDs = append(childThreadIDs, childThreadID)
		}
	}

	if len(childThreadIDs) == 0 {
		return nil
	}

	// Build query to get usage from child threads
	placeholders := make([]string, len(childThreadIDs))
	args := make([]interface{}, len(childThreadIDs))
	for i, tid := range childThreadIDs {
		placeholders[i] = "?"
		args[i] = tid
	}

	var inputTokens, outputTokens, llmCalls, toolCalls int
	err = db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN s.token_usage_input ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN s.token_usage_output ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN s.kind = 'llm' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN s.kind = 'tool' THEN 1 ELSE 0 END), 0)
		FROM traces t
		JOIN spans s ON s.trace_id = t.id
		WHERE t.thread_id IN (`+strings.Join(placeholders, ",")+`)
	`, args...).Scan(&inputTokens, &outputTokens, &llmCalls, &toolCalls)
	if err != nil {
		return nil
	}

	return map[string]interface{}{
		"input_tokens":     inputTokens,
		"output_tokens":    outputTokens,
		"total_tokens":     inputTokens + outputTokens,
		"llm_calls":        llmCalls,
		"tool_calls":       toolCalls,
		"child_thread_ids": childThreadIDs,
	}
}

// getTrajectoryForThread returns the trajectory (messages) for a thread
func getTrajectoryForThread(db *sql.DB, threadID string) []map[string]interface{} {
	return getMessagesForThread(db, threadID)
}

// getTrajectoryForThreadWithChildren returns trajectory including messages from child threads
// Child threads are identified by traces whose parent_trace_id points to a trace in this thread
func getTrajectoryForThreadWithChildren(db *sql.DB, threadID string) []map[string]interface{} {
	// Get all thread IDs: this thread + any child threads
	threadIDs := []string{threadID}

	// Find child threads: threads where a trace has parent_trace_id pointing to a trace in our thread
	childRows, err := db.Query(`
		SELECT DISTINCT child_trace.thread_id
		FROM traces child_trace
		JOIN traces parent_trace ON child_trace.parent_trace_id = parent_trace.id
		WHERE parent_trace.thread_id = ?
		AND child_trace.thread_id IS NOT NULL
		AND child_trace.thread_id != ?
		AND child_trace.thread_id != ''
	`, threadID, threadID)
	if err == nil {
		defer childRows.Close()
		for childRows.Next() {
			var childThreadID string
			if err := childRows.Scan(&childThreadID); err == nil {
				threadIDs = append(threadIDs, childThreadID)
			}
		}
	}

	// Fetch messages from all threads, ordered by timestamp
	if len(threadIDs) == 1 {
		return getMessagesForThread(db, threadID)
	}

	// Build query for multiple threads
	placeholders := make([]string, len(threadIDs))
	args := make([]interface{}, len(threadIDs))
	for i, tid := range threadIDs {
		placeholders[i] = "?"
		args[i] = tid
	}

	query := `
		SELECT m.id, m.role, m.content, m.model, m.created_at, m.metadata, m.thread_id
		FROM messages m
		WHERE m.thread_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY m.created_at ASC
	`

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Printf("Error fetching messages for threads %v: %v", threadIDs, err)
		return []map[string]interface{}{}
	}
	defer rows.Close()

	messages := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, role, content string
		var model sql.NullString
		var createdAtStr string
		var metadataStr sql.NullString
		var msgThreadID string

		err := rows.Scan(&id, &role, &content, &model, &createdAtStr, &metadataStr, &msgThreadID)
		if err != nil {
			log.Printf("Message scan error: %v", err)
			continue
		}

		msg := map[string]interface{}{
			"id":   id,
			"role": role,
		}

		// Mark if this is from a child thread
		if msgThreadID != threadID {
			msg["child_thread_id"] = msgThreadID
		}

		// Parse timestamp
		if t := parseTimeString(createdAtStr); !t.IsZero() {
			msg["timestamp"] = t
		}

		if model.Valid && model.String != "" {
			msg["model"] = model.String
		}

		msg["content"] = content

		// Parse metadata for tool calls info
		if metadataStr.Valid && metadataStr.String != "" {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(metadataStr.String), &metadata); err == nil {
				if toolCalls, ok := metadata["tool_calls"]; ok {
					msg["tool_calls"] = toolCalls
				}
				if tokens, ok := metadata["tokens"]; ok {
					msg["tokens"] = tokens
				}
				if inputTokens, ok := metadata["input_tokens"]; ok {
					msg["input_tokens"] = inputTokens
				}
				if outputTokens, ok := metadata["output_tokens"]; ok {
					msg["output_tokens"] = outputTokens
				}
			}
		}

		messages = append(messages, msg)
	}

	return messages
}

// getMessagesForThread fetches messages for a thread as trajectory
func getMessagesForThread(db *sql.DB, threadID string) []map[string]interface{} {
	rows, err := db.Query(`
		SELECT id, role, content, model, created_at, metadata
		FROM messages
		WHERE thread_id = ?
		ORDER BY created_at ASC
	`, threadID)
	if err != nil {
		log.Printf("Error fetching messages for thread %s: %v", threadID, err)
		return []map[string]interface{}{}
	}
	defer rows.Close()

	messages := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, role, content string
		var model sql.NullString
		var createdAtStr string
		var metadataStr sql.NullString

		err := rows.Scan(&id, &role, &content, &model, &createdAtStr, &metadataStr)
		if err != nil {
			log.Printf("Message scan error: %v", err)
			continue
		}

		msg := map[string]interface{}{
			"id":   id,
			"role": role,
		}

		// Parse timestamp
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			msg["timestamp"] = t
		}

		if model.Valid && model.String != "" {
			msg["model"] = model.String
		}

		msg["content"] = content

		// Parse metadata for tool calls info
		if metadataStr.Valid && metadataStr.String != "" {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(metadataStr.String), &metadata); err == nil {
				// Extract useful metadata fields
				if toolCalls, ok := metadata["tool_calls"]; ok {
					msg["tool_calls"] = toolCalls
				}
				if tokens, ok := metadata["tokens"]; ok {
					msg["tokens"] = tokens
				}
				if inputTokens, ok := metadata["input_tokens"]; ok {
					msg["input_tokens"] = inputTokens
				}
				if outputTokens, ok := metadata["output_tokens"]; ok {
					msg["output_tokens"] = outputTokens
				}
			}
		}

		messages = append(messages, msg)
	}

	return messages
}

// parseTimeString tries multiple time formats to parse a timestamp string
func parseTimeString(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	// Try common formats
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}

	log.Printf("Failed to parse time string: %s", s)
	return time.Time{}
}
