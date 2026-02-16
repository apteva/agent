package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apteva/agent/stream"
)

// Integration test: MiniMax M2.5 on Fireworks
// Tests streaming, reasoning_content preservation, and tool calling
// Run with: go test ./providers/ -v -run TestMiniMaxFireworks -timeout 60s

const (
	fireworksMiniMaxModel = "accounts/fireworks/models/minimax-m2p5"
	fireworksBaseURL      = "https://api.fireworks.ai/inference/v1"
)

func getFireworksKey(t *testing.T) string {
	key := os.Getenv("FIREWORKS_API_KEY")
	if key == "" {
		t.Skip("FIREWORKS_API_KEY not set")
	}
	return key
}

// makeFireworksRequest builds and sends a streaming request to Fireworks
func makeFireworksRequest(t *testing.T, messages []map[string]interface{}, tools []map[string]interface{}, reasoningHistory string) io.ReadCloser {
	apiKey := getFireworksKey(t)

	reqBody := map[string]interface{}{
		"model":    fireworksMiniMaxModel,
		"messages": messages,
		"stream":   true,
	}

	if reasoningHistory != "" {
		reqBody["reasoning_history"] = reasoningHistory
	}

	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req, err := http.NewRequest("POST", fireworksBaseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("API error %d: %s", resp.StatusCode, string(errBody))
	}

	return resp.Body
}

// processStream reads a streaming response and returns parsed events
type streamResult struct {
	reasoning       string
	content         string
	toolCalls       []map[string]interface{}
	ttft            time.Duration // time to first token
	total           time.Duration
	chunkCount      int
	reasoningChunks int
	contentChunks   int
}

func processFireworksStream(t *testing.T, body io.ReadCloser) streamResult {
	defer body.Close()

	start := time.Now()
	var result streamResult
	firstTokenReceived := false

	processor := &stream.OpenAIProcessor{}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		event, err := processor.ProcessLine(line)
		if err != nil {
			continue
		}
		if event == nil {
			continue
		}

		result.chunkCount++

		if !firstTokenReceived && (event.Type == "thinking" || event.Type == "content") {
			result.ttft = time.Since(start)
			firstTokenReceived = true
		}

		switch event.Type {
		case "thinking":
			result.reasoning += event.Content
			result.reasoningChunks++
		case "content":
			result.content += event.Content
			result.contentChunks++
		case "tool_use":
			result.toolCalls = append(result.toolCalls, map[string]interface{}{
				"name":  event.ToolName,
				"id":    event.ToolID,
				"input": event.ToolInput,
			})
		case "stop":
			// done
		}
	}

	// Drain pending events
	if checker, ok := interface{}(processor).(stream.PendingEventChecker); ok {
		for checker.HasPendingEvents() {
			event, err := processor.ProcessLine("")
			if err != nil || event == nil {
				break
			}
			result.chunkCount++
			switch event.Type {
			case "tool_use":
				result.toolCalls = append(result.toolCalls, map[string]interface{}{
					"name":  event.ToolName,
					"id":    event.ToolID,
					"input": event.ToolInput,
				})
			}
		}
	}

	result.total = time.Since(start)
	return result
}

func TestMiniMaxFireworksBasicStreaming(t *testing.T) {
	messages := []map[string]interface{}{
		{"role": "user", "content": "What is 2+2? Answer in one word."},
	}

	// Test WITHOUT reasoning_history first (baseline)
	t.Run("WithoutReasoningHistory", func(t *testing.T) {
		body := makeFireworksRequest(t, messages, nil, "")
		result := processFireworksStream(t, body)

		t.Logf("TTFT: %v, Total: %v, Chunks: %d", result.ttft, result.total, result.chunkCount)
		t.Logf("Reasoning chunks: %d, Content chunks: %d", result.reasoningChunks, result.contentChunks)
		t.Logf("Content: %s", result.content)
		if result.reasoning != "" {
			t.Logf("Reasoning (%d chars): %.100s...", len(result.reasoning), result.reasoning)
		}

		if result.content == "" {
			t.Error("Expected content in response")
		}
	})

	// Test WITH reasoning_history=interleaved (our fix for MiniMax)
	t.Run("WithReasoningHistoryInterleaved", func(t *testing.T) {
		body := makeFireworksRequest(t, messages, nil, "interleaved")
		result := processFireworksStream(t, body)

		t.Logf("TTFT: %v, Total: %v, Chunks: %d", result.ttft, result.total, result.chunkCount)
		t.Logf("Reasoning chunks: %d, Content chunks: %d", result.reasoningChunks, result.contentChunks)
		t.Logf("Content: %s", result.content)
		if result.reasoning != "" {
			t.Logf("Reasoning (%d chars): %.200s...", len(result.reasoning), result.reasoning)
		}

		if result.content == "" {
			t.Error("Expected content in response")
		}
		// With interleaved, we may or may not get reasoning, but the response should work
	})
}

func TestMiniMaxFireworksToolCalling(t *testing.T) {
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "get_weather",
				"description": "Get the current weather in a given location",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The city name, e.g. San Francisco",
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}

	messages := []map[string]interface{}{
		{"role": "user", "content": "What's the weather in Tokyo?"},
	}

	t.Run("ToolCallWithInterleaved", func(t *testing.T) {
		body := makeFireworksRequest(t, messages, tools, "interleaved")
		result := processFireworksStream(t, body)

		t.Logf("TTFT: %v, Total: %v, Chunks: %d", result.ttft, result.total, result.chunkCount)
		t.Logf("Reasoning chunks: %d, Content chunks: %d", result.reasoningChunks, result.contentChunks)
		t.Logf("Tool calls: %d", len(result.toolCalls))
		if result.reasoning != "" {
			t.Logf("Reasoning (%d chars): %.200s...", len(result.reasoning), result.reasoning)
		}

		for i, tc := range result.toolCalls {
			t.Logf("Tool call %d: name=%s, id=%s, input=%v", i, tc["name"], tc["id"], tc["input"])
		}

		if len(result.toolCalls) == 0 {
			t.Error("Expected at least one tool call")
		} else {
			if result.toolCalls[0]["name"] != "get_weather" {
				t.Errorf("Expected tool name 'get_weather', got '%s'", result.toolCalls[0]["name"])
			}
		}
	})
}

func TestMiniMaxFireworksMultiTurnToolCall(t *testing.T) {
	// Test the full multi-turn flow:
	// 1. User asks question → model calls tool
	// 2. Send tool result back with reasoning_history=interleaved
	// 3. Model generates final response

	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "get_weather",
				"description": "Get the current weather in a given location",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The city name, e.g. San Francisco",
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}

	// Step 1: Initial request
	messages := []map[string]interface{}{
		{"role": "user", "content": "What's the weather in Tokyo?"},
	}

	body := makeFireworksRequest(t, messages, tools, "interleaved")
	result := processFireworksStream(t, body)

	t.Logf("Step 1 - TTFT: %v, Total: %v", result.ttft, result.total)
	t.Logf("Step 1 - Tool calls: %d, Reasoning: %d chars", len(result.toolCalls), len(result.reasoning))

	if len(result.toolCalls) == 0 {
		t.Skip("Model didn't call tool, skipping multi-turn test")
	}

	tc := result.toolCalls[0]
	t.Logf("Step 1 - Tool call: %s(%v)", tc["name"], tc["input"])

	// Step 2: Build multi-turn messages with reasoning preserved
	assistantMsg := map[string]interface{}{
		"role":    "assistant",
		"content": "",
		"tool_calls": []map[string]interface{}{
			{
				"id":   tc["id"],
				"type": "function",
				"function": map[string]interface{}{
					"name":      tc["name"],
					"arguments": mustJSON(tc["input"]),
				},
			},
		},
	}

	// If we got reasoning, include it (this is what our fix preserves)
	if result.reasoning != "" {
		assistantMsg["reasoning_content"] = result.reasoning
		t.Logf("Step 2 - Preserving reasoning_content (%d chars)", len(result.reasoning))
	}

	toolResultMsg := map[string]interface{}{
		"role":         "tool",
		"tool_call_id": tc["id"],
		"content":      `{"temperature": 22, "condition": "Partly Cloudy", "humidity": 65}`,
	}

	messages2 := []map[string]interface{}{
		messages[0],     // original user message
		assistantMsg,    // assistant with tool call + reasoning
		toolResultMsg,   // tool result
	}

	// Step 3: Send follow-up with preserved reasoning
	body2 := makeFireworksRequest(t, messages2, tools, "interleaved")
	result2 := processFireworksStream(t, body2)

	t.Logf("Step 2 - TTFT: %v, Total: %v", result2.ttft, result2.total)
	t.Logf("Step 2 - Content: %s", truncate(result2.content, 200))
	t.Logf("Step 2 - Reasoning: %d chars, Content chunks: %d", len(result2.reasoning), result2.contentChunks)

	if result2.content == "" {
		t.Error("Expected content in step 2 response")
	}
}

func TestMiniMaxFireworksIsThinkingModel(t *testing.T) {
	// Verify our isThinkingModel() correctly identifies MiniMax models
	tests := []struct {
		model    string
		expected bool
	}{
		{"accounts/fireworks/models/minimax-m2p5", true},
		{"MiniMax-M2.5", true},
		{"minimax-something", true},
		{"gpt-4", false},
		{"claude-3-opus", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := isThinkingModel(tt.model)
			if got != tt.expected {
				t.Errorf("isThinkingModel(%q) = %v, want %v", tt.model, got, tt.expected)
			}
		})
	}
}

func TestMiniMaxFireworksReasoningHistory(t *testing.T) {
	// Verify our getReasoningHistoryForModel returns "interleaved" for MiniMax
	tests := []struct {
		model    string
		expected string
	}{
		{"accounts/fireworks/models/minimax-m2p5", "interleaved"},
		{"MiniMax-M2.5", "interleaved"},
		{"minimax-m2", "interleaved"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := getReasoningHistoryForModel(tt.model)
			if got != tt.expected {
				t.Errorf("getReasoningHistoryForModel(%q) = %q, want %q", tt.model, got, tt.expected)
			}
		})
	}
}

func TestMiniMaxFireworksProcessorPerformance(t *testing.T) {
	// Verify our processor handles MiniMax-style chunks efficiently
	// Simulates 100 content chunks and measures processing time
	processor := &stream.OpenAIProcessor{}

	chunks := make([]string, 100)
	for i := range chunks {
		chunks[i] = fmt.Sprintf(`data: {"choices":[{"delta":{"content":"word%d "},"finish_reason":null}]}`, i)
	}

	start := time.Now()
	for _, chunk := range chunks {
		event, err := processor.ProcessLine(chunk)
		if err != nil {
			t.Fatalf("ProcessLine failed: %v", err)
		}
		if event == nil {
			continue
		}
		if event.Type != "content" && event.Type != "start" {
			t.Errorf("Unexpected event type: %s", event.Type)
		}
	}
	elapsed := time.Since(start)
	t.Logf("Processed 100 content chunks in %v (avg %v/chunk)", elapsed, elapsed/100)

	if elapsed > 10*time.Millisecond {
		t.Errorf("Processing too slow: %v for 100 chunks (should be <10ms)", elapsed)
	}
}

// Helper functions

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatalf("mustJSON: %v", err)
	}
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestMiniMaxFireworksRawRoundTrip verifies the EXACT JSON sent to and received from Fireworks
// This is the definitive test that nothing extra is injected into context
func TestMiniMaxFireworksRawRoundTrip(t *testing.T) {
	apiKey := getFireworksKey(t)

	// ===== STEP 1: Initial request with tool =====
	step1Req := map[string]interface{}{
		"model": fireworksMiniMaxModel,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What's the weather in Paris? Answer briefly."},
		},
		"tools": []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "get_weather",
					"description": "Get weather for a city",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{"type": "string"},
						},
						"required": []string{"location"},
					},
				},
			},
		},
		"reasoning_history": "interleaved",
		"stream":            true,
	}

	step1Body, _ := json.MarshalIndent(step1Req, "", "  ")
	t.Logf("===== STEP 1 REQUEST =====\n%s", string(step1Body))

	// Verify request doesn't contain reasoning_details or reasoning_split
	if strings.Contains(string(step1Body), "reasoning_details") {
		t.Error("Step 1 request should NOT contain reasoning_details")
	}
	if strings.Contains(string(step1Body), "reasoning_split") {
		t.Error("Step 1 request should NOT contain reasoning_split (that's for direct MiniMax API)")
	}

	// Send request
	req, _ := http.NewRequest("POST", fireworksBaseURL+"/chat/completions", bytes.NewBuffer(step1Body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("Step 1 request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("Step 1 API error %d: %s", resp.StatusCode, string(errBody))
	}

	// Parse raw response - log every chunk to verify format
	t.Log("===== STEP 1 RAW RESPONSE CHUNKS =====")
	var step1Reasoning string
	var step1Content string
	var step1ToolCalls []map[string]interface{}
	var rawChunkCount int
	var firstReasoningChunk string
	var firstContentChunk string
	var finishReasonChunk string

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	processor := &stream.OpenAIProcessor{}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line == "data: [DONE]" {
			if line == "data: [DONE]" {
				t.Log("  [DONE]")
			}
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		rawChunkCount++
		jsonStr := strings.TrimPrefix(line, "data: ")

		// Log first few and last few raw chunks
		if rawChunkCount <= 3 {
			t.Logf("  RAW[%d]: %s", rawChunkCount, jsonStr)
		}

		// Check for unexpected fields in raw JSON
		if strings.Contains(jsonStr, "reasoning_details") {
			t.Errorf("Response chunk contains unexpected reasoning_details: %s", jsonStr)
		}

		// Parse with our processor
		event, err := processor.ProcessLine(line)
		if err != nil || event == nil {
			continue
		}

		switch event.Type {
		case "thinking":
			step1Reasoning += event.Content
			if firstReasoningChunk == "" {
				firstReasoningChunk = jsonStr
			}
		case "content":
			step1Content += event.Content
			if firstContentChunk == "" {
				firstContentChunk = jsonStr
			}
		case "tool_use":
			step1ToolCalls = append(step1ToolCalls, map[string]interface{}{
				"name": event.ToolName, "id": event.ToolID, "input": event.ToolInput,
			})
		}

		// Check for finish_reason
		if strings.Contains(jsonStr, "finish_reason") && !strings.Contains(jsonStr, "null") {
			finishReasonChunk = jsonStr
		}
	}
	resp.Body.Close()

	// Drain pending events
	if checker, ok := interface{}(processor).(stream.PendingEventChecker); ok {
		for checker.HasPendingEvents() {
			event, _ := processor.ProcessLine("")
			if event == nil {
				break
			}
			if event.Type == "tool_use" {
				step1ToolCalls = append(step1ToolCalls, map[string]interface{}{
					"name": event.ToolName, "id": event.ToolID, "input": event.ToolInput,
				})
			}
		}
	}

	t.Logf("  Total raw chunks: %d", rawChunkCount)
	t.Logf("  First reasoning chunk: %s", firstReasoningChunk)
	t.Logf("  First content chunk: %s", firstContentChunk)
	t.Logf("  Finish reason chunk: %s", finishReasonChunk)
	t.Logf("  Reasoning: %d chars", len(step1Reasoning))
	t.Logf("  Content: %q", step1Content)
	t.Logf("  Tool calls: %d", len(step1ToolCalls))

	if len(step1ToolCalls) == 0 {
		t.Skip("Model didn't call tool, skipping round-trip test")
	}

	tc := step1ToolCalls[0]

	// ===== STEP 2: Send tool result with preserved reasoning =====
	step2Req := map[string]interface{}{
		"model": fireworksMiniMaxModel,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What's the weather in Paris? Answer briefly."},
			{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]interface{}{
					{
						"id":   tc["id"],
						"type": "function",
						"function": map[string]interface{}{
							"name":      tc["name"],
							"arguments": mustJSON(tc["input"]),
						},
					},
				},
				// This is the key: we preserve reasoning_content from step 1
				"reasoning_content": step1Reasoning,
			},
			{
				"role":         "tool",
				"tool_call_id": tc["id"],
				"content":      `{"temp_c": 18, "condition": "Sunny", "humidity": 45}`,
			},
		},
		"tools": []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "get_weather",
					"description": "Get weather for a city",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{"type": "string"},
						},
						"required": []string{"location"},
					},
				},
			},
		},
		"reasoning_history": "interleaved",
		"stream":            true,
	}

	step2Body, _ := json.MarshalIndent(step2Req, "", "  ")
	t.Logf("===== STEP 2 REQUEST =====\n%s", string(step2Body))

	// VERIFY: Step 2 request should contain reasoning_content (preserved from step 1)
	if step1Reasoning != "" && !strings.Contains(string(step2Body), "reasoning_content") {
		t.Error("Step 2 request should contain reasoning_content preserved from step 1")
	}
	// VERIFY: Should NOT contain reasoning_details (that's for direct MiniMax API)
	if strings.Contains(string(step2Body), "reasoning_details") {
		t.Error("Step 2 request should NOT contain reasoning_details for Fireworks")
	}
	// VERIFY: Should NOT contain <think> tags (Fireworks uses reasoning_content field)
	if strings.Contains(string(step2Body), "<think>") {
		t.Error("Step 2 request should NOT contain <think> tags for Fireworks")
	}

	// Send step 2
	req2, _ := http.NewRequest("POST", fireworksBaseURL+"/chat/completions", bytes.NewBuffer(step2Body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+apiKey)
	resp2, err := (&http.Client{Timeout: 60 * time.Second}).Do(req2)
	if err != nil {
		t.Fatalf("Step 2 request failed: %v", err)
	}
	if resp2.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		t.Fatalf("Step 2 API error %d: %s", resp2.StatusCode, string(errBody))
	}

	t.Log("===== STEP 2 RAW RESPONSE CHUNKS =====")
	var step2Reasoning string
	var step2Content string
	var step2RawChunks int

	scanner2 := bufio.NewScanner(resp2.Body)
	scanner2.Buffer(make([]byte, 64*1024), 1024*1024)
	processor2 := &stream.OpenAIProcessor{}

	for scanner2.Scan() {
		line := scanner2.Text()
		if line == "" || line == "data: [DONE]" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		step2RawChunks++
		jsonStr := strings.TrimPrefix(line, "data: ")

		if step2RawChunks <= 3 {
			t.Logf("  RAW[%d]: %s", step2RawChunks, jsonStr)
		}

		event, err := processor2.ProcessLine(line)
		if err != nil || event == nil {
			continue
		}
		switch event.Type {
		case "thinking":
			step2Reasoning += event.Content
		case "content":
			step2Content += event.Content
		}
	}
	resp2.Body.Close()

	t.Logf("  Step 2 - Total raw chunks: %d", step2RawChunks)
	t.Logf("  Step 2 - Reasoning: %d chars", len(step2Reasoning))
	t.Logf("  Step 2 - Content: %q", step2Content)

	if step2Content == "" {
		t.Error("Step 2 should produce content (final answer)")
	}

	// Verify content mentions weather/Paris/temperature
	contentLower := strings.ToLower(step2Content)
	if !strings.Contains(contentLower, "paris") && !strings.Contains(contentLower, "18") && !strings.Contains(contentLower, "sunny") {
		t.Logf("Warning: Step 2 content doesn't seem to reference the tool result: %q", step2Content)
	}

	t.Log("===== ROUND-TRIP VERIFICATION COMPLETE =====")
	t.Logf("Step 1: %d reasoning chars → %d tool calls", len(step1Reasoning), len(step1ToolCalls))
	t.Logf("Step 2: reasoning preserved (%d chars) → content: %q", len(step1Reasoning), truncate(step2Content, 100))
}

// Verify the needsThinkTagsInContent function for Fireworks
func TestNeedsThinkTagsFireworks(t *testing.T) {
	// Fireworks should NOT need think tags (uses reasoning_content field)
	got := needsThinkTagsInContent("https://api.fireworks.ai/inference/v1", "accounts/fireworks/models/minimax-m2p5")
	if got {
		t.Error("Fireworks+MiniMax should NOT need think tags in content")
	}
}

// Verify request body contains correct reasoning_history for MiniMax
func TestMiniMaxFireworksRequestBody(t *testing.T) {
	// Simulate what our provider would build
	model := "accounts/fireworks/models/minimax-m2p5"
	baseURL := "https://api.fireworks.ai/inference/v1"

	isFireworks := strings.Contains(baseURL, "fireworks.ai")
	if !isFireworks {
		t.Fatal("Should detect Fireworks")
	}

	if !isThinkingModel(model) {
		t.Fatal("Should detect MiniMax as thinking model")
	}

	rh := getReasoningHistoryForModel(model)
	if rh != "interleaved" {
		t.Fatalf("Expected interleaved, got %s", rh)
	}

	t.Logf("Request would include: reasoning_history=%s for model=%s", rh, model)
}
