package agents

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"log"
	"sync"
	"time"
)

// RequestStatus represents the current state of a request
type RequestStatus string

const (
	RequestStatusProcessing RequestStatus = "processing"
	RequestStatusCancelled  RequestStatus = "cancelled"
	RequestStatusCompleted  RequestStatus = "completed"
)

// ActiveRequest represents an ongoing chat request
type ActiveRequest struct {
	RequestID       string            `json:"request_id"`
	ThreadID        string            `json:"thread_id,omitempty"`
	Status          RequestStatus     `json:"status"`
	StartTime       time.Time         `json:"start_time"`
	CancelledAt     *time.Time        `json:"cancelled_at,omitempty"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`
	CancelCallbacks []string          `json:"-"` // URLs to call when cancelled (for agent propagation)
	cancel          context.CancelFunc
	ctx             context.Context
}

// IsCancelled returns true if the request has been cancelled
func (r *ActiveRequest) IsCancelled() bool {
	return r.Status == RequestStatusCancelled
}

// Context returns the request's context
func (r *ActiveRequest) Context() context.Context {
	return r.ctx
}

// RequestTracker manages active requests and their cancellation
type RequestTracker struct {
	requests     map[string]*ActiveRequest
	mu           sync.RWMutex
	maxActive    int
	cleanupAfter time.Duration
}

// NewRequestTracker creates a new request tracker
func NewRequestTracker() *RequestTracker {
	rt := &RequestTracker{
		requests:     make(map[string]*ActiveRequest),
		maxActive:    100,           // Default max active requests
		cleanupAfter: 5 * time.Minute, // Default cleanup interval
	}

	// Start background cleanup goroutine
	go rt.cleanupLoop()

	return rt
}

// Configure updates tracker settings
func (rt *RequestTracker) Configure(maxActive int, cleanupAfter time.Duration) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if maxActive > 0 {
		rt.maxActive = maxActive
	}
	if cleanupAfter > 0 {
		rt.cleanupAfter = cleanupAfter
	}
}

// GenerateRequestID creates a unique request ID
// Format: req_{timestamp_base32}_{random_6chars}
func GenerateRequestID() string {
	// Timestamp component (compact base32)
	ts := time.Now().UnixNano() / 1000000 // Milliseconds
	tsBytes := []byte(fmt.Sprintf("%d", ts))
	tsEncoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(tsBytes)
	if len(tsEncoded) > 8 {
		tsEncoded = tsEncoded[len(tsEncoded)-8:]
	}

	// Random component
	randBytes := make([]byte, 4)
	rand.Read(randBytes)
	randEncoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randBytes)
	if len(randEncoded) > 6 {
		randEncoded = randEncoded[:6]
	}

	return fmt.Sprintf("req_%s_%s", tsEncoded, randEncoded)
}

// Register creates a new active request and returns its context
// The returned context will be cancelled when Cancel is called
func (rt *RequestTracker) Register(requestID, threadID string) (context.Context, *ActiveRequest, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Check capacity
	activeCount := 0
	for _, req := range rt.requests {
		if req.Status == RequestStatusProcessing {
			activeCount++
		}
	}
	if activeCount >= rt.maxActive {
		return nil, nil, fmt.Errorf("max active requests (%d) reached", rt.maxActive)
	}

	// Check for duplicate
	if _, exists := rt.requests[requestID]; exists {
		return nil, nil, fmt.Errorf("request %s already exists", requestID)
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	req := &ActiveRequest{
		RequestID:       requestID,
		ThreadID:        threadID,
		Status:          RequestStatusProcessing,
		StartTime:       time.Now(),
		CancelCallbacks: []string{},
		cancel:          cancel,
		ctx:             ctx,
	}

	rt.requests[requestID] = req

	if threadID != "" {
		log.Printf("📝 Request registered: %s (thread: %s)", requestID, threadID)
	} else {
		log.Printf("📝 Request registered: %s (new conversation)", requestID)
	}

	return ctx, req, nil
}

// RegisterWithParentContext creates a request with a parent context
// Cancellation of either parent or explicit cancel will cancel the request
func (rt *RequestTracker) RegisterWithParentContext(parentCtx context.Context, requestID, threadID string) (context.Context, *ActiveRequest, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Check capacity
	activeCount := 0
	for _, req := range rt.requests {
		if req.Status == RequestStatusProcessing {
			activeCount++
		}
	}
	if activeCount >= rt.maxActive {
		return nil, nil, fmt.Errorf("max active requests (%d) reached", rt.maxActive)
	}

	// Check for duplicate
	if _, exists := rt.requests[requestID]; exists {
		return nil, nil, fmt.Errorf("request %s already exists", requestID)
	}

	// Create cancellable context from parent
	ctx, cancel := context.WithCancel(parentCtx)

	req := &ActiveRequest{
		RequestID:       requestID,
		ThreadID:        threadID,
		Status:          RequestStatusProcessing,
		StartTime:       time.Now(),
		CancelCallbacks: []string{},
		cancel:          cancel,
		ctx:             ctx,
	}

	rt.requests[requestID] = req

	if threadID != "" {
		log.Printf("📝 Request registered: %s (thread: %s)", requestID, threadID)
	} else {
		log.Printf("📝 Request registered: %s (new conversation)", requestID)
	}

	return ctx, req, nil
}

// Cancel cancels an active request
func (rt *RequestTracker) Cancel(requestID string) error {
	rt.mu.Lock()
	req, exists := rt.requests[requestID]
	if !exists {
		rt.mu.Unlock()
		return fmt.Errorf("request %s not found", requestID)
	}

	if req.Status != RequestStatusProcessing {
		rt.mu.Unlock()
		return fmt.Errorf("request %s is not processing (status: %s)", requestID, req.Status)
	}

	// Update status
	now := time.Now()
	req.Status = RequestStatusCancelled
	req.CancelledAt = &now

	// Get callbacks before unlocking
	callbacks := make([]string, len(req.CancelCallbacks))
	copy(callbacks, req.CancelCallbacks)

	rt.mu.Unlock()

	// Cancel the context
	if req.cancel != nil {
		req.cancel()
	}

	log.Printf("🛑 Request cancelled: %s", requestID)

	// Propagate cancellation to downstream agents (best effort)
	for _, callbackURL := range callbacks {
		go propagateCancellation(callbackURL)
	}

	return nil
}

// Complete marks a request as completed
func (rt *RequestTracker) Complete(requestID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	req, exists := rt.requests[requestID]
	if !exists {
		return
	}

	// Only mark as completed if still processing
	if req.Status == RequestStatusProcessing {
		now := time.Now()
		req.Status = RequestStatusCompleted
		req.CompletedAt = &now
		log.Printf("✅ Request completed: %s (duration: %v)", requestID, time.Since(req.StartTime))
	}
}

// Get returns an active request by ID
func (rt *RequestTracker) Get(requestID string) *ActiveRequest {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	return rt.requests[requestID]
}

// AddCancelCallback adds a callback URL to be called when request is cancelled
func (rt *RequestTracker) AddCancelCallback(requestID, callbackURL string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	req, exists := rt.requests[requestID]
	if !exists {
		return
	}

	req.CancelCallbacks = append(req.CancelCallbacks, callbackURL)
}

// GetActiveCount returns the number of active (processing) requests
func (rt *RequestTracker) GetActiveCount() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	count := 0
	for _, req := range rt.requests {
		if req.Status == RequestStatusProcessing {
			count++
		}
	}
	return count
}

// GetAll returns all requests (for debugging)
func (rt *RequestTracker) GetAll() []*ActiveRequest {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	result := make([]*ActiveRequest, 0, len(rt.requests))
	for _, req := range rt.requests {
		result = append(result, req)
	}
	return result
}

// cleanupLoop periodically removes old completed/cancelled requests
func (rt *RequestTracker) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rt.cleanup()
	}
}

// cleanup removes old completed/cancelled requests
func (rt *RequestTracker) cleanup() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	cutoff := time.Now().Add(-rt.cleanupAfter)
	removed := 0

	for id, req := range rt.requests {
		// Remove completed/cancelled requests older than cleanup interval
		if req.Status == RequestStatusCompleted && req.CompletedAt != nil && req.CompletedAt.Before(cutoff) {
			delete(rt.requests, id)
			removed++
		} else if req.Status == RequestStatusCancelled && req.CancelledAt != nil && req.CancelledAt.Before(cutoff) {
			delete(rt.requests, id)
			removed++
		}
	}

	if removed > 0 {
		log.Printf("🧹 Cleaned up %d old requests", removed)
	}
}
