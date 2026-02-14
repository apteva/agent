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

// ChromeProvider implements BrowserProvider for raw Chrome DevTools instances.
// Connects directly to Chrome running with --remote-debugging-port.
type ChromeProvider struct {
	DebugURL string // e.g., "http://localhost:9222" or "ws://localhost:9222"
}

// NewChromeProvider creates a new ChromeProvider.
func NewChromeProvider(debugURL string) *ChromeProvider {
	return &ChromeProvider{DebugURL: debugURL}
}

func (p *ChromeProvider) Name() string { return "chrome" }

func (p *ChromeProvider) CreateSession(ctx context.Context, opts SessionOptions) (*BrowserSession, error) {
	// For raw Chrome, we create a new tab/target via the /json/new endpoint
	httpURL := p.getHTTPURL()

	// Create new tab with initial URL
	targetURL := "about:blank"
	if opts.InitialURL != "" {
		targetURL = opts.InitialURL
	}

	url := fmt.Sprintf("%s/json/new?%s", httpURL, targetURL)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create chrome tab request: %w", err)
	}

	log.Printf("🔧 Creating Chrome tab at %s", httpURL)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chrome API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read chrome response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("chrome API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse chrome response: %w", err)
	}

	// Chrome returns target info with webSocketDebuggerUrl
	targetID, _ := result["id"].(string)
	connectURL, _ := result["webSocketDebuggerUrl"].(string)

	if connectURL == "" {
		return nil, fmt.Errorf("chrome did not return webSocketDebuggerUrl")
	}

	log.Printf("🔧 Chrome tab created: target=%s ws=%s", targetID, connectURL)

	return &BrowserSession{
		ID:         targetID,
		ConnectURL: connectURL,
		TargetID:   targetID,
		Provider:   "chrome",
		Metadata:   result,
	}, nil
}

func (p *ChromeProvider) DestroySession(ctx context.Context, sessionID string) error {
	httpURL := p.getHTTPURL()
	url := fmt.Sprintf("%s/json/close/%s", httpURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create chrome close request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to close chrome tab %s: %v", sessionID, err)
		return nil
	}
	resp.Body.Close()

	log.Printf("🔧 Chrome tab closed: %s", sessionID)
	return nil
}

// getHTTPURL converts a WebSocket URL to HTTP URL for the Chrome DevTools API
func (p *ChromeProvider) getHTTPURL() string {
	url := p.DebugURL
	url = strings.TrimSuffix(url, "/")
	// Convert ws:// to http:// for REST API calls
	if strings.HasPrefix(url, "ws://") {
		url = "http://" + url[5:]
	} else if strings.HasPrefix(url, "wss://") {
		url = "https://" + url[6:]
	}
	return url
}
