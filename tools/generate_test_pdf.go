package tools

import (
	"encoding/base64"
	"fmt"
	"time"
)

type GenerateTestPDFTool struct{}

func (t *GenerateTestPDFTool) Name() string {
	return "generate_test_pdf"
}

func (t *GenerateTestPDFTool) DisplayName() string {
	return "Generate Test PDF"
}

func (t *GenerateTestPDFTool) Description() string {
	return "Generates a test PDF document as base64 for testing document handling. Returns a simple PDF with the specified text content."
}

func (t *GenerateTestPDFTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Title for the PDF document",
				"default":     "Test Document",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Text content to include in the PDF",
				"default":     "This is a test PDF document generated for testing document handling capabilities.",
			},
		},
	}
}

func (t *GenerateTestPDFTool) Execute(params map[string]interface{}) (interface{}, error) {
	title := "Test Document"
	content := "This is a test PDF document generated for testing document handling capabilities."

	if t, ok := params["title"].(string); ok && t != "" {
		title = t
	}
	if c, ok := params["content"].(string); ok && c != "" {
		content = c
	}

	// Generate a valid minimal PDF
	pdfBytes := buildSimplePDF(title, content)

	// Convert to base64
	base64Str := base64.StdEncoding.EncodeToString(pdfBytes)
	sizeKB := float64(len(base64Str)) / 1024.0

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Generated PDF document '%s' (%.1f KB base64)", title, sizeKB),
		"document": map[string]interface{}{
			"data":      base64Str,
			"mimeType":  "application/pdf",
			"title":     title,
			"pages":     1,
			"size_bytes": len(pdfBytes),
		},
		"generated_at": time.Now().Format(time.RFC3339),
	}, nil
}

// buildSimplePDF generates a minimal valid PDF with the given title and content.
// This creates a proper PDF 1.4 document with a single page.
func buildSimplePDF(title, content string) []byte {
	// Escape special characters in PDF strings
	escTitle := pdfEscapeString(title)
	escContent := pdfEscapeString(content)

	// Build the content stream (text rendering operations)
	stream := fmt.Sprintf("BT\n/F1 18 Tf\n72 720 Td\n(%s) Tj\n0 -30 Td\n/F1 12 Tf\n(%s) Tj\nET", escTitle, escContent)
	streamLen := len(stream)

	// Build the PDF object by object
	// Object 1: Catalog
	obj1 := "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"
	// Object 2: Pages
	obj2 := "2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n"
	// Object 3: Page
	obj3 := "3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n"
	// Object 4: Content stream
	obj4 := fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", streamLen, stream)
	// Object 5: Font
	obj5 := "5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n"

	// Calculate byte offsets for xref table
	header := "%PDF-1.4\n"
	offsets := make([]int, 6) // 0-5, index 0 unused
	pos := len(header)

	offsets[1] = pos
	pos += len(obj1)
	offsets[2] = pos
	pos += len(obj2)
	offsets[3] = pos
	pos += len(obj3)
	offsets[4] = pos
	pos += len(obj4)
	offsets[5] = pos
	pos += len(obj5)

	xrefOffset := pos

	// Build xref table
	xref := "xref\n0 6\n"
	xref += "0000000000 65535 f \n"
	for i := 1; i <= 5; i++ {
		xref += fmt.Sprintf("%010d 00000 n \n", offsets[i])
	}

	// Trailer
	trailer := fmt.Sprintf("trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)

	// Concatenate everything
	pdf := header + obj1 + obj2 + obj3 + obj4 + obj5 + xref + trailer

	return []byte(pdf)
}

// pdfEscapeString escapes special characters for PDF string literals
func pdfEscapeString(s string) string {
	result := ""
	for _, c := range s {
		switch c {
		case '(':
			result += "\\("
		case ')':
			result += "\\)"
		case '\\':
			result += "\\\\"
		default:
			result += string(c)
		}
	}
	return result
}
