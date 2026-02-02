package tools

import "time"

// PingTool is a simple tool with no inputs for testing
type PingTool struct{}

func (t *PingTool) Name() string {
	return "ping"
}

func (t *PingTool) DisplayName() string {
	return "Ping"
}

func (t *PingTool) Description() string {
	return "Simple ping tool that returns pong with timestamp. Has no input parameters."
}

func (t *PingTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *PingTool) Execute(params map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"response":  "pong",
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil
}
