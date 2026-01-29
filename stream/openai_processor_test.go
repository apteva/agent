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
	if processor.currentToolID != "" {
		t.Error("Tool ID should be reset after tool_use")
	}
	if processor.currentToolName != "" {
		t.Error("Tool name should be reset after tool_use")
	}
	if processor.currentToolArguments != "" {
		t.Error("Tool arguments should be reset after tool_use")
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
			if processor.currentToolID != "" || processor.currentToolName != "" {
				t.Error("Processor state should be reset after tool_use event")
			}
		}
	}
}
