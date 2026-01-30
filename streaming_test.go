package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apteva/agent/stream"
	"github.com/apteva/agent/tools"
)

func TestStreamProcessor(t *testing.T) {
	t.Run("MockStreamProcessor", func(t *testing.T) {
		events := []stream.StreamEvent{
			{Type: "start", Content: "Starting"},
			{Type: "content", Content: "Hello"},
			{Type: "content", Content: " world"},
			{Type: "stop", Content: ""},
		}

		processor := &MockStreamProcessor{Events: events}

		// Test processing each event
		for i, expectedEvent := range events {
			event, err := processor.ProcessLine(fmt.Sprintf("line_%d", i))
			AssertNil(t, err)
			AssertNotNil(t, event)
			AssertEqual(t, expectedEvent.Type, event.Type)
			AssertEqual(t, expectedEvent.Content, event.Content)
		}

		// Test completion
		AssertEqual(t, true, processor.IsComplete("any line"))
	})

	t.Run("StreamEventCreation", func(t *testing.T) {
		// Test basic event creation
		event := NewStreamEvent("content", "Hello world")
		AssertEqual(t, "content", event.Type)
		AssertEqual(t, "Hello world", event.Content)

		// Test tool use event
		toolInput := map[string]interface{}{
			"query": "test query",
			"count": 5,
		}
		toolEvent := NewToolUseEvent("tool_123", "search_tool", toolInput)
		AssertEqual(t, "tool_use", toolEvent.Type)
		AssertEqual(t, "tool_123", toolEvent.ToolID)
		AssertEqual(t, "search_tool", toolEvent.ToolName)
		AssertEqual(t, toolInput, toolEvent.ToolInput)

		// Test tool result event
		resultEvent := NewToolResultEvent("tool_123", "Search completed")
		AssertEqual(t, "tool_result", resultEvent.Type)
		AssertEqual(t, "tool_123", resultEvent.ToolID)
		AssertEqual(t, "Search completed", resultEvent.Content)
	})
}

func TestUnifiedStreamResponse(t *testing.T) {
	t.Run("BasicContentStreaming", func(t *testing.T) {
		events := []stream.StreamEvent{
			{Type: "start", Content: "Starting"},
			{Type: "content", Content: "Hello"},
			{Type: "content", Content: " world"},
			{Type: "stop", Content: ""},
		}

		processor := &MockStreamProcessor{Events: events}
		mockReader := strings.NewReader("line1\nline2\nline3\nline4")

		recorder := httptest.NewRecorder()
		err := stream.UnifiedStreamResponse(recorder, mockReader, processor)
		AssertNil(t, err)

		// Check response headers
		AssertEqual(t, "text/event-stream", recorder.Header().Get("Content-Type"))
		AssertEqual(t, "no-cache", recorder.Header().Get("Cache-Control"))

		// Check response body contains expected events
		body := recorder.Body.String()
		AssertContains(t, body, `"type":"start"`)
		AssertContains(t, body, `"type":"content"`)
		AssertContains(t, body, `"content":"Hello"`)
		AssertContains(t, body, `"content":" world"`)
		AssertContains(t, body, `"type":"stop"`)
	})

	t.Run("ToolExecutionStreaming", func(t *testing.T) {
		// Register a mock tool for testing
		RegisterMockTool("test_tool", "A test tool", map[string]interface{}{
			"result": "Tool executed successfully",
		})

		events := []stream.StreamEvent{
			{Type: "start", Content: "Starting"},
			{
				Type:      "tool_use",
				ToolID:    "tool_123",
				ToolName:  "test_tool",
				ToolInput: map[string]interface{}{"input": "test"},
				Content:   "Using tool test_tool",
			},
			{Type: "stop", Content: ""},
		}

		processor := &MockStreamProcessor{Events: events}
		mockReader := strings.NewReader("line1\nline2\nline3")

		recorder := httptest.NewRecorder()
		err := stream.UnifiedStreamResponse(recorder, mockReader, processor)
		AssertNil(t, err)

		body := recorder.Body.String()
		AssertContains(t, body, `"type":"tool_use"`)
		AssertContains(t, body, `"tool_name":"test_tool"`)
		AssertContains(t, body, `"tool_id":"tool_123"`)
		AssertContains(t, body, `"type":"tool_result"`)
	})

	t.Run("ToolExecutionError", func(t *testing.T) {
		// Register a mock tool that will fail
		mockTool := &MockTool{
			ToolName:        "failing_tool",
			ToolDescription: "A tool that fails",
			Schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"input": map[string]interface{}{
						"type": "string",
					},
				},
			},
			ExecuteError: fmt.Errorf("tool execution failed"),
		}

		registry := tools.GetGlobalRegistry()
		registry.RegisterTool(mockTool)

		events := []stream.StreamEvent{
			{
				Type:      "tool_use",
				ToolID:    "tool_456",
				ToolName:  "failing_tool",
				ToolInput: map[string]interface{}{"input": "test"},
				Content:   "Using failing tool",
			},
		}

		processor := &MockStreamProcessor{Events: events}
		mockReader := strings.NewReader("line1")

		recorder := httptest.NewRecorder()
		err := stream.UnifiedStreamResponse(recorder, mockReader, processor)
		AssertNil(t, err)

		body := recorder.Body.String()
		AssertContains(t, body, `"type":"tool_use"`)
		AssertContains(t, body, `"type":"tool_result"`)
		AssertContains(t, body, "Error:")
		AssertContains(t, body, "tool execution failed")
	})
}

func TestStreamingWithMessageSaver(t *testing.T) {
	testDB := NewTestDatabase(t)
	defer testDB.Close()

	saver := testDB.GetMessageSaver()
	threadID := CreateTestThread(t, saver)

	t.Run("StreamWithToolsAndSave", func(t *testing.T) {
		// Register a test tool
		RegisterMockTool("save_test_tool", "Tool for save testing", map[string]interface{}{
			"message": "Tool execution saved",
		})

		// Create mock provider
		provider := &MockProvider{
			StreamResponse: "mock stream data",
		}

		// Create events including tool use
		events := []stream.StreamEvent{
			{Type: "start", Content: "Starting"},
			{Type: "content", Content: "I'll use a tool."},
			{
				Type:      "tool_use",
				ToolID:    "tool_789",
				ToolName:  "save_test_tool",
				ToolInput: map[string]interface{}{"query": "test"},
				Content:   "Using save_test_tool",
			},
			{Type: "content", Content: " Task completed."},
			{Type: "stop", Content: ""},
		}

		processor := &MockStreamProcessor{Events: events}

		recorder := httptest.NewRecorder()
		model := "test-model"

		// Test the unified tool conversation
		customTools := []tools.ToolDefinition{}
		builtinTools := []interface{}{}

		err := stream.UnifiedToolConversationWithBuiltins(
			recorder, provider, processor, []stream.Message{},
			customTools, builtinTools, saver, threadID, &model, nil,
		)
		AssertNil(t, err)

		// Verify messages were saved
		messages, err := saver.GetThreadMessages(threadID)
		AssertNil(t, err)
		AssertEqual(t, true, len(messages) > 0)

		// Check that we have both assistant and user messages
		var assistantMsgs, userMsgs int
		for _, msg := range messages {
			if msg.Role == "assistant" {
				assistantMsgs++
			} else if msg.Role == "user" {
				userMsgs++
			}
		}

		AssertEqual(t, true, assistantMsgs > 0)
		AssertEqual(t, true, userMsgs > 0) // Tool results are saved as user messages
	})

	t.Run("StreamingErrorHandling", func(t *testing.T) {
		// Test with provider error
		provider := &MockProvider{
			Error: fmt.Errorf("provider connection failed"),
		}

		processor := &MockStreamProcessor{Events: []stream.StreamEvent{}}

		recorder := httptest.NewRecorder()

		err := stream.UnifiedToolConversationWithBuiltins(
			recorder, provider, processor, []stream.Message{},
			[]tools.ToolDefinition{}, []interface{}{}, saver, threadID, nil, nil,
		)
		AssertNotNil(t, err)
		AssertContains(t, err.Error(), "provider connection failed")
	})
}

func TestStreamEventParsing(t *testing.T) {
	t.Run("ParseStreamEventJSON", func(t *testing.T) {
		// Test parsing of typical stream event JSON
		eventJSON := `{"type":"content","content":"Hello world"}`

		var event stream.StreamEvent
		err := json.Unmarshal([]byte(eventJSON), &event)
		AssertNil(t, err)
		AssertEqual(t, "content", event.Type)
		AssertEqual(t, "Hello world", event.Content)
	})

	t.Run("ParseToolUseEventJSON", func(t *testing.T) {
		eventJSON := `{
			"type": "tool_use",
			"tool_id": "tool_123",
			"tool_name": "search",
			"tool_input": {"query": "test", "limit": 10},
			"content": "Searching..."
		}`

		var event stream.StreamEvent
		err := json.Unmarshal([]byte(eventJSON), &event)
		AssertNil(t, err)
		AssertEqual(t, "tool_use", event.Type)
		AssertEqual(t, "tool_123", event.ToolID)
		AssertEqual(t, "search", event.ToolName)
		AssertEqual(t, "Searching...", event.Content)
		AssertNotNil(t, event.ToolInput)
		AssertEqual(t, "test", event.ToolInput["query"])
		AssertEqual(t, float64(10), event.ToolInput["limit"]) // JSON numbers are float64
	})
}

func TestStreamResponseReader(t *testing.T) {
	t.Run("ReadStreamLines", func(t *testing.T) {
		streamData := `data: {"type":"start","content":"Starting"}

data: {"type":"content","content":"Hello"}

data: {"type":"content","content":" world"}

data: {"type":"stop","content":""}

`

		reader := strings.NewReader(streamData)
		scanner := bufio.NewScanner(reader)

		var lines []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				lines = append(lines, line)
			}
		}

		AssertEqual(t, 4, len(lines))
		AssertContains(t, lines[0], `"type":"start"`)
		AssertContains(t, lines[1], `"content":"Hello"`)
		AssertContains(t, lines[2], `"content":" world"`)
		AssertContains(t, lines[3], `"type":"stop"`)
	})

	t.Run("HandleMalformedStreamData", func(t *testing.T) {
		malformedData := `data: invalid json
data: {"type":"content","content":"valid"}
data: {"incomplete":
data: {"type":"stop","content":""}
`

		reader := strings.NewReader(malformedData)
		scanner := bufio.NewScanner(reader)

		var validLines int
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "data: {") && strings.HasSuffix(line, "}") {
				// Try to parse as JSON
				jsonPart := strings.TrimPrefix(line, "data: ")
				var event map[string]interface{}
				if json.Unmarshal([]byte(jsonPart), &event) == nil {
					validLines++
				}
			}
		}

		AssertEqual(t, 2, validLines) // Only 2 valid JSON lines
	})
}

func TestConcurrentStreaming(t *testing.T) {
	t.Run("MultipleStreamsParallel", func(t *testing.T) {
		testDB := NewTestDatabase(t)
		defer testDB.Close()

		saver := testDB.GetMessageSaver()

		// Create multiple streams concurrently
		numStreams := 3
		done := make(chan bool, numStreams)

		for i := 0; i < numStreams; i++ {
			go func(streamID int) {
				defer func() { done <- true }()

				threadID := CreateTestThread(t, saver)

				events := []stream.StreamEvent{
					{Type: "start", Content: "Starting"},
					{Type: "content", Content: fmt.Sprintf("Stream %d content", streamID)},
					{Type: "stop", Content: ""},
				}

				processor := &MockStreamProcessor{Events: events}
				provider := &MockProvider{StreamResponse: "mock data"}

				recorder := httptest.NewRecorder()
				err := stream.UnifiedToolConversationWithBuiltins(
					recorder, provider, processor, []stream.Message{},
					[]tools.ToolDefinition{}, []interface{}{}, saver, threadID, nil, nil,
				)

				if err != nil {
					t.Errorf("Stream %d failed: %v", streamID, err)
				}
			}(i)
		}

		// Wait for all streams to complete
		for i := 0; i < numStreams; i++ {
			<-done
		}
	})
}

func TestStreamingWithRealHTTPResponse(t *testing.T) {
	t.Run("HTTPStreamingResponse", func(t *testing.T) {
		// Create a test handler that simulates streaming
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			events := []stream.StreamEvent{
				{Type: "start", Content: "Starting"},
				{Type: "content", Content: "Hello"},
				{Type: "content", Content: " streaming"},
				{Type: "stop", Content: ""},
			}

			processor := &MockStreamProcessor{Events: events}
			mockReader := strings.NewReader("line1\nline2\nline3\nline4")

			err := stream.UnifiedStreamResponse(w, mockReader, processor)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		// Make request to streaming endpoint
		resp, err := http.Get(server.URL)
		AssertNil(t, err)
		defer resp.Body.Close()

		// Check headers
		AssertEqual(t, "text/event-stream", resp.Header.Get("Content-Type"))
		AssertEqual(t, "no-cache", resp.Header.Get("Cache-Control"))

		// Read response body
		body, err := io.ReadAll(resp.Body)
		AssertNil(t, err)

		bodyStr := string(body)
		AssertContains(t, bodyStr, `"type":"start"`)
		AssertContains(t, bodyStr, `"type":"content"`)
		AssertContains(t, bodyStr, `"content":"Hello"`)
		AssertContains(t, bodyStr, `"type":"stop"`)
	})
}