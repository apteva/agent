package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
	"github.com/apteva/agent/tools"
)

// ToolCall represents a single tool invocation
type ToolCall struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Input            map[string]interface{} `json:"input"`
	ThoughtSignature string                 `json:"thought_signature,omitempty"` // For Gemini 3
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Content       string                 `json:"content"`
	ContentBlocks interface{}            `json:"content_blocks,omitempty"`
	Input         map[string]interface{} `json:"input"`
	Error         error                  `json:"-"`
	Duration      time.Duration          `json:"duration"`
}

// BatchExecutor executes multiple tool calls in parallel
type BatchExecutor struct {
	maxConcurrent int
	eventBus      *events.EventBus
}

// NewBatchExecutor creates a new batch executor
func NewBatchExecutor(maxConcurrent int) *BatchExecutor {
	if maxConcurrent <= 0 {
		maxConcurrent = 10 // Default
	}
	return &BatchExecutor{
		maxConcurrent: maxConcurrent,
		eventBus:      events.GetEventBus(),
	}
}

// ExecuteParallel executes multiple tool calls in parallel with a concurrency limit (no cancellation)
func (be *BatchExecutor) ExecuteParallel(toolCalls []ToolCall) []ToolResult {
	return be.ExecuteParallelWithContext(context.Background(), toolCalls)
}

// ExecuteParallelWithContext executes multiple tool calls in parallel with cancellation support
func (be *BatchExecutor) ExecuteParallelWithContext(ctx context.Context, toolCalls []ToolCall) []ToolResult {
	if len(toolCalls) == 0 {
		return []ToolResult{}
	}

	results := make([]ToolResult, len(toolCalls))
	var wg sync.WaitGroup

	// Create a semaphore to limit concurrency
	sem := make(chan struct{}, be.maxConcurrent)

	log.Printf("🔀 Executing %d tools in parallel (max concurrent: %d)", len(toolCalls), be.maxConcurrent)

	for i, call := range toolCalls {
		wg.Add(1)

		go func(idx int, tc ToolCall) {
			defer wg.Done()

			// Check for cancellation before acquiring semaphore
			select {
			case <-ctx.Done():
				results[idx] = ToolResult{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: "Error: request cancelled",
					Input:   tc.Input,
					Error:   ctx.Err(),
				}
				return
			default:
			}

			// Acquire semaphore (with cancellation)
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = ToolResult{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: "Error: request cancelled",
					Input:   tc.Input,
					Error:   ctx.Err(),
				}
				return
			}

			startTime := time.Now()

			// Publish tool invocation event
			if be.eventBus != nil {
				toolEvent := events.NewEvent(events.CategoryTool, events.TypeToolInvocation, events.LevelInfo).
					WithData("tool_name", tc.Name).
					WithData("tool_id", tc.ID).
					WithData("input", tc.Input).
					WithData("parallel", true)
				be.eventBus.Publish(toolEvent)
			}

			// Get tool
			tool, err := tools.GetTool(tc.Name)
			if err != nil {
				log.Printf("❌ Error getting tool %s: %v", tc.Name, err)
				results[idx] = ToolResult{
					ID:       tc.ID,
					Name:     tc.Name,
					Content:  fmt.Sprintf("Error: %s", err.Error()),
					Input:    tc.Input,
					Error:    err,
					Duration: time.Since(startTime),
				}

				// Publish error event
				if be.eventBus != nil {
					errorEvent := events.NewEvent(events.CategoryTool, events.TypeToolError, events.LevelError).
						WithData("tool_name", tc.Name).
						WithData("tool_id", tc.ID).
						WithError(err).
						WithDuration(startTime)
					be.eventBus.Publish(errorEvent)
				}
				return
			}

			// Execute tool with context for cancellation
			result, err := tools.ExecuteWithContext(ctx, tool, tc.Input)
			duration := time.Since(startTime)

			if err != nil {
				log.Printf("❌ Error executing tool %s: %v", tc.Name, err)
				results[idx] = ToolResult{
					ID:       tc.ID,
					Name:     tc.Name,
					Content:  fmt.Sprintf("Error: %s", err.Error()),
					Input:    tc.Input,
					Error:    err,
					Duration: duration,
				}

				// Publish error event
				if be.eventBus != nil {
					errorEvent := events.NewEvent(events.CategoryTool, events.TypeToolError, events.LevelError).
						WithData("tool_name", tc.Name).
						WithData("tool_id", tc.ID).
						WithError(err).
						WithDuration(startTime)
					be.eventBus.Publish(errorEvent)
				}
				return
			}

			// Success - convert result to JSON string
			resultJSON, _ := json.Marshal(result)
			resultStr := string(resultJSON)

			log.Printf("✅ Tool %s completed in %v", tc.Name, duration)

			results[idx] = ToolResult{
				ID:            tc.ID,
				Name:          tc.Name,
				Content:       resultStr,
				ContentBlocks: result, // Store raw result for structured data
				Input:         tc.Input,
				Error:         nil,
				Duration:      duration,
			}

			// Publish result event
			if be.eventBus != nil {
				resultEvent := events.NewEvent(events.CategoryTool, events.TypeToolResult, events.LevelInfo).
					WithData("tool_name", tc.Name).
					WithData("tool_id", tc.ID).
					WithData("result", sanitizeForEvent(result)).
					WithDuration(startTime).
					WithData("parallel", true)
				be.eventBus.Publish(resultEvent)
			}
		}(i, call)
	}

	// Wait with cancellation support
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All tools completed normally
	case <-ctx.Done():
		// Cancelled - wait briefly for goroutines to notice, then return what we have
		log.Printf("🛑 Parallel tool execution cancelled, returning partial results")
	}

	// Calculate total time for batch
	totalDuration := time.Duration(0)
	for _, result := range results {
		if result.Duration > totalDuration {
			totalDuration = result.Duration
		}
	}

	log.Printf("✅ Parallel execution complete: %d tools in %v", len(toolCalls), totalDuration)

	return results
}

// IsParallelToolsEnabled checks if parallel tools are enabled in config
func IsParallelToolsEnabled() bool {
	cfg := config.GetConfig()
	llmConfig := cfg.GetLLMConfig()

	// Default to true if not explicitly configured
	if llmConfig.ParallelTools == nil {
		return true
	}

	return llmConfig.ParallelTools.Enabled
}

// GetMaxConcurrent returns the max concurrent tool executions from config
func GetMaxConcurrent() int {
	cfg := config.GetConfig()
	llmConfig := cfg.GetLLMConfig()

	// Default to 10 if not configured
	if llmConfig.ParallelTools == nil {
		return 10
	}

	if llmConfig.ParallelTools.MaxConcurrent <= 0 {
		return 10
	}

	return llmConfig.ParallelTools.MaxConcurrent
}

// ToolStreamCallback is a callback for streaming tool output to the client
type ToolStreamCallback func(toolID string, event tools.ToolStreamEvent)

// ExecuteCustomToolsParallel executes multiple custom tools in parallel
// Returns results in the same order as input toolCalls
// For streaming support, use ExecuteCustomToolsWithStreaming instead
func ExecuteCustomToolsParallel(toolCalls []ToolCall, threadID, taskID string) []ToolResult {
	return ExecuteCustomToolsWithStreaming(toolCalls, threadID, taskID, nil)
}

// ExecuteCustomToolsWithStreaming executes custom tools with optional streaming support (no cancellation)
func ExecuteCustomToolsWithStreaming(toolCalls []ToolCall, threadID, taskID string, streamCallback ToolStreamCallback) []ToolResult {
	return ExecuteCustomToolsWithContext(context.Background(), toolCalls, threadID, taskID, streamCallback)
}

// ExecuteCustomToolsWithContext executes custom tools with cancellation and optional streaming support
// When streamCallback is provided, streaming tools will stream their output even in parallel
// The tool_id in each stream event allows the client to distinguish which tool the output is from
func ExecuteCustomToolsWithContext(ctx context.Context, toolCalls []ToolCall, threadID, taskID string, streamCallback ToolStreamCallback) []ToolResult {
	if len(toolCalls) == 0 {
		return []ToolResult{}
	}

	maxConcurrent := GetMaxConcurrent()
	results := make([]ToolResult, len(toolCalls))
	var wg sync.WaitGroup

	// Create a semaphore to limit concurrency
	sem := make(chan struct{}, maxConcurrent)

	log.Printf("🔀 Executing %d custom tools in parallel (max concurrent: %d)", len(toolCalls), maxConcurrent)
	batchStart := time.Now()

	for i, tc := range toolCalls {
		wg.Add(1)

		go func(idx int, call ToolCall) {
			defer wg.Done()

			// Check for cancellation before acquiring semaphore
			select {
			case <-ctx.Done():
				results[idx] = ToolResult{
					ID:      call.ID,
					Name:    call.Name,
					Content: "Error: request cancelled",
					Input:   call.Input,
					Error:   ctx.Err(),
				}
				return
			default:
			}

			// Acquire semaphore (with cancellation)
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = ToolResult{
					ID:      call.ID,
					Name:    call.Name,
					Content: "Error: request cancelled",
					Input:   call.Input,
					Error:   ctx.Err(),
				}
				return
			}

			// Check if tool supports streaming and we have a callback
			tool, err := tools.GetTool(call.Name)
			if err == nil && streamCallback != nil && tools.IsStreamingTool(tool) {
				log.Printf("🔀 Tool %s supports streaming - using streaming execution", call.Name)
				results[idx] = executeCustomToolWithStreamingCtx(ctx, call, threadID, taskID, streamCallback)
			} else {
				results[idx] = executeCustomToolSyncCtx(ctx, call, threadID, taskID)
			}
		}(i, tc)
	}

	// Wait with cancellation support
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All tools completed normally
	case <-ctx.Done():
		log.Printf("🛑 Custom tool parallel execution cancelled, returning partial results")
	}

	// Calculate max duration (parallel time)
	maxDuration := time.Duration(0)
	for _, result := range results {
		if result.Duration > maxDuration {
			maxDuration = result.Duration
		}
	}

	log.Printf("✅ Parallel custom tool execution complete: %d tools in %v (max single: %v)",
		len(toolCalls), time.Since(batchStart), maxDuration)

	return results
}

// executeCustomToolSync executes a single custom tool synchronously (no cancellation)
func executeCustomToolSync(tc ToolCall, threadID, taskID string) ToolResult {
	return executeCustomToolSyncCtx(context.Background(), tc, threadID, taskID)
}

// executeCustomToolSyncCtx executes a single custom tool with cancellation support
func executeCustomToolSyncCtx(ctx context.Context, tc ToolCall, threadID, taskID string) ToolResult {
	startTime := time.Now()
	eventBus := events.GetEventBus()

	// Publish tool invocation event
	toolEvent := events.NewEvent(events.CategoryTool, events.TypeToolInvocation, events.LevelInfo).
		WithThread(threadID).
		WithData("tool_name", tc.Name).
		WithData("tool_id", tc.ID).
		WithData("input", tc.Input).
		WithData("parallel", true)
	eventBus.Publish(toolEvent)

	// Get tool
	tool, err := tools.GetTool(tc.Name)
	if err != nil {
		log.Printf("❌ Error getting tool %s: %v", tc.Name, err)

		// Publish error event
		errorEvent := events.NewEvent(events.CategoryTool, events.TypeToolError, events.LevelError).
			WithThread(threadID).
			WithData("tool_name", tc.Name).
			WithData("tool_id", tc.ID).
			WithError(err).
			WithDuration(startTime)
		eventBus.Publish(errorEvent)

		return ToolResult{
			ID:       tc.ID,
			Name:     tc.Name,
			Content:  fmt.Sprintf("Error: %s", err.Error()),
			Input:    tc.Input,
			Error:    err,
			Duration: time.Since(startTime),
		}
	}

	// Inject thread context into tool input for tools that need parent thread awareness
	if threadID != "" {
		tc.Input["_thread_id"] = threadID
	}

	// Inject per-request test mode so agent call tools can propagate it
	if tools.IsTestModeActive(ctx) {
		tc.Input["_test_mode"] = true
	}

	// Execute tool with context for cancellation
	result, execErr := tools.ExecuteWithContext(ctx, tool, tc.Input)
	duration := time.Since(startTime)

	if execErr != nil {
		log.Printf("❌ Error executing tool %s: %v", tc.Name, execErr)

		// Publish error event
		errorEvent := events.NewEvent(events.CategoryTool, events.TypeToolError, events.LevelError).
			WithThread(threadID).
			WithData("tool_name", tc.Name).
			WithData("tool_id", tc.ID).
			WithError(execErr).
			WithDuration(startTime)
		eventBus.Publish(errorEvent)

		return ToolResult{
			ID:       tc.ID,
			Name:     tc.Name,
			Content:  fmt.Sprintf("Error: %s", execErr.Error()),
			Input:    tc.Input,
			Error:    execErr,
			Duration: duration,
		}
	}

	// Success - convert result to JSON string
	resultJSON, _ := json.Marshal(result)
	resultStr := string(resultJSON)

	log.Printf("✅ Tool %s completed in %v (parallel)", tc.Name, duration)

	// Publish result event
	resultEvent := events.NewEvent(events.CategoryTool, events.TypeToolResult, events.LevelInfo).
		WithThread(threadID).
		WithData("tool_name", tc.Name).
		WithData("tool_id", tc.ID).
		WithData("result", sanitizeForEvent(result)).
		WithDuration(startTime).
		WithData("parallel", true)
	eventBus.Publish(resultEvent)

	return ToolResult{
		ID:            tc.ID,
		Name:          tc.Name,
		Content:       resultStr,
		ContentBlocks: result,
		Input:         tc.Input,
		Error:         nil,
		Duration:      duration,
	}
}

// executeCustomToolWithStreaming executes a single custom tool with streaming support (no cancellation)
func executeCustomToolWithStreaming(tc ToolCall, threadID, taskID string, streamCallback ToolStreamCallback) ToolResult {
	return executeCustomToolWithStreamingCtx(context.Background(), tc, threadID, taskID, streamCallback)
}

// executeCustomToolWithStreamingCtx executes a single custom tool with streaming and cancellation support
func executeCustomToolWithStreamingCtx(ctx context.Context, tc ToolCall, threadID, taskID string, streamCallback ToolStreamCallback) ToolResult {
	startTime := time.Now()
	eventBus := events.GetEventBus()

	// Publish tool invocation event
	toolEvent := events.NewEvent(events.CategoryTool, events.TypeToolInvocation, events.LevelInfo).
		WithThread(threadID).
		WithData("tool_name", tc.Name).
		WithData("tool_id", tc.ID).
		WithData("input", tc.Input).
		WithData("streaming", true)
	eventBus.Publish(toolEvent)

	// Get tool
	tool, err := tools.GetTool(tc.Name)
	if err != nil {
		log.Printf("❌ Error getting tool %s: %v", tc.Name, err)
		return ToolResult{
			ID:       tc.ID,
			Name:     tc.Name,
			Content:  fmt.Sprintf("Error: %s", err.Error()),
			Input:    tc.Input,
			Error:    err,
			Duration: time.Since(startTime),
		}
	}

	// Create callback wrapper that forwards to the stream callback
	toolCallback := func(event tools.ToolStreamEvent) {
		if streamCallback != nil {
			streamCallback(tc.ID, event)
		}
	}

	// Inject thread context into tool input for tools that need parent thread awareness
	if threadID != "" {
		tc.Input["_thread_id"] = threadID
	}

	// Inject per-request test mode so agent call tools can propagate it
	if tools.IsTestModeActive(ctx) {
		tc.Input["_test_mode"] = true
	}

	// Execute with streaming and context for cancellation
	result, execErr := tools.ExecuteWithStreamingContext(ctx, tool, tc.Input, toolCallback)
	duration := time.Since(startTime)

	if execErr != nil {
		log.Printf("❌ Error executing streaming tool %s: %v", tc.Name, execErr)

		// Publish error event
		errorEvent := events.NewEvent(events.CategoryTool, events.TypeToolError, events.LevelError).
			WithThread(threadID).
			WithData("tool_name", tc.Name).
			WithData("tool_id", tc.ID).
			WithError(execErr).
			WithDuration(startTime)
		eventBus.Publish(errorEvent)

		return ToolResult{
			ID:       tc.ID,
			Name:     tc.Name,
			Content:  fmt.Sprintf("Error: %s", execErr.Error()),
			Input:    tc.Input,
			Error:    execErr,
			Duration: duration,
		}
	}

	// Success - convert result to JSON string
	resultJSON, _ := json.Marshal(result)
	resultStr := string(resultJSON)

	log.Printf("✅ Streaming tool %s completed in %v", tc.Name, duration)

	// Publish result event
	resultEvent := events.NewEvent(events.CategoryTool, events.TypeToolResult, events.LevelInfo).
		WithThread(threadID).
		WithData("tool_name", tc.Name).
		WithData("tool_id", tc.ID).
		WithData("result", sanitizeForEvent(result)).
		WithDuration(startTime).
		WithData("streaming", true)
	eventBus.Publish(resultEvent)

	return ToolResult{
		ID:            tc.ID,
		Name:          tc.Name,
		Content:       resultStr,
		ContentBlocks: result,
		Input:         tc.Input,
		Error:         nil,
		Duration:      duration,
	}
}
