package tools

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"time"
)

type AnalyzeImageURLTool struct{}

func (t *AnalyzeImageURLTool) Name() string {
	return "analyze_image_url"
}

func (t *AnalyzeImageURLTool) DisplayName() string {
	return "Analyze Image URL"
}

func (t *AnalyzeImageURLTool) Description() string {
	return "Analyzes an image from a public URL. Fetches the image and returns metadata (dimensions, size, format). Use this to test URL-based image processing and verify that images stored in the filesystem are accessible via public URLs."
}

func (t *AnalyzeImageURLTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "Public URL of the image to analyze (e.g., https://agent.example.com/files/abc123/download)",
			},
		},
		"required": []string{"url"},
	}
}

func (t *AnalyzeImageURLTool) Execute(params map[string]interface{}) (interface{}, error) {
	url, ok := params["url"].(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("url parameter is required")
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Fetch image from URL
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image from URL: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch image: HTTP %d", resp.StatusCode)
	}

	// Decode image to get metadata
	img, format, err := image.Decode(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image (format detection failed): %w", err)
	}

	// Get image dimensions
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Get content type and size
	contentType := resp.Header.Get("Content-Type")
	contentLength := resp.ContentLength

	// Build result
	result := map[string]interface{}{
		"status":       "success",
		"url":          url,
		"format":       format,
		"width":        width,
		"height":       height,
		"content_type": contentType,
		"size_bytes":   contentLength,
	}

	// Add size in KB/MB for readability
	if contentLength > 0 {
		if contentLength < 1024 {
			result["size_display"] = fmt.Sprintf("%d bytes", contentLength)
		} else if contentLength < 1024*1024 {
			result["size_display"] = fmt.Sprintf("%.1f KB", float64(contentLength)/1024.0)
		} else {
			result["size_display"] = fmt.Sprintf("%.2f MB", float64(contentLength)/(1024.0*1024.0))
		}
	}

	return result, nil
}
