package agents

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Headers for call chain tracking between agents
const (
	HeaderCallChain   = "X-Agent-Call-Chain"   // JSON array of agent IDs in call path
	HeaderCallDepth   = "X-Agent-Call-Depth"   // Current depth (integer)
	HeaderOriginAgent = "X-Agent-Origin"       // Original requesting agent
	HeaderCallID      = "X-Agent-Call-ID"      // Unique ID for this call tree
	HeaderTTL         = "X-Agent-TTL"          // Time-to-live in seconds
	HeaderStartTime   = "X-Agent-Start-Time"   // Unix timestamp (nanoseconds) when call chain started
)

// CallContext holds the context of a call chain
type CallContext struct {
	CallChain   []string  `json:"call_chain"`   // List of agent IDs in the call path
	CallDepth   int       `json:"call_depth"`   // Current depth in the call tree
	OriginAgent string    `json:"origin_agent"` // The agent that started the chain
	CallID      string    `json:"call_id"`      // Unique identifier for this call tree
	StartTime   time.Time `json:"start_time"`   // When the call chain started
}

// GuardRailError represents a blocked call due to guard rails
type GuardRailError struct {
	Type    string      `json:"type"`    // "circular_call", "max_depth", "circuit_open", "ttl_expired"
	Message string      `json:"message"` // Human-readable message
	Details interface{} `json:"details"` // Additional context
}

func (e *GuardRailError) Error() string {
	return e.Message
}

// CircuitBreaker prevents repeated calls to failing agents
type CircuitBreaker struct {
	failures    map[string]int       // agentID -> consecutive failure count
	lastFailure map[string]time.Time // agentID -> last failure time
	openUntil   map[string]time.Time // agentID -> circuit open until this time
	mu          sync.RWMutex

	// Configuration
	threshold  int           // failures before opening circuit (default: 3)
	resetAfter time.Duration // time before resetting failure count (default: 2m)
	openFor    time.Duration // how long circuit stays open (default: 30s)
}

// NewCircuitBreaker creates a new circuit breaker with default settings
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		failures:    make(map[string]int),
		lastFailure: make(map[string]time.Time),
		openUntil:   make(map[string]time.Time),
		threshold:   3,
		resetAfter:  2 * time.Minute,
		openFor:     30 * time.Second,
	}
}

// Configure updates circuit breaker settings
func (cb *CircuitBreaker) Configure(threshold int, resetAfter, openFor time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if threshold > 0 {
		cb.threshold = threshold
	}
	if resetAfter > 0 {
		cb.resetAfter = resetAfter
	}
	if openFor > 0 {
		cb.openFor = openFor
	}
}

// CanCall checks if a call to the agent is allowed
func (cb *CircuitBreaker) CanCall(agentID string) bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Check if circuit is open
	if openUntil, exists := cb.openUntil[agentID]; exists {
		if time.Now().Before(openUntil) {
			return false // Circuit is still open
		}
	}
	return true
}

// GetState returns the current state for an agent
func (cb *CircuitBreaker) GetState(agentID string) string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if openUntil, exists := cb.openUntil[agentID]; exists {
		if time.Now().Before(openUntil) {
			return "open"
		}
	}

	if failures, exists := cb.failures[agentID]; exists && failures > 0 {
		return "half-open"
	}

	return "closed"
}

// RecordSuccess records a successful call and resets the failure count
func (cb *CircuitBreaker) RecordSuccess(agentID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Reset failure count on success
	delete(cb.failures, agentID)
	delete(cb.lastFailure, agentID)
	delete(cb.openUntil, agentID)
}

// RecordFailure records a failed call and potentially opens the circuit
func (cb *CircuitBreaker) RecordFailure(agentID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if we should reset due to time elapsed
	if lastFail, exists := cb.lastFailure[agentID]; exists {
		if time.Since(lastFail) > cb.resetAfter {
			cb.failures[agentID] = 0
		}
	}

	cb.failures[agentID]++
	cb.lastFailure[agentID] = time.Now()

	if cb.failures[agentID] >= cb.threshold {
		cb.openUntil[agentID] = time.Now().Add(cb.openFor)
		log.Printf("⚡ Circuit breaker OPEN for agent %s (failures: %d, open for: %v)",
			agentID, cb.failures[agentID], cb.openFor)
	}
}

// TimeUntilReset returns how long until the circuit might close for an agent
func (cb *CircuitBreaker) TimeUntilReset(agentID string) time.Duration {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if openUntil, exists := cb.openUntil[agentID]; exists {
		remaining := time.Until(openUntil)
		if remaining > 0 {
			return remaining
		}
	}
	return 0
}

// GuardRails provides protection against loops and cascading failures
type GuardRails struct {
	circuitBreaker *CircuitBreaker
	selfAgentID    string // This agent's ID (to detect self-calls)

	// Configuration
	maxCallDepth        int
	allowRecursiveCalls bool
	trackCallChain      bool
	callTTL             time.Duration
}

// NewGuardRails creates a new guard rails instance
func NewGuardRails(selfAgentID string) *GuardRails {
	return &GuardRails{
		circuitBreaker:      NewCircuitBreaker(),
		selfAgentID:         selfAgentID,
		maxCallDepth:        5,  // Default max depth
		allowRecursiveCalls: false,
		trackCallChain:      true,
		callTTL:             2 * time.Minute,
	}
}

// Configure updates guard rails settings
func (g *GuardRails) Configure(maxDepth int, allowRecursive, trackChain bool, ttl time.Duration) {
	if maxDepth > 0 {
		g.maxCallDepth = maxDepth
	}
	g.allowRecursiveCalls = allowRecursive
	g.trackCallChain = trackChain
	if ttl > 0 {
		g.callTTL = ttl
	}
}

// ConfigureCircuitBreaker updates circuit breaker settings
func (g *GuardRails) ConfigureCircuitBreaker(threshold int, resetAfter, openFor time.Duration) {
	g.circuitBreaker.Configure(threshold, resetAfter, openFor)
}

// ValidateCall checks if a call to targetAgentID is allowed
func (g *GuardRails) ValidateCall(ctx *CallContext, targetAgentID string) *GuardRailError {
	// 1. Check circuit breaker first (fast fail for known-bad agents)
	if !g.circuitBreaker.CanCall(targetAgentID) {
		remaining := g.circuitBreaker.TimeUntilReset(targetAgentID)
		return &GuardRailError{
			Type:    "circuit_open",
			Message: fmt.Sprintf("circuit breaker open for agent %s, retry in %v", targetAgentID, remaining.Round(time.Second)),
			Details: map[string]interface{}{
				"agent_id":     targetAgentID,
				"retry_after":  remaining.Seconds(),
				"circuit_state": "open",
			},
		}
	}

	// 2. Check max call depth
	if ctx != nil && g.maxCallDepth > 0 && ctx.CallDepth >= g.maxCallDepth {
		return &GuardRailError{
			Type:    "max_depth_exceeded",
			Message: fmt.Sprintf("max call depth (%d) exceeded at depth %d", g.maxCallDepth, ctx.CallDepth),
			Details: map[string]interface{}{
				"current_depth": ctx.CallDepth,
				"max_depth":     g.maxCallDepth,
				"call_chain":    ctx.CallChain,
			},
		}
	}

	// 3. Check for self-call
	if targetAgentID == g.selfAgentID && !g.allowRecursiveCalls {
		return &GuardRailError{
			Type:    "self_call_blocked",
			Message: fmt.Sprintf("self-call to %s not allowed (enable allow_recursive_calls to permit)", targetAgentID),
			Details: map[string]interface{}{
				"agent_id":              targetAgentID,
				"allow_recursive_calls": g.allowRecursiveCalls,
			},
		}
	}

	// 4. Check for circular calls (target already in chain)
	if ctx != nil && g.trackCallChain {
		for _, agentID := range ctx.CallChain {
			if agentID == targetAgentID {
				return &GuardRailError{
					Type:    "circular_call_detected",
					Message: fmt.Sprintf("circular call detected: %s already in call chain", targetAgentID),
					Details: map[string]interface{}{
						"target_agent": targetAgentID,
						"call_chain":   ctx.CallChain,
						"call_depth":   ctx.CallDepth,
					},
				}
			}
		}
	}

	// 5. Check TTL expiry
	if ctx != nil && g.callTTL > 0 && !ctx.StartTime.IsZero() {
		elapsed := time.Since(ctx.StartTime)
		if elapsed > g.callTTL {
			return &GuardRailError{
				Type:    "ttl_expired",
				Message: fmt.Sprintf("call chain TTL expired after %v (limit: %v)", elapsed.Round(time.Second), g.callTTL),
				Details: map[string]interface{}{
					"elapsed":    elapsed.Seconds(),
					"ttl":        g.callTTL.Seconds(),
					"call_chain": ctx.CallChain,
				},
			}
		}
	}

	return nil
}

// RecordSuccess records a successful call
func (g *GuardRails) RecordSuccess(agentID string) {
	g.circuitBreaker.RecordSuccess(agentID)
}

// RecordFailure records a failed call
func (g *GuardRails) RecordFailure(agentID string) {
	g.circuitBreaker.RecordFailure(agentID)
}

// ExtractCallContext extracts call context from incoming request headers
func ExtractCallContext(r *http.Request) *CallContext {
	ctx := &CallContext{
		CallChain: []string{},
		CallDepth: 0,
	}

	// Extract call chain
	if chainHeader := r.Header.Get(HeaderCallChain); chainHeader != "" {
		var chain []string
		if err := json.Unmarshal([]byte(chainHeader), &chain); err == nil {
			ctx.CallChain = chain
		}
	}

	// Extract call depth
	if depthHeader := r.Header.Get(HeaderCallDepth); depthHeader != "" {
		var depth int
		if _, err := fmt.Sscanf(depthHeader, "%d", &depth); err == nil {
			ctx.CallDepth = depth
		}
	}

	// Extract origin agent
	ctx.OriginAgent = r.Header.Get(HeaderOriginAgent)

	// Extract call ID
	ctx.CallID = r.Header.Get(HeaderCallID)

	// Extract start time from header (propagated from origin)
	// If not present, this is the start of a new call chain
	if startTimeHeader := r.Header.Get(HeaderStartTime); startTimeHeader != "" {
		var startNanos int64
		if _, err := fmt.Sscanf(startTimeHeader, "%d", &startNanos); err == nil {
			ctx.StartTime = time.Unix(0, startNanos)
		} else {
			ctx.StartTime = time.Now()
		}
	} else {
		ctx.StartTime = time.Now()
	}

	return ctx
}

// AddCallContextHeaders adds call context headers to an outgoing request
func AddCallContextHeaders(req *http.Request, ctx *CallContext, currentAgentID string) {
	// Add current agent to chain
	newChain := append(ctx.CallChain, currentAgentID)
	chainJSON, _ := json.Marshal(newChain)
	req.Header.Set(HeaderCallChain, string(chainJSON))

	// Increment depth
	req.Header.Set(HeaderCallDepth, fmt.Sprintf("%d", ctx.CallDepth+1))

	// Set origin if not already set
	if ctx.OriginAgent == "" {
		req.Header.Set(HeaderOriginAgent, currentAgentID)
	} else {
		req.Header.Set(HeaderOriginAgent, ctx.OriginAgent)
	}

	// Preserve call ID or generate new one
	if ctx.CallID != "" {
		req.Header.Set(HeaderCallID, ctx.CallID)
	} else {
		req.Header.Set(HeaderCallID, fmt.Sprintf("%s-%d", currentAgentID, time.Now().UnixNano()))
	}

	// Propagate start time (preserve original start time for TTL calculation)
	if !ctx.StartTime.IsZero() {
		req.Header.Set(HeaderStartTime, fmt.Sprintf("%d", ctx.StartTime.UnixNano()))
	} else {
		req.Header.Set(HeaderStartTime, fmt.Sprintf("%d", time.Now().UnixNano()))
	}
}
