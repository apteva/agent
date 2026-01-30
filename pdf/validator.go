package pdf

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/apteva/agent/config"
)

const (
	MaxPDFSize  = 32 * 1024 * 1024 // 32MB request limit
	MaxPDFPages = 100               // Max pages per request
)

type PDFValidator struct {
	config *config.PDFConfig
}

func NewPDFValidator(cfg *config.PDFConfig) *PDFValidator {
	return &PDFValidator{config: cfg}
}

// ValidatePDF checks if PDF meets requirements
func (v *PDFValidator) ValidatePDF(data []byte) error {
	if v.config == nil || !v.config.Enabled {
		return fmt.Errorf("PDF support is not enabled")
	}

	if int64(len(data)) > v.config.MaxFileSize {
		return fmt.Errorf("PDF size %d exceeds maximum %d bytes", len(data), v.config.MaxFileSize)
	}

	// Basic PDF header check
	if len(data) < 4 || string(data[:4]) != "%PDF" {
		return fmt.Errorf("invalid PDF format")
	}

	// Note: Page count validation would require parsing PDF structure
	// For now, we rely on Claude's API to enforce the 100-page limit

	return nil
}

// FetchPDFFromURL downloads a PDF from URL
func FetchPDFFromURL(url string, maxSize int64) ([]byte, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PDF: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch PDF: status %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(contentType), "pdf") {
		// Some servers might not set content-type correctly, so also check URL
		if !strings.HasSuffix(strings.ToLower(url), ".pdf") {
			return nil, fmt.Errorf("URL does not point to a PDF (content-type: %s)", contentType)
		}
	}

	// Read with size limit
	limitReader := io.LimitReader(resp.Body, maxSize+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF: %w", err)
	}

	if int64(len(data)) > maxSize {
		return nil, fmt.Errorf("PDF size exceeds maximum %d bytes", maxSize)
	}

	return data, nil
}

