package stream

import (
	"encoding/json"
	"testing"

	"github.com/apteva/agent/config"
)

func defaultCompactionConfig() *config.CompactionConfig {
	return &config.CompactionConfig{
		Enabled:         true,
		TriggerRatio:    0.7,
		KeepRecent:      20,
		SummaryMaxTokens: 1500,
		Model:           "claude-haiku-4-5",
	}
}

func newTestCompactor(cfg *config.CompactionConfig, maxTokens int) *Compactor {
	return NewCompactor(cfg, maxTokens, nil, "")
}

func makeMessages(n int, contentLen int) []Message {
	msgs := make([]Message, n)
	content := make([]byte, contentLen)
	for i := range content {
		content[i] = 'a'
	}
	s := string(content)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = Message{Role: "user", Content: s}
		} else {
			msgs[i] = Message{Role: "assistant", Content: s}
		}
	}
	return msgs
}

// --- ShouldCompact tests ---

func TestShouldCompact_Disabled(t *testing.T) {
	cfg := defaultCompactionConfig()
	cfg.Enabled = false
	c := newTestCompactor(cfg, 100000)

	msgs := makeMessages(50, 4000) // way over threshold
	if c.ShouldCompact(msgs) {
		t.Error("ShouldCompact should return false when disabled")
	}
}

func TestShouldCompact_ZeroMaxTokens(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 0)

	msgs := makeMessages(50, 4000)
	if c.ShouldCompact(msgs) {
		t.Error("ShouldCompact should return false when maxTokens is 0")
	}
}

func TestShouldCompact_TooFewMessages(t *testing.T) {
	cfg := defaultCompactionConfig()
	cfg.KeepRecent = 20
	c := newTestCompactor(cfg, 1000) // very low threshold

	// 25 messages = KeepRecent(20) + 5, which is the boundary
	msgs := makeMessages(25, 4000)
	if c.ShouldCompact(msgs) {
		t.Error("ShouldCompact should return false when messages <= KeepRecent + 5")
	}

	// 26 messages = KeepRecent(20) + 6, should evaluate tokens
	msgs = makeMessages(26, 4000)
	if !c.ShouldCompact(msgs) {
		t.Error("ShouldCompact should evaluate tokens when messages > KeepRecent + 5")
	}
}

func TestShouldCompact_BelowThreshold(t *testing.T) {
	cfg := defaultCompactionConfig()
	cfg.TriggerRatio = 0.7
	// maxTokens=1000000, threshold=700000
	// 30 messages * ~25 tokens each = ~750 tokens, well below threshold
	c := newTestCompactor(cfg, 1000000)

	msgs := makeMessages(30, 100)
	if c.ShouldCompact(msgs) {
		t.Error("ShouldCompact should return false when tokens are below threshold")
	}
}

func TestShouldCompact_AboveThreshold(t *testing.T) {
	cfg := defaultCompactionConfig()
	cfg.TriggerRatio = 0.7
	// maxTokens=1000, threshold=700
	// 30 messages * ~1004 tokens each (4000 chars / 4) = ~30120 tokens
	c := newTestCompactor(cfg, 1000)

	msgs := makeMessages(30, 4000)
	if !c.ShouldCompact(msgs) {
		t.Error("ShouldCompact should return true when tokens exceed threshold")
	}
}

// --- Token estimation tests ---

func TestEstimateMessageTokens_String(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{Role: "user", Content: "Hello, how are you?"}
	tokens := c.estimateMessageTokens(&msg)

	// 4 base + len("Hello, how are you?")/4 = 4 + 4 = 8
	expected := 4 + len("Hello, how are you?")/4
	if tokens != expected {
		t.Errorf("Expected %d tokens, got %d", expected, tokens)
	}
}

func TestEstimateMessageTokens_EmptyString(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{Role: "user", Content: ""}
	tokens := c.estimateMessageTokens(&msg)

	if tokens != 4 { // base overhead only
		t.Errorf("Expected 4 tokens for empty string, got %d", tokens)
	}
}

func TestEstimateMessageTokens_ContentBlocks(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: "Here is the result"},
		},
	}
	tokens := c.estimateMessageTokens(&msg)

	// 4 base + (3 block overhead + len("Here is the result")/4)
	expected := 4 + 3 + len("Here is the result")/4
	if tokens != expected {
		t.Errorf("Expected %d tokens, got %d", expected, tokens)
	}
}

func TestEstimateMessageTokens_ToolUseBlock(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	input := map[string]interface{}{"query": "test search"}
	msg := Message{
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "tool_use", Name: "web_search", Input: input},
		},
	}
	tokens := c.estimateMessageTokens(&msg)

	inputJSON, _ := json.Marshal(input)
	expected := 4 + 3 + len("web_search")/4 + len(inputJSON)/4
	if tokens != expected {
		t.Errorf("Expected %d tokens, got %d", expected, tokens)
	}
}

func TestEstimateMessageTokens_ImageBlock(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "image"},
		},
	}
	tokens := c.estimateMessageTokens(&msg)

	// 4 base + 3 block overhead + 1000 for image
	if tokens != 4+3+1000 {
		t.Errorf("Expected %d tokens for image, got %d", 4+3+1000, tokens)
	}
}

func TestEstimateMessageTokens_MapBlocks(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	// Simulate []interface{} content (as parsed from JSON)
	msg := Message{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "Hello world",
			},
		},
	}
	tokens := c.estimateMessageTokens(&msg)

	expected := 4 + 3 + len("Hello world")/4
	if tokens != expected {
		t.Errorf("Expected %d tokens, got %d", expected, tokens)
	}
}

func TestEstimateTotalTokens(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msgs := []Message{
		{Role: "user", Content: "Hello"},           // 4 + 1 = 5
		{Role: "assistant", Content: "Hi there!"},   // 4 + 2 = 6
		{Role: "user", Content: "How are you?"},     // 4 + 3 = 7
	}
	total := c.estimateTotalTokens(msgs)
	expected := (4 + len("Hello")/4) + (4 + len("Hi there!")/4) + (4 + len("How are you?")/4)
	if total != expected {
		t.Errorf("Expected %d total tokens, got %d", expected, total)
	}
}

// --- extractTextContent tests ---

func TestExtractTextContent_String(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{Role: "user", Content: "Simple text message"}
	text := c.extractTextContent(msg)

	if text != "Simple text message" {
		t.Errorf("Expected 'Simple text message', got '%s'", text)
	}
}

func TestExtractTextContent_ContentBlocks(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: "First part"},
			{Type: "text", Text: "Second part"},
		},
	}
	text := c.extractTextContent(msg)

	if text != "First part Second part" {
		t.Errorf("Expected 'First part Second part', got '%s'", text)
	}
}

func TestExtractTextContent_ToolUseBlock(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "tool_use", Name: "web_search", Input: map[string]interface{}{"q": "test"}},
		},
	}
	text := c.extractTextContent(msg)

	if text == "" {
		t.Error("Expected tool use to produce text output")
	}
	if !contains(text, "web_search") {
		t.Errorf("Expected text to contain 'web_search', got '%s'", text)
	}
}

func TestExtractTextContent_ToolResultBlock(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "tool_result", Content: "Search returned 5 results"},
		},
	}
	text := c.extractTextContent(msg)

	if !contains(text, "Search returned 5 results") {
		t.Errorf("Expected text to contain tool result, got '%s'", text)
	}
}

func TestExtractTextContent_LongToolResult_Truncated(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	longResult := make([]byte, 1000)
	for i := range longResult {
		longResult[i] = 'x'
	}

	msg := Message{
		Role: "user",
		Content: []ContentBlock{
			{Type: "tool_result", Content: string(longResult)},
		},
	}
	text := c.extractTextContent(msg)

	// Should be truncated to 500 chars + "..."
	if len(text) > 600 { // some overhead from "[Tool Result: ...]"
		t.Errorf("Expected truncated text, got length %d", len(text))
	}
	if !contains(text, "...") {
		t.Error("Expected truncation marker '...'")
	}
}

func TestExtractTextContent_MapBlocks(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{
		Role: "assistant",
		Content: []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "Map-based text",
			},
			map[string]interface{}{
				"type": "tool_use",
				"name": "file_read",
				"input": map[string]interface{}{"path": "/tmp/test.txt"},
			},
		},
	}
	text := c.extractTextContent(msg)

	if !contains(text, "Map-based text") {
		t.Errorf("Expected text to contain 'Map-based text', got '%s'", text)
	}
	if !contains(text, "file_read") {
		t.Errorf("Expected text to contain 'file_read', got '%s'", text)
	}
}

func TestExtractTextContent_EmptyMessage(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msg := Message{Role: "user", Content: ""}
	text := c.extractTextContent(msg)
	if text != "" {
		t.Errorf("Expected empty string, got '%s'", text)
	}
}

// --- Compact split logic tests ---

func TestCompact_TooFewMessages_NoSplit(t *testing.T) {
	cfg := defaultCompactionConfig()
	cfg.KeepRecent = 20
	c := newTestCompactor(cfg, 100000)

	msgs := makeMessages(15, 100) // less than KeepRecent
	summary, remaining, err := c.Compact("test-thread", msgs)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if summary != "" {
		t.Error("Expected no summary when messages <= KeepRecent")
	}
	if len(remaining) != 15 {
		t.Errorf("Expected all 15 messages returned, got %d", len(remaining))
	}
}

// --- Default config tests ---

func TestNewCompactor_Defaults(t *testing.T) {
	cfg := &config.CompactionConfig{Enabled: true}
	c := NewCompactor(cfg, 100000, nil, "test-key")

	if c.config.TriggerRatio != 0.7 {
		t.Errorf("Expected default TriggerRatio 0.7, got %f", c.config.TriggerRatio)
	}
	if c.config.KeepRecent != 20 {
		t.Errorf("Expected default KeepRecent 20, got %d", c.config.KeepRecent)
	}
	if c.config.SummaryMaxTokens != 1500 {
		t.Errorf("Expected default SummaryMaxTokens 1500, got %d", c.config.SummaryMaxTokens)
	}
	if c.config.Model != "claude-haiku-4-5" {
		t.Errorf("Expected default Model 'claude-haiku-4-5', got '%s'", c.config.Model)
	}
}

func TestNewCompactor_CustomConfig(t *testing.T) {
	cfg := &config.CompactionConfig{
		Enabled:         true,
		TriggerRatio:    0.5,
		KeepRecent:      10,
		SummaryMaxTokens: 2000,
		Model:           "claude-sonnet-4-5",
	}
	c := NewCompactor(cfg, 50000, nil, "test-key")

	if c.config.TriggerRatio != 0.5 {
		t.Errorf("Expected TriggerRatio 0.5, got %f", c.config.TriggerRatio)
	}
	if c.config.KeepRecent != 10 {
		t.Errorf("Expected KeepRecent 10, got %d", c.config.KeepRecent)
	}
	if c.config.SummaryMaxTokens != 2000 {
		t.Errorf("Expected SummaryMaxTokens 2000, got %d", c.config.SummaryMaxTokens)
	}
	if c.config.Model != "claude-sonnet-4-5" {
		t.Errorf("Expected Model 'claude-sonnet-4-5', got '%s'", c.config.Model)
	}
	if c.maxTokens != 50000 {
		t.Errorf("Expected maxTokens 50000, got %d", c.maxTokens)
	}
}

// --- getMessageID tests ---

func TestGetMessageID(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	msgs := makeMessages(5, 100)

	id := c.getMessageID(msgs, 0)
	if id != "msg_0" {
		t.Errorf("Expected 'msg_0', got '%s'", id)
	}

	id = c.getMessageID(msgs, 4)
	if id != "msg_4" {
		t.Errorf("Expected 'msg_4', got '%s'", id)
	}

	// Out of bounds
	id = c.getMessageID(msgs, -1)
	if id != "" {
		t.Errorf("Expected empty for negative index, got '%s'", id)
	}

	id = c.getMessageID(msgs, 5)
	if id != "" {
		t.Errorf("Expected empty for out of bounds, got '%s'", id)
	}
}

// --- estimateMapBlockTokens tests ---

func TestEstimateMapBlockTokens_ToolResult(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	// String content
	block := map[string]interface{}{
		"type":    "tool_result",
		"content": "result text here",
	}
	tokens := c.estimateMapBlockTokens(block)
	expected := 3 + len("result text here")/4
	if tokens != expected {
		t.Errorf("Expected %d tokens, got %d", expected, tokens)
	}

	// Object content
	block2 := map[string]interface{}{
		"type":    "tool_result",
		"content": map[string]interface{}{"key": "value"},
	}
	tokens2 := c.estimateMapBlockTokens(block2)
	contentJSON, _ := json.Marshal(map[string]interface{}{"key": "value"})
	expected2 := 3 + len(contentJSON)/4
	if tokens2 != expected2 {
		t.Errorf("Expected %d tokens, got %d", expected2, tokens2)
	}
}

func TestEstimateMapBlockTokens_Image(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	block := map[string]interface{}{"type": "image"}
	tokens := c.estimateMapBlockTokens(block)
	if tokens != 3+1000 {
		t.Errorf("Expected %d tokens for image, got %d", 3+1000, tokens)
	}
}

func TestEstimateMapBlockTokens_Unknown(t *testing.T) {
	cfg := defaultCompactionConfig()
	c := newTestCompactor(cfg, 100000)

	block := map[string]interface{}{"type": "unknown_block_type"}
	tokens := c.estimateMapBlockTokens(block)
	if tokens != 3 { // base overhead only
		t.Errorf("Expected 3 tokens for unknown type, got %d", tokens)
	}
}

// --- Integration-style tests ---

func TestShouldCompact_TriggerRatioEdge(t *testing.T) {
	cfg := defaultCompactionConfig()
	cfg.TriggerRatio = 0.5
	cfg.KeepRecent = 5

	// Each message: 4 base + 100/4 = 29 tokens
	// 30 messages * 29 tokens = 870 tokens
	// maxTokens=2000, threshold=1000 → 870 < 1000 → no compaction
	c := newTestCompactor(cfg, 2000)
	msgs := makeMessages(30, 100)
	if c.ShouldCompact(msgs) {
		t.Error("Should not compact when below 50% threshold")
	}

	// maxTokens=1000, threshold=500 → 870 > 500 → compaction
	c2 := newTestCompactor(cfg, 1000)
	if !c2.ShouldCompact(msgs) {
		t.Error("Should compact when above 50% threshold")
	}
}

func TestShouldCompact_KeepRecentBoundary(t *testing.T) {
	cfg := defaultCompactionConfig()
	cfg.KeepRecent = 10
	cfg.TriggerRatio = 0.01 // very low threshold to ensure token check passes

	c := newTestCompactor(cfg, 100) // very low max

	// Exactly KeepRecent + 5 = 15 messages → should NOT compact
	msgs := makeMessages(15, 100)
	if c.ShouldCompact(msgs) {
		t.Error("Should not compact at exactly KeepRecent + 5 boundary")
	}

	// KeepRecent + 6 = 16 messages → should evaluate tokens
	msgs = makeMessages(16, 100)
	if !c.ShouldCompact(msgs) {
		t.Error("Should compact when above KeepRecent + 5 and tokens exceed threshold")
	}
}

// Helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
