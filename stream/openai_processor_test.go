package stream

import (
	"testing"
)

// TestOpenAIProcessorToolCallFlow tests the complete tool call flow
func TestOpenAIProcessorToolCallFlow(t *testing.T) {
	processor := &OpenAIProcessor{}

	// Simulate OpenAI streaming response with tool call
	testCases := []struct {
		name           string
		line           string
		expectedEvent  *StreamEvent
		expectedType   string
		shouldBeNil    bool
	}{
		{
			name:          "Start message",
			line:          `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			expectedType:  "start",
			shouldBeNil:   false,
		},
		{
			name:          "Content chunk",
			line:          `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"Let me use a tool."},"finish_reason":null}]}`,
			expectedType:  "content",
			shouldBeNil:   false,
		},
		{
			name:          "Tool call start (with name and ID)",
			line:          `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc123","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
			expectedType:  "tool_call",
			shouldBeNil:   false,
		},
		{
			name:          "Tool arguments chunk 1",
			line:          `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location"}}]},"finish_reason":null}]}`,
			expectedType:  "tool_input_delta",
			shouldBeNil:   false,
		},
		{
			name:          "Tool arguments chunk 2",
			line:          `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"Boston\"}"}}]},"finish_reason":null}]}`,
			expectedType:  "tool_input_delta",
			shouldBeNil:   false,
		},
		{
			name:          "Tool call complete",
			line:          `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			expectedType:  "tool_use",
			shouldBeNil:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := processor.ProcessLine(tc.line)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tc.shouldBeNil {
				if event != nil {
					t.Errorf("Expected nil event, got: %+v", event)
				}
				return
			}

			if event == nil {
				t.Fatal("Expected event, got nil")
			}

			if event.Type != tc.expectedType {
				t.Errorf("Expected event type %s, got %s", tc.expectedType, event.Type)
			}

			// Additional validations for specific event types
			switch tc.expectedType {
			case "tool_call":
				if event.ToolName != "get_weather" {
					t.Errorf("Expected tool name 'get_weather', got '%s'", event.ToolName)
				}
				if event.ToolID != "call_abc123" {
					t.Errorf("Expected tool ID 'call_abc123', got '%s'", event.ToolID)
				}
			case "tool_input_delta":
				if event.Content == "" {
					t.Error("Expected non-empty content for tool_input_delta")
				}
				if event.ToolID != "call_abc123" {
					t.Errorf("Expected tool ID 'call_abc123', got '%s'", event.ToolID)
				}
			case "tool_use":
				if event.ToolName != "get_weather" {
					t.Errorf("Expected tool name 'get_weather', got '%s'", event.ToolName)
				}
				if event.ToolID != "call_abc123" {
					t.Errorf("Expected tool ID 'call_abc123', got '%s'", event.ToolID)
				}
				if event.ToolInput == nil {
					t.Error("Expected non-nil tool input")
				} else {
					location, ok := event.ToolInput["location"].(string)
					if !ok || location != "Boston" {
						t.Errorf("Expected location 'Boston', got %v", event.ToolInput["location"])
					}
				}
			}
		})
	}
}

// TestOpenAIProcessorContentAfterToolCall tests that content after tool execution is properly separated
func TestOpenAIProcessorContentAfterToolCall(t *testing.T) {
	processor := &OpenAIProcessor{}

	// First tool call sequence
	toolCallLines := []string{
		`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"test_tool","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"key\":\"value\"}"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}

	eventTypes := []string{"start", "tool_call", "tool_input_delta", "tool_use"}

	for i, line := range toolCallLines {
		event, err := processor.ProcessLine(line)
		if err != nil {
			t.Fatalf("Line %d error: %v", i, err)
		}
		if event == nil {
			t.Fatalf("Line %d: expected event, got nil", i)
		}
		if event.Type != eventTypes[i] {
			t.Errorf("Line %d: expected type %s, got %s", i, eventTypes[i], event.Type)
		}
	}

	// Verify tool state was reset after tool_use
	if len(processor.toolIDs) != 0 {
		t.Error("Tool IDs should be empty after tool_use")
	}
}

// TestOpenAIProcessorStopEvent tests stop event handling
func TestOpenAIProcessorStopEvent(t *testing.T) {
	processor := &OpenAIProcessor{}

	testCases := []struct {
		name         string
		line         string
		expectedType string
	}{
		{
			name:         "Normal stop",
			line:         `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			expectedType: "stop",
		},
		{
			name:         "Tool calls finish without accumulated tool data",
			line:         `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			expectedType: "stop",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := processor.ProcessLine(tc.line)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if event == nil {
				t.Fatal("Expected event, got nil")
			}
			if event.Type != tc.expectedType {
				t.Errorf("Expected type %s, got %s", tc.expectedType, event.Type)
			}
		})
	}
}

// TestOpenAIProcessorDoneMarker tests [DONE] marker handling
func TestOpenAIProcessorDoneMarker(t *testing.T) {
	processor := &OpenAIProcessor{}

	event, err := processor.ProcessLine("data: [DONE]")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("Expected nil event for [DONE] marker, got: %+v", event)
	}

	if !processor.IsComplete("data: [DONE]") {
		t.Error("IsComplete should return true for [DONE] marker")
	}
}

// TestOpenAIProcessorMalformedJSON tests handling of malformed JSON
func TestOpenAIProcessorMalformedJSON(t *testing.T) {
	processor := &OpenAIProcessor{}

	testCases := []string{
		`data: {invalid json`,
		`data: {"choices": [{"delta": {incomplete`,
		`not a data line`,
		``,
	}

	for i, line := range testCases {
		t.Run(line, func(t *testing.T) {
			event, err := processor.ProcessLine(line)
			// Should either return nil event (skip) or error
			if event != nil && err == nil {
				t.Errorf("Case %d: Expected nil event or error for malformed line: %s", i, line)
			}
		})
	}
}

// TestOpenAIProcessorReasoningContent tests reasoning_content delta parsing and accumulation
func TestOpenAIProcessorReasoningContent(t *testing.T) {
	processor := &OpenAIProcessor{}

	// Simulate reasoning_content deltas (Fireworks Kimi K2 format)
	lines := []string{
		`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"Let me think about "},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"this problem carefully."},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"The answer is 42."},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}

	expectedTypes := []string{"start", "thinking", "thinking", "content", "stop"}

	for i, line := range lines {
		event, err := processor.ProcessLine(line)
		if err != nil {
			t.Fatalf("Line %d error: %v", i, err)
		}
		if event == nil {
			t.Fatalf("Line %d: expected event, got nil", i)
		}
		if event.Type != expectedTypes[i] {
			t.Errorf("Line %d: expected type %s, got %s", i, expectedTypes[i], event.Type)
		}
	}

	// Verify accumulated reasoning
	reasoning := processor.GetAccumulatedReasoning()
	expected := "Let me think about this problem carefully."
	if reasoning != expected {
		t.Errorf("Expected accumulated reasoning %q, got %q", expected, reasoning)
	}
}

// TestOpenAIProcessorReasoningField tests the "reasoning" field (Together AI format)
func TestOpenAIProcessorReasoningField(t *testing.T) {
	processor := &OpenAIProcessor{}

	lines := []string{
		`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"reasoning":"Thinking via reasoning field."},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"Done."},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}

	expectedTypes := []string{"start", "thinking", "content", "stop"}

	for i, line := range lines {
		event, err := processor.ProcessLine(line)
		if err != nil {
			t.Fatalf("Line %d error: %v", i, err)
		}
		if event == nil {
			t.Fatalf("Line %d: expected event, got nil", i)
		}
		if event.Type != expectedTypes[i] {
			t.Errorf("Line %d: expected type %s, got %s", i, expectedTypes[i], event.Type)
		}
	}

	reasoning := processor.GetAccumulatedReasoning()
	if reasoning != "Thinking via reasoning field." {
		t.Errorf("Expected accumulated reasoning from 'reasoning' field, got %q", reasoning)
	}
}

// TestOpenAIProcessorReasoningDetails tests reasoning_details delta parsing (MiniMax M2.5)
func TestOpenAIProcessorReasoningDetails(t *testing.T) {
	processor := &OpenAIProcessor{}

	// MiniMax M2.5 sends reasoning_details as structured entries
	lines := []string{
		`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","id":"reasoning-text-1","text":"Let me analyze this."}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"Here's my answer."},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}

	expectedTypes := []string{"start", "thinking", "content", "stop"}

	for i, line := range lines {
		event, err := processor.ProcessLine(line)
		if err != nil {
			t.Fatalf("Line %d error: %v", i, err)
		}
		if event == nil {
			t.Fatalf("Line %d: expected event, got nil", i)
		}
		if event.Type != expectedTypes[i] {
			t.Errorf("Line %d: expected type %s, got %s", i, expectedTypes[i], event.Type)
		}
		if expectedTypes[i] == "thinking" {
			if event.Content != "Let me analyze this." {
				t.Errorf("Expected thinking content 'Let me analyze this.', got %q", event.Content)
			}
		}
	}

	// Verify both accumulators
	reasoning := processor.GetAccumulatedReasoning()
	if reasoning != "Let me analyze this." {
		t.Errorf("Expected accumulated reasoning %q, got %q", "Let me analyze this.", reasoning)
	}

	details := processor.GetAccumulatedReasoningDetails()
	if len(details) != 1 {
		t.Fatalf("Expected 1 reasoning_detail entry, got %d", len(details))
	}
	if detailMap, ok := details[0].(map[string]interface{}); ok {
		if detailMap["type"] != "reasoning.text" {
			t.Errorf("Expected detail type 'reasoning.text', got %v", detailMap["type"])
		}
	} else {
		t.Error("Expected detail to be a map")
	}
}

// TestOpenAIProcessorThinkTags tests <think> tag parsing and accumulation
func TestOpenAIProcessorThinkTags(t *testing.T) {
	processor := &OpenAIProcessor{}

	// Simulate content with embedded <think> tags (Together AI K2.5 / MiniMax native format)
	lines := []string{
		`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"<think>Deep thought about the problem."},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"</think>The answer is clear."},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}

	expectedTypes := []string{"start", "thinking", "content", "stop"}

	for i, line := range lines {
		event, err := processor.ProcessLine(line)
		if err != nil {
			t.Fatalf("Line %d error: %v", i, err)
		}
		if event == nil {
			t.Fatalf("Line %d: expected event, got nil", i)
		}
		if event.Type != expectedTypes[i] {
			t.Errorf("Line %d: expected type %s, got %s", i, expectedTypes[i], event.Type)
		}
	}

	// Verify thinking was accumulated
	reasoning := processor.GetAccumulatedReasoning()
	if reasoning != "Deep thought about the problem." {
		t.Errorf("Expected accumulated reasoning from <think> tags, got %q", reasoning)
	}
}

// TestOpenAIProcessorReasoningWithToolCalls tests reasoning preservation across tool calls
func TestOpenAIProcessorReasoningWithToolCalls(t *testing.T) {
	processor := &OpenAIProcessor{}

	// Simulate: reasoning → tool call
	lines := []string{
		`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"I need to use a tool."},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\":\"NYC\"}"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}

	for _, line := range lines {
		_, err := processor.ProcessLine(line)
		if err != nil {
			t.Fatalf("Error: %v", err)
		}
	}

	// After tool call, reasoning should still be accumulated (not cleared)
	reasoning := processor.GetAccumulatedReasoning()
	if reasoning != "I need to use a tool." {
		t.Errorf("Expected reasoning to persist after tool call, got %q", reasoning)
	}

	// ResetForNewTurn should NOT clear reasoning
	processor.ResetForNewTurn()
	reasoning = processor.GetAccumulatedReasoning()
	if reasoning != "I need to use a tool." {
		t.Errorf("Expected reasoning to persist after ResetForNewTurn, got %q", reasoning)
	}

	// ClearAccumulatedReasoning should clear it
	processor.ClearAccumulatedReasoning()
	if processor.GetAccumulatedReasoning() != "" {
		t.Error("Expected empty reasoning after ClearAccumulatedReasoning")
	}
}

// TestOpenAIProcessorReasoningDetailsAccumulator tests the ReasoningDetailsAccumulator interface
func TestOpenAIProcessorReasoningDetailsAccumulator(t *testing.T) {
	processor := &OpenAIProcessor{}

	// Initially empty
	details := processor.GetAccumulatedReasoningDetails()
	if len(details) != 0 {
		t.Errorf("Expected empty details initially, got %d", len(details))
	}

	// Parse a reasoning_details delta
	line := `data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","text":"First thought."}]},"finish_reason":null}]}`
	processor.ProcessLine(`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`) // Start
	processor.ProcessLine(line)

	details = processor.GetAccumulatedReasoningDetails()
	if len(details) != 1 {
		t.Fatalf("Expected 1 detail, got %d", len(details))
	}

	// Clear should empty it
	processor.ClearAccumulatedReasoningDetails()
	if len(processor.GetAccumulatedReasoningDetails()) != 0 {
		t.Error("Expected empty details after Clear")
	}
}

// TestOpenAIProcessorMultipleToolCalls tests handling of multiple tool calls in sequence
func TestOpenAIProcessorMultipleToolCalls(t *testing.T) {
	processor := &OpenAIProcessor{}

	// First tool call
	lines1 := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"tool_1","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":1}"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}

	// Process first tool call
	for _, line := range lines1 {
		event, err := processor.ProcessLine(line)
		if err != nil {
			t.Fatalf("Error processing line: %v", err)
		}
		if event != nil && event.Type == "tool_use" {
			if event.ToolName != "tool_1" {
				t.Errorf("Expected tool_1, got %s", event.ToolName)
			}
			// Verify the processor state is reset
			if len(processor.toolIDs) != 0 {
				t.Error("Processor state should be reset after tool_use event")
			}
		}
	}
}
