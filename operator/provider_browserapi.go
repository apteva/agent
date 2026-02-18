package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// BrowserAPIProvider implements BrowserProvider for the Browser API gateway.
// It routes through the API gateway which adds authentication, billing,
// usage tracking, and command audit logging.
type BrowserAPIProvider struct {
	BaseURL string // e.g., "http://91.99.63.64:3000/browser"
	APIKey  string // X-API-Key header value
}

// NewBrowserAPIProvider creates a new BrowserAPIProvider.
func NewBrowserAPIProvider(baseURL, apiKey string) *BrowserAPIProvider {
	// Strip trailing slash
	baseURL = strings.TrimRight(baseURL, "/")
	return &BrowserAPIProvider{
		BaseURL: baseURL,
		APIKey:  apiKey,
	}
}

func (p *BrowserAPIProvider) Name() string { return "browserapi" }

func (p *BrowserAPIProvider) CreateSession(ctx context.Context, opts SessionOptions) (*BrowserSession, error) {
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

	log.Printf("Creating BrowserAPI session at %s", url)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BrowserAPI request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read session response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BrowserAPI returned status %d: %s", resp.StatusCode, string(body))
	}

	// Gateway returns: {"success": true, "data": {id, stream_url, debug_url, connect_url, ...}}
	var gatewayResp struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
		Error   string                 `json:"error"`
	}
	if err := json.Unmarshal(body, &gatewayResp); err != nil {
		return nil, fmt.Errorf("failed to parse session response: %w", err)
	}

	if !gatewayResp.Success {
		return nil, fmt.Errorf("BrowserAPI session creation failed: %s", gatewayResp.Error)
	}

	data := gatewayResp.Data
	sessionID, ok := data["id"].(string)
	if !ok {
		return nil, fmt.Errorf("no session ID in BrowserAPI response")
	}

	session := &BrowserSession{
		ID:       sessionID,
		Provider: "browserapi",
		Metadata: data,
	}

	if streamURL, ok := data["stream_url"].(string); ok && streamURL != "" {
		session.StreamURL = streamURL
	}
	if viewURL, ok := data["debug_url"].(string); ok && viewURL != "" {
		session.ViewURL = viewURL
	}
	if connectURL, ok := data["connect_url"].(string); ok && connectURL != "" {
		session.ConnectURL = connectURL
	}

	log.Printf("Created BrowserAPI session %s", sessionID)
	return session, nil
}

func (p *BrowserAPIProvider) DestroySession(ctx context.Context, sessionID string) error {
	url := fmt.Sprintf("%s/sessions/%s", p.BaseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create cleanup request: %w", err)
	}
	req.Header.Set("X-API-Key", p.APIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to destroy BrowserAPI session %s: %v", sessionID, err)
		return nil
	}
	resp.Body.Close()

	log.Printf("Destroyed BrowserAPI session %s", sessionID)
	return nil
}

// ExecuteCommand sends a command through the Browser API gateway.
// Used by executeRESTCommand when the active provider is browserapi.
func (p *BrowserAPIProvider) ExecuteCommand(ctx context.Context, sessionID, cmdType string, params map[string]interface{}) (map[string]interface{}, error) {
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

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BrowserAPI command failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read command response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BrowserAPI command error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// Gateway returns: {"success": true, "data": {...}, "duration_ms": ...}
	var gatewayResp struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
		Error   string                 `json:"error"`
	}
	if err := json.Unmarshal(body, &gatewayResp); err != nil {
		return nil, fmt.Errorf("failed to parse command response: %w", err)
	}

	if !gatewayResp.Success {
		return nil, fmt.Errorf("BrowserAPI command failed: %s", gatewayResp.Error)
	}

	// Return the inner data to match direct service format
	if gatewayResp.Data != nil {
		return map[string]interface{}{
			"success": true,
			"data":    gatewayResp.Data,
		}, nil
	}

	return map[string]interface{}{
		"success": true,
	}, nil
}
