package stream

import (
	"testing"
)

// mockMessageSaver records all SaveMessage calls in order for verification.
type mockMessageSaver struct {
	calls []mockSaveCall
}

type mockSaveCall struct {
	ThreadID string
	Role     string
	Content  interface{}
	Model    *string
	Metadata map[string]interface{}
}

func (m *mockMessageSaver) SaveMessage(threadID, role string, content interface{}, model *string, metadata map[string]interface{}) error {
	m.calls = append(m.calls, mockSaveCall{
		ThreadID: threadID,
		Role:     role,
		Content:  content,
		Model:    model,
		Metadata: metadata,
	})
	return nil
}

func (m *mockMessageSaver) CreateThread(title string) (string, error) {
	return "test-thread", nil
}

// TestDeferredToolResultSaveOrdering verifies that the saveOrDeferToolResult closure
// correctly defers tool result saves until after the consolidated assistant message is saved.
// This tests the core logic that prevents the DB ordering bug:
//   DB wrong: tool_result(user) → assistant(tool_use)
//   DB right: assistant(tool_use) → tool_result(user)
func TestDeferredToolResultSaveOrdering(t *testing.T) {
	t.Run("defers when consolidated not yet saved", func(t *testing.T) {
		saver := &mockMessageSaver{}

		// Simulate the deferred save state
		var consolidatedSaved bool
		var deferredToolResultSaves [][]interface{}
		pendingToolUseBlocks := []map[string]interface{}{
			{"type": "tool_use", "id": "call_1", "name": "mcp__tool1", "input": map[string]interface{}{}},
			{"type": "tool_use", "id": "call_2", "name": "mcp__tool2", "input": map[string]interface{}{}},
		}

		saveOrDeferToolResult := func(toolResultArray []interface{}, label string) {
			if consolidatedSaved || len(pendingToolUseBlocks) == 0 {
				saver.SaveMessage("thread1", "user", toolResultArray, nil, nil)
			} else {
				deferredToolResultSaves = append(deferredToolResultSaves, toolResultArray)
			}
		}

		flushDeferredToolResults := func() {
			for _, arr := range deferredToolResultSaves {
				saver.SaveMessage("thread1", "user", arr, nil, nil)
			}
			deferredToolResultSaves = nil
		}

		// Simulate: two MCP tool results arrive BEFORE consolidated save
		result1 := []interface{}{map[string]interface{}{
			"type": "tool_result", "tool_use_id": "call_1", "content": "result1",
		}}
		result2 := []interface{}{map[string]interface{}{
			"type": "tool_result", "tool_use_id": "call_2", "content": "result2",
		}}
		saveOrDeferToolResult(result1, "MCP")
		saveOrDeferToolResult(result2, "MCP")

		// Should NOT be saved yet
		if len(saver.calls) != 0 {
			t.Fatalf("Expected 0 saves before consolidated, got %d", len(saver.calls))
		}
		if len(deferredToolResultSaves) != 2 {
			t.Fatalf("Expected 2 deferred saves, got %d", len(deferredToolResultSaves))
		}

		// Now save consolidated assistant message (simulating the stop event)
		contentBlocks := []interface{}{
			map[string]interface{}{"type": "text", "text": "Let me use tools."},
		}
		for _, block := range pendingToolUseBlocks {
			contentBlocks = append(contentBlocks, block)
		}
		model := "minimax-m2p5"
		saver.SaveMessage("thread1", "assistant", contentBlocks, &model, nil)
		pendingToolUseBlocks = nil
		consolidatedSaved = true
		flushDeferredToolResults()

		// Verify order: assistant first, then tool results
		if len(saver.calls) != 3 {
			t.Fatalf("Expected 3 saves total, got %d", len(saver.calls))
		}
		if saver.calls[0].Role != "assistant" {
			t.Errorf("Expected first save to be assistant, got %s", saver.calls[0].Role)
		}
		if saver.calls[1].Role != "user" {
			t.Errorf("Expected second save to be user (tool_result), got %s", saver.calls[1].Role)
		}
		if saver.calls[2].Role != "user" {
			t.Errorf("Expected third save to be user (tool_result), got %s", saver.calls[2].Role)
		}

		// Verify assistant message has tool_use blocks
		assistantContent, ok := saver.calls[0].Content.([]interface{})
		if !ok {
			t.Fatal("Assistant content should be []interface{}")
		}
		if len(assistantContent) != 3 { // text + 2 tool_use blocks
			t.Errorf("Expected 3 content blocks in assistant message, got %d", len(assistantContent))
		}
	})

	t.Run("saves immediately when no pending tool use blocks", func(t *testing.T) {
		saver := &mockMessageSaver{}

		var consolidatedSaved bool
		var deferredToolResultSaves [][]interface{}
		var pendingToolUseBlocks []map[string]interface{} // empty

		saveOrDeferToolResult := func(toolResultArray []interface{}, label string) {
			if consolidatedSaved || len(pendingToolUseBlocks) == 0 {
				saver.SaveMessage("thread1", "user", toolResultArray, nil, nil)
			} else {
				deferredToolResultSaves = append(deferredToolResultSaves, toolResultArray)
			}
		}

		_ = consolidatedSaved
		_ = pendingToolUseBlocks

		// With no pending tool_use blocks, save immediately
		result := []interface{}{map[string]interface{}{
			"type": "tool_result", "tool_use_id": "call_1", "content": "result",
		}}
		saveOrDeferToolResult(result, "test")

		if len(saver.calls) != 1 {
			t.Fatalf("Expected immediate save, got %d saves", len(saver.calls))
		}
		if saver.calls[0].Role != "user" {
			t.Errorf("Expected user role, got %s", saver.calls[0].Role)
		}
	})

	t.Run("saves immediately after consolidated is saved", func(t *testing.T) {
		saver := &mockMessageSaver{}

		consolidatedSaved := true // Already saved
		var deferredToolResultSaves [][]interface{}
		pendingToolUseBlocks := []map[string]interface{}{
			{"type": "tool_use", "id": "call_1", "name": "tool", "input": map[string]interface{}{}},
		}

		saveOrDeferToolResult := func(toolResultArray []interface{}, label string) {
			if consolidatedSaved || len(pendingToolUseBlocks) == 0 {
				saver.SaveMessage("thread1", "user", toolResultArray, nil, nil)
			} else {
				deferredToolResultSaves = append(deferredToolResultSaves, toolResultArray)
			}
		}

		_ = deferredToolResultSaves

		result := []interface{}{map[string]interface{}{
			"type": "tool_result", "tool_use_id": "call_1", "content": "result",
		}}
		saveOrDeferToolResult(result, "test")

		if len(saver.calls) != 1 {
			t.Fatalf("Expected immediate save when consolidated already saved, got %d", len(saver.calls))
		}
	})

	t.Run("Fireworks multi-tool-call full flow", func(t *testing.T) {
		// Simulates a Fireworks MiniMax M2.5 response with:
		// - reasoning_content
		// - 3 parallel MCP tool calls
		// - consolidated assistant message saved at stop
		// - all 3 tool results flushed after
		saver := &mockMessageSaver{}

		var consolidatedSaved bool
		var deferredToolResultSaves [][]interface{}
		pendingToolUseBlocks := []map[string]interface{}{}

		saveOrDeferToolResult := func(toolResultArray []interface{}, label string) {
			if consolidatedSaved || len(pendingToolUseBlocks) == 0 {
				saver.SaveMessage("thread1", "user", toolResultArray, nil, nil)
			} else {
				deferredToolResultSaves = append(deferredToolResultSaves, toolResultArray)
			}
		}

		flushDeferredToolResults := func() {
			for _, arr := range deferredToolResultSaves {
				saver.SaveMessage("thread1", "user", arr, nil, nil)
			}
			deferredToolResultSaves = nil
		}

		// Step 1: Collect 3 tool_use blocks (as they arrive from stream)
		for i := 1; i <= 3; i++ {
			pendingToolUseBlocks = append(pendingToolUseBlocks, map[string]interface{}{
				"type":  "tool_use",
				"id":    "call_" + string(rune('0'+i)),
				"name":  "mcp__server__tool",
				"input": map[string]interface{}{"q": i},
			})
		}

		// Step 2: Each MCP tool executes inline and tries to save result
		for i := 1; i <= 3; i++ {
			result := []interface{}{map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "call_" + string(rune('0'+i)),
				"content":     "result for tool " + string(rune('0'+i)),
			}}
			saveOrDeferToolResult(result, "MCP")
		}

		// All should be deferred
		if len(saver.calls) != 0 {
			t.Fatalf("Expected 0 saves, got %d (tool results leaked before consolidated)", len(saver.calls))
		}
		if len(deferredToolResultSaves) != 3 {
			t.Fatalf("Expected 3 deferred, got %d", len(deferredToolResultSaves))
		}

		// Step 3: Stop event → save consolidated assistant message
		contentBlocks := []interface{}{
			map[string]interface{}{"type": "text", "text": "Using tools..."},
		}
		for _, block := range pendingToolUseBlocks {
			contentBlocks = append(contentBlocks, block)
		}
		model := "accounts/fireworks/models/minimax-m2p5"
		metadata := map[string]interface{}{"reasoning_content": "I need to search three things."}
		saver.SaveMessage("thread1", "assistant", contentBlocks, &model, metadata)
		pendingToolUseBlocks = nil
		consolidatedSaved = true
		flushDeferredToolResults()

		// Verify: 1 assistant + 3 tool results = 4 total
		if len(saver.calls) != 4 {
			t.Fatalf("Expected 4 saves total, got %d", len(saver.calls))
		}

		// First must be assistant
		if saver.calls[0].Role != "assistant" {
			t.Errorf("First save must be assistant, got %s", saver.calls[0].Role)
		}
		// Next 3 must be user (tool_result)
		for i := 1; i <= 3; i++ {
			if saver.calls[i].Role != "user" {
				t.Errorf("Save %d must be user, got %s", i, saver.calls[i].Role)
			}
		}

		// Verify reasoning metadata preserved
		if saver.calls[0].Metadata["reasoning_content"] != "I need to search three things." {
			t.Error("Expected reasoning_content in assistant metadata")
		}
	})

	t.Run("Anthropic sequential tool calls (no deferral needed)", func(t *testing.T) {
		// Anthropic sends tool_use events that are handled by the server side.
		// tool_result events come back from the API, not from inline execution.
		// In this case, pendingToolUseBlocks is empty when tool_result arrives
		// (because Anthropic saves differently), so no deferral is needed.
		saver := &mockMessageSaver{}

		var consolidatedSaved bool
		var deferredToolResultSaves [][]interface{}
		var pendingToolUseBlocks []map[string]interface{} // stays empty for Anthropic

		saveOrDeferToolResult := func(toolResultArray []interface{}, label string) {
			if consolidatedSaved || len(pendingToolUseBlocks) == 0 {
				saver.SaveMessage("thread1", "user", toolResultArray, nil, nil)
			} else {
				deferredToolResultSaves = append(deferredToolResultSaves, toolResultArray)
			}
		}

		_ = consolidatedSaved

		// In Anthropic flow, tool_result events are server tool results
		// that arrive with no pending tool_use blocks
		result := []interface{}{map[string]interface{}{
			"type": "tool_result", "tool_use_id": "toolu_1", "content": "result",
		}}
		saveOrDeferToolResult(result, "server/tool_result")

		// Should save immediately since pendingToolUseBlocks is empty
		if len(saver.calls) != 1 {
			t.Fatalf("Expected immediate save for Anthropic flow, got %d", len(saver.calls))
		}
	})

	t.Run("drain phase deferred saves flushed after drain consolidated", func(t *testing.T) {
		// When stop event is consumed by drain phase, consolidated save
		// happens after drain loop. Tool results during drain should also be deferred.
		saver := &mockMessageSaver{}

		var consolidatedSaved bool
		var deferredToolResultSaves [][]interface{}
		pendingToolUseBlocks := []map[string]interface{}{
			{"type": "tool_use", "id": "call_1", "name": "mcp__tool", "input": map[string]interface{}{}},
		}

		saveOrDeferToolResult := func(toolResultArray []interface{}, label string) {
			if consolidatedSaved || len(pendingToolUseBlocks) == 0 {
				saver.SaveMessage("thread1", "user", toolResultArray, nil, nil)
			} else {
				deferredToolResultSaves = append(deferredToolResultSaves, toolResultArray)
			}
		}

		flushDeferredToolResults := func() {
			for _, arr := range deferredToolResultSaves {
				saver.SaveMessage("thread1", "user", arr, nil, nil)
			}
			deferredToolResultSaves = nil
		}

		// Drain phase: tool result arrives before drain consolidated save
		result := []interface{}{map[string]interface{}{
			"type": "tool_result", "tool_use_id": "call_1", "content": "drain result",
		}}
		saveOrDeferToolResult(result, "MCP/drain")

		if len(saver.calls) != 0 {
			t.Fatal("Should defer during drain phase too")
		}

		// Drain consolidated save
		contentBlocks := []interface{}{
			map[string]interface{}{"type": "tool_use", "id": "call_1", "name": "mcp__tool", "input": map[string]interface{}{}},
		}
		model := "test-model"
		saver.SaveMessage("thread1", "assistant", contentBlocks, &model, nil)
		pendingToolUseBlocks = nil
		consolidatedSaved = true
		flushDeferredToolResults()

		if len(saver.calls) != 2 {
			t.Fatalf("Expected 2 saves, got %d", len(saver.calls))
		}
		if saver.calls[0].Role != "assistant" {
			t.Error("First save must be assistant")
		}
		if saver.calls[1].Role != "user" {
			t.Error("Second save must be user (tool_result)")
		}
	})
}
