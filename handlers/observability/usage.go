package observability

import (
	"database/sql"
	"log"
	"net/http"
	"time"
)

// UsageSummary represents token usage statistics
type UsageSummary struct {
	TotalInputTokens         int64  `json:"total_input_tokens"`
	TotalOutputTokens        int64  `json:"total_output_tokens"`
	TotalTokens              int64  `json:"total_tokens"`
	TotalCacheCreationTokens int64  `json:"total_cache_creation_tokens,omitempty"`
	TotalCacheReadTokens     int64  `json:"total_cache_read_tokens,omitempty"`
	TotalReasoningTokens     int64  `json:"total_reasoning_tokens,omitempty"`
	CallCount                int64  `json:"call_count"`
	StartTime                string `json:"start_time,omitempty"`
	EndTime                  string `json:"end_time,omitempty"`
}

// UsageByThread represents token usage grouped by thread
type UsageByThread struct {
	ThreadID             string `json:"thread_id"`
	ThreadTitle          string `json:"thread_title,omitempty"`
	InputTokens          int64  `json:"input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	TotalTokens          int64  `json:"total_tokens"`
	CacheCreationTokens  int64  `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens      int64  `json:"cache_read_tokens,omitempty"`
	ReasoningTokens      int64  `json:"reasoning_tokens,omitempty"`
	CallCount            int64  `json:"call_count"`
}

// UsageByTask represents token usage grouped by task
type UsageByTask struct {
	TaskID               string `json:"task_id"`
	InputTokens          int64  `json:"input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	TotalTokens          int64  `json:"total_tokens"`
	CacheCreationTokens  int64  `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens      int64  `json:"cache_read_tokens,omitempty"`
	ReasoningTokens      int64  `json:"reasoning_tokens,omitempty"`
	CallCount            int64  `json:"call_count"`
}

// UsageByDay represents token usage grouped by day
type UsageByDay struct {
	Date                 string `json:"date"`
	InputTokens          int64  `json:"input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	TotalTokens          int64  `json:"total_tokens"`
	CacheCreationTokens  int64  `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens      int64  `json:"cache_read_tokens,omitempty"`
	ReasoningTokens      int64  `json:"reasoning_tokens,omitempty"`
	CallCount            int64  `json:"call_count"`
}

// HandleUsage - GET /usage - Get token usage statistics
// Query parameters:
//   - start_time: RFC3339 timestamp for start of range (optional)
//   - end_time: RFC3339 timestamp for end of range (optional)
//   - thread_id: Filter by specific thread (optional)
//   - task_id: Filter by specific task (optional)
//   - group_by: "thread", "task", or "day" for grouped results (optional)
func HandleUsage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()
		startTimeStr := query.Get("start_time")
		endTimeStr := query.Get("end_time")
		threadID := query.Get("thread_id")
		taskID := query.Get("task_id")
		groupBy := query.Get("group_by")

		// Parse time parameters
		var startTime, endTime *time.Time
		if startTimeStr != "" {
			t, err := time.Parse(time.RFC3339, startTimeStr)
			if err != nil {
				http.Error(w, "Invalid start_time format (use RFC3339)", http.StatusBadRequest)
				return
			}
			startTime = &t
		}
		if endTimeStr != "" {
			t, err := time.Parse(time.RFC3339, endTimeStr)
			if err != nil {
				http.Error(w, "Invalid end_time format (use RFC3339)", http.StatusBadRequest)
				return
			}
			endTime = &t
		}

		switch groupBy {
		case "thread":
			handleUsageByThread(w, db, startTime, endTime, threadID, taskID)
		case "task":
			handleUsageByTask(w, db, startTime, endTime, threadID, taskID)
		case "day":
			handleUsageByDay(w, db, startTime, endTime, threadID, taskID)
		default:
			handleUsageSummary(w, db, startTime, endTime, threadID, taskID)
		}
	}
}

func handleUsageSummary(w http.ResponseWriter, db *sql.DB, startTime, endTime *time.Time, threadID, taskID string) {
	// Build SQL query for summary
	sqlQuery := `
		SELECT
			COALESCE(SUM(token_usage_input), 0) as input_tokens,
			COALESCE(SUM(token_usage_output), 0) as output_tokens,
			COALESCE(SUM(token_usage_input + token_usage_output), 0) as total_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(SUM(reasoning_tokens), 0) as reasoning_tokens,
			COUNT(*) as call_count
		FROM spans
		WHERE kind = 'llm'
			AND token_usage_input IS NOT NULL
	`
	args := []interface{}{}

	if startTime != nil {
		sqlQuery += " AND start_time >= ?"
		args = append(args, startTime.Format(time.RFC3339))
	}
	if endTime != nil {
		sqlQuery += " AND start_time <= ?"
		args = append(args, endTime.Format(time.RFC3339))
	}
	if threadID != "" {
		sqlQuery += " AND thread_id = ?"
		args = append(args, threadID)
	}
	if taskID != "" {
		sqlQuery += " AND task_id = ?"
		args = append(args, taskID)
	}

	var summary UsageSummary
	err := db.QueryRow(sqlQuery, args...).Scan(
		&summary.TotalInputTokens,
		&summary.TotalOutputTokens,
		&summary.TotalTokens,
		&summary.TotalCacheCreationTokens,
		&summary.TotalCacheReadTokens,
		&summary.TotalReasoningTokens,
		&summary.CallCount,
	)
	if err != nil {
		log.Printf("Error querying usage summary: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if startTime != nil {
		summary.StartTime = startTime.Format(time.RFC3339)
	}
	if endTime != nil {
		summary.EndTime = endTime.Format(time.RFC3339)
	}

	response := map[string]interface{}{
		"usage": summary,
	}
	if threadID != "" {
		response["thread_id"] = threadID
	}
	if taskID != "" {
		response["task_id"] = taskID
	}

	sendJSON(w, http.StatusOK, response)
}

func handleUsageByThread(w http.ResponseWriter, db *sql.DB, startTime, endTime *time.Time, threadID, taskID string) {
	// Build SQL query grouped by thread
	sqlQuery := `
		SELECT
			s.thread_id,
			COALESCE(t.title, '') as thread_title,
			COALESCE(SUM(s.token_usage_input), 0) as input_tokens,
			COALESCE(SUM(s.token_usage_output), 0) as output_tokens,
			COALESCE(SUM(s.token_usage_input + s.token_usage_output), 0) as total_tokens,
			COALESCE(SUM(s.cache_creation_tokens), 0) as cache_creation_tokens,
			COALESCE(SUM(s.cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(SUM(s.reasoning_tokens), 0) as reasoning_tokens,
			COUNT(*) as call_count
		FROM spans s
		LEFT JOIN threads t ON s.thread_id = t.id
		WHERE s.kind = 'llm'
			AND s.token_usage_input IS NOT NULL
			AND s.thread_id IS NOT NULL
	`
	args := []interface{}{}

	if startTime != nil {
		sqlQuery += " AND s.start_time >= ?"
		args = append(args, startTime.Format(time.RFC3339))
	}
	if endTime != nil {
		sqlQuery += " AND s.start_time <= ?"
		args = append(args, endTime.Format(time.RFC3339))
	}
	if threadID != "" {
		sqlQuery += " AND s.thread_id = ?"
		args = append(args, threadID)
	}
	if taskID != "" {
		sqlQuery += " AND s.task_id = ?"
		args = append(args, taskID)
	}

	sqlQuery += " GROUP BY s.thread_id ORDER BY total_tokens DESC"

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		log.Printf("Error querying usage by thread: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := make([]UsageByThread, 0)
	var totalInput, totalOutput, totalTokens int64
	var totalCalls int64

	for rows.Next() {
		var usage UsageByThread
		err := rows.Scan(
			&usage.ThreadID,
			&usage.ThreadTitle,
			&usage.InputTokens,
			&usage.OutputTokens,
			&usage.TotalTokens,
			&usage.CacheCreationTokens,
			&usage.CacheReadTokens,
			&usage.ReasoningTokens,
			&usage.CallCount,
		)
		if err != nil {
			continue
		}
		results = append(results, usage)
		totalInput += usage.InputTokens
		totalOutput += usage.OutputTokens
		totalTokens += usage.TotalTokens
		totalCalls += usage.CallCount
	}

	response := map[string]interface{}{
		"usage_by_thread": results,
		"summary": UsageSummary{
			TotalInputTokens:  totalInput,
			TotalOutputTokens: totalOutput,
			TotalTokens:       totalTokens,
			CallCount:         totalCalls,
		},
	}

	if startTime != nil {
		response["start_time"] = startTime.Format(time.RFC3339)
	}
	if endTime != nil {
		response["end_time"] = endTime.Format(time.RFC3339)
	}
	if taskID != "" {
		response["task_id"] = taskID
	}

	sendJSON(w, http.StatusOK, response)
}

func handleUsageByTask(w http.ResponseWriter, db *sql.DB, startTime, endTime *time.Time, threadID, taskID string) {
	// Build SQL query grouped by task
	sqlQuery := `
		SELECT
			task_id,
			COALESCE(SUM(token_usage_input), 0) as input_tokens,
			COALESCE(SUM(token_usage_output), 0) as output_tokens,
			COALESCE(SUM(token_usage_input + token_usage_output), 0) as total_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(SUM(reasoning_tokens), 0) as reasoning_tokens,
			COUNT(*) as call_count
		FROM spans
		WHERE kind = 'llm'
			AND token_usage_input IS NOT NULL
			AND task_id IS NOT NULL
			AND task_id != ''
	`
	args := []interface{}{}

	if startTime != nil {
		sqlQuery += " AND start_time >= ?"
		args = append(args, startTime.Format(time.RFC3339))
	}
	if endTime != nil {
		sqlQuery += " AND start_time <= ?"
		args = append(args, endTime.Format(time.RFC3339))
	}
	if threadID != "" {
		sqlQuery += " AND thread_id = ?"
		args = append(args, threadID)
	}
	if taskID != "" {
		sqlQuery += " AND task_id = ?"
		args = append(args, taskID)
	}

	sqlQuery += " GROUP BY task_id ORDER BY total_tokens DESC"

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		log.Printf("Error querying usage by task: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := make([]UsageByTask, 0)
	var totalInput, totalOutput, totalTokens int64
	var totalCalls int64

	for rows.Next() {
		var usage UsageByTask
		err := rows.Scan(
			&usage.TaskID,
			&usage.InputTokens,
			&usage.OutputTokens,
			&usage.TotalTokens,
			&usage.CacheCreationTokens,
			&usage.CacheReadTokens,
			&usage.ReasoningTokens,
			&usage.CallCount,
		)
		if err != nil {
			continue
		}
		results = append(results, usage)
		totalInput += usage.InputTokens
		totalOutput += usage.OutputTokens
		totalTokens += usage.TotalTokens
		totalCalls += usage.CallCount
	}

	response := map[string]interface{}{
		"usage_by_task": results,
		"summary": UsageSummary{
			TotalInputTokens:  totalInput,
			TotalOutputTokens: totalOutput,
			TotalTokens:       totalTokens,
			CallCount:         totalCalls,
		},
	}

	if startTime != nil {
		response["start_time"] = startTime.Format(time.RFC3339)
	}
	if endTime != nil {
		response["end_time"] = endTime.Format(time.RFC3339)
	}
	if threadID != "" {
		response["thread_id"] = threadID
	}

	sendJSON(w, http.StatusOK, response)
}

func handleUsageByDay(w http.ResponseWriter, db *sql.DB, startTime, endTime *time.Time, threadID, taskID string) {
	// Build SQL query grouped by day
	sqlQuery := `
		SELECT
			DATE(start_time) as date,
			COALESCE(SUM(token_usage_input), 0) as input_tokens,
			COALESCE(SUM(token_usage_output), 0) as output_tokens,
			COALESCE(SUM(token_usage_input + token_usage_output), 0) as total_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
			COALESCE(SUM(reasoning_tokens), 0) as reasoning_tokens,
			COUNT(*) as call_count
		FROM spans
		WHERE kind = 'llm'
			AND token_usage_input IS NOT NULL
	`
	args := []interface{}{}

	if startTime != nil {
		sqlQuery += " AND start_time >= ?"
		args = append(args, startTime.Format(time.RFC3339))
	}
	if endTime != nil {
		sqlQuery += " AND start_time <= ?"
		args = append(args, endTime.Format(time.RFC3339))
	}
	if threadID != "" {
		sqlQuery += " AND thread_id = ?"
		args = append(args, threadID)
	}
	if taskID != "" {
		sqlQuery += " AND task_id = ?"
		args = append(args, taskID)
	}

	sqlQuery += " GROUP BY DATE(start_time) ORDER BY date DESC"

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		log.Printf("Error querying usage by day: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	results := make([]UsageByDay, 0)
	var totalInput, totalOutput, totalTokens int64
	var totalCalls int64

	for rows.Next() {
		var usage UsageByDay
		err := rows.Scan(
			&usage.Date,
			&usage.InputTokens,
			&usage.OutputTokens,
			&usage.TotalTokens,
			&usage.CacheCreationTokens,
			&usage.CacheReadTokens,
			&usage.ReasoningTokens,
			&usage.CallCount,
		)
		if err != nil {
			continue
		}
		results = append(results, usage)
		totalInput += usage.InputTokens
		totalOutput += usage.OutputTokens
		totalTokens += usage.TotalTokens
		totalCalls += usage.CallCount
	}

	response := map[string]interface{}{
		"usage_by_day": results,
		"summary": UsageSummary{
			TotalInputTokens:  totalInput,
			TotalOutputTokens: totalOutput,
			TotalTokens:       totalTokens,
			CallCount:         totalCalls,
		},
	}

	if startTime != nil {
		response["start_time"] = startTime.Format(time.RFC3339)
	}
	if endTime != nil {
		response["end_time"] = endTime.Format(time.RFC3339)
	}
	if threadID != "" {
		response["thread_id"] = threadID
	}
	if taskID != "" {
		response["task_id"] = taskID
	}

	sendJSON(w, http.StatusOK, response)
}
