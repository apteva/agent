package realtime

import "time"

// EventType represents the type of unified event
type EventType string

const (
	EventTypeAudioDelta        EventType = "audio_delta"
	EventTypeAudioComplete     EventType = "audio_complete"
	EventTypeAudioInterrupt    EventType = "audio_interrupt"
	EventTypeTranscript        EventType = "transcript"
	EventTypeToolCall          EventType = "tool_call"
	EventTypeToolResult        EventType = "tool_result"
	EventTypeTurnStart         EventType = "turn_start"
	EventTypeTurnEnd           EventType = "turn_end"
	EventTypeError             EventType = "error"
	EventTypeSessionCreated    EventType = "session_created"
)

// UnifiedEvent represents a provider-agnostic event
type UnifiedEvent struct {
	Type      EventType   `json:"type"`
	SessionID string      `json:"session_id,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// AudioDeltaData contains audio chunk data
type AudioDeltaData struct {
	Format     string `json:"format"`      // "pcm16"
	SampleRate int    `json:"sample_rate"` // 24000
	Chunk      string `json:"chunk"`       // Base64-encoded audio
}

// TranscriptData contains transcription information
type TranscriptData struct {
	Role    string `json:"role"`              // "user" or "assistant"
	Content string `json:"content"`           // Transcribed text
	Partial bool   `json:"partial,omitempty"` // True for interim/partial transcripts
}

// ToolCallData contains tool invocation information
type ToolCallData struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

// ToolResultData contains tool execution result
type ToolResultData struct {
	CallID string      `json:"call_id"`
	Name   string      `json:"name"`
	Result interface{} `json:"result"`
	Error  error       `json:"error,omitempty"`
}

// ErrorData contains error information
type ErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
