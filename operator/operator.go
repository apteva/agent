package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
)

// Session cache: agentID -> sessionID
var agentSessions = sync.Map{}

// Pending URL cache: agentID -> URL (set by create_operator_session, used by HandleComputerTool)
var pendingURLs = sync.Map{}

// HTTP client with timeout
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// SetPendingURL stores a URL to be used when creating the next browser session
func SetPendingURL(url string) {
	cfg := config.GetConfig()
	agentID := cfg.Get().ID
	pendingURLs.Store(agentID, url)
	log.Printf("Stored pending URL for agent %s: %s", agentID[:8], url)
}

// GetPendingURL retrieves and clears the pending URL for the current agent
func GetPendingURL() string {
	cfg := config.GetConfig()
	agentID := cfg.Get().ID
	if url, ok := pendingURLs.Load(agentID); ok {
		// Don't delete - keep URL for session reuse
		return url.(string)
	}
	return "about:blank"
}

// SetSessionID injects an existing session ID into the cache for reuse
func SetSessionID(agentID, sessionID string) {
	agentSessions.Store(agentID, sessionID)
	log.Printf("Injected existing session %s for agent %s", sessionID, agentID[:8])
}

// InitBrowserSession initializes browser session if operator mode is enabled
// In virtual browser mode, sessions are created on-demand, so this is a no-op
func InitBrowserSession() {
	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		log.Println("Operator mode not enabled, skipping browser initialization")
		return
	}

	log.Printf("Operator mode enabled using virtual browser: %s", operatorConfig.VirtualBrowser)

	eventBus := events.GetEventBus()
	browserEvent := events.NewEvent("browser", "operator_enabled", events.LevelInfo).
		WithData("virtual_browser", operatorConfig.VirtualBrowser).
		WithData("test_mode", getTestMode(operatorConfig))
	eventBus.Publish(browserEvent)
}

// InitBrowserSessionOnEnable initializes browser session when operator mode is enabled via config
func InitBrowserSessionOnEnable(operatorConfig *config.OperatorConfig) {
	log.Printf("Operator mode enabled via config using virtual browser: %s", operatorConfig.VirtualBrowser)
}

// getTestMode determines if test mode is enabled
func getTestMode(operatorConfig *config.OperatorConfig) bool {
	if operatorConfig.TestMode != nil {
		return *operatorConfig.TestMode
	}
	// Fallback to global test mode
	cfg := config.GetConfig()
	return cfg.Get().TestMode
}

// CreateSessionWithData creates a new session and returns both the session ID and full response data
func CreateSessionWithData(agentID string, initialURL string) (string, map[string]interface{}, error) {
	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		return "", nil, fmt.Errorf("operator mode is not enabled")
	}

	// SESSION CACHE DISABLED - Always create new session
	// To re-enable caching, uncomment this block:
	/*
	if sessionID, ok := agentSessions.Load(agentID); ok {
		// Return existing session (we don't have the full data, but session exists)
		return sessionID.(string), map[string]interface{}{
			"id":     sessionID.(string),
			"status": "active",
		}, nil
	}
	*/

	// Create new session
	sessionData := map[string]interface{}{
		"initial_url": initialURL,
		"test_mode":   getTestMode(operatorConfig),
		"proxy":       true, // Always use proxy for browser sessions
	}

	// Add viewport if configured
	if operatorConfig.DisplayWidth > 0 && operatorConfig.DisplayHeight > 0 {
		sessionData["viewport"] = map[string]interface{}{
			"width":  operatorConfig.DisplayWidth,
			"height": operatorConfig.DisplayHeight,
		}
	}

	jsonData, err := json.Marshal(sessionData)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal session data: %w", err)
	}

	url := fmt.Sprintf("%s/sessions", operatorConfig.VirtualBrowser)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return "", nil, fmt.Errorf("failed to create session request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	log.Printf("Creating virtual browser session at %s with data: %s", url, string(jsonData))

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to make session request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read session response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("virtual browser returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse session response: %w", err)
	}

	// Virtual browser returns session ID in "id" field
	sessionID, ok := result["id"].(string)
	if !ok {
		return "", nil, fmt.Errorf("invalid session id in response")
	}

	// Cache session ID
	agentSessions.Store(agentID, sessionID)

	log.Printf("Created virtual browser session %s for agent %s", sessionID, agentID[:8])

	eventBus := events.GetEventBus()
	sessionEvent := events.NewEvent("browser", "session_created", events.LevelInfo).
		WithData("agent_id", agentID).
		WithData("session_id", sessionID).
		WithData("url", initialURL).
		WithData("stream_url", result["stream_url"]).
		WithData("view_url", result["view_url"])
	eventBus.Publish(sessionEvent)

	return sessionID, result, nil
}

// GetOrCreateSession gets existing session or creates a new one
func GetOrCreateSession(agentID string, initialURL string) (string, error) {
	sessionID, _, err := CreateSessionWithData(agentID, initialURL)
	return sessionID, err
}

// ExecuteVirtualCommand executes a command in a virtual browser session (no cancellation)
func ExecuteVirtualCommand(sessionID string, cmdType string, params map[string]interface{}) (map[string]interface{}, error) {
	return ExecuteVirtualCommandWithContext(context.Background(), sessionID, cmdType, params)
}

// ExecuteVirtualCommandWithContext executes a command in a virtual browser session with cancellation support
func ExecuteVirtualCommandWithContext(ctx context.Context, sessionID string, cmdType string, params map[string]interface{}) (map[string]interface{}, error) {
	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		return nil, fmt.Errorf("operator mode is not enabled")
	}

	commandData := map[string]interface{}{
		"type":   cmdType,
		"params": params,
	}

	jsonData, err := json.Marshal(commandData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command data: %w", err)
	}

	url := fmt.Sprintf("%s/sessions/%s/commands", operatorConfig.VirtualBrowser, sessionID)

	log.Printf("🔧 ExecuteVirtualCommand: POST %s with payload: %s", url, string(jsonData))

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create command request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("command API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read command response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse command response: %w", err)
	}

	return result, nil
}

// CleanupSession cleans up a browser session
func CleanupSession(agentID string) error {
	sessionID, ok := agentSessions.Load(agentID)
	if !ok {
		return nil // No session to cleanup
	}

	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		return nil
	}

	url := fmt.Sprintf("%s/sessions/%s", operatorConfig.VirtualBrowser, sessionID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create cleanup request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		// Don't fail on cleanup errors
		log.Printf("Failed to cleanup session %s: %v", sessionID, err)
	} else {
		resp.Body.Close()
	}

	agentSessions.Delete(agentID)
	pendingURLs.Delete(agentID) // Also cleanup pending URL to prevent memory leak

	log.Printf("Cleaned up virtual browser session %s for agent %s", sessionID, agentID[:8])

	eventBus := events.GetEventBus()
	cleanupEvent := events.NewEvent("browser", "session_cleanup", events.LevelInfo).
		WithData("agent_id", agentID).
		WithData("session_id", sessionID)
	eventBus.Publish(cleanupEvent)

	return nil
}

// HandleComputerTool handles Claude's computer tool requests (no cancellation)
func HandleComputerTool(input map[string]interface{}) (map[string]interface{}, error) {
	return HandleComputerToolWithContext(context.Background(), input)
}

// HandleComputerToolWithContext handles Claude's computer tool requests with cancellation support
func HandleComputerToolWithContext(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	cfg := config.GetConfig()
	agentID := cfg.Get().ID

	action, ok := input["action"].(string)
	if !ok {
		return nil, fmt.Errorf("missing action parameter")
	}

	// Get existing session from cache - do NOT create a new one
	// The session should have been created by create_operator_session tool
	sessionID, ok := agentSessions.Load(agentID)
	if !ok {
		return nil, fmt.Errorf("no operator session found - use create_operator_session tool first")
	}
	sessionIDStr := sessionID.(string)
	log.Printf("🖥️ HandleComputerTool: Using cached session %s for action %s", sessionIDStr[:8], action)

	var cmdType string
	var params map[string]interface{}

	switch action {
	case "screenshot":
		cmdType = "screenshot"
		params = map[string]interface{}{
			"full_page": false, // Only visible viewport
		}

	case "left_click":
		coordinate, ok := input["coordinate"].([]interface{})
		if !ok || len(coordinate) != 2 {
			return nil, fmt.Errorf("invalid coordinate parameter for left_click")
		}

		x, _ := coordinate[0].(float64)
		y, _ := coordinate[1].(float64)

		cmdType = "click"
		params = map[string]interface{}{
			"x": int(x),
			"y": int(y),
		}

	case "right_click":
		coordinate, ok := input["coordinate"].([]interface{})
		if !ok || len(coordinate) != 2 {
			return nil, fmt.Errorf("invalid coordinate parameter for right_click")
		}

		x, _ := coordinate[0].(float64)
		y, _ := coordinate[1].(float64)

		cmdType = "right_click"
		params = map[string]interface{}{
			"x": int(x),
			"y": int(y),
		}

	case "middle_click":
		coordinate, ok := input["coordinate"].([]interface{})
		if !ok || len(coordinate) != 2 {
			return nil, fmt.Errorf("invalid coordinate parameter for middle_click")
		}

		x, _ := coordinate[0].(float64)
		y, _ := coordinate[1].(float64)

		cmdType = "middle_click"
		params = map[string]interface{}{
			"x": int(x),
			"y": int(y),
		}

	case "double_click":
		coordinate, ok := input["coordinate"].([]interface{})
		if !ok || len(coordinate) != 2 {
			return nil, fmt.Errorf("invalid coordinate parameter for double_click")
		}

		x, _ := coordinate[0].(float64)
		y, _ := coordinate[1].(float64)

		cmdType = "double_click"
		params = map[string]interface{}{
			"x": int(x),
			"y": int(y),
		}

	case "triple_click":
		coordinate, ok := input["coordinate"].([]interface{})
		if !ok || len(coordinate) != 2 {
			return nil, fmt.Errorf("invalid coordinate parameter for triple_click")
		}

		x, _ := coordinate[0].(float64)
		y, _ := coordinate[1].(float64)

		cmdType = "triple_click"
		params = map[string]interface{}{
			"x": int(x),
			"y": int(y),
		}

	case "mouse_move":
		coordinate, ok := input["coordinate"].([]interface{})
		if !ok || len(coordinate) != 2 {
			return nil, fmt.Errorf("invalid coordinate parameter for mouse_move")
		}

		x, _ := coordinate[0].(float64)
		y, _ := coordinate[1].(float64)

		cmdType = "mouse_move"
		params = map[string]interface{}{
			"x": int(x),
			"y": int(y),
		}

	case "left_click_drag":
		// Claude sends start_coordinate and coordinate (end point)
		startCoord, startOk := input["start_coordinate"].([]interface{})
		endCoord, endOk := input["coordinate"].([]interface{})

		if !startOk || len(startCoord) != 2 || !endOk || len(endCoord) != 2 {
			return nil, fmt.Errorf("invalid start_coordinate or coordinate parameter for left_click_drag")
		}

		startX, _ := startCoord[0].(float64)
		startY, _ := startCoord[1].(float64)
		endX, _ := endCoord[0].(float64)
		endY, _ := endCoord[1].(float64)

		cmdType = "left_click_drag"
		params = map[string]interface{}{
			"start_x": int(startX),
			"start_y": int(startY),
			"end_x":   int(endX),
			"end_y":   int(endY),
		}

	case "left_mouse_down":
		coordinate, ok := input["coordinate"].([]interface{})
		if !ok || len(coordinate) != 2 {
			return nil, fmt.Errorf("invalid coordinate parameter for left_mouse_down")
		}

		x, _ := coordinate[0].(float64)
		y, _ := coordinate[1].(float64)

		cmdType = "mouse_down"
		params = map[string]interface{}{
			"x":      int(x),
			"y":      int(y),
			"button": "left",
		}

	case "left_mouse_up":
		coordinate, ok := input["coordinate"].([]interface{})
		if !ok || len(coordinate) != 2 {
			return nil, fmt.Errorf("invalid coordinate parameter for left_mouse_up")
		}

		x, _ := coordinate[0].(float64)
		y, _ := coordinate[1].(float64)

		cmdType = "mouse_up"
		params = map[string]interface{}{
			"x":      int(x),
			"y":      int(y),
			"button": "left",
		}

	case "cursor_position":
		// Return placeholder
		return map[string]interface{}{
			"success": true,
			"x":       0,
			"y":       0,
		}, nil

	case "wait":
		duration := 1000
		if durationParam, ok := input["duration"].(float64); ok {
			duration = int(durationParam)
		}

		time.Sleep(time.Duration(duration) * time.Millisecond)

		return map[string]interface{}{
			"success":  true,
			"duration": duration,
			"message":  fmt.Sprintf("Waited for %dms", duration),
		}, nil

	case "type":
		text, ok := input["text"].(string)
		if !ok {
			return nil, fmt.Errorf("missing text parameter for type")
		}

		// If coordinates provided, click first to focus
		if coordinate, ok := input["coordinate"].([]interface{}); ok && len(coordinate) == 2 {
			x, _ := coordinate[0].(float64)
			y, _ := coordinate[1].(float64)

			clickParams := map[string]interface{}{
				"x": int(x),
				"y": int(y),
			}

			_, err := ExecuteVirtualCommandWithContext(ctx, sessionIDStr, "click", clickParams)
			if err != nil {
				return nil, fmt.Errorf("failed to click before typing: %w", err)
			}
		}

		cmdType = "type"
		params = map[string]interface{}{
			"text": text,
		}

	case "scroll":
		// Claude sends coordinate [x, y] and scroll_direction (up/down/left/right)
		// and scroll_amount (pixels)
		coordinate, coordOk := input["coordinate"].([]interface{})
		direction, _ := input["scroll_direction"].(string)
		amount := 100 // Default scroll amount
		if scrollAmount, ok := input["scroll_amount"].(float64); ok {
			amount = int(scrollAmount)
		}

		// Default to center of screen if no coordinate
		x, y := 640, 400
		if coordOk && len(coordinate) == 2 {
			xf, _ := coordinate[0].(float64)
			yf, _ := coordinate[1].(float64)
			x = int(xf)
			y = int(yf)
		}

		// Convert direction to delta
		deltaX, deltaY := 0, 0
		switch direction {
		case "up":
			deltaY = -amount
		case "down":
			deltaY = amount
		case "left":
			deltaX = -amount
		case "right":
			deltaX = amount
		default:
			// Default to scroll down
			deltaY = amount
		}

		cmdType = "scroll"
		params = map[string]interface{}{
			"x":       x,
			"y":       y,
			"delta_x": deltaX,
			"delta_y": deltaY,
		}

	case "key":
		key, ok := input["text"].(string) // Claude sends key as "text" field
		if !ok {
			return nil, fmt.Errorf("missing key parameter")
		}

		cmdType = "key"
		params = map[string]interface{}{
			"key": key,
		}

	case "hold_key":
		key, ok := input["key"].(string)
		if !ok {
			return nil, fmt.Errorf("missing key parameter for hold_key")
		}

		params = map[string]interface{}{
			"key": key,
		}

		// Check if there's an action to perform while holding
		if actionType, ok := input["action"].(string); ok {
			params["action"] = actionType
			if coordinate, ok := input["coordinate"].([]interface{}); ok && len(coordinate) == 2 {
				x, _ := coordinate[0].(float64)
				y, _ := coordinate[1].(float64)
				params["x"] = int(x)
				params["y"] = int(y)
			}
		}

		cmdType = "hold_key"

	default:
		return nil, fmt.Errorf("unsupported computer tool action: %s", action)
	}

	// Execute the command
	result, err := ExecuteVirtualCommandWithContext(ctx, sessionIDStr, cmdType, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute browser command: %w", err)
	}

	// Handle screenshot response - convert to Anthropic format
	if action == "screenshot" {
		// Browser service returns: {"success": true, "data": {"image": "base64...", "format": "png"}}
		if data, ok := result["data"].(map[string]interface{}); ok {
			if image, ok := data["image"].(string); ok {
				mediaType := "image/png"
				if format, ok := data["format"].(string); ok && format == "jpeg" {
					mediaType = "image/jpeg"
				}
				return map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": mediaType,
						"data":       image,
					},
				}, nil
			}
		}
		// Fallback: check for legacy "screenshot" field
		if screenshot, ok := result["screenshot"].(string); ok {
			return map[string]interface{}{
				"type": "image",
				"source": map[string]interface{}{
					"type":       "base64",
					"media_type": "image/png",
					"data":       screenshot,
				},
			}, nil
		}
	}

	log.Printf("Computer tool executed: %s on session %s", action, sessionIDStr)
	return result, nil
}

// HandleHighQualityScreenshot captures a high-quality screenshot with full-page support
// This is separate from the computer tool screenshot to allow explicit quality control
// and filesystem saving. The computer tool screenshot stays fast for navigation.
func HandleHighQualityScreenshot(fullPage bool, quality int) (map[string]interface{}, error) {
	cfg := config.GetConfig()
	agentID := cfg.Get().ID

	// Get existing session from cache
	sessionID, ok := agentSessions.Load(agentID)
	if !ok {
		return nil, fmt.Errorf("no operator session found - use create_operator_session tool first")
	}
	sessionIDStr := sessionID.(string)

	log.Printf("📸 High quality screenshot: session=%s, full_page=%v, quality=%d", sessionIDStr[:8], fullPage, quality)

	// Execute screenshot with quality parameters
	params := map[string]interface{}{
		"full_page": fullPage,
		"quality":   quality,
	}

	result, err := ExecuteVirtualCommand(sessionIDStr, "screenshot", params)
	if err != nil {
		return nil, fmt.Errorf("failed to capture high quality screenshot: %w", err)
	}

	// Extract image data from browser service response
	var imageData string
	var mediaType string = "image/png"

	if data, ok := result["data"].(map[string]interface{}); ok {
		if image, ok := data["image"].(string); ok {
			imageData = image
		}
		if format, ok := data["format"].(string); ok && format == "jpeg" {
			mediaType = "image/jpeg"
		}
	}

	if imageData == "" {
		return nil, fmt.Errorf("no image data in screenshot response")
	}

	// Return in a format that indicates this should be saved to filesystem
	// The stream processor will detect "high_quality_screenshot" tool and handle saving
	return map[string]interface{}{
		"success":    true,
		"type":       "high_quality_screenshot",
		"image":      imageData,
		"media_type": mediaType,
		"full_page":  fullPage,
		"quality":    quality,
		"message":    fmt.Sprintf("High quality screenshot captured (full_page=%v, quality=%d)", fullPage, quality),
	}, nil
}

// Legacy compatibility functions (no-op or simple wrappers)

// EnsureBrowserSession ensures browser session is active (no-op in virtual browser mode)
func EnsureBrowserSession(operatorConfig *config.OperatorConfig) error {
	log.Printf("EnsureBrowserSession called (no-op in virtual browser mode)")
	return nil
}

// FindBrowserInstance - legacy function (not used in virtual browser mode)
func FindBrowserInstance(agentID string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("FindBrowserInstance is deprecated in virtual browser mode")
}

// CreateBrowserSession - legacy function (not used in virtual browser mode)
func CreateBrowserSession(browserInstanceID, url, name string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("CreateBrowserSession is deprecated in virtual browser mode - use GetOrCreateSession instead")
}

// FindBrowserSession - legacy function (not used in virtual browser mode)
func FindBrowserSession(agentID string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("FindBrowserSession is deprecated in virtual browser mode")
}

// ExecuteBrowserCommand - legacy function (not used in virtual browser mode)
func ExecuteBrowserCommand(sessionID, commandType string, commandData map[string]interface{}) (map[string]interface{}, error) {
	return nil, fmt.Errorf("ExecuteBrowserCommand is deprecated in virtual browser mode - use ExecuteVirtualCommand instead")
}
