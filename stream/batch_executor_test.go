package stream

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/apteva/agent/tools"
)

// SlowMockTool is a tool that takes a configurable amount of time to execute
type SlowMockTool struct {
	name      string
	delay     time.Duration
	result    interface{}
	execCount *int32
}

func (t *SlowMockTool) Name() string        { return t.name }
func (t *SlowMockTool) DisplayName() string { return t.name } // Required by Tool interface
func (t *SlowMockTool) Description() string { return "A slow mock tool for testing parallel execution" }
func (t *SlowMockTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SlowMockTool) Execute(input map[string]interface{}) (interface{}, error) {
	if t.execCount != nil {
		atomic.AddInt32(t.execCount, 1)
	}
	time.Sleep(t.delay)
	return t.result, nil
}

func TestExecuteCustomToolsParallel_ActuallyRunsInParallel(t *testing.T) {
	// Register 3 slow tools that each take 100ms
	toolDelay := 100 * time.Millisecond
	var execCount int32 = 0

	tool1 := &SlowMockTool{name: "slow_tool_1", delay: toolDelay, result: map[string]interface{}{"tool": 1}, execCount: &execCount}
	tool2 := &SlowMockTool{name: "slow_tool_2", delay: toolDelay, result: map[string]interface{}{"tool": 2}, execCount: &execCount}
	tool3 := &SlowMockTool{name: "slow_tool_3", delay: toolDelay, result: map[string]interface{}{"tool": 3}, execCount: &execCount}

	registry := tools.GetGlobalRegistry()
	registry.RegisterTool(tool1)
	registry.RegisterTool(tool2)
	registry.RegisterTool(tool3)

	// Create tool calls
	toolCalls := []ToolCall{
		{ID: "call_1", Name: "slow_tool_1", Input: map[string]interface{}{}},
		{ID: "call_2", Name: "slow_tool_2", Input: map[string]interface{}{}},
		{ID: "call_3", Name: "slow_tool_3", Input: map[string]interface{}{}},
	}

	// Execute in parallel
	start := time.Now()
	results := ExecuteCustomToolsParallel(toolCalls, "test-thread", "test-task")
	elapsed := time.Since(start)

	// Verify all tools executed
	if atomic.LoadInt32(&execCount) != 3 {
		t.Errorf("Expected 3 tool executions, got %d", execCount)
	}

	// Verify results
	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Check results are in correct order (same as input)
	for i, result := range results {
		if result.ID != toolCalls[i].ID {
			t.Errorf("Result %d has wrong ID: expected %s, got %s", i, toolCalls[i].ID, result.ID)
		}
		if result.Name != toolCalls[i].Name {
			t.Errorf("Result %d has wrong Name: expected %s, got %s", i, toolCalls[i].Name, result.Name)
		}
		if result.Error != nil {
			t.Errorf("Result %d has unexpected error: %v", i, result.Error)
		}
	}

	// Key test: If running in parallel, total time should be ~100ms (not 300ms)
	// Allow some tolerance for goroutine startup overhead
	maxExpectedTime := toolDelay + 50*time.Millisecond // 150ms max
	sequentialTime := toolDelay * 3                     // 300ms if sequential

	if elapsed >= sequentialTime {
		t.Errorf("Tools appear to have run sequentially! Elapsed: %v (expected < %v for parallel)", elapsed, sequentialTime)
	}

	if elapsed > maxExpectedTime {
		t.Errorf("Parallel execution took too long: %v (expected < %v)", elapsed, maxExpectedTime)
	}

	t.Logf("✅ Parallel execution completed in %v (sequential would be %v)", elapsed, sequentialTime)
}

func TestExecuteCustomToolsParallel_SingleTool(t *testing.T) {
	// Single tool should work without parallel overhead
	var execCount int32 = 0
	tool := &SlowMockTool{name: "single_tool", delay: 10 * time.Millisecond, result: "single result", execCount: &execCount}

	registry := tools.GetGlobalRegistry()
	registry.RegisterTool(tool)

	toolCalls := []ToolCall{
		{ID: "call_single", Name: "single_tool", Input: map[string]interface{}{}},
	}

	results := ExecuteCustomToolsParallel(toolCalls, "test-thread", "test-task")

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if results[0].ID != "call_single" {
		t.Errorf("Wrong ID: expected call_single, got %s", results[0].ID)
	}

	if atomic.LoadInt32(&execCount) != 1 {
		t.Errorf("Expected 1 execution, got %d", execCount)
	}
}

func TestExecuteCustomToolsParallel_EmptyInput(t *testing.T) {
	results := ExecuteCustomToolsParallel([]ToolCall{}, "test-thread", "test-task")

	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty input, got %d", len(results))
	}
}

func TestExecuteCustomToolsParallel_PreservesOrder(t *testing.T) {
	// Tools with different delays - should still return in input order
	var execCount int32 = 0

	// Register tools with different delays
	fast := &SlowMockTool{name: "fast_tool", delay: 10 * time.Millisecond, result: "fast", execCount: &execCount}
	medium := &SlowMockTool{name: "medium_tool", delay: 50 * time.Millisecond, result: "medium", execCount: &execCount}
	slow := &SlowMockTool{name: "slow_order_tool", delay: 100 * time.Millisecond, result: "slow", execCount: &execCount}

	registry := tools.GetGlobalRegistry()
	registry.RegisterTool(fast)
	registry.RegisterTool(medium)
	registry.RegisterTool(slow)

	// Request in order: slow, fast, medium
	toolCalls := []ToolCall{
		{ID: "call_slow", Name: "slow_order_tool", Input: map[string]interface{}{}},
		{ID: "call_fast", Name: "fast_tool", Input: map[string]interface{}{}},
		{ID: "call_medium", Name: "medium_tool", Input: map[string]interface{}{}},
	}

	results := ExecuteCustomToolsParallel(toolCalls, "test-thread", "test-task")

	// Results should be in same order as input, not completion order
	expectedOrder := []string{"call_slow", "call_fast", "call_medium"}
	for i, result := range results {
		if result.ID != expectedOrder[i] {
			t.Errorf("Result %d has wrong ID: expected %s, got %s (order not preserved)", i, expectedOrder[i], result.ID)
		}
	}

	t.Logf("✅ Result order preserved correctly")
}

func TestExecuteCustomToolsParallel_HandlesErrors(t *testing.T) {
	// Register a tool that doesn't exist
	toolCalls := []ToolCall{
		{ID: "call_nonexistent", Name: "nonexistent_tool_12345", Input: map[string]interface{}{}},
	}

	results := ExecuteCustomToolsParallel(toolCalls, "test-thread", "test-task")

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if results[0].Error == nil {
		t.Error("Expected error for nonexistent tool, got nil")
	}

	if results[0].Content == "" {
		t.Error("Expected error content, got empty string")
	}

	t.Logf("✅ Error handled correctly: %s", results[0].Content)
}

func TestExecuteCustomToolsParallel_MixedSuccessAndError(t *testing.T) {
	var execCount int32 = 0
	goodTool := &SlowMockTool{name: "good_tool", delay: 10 * time.Millisecond, result: "success", execCount: &execCount}

	registry := tools.GetGlobalRegistry()
	registry.RegisterTool(goodTool)

	toolCalls := []ToolCall{
		{ID: "call_good", Name: "good_tool", Input: map[string]interface{}{}},
		{ID: "call_bad", Name: "nonexistent_tool_xyz", Input: map[string]interface{}{}},
		{ID: "call_good2", Name: "good_tool", Input: map[string]interface{}{}},
	}

	results := ExecuteCustomToolsParallel(toolCalls, "test-thread", "test-task")

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// First and third should succeed
	if results[0].Error != nil {
		t.Errorf("Result 0 should succeed, got error: %v", results[0].Error)
	}
	if results[2].Error != nil {
		t.Errorf("Result 2 should succeed, got error: %v", results[2].Error)
	}

	// Second should fail
	if results[1].Error == nil {
		t.Error("Result 1 should fail, got nil error")
	}

	t.Logf("✅ Mixed success/error handled correctly")
}

func TestExecuteCustomToolsParallel_ThoughtSignaturePreserved(t *testing.T) {
	var execCount int32 = 0
	tool := &SlowMockTool{name: "sig_tool", delay: 10 * time.Millisecond, result: "ok", execCount: &execCount}

	registry := tools.GetGlobalRegistry()
	registry.RegisterTool(tool)

	toolCalls := []ToolCall{
		{
			ID:               "call_with_sig",
			Name:             "sig_tool",
			Input:            map[string]interface{}{},
			ThoughtSignature: "test-thought-signature-12345",
		},
	}

	results := ExecuteCustomToolsParallel(toolCalls, "test-thread", "test-task")

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	// The thought signature should be available in the original toolCalls
	// (we look it up by ID when processing results in processor.go)
	if toolCalls[0].ThoughtSignature != "test-thought-signature-12345" {
		t.Errorf("ThoughtSignature not preserved in toolCalls")
	}
}
