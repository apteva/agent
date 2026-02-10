package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// MCPResource represents a resource from an MCP server
type MCPResource struct {
	URI         string               `json:"uri"`
	Name        string               `json:"name"`
	Title       string               `json:"title,omitempty"`
	Description string               `json:"description,omitempty"`
	MimeType    string               `json:"mimeType,omitempty"`
	Size        int64                `json:"size,omitempty"`
	Annotations *ResourceAnnotations `json:"annotations,omitempty"`
	ServerID    int                  `json:"serverId"`    // MCP server returns camelCase
	ServerName  string               `json:"server_name"` // Set by agent
	LoadedAt    time.Time            `json:"loaded_at"`   // Set by agent
}

// ResourceAnnotations contains optional hints about resource usage
type ResourceAnnotations struct {
	Audience     []string `json:"audience,omitempty"`     // "user", "assistant"
	Priority     float64  `json:"priority,omitempty"`     // 0.0-1.0
	LastModified string   `json:"lastModified,omitempty"` // ISO 8601
}

// ResourceContent represents the content of a resource
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // Base64 encoded binary
}

// ResourceTemplate represents a parameterized resource template
type ResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// API request/response structures

type ResourceListRequest struct {
	ServerID int    `json:"server_id"`
	Cursor   string `json:"cursor,omitempty"`
}

type ResourceListResponse struct {
	Success    bool          `json:"success"`
	Resources  []MCPResource `json:"resources"` // Legacy field
	Data       []MCPResource `json:"data"`      // Current MCP server format
	NextCursor string        `json:"nextCursor,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// GetResources returns resources from either field (data or resources)
func (r *ResourceListResponse) GetResources() []MCPResource {
	if len(r.Data) > 0 {
		return r.Data
	}
	return r.Resources
}

type ResourceReadRequest struct {
	ServerID int    `json:"server_id"`
	URI      string `json:"uri"`
}

type ResourceReadResponse struct {
	Success  bool              `json:"success"`
	Contents []ResourceContent `json:"contents"`
	Error    string            `json:"error,omitempty"`
}

type ResourceSubscribeRequest struct {
	ServerID int    `json:"server_id"`
	URI      string `json:"uri"`
}

type ResourceSubscribeResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ListResources fetches available resources from an MCP server
func (c *MCPClient) ListResources(serverID int) ([]MCPResource, error) {
	url := fmt.Sprintf("%s/resources/list", c.config.BaseURL)

	reqBody := ResourceListRequest{
		ServerID: serverID,
	}

	var allResources []MCPResource
	cursor := ""

	for {
		reqBody.Cursor = cursor

		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		if c.config.APIKey != "" {
			req.Header.Set("X-API-Key", c.config.APIKey)
		}
		req.Header.Set("Content-Type", "application/json")

		var resp *http.Response
		var lastErr error

		// Retry logic
		for i := 0; i <= c.config.RetryCount; i++ {
			resp, lastErr = c.httpClient.Do(req)
			if lastErr == nil && resp.StatusCode == http.StatusOK {
				break
			}
			if resp != nil {
				log.Printf("📚 MCP ListResources: POST %s server_id=%d attempt=%d status=%d", url, serverID, i+1, resp.StatusCode)
			}
			if i < c.config.RetryCount {
				time.Sleep(time.Second * time.Duration(i+1))
			}
		}

		if lastErr != nil {
			return nil, fmt.Errorf("failed to fetch resources after %d retries: %w", c.config.RetryCount, lastErr)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
		}

		var listResp ResourceListResponse
		if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if !listResp.Success {
			errMsg := "unknown error"
			if listResp.Error != "" {
				errMsg = listResp.Error
			}
			return nil, fmt.Errorf("server returned success=false: %s", errMsg)
		}

		// Set metadata for each resource
		now := time.Now()
		resources := listResp.GetResources()
		for i := range resources {
			resources[i].ServerID = serverID
			resources[i].LoadedAt = now
		}

		allResources = append(allResources, resources...)

		// Check for pagination
		if listResp.NextCursor == "" {
			break
		}
		cursor = listResp.NextCursor
	}

	return allResources, nil
}

// ListAllResourcesFromAPI fetches all resources in a single API call to /resources/list
func (c *MCPClient) ListAllResourcesFromAPI() ([]MCPResource, error) {
	url := fmt.Sprintf("%s/resources/list", c.config.BaseURL)

	var allResources []MCPResource
	cursor := ""

	for {
		// Request body - no server_id filter, get all resources
		reqBody := map[string]interface{}{}
		if cursor != "" {
			reqBody["cursor"] = cursor
		}

		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		if c.config.APIKey != "" {
			req.Header.Set("X-API-Key", c.config.APIKey)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch resources: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
		}

		var listResp ResourceListResponse
		if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		if !listResp.Success {
			return nil, fmt.Errorf("server returned success=false: %s", listResp.Error)
		}

		// Set loaded timestamp
		now := time.Now()
		resources := listResp.GetResources()
		for i := range resources {
			resources[i].LoadedAt = now
		}

		allResources = append(allResources, resources...)

		// Check for pagination
		if listResp.NextCursor == "" {
			break
		}
		cursor = listResp.NextCursor
	}

	return allResources, nil
}

// ListAllResources fetches resources from all servers that support resources (DEPRECATED - use ListAllResourcesFromAPI)
func (c *MCPClient) ListAllResources() ([]MCPResource, error) {
	cache := GetMCPCache()
	servers := cache.GetServers()

	var allResources []MCPResource
	for _, server := range servers {
		resources, err := c.ListResources(server.ID)
		if err != nil {
			// Log but continue with other servers
			continue
		}
		// Set server name
		for i := range resources {
			resources[i].ServerName = server.Name
		}
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

// ReadResource fetches the content of a specific resource
func (c *MCPClient) ReadResource(serverID int, uri string) (*ResourceContent, error) {
	url := fmt.Sprintf("%s/resources/read", c.config.BaseURL)

	reqBody := ResourceReadRequest{
		ServerID: serverID,
		URI:      uri,
	}

	log.Printf("📖 MCP ReadResource: POST %s server_id=%d uri=%s", url, serverID, uri)

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	var lastErr error

	// Retry logic
	for i := 0; i <= c.config.RetryCount; i++ {
		resp, lastErr = c.httpClient.Do(req)
		if lastErr == nil && resp.StatusCode == http.StatusOK {
			break
		}
		if i < c.config.RetryCount {
			log.Printf("📖 MCP ReadResource: Retry %d/%d for %s", i+1, c.config.RetryCount, uri)
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to read resource after %d retries: %w", c.config.RetryCount, lastErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var readResp ResourceReadResponse
	if err := json.NewDecoder(resp.Body).Decode(&readResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !readResp.Success {
		return nil, fmt.Errorf("failed to read resource: %s", readResp.Error)
	}

	if len(readResp.Contents) == 0 {
		return nil, fmt.Errorf("no content returned for resource")
	}

	contentLen := len(readResp.Contents[0].Text)
	if readResp.Contents[0].Blob != "" {
		contentLen = len(readResp.Contents[0].Blob)
	}
	log.Printf("📖 MCP ReadResource: Got %d bytes for %s (mime: %s)", contentLen, uri, readResp.Contents[0].MimeType)

	return &readResp.Contents[0], nil
}

// Subscribe subscribes to updates for a specific resource
func (c *MCPClient) Subscribe(serverID int, uri string) error {
	url := fmt.Sprintf("%s/resources/subscribe", c.config.BaseURL)

	reqBody := ResourceSubscribeRequest{
		ServerID: serverID,
		URI:      uri,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var subResp ResourceSubscribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&subResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !subResp.Success {
		return fmt.Errorf("failed to subscribe: %s", subResp.Error)
	}

	return nil
}

// Unsubscribe unsubscribes from updates for a specific resource
func (c *MCPClient) Unsubscribe(serverID int, uri string) error {
	url := fmt.Sprintf("%s/resources/unsubscribe", c.config.BaseURL)

	reqBody := ResourceSubscribeRequest{
		ServerID: serverID,
		URI:      uri,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.config.APIKey != "" {
		req.Header.Set("X-API-Key", c.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to unsubscribe: %w", err)
	}
	defer resp.Body.Close()

	// Don't fail if unsubscribe fails - resource might not exist
	return nil
}
