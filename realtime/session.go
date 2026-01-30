package realtime

import (
	"sync"
	"time"

	"github.com/apteva/agent/events"
	"github.com/apteva/agent/handlers/threads"

	"github.com/gorilla/websocket"
)

// Session represents an active realtime voice session
type Session struct {
	ID           string
	ThreadID     string
	Provider     string // "openai-realtime"

	// Tool execution state
	PendingTools map[string]*ToolExecution

	// Connections
	clientConn   *websocket.Conn
	openaiConn   *websocket.Conn

	// Infrastructure
	messageSaver threads.MessageSaver
	eventBus     *events.EventBus

	State        SessionState
	CreatedAt    time.Time
	LastActivity time.Time

	mu           sync.RWMutex
}

// SessionState tracks the current state of a session
type SessionState struct {
	IsActive          bool
	TurnState         string // "idle", "user_speaking", "assistant_speaking", "tool_executing"
	ConversationTurns int
	CurrentResponseID string
}

// ToolExecution represents a tool call in progress
type ToolExecution struct {
	CallID    string
	Name      string
	Arguments map[string]interface{}
	Status    string // "pending", "executing", "completed", "failed"
	Result    interface{}
	Error     error
	StartTime time.Time
	EndTime   time.Time
}

// UpdateLastActivity updates the last activity timestamp
func (s *Session) UpdateLastActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActivity = time.Now()
}

// AddPendingTool adds a tool execution to pending state
func (s *Session) AddPendingTool(execution *ToolExecution) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PendingTools[execution.CallID] = execution
}

// GetPendingTool retrieves a pending tool execution
func (s *Session) GetPendingTool(callID string) (*ToolExecution, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exec, ok := s.PendingTools[callID]
	return exec, ok
}

// UpdateToolStatus updates the status of a tool execution
func (s *Session) UpdateToolStatus(callID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if exec, ok := s.PendingTools[callID]; ok {
		exec.Status = status
		if status == "completed" || status == "failed" {
			exec.EndTime = time.Now()
		}
	}
}

// SetTurnState sets the current turn state
func (s *Session) SetTurnState(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State.TurnState = state
}

// Close closes the session connections
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.State.IsActive = false

	if s.clientConn != nil {
		s.clientConn.Close()
	}

	if s.openaiConn != nil {
		s.openaiConn.Close()
	}
}
