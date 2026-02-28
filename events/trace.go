package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Context keys for trace propagation
type traceContextKey struct{}
type spanContextKey struct{}

// WithTrace returns a new context with the trace attached
func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceContextKey{}, trace)
}

// TraceFromContext extracts the trace from context, or nil if not present
func TraceFromContext(ctx context.Context) *Trace {
	if ctx == nil {
		return nil
	}
	trace, _ := ctx.Value(traceContextKey{}).(*Trace)
	return trace
}

// WithSpan returns a new context with the span attached
func WithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, spanContextKey{}, span)
}

// SpanFromContext extracts the span from context, or nil if not present
func SpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	span, _ := ctx.Value(spanContextKey{}).(*Span)
	return span
}

// WithTraceAndSpan is a convenience function to attach both trace and span to context
func WithTraceAndSpan(ctx context.Context, trace *Trace, span *Span) context.Context {
	ctx = WithTrace(ctx, trace)
	ctx = WithSpan(ctx, span)
	return ctx
}

// SpanKind represents the type of span
type SpanKind string

const (
	SpanKindInternal SpanKind = "internal"
	SpanKindServer   SpanKind = "server" // Incoming request
	SpanKindClient   SpanKind = "client" // Outgoing request
	SpanKindTool     SpanKind = "tool"   // Tool execution
	SpanKindLLM      SpanKind = "llm"    // LLM API call
	SpanKindAgent    SpanKind = "agent"  // Agent-to-agent call
)

// SpanStatus represents the completion status of a span
type SpanStatus string

const (
	SpanStatusOK      SpanStatus = "ok"
	SpanStatusError   SpanStatus = "error"
	SpanStatusRunning SpanStatus = "running"
)

// TraceStatus represents the completion status of a trace
type TraceStatus string

const (
	TraceStatusOK         TraceStatus = "ok"
	TraceStatusError      TraceStatus = "error"
	TraceStatusUnfinished TraceStatus = "unfinished"
)

// TraceSource represents how the trace was initiated
type TraceSource string

const (
	TraceSourceChat      TraceSource = "chat"       // Direct /chat request
	TraceSourceWebhook   TraceSource = "webhook"    // Webhook trigger
	TraceSourceTask      TraceSource = "task"       // Scheduled task
	TraceSourceSubtask   TraceSource = "subtask"    // Spawned subtask
	TraceSourceAgentCall TraceSource = "agent_call" // Agent-to-agent call
)

// Trace represents a complete request/operation flow
type Trace struct {
	ID            string                 `json:"id"`
	ParentTraceID string                 `json:"parent_trace_id,omitempty"`
	Source        TraceSource            `json:"source"`
	Name          string                 `json:"name"`
	ThreadID      string                 `json:"thread_id,omitempty"`
	ServiceName   string                 `json:"service_name"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       *time.Time             `json:"end_time,omitempty"`
	DurationMS    int64                  `json:"duration_ms,omitempty"`
	Status        TraceStatus            `json:"status"`
	RootSpanID    string                 `json:"root_span_id,omitempty"`
	SpanCount     int                    `json:"span_count"`
	ErrorCount    int                    `json:"error_count"`
	Metadata      map[string]interface{} `json:"metadata"`

	db     *sql.DB
	mu     sync.RWMutex
	active bool
}

// Span represents a unit of work within a trace
type Span struct {
	ID            string                 `json:"id"`
	TraceID       string                 `json:"trace_id"`
	ParentSpanID  string                 `json:"parent_span_id,omitempty"`
	Name          string                 `json:"name"`
	Kind          SpanKind               `json:"kind"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       *time.Time             `json:"end_time,omitempty"`
	DurationMS    int64                  `json:"duration_ms,omitempty"`
	Status        SpanStatus             `json:"status"`
	StatusMessage string                 `json:"status_message,omitempty"`
	Category      EventCategory          `json:"category,omitempty"`
	ThreadID      string                 `json:"thread_id,omitempty"`
	TaskID        string                 `json:"task_id,omitempty"`
	SessionID     string                 `json:"session_id,omitempty"`
	AgentID       string                 `json:"agent_id,omitempty"`
	Attributes    map[string]interface{} `json:"attributes"`
	InputData     string                 `json:"input_data,omitempty"`
	OutputData    string                 `json:"output_data,omitempty"`
	TokenUsageIn        int              `json:"token_usage_input,omitempty"`
	TokenUsageOut       int              `json:"token_usage_output,omitempty"`
	CacheCreationTokens int             `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int             `json:"cache_read_tokens,omitempty"`
	ReasoningTokens     int             `json:"reasoning_tokens,omitempty"`
	Provider            string          `json:"provider,omitempty"`
	Model               string          `json:"model,omitempty"`

	// Not persisted, used for building tree
	Events     []*Event `json:"events,omitempty"`
	ChildSpans []*Span  `json:"child_spans,omitempty"`

	trace      *Trace
	db         *sql.DB
	mu         sync.RWMutex
	active     bool
	startTimer time.Time
}

// NewTrace creates a new trace
func NewTrace(name, threadID string, db *sql.DB) *Trace {
	trace := &Trace{
		ID:          "trace_" + uuid.New().String()[:12],
		Name:        name,
		ThreadID:    threadID,
		Source:      TraceSourceChat,
		ServiceName: "agent-core",
		StartTime:   time.Now(),
		Status:      TraceStatusUnfinished,
		Metadata:    make(map[string]interface{}),
		db:          db,
		active:      true,
	}

	// Insert into database
	if db != nil {
		trace.persist()
	}

	return trace
}

// WithParent sets the parent trace ID for hierarchical tracing
func (t *Trace) WithParent(parentTraceID string) *Trace {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ParentTraceID = parentTraceID
	if t.db != nil {
		t.db.Exec("UPDATE traces SET parent_trace_id = ? WHERE id = ?", parentTraceID, t.ID)
	}
	return t
}

// WithSource sets the trace source
func (t *Trace) WithSource(source TraceSource) *Trace {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Source = source
	if t.db != nil {
		t.db.Exec("UPDATE traces SET source = ? WHERE id = ?", string(source), t.ID)
	}
	return t
}

// StartSpan creates a new root span for this trace
func (t *Trace) StartSpan(name string, kind SpanKind) *Span {
	t.mu.Lock()
	defer t.mu.Unlock()

	span := &Span{
		ID:         "span_" + uuid.New().String()[:12],
		TraceID:    t.ID,
		Name:       name,
		Kind:       kind,
		StartTime:  time.Now(),
		Status:     SpanStatusRunning,
		Attributes: make(map[string]interface{}),
		trace:      t,
		db:         t.db,
		active:     true,
		startTimer: time.Now(),
	}

	// Set as root span if first
	if t.RootSpanID == "" {
		t.RootSpanID = span.ID
	}

	t.SpanCount++

	// Insert into database
	if span.db != nil {
		span.persist()
		t.updateSpanCount()
	}

	return span
}

// StartChild creates a child span from this span
func (s *Span) StartChild(name string, kind SpanKind) *Span {
	s.mu.RLock()
	defer s.mu.RUnlock()

	child := &Span{
		ID:           "span_" + uuid.New().String()[:12],
		TraceID:      s.TraceID,
		ParentSpanID: s.ID,
		Name:         name,
		Kind:         kind,
		StartTime:    time.Now(),
		Status:       SpanStatusRunning,
		ThreadID:     s.ThreadID,
		TaskID:       s.TaskID,
		SessionID:    s.SessionID,
		AgentID:      s.AgentID,
		Attributes:   make(map[string]interface{}),
		trace:        s.trace,
		db:           s.db,
		active:       true,
		startTimer:   time.Now(),
	}

	// Update trace span count
	if s.trace != nil {
		s.trace.mu.Lock()
		s.trace.SpanCount++
		s.trace.mu.Unlock()
	}

	// Insert into database
	if child.db != nil {
		child.persist()
		if s.trace != nil {
			s.trace.updateSpanCount()
		}
	}

	return child
}

// Span builder methods
func (s *Span) WithThreadID(threadID string) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ThreadID = threadID
	return s
}

func (s *Span) WithSessionID(sessionID string) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SessionID = sessionID
	return s
}

func (s *Span) WithTaskID(taskID string) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TaskID = taskID
	return s
}

func (s *Span) WithAttribute(key string, value interface{}) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Attributes[key] = value
	return s
}

func (s *Span) WithCategory(category EventCategory) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Category = category
	return s
}

func (s *Span) WithTokenUsage(inputTokens, outputTokens int) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TokenUsageIn = inputTokens
	s.TokenUsageOut = outputTokens
	return s
}

// WithDetailedTokenUsage sets cache and reasoning token counts.
// Accepts a struct with the same field layout as stream.TokenUsageResult
// to avoid circular imports.
func (s *Span) WithDetailedTokenUsage(detail interface{ GetCacheCreation() int; GetCacheRead() int; GetReasoning() int }) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CacheCreationTokens = detail.GetCacheCreation()
	s.CacheReadTokens = detail.GetCacheRead()
	s.ReasoningTokens = detail.GetReasoning()
	return s
}

func (s *Span) WithProviderModel(provider, model string) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Provider = provider
	s.Model = model
	return s
}

func (s *Span) SetStatus(status SpanStatus) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
	return s
}

func (s *Span) RecordError(err error) *Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.Status = SpanStatusError
		s.StatusMessage = err.Error()

		// Update trace error count
		if s.trace != nil {
			s.trace.mu.Lock()
			s.trace.ErrorCount++
			s.trace.mu.Unlock()
		}
	}
	return s
}

// End completes the span and persists it
func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	now := time.Now()
	s.EndTime = &now
	s.DurationMS = time.Since(s.startTimer).Milliseconds()
	s.active = false

	// If status is still running, mark as ok
	if s.Status == SpanStatusRunning {
		s.Status = SpanStatusOK
	}

	// Update in database
	if s.db != nil {
		s.update()
	}
}

// UpdateThreadID updates the trace's thread ID
func (t *Trace) UpdateThreadID(threadID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ThreadID = threadID
}

// Finish completes the trace
func (t *Trace) Finish() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active {
		return
	}

	now := time.Now()
	t.EndTime = &now
	t.DurationMS = time.Since(t.StartTime).Milliseconds()
	t.active = false

	// Determine final status
	if t.ErrorCount > 0 {
		t.Status = TraceStatusError
	} else {
		t.Status = TraceStatusOK
	}

	// Update in database
	if t.db != nil {
		t.update()
	}
}

// Database persistence methods
func (t *Trace) persist() error {
	metadataJSON, _ := json.Marshal(t.Metadata)

	// Use nullstring for optional parent_trace_id
	var parentTraceID interface{}
	if t.ParentTraceID != "" {
		parentTraceID = t.ParentTraceID
	}

	_, err := t.db.Exec(`
		INSERT INTO traces (id, parent_trace_id, source, name, thread_id, service_name, start_time, status, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, parentTraceID, string(t.Source), t.Name, t.ThreadID, t.ServiceName, t.StartTime, t.Status, string(metadataJSON))

	return err
}

func (t *Trace) update() error {
	_, err := t.db.Exec(`
		UPDATE traces
		SET end_time = ?, duration_ms = ?, status = ?, root_span_id = ?,
		    span_count = ?, error_count = ?, thread_id = ?
		WHERE id = ?`,
		t.EndTime, t.DurationMS, t.Status, t.RootSpanID, t.SpanCount, t.ErrorCount, t.ThreadID, t.ID)

	return err
}

func (t *Trace) updateSpanCount() error {
	_, err := t.db.Exec(`UPDATE traces SET span_count = ? WHERE id = ?`, t.SpanCount, t.ID)
	return err
}

func (s *Span) persist() error {
	attributesJSON, _ := json.Marshal(s.Attributes)

	_, err := s.db.Exec(`
		INSERT INTO spans (
			id, trace_id, parent_span_id, name, kind, start_time, status,
			category, thread_id, task_id, session_id, agent_id, attributes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.TraceID, s.ParentSpanID, s.Name, s.Kind, s.StartTime, s.Status,
		s.Category, s.ThreadID, s.TaskID, s.SessionID, s.AgentID, string(attributesJSON))

	return err
}

func (s *Span) update() error {
	attributesJSON, _ := json.Marshal(s.Attributes)

	_, err := s.db.Exec(`
		UPDATE spans
		SET end_time = ?, duration_ms = ?, status = ?, status_message = ?,
		    attributes = ?, token_usage_input = ?, token_usage_output = ?,
		    cache_creation_tokens = ?, cache_read_tokens = ?, reasoning_tokens = ?,
		    provider = ?, model = ?,
		    thread_id = ?, task_id = ?
		WHERE id = ?`,
		s.EndTime, s.DurationMS, s.Status, s.StatusMessage,
		string(attributesJSON), s.TokenUsageIn, s.TokenUsageOut,
		s.CacheCreationTokens, s.CacheReadTokens, s.ReasoningTokens,
		s.Provider, s.Model,
		s.ThreadID, s.TaskID, s.ID)

	return err
}

// GetCurrentSpan returns the currently active span from context
// In a real implementation, this would use context.Context
var currentSpan *Span
var currentTrace *Trace
var spanMu sync.RWMutex

func SetCurrentSpan(span *Span) {
	spanMu.Lock()
	defer spanMu.Unlock()
	currentSpan = span
}

func GetCurrentSpan() *Span {
	spanMu.RLock()
	defer spanMu.RUnlock()
	return currentSpan
}

func SetCurrentTrace(trace *Trace) {
	spanMu.Lock()
	defer spanMu.Unlock()
	currentTrace = trace
}

func GetCurrentTrace() *Trace {
	spanMu.RLock()
	defer spanMu.RUnlock()
	return currentTrace
}

// Helper to convert span to JSON
func (s *Span) ToJSON() (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Helper to convert trace to JSON
func (t *Trace) ToJSON() (string, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Query helpers
func GetTrace(db *sql.DB, traceID string) (*Trace, error) {
	var trace Trace
	var metadataJSON, endTime sql.NullString
	var durationMS, spanCount, errorCount sql.NullInt64

	err := db.QueryRow(`
		SELECT id, name, thread_id, service_name, start_time, end_time, duration_ms,
		       status, root_span_id, span_count, error_count, metadata
		FROM traces WHERE id = ?`, traceID).Scan(
		&trace.ID, &trace.Name, &trace.ThreadID, &trace.ServiceName, &trace.StartTime,
		&endTime, &durationMS, &trace.Status, &trace.RootSpanID,
		&spanCount, &errorCount, &metadataJSON)

	if err != nil {
		return nil, err
	}

	if endTime.Valid {
		t, _ := time.Parse(time.RFC3339, endTime.String)
		trace.EndTime = &t
	}
	if durationMS.Valid {
		trace.DurationMS = durationMS.Int64
	}
	if spanCount.Valid {
		trace.SpanCount = int(spanCount.Int64)
	}
	if errorCount.Valid {
		trace.ErrorCount = int(errorCount.Int64)
	}
	if metadataJSON.Valid {
		json.Unmarshal([]byte(metadataJSON.String), &trace.Metadata)
	}

	trace.db = db
	return &trace, nil
}

func GetSpan(db *sql.DB, spanID string) (*Span, error) {
	var span Span
	var attributesJSON, endTime, parentSpanID, statusMessage, category sql.NullString
	var threadID, sessionID, agentID, inputData, outputData sql.NullString
	var provider, model sql.NullString
	var durationMS, tokenIn, tokenOut, cacheCreation, cacheRead, reasoning sql.NullInt64

	err := db.QueryRow(`
		SELECT id, trace_id, parent_span_id, name, kind, start_time, end_time, duration_ms,
		       status, status_message, category, thread_id, session_id, agent_id, attributes,
		       input_data, output_data, token_usage_input, token_usage_output,
		       cache_creation_tokens, cache_read_tokens, reasoning_tokens, provider, model
		FROM spans WHERE id = ?`, spanID).Scan(
		&span.ID, &span.TraceID, &parentSpanID, &span.Name, &span.Kind, &span.StartTime,
		&endTime, &durationMS, &span.Status, &statusMessage, &category,
		&threadID, &sessionID, &agentID, &attributesJSON,
		&inputData, &outputData, &tokenIn, &tokenOut,
		&cacheCreation, &cacheRead, &reasoning, &provider, &model)

	if err != nil {
		return nil, err
	}

	// Handle nullable fields
	if parentSpanID.Valid {
		span.ParentSpanID = parentSpanID.String
	}
	if endTime.Valid {
		t, _ := time.Parse(time.RFC3339, endTime.String)
		span.EndTime = &t
	}
	if durationMS.Valid {
		span.DurationMS = durationMS.Int64
	}
	if statusMessage.Valid {
		span.StatusMessage = statusMessage.String
	}
	if category.Valid {
		span.Category = EventCategory(category.String)
	}
	if threadID.Valid {
		span.ThreadID = threadID.String
	}
	if sessionID.Valid {
		span.SessionID = sessionID.String
	}
	if agentID.Valid {
		span.AgentID = agentID.String
	}
	if attributesJSON.Valid {
		json.Unmarshal([]byte(attributesJSON.String), &span.Attributes)
	}
	if tokenIn.Valid {
		span.TokenUsageIn = int(tokenIn.Int64)
	}
	if tokenOut.Valid {
		span.TokenUsageOut = int(tokenOut.Int64)
	}
	if cacheCreation.Valid {
		span.CacheCreationTokens = int(cacheCreation.Int64)
	}
	if cacheRead.Valid {
		span.CacheReadTokens = int(cacheRead.Int64)
	}
	if reasoning.Valid {
		span.ReasoningTokens = int(reasoning.Int64)
	}
	if provider.Valid {
		span.Provider = provider.String
	}
	if model.Valid {
		span.Model = model.String
	}

	span.db = db
	return &span, nil
}

func GetSpansForTrace(db *sql.DB, traceID string) ([]*Span, error) {
	rows, err := db.Query(`
		SELECT id, trace_id, parent_span_id, name, kind, start_time, end_time, duration_ms,
		       status, status_message, category, thread_id, session_id, agent_id, attributes,
		       token_usage_input, token_usage_output,
		       cache_creation_tokens, cache_read_tokens, reasoning_tokens, provider, model
		FROM spans
		WHERE trace_id = ?
		ORDER BY start_time`, traceID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	spans := make([]*Span, 0)
	for rows.Next() {
		var span Span
		var attributesJSON, endTime, parentSpanID, statusMessage, category sql.NullString
		var threadID, sessionID, agentID sql.NullString
		var provider, model sql.NullString
		var durationMS, tokenIn, tokenOut, cacheCreation, cacheRead, reasoning sql.NullInt64

		err := rows.Scan(
			&span.ID, &span.TraceID, &parentSpanID, &span.Name, &span.Kind, &span.StartTime,
			&endTime, &durationMS, &span.Status, &statusMessage, &category,
			&threadID, &sessionID, &agentID, &attributesJSON,
			&tokenIn, &tokenOut,
			&cacheCreation, &cacheRead, &reasoning, &provider, &model)

		if err != nil {
			continue
		}

		// Handle nullable fields
		if parentSpanID.Valid {
			span.ParentSpanID = parentSpanID.String
		}
		if endTime.Valid {
			t, _ := time.Parse(time.RFC3339, endTime.String)
			span.EndTime = &t
		}
		if durationMS.Valid {
			span.DurationMS = durationMS.Int64
		}
		if statusMessage.Valid {
			span.StatusMessage = statusMessage.String
		}
		if category.Valid {
			span.Category = EventCategory(category.String)
		}
		if threadID.Valid {
			span.ThreadID = threadID.String
		}
		if sessionID.Valid {
			span.SessionID = sessionID.String
		}
		if agentID.Valid {
			span.AgentID = agentID.String
		}
		if attributesJSON.Valid {
			json.Unmarshal([]byte(attributesJSON.String), &span.Attributes)
		}
		if tokenIn.Valid {
			span.TokenUsageIn = int(tokenIn.Int64)
		}
		if tokenOut.Valid {
			span.TokenUsageOut = int(tokenOut.Int64)
		}
		if cacheCreation.Valid {
			span.CacheCreationTokens = int(cacheCreation.Int64)
		}
		if cacheRead.Valid {
			span.CacheReadTokens = int(cacheRead.Int64)
		}
		if reasoning.Valid {
			span.ReasoningTokens = int(reasoning.Int64)
		}
		if provider.Valid {
			span.Provider = provider.String
		}
		if model.Valid {
			span.Model = model.String
		}

		span.db = db
		spans = append(spans, &span)
	}

	return spans, nil
}
