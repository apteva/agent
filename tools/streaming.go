package tools

import (
	"time"
)

// StreamingTool extends Tool with streaming execution capability
// Tools that implement this interface can stream output during execution
type StreamingTool interface {
	Tool // Embed base interface

	// SupportsStreaming returns true if this tool can stream output
	SupportsStreaming() bool

	// ExecuteStreaming executes the tool and streams output via callback
	// The callback is called for each chunk of output
	// Final result is returned when complete
	ExecuteStreaming(params map[string]interface{}, callback StreamCallback) (interface{}, error)
}

// StreamCallback is called for each streaming event during tool execution
type StreamCallback func(event ToolStreamEvent)

// ToolStreamEvent represents a streaming event from a tool
type ToolStreamEvent struct {
	Type      string      `json:"type"`               // Event type (progress, output, log, chunk, error)
	Content   string      `json:"content,omitempty"`  // Text content
	Data      interface{} `json:"data,omitempty"`     // Structured data (optional)
	Progress  *float64    `json:"progress,omitempty"` // 0.0-1.0 for progress events
	Timestamp int64       `json:"timestamp"`          // Unix milliseconds
}

// Event type constants
const (
	ToolEventProgress = "progress" // Progress update (percentage)
	ToolEventOutput   = "output"   // Streaming output (like agent response)
	ToolEventLog      = "log"      // Log/status message
	ToolEventChunk    = "chunk"    // Raw chunk (for sub-agent streaming)
	ToolEventError    = "error"    // Non-fatal error/warning
)

// NewToolStreamEvent creates a new ToolStreamEvent with current timestamp
func NewToolStreamEvent(eventType, content string) ToolStreamEvent {
	return ToolStreamEvent{
		Type:      eventType,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewProgressEvent creates a progress event with percentage
func NewProgressEvent(content string, progress float64) ToolStreamEvent {
	return ToolStreamEvent{
		Type:      ToolEventProgress,
		Content:   content,
		Progress:  &progress,
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewChunkEvent creates a chunk event for streaming content
func NewChunkEvent(content string) ToolStreamEvent {
	return ToolStreamEvent{
		Type:      ToolEventChunk,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewOutputEvent creates an output event
func NewOutputEvent(content string) ToolStreamEvent {
	return ToolStreamEvent{
		Type:      ToolEventOutput,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewLogEvent creates a log event
func NewLogEvent(content string) ToolStreamEvent {
	return ToolStreamEvent{
		Type:      ToolEventLog,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewErrorEvent creates an error event (non-fatal)
func NewErrorEvent(content string) ToolStreamEvent {
	return ToolStreamEvent{
		Type:      ToolEventError,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}
}

// IsStreamingTool checks if a tool supports streaming
func IsStreamingTool(tool Tool) bool {
	if streamingTool, ok := tool.(StreamingTool); ok {
		return streamingTool.SupportsStreaming()
	}
	return false
}

// ExecuteWithStreaming executes a tool, using streaming if supported
// If the tool doesn't support streaming, it falls back to regular Execute
func ExecuteWithStreaming(tool Tool, params map[string]interface{}, callback StreamCallback) (interface{}, error) {
	if streamingTool, ok := tool.(StreamingTool); ok && streamingTool.SupportsStreaming() {
		return streamingTool.ExecuteStreaming(params, callback)
	}
	// Fall back to regular execution
	return tool.Execute(params)
}
