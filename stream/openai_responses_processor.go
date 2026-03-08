package stream

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// ==================== OpenAI Responses API Stream Processor ====================
//
// The Responses API uses a different SSE format from Chat Completions:
//   - Chat Completions: "data: {json}"
//   - Responses API:    "event: response.output_text.delta\ndata: {json}"
//
// Events come in pairs: an "event:" line followed by a "data:" line.

// OpenAIResponsesProcessor processes streaming events from the Responses API
type OpenAIResponsesProcessor struct {
	hasStarted           bool
	currentEventType     string // Tracks the SSE "event:" type for the next "data:" line
	responseID           string // For previous_response_id chaining
	pendingEvents        []*StreamEvent
	accumulatedReasoning string
	streamDone           bool

	// Function call accumulation (for custom tools)
	funcCallIDs   map[string]string // item_id -> call_id
	funcCallNames map[string]string // item_id -> function name
	funcCallArgs  map[string]string // item_id -> accumulated arguments JSON

	// Computer call tracking
	currentComputerCallID string
	computerActions       []interface{} // Accumulated actions for current computer_call
}

// NewOpenAIResponsesProcessor creates a new Responses API stream processor
func NewOpenAIResponsesProcessor() *OpenAIResponsesProcessor {
	return &OpenAIResponsesProcessor{
		funcCallIDs:   make(map[string]string),
		funcCallNames: make(map[string]string),
		funcCallArgs:  make(map[string]string),
	}
}

// GetResponseID returns the response ID for conversation chaining
func (p *OpenAIResponsesProcessor) GetResponseID() string {
	return p.responseID
}

// ProcessLine handles one line of SSE output from the Responses API
func (p *OpenAIResponsesProcessor) ProcessLine(line string) (*StreamEvent, error) {
	// Emit queued events first
	if len(p.pendingEvents) > 0 {
		event := p.pendingEvents[0]
		p.pendingEvents = p.pendingEvents[1:]
		return event, nil
	}

	// Handle SSE "event:" lines — store the event type for the next "data:" line
	if strings.HasPrefix(line, "event: ") {
		p.currentEventType = strings.TrimPrefix(line, "event: ")
		return nil, nil
	}

	// Handle "data:" lines
	if !strings.HasPrefix(line, "data: ") {
		return nil, nil
	}

	dataStr := strings.TrimPrefix(line, "data: ")
	if dataStr == "" {
		return nil, nil
	}

	// Parse the JSON data
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		log.Printf("⚠️  OpenAI Responses: failed to parse data: %s", dataStr[:min(len(dataStr), 200)])
		return nil, nil
	}

	eventType := p.currentEventType
	p.currentEventType = "" // Reset after consuming

	return p.handleEvent(eventType, data)
}

// handleEvent processes a parsed Responses API event
func (p *OpenAIResponsesProcessor) handleEvent(eventType string, data map[string]interface{}) (*StreamEvent, error) {
	switch eventType {
	// ── Response lifecycle ──
	case "response.created":
		if id, ok := data["id"].(string); ok {
			p.responseID = id
			log.Printf("🔗 OpenAI Responses: response created id=%s", id)
		}
		if !p.hasStarted {
			p.hasStarted = true
			return &StreamEvent{
				Type:      "start",
				Timestamp: time.Now().UnixMilli(),
			}, nil
		}
		return nil, nil

	case "response.in_progress":
		return nil, nil

	case "response.completed":
		log.Printf("✅ OpenAI Responses: response completed")
		p.streamDone = true
		// Extract usage if present
		if usage, ok := data["usage"].(map[string]interface{}); ok {
			usageEvent := p.parseUsage(usage)
			p.pendingEvents = append(p.pendingEvents, usageEvent)
		}
		return &StreamEvent{
			Type:      "stop",
			Timestamp: time.Now().UnixMilli(),
		}, nil

	case "response.failed":
		errMsg := "unknown error"
		if errObj, ok := data["error"].(map[string]interface{}); ok {
			if msg, ok := errObj["message"].(string); ok {
				errMsg = msg
			}
		}
		log.Printf("❌ OpenAI Responses: response failed: %s", errMsg)
		return nil, fmt.Errorf("OpenAI Responses API error: %s", errMsg)

	case "response.incomplete":
		log.Printf("⚠️  OpenAI Responses: response incomplete")
		p.streamDone = true
		return &StreamEvent{
			Type:      "stop",
			Timestamp: time.Now().UnixMilli(),
		}, nil

	// ── Output items ──
	case "response.output_item.added":
		return p.handleOutputItemAdded(data)

	case "response.output_item.done":
		return p.handleOutputItemDone(data)

	// ── Text content ──
	case "response.output_text.delta":
		delta, _ := data["delta"].(string)
		if delta != "" {
			if !p.hasStarted {
				p.hasStarted = true
				p.pendingEvents = append(p.pendingEvents, &StreamEvent{
					Type:      "content",
					Content:   delta,
					Timestamp: time.Now().UnixMilli(),
				})
				return &StreamEvent{
					Type:      "start",
					Timestamp: time.Now().UnixMilli(),
				}, nil
			}
			return &StreamEvent{
				Type:      "content",
				Content:   delta,
				Timestamp: time.Now().UnixMilli(),
			}, nil
		}
		return nil, nil

	case "response.output_text.done":
		return nil, nil

	// ── Reasoning/thinking ──
	case "response.reasoning_summary_text.delta":
		delta, _ := data["delta"].(string)
		if delta != "" {
			p.accumulatedReasoning += delta
			return &StreamEvent{
				Type:      "thinking",
				Content:   delta,
				Timestamp: time.Now().UnixMilli(),
			}, nil
		}
		return nil, nil

	case "response.reasoning_summary_text.done":
		return nil, nil

	// ── Function call arguments streaming ──
	case "response.function_call_arguments.delta":
		itemID, _ := data["item_id"].(string)
		delta, _ := data["delta"].(string)
		if itemID != "" && delta != "" {
			p.funcCallArgs[itemID] += delta
			return &StreamEvent{
				Type:      "tool_input_delta",
				Content:   delta,
				Timestamp: time.Now().UnixMilli(),
			}, nil
		}
		return nil, nil

	case "response.function_call_arguments.done":
		return nil, nil

	// ── Content part events ──
	case "response.content_part.added",
		"response.content_part.done",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_part.done":
		return nil, nil

	// ── Rate limit info ──
	case "response.rate_limits.updated":
		return nil, nil

	default:
		if eventType != "" {
			log.Printf("🔍 OpenAI Responses: unhandled event type: %s", eventType)
		}
		return nil, nil
	}
}

// handleOutputItemAdded handles when a new output item starts
func (p *OpenAIResponsesProcessor) handleOutputItemAdded(data map[string]interface{}) (*StreamEvent, error) {
	item, ok := data["item"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	itemType, _ := item["type"].(string)
	itemID, _ := item["id"].(string)

	switch itemType {
	case "computer_call":
		callID, _ := item["call_id"].(string)
		if callID == "" {
			callID = itemID
		}
		p.currentComputerCallID = callID
		p.computerActions = nil
		log.Printf("🖥️  OpenAI Responses: computer_call started, call_id=%s", callID)
		return &StreamEvent{
			Type:      "tool_call",
			ToolName:  "computer",
			ToolID:    callID,
			Timestamp: time.Now().UnixMilli(),
		}, nil

	case "function_call":
		callID, _ := item["call_id"].(string)
		name, _ := item["name"].(string)
		if callID == "" {
			callID = itemID
		}
		p.funcCallIDs[itemID] = callID
		p.funcCallNames[itemID] = name
		p.funcCallArgs[itemID] = ""
		log.Printf("🔧 OpenAI Responses: function_call started, name=%s call_id=%s", name, callID)
		return &StreamEvent{
			Type:      "tool_call",
			ToolName:  name,
			ToolID:    callID,
			Timestamp: time.Now().UnixMilli(),
		}, nil

	case "message":
		// Text message output item — no action needed until content arrives
		return nil, nil
	}

	return nil, nil
}

// handleOutputItemDone handles when an output item is complete
func (p *OpenAIResponsesProcessor) handleOutputItemDone(data map[string]interface{}) (*StreamEvent, error) {
	item, ok := data["item"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	itemType, _ := item["type"].(string)
	itemID, _ := item["id"].(string)

	switch itemType {
	case "computer_call":
		return p.handleComputerCallDone(item)

	case "function_call":
		return p.handleFunctionCallDone(itemID, item)

	case "message":
		return nil, nil
	}

	return nil, nil
}

// handleComputerCallDone processes a completed computer_call and emits a tool_use event
func (p *OpenAIResponsesProcessor) handleComputerCallDone(item map[string]interface{}) (*StreamEvent, error) {
	callID, _ := item["call_id"].(string)
	if callID == "" {
		callID = p.currentComputerCallID
	}

	// Extract actions array
	var actions []interface{}
	if actionsRaw, ok := item["actions"].([]interface{}); ok {
		actions = actionsRaw
	} else if actionRaw, ok := item["action"].(map[string]interface{}); ok {
		// Single action format (older API)
		actions = []interface{}{actionRaw}
	}

	if len(actions) == 0 {
		log.Printf("⚠️  OpenAI Responses: computer_call done but no actions found")
		return nil, nil
	}

	// Translate OpenAI actions to our operator format
	// We send the full actions array as "actions" in ToolInput
	// The tool executor in processor.go will handle batched execution
	translatedActions := make([]interface{}, 0, len(actions))
	for _, action := range actions {
		if actionMap, ok := action.(map[string]interface{}); ok {
			translated := TranslateOpenAIActionToOperator(actionMap)
			translatedActions = append(translatedActions, translated)
		}
	}

	toolInput := map[string]interface{}{
		"actions": translatedActions,
		"call_id": callID,
	}

	// If there's only one action, also set top-level action fields for compatibility
	if len(translatedActions) == 1 {
		if singleAction, ok := translatedActions[0].(map[string]interface{}); ok {
			for k, v := range singleAction {
				toolInput[k] = v
			}
		}
	}

	log.Printf("🖥️  OpenAI Responses: computer_call done, call_id=%s, %d actions", callID, len(actions))

	return &StreamEvent{
		Type:      "tool_use",
		ToolName:  "computer",
		ToolID:    callID,
		ToolInput: toolInput,
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

// handleFunctionCallDone processes a completed function_call
func (p *OpenAIResponsesProcessor) handleFunctionCallDone(itemID string, item map[string]interface{}) (*StreamEvent, error) {
	callID := p.funcCallIDs[itemID]
	name := p.funcCallNames[itemID]
	argsStr := p.funcCallArgs[itemID]

	if callID == "" {
		callID, _ = item["call_id"].(string)
	}
	if name == "" {
		name, _ = item["name"].(string)
	}
	if argsStr == "" {
		argsStr, _ = item["arguments"].(string)
	}

	// Parse arguments
	toolInput := make(map[string]interface{})
	if argsStr != "" {
		if err := json.Unmarshal([]byte(argsStr), &toolInput); err != nil {
			log.Printf("⚠️  OpenAI Responses: failed to parse function args: %v", err)
			toolInput = map[string]interface{}{"raw_args": argsStr}
		}
	}

	// Clean up tracking maps
	delete(p.funcCallIDs, itemID)
	delete(p.funcCallNames, itemID)
	delete(p.funcCallArgs, itemID)

	log.Printf("🔧 OpenAI Responses: function_call done, name=%s call_id=%s", name, callID)

	return &StreamEvent{
		Type:      "tool_use",
		ToolName:  name,
		ToolID:    callID,
		ToolInput: toolInput,
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

// parseUsage extracts token usage from the response
func (p *OpenAIResponsesProcessor) parseUsage(usage map[string]interface{}) *StreamEvent {
	inputTokens, _ := usage["input_tokens"].(float64)
	outputTokens, _ := usage["output_tokens"].(float64)
	totalTokens, _ := usage["total_tokens"].(float64)

	event := &StreamEvent{
		Type:         "usage",
		InputTokens:  int(inputTokens),
		OutputTokens: int(outputTokens),
		TotalTokens:  int(totalTokens),
		Timestamp:    time.Now().UnixMilli(),
	}

	// Extract detailed token info if available
	if inputDetails, ok := usage["input_tokens_details"].(map[string]interface{}); ok {
		if cached, ok := inputDetails["cached_tokens"].(float64); ok {
			event.CacheReadTokens = int(cached)
		}
	}
	if outputDetails, ok := usage["output_tokens_details"].(map[string]interface{}); ok {
		if reasoning, ok := outputDetails["reasoning_tokens"].(float64); ok {
			event.ReasoningTokens = int(reasoning)
		}
	}

	return event
}

// IsComplete checks if the stream is finished
func (p *OpenAIResponsesProcessor) IsComplete(line string) bool {
	return p.streamDone
}

// HasPendingEvents implements PendingEventChecker
func (p *OpenAIResponsesProcessor) HasPendingEvents() bool {
	return len(p.pendingEvents) > 0
}

// GetAccumulatedReasoning implements ReasoningAccumulator
func (p *OpenAIResponsesProcessor) GetAccumulatedReasoning() string {
	return p.accumulatedReasoning
}

// ClearAccumulatedReasoning implements ReasoningAccumulator
func (p *OpenAIResponsesProcessor) ClearAccumulatedReasoning() {
	p.accumulatedReasoning = ""
}

// ResetForNewTurn implements TurnResetter
func (p *OpenAIResponsesProcessor) ResetForNewTurn() {
	p.hasStarted = false
	p.currentEventType = ""
	p.streamDone = false
	p.pendingEvents = nil
	p.computerActions = nil
	p.currentComputerCallID = ""
	// Don't clear responseID — needed for chaining
	// Don't clear accumulatedReasoning — cleared explicitly
}

// ==================== Action Translation ====================

// TranslateOpenAIActionToOperator converts an OpenAI Responses API action to our operator format
func TranslateOpenAIActionToOperator(action map[string]interface{}) map[string]interface{} {
	actionType, _ := action["type"].(string)
	result := map[string]interface{}{}

	switch actionType {
	case "click":
		button, _ := action["button"].(string)
		if button == "" {
			button = "left"
		}
		switch button {
		case "left":
			result["action"] = "left_click"
		case "right":
			result["action"] = "right_click"
		case "middle":
			result["action"] = "middle_click"
		default:
			result["action"] = "left_click"
		}
		x, _ := action["x"].(float64)
		y, _ := action["y"].(float64)
		result["coordinate"] = []interface{}{x, y}

	case "double_click":
		result["action"] = "double_click"
		x, _ := action["x"].(float64)
		y, _ := action["y"].(float64)
		result["coordinate"] = []interface{}{x, y}

	case "type":
		result["action"] = "type"
		text, _ := action["text"].(string)
		result["text"] = text

	case "keypress":
		result["action"] = "key"
		if keys, ok := action["keys"].([]interface{}); ok {
			var keyStrs []string
			for _, k := range keys {
				if ks, ok := k.(string); ok {
					// Map OpenAI key names to our format
					mapped := mapOpenAIKey(ks)
					keyStrs = append(keyStrs, mapped)
				}
			}
			result["text"] = strings.Join(keyStrs, "+")
		}

	case "scroll":
		result["action"] = "scroll"
		x, _ := action["x"].(float64)
		y, _ := action["y"].(float64)
		result["coordinate"] = []interface{}{x, y}

		scrollX, _ := action["scroll_x"].(float64)
		scrollY, _ := action["scroll_y"].(float64)
		// Prefer scrollX if larger, otherwise scrollY
		if abs(scrollX) > abs(scrollY) {
			if scrollX < 0 {
				result["scroll_direction"] = "left"
				result["scroll_amount"] = -scrollX
			} else {
				result["scroll_direction"] = "right"
				result["scroll_amount"] = scrollX
			}
		} else {
			if scrollY < 0 {
				result["scroll_direction"] = "up"
				result["scroll_amount"] = -scrollY
			} else {
				result["scroll_direction"] = "down"
				result["scroll_amount"] = scrollY
			}
		}

	case "drag":
		result["action"] = "left_click_drag"
		startX, _ := action["start_x"].(float64)
		startY, _ := action["start_y"].(float64)
		endX, _ := action["end_x"].(float64)
		endY, _ := action["end_y"].(float64)
		result["start_coordinate"] = []interface{}{startX, startY}
		result["coordinate"] = []interface{}{endX, endY}

	case "move":
		result["action"] = "mouse_move"
		x, _ := action["x"].(float64)
		y, _ := action["y"].(float64)
		result["coordinate"] = []interface{}{x, y}

	case "wait":
		result["action"] = "wait"
		if ms, ok := action["ms"].(float64); ok {
			result["duration"] = ms
		} else {
			result["duration"] = float64(2000) // Default 2 seconds
		}

	case "screenshot":
		result["action"] = "screenshot"

	default:
		// Pass through unknown action types
		result["action"] = actionType
		log.Printf("⚠️  OpenAI Responses: unknown action type: %s", actionType)
	}

	return result
}

// mapOpenAIKey maps OpenAI key names to our operator key format
func mapOpenAIKey(key string) string {
	// OpenAI uses uppercase key names like "SPACE", "ENTER", "BACKSPACE"
	switch strings.ToUpper(key) {
	case "SPACE":
		return "space"
	case "ENTER", "RETURN":
		return "Return"
	case "BACKSPACE":
		return "BackSpace"
	case "TAB":
		return "Tab"
	case "ESCAPE", "ESC":
		return "Escape"
	case "DELETE":
		return "Delete"
	case "UP":
		return "Up"
	case "DOWN":
		return "Down"
	case "LEFT":
		return "Left"
	case "RIGHT":
		return "Right"
	case "HOME":
		return "Home"
	case "END":
		return "End"
	case "PAGEUP":
		return "Page_Up"
	case "PAGEDOWN":
		return "Page_Down"
	default:
		return key
	}
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

