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

// SteelProvider implements BrowserProvider for Steel.dev.
// Sessions are created via REST API and controlled via CDP WebSocket.
type SteelProvider struct {
	APIKey  string
	BaseURL string // Default: "https://api.steel.dev"
}

// NewSteelProvider creates a new SteelProvider.
func NewSteelProvider(apiKey, baseURL string) *SteelProvider {
	if baseURL == "" {
		baseURL = "https://api.steel.dev"
	}
	return &SteelProvider{
		APIKey:  apiKey,
		BaseURL: baseURL,
	}
}

func (p *SteelProvider) Name() string { return "steel" }

func (p *SteelProvider) CreateSession(ctx context.Context, opts SessionOptions) (*BrowserSession, error) {
	body := map[string]interface{}{}

	if opts.ViewportWidth > 0 && opts.ViewportHeight > 0 {
		body["dimensions"] = map[string]int{
			"width":  opts.ViewportWidth,
			"height": opts.ViewportHeight,
		}
	}

	// Note: Steel proxies require a paid plan. Don't send useProxy by default.
	// Users on paid plans can enable this via config if needed.

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal steel request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/sessions", p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create steel request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Steel-Api-Key", p.APIKey)

	log.Printf("🔧 Steel: Creating session: POST %s", url)
	log.Printf("🔧 Steel: Request body: %s", string(jsonData))
	log.Printf("🔧 Steel: API key: %s...%s (%d chars)", p.APIKey[:4], p.APIKey[len(p.APIKey)-4:], len(p.APIKey))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("steel API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read steel response: %w", err)
	}

	log.Printf("🔧 Steel: Response status: %d", resp.StatusCode)
	log.Printf("🔧 Steel: Response body: %s", string(respBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("steel API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse steel response: %w", err)
	}

	sessionID, ok := result["id"].(string)
	if !ok {
		return nil, fmt.Errorf("no session ID in steel response")
	}

	// Steel requires apiKey in the WebSocket URL for authentication.
	// The API response includes websocketUrl but without the apiKey — we must append it.
	connectURL, _ := result["websocketUrl"].(string)
	if connectURL == "" {
		connectURL = fmt.Sprintf("wss://connect.steel.dev?apiKey=%s&sessionId=%s", p.APIKey, sessionID)
	} else if !strings.Contains(connectURL, "apiKey=") {
		connectURL = connectURL + "&apiKey=" + p.APIKey
	}

	// Extract optional URLs
	viewURL, _ := result["sessionViewerUrl"].(string)

	log.Printf("🔧 Steel session created: %s", sessionID)

	return &BrowserSession{
		ID:         sessionID,
		ConnectURL: connectURL,
		ViewURL:    viewURL,
		Provider:   "steel",
		Metadata:   result,
	}, nil
}

func (p *SteelProvider) DestroySession(ctx context.Context, sessionID string) error {
	url := fmt.Sprintf("%s/v1/sessions/%s/release", p.BaseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create steel release request: %w", err)
	}

	req.Header.Set("Steel-Api-Key", p.APIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to release steel session %s: %v", sessionID, err)
		return nil
	}
	resp.Body.Close()

	log.Printf("🔧 Steel session released: %s", sessionID)
	return nil
}
