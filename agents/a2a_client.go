package agents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	a2a "github.com/apteva/agent/a2a"
	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
	"github.com/apteva/agent/tools"
	"github.com/google/uuid"
)

// callAgentA2A calls another agent via the A2A protocol (message/send).
func (c *AgentClient) callAgentA2A(ctx context.Context, agent *config.AgentInfo, message string, threadID string) (*CallResult, error) {
	startTime := time.Now()

	// Build A2A JSON-RPC request
	rpcReq := a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  "message/send",
	}

	contextID := threadID
	if contextID == "" {
		contextID = "ctx_" + uuid.New().String()[:8]
	}

	params := a2a.MessageSendParams{
		Message: a2a.Message{
			Role:      "user",
			MessageID: "msg_" + uuid.New().String()[:8],
			Parts:     []a2a.Part{{Kind: "text", Text: message}},
		},
		ContextID: contextID,
	}

	paramsJSON, _ := json.Marshal(params)
	rpcReq.Params = paramsJSON

	jsonData, err := json.Marshal(rpcReq)
	if err != nil {
		return &CallResult{
			Success:   false,
			AgentID:   agent.ID,
			AgentName: agent.Name,
			Error:     fmt.Sprintf("failed to marshal A2A request: %v", err),
		}, nil
	}

	// Get timeout
	timeout, _ := time.ParseDuration(agent.Timeout)
	if timeout == 0 {
		timeout, _ = time.ParseDuration(c.config.Settings.DefaultTimeout)
	}
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	client := &http.Client{Timeout: timeout}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", agent.URL+"/a2a", bytes.NewReader(jsonData))
	if err != nil {
		return &CallResult{
			Success:   false,
			AgentID:   agent.ID,
			AgentName: agent.Name,
			Error:     fmt.Sprintf("failed to create A2A request: %v", err),
		}, nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("A2A-Version", a2a.A2AProtocolVersion)

	// Add API key — use agent-specific key, or fall back to our own AGENT_API_KEY
	apiKey := agent.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("AGENT_API_KEY")
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	// Add call chain headers for guard rails
	callCtx := c.currentCallCtx
	if callCtx == nil {
		callCtx = &CallContext{CallChain: []string{}, CallDepth: 0, StartTime: time.Now()}
	}
	if c.config.Settings.TrackCallChain {
		AddCallContextHeaders(req, callCtx, c.selfAgentID)
	}

	// Add cancellation headers
	if c.currentRequestID != "" {
		req.Header.Set(HeaderRequestID, c.currentRequestID)
		if c.selfAgentURL != "" {
			cancelCallback := fmt.Sprintf("%s/requests/%s/cancel", c.selfAgentURL, c.currentRequestID)
			req.Header.Set(HeaderCancelCallback, cancelCallback)
		}
	}

	log.Printf("📡 A2A call to %s (%s/a2a)", agent.Name, agent.URL)

	// Make request with retry
	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt <= c.config.Settings.RetryCount; attempt++ {
		select {
		case <-ctx.Done():
			return &CallResult{
				Success:    false,
				AgentID:    agent.ID,
				AgentName:  agent.Name,
				Error:      "request cancelled",
				DurationMS: time.Since(startTime).Milliseconds(),
			}, nil
		default:
		}

		resp, lastErr = client.Do(req)
		if lastErr == nil && resp.StatusCode == 200 {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}

		if attempt < c.config.Settings.RetryCount {
			delay, _ := time.ParseDuration(c.config.Settings.RetryDelay)
			if delay == 0 {
				delay = 2 * time.Second
			}
			select {
			case <-ctx.Done():
				return &CallResult{
					Success: false, AgentID: agent.ID, AgentName: agent.Name,
					Error: "request cancelled", DurationMS: time.Since(startTime).Milliseconds(),
				}, nil
			case <-time.After(delay):
			}
		}
	}

	duration := time.Since(startTime)

	if lastErr != nil || resp == nil || resp.StatusCode != 200 {
		errorMsg := "unknown error"
		if lastErr != nil {
			errorMsg = lastErr.Error()
		} else if resp != nil {
			errorMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		if c.guardRails != nil {
			c.guardRails.RecordFailure(agent.ID)
		}
		return &CallResult{
			Success:    false,
			AgentID:    agent.ID,
			AgentName:  agent.Name,
			Error:      errorMsg,
			DurationMS: duration.Milliseconds(),
		}, nil
	}
	defer resp.Body.Close()

	// Parse JSON-RPC response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &CallResult{
			Success: false, AgentID: agent.ID, AgentName: agent.Name,
			Error: fmt.Sprintf("failed to read A2A response: %v", err), DurationMS: duration.Milliseconds(),
		}, nil
	}

	var rpcResp a2a.JSONRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return &CallResult{
			Success: false, AgentID: agent.ID, AgentName: agent.Name,
			Error: fmt.Sprintf("failed to parse A2A response: %v", err), DurationMS: duration.Milliseconds(),
		}, nil
	}

	// Check for RPC error
	if rpcResp.Error != nil {
		if c.guardRails != nil {
			c.guardRails.RecordFailure(agent.ID)
		}
		return &CallResult{
			Success: false, AgentID: agent.ID, AgentName: agent.Name,
			Error: rpcResp.Error.Message, DurationMS: duration.Milliseconds(),
		}, nil
	}

	// Extract task from result
	resultJSON, _ := json.Marshal(rpcResp.Result)
	var task a2a.Task
	if err := json.Unmarshal(resultJSON, &task); err != nil {
		return &CallResult{
			Success: false, AgentID: agent.ID, AgentName: agent.Name,
			Error: fmt.Sprintf("failed to parse A2A task: %v", err), DurationMS: duration.Milliseconds(),
		}, nil
	}

	// Extract response text from artifacts
	responseText := extractA2AResponseText(&task)

	if c.guardRails != nil {
		c.guardRails.RecordSuccess(agent.ID)
	}

	event := events.NewEvent(events.CategoryAgent, "agent_call_completed", events.LevelInfo).
		WithData("target_agent", agent.Name).
		WithData("duration_ms", duration.Milliseconds()).
		WithData("response_length", len(responseText)).
		WithData("protocol", "a2a").
		WithData("task_id", task.ID)
	c.eventBus.Publish(event)

	return &CallResult{
		Success:    true,
		AgentID:    agent.ID,
		AgentName:  agent.Name,
		Response:   responseText,
		DurationMS: duration.Milliseconds(),
	}, nil
}

// callAgentA2AStreaming calls another agent via A2A message/stream (SSE).
func (c *AgentClient) callAgentA2AStreaming(ctx context.Context, agent *config.AgentInfo, message string, threadID string, callback tools.StreamCallback) (*CallResult, error) {
	startTime := time.Now()

	// Build A2A JSON-RPC request
	rpcReq := a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  "message/stream",
	}

	streamContextID := threadID
	if streamContextID == "" {
		streamContextID = "ctx_" + uuid.New().String()[:8]
	}

	params := a2a.MessageSendParams{
		Message: a2a.Message{
			Role:      "user",
			MessageID: "msg_" + uuid.New().String()[:8],
			Parts:     []a2a.Part{{Kind: "text", Text: message}},
		},
		ContextID: streamContextID,
	}

	paramsJSON, _ := json.Marshal(params)
	rpcReq.Params = paramsJSON

	jsonData, err := json.Marshal(rpcReq)
	if err != nil {
		return &CallResult{
			Success: false, AgentID: agent.ID, AgentName: agent.Name,
			Error: fmt.Sprintf("failed to marshal A2A request: %v", err),
		}, nil
	}

	// Get timeout
	timeout, _ := time.ParseDuration(agent.Timeout)
	if timeout == 0 {
		timeout, _ = time.ParseDuration(c.config.Settings.DefaultTimeout)
	}
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	client := &http.Client{Timeout: timeout}

	callback(tools.NewProgressEvent("Connecting via A2A...", 0.1))

	req, err := http.NewRequestWithContext(ctx, "POST", agent.URL+"/a2a", bytes.NewReader(jsonData))
	if err != nil {
		return &CallResult{
			Success: false, AgentID: agent.ID, AgentName: agent.Name,
			Error: fmt.Sprintf("failed to create A2A request: %v", err),
		}, nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("A2A-Version", a2a.A2AProtocolVersion)

	streamAPIKey := agent.APIKey
	if streamAPIKey == "" {
		streamAPIKey = os.Getenv("AGENT_API_KEY")
	}
	if streamAPIKey != "" {
		req.Header.Set("X-API-Key", streamAPIKey)
	}

	callCtx := c.currentCallCtx
	if callCtx == nil {
		callCtx = &CallContext{CallChain: []string{}, CallDepth: 0, StartTime: time.Now()}
	}
	if c.config.Settings.TrackCallChain {
		AddCallContextHeaders(req, callCtx, c.selfAgentID)
	}

	if c.currentRequestID != "" {
		req.Header.Set(HeaderRequestID, c.currentRequestID)
		if c.selfAgentURL != "" {
			cancelCallback := fmt.Sprintf("%s/requests/%s/cancel", c.selfAgentURL, c.currentRequestID)
			req.Header.Set(HeaderCancelCallback, cancelCallback)
		}
	}

	log.Printf("📡 A2A stream call to %s (%s/a2a)", agent.Name, agent.URL)

	resp, err := client.Do(req)
	if err != nil {
		if c.guardRails != nil {
			c.guardRails.RecordFailure(agent.ID)
		}
		return &CallResult{
			Success: false, AgentID: agent.ID, AgentName: agent.Name,
			Error: fmt.Sprintf("A2A request failed: %v", err), DurationMS: time.Since(startTime).Milliseconds(),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if c.guardRails != nil {
			c.guardRails.RecordFailure(agent.ID)
		}
		return &CallResult{
			Success: false, AgentID: agent.ID, AgentName: agent.Name,
			Error: fmt.Sprintf("HTTP %d", resp.StatusCode), DurationMS: time.Since(startTime).Milliseconds(),
		}, nil
	}

	callback(tools.NewProgressEvent("Agent responding...", 0.2))

	// Parse A2A SSE events
	var fullResponse strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return &CallResult{
				Success: false, AgentID: agent.ID, AgentName: agent.Name,
				Response: fullResponse.String(), Error: "request cancelled",
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

		// Parse the SSE data as JSON-RPC response wrapping an A2A event
		var rpcResp a2a.JSONRPCResponse
		if err := json.Unmarshal([]byte(data), &rpcResp); err != nil {
			continue
		}

		if rpcResp.Result == nil {
			continue
		}

		// Re-marshal result to detect event kind
		resultJSON, _ := json.Marshal(rpcResp.Result)
		var rawResult map[string]interface{}
		if err := json.Unmarshal(resultJSON, &rawResult); err != nil {
			continue
		}

		kind, _ := rawResult["kind"].(string)

		switch kind {
		case "status-update":
			var statusEvent a2a.TaskStatusUpdateEvent
			if err := json.Unmarshal(resultJSON, &statusEvent); err != nil {
				continue
			}
			state := statusEvent.Status.State
			switch state {
			case a2a.TaskStateWorking:
				if statusEvent.Status.Message != nil {
					// Tool usage or progress message
					text := extractA2AMessageText(statusEvent.Status.Message)
					if text != "" {
						callback(tools.NewLogEvent(text))
					}
				}
			case a2a.TaskStateFailed:
				if statusEvent.Status.Message != nil {
					text := extractA2AMessageText(statusEvent.Status.Message)
					callback(tools.NewErrorEvent(text))
				}
			case a2a.TaskStateCompleted:
				// Final — handled after loop
			}

		case "artifact-update":
			var artifactEvent a2a.TaskArtifactUpdateEvent
			if err := json.Unmarshal(resultJSON, &artifactEvent); err != nil {
				continue
			}
			// Stream text parts
			for _, part := range artifactEvent.Artifact.Parts {
				if part.Kind == "text" && part.Text != "" {
					fullResponse.WriteString(part.Text)
					callback(tools.NewChunkEvent(part.Text))
				}
			}
		}
	}

	duration := time.Since(startTime)

	if c.guardRails != nil {
		c.guardRails.RecordSuccess(agent.ID)
	}

	callback(tools.NewProgressEvent("Complete", 1.0))

	completionEvent := events.NewEvent(events.CategoryAgent, "agent_call_completed", events.LevelInfo).
		WithData("target_agent", agent.Name).
		WithData("duration_ms", duration.Milliseconds()).
		WithData("response_length", fullResponse.Len()).
		WithData("protocol", "a2a").
		WithData("streaming", true)
	c.eventBus.Publish(completionEvent)

	return &CallResult{
		Success:    true,
		AgentID:    agent.ID,
		AgentName:  agent.Name,
		Response:   fullResponse.String(),
		DurationMS: duration.Milliseconds(),
	}, nil
}

// extractA2AResponseText extracts the full response text from an A2A Task's artifacts.
func extractA2AResponseText(task *a2a.Task) string {
	var texts []string
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			if part.Kind == "text" && part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n")
	}
	// Fallback: try status message
	if task.Status.Message != nil {
		return extractA2AMessageText(task.Status.Message)
	}
	return ""
}

// extractA2AMessageText extracts text from an A2A Message.
func extractA2AMessageText(msg *a2a.Message) string {
	var texts []string
	for _, part := range msg.Parts {
		if part.Kind == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}
