package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// Headers for request cancellation in agent-to-agent communication
const (
	HeaderRequestID      = "X-Agent-Request-ID"      // Unique request identifier
	HeaderCancelCallback = "X-Agent-Cancel-Callback" // URL to call for cancellation
)

// CancellationConfig holds configuration for request cancellation
type CancellationConfig struct {
	Enabled           bool   `json:"enabled"`
	MaxActiveRequests int    `json:"max_active_requests"`
	CleanupInterval   string `json:"cleanup_interval"`
	PropagateToAgents bool   `json:"propagate_to_agents"`
}

// CancelRequest is the request body for cancel endpoint
type CancelRequest struct {
	Reason string `json:"reason,omitempty"`
}

// CancelResponse is the response from cancel endpoint
type CancelResponse struct {
	Success   bool   `json:"success"`
	RequestID string `json:"request_id"`
	Cancelled bool   `json:"cancelled"`
	Error     string `json:"error,omitempty"`
}

// propagateCancellation calls a downstream agent's cancel endpoint
func propagateCancellation(callbackURL string) {
	if callbackURL == "" {
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}

	reqBody := CancelRequest{Reason: "parent_cancelled"}
	jsonData, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", callbackURL, bytes.NewReader(jsonData))
	if err != nil {
		log.Printf("⚠️  Failed to create cancel request for %s: %v", callbackURL, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("⚠️  Failed to propagate cancellation to %s: %v", callbackURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Printf("🔗 Cancellation propagated to %s", callbackURL)
	} else {
		log.Printf("⚠️  Cancel callback returned %d for %s", resp.StatusCode, callbackURL)
	}
}

// AddCancellationHeaders adds cancellation-related headers to an outgoing request
func AddCancellationHeaders(req *http.Request, requestID, cancelCallbackURL string) {
	if requestID != "" {
		req.Header.Set(HeaderRequestID, requestID)
	}
	if cancelCallbackURL != "" {
		req.Header.Set(HeaderCancelCallback, cancelCallbackURL)
	}
}

// ExtractCancellationContext extracts cancellation info from incoming request headers
func ExtractCancellationContext(r *http.Request) (requestID, cancelCallback string) {
	requestID = r.Header.Get(HeaderRequestID)
	cancelCallback = r.Header.Get(HeaderCancelCallback)
	return
}

// ContextWithCancellation creates a child context that respects both parent cancellation
// and the request tracker's cancellation
func ContextWithCancellation(parent context.Context, tracker *RequestTracker, requestID string) context.Context {
	if tracker == nil || requestID == "" {
		return parent
	}

	req := tracker.Get(requestID)
	if req == nil {
		return parent
	}

	// Create a context that cancels when either parent or request is cancelled
	ctx, cancel := context.WithCancel(parent)

	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-req.ctx.Done():
			cancel()
		}
	}()

	return ctx
}

// IsCancelled checks if a request has been cancelled
func IsCancelled(tracker *RequestTracker, requestID string) bool {
	if tracker == nil || requestID == "" {
		return false
	}

	req := tracker.Get(requestID)
	if req == nil {
		return false
	}

	return req.IsCancelled()
}

// SSECancelledEvent returns the SSE event data for a cancelled request
type SSECancelledEvent struct {
	RequestID string `json:"request_id"`
	Reason    string `json:"reason"`
}

// SSEStartEvent returns the SSE event data for request start
type SSEStartEvent struct {
	RequestID string `json:"request_id"`
	ThreadID  string `json:"thread_id,omitempty"`
}
