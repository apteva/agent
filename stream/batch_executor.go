package stream

import (
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

// ExecuteParallel executes multiple tool calls in parallel with a concurrency limit
func (be *BatchExecutor) ExecuteParallel(toolCalls []ToolCall) []ToolResult {
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

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

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

			// Execute tool
			result, err := tool.Execute(tc.Input)
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

	wg.Wait()

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

// ExecuteCustomToolsParallel executes multiple custom tools in parallel
// Returns results in the same order as input toolCalls
func ExecuteCustomToolsParallel(toolCalls []ToolCall, threadID, taskID string) []ToolResult {
	if len(toolCalls) == 0 {
		return []ToolResult{}
	}

	// If only one tool, no need for parallel overhead
	if len(toolCalls) == 1 {
		return []ToolResult{executeCustomToolSync(toolCalls[0], threadID, taskID)}
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

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			results[idx] = executeCustomToolSync(call, threadID, taskID)
		}(i, tc)
	}

	wg.Wait()

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

// executeCustomToolSync executes a single custom tool synchronously
func executeCustomToolSync(tc ToolCall, threadID, taskID string) ToolResult {
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

	// Execute tool (non-streaming for parallel execution)
	// Note: We use Execute() not ExecuteStreaming() for parallel calls
	// because interleaving multiple streams is complex and not needed for most cases
	result, execErr := tool.Execute(tc.Input)
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
