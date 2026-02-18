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

// Session cache: agentID -> *BrowserSession
var agentSessions = sync.Map{}

// Pending URL cache: agentID -> URL (set by create_operator_session, used by HandleComputerTool)
var pendingURLs = sync.Map{}

// Active browser provider
var activeProvider BrowserProvider
var providerMu sync.RWMutex

// HTTP client with timeout (used by REST commands and providers)
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// InitProvider initializes the browser provider based on config.
func InitProvider(operatorConfig *config.OperatorConfig) BrowserProvider {
	providerMu.Lock()
	defer providerMu.Unlock()

	providerName := operatorConfig.BrowserProvider
	if providerName == "" {
		providerName = "browserengine"
	}

	switch providerName {
	case "browserbase":
		if operatorConfig.Browserbase != nil && operatorConfig.Browserbase.APIKey != "" {
			activeProvider = NewBrowserbaseProvider(
				operatorConfig.Browserbase.APIKey,
				operatorConfig.Browserbase.ProjectID,
			)
		} else {
			log.Printf("⚠️  Browserbase provider selected but no API key configured, falling back to browserengine")
			activeProvider = NewBrowserEngineProvider(operatorConfig.VirtualBrowser)
		}
	case "steel":
		if operatorConfig.Steel != nil && operatorConfig.Steel.APIKey != "" {
			baseURL := operatorConfig.Steel.BaseURL
			if baseURL == "" {
				baseURL = "https://api.steel.dev"
			}
			activeProvider = NewSteelProvider(operatorConfig.Steel.APIKey, baseURL)
		} else {
			log.Printf("⚠️  Steel provider selected but no API key configured, falling back to browserengine")
			activeProvider = NewBrowserEngineProvider(operatorConfig.VirtualBrowser)
		}
	case "chrome":
		if operatorConfig.Chrome != nil && operatorConfig.Chrome.DebugURL != "" {
			activeProvider = NewChromeProvider(operatorConfig.Chrome.DebugURL)
		} else {
			log.Printf("⚠️  Chrome provider selected but no debug URL configured, falling back to browserengine")
			activeProvider = NewBrowserEngineProvider(operatorConfig.VirtualBrowser)
		}
	case "browserapi":
		if operatorConfig.BrowserAPI != nil && operatorConfig.BrowserAPI.APIKey != "" {
			baseURL := operatorConfig.BrowserAPI.BaseURL
			if baseURL == "" {
				baseURL = "https://api.browserengine.co"
			}
			activeProvider = NewBrowserAPIProvider(baseURL, operatorConfig.BrowserAPI.APIKey)
		} else {
			log.Printf("⚠️  BrowserAPI provider selected but no API key configured, falling back to browserengine")
			activeProvider = NewBrowserEngineProvider(operatorConfig.VirtualBrowser)
		}
	default: // "browserengine" or unknown
		activeProvider = NewBrowserEngineProvider(operatorConfig.VirtualBrowser)
	}

	log.Printf("🖥️  Browser provider initialized: %s", activeProvider.Name())
	return activeProvider
}

func getProvider() BrowserProvider {
	providerMu.RLock()
	defer providerMu.RUnlock()
	return activeProvider
}

// SetPendingURL stores a URL to be used when creating the next browser session
func SetPendingURL(url string) {
	cfg := config.GetConfig()
	agentID := cfg.Get().ID
	pendingURLs.Store(agentID, url)
	log.Printf("Stored pending URL for agent %s: %s", agentID[:8], url)
}

// GetPendingURL retrieves the pending URL for the current agent
func GetPendingURL() string {
	cfg := config.GetConfig()
	agentID := cfg.Get().ID
	if url, ok := pendingURLs.Load(agentID); ok {
		return url.(string)
	}
	return "about:blank"
}

// SetSessionID injects an existing session ID into the cache for reuse
func SetSessionID(agentID, sessionID string) {
	session := &BrowserSession{
		ID:       sessionID,
		Provider: "browserengine",
	}
	agentSessions.Store(agentID, session)
	log.Printf("Injected existing session %s for agent %s", sessionID, agentID[:8])
}

// InitBrowserSession initializes browser session if operator mode is enabled
func InitBrowserSession() {
	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		log.Println("Operator mode not enabled, skipping browser initialization")
		return
	}

	// Initialize the browser provider
	provider := InitProvider(operatorConfig)

	log.Printf("Operator mode enabled using provider: %s", provider.Name())

	eventBus := events.GetEventBus()
	browserEvent := events.NewEvent("browser", "operator_enabled", events.LevelInfo).
		WithData("browser_provider", provider.Name()).
		WithData("virtual_browser", operatorConfig.VirtualBrowser).
		WithData("test_mode", getTestMode(operatorConfig))
	eventBus.Publish(browserEvent)
}

// InitBrowserSessionOnEnable initializes browser session when operator mode is enabled via config
func InitBrowserSessionOnEnable(operatorConfig *config.OperatorConfig) {
	provider := InitProvider(operatorConfig)
	log.Printf("Operator mode enabled via config using provider: %s", provider.Name())
}

// getTestMode determines if test mode is enabled
func getTestMode(operatorConfig *config.OperatorConfig) bool {
	if operatorConfig.TestMode != nil {
		return *operatorConfig.TestMode
	}
	cfg := config.GetConfig()
	return cfg.Get().TestMode
}

// CreateSessionWithData creates a new session and returns both the session ID and full response data
func CreateSessionWithData(agentID string, initialURL string, proxyEnabled bool, proxyCountry string) (string, map[string]interface{}, error) {
	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		return "", nil, fmt.Errorf("operator mode is not enabled")
	}

	provider := getProvider()
	if provider == nil {
		return "", nil, fmt.Errorf("no browser provider initialized")
	}

	opts := SessionOptions{
		InitialURL:     initialURL,
		ViewportWidth:  operatorConfig.DisplayWidth,
		ViewportHeight: operatorConfig.DisplayHeight,
		Proxy:          proxyEnabled,
		ProxyCountry:   proxyCountry,
		TestMode:       getTestMode(operatorConfig),
	}

	session, err := provider.CreateSession(context.Background(), opts)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create session via %s: %w", provider.Name(), err)
	}

	// If session has a CDP connect URL, establish WebSocket connection
	if session.ConnectURL != "" {
		if err := ConnectCDP(context.Background(), session); err != nil {
			log.Printf("⚠️  CDP connection failed for session %s: %v (falling back to REST if available)", session.ID, err)
			// Don't fail — the session was created, REST fallback may work for self provider
		} else if initialURL != "" && initialURL != "about:blank" {
			// Navigate to the initial URL via CDP (providers like Steel/Browserbase open about:blank)
			log.Printf("🔧 Navigating to initial URL via CDP: %s", initialURL)
			if _, err := ExecuteCDPCommand(context.Background(), session, "navigate", map[string]interface{}{"url": initialURL}); err != nil {
				log.Printf("⚠️  CDP navigation to %s failed: %v", initialURL, err)
			} else {
				log.Printf("✅ CDP navigation to %s initiated", initialURL)
				// Brief wait for navigation to start before returning
				time.Sleep(2 * time.Second)
			}
		}
	}

	// Cache the session
	agentSessions.Store(agentID, session)

	log.Printf("Created browser session %s via %s for agent %s", session.ID, provider.Name(), agentID[:8])

	// Build response data map for backwards compatibility
	responseData := map[string]interface{}{
		"id":       session.ID,
		"provider": session.Provider,
	}
	if session.StreamURL != "" {
		responseData["stream_url"] = session.StreamURL
	}
	if session.ViewURL != "" {
		responseData["view_url"] = session.ViewURL
	}
	if session.TargetID != "" {
		responseData["target_id"] = session.TargetID
	}
	// Merge any extra metadata from provider
	for k, v := range session.Metadata {
		if _, exists := responseData[k]; !exists {
			responseData[k] = v
		}
	}

	eventBus := events.GetEventBus()
	sessionEvent := events.NewEvent("browser", "session_created", events.LevelInfo).
		WithData("agent_id", agentID).
		WithData("session_id", session.ID).
		WithData("provider", session.Provider).
		WithData("url", initialURL).
		WithData("stream_url", session.StreamURL).
		WithData("view_url", session.ViewURL).
		WithData("cdp_connected", session.IsConnectedCDP())
	eventBus.Publish(sessionEvent)

	return session.ID, responseData, nil
}

// GetOrCreateSession gets existing session or creates a new one
func GetOrCreateSession(agentID string, initialURL string, proxyEnabled bool, proxyCountry string) (string, error) {
	sessionID, _, err := CreateSessionWithData(agentID, initialURL, proxyEnabled, proxyCountry)
	return sessionID, err
}

// getSession retrieves the cached BrowserSession for an agent
func getSession(agentID string) *BrowserSession {
	val, ok := agentSessions.Load(agentID)
	if !ok {
		return nil
	}
	return val.(*BrowserSession)
}

// executeCommand routes a command to CDP or REST based on session state.
// For CDP sessions, it attempts one reconnect if the WebSocket has dropped.
func executeCommand(ctx context.Context, session *BrowserSession, cmdType string, params map[string]interface{}) (map[string]interface{}, error) {
	if session.IsConnectedCDP() {
		result, err := ExecuteCDPCommand(ctx, session, cmdType, params)
		if err != nil && session.ConnectURL != "" && (strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "websocket")) {
			// CDP WebSocket may have dropped (e.g., Browserbase keepAlive session idle disconnect).
			// Try to reconnect once before failing.
			log.Printf("⚠️  CDP command failed (%v), attempting reconnect to %s...", err, session.ID[:8])
			session.Close()
			if reconnErr := ConnectCDP(context.Background(), session); reconnErr != nil {
				log.Printf("❌ CDP reconnect failed: %v", reconnErr)
				return nil, fmt.Errorf("CDP connection lost and reconnect failed: %w (original: %v)", reconnErr, err)
			}
			log.Printf("✅ CDP reconnected to session %s, retrying command", session.ID[:8])
			return ExecuteCDPCommand(ctx, session, cmdType, params)
		}
		return result, err
	}
	return executeRESTCommand(ctx, session.ID, cmdType, params)
}

// executeRESTCommand sends a command via REST HTTP to the virtual browser service.
// If the active provider is browserapi, it routes through the API gateway instead.
func executeRESTCommand(ctx context.Context, sessionID string, cmdType string, params map[string]interface{}) (map[string]interface{}, error) {
	// If active provider is browserapi, use its own command execution (with auth + billing)
	provider := getProvider()
	if apiProvider, ok := provider.(*BrowserAPIProvider); ok {
		return apiProvider.ExecuteCommand(ctx, sessionID, cmdType, params)
	}

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

	log.Printf("🔧 ExecuteRESTCommand: POST %s with payload: %s", url, string(jsonData))

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

// ExecuteVirtualCommand executes a command in a browser session (no cancellation)
func ExecuteVirtualCommand(sessionID string, cmdType string, params map[string]interface{}) (map[string]interface{}, error) {
	return ExecuteVirtualCommandWithContext(context.Background(), sessionID, cmdType, params)
}

// ExecuteVirtualCommandWithContext executes a command with cancellation support.
// Routes to CDP or REST depending on session state.
func ExecuteVirtualCommandWithContext(ctx context.Context, sessionID string, cmdType string, params map[string]interface{}) (map[string]interface{}, error) {
	// Find session by ID
	var session *BrowserSession
	agentSessions.Range(func(key, value interface{}) bool {
		s := value.(*BrowserSession)
		if s.ID == sessionID {
			session = s
			return false
		}
		return true
	})

	if session != nil {
		return executeCommand(ctx, session, cmdType, params)
	}

	// Fallback: no cached session found, try REST directly
	return executeRESTCommand(ctx, sessionID, cmdType, params)
}

// CleanupSession cleans up a browser session
func CleanupSession(agentID string) error {
	val, ok := agentSessions.Load(agentID)
	if !ok {
		return nil // No session to cleanup
	}

	session := val.(*BrowserSession)

	// Close CDP connection first if active
	session.Close()

	// Destroy via provider
	provider := getProvider()
	if provider != nil {
		if err := provider.DestroySession(context.Background(), session.ID); err != nil {
			log.Printf("Failed to destroy session %s via provider: %v", session.ID, err)
		}
	}

	agentSessions.Delete(agentID)
	pendingURLs.Delete(agentID)

	log.Printf("Cleaned up browser session %s for agent %s", session.ID, agentID[:8])

	eventBus := events.GetEventBus()
	cleanupEvent := events.NewEvent("browser", "session_cleanup", events.LevelInfo).
		WithData("agent_id", agentID).
		WithData("session_id", session.ID).
		WithData("provider", session.Provider)
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

	// Get existing session from cache
	session := getSession(agentID)
	if session == nil {
		return nil, fmt.Errorf("no operator session found - use create_operator_session tool first")
	}
	log.Printf("🖥️ HandleComputerTool: Using session %s (%s, cdp=%v) for action %s",
		session.ID[:8], session.Provider, session.IsConnectedCDP(), action)

	var cmdType string
	var params map[string]interface{}

	switch action {
	case "screenshot":
		cmdType = "screenshot"
		params = map[string]interface{}{
			"full_page": false,
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
			_, err := executeCommand(ctx, session, "click", clickParams)
			if err != nil {
				return nil, fmt.Errorf("failed to click before typing: %w", err)
			}
		}

		cmdType = "type"
		params = map[string]interface{}{
			"text": text,
		}

	case "scroll":
		coordinate, coordOk := input["coordinate"].([]interface{})
		direction, _ := input["scroll_direction"].(string)
		amount := 100
		if scrollAmount, ok := input["scroll_amount"].(float64); ok {
			amount = int(scrollAmount)
		}

		x, y := 640, 400
		if coordOk && len(coordinate) == 2 {
			xf, _ := coordinate[0].(float64)
			yf, _ := coordinate[1].(float64)
			x = int(xf)
			y = int(yf)
		}

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
		key, ok := input["text"].(string)
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

	// Execute the command (routes to CDP or REST automatically)
	result, err := executeCommand(ctx, session, cmdType, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute browser command: %w", err)
	}

	// Handle screenshot response - convert to Anthropic format
	if action == "screenshot" {
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

	log.Printf("Computer tool executed: %s on session %s", action, session.ID)
	return result, nil
}

// HandleHighQualityScreenshot captures a high-quality screenshot with full-page support
func HandleHighQualityScreenshot(fullPage bool, quality int) (map[string]interface{}, error) {
	cfg := config.GetConfig()
	agentID := cfg.Get().ID

	session := getSession(agentID)
	if session == nil {
		return nil, fmt.Errorf("no operator session found - use create_operator_session tool first")
	}

	log.Printf("📸 High quality screenshot: session=%s, full_page=%v, quality=%d", session.ID[:8], fullPage, quality)

	params := map[string]interface{}{
		"full_page": fullPage,
		"quality":   quality,
	}

	result, err := executeCommand(context.Background(), session, "screenshot", params)
	if err != nil {
		return nil, fmt.Errorf("failed to capture high quality screenshot: %w", err)
	}

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

// EnsureBrowserSession ensures browser session is active (no-op, sessions are on-demand)
func EnsureBrowserSession(operatorConfig *config.OperatorConfig) error {
	return nil
}
