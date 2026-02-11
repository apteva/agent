package agents

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
	"github.com/apteva/agent/tools"
)

type AgentClient struct {
	config           *config.AgentsConfig
	httpClient       *http.Client
	eventBus         *events.EventBus
	db               *sql.DB
	discoveryService DiscoveryService
	guardRails       *GuardRails
	selfAgentID      string       // This agent's ID
	selfAgentURL     string       // This agent's URL (for cancel callbacks)
	currentCallCtx   *CallContext // Current incoming call context (set per-request)
	currentRequestID string       // Current request ID (for cancellation propagation)
}

// DiscoveryService interface (avoid circular import)
type DiscoveryService interface {
	GetAgents() []config.AgentInfo
	IsRunning() bool
}

// ChatRequest matches the /chat endpoint structure
type ChatRequest struct {
	Message  string `json:"message"`
	Stream   bool   `json:"stream"` // Always false for agent-to-agent
	ThreadID string `json:"thread_id,omitempty"`
}

// ChatResponse matches the /chat endpoint non-streaming response
type ChatResponse struct {
	Success  bool   `json:"success"`
	Response string `json:"response"`
	ThreadID string `json:"thread_id"`
	Model    string `json:"model,omitempty"`
	Error    string `json:"error,omitempty"`
}

type CallResult struct {
	Success    bool
	AgentID    string
	AgentName  string
	Response   string
	ThreadID   string
	DurationMS int64
	TokensUsed int
	Error      string
}

func NewAgentClient(cfg *config.AgentsConfig, eventBus *events.EventBus, db *sql.DB, selfAgentID string) *AgentClient {
	timeout, _ := time.ParseDuration(cfg.Settings.DefaultTimeout)
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	// Initialize guard rails
	guardRails := NewGuardRails(selfAgentID)

	// Configure from settings
	maxDepth := cfg.Settings.MaxCallDepth
	if maxDepth == 0 {
		maxDepth = 5 // Default
	}

	callTTL, _ := time.ParseDuration(cfg.Settings.CallTTL)
	if callTTL == 0 {
		callTTL = 2 * time.Minute // Default
	}

	guardRails.Configure(
		maxDepth,
		cfg.Settings.AllowRecursiveCalls,
		cfg.Settings.TrackCallChain,
		callTTL,
	)

	// Configure circuit breaker if settings exist
	if cfg.Settings.CircuitBreaker != nil && cfg.Settings.CircuitBreaker.Enabled {
		resetAfter, _ := time.ParseDuration(cfg.Settings.CircuitBreaker.ResetAfter)
		openFor, _ := time.ParseDuration(cfg.Settings.CircuitBreaker.OpenDuration)
		guardRails.ConfigureCircuitBreaker(
			cfg.Settings.CircuitBreaker.FailureThreshold,
			resetAfter,
			openFor,
		)
	}

	return &AgentClient{
		config:      cfg,
		eventBus:    eventBus,
		db:          db,
		httpClient:  &http.Client{Timeout: timeout},
		guardRails:  guardRails,
		selfAgentID: selfAgentID,
	}
}

func (c *AgentClient) CallAgent(agentID, message, contextType, threadID string) (*CallResult, error) {
	// Use background context for backwards compatibility
	return c.CallAgentWithContext(context.Background(), agentID, message, contextType, threadID)
}

// CallAgentWithContext calls another agent with context support for cancellation
func (c *AgentClient) CallAgentWithContext(ctx context.Context, agentID, message, contextType, threadID string) (*CallResult, error) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return &CallResult{
			Success: false,
			AgentID: agentID,
			Error:   "request cancelled",
		}, nil
	default:
	}

	// Get current call context (may be nil if this is the first call in chain)
	callCtx := c.currentCallCtx
	if callCtx == nil {
		callCtx = &CallContext{
			CallChain: []string{},
			CallDepth: 0,
			StartTime: time.Now(),
		}
	}

	// Validate call using guard rails
	if c.guardRails != nil {
		if err := c.guardRails.ValidateCall(callCtx, agentID); err != nil {
			log.Printf("🛡️ Guard rail blocked call to %s: %s", agentID, err.Message)

			// Publish blocked event
			event := events.NewEvent(events.CategoryAgent, "agent_call_blocked", events.LevelWarn).
				WithData("target_agent_id", agentID).
				WithData("block_type", err.Type).
				WithData("reason", err.Message).
				WithData("call_depth", callCtx.CallDepth).
				WithData("call_chain", callCtx.CallChain)
			c.eventBus.Publish(event)

			return &CallResult{
				Success: false,
				AgentID: agentID,
				Error:   fmt.Sprintf("GUARD_RAIL_BLOCKED: %s", err.Message),
			}, nil
		}
	}

	agent := c.findAgent(agentID)
	if agent == nil {
		return &CallResult{
			Success: false,
			AgentID: agentID,
			Error:   fmt.Sprintf("agent not found: %s", agentID),
		}, nil // Return result, not error, so tool can show this to LLM
	}

	if !agent.Enabled {
		return &CallResult{
			Success:   false,
			AgentID:   agentID,
			AgentName: agent.Name,
			Error:     fmt.Sprintf("agent is disabled: %s", agent.Name),
		}, nil
	}

	// Get agent-specific timeout if configured
	timeout, _ := time.ParseDuration(agent.Timeout)
	if timeout == 0 {
		timeout, _ = time.ParseDuration(c.config.Settings.DefaultTimeout)
	}

	// Create HTTP client with agent-specific timeout
	client := &http.Client{Timeout: timeout}

	// Publish event
	event := events.NewEvent(events.CategoryAgent, "agent_call_started", events.LevelInfo).
		WithData("target_agent_id", agent.ID).
		WithData("target_agent_name", agent.Name).
		WithData("message_length", len(message))
	c.eventBus.Publish(event)

	startTime := time.Now()

	// Wrap message with agent-to-agent instructions
	wrappedMessage := fmt.Sprintf(`You are being called by another AI agent. No human user is present.

REQUEST:
%s

INSTRUCTIONS:
1. Run autonomously - DO NOT ask clarifying questions. Make your best judgment.
2. Keep responses SHORT and direct - no verbose explanations or pleasantries.
3. Return only the essential information or result requested.
4. Use available tools if needed to complete the request.

Execute now.`, message)

	// Build chat request - ALWAYS stream: false for synchronous response
	chatReq := ChatRequest{
		Message:  wrappedMessage,
		Stream:   false, // CRITICAL: Must be false for sync response
		ThreadID: threadID,
	}

	jsonData, err := json.Marshal(chatReq)
	if err != nil {
		return &CallResult{
			Success:   false,
			AgentID:   agentID,
			AgentName: agent.Name,
			Error:     fmt.Sprintf("failed to marshal request: %v", err),
		}, nil
	}

	// Retry logic
	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt <= c.config.Settings.RetryCount; attempt++ {
		// Check cancellation before each attempt
		select {
		case <-ctx.Done():
			return &CallResult{
				Success:    false,
				AgentID:    agentID,
				AgentName:  agent.Name,
				Error:      "request cancelled",
				DurationMS: time.Since(startTime).Milliseconds(),
			}, nil
		default:
		}

		// Create request with context
		req, err := http.NewRequestWithContext(ctx, "POST", agent.URL+"/chat", bytes.NewReader(jsonData))
		if err != nil {
			lastErr = err
			continue
		}

		req.Header.Set("Content-Type", "application/json")

		// Add API key if configured
		if agent.APIKey != "" {
			req.Header.Set("X-Agent-Key", agent.APIKey)
		}

		// Add call chain headers for loop detection
		if c.config.Settings.TrackCallChain {
			AddCallContextHeaders(req, callCtx, c.selfAgentID)
		}

		// Add cancellation headers for propagation
		if c.currentRequestID != "" {
			req.Header.Set(HeaderRequestID, c.currentRequestID)
			if c.selfAgentURL != "" {
				cancelCallback := fmt.Sprintf("%s/requests/%s/cancel", c.selfAgentURL, c.currentRequestID)
				req.Header.Set(HeaderCancelCallback, cancelCallback)
			}
		}

		// Make request
		resp, lastErr = client.Do(req)

		// Check if cancelled during request
		if ctx.Err() != nil {
			if resp != nil {
				resp.Body.Close()
			}
			return &CallResult{
				Success:    false,
				AgentID:    agentID,
				AgentName:  agent.Name,
				Error:      "request cancelled",
				DurationMS: time.Since(startTime).Milliseconds(),
			}, nil
		}

		// Success
		if lastErr == nil && resp.StatusCode == 200 {
			break
		}

		// Close failed response body
		if resp != nil {
			resp.Body.Close()
		}

		// Retry delay (with cancellation check)
		if attempt < c.config.Settings.RetryCount {
			delay, _ := time.ParseDuration(c.config.Settings.RetryDelay)
			if delay == 0 {
				delay = 2 * time.Second
			}

			select {
			case <-ctx.Done():
				return &CallResult{
					Success:    false,
					AgentID:    agentID,
					AgentName:  agent.Name,
					Error:      "request cancelled",
					DurationMS: time.Since(startTime).Milliseconds(),
				}, nil
			case <-time.After(delay):
			}

			// Publish retry event
			retryEvent := events.NewEvent(events.CategoryAgent, "agent_call_retry", events.LevelWarn).
				WithData("target_agent", agent.Name).
				WithData("attempt", attempt+2).
				WithData("error", fmt.Sprintf("%v", lastErr))
			c.eventBus.Publish(retryEvent)
		}
	}

	duration := time.Since(startTime)

	// Handle final failure
	if lastErr != nil || resp == nil || resp.StatusCode != 200 {
		errorMsg := "unknown error"
		if lastErr != nil {
			errorMsg = lastErr.Error()
		} else if resp != nil {
			errorMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}

		// Record failure for circuit breaker
		if c.guardRails != nil {
			c.guardRails.RecordFailure(agentID)
		}

		event := events.NewEvent(events.CategoryAgent, "agent_call_failed", events.LevelError).
			WithData("target_agent", agent.Name).
			WithData("duration_ms", duration.Milliseconds()).
			WithData("error", errorMsg).
			WithData("circuit_state", c.getCircuitState(agentID))
		c.eventBus.Publish(event)

		return &CallResult{
			Success:    false,
			AgentID:    agentID,
			AgentName:  agent.Name,
			Error:      errorMsg,
			DurationMS: duration.Milliseconds(),
		}, nil
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &CallResult{
			Success:    false,
			AgentID:    agentID,
			AgentName:  agent.Name,
			Error:      fmt.Sprintf("failed to read response: %v", err),
			DurationMS: duration.Milliseconds(),
		}, nil
	}

	// Parse chat response
	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return &CallResult{
			Success:    false,
			AgentID:    agentID,
			AgentName:  agent.Name,
			Error:      fmt.Sprintf("failed to parse response: %v", err),
			DurationMS: duration.Milliseconds(),
		}, nil
	}

	// Check if chat endpoint returned error
	if !chatResp.Success {
		errorMsg := chatResp.Error
		if errorMsg == "" {
			errorMsg = "agent returned success=false"
		}

		// Record failure for circuit breaker
		if c.guardRails != nil {
			c.guardRails.RecordFailure(agentID)
		}

		return &CallResult{
			Success:    false,
			AgentID:    agentID,
			AgentName:  agent.Name,
			Error:      errorMsg,
			DurationMS: duration.Milliseconds(),
		}, nil
	}

	// Success! Record for circuit breaker
	if c.guardRails != nil {
		c.guardRails.RecordSuccess(agentID)
	}
	event = events.NewEvent(events.CategoryAgent, "agent_call_completed", events.LevelInfo).
		WithData("target_agent", agent.Name).
		WithData("duration_ms", duration.Milliseconds()).
		WithData("response_length", len(chatResp.Response)).
		WithData("thread_id", chatResp.ThreadID)
	c.eventBus.Publish(event)

	return &CallResult{
		Success:    true,
		AgentID:    agentID,
		AgentName:  agent.Name,
		Response:   chatResp.Response,
		ThreadID:   chatResp.ThreadID,
		DurationMS: duration.Milliseconds(),
		TokensUsed: 0, // Chat endpoint doesn't return this yet
	}, nil
}

// CallAgentStreaming calls another agent with streaming support
// The callback is called for each chunk of the response
func (c *AgentClient) CallAgentStreaming(agentID, message, contextType, threadID string, callback tools.StreamCallback) (*CallResult, error) {
	return c.CallAgentStreamingWithContext(context.Background(), agentID, message, contextType, threadID, callback)
}

// CallAgentStreamingWithContext calls another agent with streaming and context support
func (c *AgentClient) CallAgentStreamingWithContext(ctx context.Context, agentID, message, contextType, threadID string, callback tools.StreamCallback) (*CallResult, error) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return &CallResult{
			Success: false,
			AgentID: agentID,
			Error:   "request cancelled",
		}, nil
	default:
	}

	// Get current call context
	callCtx := c.currentCallCtx
	if callCtx == nil {
		callCtx = &CallContext{
			CallChain: []string{},
			CallDepth: 0,
			StartTime: time.Now(),
		}
	}

	// Validate call using guard rails
	if c.guardRails != nil {
		if err := c.guardRails.ValidateCall(callCtx, agentID); err != nil {
			log.Printf("🛡️ Guard rail blocked call to %s: %s", agentID, err.Message)
			return &CallResult{
				Success: false,
				AgentID: agentID,
				Error:   fmt.Sprintf("GUARD_RAIL_BLOCKED: %s", err.Message),
			}, nil
		}
	}

	agent := c.findAgent(agentID)
	if agent == nil {
		return &CallResult{
			Success: false,
			AgentID: agentID,
			Error:   fmt.Sprintf("agent not found: %s", agentID),
		}, nil
	}

	if !agent.Enabled {
		return &CallResult{
			Success:   false,
			AgentID:   agentID,
			AgentName: agent.Name,
			Error:     fmt.Sprintf("agent is disabled: %s", agent.Name),
		}, nil
	}

	// Send progress event
	callback(tools.NewProgressEvent("Connecting to agent...", 0.1))

	// Get agent-specific timeout
	timeout, _ := time.ParseDuration(agent.Timeout)
	if timeout == 0 {
		timeout, _ = time.ParseDuration(c.config.Settings.DefaultTimeout)
	}
	if timeout == 0 {
		timeout = 5 * time.Minute // Longer timeout for streaming
	}

	client := &http.Client{Timeout: timeout}

	// Publish event
	event := events.NewEvent(events.CategoryAgent, "agent_call_started", events.LevelInfo).
		WithData("target_agent_id", agent.ID).
		WithData("target_agent_name", agent.Name).
		WithData("message_length", len(message)).
		WithData("streaming", true)
	c.eventBus.Publish(event)

	startTime := time.Now()

	// Wrap message with agent-to-agent instructions
	wrappedMessage := fmt.Sprintf(`You are being called by another AI agent. No human user is present.

REQUEST:
%s

INSTRUCTIONS:
1. Run autonomously - DO NOT ask clarifying questions. Make your best judgment.
2. Keep responses SHORT and direct - no verbose explanations or pleasantries.
3. Return only the essential information or result requested.
4. Use available tools if needed to complete the request.

Execute now.`, message)

	// Build chat request - stream: true for SSE response
	chatReq := ChatRequest{
		Message:  wrappedMessage,
		Stream:   true, // STREAMING MODE
		ThreadID: threadID,
	}

	jsonData, err := json.Marshal(chatReq)
	if err != nil {
		return &CallResult{
			Success:   false,
			AgentID:   agentID,
			AgentName: agent.Name,
			Error:     fmt.Sprintf("failed to marshal request: %v", err),
		}, nil
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "POST", agent.URL+"/chat", bytes.NewReader(jsonData))
	if err != nil {
		return &CallResult{
			Success:   false,
			AgentID:   agentID,
			AgentName: agent.Name,
			Error:     fmt.Sprintf("failed to create request: %v", err),
		}, nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Add API key if configured
	if agent.APIKey != "" {
		req.Header.Set("X-Agent-Key", agent.APIKey)
	}

	// Add call chain headers
	if c.config.Settings.TrackCallChain {
		AddCallContextHeaders(req, callCtx, c.selfAgentID)
	}

	// Add cancellation headers for propagation (same as non-streaming)
	if c.currentRequestID != "" {
		req.Header.Set(HeaderRequestID, c.currentRequestID)
		if c.selfAgentURL != "" {
			cancelCallback := fmt.Sprintf("%s/requests/%s/cancel", c.selfAgentURL, c.currentRequestID)
			req.Header.Set(HeaderCancelCallback, cancelCallback)
		}
	}

	callback(tools.NewProgressEvent("Agent responding...", 0.2))

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		if c.guardRails != nil {
			c.guardRails.RecordFailure(agentID)
		}
		return &CallResult{
			Success:    false,
			AgentID:    agentID,
			AgentName:  agent.Name,
			Error:      fmt.Sprintf("request failed: %v", err),
			DurationMS: time.Since(startTime).Milliseconds(),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if c.guardRails != nil {
			c.guardRails.RecordFailure(agentID)
		}
		return &CallResult{
			Success:    false,
			AgentID:    agentID,
			AgentName:  agent.Name,
			Error:      fmt.Sprintf("HTTP %d", resp.StatusCode),
			DurationMS: time.Since(startTime).Milliseconds(),
		}, nil
	}

	// Parse SSE stream
	var fullResponse strings.Builder
	var respThreadID string
	scanner := bufio.NewScanner(resp.Body)

	// Increase buffer size for large responses
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return &CallResult{
				Success:    false,
				AgentID:    agentID,
				AgentName:  agent.Name,
				Response:   fullResponse.String(),
				Error:      "request cancelled",
				DurationMS: time.Since(startTime).Milliseconds(),
			}, nil
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)

		switch eventType {
		case "content":
			// Stream content chunk to callback
			content, _ := event["content"].(string)
			fullResponse.WriteString(content)
			callback(tools.NewChunkEvent(content))

		case "thread_id":
			// Capture thread ID
			respThreadID, _ = event["thread_id"].(string)

		case "tool_use", "tool_call":
			// Agent is using a tool - send log event
			toolName, _ := event["tool_name"].(string)
			callback(tools.NewLogEvent(fmt.Sprintf("Agent using tool: %s", toolName)))

		case "tool_result":
			// Tool completed
			callback(tools.NewLogEvent("Tool completed"))

		case "stop", "end":
			// Stream complete
			break

		case "error":
			// Error occurred
			errorMsg, _ := event["error"].(string)
			if errorMsg == "" {
				errorMsg, _ = event["content"].(string)
			}
			callback(tools.NewErrorEvent(errorMsg))
		}
	}

	duration := time.Since(startTime)

	// Record success
	if c.guardRails != nil {
		c.guardRails.RecordSuccess(agentID)
	}

	callback(tools.NewProgressEvent("Complete", 1.0))

	// Publish completion event
	completionEvent := events.NewEvent(events.CategoryAgent, "agent_call_completed", events.LevelInfo).
		WithData("target_agent", agent.Name).
		WithData("duration_ms", duration.Milliseconds()).
		WithData("response_length", fullResponse.Len()).
		WithData("thread_id", respThreadID).
		WithData("streaming", true)
	c.eventBus.Publish(completionEvent)

	return &CallResult{
		Success:    true,
		AgentID:    agentID,
		AgentName:  agent.Name,
		Response:   fullResponse.String(),
		ThreadID:   respThreadID,
		DurationMS: duration.Milliseconds(),
		TokensUsed: 0,
	}, nil
}

// SetDiscoveryService injects the discovery service
func (c *AgentClient) SetDiscoveryService(ds DiscoveryService) {
	c.discoveryService = ds
}

// getAgentList returns agents from discovery service or manual config
func (c *AgentClient) getAgentList() []config.AgentInfo {
	// Use discovery service if available
	if c.discoveryService != nil && c.discoveryService.IsRunning() {
		return c.discoveryService.GetAgents()
	}
	// Fall back to manual config
	return c.config.AvailableAgents
}

func (c *AgentClient) findAgent(agentID string) *config.AgentInfo {
	// Retry up to 3 times if discovery is running but agent not found
	// This handles temporary discovery failures
	maxRetries := 3
	retryDelay := 2 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		agents := c.getAgentList()
		for i := range agents {
			if agents[i].ID == agentID {
				return &agents[i]
			}
		}

		// If discovery is running and we didn't find the agent, retry
		if c.discoveryService != nil && c.discoveryService.IsRunning() && attempt < maxRetries-1 {
			log.Printf("⚠️  Agent %s not found (attempt %d/%d), retrying in %v...", agentID, attempt+1, maxRetries, retryDelay)
			time.Sleep(retryDelay)
			continue
		}

		// No discovery or last attempt - give up
		break
	}

	return nil
}

// GetAgentName returns the human-readable name for an agent ID, or the ID itself if not found
func (c *AgentClient) GetAgentName(agentID string) string {
	agents := c.getAgentList()
	for _, agent := range agents {
		if agent.ID == agentID {
			return agent.Name
		}
	}
	return agentID
}

func (c *AgentClient) GetAvailableAgents(filterTags, filterCapabilities []string) []map[string]interface{} {
	agents := []map[string]interface{}{}

	agentList := c.getAgentList()
	for _, agent := range agentList {
		if !agent.Enabled {
			continue
		}

		// Apply tag filter
		if len(filterTags) > 0 && !hasAnyMatch(agent.Tags, filterTags) {
			continue
		}

		// Apply capability filter
		if len(filterCapabilities) > 0 && !hasAnyMatch(agent.Capabilities, filterCapabilities) {
			continue
		}

		agents = append(agents, map[string]interface{}{
			"id":           agent.ID,
			"name":         agent.Name,
			"description":  agent.Description,
			"capabilities": agent.Capabilities,
			"tags":         agent.Tags,
			"features":     agent.Features,
			"status":       "available",
		})
	}

	return agents
}

func hasAnyMatch(agentItems, filterItems []string) bool {
	for _, filter := range filterItems {
		for _, item := range agentItems {
			if item == filter {
				return true
			}
		}
	}
	return false
}

// getCircuitState returns the circuit breaker state for an agent
func (c *AgentClient) getCircuitState(agentID string) string {
	if c.guardRails != nil {
		return c.guardRails.circuitBreaker.GetState(agentID)
	}
	return "unknown"
}

// SetCallContext sets the call context for the current request
// This should be called when handling an incoming agent-to-agent call
func (c *AgentClient) SetCallContext(ctx *CallContext) {
	c.currentCallCtx = ctx
}

// ClearCallContext clears the current call context
func (c *AgentClient) ClearCallContext() {
	c.currentCallCtx = nil
}

// GetGuardRails returns the guard rails instance for external use
func (c *AgentClient) GetGuardRails() *GuardRails {
	return c.guardRails
}

// SetRequestContext sets the current request ID and self URL for cancellation propagation
func (c *AgentClient) SetRequestContext(requestID, selfURL string) {
	c.currentRequestID = requestID
	c.selfAgentURL = selfURL
}

// ClearRequestContext clears the current request context
func (c *AgentClient) ClearRequestContext() {
	c.currentRequestID = ""
}

// SetSelfAgentURL sets this agent's URL for cancel callbacks
func (c *AgentClient) SetSelfAgentURL(url string) {
	c.selfAgentURL = url
}

// DelegateTask creates a task on a remote agent for async execution
func (c *AgentClient) DelegateTask(agentID, title, description string, priority int, executeAt string) (map[string]interface{}, error) {
	agent := c.findAgent(agentID)
	if agent == nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("agent not found: %s", agentID),
		}, nil
	}

	if !agent.Enabled {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("agent is disabled: %s", agent.Name),
		}, nil
	}

	// Check if agent has tasks feature enabled
	if agent.Features == nil || !agent.Features["tasks"] {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("agent %s does not have tasks feature enabled", agent.Name),
		}, nil
	}

	// Build task creation request
	taskReq := map[string]interface{}{
		"title":        title,
		"description":  description,
		"priority":     priority,
		"source":       "delegated",
		"delegator_id": c.selfAgentID,
	}
	if executeAt != "" {
		taskReq["execute_at"] = executeAt
	}

	jsonData, err := json.Marshal(taskReq)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("failed to marshal request: %v", err),
		}, nil
	}

	// POST to agent's /tasks endpoint
	req, err := http.NewRequest("POST", agent.URL+"/tasks", bytes.NewReader(jsonData))
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("failed to create request: %v", err),
		}, nil
	}

	req.Header.Set("Content-Type", "application/json")
	if agent.APIKey != "" {
		req.Header.Set("X-Agent-Key", agent.APIKey)
	}
	req.Header.Set("X-Delegator-ID", c.selfAgentID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("request failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)),
		}, nil
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("failed to parse response: %v", err),
		}, nil
	}

	// Add agent info to result
	result["agent_id"] = agentID
	result["agent_name"] = agent.Name

	return result, nil
}

// CheckDelegatedTask checks the status of a task on a remote agent
func (c *AgentClient) CheckDelegatedTask(agentID, taskID string) (map[string]interface{}, error) {
	agent := c.findAgent(agentID)
	if agent == nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("agent not found: %s", agentID),
		}, nil
	}

	if !agent.Enabled {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("agent is disabled: %s", agent.Name),
		}, nil
	}

	// GET from agent's /tasks/{id} endpoint
	req, err := http.NewRequest("GET", agent.URL+"/tasks/"+taskID, nil)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("failed to create request: %v", err),
		}, nil
	}

	if agent.APIKey != "" {
		req.Header.Set("X-Agent-Key", agent.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("request failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)),
		}, nil
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("failed to parse response: %v", err),
		}, nil
	}

	// Add agent info
	result["agent_id"] = agentID
	result["agent_name"] = agent.Name

	return result, nil
}
