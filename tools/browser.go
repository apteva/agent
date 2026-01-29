package tools

import (
	"fmt"
	"log"

	"agent-server/operator"
)

// BrowserTool provides browser control for non-Claude models with vision capabilities.
// This is equivalent to Claude's built-in "computer" tool but works with any model.
type BrowserTool struct{}

func (t *BrowserTool) Name() string {
	return "browser"
}

func (t *BrowserTool) DisplayName() string {
	return "Browser Control"
}

func (t *BrowserTool) Description() string {
	return `Control a web browser to interact with web pages. Use this tool to automate browser actions.

WORKFLOW:
1. First use create_operator_session to open a browser with a URL
2. Use action "screenshot" to see the current screen
3. Analyze the screenshot to identify coordinates of elements
4. Use actions like "click", "type", "scroll" to interact
5. Take another screenshot to verify your action worked
6. Repeat until task is complete

IMPORTANT: Coordinates (x, y) are in pixels from the top-left corner of the viewport.`
}

func (t *BrowserTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type": "string",
				"enum": []string{
					"screenshot",
					"click", "left_click", "right_click", "middle_click", "double_click", "triple_click",
					"type",
					"key",
					"scroll",
					"mouse_move",
					"left_click_drag",
					"wait",
				},
				"description": "The browser action to perform",
			},
			"coordinate": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "integer"},
				"description": "Click/scroll coordinates as [x, y] pixels from top-left",
			},
			"start_coordinate": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "integer"},
				"description": "Start coordinates for drag actions as [x, y]",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to type, or key to press (for 'type' and 'key' actions)",
			},
			"scroll_direction": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"up", "down", "left", "right"},
				"description": "Scroll direction",
			},
			"scroll_amount": map[string]interface{}{
				"type":        "integer",
				"description": "Scroll amount in pixels (default: 100)",
			},
			"duration": map[string]interface{}{
				"type":        "integer",
				"description": "Wait duration in milliseconds (for 'wait' action)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *BrowserTool) Execute(params map[string]interface{}) (interface{}, error) {
	action, ok := params["action"].(string)
	if !ok || action == "" {
		return nil, fmt.Errorf("action parameter is required")
	}

	log.Printf("🌐 BrowserTool executing action: %s", action)

	// Map "click" to "left_click" for convenience
	if action == "click" {
		params["action"] = "left_click"
	}

	// Delegate to operator.HandleComputerTool which already handles all the logic
	result, err := operator.HandleComputerTool(params)
	if err != nil {
		return nil, fmt.Errorf("browser action failed: %w", err)
	}

	return result, nil
}

// RegisterBrowserTool registers the browser tool in the global registry
// Called from registry.go init() to ensure proper initialization order
func RegisterBrowserTool() {
	registry := GetGlobalRegistry()
	registry.RegisterTool(&BrowserTool{})
}
