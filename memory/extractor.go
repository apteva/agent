package memory

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// TextExtractor extracts text content from various file formats
type TextExtractor struct{}

// NewTextExtractor creates a new text extractor
func NewTextExtractor() *TextExtractor {
	return &TextExtractor{}
}

// ExtractResult contains extracted text and metadata
type ExtractResult struct {
	Content  string                 // Extracted text content
	Title    string                 // Document title if detected
	Metadata map[string]interface{} // Additional metadata
}

// Extract extracts text from file data based on MIME type
func (te *TextExtractor) Extract(data []byte, mimeType string) (*ExtractResult, error) {
	switch mimeType {
	case "text/plain":
		return te.extractPlainText(data)
	case "text/markdown":
		return te.extractMarkdown(data)
	case "application/json":
		return te.extractJSON(data)
	case "text/csv":
		return te.extractCSV(data)
	case "text/html":
		return te.extractHTML(data)
	case "application/pdf":
		return te.extractPDF(data)
	default:
		return nil, fmt.Errorf("unsupported MIME type: %s", mimeType)
	}
}

// CanExtract checks if a MIME type is supported for extraction
func (te *TextExtractor) CanExtract(mimeType string) bool {
	supported := []string{
		"text/plain",
		"text/markdown",
		"application/json",
		"text/csv",
		"text/html",
		"application/pdf",
	}
	for _, s := range supported {
		if mimeType == s {
			return true
		}
	}
	return false
}

// extractPlainText handles plain text files
func (te *TextExtractor) extractPlainText(data []byte) (*ExtractResult, error) {
	content := string(data)
	content = normalizeWhitespace(content)

	// Try to detect title from first line
	title := ""
	lines := strings.SplitN(content, "\n", 2)
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		if len(firstLine) > 0 && len(firstLine) < 100 {
			title = firstLine
		}
	}

	return &ExtractResult{
		Content:  content,
		Title:    title,
		Metadata: make(map[string]interface{}),
	}, nil
}

// extractMarkdown handles Markdown files
func (te *TextExtractor) extractMarkdown(data []byte) (*ExtractResult, error) {
	content := string(data)
	content = normalizeWhitespace(content)

	// Extract title from first H1 heading
	title := ""
	h1Regex := regexp.MustCompile(`(?m)^#\s+(.+)$`)
	if match := h1Regex.FindStringSubmatch(content); len(match) > 1 {
		title = strings.TrimSpace(match[1])
	}

	// Extract metadata from YAML frontmatter if present
	metadata := make(map[string]interface{})
	frontmatterRegex := regexp.MustCompile(`(?s)^---\n(.+?)\n---\n`)
	if match := frontmatterRegex.FindStringSubmatch(content); len(match) > 1 {
		// Parse simple key: value pairs from frontmatter
		for _, line := range strings.Split(match[1], "\n") {
			if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				metadata[key] = value
				if key == "title" && title == "" {
					title = value
				}
			}
		}
		// Remove frontmatter from content
		content = frontmatterRegex.ReplaceAllString(content, "")
	}

	// Convert Markdown to plain text for better embedding
	plainContent := markdownToPlainText(content)

	return &ExtractResult{
		Content:  plainContent,
		Title:    title,
		Metadata: metadata,
	}, nil
}

// extractJSON handles JSON files
func (te *TextExtractor) extractJSON(data []byte) (*ExtractResult, error) {
	// Pretty-print JSON for readability
	var parsed interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		// If invalid JSON, treat as plain text
		return te.extractPlainText(data)
	}

	// Convert to readable text representation
	content := jsonToText(parsed, 0)

	// Try to extract a title
	title := ""
	if obj, ok := parsed.(map[string]interface{}); ok {
		if t, ok := obj["title"].(string); ok {
			title = t
		} else if t, ok := obj["name"].(string); ok {
			title = t
		}
	}

	return &ExtractResult{
		Content:  content,
		Title:    title,
		Metadata: make(map[string]interface{}),
	}, nil
}

// extractCSV handles CSV files
func (te *TextExtractor) extractCSV(data []byte) (*ExtractResult, error) {
	content := string(data)
	content = normalizeWhitespace(content)

	// Convert CSV to more readable format
	lines := strings.Split(content, "\n")
	var result strings.Builder

	// Get headers from first line
	var headers []string
	if len(lines) > 0 {
		headers = parseCSVLine(lines[0])
		result.WriteString("Columns: ")
		result.WriteString(strings.Join(headers, ", "))
		result.WriteString("\n\n")
	}

	// Convert data rows to "Column: Value" format for better embedding
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		values := parseCSVLine(line)
		result.WriteString(fmt.Sprintf("Row %d:\n", i+1))
		for j, val := range values {
			if j < len(headers) {
				result.WriteString(fmt.Sprintf("  %s: %s\n", headers[j], val))
			} else {
				result.WriteString(fmt.Sprintf("  Column %d: %s\n", j+1, val))
			}
		}
		result.WriteString("\n")
	}

	return &ExtractResult{
		Content:  result.String(),
		Title:    "",
		Metadata: map[string]interface{}{"columns": headers, "row_count": len(lines) - 1},
	}, nil
}

// extractHTML handles HTML files
func (te *TextExtractor) extractHTML(data []byte) (*ExtractResult, error) {
	content := string(data)

	// Extract title from <title> tag
	title := ""
	titleRegex := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	if match := titleRegex.FindStringSubmatch(content); len(match) > 1 {
		title = strings.TrimSpace(match[1])
	}

	// Strip HTML tags
	plainContent := htmlToPlainText(content)

	return &ExtractResult{
		Content:  plainContent,
		Title:    title,
		Metadata: make(map[string]interface{}),
	}, nil
}

// extractPDF handles PDF files using a simple built-in extractor
// This works for basic text-based PDFs with FlateDecode compression
// It will NOT work for: scanned PDFs, custom fonts, encrypted PDFs
func (te *TextExtractor) extractPDF(data []byte) (*ExtractResult, error) {
	text, err := extractPDFText(data)
	if err != nil {
		return nil, fmt.Errorf("PDF extraction failed: %w", err)
	}

	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("no text content found in PDF (may be scanned/image-based)")
	}

	// Try to extract title from first non-empty line
	title := ""
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 0 && len(line) < 100 {
			title = line
			break
		}
	}

	return &ExtractResult{
		Content:  normalizeWhitespace(text),
		Title:    title,
		Metadata: map[string]interface{}{"format": "pdf"},
	}, nil
}

// extractPDFText extracts text from a PDF file
// Simple implementation that handles basic PDFs with FlateDecode streams
func extractPDFText(data []byte) (string, error) {
	// Find all stream objects and extract text
	var allText strings.Builder

	// Find stream...endstream blocks
	streamRegex := regexp.MustCompile(`(?s)stream\r?\n(.+?)\r?\nendstream`)
	matches := streamRegex.FindAllSubmatch(data, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		streamData := match[1]

		// Try to decompress (most PDFs use FlateDecode/zlib)
		decompressed, err := decompressStream(streamData)
		if err != nil {
			// If decompression fails, try using raw data (uncompressed stream)
			decompressed = streamData
		}

		// Extract text from the content stream
		text := extractTextFromContentStream(decompressed)
		if text != "" {
			allText.WriteString(text)
			allText.WriteString("\n")
		}
	}

	return allText.String(), nil
}

// decompressStream attempts to decompress a PDF stream using zlib
func decompressStream(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return decompressed, nil
}

// extractTextFromContentStream parses PDF content stream operators to extract text
func extractTextFromContentStream(data []byte) string {
	var text strings.Builder
	content := string(data)

	// Extract text from Tj operator: (Hello World) Tj or <48656c6c6f> Tj
	tjRegex := regexp.MustCompile(`\(([^)]*)\)\s*Tj`)
	for _, match := range tjRegex.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			text.WriteString(decodePDFString(match[1]))
		}
	}
	// Hex string version
	tjHexRegex := regexp.MustCompile(`<([0-9A-Fa-f]+)>\s*Tj`)
	for _, match := range tjHexRegex.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			text.WriteString(decodeHexString(match[1]))
		}
	}

	// Extract text from TJ operator with smart spacing for word boundaries
	tjArrayRegex := regexp.MustCompile(`\[([^\]]+)\]\s*TJ`)
	for _, match := range tjArrayRegex.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			text.WriteString(parseTJArray(match[1]))
		}
	}

	// Extract text from ' operator (same as Tj but moves to next line): (text) '
	quoteRegex := regexp.MustCompile(`\(([^)]*)\)\s*'`)
	for _, match := range quoteRegex.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			text.WriteString(decodePDFString(match[1]))
			text.WriteString("\n")
		}
	}

	// Extract text from " operator: aw ac (text) "
	dquoteRegex := regexp.MustCompile(`[\d.-]+\s+[\d.-]+\s+\(([^)]*)\)\s*"`)
	for _, match := range dquoteRegex.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			text.WriteString(decodePDFString(match[1]))
			text.WriteString("\n")
		}
	}

	return text.String()
}

// parseTJArray parses a TJ array like [(Hello)-264(World)] and returns text with proper spacing
// Negative numbers with abs > 100 typically indicate word spaces
func parseTJArray(array string) string {
	var result strings.Builder

	// Match elements: strings (text), <hex>, or numbers
	elementRegex := regexp.MustCompile(`\(([^)]*)\)|<([0-9A-Fa-f]+)>|(-?[\d.]+)`)
	matches := elementRegex.FindAllStringSubmatch(array, -1)

	for _, match := range matches {
		if match[1] != "" {
			// Literal string
			result.WriteString(decodePDFString(match[1]))
		} else if match[2] != "" {
			// Hex string
			result.WriteString(decodeHexString(match[2]))
		} else if match[3] != "" {
			// Number - negative values with large magnitude indicate spaces
			var num float64
			fmt.Sscanf(match[3], "%f", &num)
			// Threshold: if adjustment is less than -100, add a space
			// (negative means moving right in PDF coordinates = space between words)
			if num < -100 {
				result.WriteString(" ")
			}
		}
	}

	return result.String()
}

// decodeHexString converts PDF hex string like "48656c6c6f" to "Hello"
func decodeHexString(hex string) string {
	// Remove any whitespace
	hex = strings.ReplaceAll(hex, " ", "")
	hex = strings.ReplaceAll(hex, "\n", "")
	hex = strings.ReplaceAll(hex, "\r", "")

	// Pad with 0 if odd length
	if len(hex)%2 != 0 {
		hex += "0"
	}

	var result strings.Builder
	for i := 0; i < len(hex); i += 2 {
		var b byte
		fmt.Sscanf(hex[i:i+2], "%02x", &b)
		if b > 0 {
			result.WriteByte(b)
		}
	}
	return result.String()
}

// decodePDFString decodes escape sequences in PDF strings
func decodePDFString(s string) string {
	// Handle common PDF escape sequences
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\r", "\r")
	s = strings.ReplaceAll(s, "\\t", "\t")
	s = strings.ReplaceAll(s, "\\(", "(")
	s = strings.ReplaceAll(s, "\\)", ")")
	s = strings.ReplaceAll(s, "\\\\", "\\")

	// Handle octal escapes like \000 to \377
	octalRegex := regexp.MustCompile(`\\([0-7]{1,3})`)
	s = octalRegex.ReplaceAllStringFunc(s, func(match string) string {
		var val int
		fmt.Sscanf(match, "\\%o", &val)
		if val > 0 && val < 256 {
			return string(rune(val))
		}
		return match
	})

	return s
}

// Helper functions

// normalizeWhitespace cleans up whitespace in text
func normalizeWhitespace(text string) string {
	// Normalize line endings
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// Remove excessive blank lines
	blankLineRegex := regexp.MustCompile(`\n{3,}`)
	text = blankLineRegex.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

// markdownToPlainText converts Markdown to plain text
func markdownToPlainText(md string) string {
	// Remove code blocks
	codeBlockRegex := regexp.MustCompile("(?s)```.*?```")
	md = codeBlockRegex.ReplaceAllString(md, "[code block]")

	// Remove inline code
	inlineCodeRegex := regexp.MustCompile("`[^`]+`")
	md = inlineCodeRegex.ReplaceAllStringFunc(md, func(s string) string {
		return strings.Trim(s, "`")
	})

	// Convert headers to plain text
	headerRegex := regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	md = headerRegex.ReplaceAllString(md, "$1")

	// Remove bold/italic markers
	md = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(md, "$1")
	md = regexp.MustCompile(`\*([^*]+)\*`).ReplaceAllString(md, "$1")
	md = regexp.MustCompile(`__([^_]+)__`).ReplaceAllString(md, "$1")
	md = regexp.MustCompile(`_([^_]+)_`).ReplaceAllString(md, "$1")

	// Convert links to just text
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	md = linkRegex.ReplaceAllString(md, "$1")

	// Remove images
	imgRegex := regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	md = imgRegex.ReplaceAllString(md, "[image: $1]")

	// Clean up list markers
	md = regexp.MustCompile(`(?m)^[\*\-\+]\s+`).ReplaceAllString(md, "- ")
	md = regexp.MustCompile(`(?m)^\d+\.\s+`).ReplaceAllString(md, "- ")

	return normalizeWhitespace(md)
}

// htmlToPlainText strips HTML tags and extracts text
func htmlToPlainText(html string) string {
	// Remove script and style elements
	scriptRegex := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	html = scriptRegex.ReplaceAllString(html, "")
	styleRegex := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = styleRegex.ReplaceAllString(html, "")

	// Convert block elements to newlines
	blockRegex := regexp.MustCompile(`(?i)</(p|div|br|h[1-6]|li|tr)[^>]*>`)
	html = blockRegex.ReplaceAllString(html, "\n")

	// Remove all remaining tags
	tagRegex := regexp.MustCompile(`<[^>]+>`)
	html = tagRegex.ReplaceAllString(html, "")

	// Decode common HTML entities
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = strings.ReplaceAll(html, "&quot;", "\"")
	html = strings.ReplaceAll(html, "&#39;", "'")

	return normalizeWhitespace(html)
}

// jsonToText converts JSON to readable text
func jsonToText(v interface{}, depth int) string {
	indent := strings.Repeat("  ", depth)

	switch val := v.(type) {
	case map[string]interface{}:
		var lines []string
		for k, v := range val {
			childText := jsonToText(v, depth+1)
			if strings.Contains(childText, "\n") {
				lines = append(lines, fmt.Sprintf("%s%s:\n%s", indent, k, childText))
			} else {
				lines = append(lines, fmt.Sprintf("%s%s: %s", indent, k, childText))
			}
		}
		return strings.Join(lines, "\n")
	case []interface{}:
		var lines []string
		for i, v := range val {
			childText := jsonToText(v, depth+1)
			lines = append(lines, fmt.Sprintf("%s[%d] %s", indent, i, childText))
		}
		return strings.Join(lines, "\n")
	case string:
		return val
	case float64:
		return fmt.Sprintf("%v", val)
	case bool:
		return fmt.Sprintf("%v", val)
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// parseCSVLine parses a single CSV line (simple implementation)
func parseCSVLine(line string) []string {
	var values []string
	var current strings.Builder
	inQuotes := false

	for _, r := range line {
		switch r {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if inQuotes {
				current.WriteRune(r)
			} else {
				values = append(values, strings.TrimSpace(current.String()))
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	values = append(values, strings.TrimSpace(current.String()))

	return values
}
