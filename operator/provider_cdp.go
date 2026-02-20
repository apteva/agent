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

// CDPProvider implements BrowserProvider for direct CDP WebSocket connections.
// Use this to connect to any browser exposing a Chrome DevTools Protocol endpoint.
type CDPProvider struct {
	URL string // e.g., "ws://localhost:9222" or "http://localhost:9222"
}

// NewCDPProvider creates a new CDPProvider.
func NewCDPProvider(url string) *CDPProvider {
	return &CDPProvider{URL: strings.TrimSuffix(url, "/")}
}

func (p *CDPProvider) Name() string { return "cdp" }

func (p *CDPProvider) CreateSession(ctx context.Context, opts SessionOptions) (*BrowserSession, error) {
	httpURL := p.getHTTPURL()

	targetURL := "about:blank"
	if opts.InitialURL != "" {
		targetURL = opts.InitialURL
	}

	url := fmt.Sprintf("%s/json/new?%s", httpURL, targetURL)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create CDP tab request: %w", err)
	}

	log.Printf("Creating CDP tab at %s", httpURL)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CDP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read CDP response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("CDP error %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse CDP response: %w", err)
	}

	targetID, _ := result["id"].(string)
	connectURL, _ := result["webSocketDebuggerUrl"].(string)

	if connectURL == "" {
		return nil, fmt.Errorf("CDP did not return webSocketDebuggerUrl")
	}

	log.Printf("CDP tab created: target=%s ws=%s", targetID, connectURL)

	return &BrowserSession{
		ID:         targetID,
		ConnectURL: connectURL,
		TargetID:   targetID,
		Provider:   "cdp",
		Metadata:   result,
	}, nil
}

func (p *CDPProvider) DestroySession(ctx context.Context, sessionID string) error {
	httpURL := p.getHTTPURL()
	url := fmt.Sprintf("%s/json/close/%s", httpURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create CDP close request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to close CDP tab %s: %v", sessionID, err)
		return nil
	}
	resp.Body.Close()

	log.Printf("CDP tab closed: %s", sessionID)
	return nil
}

// getHTTPURL converts a WebSocket URL to HTTP URL for the Chrome DevTools API
func (p *CDPProvider) getHTTPURL() string {
	url := p.URL
	if strings.HasPrefix(url, "ws://") {
		url = "http://" + url[5:]
	} else if strings.HasPrefix(url, "wss://") {
		url = "https://" + url[6:]
	}
	return url
}
