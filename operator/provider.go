package operator

import (
	"context"
	"sync"
)

// BrowserProvider abstracts browser session lifecycle.
// All providers create sessions (possibly via REST) and return connection info.
// Providers that support CDP return a ConnectURL for direct WebSocket control.
type BrowserProvider interface {
	// Name returns the provider identifier ("browserengine", "browserbase", "steel", "chrome")
	Name() string

	// CreateSession creates a browser session and returns connection metadata.
	// Sessions with a non-empty ConnectURL support direct CDP WebSocket control.
	CreateSession(ctx context.Context, opts SessionOptions) (*BrowserSession, error)

	// DestroySession terminates a browser session and cleans up resources.
	DestroySession(ctx context.Context, sessionID string) error
}

// SessionOptions contains parameters for session creation.
type SessionOptions struct {
	InitialURL     string
	ViewportWidth  int
	ViewportHeight int
	Proxy          bool
	TestMode       bool
}

// BrowserSession holds the state of a connected browser session.
type BrowserSession struct {
	ID         string                 // Provider-assigned session ID
	ConnectURL string                 // CDP WebSocket URL (empty for REST-only providers)
	StreamURL  string                 // Optional: live stream URL for viewing
	ViewURL    string                 // Optional: browser view URL
	TargetID   string                 // Optional: CDP target ID
	Provider   string                 // Provider name that created this session
	Metadata   map[string]interface{} // Extra provider-specific data

	// CDP connection state (populated after ConnectCDP for CDP-capable sessions)
	mu          sync.Mutex
	cdpCtx      context.Context
	cdpCancel   context.CancelFunc
	isConnected bool
}

// IsConnectedCDP returns true if this session has an active CDP WebSocket.
func (s *BrowserSession) IsConnectedCDP() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isConnected && s.cdpCtx != nil
}

// GetCDPContext returns the CDP context for command execution.
func (s *BrowserSession) GetCDPContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cdpCtx
}

// SetCDP stores the CDP connection state.
func (s *BrowserSession) SetCDP(ctx context.Context, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cdpCtx = ctx
	s.cdpCancel = cancel
	s.isConnected = true
}

// Close cleans up the CDP connection if active.
func (s *BrowserSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cdpCancel != nil {
		s.cdpCancel()
		s.cdpCancel = nil
		s.cdpCtx = nil
		s.isConnected = false
	}
}
