package memory

import (
	"encoding/json"
	"fmt"
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
