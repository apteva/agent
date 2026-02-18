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

// BrowserEngineProvider implements BrowserProvider for our BrowserEngine service.
// It uses REST HTTP for session creation/destruction.
// Command execution stays in operator.go via executeRESTCommand.
type BrowserEngineProvider struct {
	BaseURL string // e.g., "http://localhost:8098"
}

// NewBrowserEngineProvider creates a new BrowserEngineProvider.
func NewBrowserEngineProvider(baseURL string) *BrowserEngineProvider {
	return &BrowserEngineProvider{BaseURL: baseURL}
}

func (p *BrowserEngineProvider) Name() string { return "browserengine" }

func (p *BrowserEngineProvider) CreateSession(ctx context.Context, opts SessionOptions) (*BrowserSession, error) {
	sessionData := map[string]interface{}{
		"initial_url": opts.InitialURL,
		"test_mode":   opts.TestMode,
		"proxy":       opts.Proxy,
	}
	if opts.ProxyCountry != "" {
		sessionData["proxy_country"] = opts.ProxyCountry
	}

	if opts.ViewportWidth > 0 && opts.ViewportHeight > 0 {
		sessionData["viewport"] = map[string]interface{}{
			"width":  opts.ViewportWidth,
			"height": opts.ViewportHeight,
		}
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

	log.Printf("Creating BrowserEngine session at %s with data: %s", url, string(jsonData))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make session request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read session response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("BrowserEngine returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse session response: %w", err)
	}

	sessionID, ok := result["id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid session id in response")
	}

	session := &BrowserSession{
		ID:       sessionID,
		Provider: "browserengine",
		Metadata: result,
	}

	// Extract optional URLs (only prepend BaseURL if they're relative paths)
	if streamURL, ok := result["stream_url"].(string); ok {
		if strings.HasPrefix(streamURL, "http://") || strings.HasPrefix(streamURL, "https://") {
			session.StreamURL = streamURL
		} else {
			session.StreamURL = fmt.Sprintf("%s%s", p.BaseURL, streamURL)
		}
	}
	if viewURL, ok := result["view_url"].(string); ok {
		if strings.HasPrefix(viewURL, "http://") || strings.HasPrefix(viewURL, "https://") {
			session.ViewURL = viewURL
		} else {
			session.ViewURL = fmt.Sprintf("%s%s", p.BaseURL, viewURL)
		}
	}
	if targetID, ok := result["target_id"].(string); ok {
		session.TargetID = targetID
	}
	// Forward-compatible: if BrowserEngine returns CDP URL in future
	if connectURL, ok := result["connect_url"].(string); ok {
		session.ConnectURL = connectURL
	}

	log.Printf("Created BrowserEngine session %s", sessionID)
	return session, nil
}

func (p *BrowserEngineProvider) DestroySession(ctx context.Context, sessionID string) error {
	url := fmt.Sprintf("%s/sessions/%s", p.BaseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create cleanup request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		// Don't fail on cleanup errors
		log.Printf("Failed to destroy BrowserEngine session %s: %v", sessionID, err)
		return nil
	}
	resp.Body.Close()
	return nil
}
