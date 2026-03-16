package tools

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/operator"
)

// SetPendingURL stores a URL to be used when creating the next session
func SetPendingURL(url string) {
	operator.SetPendingURL(url)
}

// CreateOperatorSession creates a new operator session for a given URL.
// proxy: nil = use config default (true if unset), ptr to bool for explicit override
// proxyCountry: empty = use config default, non-empty = override
func CreateOperatorSession(url, name string, proxy *bool, proxyCountry string) map[string]interface{} {
	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		return map[string]interface{}{
			"success": false,
			"error":   "Operator mode is not enabled",
		}
	}

	if url == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "URL is required to create a new session",
		}
	}

	// Set default name if not provided
	if name == "" {
		name = fmt.Sprintf("Session for %s", url)
	}

	// Resolve proxy settings: tool param > config > default (true)
	proxyEnabled := true
	if proxy != nil {
		proxyEnabled = *proxy
	} else if operatorConfig.Proxy != nil {
		proxyEnabled = *operatorConfig.Proxy
	}

	if proxyCountry == "" {
		proxyCountry = operatorConfig.ProxyCountry
	}

	agentID := cfg.Get().ID

	// Store the URL for the computer tool to use
	operator.SetPendingURL(url)

	// Create the session on the browser service
	newSessionID, sessionData, err := operator.CreateSessionWithData(agentID, url, proxyEnabled, proxyCountry)
	if err != nil {
		log.Printf("Failed to create operator session: %v", err)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to create session: %v", err),
		}
	}

	log.Printf("Operator session created: %s for URL %s", newSessionID, url)

	// Build response with session data from browser service
	response := map[string]interface{}{
		"success":    true,
		"session_id": newSessionID,
		"url":        url,
		"name":       name,
		"status":     "active",
		"created_at": time.Now().Format(time.RFC3339),
		"message":    fmt.Sprintf("Browser session created for %s. Use computer tools to interact.", url),
	}

	// Add stream/view URLs if available from session data.
	if streamURL, ok := sessionData["stream_url"].(string); ok {
		response["stream_url"] = streamURL
	}
	if viewURL, ok := sessionData["view_url"].(string); ok {
		response["view_url"] = viewURL
	}
	if title, ok := sessionData["title"].(string); ok {
		response["title"] = title
	}
	if targetID, ok := sessionData["target_id"].(string); ok {
		response["target_id"] = targetID
	}

	return response
}

// ConnectOperatorSession connects to an existing browser session by ID.
func ConnectOperatorSession(sessionID string) map[string]interface{} {
	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		return map[string]interface{}{
			"success": false,
			"error":   "Operator mode is not enabled",
		}
	}

	if sessionID == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "session_id is required",
		}
	}

	agentID := cfg.Get().ID

	log.Printf("Connecting to existing operator session: %s", sessionID)

	// Fetch session details from provider and cache for command execution
	if err := operator.ConnectToSession(agentID, sessionID); err != nil {
		log.Printf("⚠️  ConnectToSession failed: %v", err)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to connect to session %s: %v", sessionID, err),
		}
	}

	return map[string]interface{}{
		"success":    true,
		"session_id": sessionID,
		"status":     "connected",
		"message":    fmt.Sprintf("Connected to existing session %s. Use computer tools to interact.", sessionID),
	}
}

// ListOperatorSessions lists all active operator sessions from the provider
func ListOperatorSessions(status string) map[string]interface{} {
	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		return map[string]interface{}{
			"success": false,
			"error":   "Operator mode is not enabled",
		}
	}

	// Try to list from the provider
	sessions, err := operator.ListProviderSessions()
	if err != nil {
		log.Printf("⚠️  ListProviderSessions failed: %v", err)
		return map[string]interface{}{
			"success":  true,
			"sessions": []interface{}{},
			"count":    0,
			"message":  fmt.Sprintf("Could not list sessions: %v", err),
		}
	}

	// Map tool-level status to API statuses
	// API returns: RUNNING, COMPLETED, TIMED_OUT, ERROR
	// Tool enum: active (→ RUNNING), closed (→ COMPLETED, TIMED_OUT), all
	statusMatch := func(apiStatus string) bool {
		if status == "" || status == "all" {
			return true
		}
		switch status {
		case "active":
			return apiStatus == "RUNNING"
		case "closed":
			return apiStatus == "COMPLETED" || apiStatus == "TIMED_OUT" || apiStatus == "CLOSED"
		default:
			return strings.EqualFold(apiStatus, status)
		}
	}

	var filtered []interface{}
	for _, s := range sessions {
		if !statusMatch(s.Status) {
			continue
		}
		filtered = append(filtered, map[string]interface{}{
			"id":         s.ID,
			"status":     s.Status,
			"url":        s.URL,
			"stream_url": s.StreamURL,
			"debug_url":  s.DebugURL,
			"created_at": s.CreatedAt,
		})
	}

	if filtered == nil {
		filtered = []interface{}{}
	}

	return map[string]interface{}{
		"success":  true,
		"sessions": filtered,
		"count":    len(filtered),
		"message":  fmt.Sprintf("Found %d sessions. Use connect_operator_session to connect to an existing session.", len(filtered)),
	}
}

// CloseOperatorSession closes an operator session
func CloseOperatorSession(sessionID string) map[string]interface{} {
	cfg := config.GetConfig()
	operatorConfig := cfg.Get().Operator

	if operatorConfig == nil || !operatorConfig.Enabled {
		return map[string]interface{}{
			"success": false,
			"error":   "Operator mode is not enabled",
		}
	}

	if sessionID == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "Session ID is required",
		}
	}

	// Cleanup the session via operator package
	agentID := cfg.Get().ID
	err := operator.CleanupSession(agentID)
	if err != nil {
		log.Printf("Failed to cleanup session: %v", err)
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to cleanup session: %v", err),
		}
	}

	log.Printf("Operator session closed: %s", sessionID)

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Session %s has been closed", sessionID),
	}
}

// Tool wrappers for registration

// CreateOperatorSessionToolWrapper wraps the create operator session function
type CreateOperatorSessionToolWrapper struct{}

func (t *CreateOperatorSessionToolWrapper) Name() string {
	return "create_operator_session"
}

func (t *CreateOperatorSessionToolWrapper) DisplayName() string {
	return "Create Browser Session"
}

func (t *CreateOperatorSessionToolWrapper) Description() string {
	return "Create a new browser session for a given URL. Use this to open a new browser and navigate to a website."
}

func (t *CreateOperatorSessionToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to navigate to",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Optional name for the session",
			},
			"proxy": map[string]interface{}{
				"type":        "boolean",
				"description": "Enable proxy for this session. Defaults to true. Set to false to disable proxy.",
			},
			"proxy_country": map[string]interface{}{
				"type":        "string",
				"description": "ISO 3166-1 alpha-2 country code for proxy geolocation (e.g., \"US\", \"GB\", \"DE\", \"JP\").",
			},
		},
		"required": []string{"url"},
	}
}

func (t *CreateOperatorSessionToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	url, _ := params["url"].(string)
	name, _ := params["name"].(string)
	proxyCountry, _ := params["proxy_country"].(string)

	var proxy *bool
	if p, ok := params["proxy"].(bool); ok {
		proxy = &p
	}

	return CreateOperatorSession(url, name, proxy, proxyCountry), nil
}

// ConnectOperatorSessionToolWrapper wraps the connect operator session function
type ConnectOperatorSessionToolWrapper struct{}

func (t *ConnectOperatorSessionToolWrapper) Name() string {
	return "connect_operator_session"
}

func (t *ConnectOperatorSessionToolWrapper) DisplayName() string {
	return "Connect to Browser Session"
}

func (t *ConnectOperatorSessionToolWrapper) Description() string {
	return "Connect to an existing browser session by ID. Use this to take over or resume a previously created session. Use list_operator_sessions first to find available sessions."
}

func (t *ConnectOperatorSessionToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "The ID of the existing session to connect to",
			},
		},
		"required": []string{"session_id"},
	}
}

func (t *ConnectOperatorSessionToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	sessionID, _ := params["session_id"].(string)
	return ConnectOperatorSession(sessionID), nil
}

// ListOperatorSessionsToolWrapper wraps the list operator sessions function
type ListOperatorSessionsToolWrapper struct{}

func (t *ListOperatorSessionsToolWrapper) Name() string {
	return "list_operator_sessions"
}

func (t *ListOperatorSessionsToolWrapper) DisplayName() string {
	return "List Browser Sessions"
}

func (t *ListOperatorSessionsToolWrapper) Description() string {
	return "List browser sessions filtered by status. Returns session IDs that can be used with connect_operator_session."
}

func (t *ListOperatorSessionsToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"status": map[string]interface{}{
				"type":        "string",
				"description": "Filter sessions by status: 'active', 'closed', or 'all'",
				"enum":        []string{"active", "closed", "all"},
			},
		},
		"required": []string{"status"},
	}
}

func (t *ListOperatorSessionsToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	status, _ := params["status"].(string)
	return ListOperatorSessions(status), nil
}

// CloseOperatorSessionToolWrapper wraps the close operator session function
type CloseOperatorSessionToolWrapper struct{}

func (t *CloseOperatorSessionToolWrapper) Name() string {
	return "close_operator_session"
}

func (t *CloseOperatorSessionToolWrapper) DisplayName() string {
	return "Close Browser Session"
}

func (t *CloseOperatorSessionToolWrapper) Description() string {
	return "Close an active browser session"
}

func (t *CloseOperatorSessionToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "The ID of the session to close",
			},
		},
		"required": []string{"session_id"},
	}
}

func (t *CloseOperatorSessionToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	sessionID, _ := params["session_id"].(string)
	return CloseOperatorSession(sessionID), nil
}

// HighQualityScreenshotToolWrapper wraps the high quality screenshot function
type HighQualityScreenshotToolWrapper struct{}

func (t *HighQualityScreenshotToolWrapper) Name() string {
	return "high_quality_screenshot"
}

func (t *HighQualityScreenshotToolWrapper) DisplayName() string {
	return "Capture Screenshot"
}

func (t *HighQualityScreenshotToolWrapper) Description() string {
	return "Capture a high-quality screenshot of the current browser session. Use this when you need to save or analyze page content in detail. Supports full-page captures and quality settings. Results are automatically saved when filesystem is enabled."
}

func (t *HighQualityScreenshotToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"full_page": map[string]interface{}{
				"type":        "boolean",
				"description": "Capture entire scrollable page (true) or just visible viewport (false). Default: false",
				"default":     false,
			},
			"quality": map[string]interface{}{
				"type":        "integer",
				"description": "Image quality 1-100. Higher quality means larger file size. Default: 95",
				"default":     95,
				"minimum":     1,
				"maximum":     100,
			},
		},
	}
}

func (t *HighQualityScreenshotToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	fullPage := false
	if fp, ok := params["full_page"].(bool); ok {
		fullPage = fp
	}

	quality := 95
	if q, ok := params["quality"].(float64); ok {
		quality = int(q)
	}

	return operator.HandleHighQualityScreenshot(fullPage, quality)
}

// RegisterOperatorSessionTools registers all operator session tools
func RegisterOperatorSessionTools() {
	registry := GetGlobalRegistry()
	registry.RegisterTool(&CreateOperatorSessionToolWrapper{})
	registry.RegisterTool(&ConnectOperatorSessionToolWrapper{})
	registry.RegisterTool(&ListOperatorSessionsToolWrapper{})
	registry.RegisterTool(&CloseOperatorSessionToolWrapper{})
	registry.RegisterTool(&HighQualityScreenshotToolWrapper{})
}
