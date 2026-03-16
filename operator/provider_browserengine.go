package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// longHTTPClient has a longer timeout for session create/resume operations
// which involve spinning up browsers, injecting cookies, and navigating.
var longHTTPClient = &http.Client{
	Timeout: 2 * time.Minute,
}

// BrowserEngineProvider implements BrowserProvider for the BrowserEngine cloud service.
// Routes through the API with authentication, billing, and usage tracking.
type BrowserEngineProvider struct {
	BaseURL string // e.g., "https://api.browserengine.co"
	APIKey  string // X-API-Key header value
}

// truncateLog truncates a string for log output
func truncateLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// NewBrowserEngineProvider creates a new BrowserEngineProvider.
func NewBrowserEngineProvider(baseURL, apiKey string) *BrowserEngineProvider {
	baseURL = strings.TrimRight(baseURL, "/")
	return &BrowserEngineProvider{
		BaseURL: baseURL,
		APIKey:  apiKey,
	}
}

func (p *BrowserEngineProvider) Name() string { return "browserengine" }

func (p *BrowserEngineProvider) CreateSession(ctx context.Context, opts SessionOptions) (*BrowserSession, error) {
	sessionData := map[string]interface{}{
		"initial_url": opts.InitialURL,
	}

	if opts.ViewportWidth > 0 && opts.ViewportHeight > 0 {
		sessionData["viewport"] = map[string]interface{}{
			"width":  opts.ViewportWidth,
			"height": opts.ViewportHeight,
		}
	}

	if opts.TestMode || opts.Proxy {
		browserSettings := map[string]interface{}{}
		if opts.TestMode {
			browserSettings["test_mode"] = true
		}
		sessionData["browser_settings"] = browserSettings
	}

	if opts.Proxy {
		proxyData := map[string]interface{}{
			"enabled": true,
		}
		if opts.ProxyCountry != "" {
			proxyData["country"] = opts.ProxyCountry
		}
		sessionData["proxy"] = proxyData
	}

	jsonData, err := json.Marshal(sessionData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session data: %w", err)
	}

	url := fmt.Sprintf("%s/sessions", p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.APIKey)

	maskedKey := p.APIKey
	if len(maskedKey) > 8 {
		maskedKey = maskedKey[:4] + "..." + maskedKey[len(maskedKey)-4:]
	}
	log.Printf("🔧 BrowserEngine CreateSession: POST %s", url)
	log.Printf("   Key: %s, Body: %s", maskedKey, string(jsonData))

	resp, err := longHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BrowserEngine request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read session response: %w", err)
	}

	log.Printf("   Response: HTTP %d, Body: %s", resp.StatusCode, truncateLog(string(body), 500))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BrowserEngine returned status %d: %s", resp.StatusCode, string(body))
	}

	// Support both API gateway wrapped response {"success":true,"data":{...}}
	// and direct browser-service flat response {"id":"...","url":"...",...}
	var data map[string]interface{}
	var gatewayResp struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
		Error   string                 `json:"error"`
	}
	if err := json.Unmarshal(body, &gatewayResp); err != nil {
		return nil, fmt.Errorf("failed to parse session response: %w", err)
	}

	if gatewayResp.Data != nil {
		// Gateway-wrapped response
		if !gatewayResp.Success {
			return nil, fmt.Errorf("BrowserEngine session creation failed: %s", gatewayResp.Error)
		}
		data = gatewayResp.Data
	} else {
		// Flat response from direct browser-service
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, fmt.Errorf("failed to parse session response: %w", err)
		}
		if errMsg, ok := data["error"].(string); ok && errMsg != "" {
			return nil, fmt.Errorf("BrowserEngine session creation failed: %s", errMsg)
		}
	}
	sessionID, ok := data["id"].(string)
	if !ok {
		return nil, fmt.Errorf("no session ID in BrowserEngine response")
	}

	session := &BrowserSession{
		ID:       sessionID,
		Provider: "browserengine",
		Metadata: data,
	}

	if streamURL, ok := data["stream_url"].(string); ok && streamURL != "" {
		session.StreamURL = streamURL
	}
	if viewURL, ok := data["debug_url"].(string); ok && viewURL != "" {
		session.ViewURL = viewURL
	}
	// NOTE: connect_url from BrowserEngine is a raw WebSocket to the browser-service
	// for display/debugging only. BrowserEngine commands go via REST, not CDP.
	// We do NOT set session.ConnectURL here — that field is for CDP-native providers.

	log.Printf("Created BrowserEngine session %s", sessionID)
	return session, nil
}

func (p *BrowserEngineProvider) DestroySession(ctx context.Context, sessionID string) error {
	url := fmt.Sprintf("%s/sessions/%s", p.BaseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create cleanup request: %w", err)
	}
	req.Header.Set("X-API-Key", p.APIKey)

	log.Printf("🔧 BrowserEngine DestroySession: DELETE %s", url)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to destroy BrowserEngine session %s: %v", sessionID, err)
		return nil
	}
	resp.Body.Close()

	log.Printf("Destroyed BrowserEngine session %s", sessionID)
	return nil
}

// ListSessions fetches active sessions from the BrowserEngine API.
func (p *BrowserEngineProvider) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	url := fmt.Sprintf("%s/sessions", p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list request: %w", err)
	}
	req.Header.Set("X-API-Key", p.APIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BrowserEngine list sessions failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read list response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BrowserEngine returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response — supports gateway-wrapped {"success":true,"data":[...]}
	// and direct flat response {"sessions":[...]} or [...]
	var sessions []SessionInfo

	var gatewayResp struct {
		Success bool `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &gatewayResp); err == nil && gatewayResp.Data != nil {
		if err := json.Unmarshal(gatewayResp.Data, &sessions); err != nil {
			return nil, fmt.Errorf("failed to parse sessions data: %w", err)
		}
		return sessions, nil
	}

	// Try flat array
	if err := json.Unmarshal(body, &sessions); err != nil {
		// Try {"sessions": [...]}
		var wrapper struct {
			Sessions []SessionInfo `json:"sessions"`
		}
		if err2 := json.Unmarshal(body, &wrapper); err2 != nil {
			return nil, fmt.Errorf("failed to parse list response: %w", err)
		}
		sessions = wrapper.Sessions
	}

	return sessions, nil
}

// GetSession fetches a single session's details from the BrowserEngine API.
func (p *BrowserEngineProvider) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	url := fmt.Sprintf("%s/sessions/%s", p.BaseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get request: %w", err)
	}
	req.Header.Set("X-API-Key", p.APIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BrowserEngine get session failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read get response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BrowserEngine returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response — supports gateway-wrapped {"success":true,"data":{...}}
	var info SessionInfo
	var gatewayResp struct {
		Success bool        `json:"success"`
		Data    SessionInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &gatewayResp); err == nil && gatewayResp.Data.ID != "" {
		return &gatewayResp.Data, nil
	}

	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to parse session response: %w", err)
	}
	return &info, nil
}

// ResumeSession resumes a closed/timed-out session via the BrowserEngine API.
// Creates a new browser instance, restores cookies/localStorage, and navigates to the stored URL.
// Returns the updated session info (now RUNNING with a new browser_session_id).
func (p *BrowserEngineProvider) ResumeSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	url := fmt.Sprintf("%s/sessions/%s/resume", p.BaseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("failed to create resume request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.APIKey)

	log.Printf("🔧 BrowserEngine ResumeSession: POST %s", url)

	resp, err := longHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BrowserEngine resume session failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read resume response: %w", err)
	}

	log.Printf("   Response: HTTP %d, Body: %s", resp.StatusCode, truncateLog(string(body), 500))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BrowserEngine resume returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse gateway-wrapped response
	var gatewayResp struct {
		Success bool        `json:"success"`
		Data    SessionInfo `json:"data"`
		Error   string      `json:"error"`
	}
	if err := json.Unmarshal(body, &gatewayResp); err != nil {
		return nil, fmt.Errorf("failed to parse resume response: %w", err)
	}

	if !gatewayResp.Success {
		return nil, fmt.Errorf("BrowserEngine resume failed: %s", gatewayResp.Error)
	}

	if gatewayResp.Data.ID != "" {
		return &gatewayResp.Data, nil
	}

	// Try flat response
	var info SessionInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to parse resume response: %w", err)
	}
	return &info, nil
}

// ExecuteCommand sends a command through the BrowserEngine API.
func (p *BrowserEngineProvider) ExecuteCommand(ctx context.Context, sessionID, cmdType string, params map[string]interface{}) (map[string]interface{}, error) {
	commandData := map[string]interface{}{
		"type":   cmdType,
		"params": params,
	}

	jsonData, err := json.Marshal(commandData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command data: %w", err)
	}

	url := fmt.Sprintf("%s/sessions/%s/commands", p.BaseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create command request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.APIKey)

	log.Printf("🔧 BrowserEngine ExecuteCommand: POST %s", url)
	log.Printf("   Command: %s, Body: %s", cmdType, string(jsonData))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BrowserEngine command failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read command response: %w", err)
	}

	log.Printf("   Response: HTTP %d, Body: %s", resp.StatusCode, truncateLog(string(body), 500))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BrowserEngine command error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// Support both gateway-wrapped and flat responses
	var gatewayResp struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
		Error   string                 `json:"error"`
	}
	if err := json.Unmarshal(body, &gatewayResp); err != nil {
		return nil, fmt.Errorf("failed to parse command response: %w", err)
	}

	if gatewayResp.Data != nil {
		if !gatewayResp.Success {
			return nil, fmt.Errorf("BrowserEngine command failed: %s", gatewayResp.Error)
		}
		return map[string]interface{}{
			"success": true,
			"data":    gatewayResp.Data,
		}, nil
	}

	// Flat response from direct browser-service
	var flat map[string]interface{}
	if err := json.Unmarshal(body, &flat); err != nil {
		return nil, fmt.Errorf("failed to parse command response: %w", err)
	}
	if errMsg, ok := flat["error"].(string); ok && errMsg != "" {
		return nil, fmt.Errorf("BrowserEngine command failed: %s", errMsg)
	}
	return map[string]interface{}{
		"success": true,
		"data":    flat,
	}, nil
}
