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

// BrowserbaseProvider implements BrowserProvider for Browserbase (browserbase.com).
// Sessions are created via REST API and controlled via CDP WebSocket.
type BrowserbaseProvider struct {
	APIKey    string
	ProjectID string
	BaseURL   string // Default: "https://api.browserbase.com"
}

// NewBrowserbaseProvider creates a new BrowserbaseProvider.
func NewBrowserbaseProvider(apiKey, projectID string) *BrowserbaseProvider {
	return &BrowserbaseProvider{
		APIKey:    apiKey,
		ProjectID: projectID,
		BaseURL:   "https://api.browserbase.com",
	}
}

func (p *BrowserbaseProvider) Name() string { return "browserbase" }

func (p *BrowserbaseProvider) CreateSession(ctx context.Context, opts SessionOptions) (*BrowserSession, error) {
	body := map[string]interface{}{
		"projectId": p.ProjectID,
		"keepAlive": true,
		"timeout":   1800, // 30 minutes — prevents premature session expiry during agent thinking
	}

	// Add browser settings if viewport specified
	if opts.ViewportWidth > 0 && opts.ViewportHeight > 0 {
		body["browserSettings"] = map[string]interface{}{
			"viewport": map[string]int{
				"width":  opts.ViewportWidth,
				"height": opts.ViewportHeight,
			},
		}
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal browserbase request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/sessions", p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create browserbase request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BB-API-Key", p.APIKey)

	log.Printf("🌐 Creating Browserbase session (project: %s)", p.ProjectID)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("browserbase API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read browserbase response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("browserbase API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse browserbase response: %w", err)
	}

	sessionID, ok := result["id"].(string)
	if !ok {
		return nil, fmt.Errorf("no session ID in browserbase response")
	}

	// Browserbase returns connectUrl directly in the response
	connectURL, _ := result["connectUrl"].(string)
	if connectURL == "" {
		// Fallback: construct from known pattern
		connectURL = fmt.Sprintf("wss://connect.browserbase.com?apiKey=%s&sessionId=%s", p.APIKey, sessionID)
	}

	log.Printf("🌐 Browserbase session created: %s", sessionID)

	// Navigate to initial URL after connection if specified
	session := &BrowserSession{
		ID:         sessionID,
		ConnectURL: connectURL,
		Provider:   "browserbase",
		Metadata:   result,
	}

	return session, nil
}

func (p *BrowserbaseProvider) DestroySession(ctx context.Context, sessionID string) error {
	// Browserbase sessions are closed by updating status to REQUEST_RELEASE
	body := map[string]interface{}{
		"status":    "REQUEST_RELEASE",
		"projectId": p.ProjectID,
	}

	jsonData, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/v1/sessions/%s", p.BaseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("failed to create browserbase close request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BB-API-Key", p.APIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to close browserbase session %s: %v", sessionID, err)
		return nil
	}
	resp.Body.Close()

	log.Printf("🌐 Browserbase session closed: %s", sessionID)
	return nil
}
