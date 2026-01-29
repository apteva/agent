package tools

import (
	"fmt"
	"time"
)

// WaitToolWrapper implements a general-purpose wait/sleep tool
type WaitToolWrapper struct{}

func (t *WaitToolWrapper) Name() string {
	return "wait"
}

func (t *WaitToolWrapper) DisplayName() string {
	return "Wait"
}

func (t *WaitToolWrapper) Description() string {
	return "Wait for async operations to complete before checking their status. Use ONLY when polling for results from async tools like file uploads, video processing, or external API jobs that return a status URL. Do NOT use for arbitrary delays or rate limiting - only when you need to check if an async operation has finished."
}

func (t *WaitToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"duration": map[string]interface{}{
				"type":        "string",
				"description": "Duration to pause in Go format: '5s' (seconds), '30s', '2m' (minutes). Maximum: 5 minutes. Minimum: 100ms.",
			},
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Optional reason for waiting (for logging/debugging)",
			},
		},
		"required": []string{"duration"},
	}
}

func (t *WaitToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	durationStr, ok := params["duration"].(string)
	if !ok || durationStr == "" {
		return nil, fmt.Errorf("duration is required")
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return nil, fmt.Errorf("invalid duration format: %w (use format like '5s', '2m', '1h')", err)
	}

	// Safety limit: max 5 minutes to prevent hanging conversations
	maxDuration := 5 * time.Minute
	if duration > maxDuration {
		return nil, fmt.Errorf("duration %s exceeds maximum allowed duration of %s", duration, maxDuration)
	}

	if duration < 0 {
		return nil, fmt.Errorf("duration must be positive")
	}

	// Minimum duration: 100ms (prevent accidental zero or too-short waits)
	minDuration := 100 * time.Millisecond
	if duration < minDuration {
		return nil, fmt.Errorf("duration must be at least %s", minDuration)
	}

	// Get optional reason
	reason := ""
	if reasonVal, ok := params["reason"].(string); ok {
		reason = reasonVal
	}

	// Perform the wait
	startTime := time.Now()
	time.Sleep(duration)
	actualDuration := time.Since(startTime)

	response := map[string]interface{}{
		"success":  true,
		"waited":   actualDuration.String(),
		"message":  fmt.Sprintf("Waited for %s", actualDuration.String()),
		"start_at": startTime.Format(time.RFC3339),
		"end_at":   time.Now().Format(time.RFC3339),
	}

	if reason != "" {
		response["reason"] = reason
	}

	return response, nil
}

// RegisterWaitTool registers the wait tool in the global registry
func RegisterWaitTool() {
	registry := GetGlobalRegistry()
	registry.RegisterTool(&WaitToolWrapper{})
}
