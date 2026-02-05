package memory

import (
	"testing"
)

func TestExtractPDF_SimpleText(t *testing.T) {
	// A minimal PDF with uncompressed text stream
	// This is a hand-crafted minimal PDF for testing
	pdfData := []byte(`%PDF-1.4
1 0 obj
<< /Type /Catalog /Pages 2 0 R >>
endobj
2 0 obj
<< /Type /Pages /Kids [3 0 R] /Count 1 >>
endobj
3 0 obj
<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>
endobj
4 0 obj
<< /Length 44 >>
stream
BT
/F1 12 Tf
100 700 Td
(Hello World) Tj
ET
endstream
endobj
xref
0 5
trailer
<< /Size 5 /Root 1 0 R >>
startxref
0
%%EOF`)

	extractor := NewTextExtractor()

	// Test CanExtract
	if !extractor.CanExtract("application/pdf") {
		t.Error("CanExtract should return true for application/pdf")
	}

	// Test extraction
	result, err := extractor.Extract(pdfData, "application/pdf")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Content == "" {
		t.Error("Expected non-empty content")
	}

	// Check if "Hello World" was extracted
	if !stringContains(result.Content, "Hello World") {
		t.Errorf("Expected content to contain 'Hello World', got: %s", result.Content)
	}
}

func TestExtractPDF_TJArray(t *testing.T) {
	// Test TJ operator with array
	pdfData := []byte(`%PDF-1.4
stream
BT
[(Hello) ( ) (World)] TJ
ET
endstream
%%EOF`)

	extractor := NewTextExtractor()
	result, err := extractor.Extract(pdfData, "application/pdf")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if !stringContains(result.Content, "Hello") || !stringContains(result.Content, "World") {
		t.Errorf("Expected content to contain 'Hello' and 'World', got: %s", result.Content)
	}
}

func TestExtractPDF_EscapeSequences(t *testing.T) {
	// Test escape sequence decoding
	tests := []struct {
		input    string
		expected string
	}{
		{`Hello\nWorld`, "Hello\nWorld"},
		{`\(test\)`, "(test)"},
		{`back\\slash`, "back\\slash"},
	}

	for _, tt := range tests {
		result := decodePDFString(tt.input)
		if result != tt.expected {
			t.Errorf("decodePDFString(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestExtractPDF_HexStrings(t *testing.T) {
	// Test hex-encoded strings like <48656c6c6f> = "Hello"
	pdfData := []byte(`%PDF-1.4
stream
BT
<48656c6c6f> Tj
[<576f726c64> 40 <21>] TJ
ET
endstream
%%EOF`)

	extractor := NewTextExtractor()
	result, err := extractor.Extract(pdfData, "application/pdf")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// "Hello" = 48656c6c6f, "World" = 576f726c64, "!" = 21
	if !stringContains(result.Content, "Hello") {
		t.Errorf("Expected 'Hello' from hex <48656c6c6f>, got: %s", result.Content)
	}
	if !stringContains(result.Content, "World") {
		t.Errorf("Expected 'World' from hex <576f726c64>, got: %s", result.Content)
	}
}

func TestExtractPDF_EmptyPDF(t *testing.T) {
	// A PDF with no text content should return an error
	pdfData := []byte(`%PDF-1.4
stream
q
100 0 0 100 0 0 cm
Q
endstream
%%EOF`)

	extractor := NewTextExtractor()
	_, err := extractor.Extract(pdfData, "application/pdf")
	if err == nil {
		t.Error("Expected error for PDF with no text content")
	}
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
