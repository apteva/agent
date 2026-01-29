package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"agent-server/config"
	"agent-server/events"
)

// Subscription represents an MCP webhook subscription
type Subscription struct {
	ID           string    `json:"id"`
	Server       string    `json:"server"`
	Events       []string  `json:"events"`
	CredentialID *int64    `json:"credential_id,omitempty"`
	Prompt       *string   `json:"prompt,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// getAgentID returns the agent ID from config
func getAgentID() string {
	cfg := config.GetConfig()
	if cfg == nil {
		return ""
	}
	return cfg.Get().ID
}

// getCallbackURL returns the agent's webhook callback URL (points to /chat endpoint)
func getCallbackURL() string {
	// First check config PublicURL
	cfg := config.GetConfig()
	if cfg != nil {
		agentConfig := cfg.Get()
		if agentConfig.PublicURL != "" {
			return agentConfig.PublicURL + "/chat"
		}
	}

	// Fallback to environment variable
	if callbackURL := os.Getenv("WEBHOOK_CALLBACK_URL"); callbackURL != "" {
		return callbackURL
	}

	// Fallback to port-based URL
	port := os.Getenv("PORT")
	if port == "" {
		port = "4015"
	}
	return fmt.Sprintf("http://localhost:%s/chat", port)
}

// getMCPAPIURL returns the MCP API base URL (without /mcp suffix for subscriptions)
func getMCPAPIURL() string {
	cfg := config.GetConfig()
	if cfg == nil {
		return "http://localhost:3000"
	}
	agentConfig := cfg.Get()
	if agentConfig.MCP != nil && agentConfig.MCP.BaseURL != "" {
		// Strip /mcp suffix if present to get base API URL
		baseURL := agentConfig.MCP.BaseURL
		if len(baseURL) > 4 && baseURL[len(baseURL)-4:] == "/mcp" {
			return baseURL[:len(baseURL)-4]
		}
		return baseURL
	}
	return "http://localhost:3000"
}

// getMCPAPIKey returns the MCP API key
func getMCPAPIKey() string {
	cfg := config.GetConfig()
	if cfg == nil {
		return ""
	}
	agentConfig := cfg.Get()
	if agentConfig.MCP != nil {
		return agentConfig.MCP.APIKey
	}
	return ""
}

// MCPWebhooksListTool lists available webhooks from MCP servers
// Only returns webhooks from servers enabled in the agent's MCP.Webhooks config
func MCPWebhooksListTool(input map[string]interface{}) (map[string]interface{}, error) {
	// Get MCP client from context or global
	mcpClient := GetMCPClient()
	if mcpClient == nil {
		return map[string]interface{}{
			"webhooks": []interface{}{},
			"message":  "MCP client not available",
		}, nil
	}

	// Get enabled webhook servers from agent config
	cfg := config.GetConfig()
	var enabledServers []string
	if cfg != nil {
		agentConfig := cfg.Get()
		if agentConfig.MCP != nil {
			enabledServers = agentConfig.MCP.Webhooks
		}
	}

	// If no webhook servers are enabled, return empty
	if len(enabledServers) == 0 {
		return map[string]interface{}{
			"webhooks": []interface{}{},
			"message":  "No webhook servers enabled in agent config",
			"count":    0,
		}, nil
	}

	// Get available webhooks from MCP servers
	allWebhooks, err := mcpClient.ListWebhooks()
	if err != nil {
		return nil, fmt.Errorf("failed to list webhooks: %w", err)
	}

	// Filter webhooks to only include enabled servers
	var filteredWebhooks []map[string]interface{}
	for _, wh := range allWebhooks {
		serverName, _ := wh["server"].(string)
		for _, enabled := range enabledServers {
			if serverName == enabled {
				filteredWebhooks = append(filteredWebhooks, wh)
				break
			}
		}
	}

	return map[string]interface{}{
		"webhooks":        filteredWebhooks,
		"enabled_servers": enabledServers,
		"count":           len(filteredWebhooks),
	}, nil
}

// SubscribeTool creates or updates a webhook subscription via backend API
func SubscribeTool(input map[string]interface{}) (map[string]interface{}, error) {
	server, ok := input["server"].(string)
	if !ok || server == "" {
		return nil, fmt.Errorf("server is required")
	}

	eventsRaw, ok := input["events"]
	if !ok {
		return nil, fmt.Errorf("events is required")
	}

	// Parse events array
	var eventList []string
	switch e := eventsRaw.(type) {
	case []interface{}:
		for _, ev := range e {
			if s, ok := ev.(string); ok {
				eventList = append(eventList, s)
			}
		}
	case []string:
		eventList = e
	default:
		return nil, fmt.Errorf("events must be an array of strings")
	}

	if len(eventList) == 0 {
		return nil, fmt.Errorf("at least one event is required")
	}

	// Required credential_id
	var credentialID int64
	if credIDRaw, ok := input["credential_id"]; ok {
		switch v := credIDRaw.(type) {
		case float64:
			credentialID = int64(v)
		case int64:
			credentialID = v
		case int:
			credentialID = int64(v)
		}
	}
	if credentialID == 0 {
		return nil, fmt.Errorf("credential_id is required for subscriptions")
	}

	// Agent ID is required for subscriptions
	if getAgentID() == "" {
		return nil, fmt.Errorf("agent context not set - subscriptions require agent_id")
	}

	// Optional prompt
	var prompt string
	if p, ok := input["prompt"].(string); ok && p != "" {
		prompt = p
	}

	// Required title
	var title string
	if t, ok := input["title"].(string); ok && t != "" {
		title = t
	}
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	// Build request to backend API
	callbackURL := getCallbackURL()
	apiURL := getMCPAPIURL() + "/mcp/subscribe"
	apiKey := getMCPAPIKey()

	requestBody := map[string]interface{}{
		"agent_id":      getAgentID(),
		"server":        server,
		"events":        eventList,
		"credential_id": credentialID,
		"callback_url":  callbackURL,
	}

	if prompt != "" {
		requestBody["prompt"] = prompt
	}

	if title != "" {
		requestBody["title"] = title
	}


	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call subscribe API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		errMsg := "subscription failed"
		if msg, ok := result["error"].(string); ok {
			errMsg = msg
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Publish event
	eventBus := events.GetEventBus()
	subEvent := events.NewEvent(events.CategorySystem, "subscription_created", events.LevelInfo).
		WithData("server", server).
		WithData("events", eventList)
	if subID, ok := result["subscription_id"].(string); ok {
		subEvent.WithData("subscription_id", subID)
	}
	eventBus.Publish(subEvent)

	return map[string]interface{}{
		"success":         true,
		"subscription_id": result["subscription_id"],
		"server":          server,
		"events":          eventList,
		"callback_url":    callbackURL,
		"message":         fmt.Sprintf("Subscribed to %d events from %s", len(eventList), server),
	}, nil
}

// SubscriptionsListTool lists all active subscriptions via backend API
func SubscriptionsListTool(input map[string]interface{}) (map[string]interface{}, error) {
	// Build request to backend API
	apiURL := getMCPAPIURL() + "/mcp/subscriptions"
	apiKey := getMCPAPIKey()
	agentID := getAgentID()

	fmt.Printf("[SubscriptionsListTool] apiURL=%s, agentID=%s\n", apiURL, agentID)

	// Add query params
	queryParams := "?"
	if agentID != "" {
		queryParams += "agent_id=" + agentID
	}
	if server, ok := input["server"].(string); ok && server != "" && server != "undefined" && server != "null" {
		if queryParams != "?" {
			queryParams += "&"
		}
		queryParams += "server=" + server
	}

	if queryParams != "?" {
		apiURL += queryParams
	}

	fmt.Printf("[SubscriptionsListTool] Full URL: %s\n", apiURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call subscriptions API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := "failed to list subscriptions"
		if msg, ok := result["error"].(string); ok {
			errMsg = msg
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	return result, nil
}

// UnsubscribeTool removes a subscription via backend API
func UnsubscribeTool(input map[string]interface{}) (map[string]interface{}, error) {
	// Title is required to identify the subscription
	title, ok := input["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required to identify the subscription")
	}

	// Agent ID is required for unsubscribe
	if getAgentID() == "" {
		return nil, fmt.Errorf("agent context not set - unsubscribe requires agent_id")
	}

	// Build request to backend API
	apiURL := getMCPAPIURL() + "/mcp/unsubscribe"
	apiKey := getMCPAPIKey()

	requestBody := map[string]interface{}{
		"agent_id": getAgentID(),
		"title":    title,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call unsubscribe API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := "unsubscribe failed"
		if msg, ok := result["error"].(string); ok {
			errMsg = msg
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Publish event
	eventBus := events.GetEventBus()
	subEvent := events.NewEvent(events.CategorySystem, "subscription_deleted", events.LevelInfo).
		WithData("title", title)
	eventBus.Publish(subEvent)

	return map[string]interface{}{
		"success": true,
		"title":   title,
		"message": fmt.Sprintf("Unsubscribed: %s", title),
	}, nil
}

// UpdateSubscriptionTool updates an existing subscription via backend API
func UpdateSubscriptionTool(input map[string]interface{}) (map[string]interface{}, error) {
	// Title is required to identify the subscription
	title, ok := input["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required to identify the subscription")
	}

	// Agent ID is required
	if getAgentID() == "" {
		return nil, fmt.Errorf("agent context not set - update requires agent_id")
	}

	// Build request to backend API
	apiURL := getMCPAPIURL() + "/mcp/subscription/update"
	apiKey := getMCPAPIKey()

	requestBody := map[string]interface{}{
		"agent_id": getAgentID(),
		"title":    title,
	}

	// Add optional fields if provided
	if prompt, ok := input["prompt"].(string); ok {
		requestBody["prompt"] = prompt
	}

	// new_title allows renaming the subscription
	if newTitle, ok := input["new_title"].(string); ok {
		requestBody["new_title"] = newTitle
	}

	if eventsRaw, ok := input["events"]; ok {
		switch e := eventsRaw.(type) {
		case []interface{}:
			var eventList []string
			for _, ev := range e {
				if s, ok := ev.(string); ok {
					eventList = append(eventList, s)
				}
			}
			if len(eventList) > 0 {
				requestBody["events"] = eventList
			}
		case []string:
			if len(e) > 0 {
				requestBody["events"] = e
			}
		}
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call update API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := "update failed"
		if msg, ok := result["error"].(string); ok {
			errMsg = msg
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	return map[string]interface{}{
		"success": true,
		"title":   title,
		"message": fmt.Sprintf("Updated subscription: %s", title),
	}, nil
}

// MCP client interface for getting webhooks
var mcpClientInstance MCPWebhookClient

type MCPWebhookClient interface {
	ListWebhooks() ([]map[string]interface{}, error)
}

// SetMCPClient sets the MCP client for webhook operations
func SetMCPClient(client MCPWebhookClient) {
	mcpClientInstance = client
}

// GetMCPClient returns the MCP client
func GetMCPClient() MCPWebhookClient {
	return mcpClientInstance
}
