package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// EventCategory represents the category of an event
type EventCategory string

const (
	CategorySystem    EventCategory = "SYSTEM"
	CategoryChat      EventCategory = "CHAT"
	CategoryTool      EventCategory = "TOOL"
	CategoryDatabase  EventCategory = "DATABASE"
	CategoryMCP       EventCategory = "MCP"
	CategoryScheduler EventCategory = "SCHEDULER"
	CategoryLLM       EventCategory = "LLM"
	CategoryTask      EventCategory = "TASK"
	CategoryMemory    EventCategory = "MEMORY"
	CategoryAgent     EventCategory = "AGENT"
	CategoryError      EventCategory = "ERROR"
	CategoryFile       EventCategory = "FILE"
	CategoryReflection EventCategory = "REFLECTION"
)

// EventType represents specific event types within categories
type EventType string

const (
	// System events
	TypeSystemStartup  EventType = "system_startup"
	TypeSystemShutdown EventType = "system_shutdown"
	TypeConfigChange   EventType = "config_change"

	// Chat events
	TypeChatMessageReceived EventType = "message_received"
	TypeChatResponseStarted EventType = "response_started"
	TypeChatStreamChunk     EventType = "stream_chunk"
	TypeChatResponseComplete EventType = "response_complete"
	TypeChatError           EventType = "chat_error"

	// Tool events
	TypeToolInvocation EventType = "tool_invocation"
	TypeToolResult     EventType = "tool_result"
	TypeToolError      EventType = "tool_error"

	// Database events
	TypeDBQuery  EventType = "db_query"
	TypeDBInsert EventType = "db_insert"
	TypeDBUpdate EventType = "db_update"
	TypeDBDelete EventType = "db_delete"
	TypeDBError  EventType = "db_error"

	// MCP events
	TypeMCPServerConnect EventType = "mcp_server_connect"
	TypeMCPToolDiscovery EventType = "mcp_tool_discovery"
	TypeMCPToolExecution EventType = "mcp_tool_execution"
	TypeMCPError         EventType = "mcp_error"

	// Scheduler events
	TypeSchedulerTaskStart    EventType = "scheduler_task_start"
	TypeSchedulerTaskComplete EventType = "scheduler_task_complete"
	TypeSchedulerTaskFailed   EventType = "scheduler_task_failed"

	// LLM events
	TypeLLMRequest  EventType = "llm_request"
	TypeLLMResponse EventType = "llm_response"
	TypeLLMTokens   EventType = "llm_tokens"
	TypeLLMError    EventType = "llm_error"

	// Task events
	TypeTaskCreated  EventType = "task_created"
	TypeTaskUpdated  EventType = "task_updated"
	TypeTaskExecuted EventType = "task_executed"
	TypeTaskDeleted  EventType = "task_deleted"
	TypeTaskFailed   EventType = "task_failed"
	TypeTaskListed   EventType = "task_listed"

	// File events
	TypeFileUploaded EventType = "file_uploaded"
	TypeFileDeleted  EventType = "file_deleted"
	TypeFileExpired  EventType = "file_expired"
	TypeFileIngested EventType = "file_ingested"
)

// EventLevel represents the severity level of an event
type EventLevel string

const (
	LevelDebug EventLevel = "debug"
	LevelInfo  EventLevel = "info"
	LevelWarn  EventLevel = "warn"
	LevelError EventLevel = "error"
)

// Event represents a single event in the system
type Event struct {
	ID         string                 `json:"id"`
	Timestamp  time.Time              `json:"timestamp"`
	Category   EventCategory          `json:"category"`
	Type       EventType              `json:"type"`
	Level      EventLevel             `json:"level"`
	ThreadID   string                 `json:"thread_id,omitempty"`
	TaskID     string                 `json:"task_id,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	TraceID    string                 `json:"trace_id,omitempty"`    // Link to trace
	SpanID     string                 `json:"span_id,omitempty"`     // Link to span
	Data       map[string]interface{} `json:"data,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	DurationMS int64                  `json:"duration_ms,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

// NewEvent creates a new event with generated ID and current timestamp
func NewEvent(category EventCategory, eventType EventType, level EventLevel) *Event {
	return &Event{
		ID:        "evt_" + uuid.New().String()[:8],
		Timestamp: time.Now(),
		Category:  category,
		Type:      eventType,
		Level:     level,
		Data:      make(map[string]interface{}),
		Metadata:  make(map[string]interface{}),
	}
}

// WithThread adds thread context to the event
func (e *Event) WithThread(threadID string) *Event {
	e.ThreadID = threadID
	return e
}

// WithTask adds task context to the event
func (e *Event) WithTask(taskID string) *Event {
	e.TaskID = taskID
	return e
}

// WithSession adds session context to the event
func (e *Event) WithSession(sessionID string) *Event {
	e.SessionID = sessionID
	return e
}

// WithTrace adds trace context to the event
func (e *Event) WithTrace(traceID string) *Event {
	e.TraceID = traceID
	return e
}

// WithSpan adds span context to the event
func (e *Event) WithSpan(spanID string) *Event {
	e.SpanID = spanID
	return e
}

// WithData adds data fields to the event
func (e *Event) WithData(key string, value interface{}) *Event {
	if e.Data == nil {
		e.Data = make(map[string]interface{})
	}
	e.Data[key] = value
	return e
}

// WithMetadata adds metadata fields to the event
func (e *Event) WithMetadata(key string, value interface{}) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// WithDuration adds duration in milliseconds
func (e *Event) WithDuration(startTime time.Time) *Event {
	e.DurationMS = time.Since(startTime).Milliseconds()
	return e
}

// WithError adds error information to the event
func (e *Event) WithError(err error) *Event {
	if err != nil {
		e.Error = err.Error()
		e.Level = LevelError
	}
	return e
}

// ToJSON converts event to JSON string
func (e *Event) ToJSON() (string, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ToSSE formats the event for Server-Sent Events
func (e *Event) ToSSE() (string, error) {
	jsonData, err := e.ToJSON()
	if err != nil {
		return "", err
	}
	return "data: " + jsonData + "\n\n", nil
}