package realtime

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
	"github.com/apteva/agent/handlers/threads"
	"github.com/apteva/agent/mcp"
	"github.com/apteva/agent/tools"

	"github.com/gorilla/websocket"
)

// Adapter interface for different realtime providers
type Adapter interface {
	SendAudio(base64Audio string) error
	SendText(text string) error
	HandleControl(action string, data map[string]interface{}) error
	ReceiveEvents() <-chan UnifiedEvent
	Close() error
	InputSampleRate() int // Expected input audio sample rate (e.g., 16000 or 24000)
}

// OpenAIRealtimeAdapter implements the Adapter interface for OpenAI Realtime API
type OpenAIRealtimeAdapter struct {
	session      *Session
	conn         *websocket.Conn
	eventChan    chan UnifiedEvent
	toolRegistry *tools.Registry
	mcpConfig    *config.MCPConfig
	closeChan    chan struct{}
	mu           sync.RWMutex // protects WebSocket writes only
	model        string       // Model name for logging/saving messages

	// Track current response item for truncation on interrupt (lock-free)
	currentItemID     atomic.Value // stores string
	currentContentIdx atomic.Int32
}

// NewOpenAIRealtimeAdapter creates a new OpenAI Realtime adapter
func NewOpenAIRealtimeAdapter(session *Session, messageSaver threads.MessageSaver,
	eventBus *events.EventBus) (*OpenAIRealtimeAdapter, error) {

	// Get configuration
	cfg := config.GetConfig()
	agentConfig := cfg.Get()
	llmConfig := agentConfig.LLM

	// Get model from config or default to gpt-realtime (GA model)
	model := "gpt-realtime"
	if agentConfig.Realtime != nil && agentConfig.Realtime.Model != "" {
		model = agentConfig.Realtime.Model
	}

	// Connect to OpenAI Realtime API
	openaiURL := fmt.Sprintf("wss://api.openai.com/v1/realtime?model=%s", model)
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}

	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+apiKey)
	headers.Add("OpenAI-Beta", "realtime=v1")

	conn, resp, err := websocket.DefaultDialer.Dial(openaiURL, headers)
	if err != nil {
		if resp != nil {
			log.Printf("❌ OpenAI Realtime connection failed: %v (HTTP %d)", err, resp.StatusCode)
		}
		return nil, fmt.Errorf("failed to connect to OpenAI Realtime API: %w", err)
	}

	adapter := &OpenAIRealtimeAdapter{
		session:      session,
		conn:         conn,
		eventChan:    make(chan UnifiedEvent, 512),
		toolRegistry: tools.GetGlobalRegistry(),
		mcpConfig:    agentConfig.MCP,
		closeChan:    make(chan struct{}),
		model:        model,
	}

	// Store OpenAI connection in session
	session.openaiConn = conn

	// Get custom tool definitions
	var customTools []tools.ToolDefinition
	if len(llmConfig.Tools) > 0 {
		customTools = tools.GetToolsForNames(llmConfig.Tools)
	}

	// Get MCP tool definitions
	if agentConfig.MCP != nil && agentConfig.MCP.Enabled {
		mcpToolDefs := mcp.ConvertMCPToolsToToolDefinitions(mcp.GetEnabledMCPTools(agentConfig.MCP))
		customTools = append(customTools, mcpToolDefs...)
	}

	// Convert to OpenAI Realtime format
	realtimeTools := make([]map[string]interface{}, len(customTools))
	for i, tool := range customTools {
		realtimeTools[i] = map[string]interface{}{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.InputSchema,
		}
	}

	// Get voice from config or default (marin recommended by OpenAI for best quality)
	voice := "marin"
	if agentConfig.Realtime != nil && agentConfig.Realtime.Voice != "" {
		voice = agentConfig.Realtime.Voice
	}

	// Get VAD type from config or default
	vadType := "semantic_vad"
	if agentConfig.Realtime != nil && agentConfig.Realtime.VADType != "" {
		vadType = agentConfig.Realtime.VADType
	}

	// Build turn detection config
	turnDetection := map[string]interface{}{
		"type": vadType,
	}
	if vadType == "semantic_vad" {
		turnDetection["eagerness"] = "medium"
		turnDetection["create_response"] = true
		turnDetection["interrupt_response"] = true
	} else if vadType == "server_vad" {
		turnDetection["threshold"] = 0.5
		turnDetection["prefix_padding_ms"] = 300
		turnDetection["silence_duration_ms"] = 500
		turnDetection["create_response"] = true
		turnDetection["interrupt_response"] = true
	}

	// Configure session
	sessionConfig := map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"modalities":               []string{"audio", "text"},
			"voice":                    voice,
			"input_audio_format":       "pcm16",
			"output_audio_format":      "pcm16",
			"turn_detection":           turnDetection,
			"max_response_output_tokens": "inf",
			"input_audio_transcription": map[string]interface{}{
				"model": "gpt-4o-mini-transcribe-2025-12-15",
			},
			"input_audio_noise_reduction": map[string]interface{}{
				"type": "near_field",
			},
			"instructions": llmConfig.SystemPrompt,
			"tools":        realtimeTools,
			"tool_choice":  "auto",
		},
	}

	if err := conn.WriteJSON(sessionConfig); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to configure session: %w", err)
	}

	// Start event listener
	go adapter.listenForEvents()

	log.Printf("OpenAI Realtime session ready (model: %s, voice: %s, tools: %d)", model, voice, len(realtimeTools))

	return adapter, nil
}

// listenForEvents processes events from OpenAI
func (a *OpenAIRealtimeAdapter) listenForEvents() {
	defer close(a.eventChan)

	for {
		select {
		case <-a.closeChan:
			return
		default:
			var msg map[string]interface{}
			if err := a.conn.ReadJSON(&msg); err != nil {
				log.Printf("OpenAI read error: %v", err)
				return
			}

			a.handleOpenAIEvent(msg)
		}
	}
}

// handleOpenAIEvent routes OpenAI events
func (a *OpenAIRealtimeAdapter) handleOpenAIEvent(msg map[string]interface{}) {
	eventType, ok := msg["type"].(string)
	if !ok {
		return
	}

	switch eventType {
	case "session.created":
		a.eventChan <- UnifiedEvent{
			Type:      EventTypeSessionCreated,
			SessionID: a.session.ID,
			Timestamp: time.Now(),
			Data:      msg,
		}

	case "session.updated":
		// Session config confirmed by OpenAI

	case "input_audio_buffer.speech_started":
		a.session.SetTurnState("user_speaking")

		// Always tell client to stop audio playback when user starts speaking.
		// Audio is sent faster than realtime, so buffered audio may still be playing
		// even after response.audio.done has cleared currentItemID.
		itemID, _ := a.currentItemID.Load().(string)
		contentIdx := int(a.currentContentIdx.Load())

		a.eventChan <- UnifiedEvent{
			Type:      EventTypeAudioInterrupt,
			SessionID: a.session.ID,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"item_id":       itemID,
				"content_index": contentIdx,
			},
		}

		a.eventChan <- UnifiedEvent{
			Type:      EventTypeTurnStart,
			SessionID: a.session.ID,
			Timestamp: time.Now(),
			Data:      map[string]interface{}{"speaker": "user"},
		}

	case "input_audio_buffer.speech_stopped":
		a.session.SetTurnState("idle")

	case "response.audio.delta":
		// Track item_id and content_index for potential truncation on interrupt (lock-free)
		if itemID, ok := msg["item_id"].(string); ok {
			a.currentItemID.Store(itemID)
			if contentIdx, ok := msg["content_index"].(float64); ok {
				a.currentContentIdx.Store(int32(contentIdx))
			}
		}

		if delta, ok := msg["delta"].(string); ok {
			a.eventChan <- UnifiedEvent{
				Type:      EventTypeAudioDelta,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: AudioDeltaData{
					Format:     "pcm16",
					SampleRate: 24000,
					Chunk:      delta,
				},
			}
		}

	case "response.audio.done":
		// Clear item tracking — response completed normally (not interrupted)
		a.currentItemID.Store("")
		a.currentContentIdx.Store(0)

		a.eventChan <- UnifiedEvent{
			Type:      EventTypeAudioComplete,
			SessionID: a.session.ID,
			Timestamp: time.Now(),
			Data:      map[string]interface{}{"response_id": msg["response_id"]},
		}

	case "conversation.item.input_audio_transcription.completed":
		if transcript, ok := msg["transcript"].(string); ok {
			// Save user message to database (async — don't block event processing)
			go a.session.messageSaver.SaveMessage(
				a.session.ThreadID,
				"user",
				transcript,
				nil,
				nil,
			)

			a.eventChan <- UnifiedEvent{
				Type:      EventTypeTranscript,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: TranscriptData{
					Role:    "user",
					Content: transcript,
				},
			}
		}

	case "response.audio_transcript.done":
		if transcript, ok := msg["transcript"].(string); ok {
			// Save assistant message to database (async — don't block event processing)
			modelName := a.model
			go a.session.messageSaver.SaveMessage(
				a.session.ThreadID,
				"assistant",
				transcript,
				&modelName,
				nil,
			)

			a.eventChan <- UnifiedEvent{
				Type:      EventTypeTranscript,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: TranscriptData{
					Role:    "assistant",
					Content: transcript,
				},
			}
		}

	case "conversation.item.input_audio_transcription.delta":
		// Partial/interim transcription — send to client for real-time display
		if delta, ok := msg["delta"].(string); ok && delta != "" {
			a.eventChan <- UnifiedEvent{
				Type:      EventTypeTranscript,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: TranscriptData{
					Role:    "user",
					Content: delta,
					Partial: true,
				},
			}
		}

	case "response.function_call_arguments.done":
		a.handleFunctionCall(msg)

	case "response.done":
		// Check if this response was cancelled (interrupted by user speech)
		status := ""
		if resp, ok := msg["response"].(map[string]interface{}); ok {
			if s, ok := resp["status"].(string); ok {
				status = s
			}
		}

		if status == "cancelled" {
			// Clear item tracking since the response was cut short
			a.currentItemID.Store("")
			a.currentContentIdx.Store(0)
		}

		a.eventChan <- UnifiedEvent{
			Type:      EventTypeTurnEnd,
			SessionID: a.session.ID,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"status": status,
			},
		}

	case "conversation.item.truncated", "input_audio_buffer.cleared":
		// Acknowledged silently

	case "error":
		if errorData, ok := msg["error"].(map[string]interface{}); ok {
			log.Printf("❌ OpenAI error: %v", errorData)
			a.eventChan <- UnifiedEvent{
				Type:      EventTypeError,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: ErrorData{
					Code:    fmt.Sprintf("%v", errorData["code"]),
					Message: fmt.Sprintf("%v", errorData["message"]),
				},
			}
		}
	}
}

// handleFunctionCall processes function call requests from OpenAI
func (a *OpenAIRealtimeAdapter) handleFunctionCall(msg map[string]interface{}) {
	callID, _ := msg["call_id"].(string)
	functionName, _ := msg["name"].(string)
	argumentsJSON, _ := msg["arguments"].(string)

	// Parse arguments
	var arguments map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
		log.Printf("Failed to parse function arguments: %v", err)
		a.sendFunctionError(callID, "Invalid arguments format")
		return
	}

	// Update session state
	a.session.SetTurnState("tool_executing")
	a.session.AddPendingTool(&ToolExecution{
		CallID:    callID,
		Name:      functionName,
		Arguments: arguments,
		Status:    "executing",
		StartTime: time.Now(),
	})

	// Send event to client
	a.eventChan <- UnifiedEvent{
		Type:      EventTypeToolCall,
		SessionID: a.session.ID,
		Timestamp: time.Now(),
		Data: ToolCallData{
			ID:    callID,
			Name:  functionName,
			Input: arguments,
		},
	}

	// Execute tool asynchronously
	go a.executeToolAndRespond(callID, functionName, arguments)
}

// executeToolAndRespond executes the tool and sends result back to OpenAI
func (a *OpenAIRealtimeAdapter) executeToolAndRespond(callID, functionName string,
	arguments map[string]interface{}) {

	var result interface{}
	var err error

	// Publish tool invocation event
	toolStartTime := time.Now()
	toolEvent := events.NewEvent(events.CategoryTool, events.TypeToolInvocation, events.LevelInfo).
		WithThread(a.session.ThreadID).
		WithData("tool_name", functionName).
		WithData("tool_id", callID).
		WithData("input", arguments).
		WithData("mode", "realtime_voice")
	a.session.eventBus.Publish(toolEvent)

	// Check if MCP tool
	if mcp.IsMCPTool(functionName, a.mcpConfig) {
		result, err = mcp.ExecuteTool(functionName, arguments, a.mcpConfig, a.session.ThreadID)
	} else {
		tool, getErr := a.toolRegistry.GetTool(functionName)
		if getErr != nil {
			err = getErr
		} else {
			result, err = tool.Execute(arguments)
		}
	}

	// Update session state
	a.session.SetTurnState("assistant_speaking")
	a.session.UpdateToolStatus(callID, "completed")
	if err != nil {
		a.session.UpdateToolStatus(callID, "failed")
	}

	// Publish result event
	if err != nil {
		errorEvent := events.NewEvent(events.CategoryTool, events.TypeToolError, events.LevelError).
			WithThread(a.session.ThreadID).
			WithData("tool_name", functionName).
			WithError(err).
			WithDuration(toolStartTime)
		a.session.eventBus.Publish(errorEvent)

		a.sendFunctionError(callID, err.Error())
	} else {
		resultEvent := events.NewEvent(events.CategoryTool, events.TypeToolResult, events.LevelInfo).
			WithThread(a.session.ThreadID).
			WithData("tool_name", functionName).
			WithData("result", sanitizeForEvent(result)).
			WithDuration(toolStartTime)
		a.session.eventBus.Publish(resultEvent)

		a.sendFunctionResult(callID, result)
	}

	// Send tool result event to client
	a.eventChan <- UnifiedEvent{
		Type:      EventTypeToolResult,
		SessionID: a.session.ID,
		Timestamp: time.Now(),
		Data: ToolResultData{
			CallID: callID,
			Name:   functionName,
			Result: result,
			Error:  err,
		},
	}

	// Continue conversation
	a.continueConversation()
}

// safeWriteJSON writes to websocket with mutex protection
func (a *OpenAIRealtimeAdapter) safeWriteJSON(v interface{}) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	select {
	case <-a.closeChan:
		return fmt.Errorf("connection closed")
	default:
	}

	return a.conn.WriteJSON(v)
}

// sendFunctionResult sends tool result back to OpenAI
func (a *OpenAIRealtimeAdapter) sendFunctionResult(callID string, result interface{}) {
	resultJSON, _ := json.Marshal(result)

	err := a.safeWriteJSON(map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  string(resultJSON),
		},
	})

	if err != nil {
		log.Printf("Failed to send function result: %v", err)
	}
}

// sendFunctionError sends tool error back to OpenAI
func (a *OpenAIRealtimeAdapter) sendFunctionError(callID string, errorMsg string) {
	err := a.safeWriteJSON(map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  fmt.Sprintf("Error: %s", errorMsg),
		},
	})

	if err != nil {
		log.Printf("Failed to send function error: %v", err)
	}
}

// continueConversation tells OpenAI to generate next response
func (a *OpenAIRealtimeAdapter) continueConversation() {
	err := a.safeWriteJSON(map[string]interface{}{
		"type": "response.create",
		"response": map[string]interface{}{
			"modalities": []string{"text", "audio"},
		},
	})

	if err != nil {
		log.Printf("Failed to continue conversation: %v", err)
	}
}

// SendAudio sends audio chunk to OpenAI
func (a *OpenAIRealtimeAdapter) SendAudio(base64Audio string) error {
	return a.safeWriteJSON(map[string]interface{}{
		"type":  "input_audio_buffer.append",
		"audio": base64Audio,
	})
}

// SendText sends text message to OpenAI
func (a *OpenAIRealtimeAdapter) SendText(text string) error {
	// Create conversation item with text
	if err := a.safeWriteJSON(map[string]interface{}{
		"type": "conversation.item.create",
		"item": map[string]interface{}{
			"type": "message",
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type": "input_text",
					"text": text,
				},
			},
		},
	}); err != nil {
		return err
	}

	// Trigger response
	a.continueConversation()
	return nil
}

// HandleControl handles control messages
func (a *OpenAIRealtimeAdapter) HandleControl(action string, data map[string]interface{}) error {
	switch action {
	case "commit_audio":
		return a.safeWriteJSON(map[string]interface{}{
			"type": "input_audio_buffer.commit",
		})

	case "clear_audio":
		return a.safeWriteJSON(map[string]interface{}{
			"type": "input_audio_buffer.clear",
		})

	case "interrupt":
		return a.safeWriteJSON(map[string]interface{}{
			"type": "response.cancel",
		})

	case "truncate":
		itemID, _ := data["item_id"].(string)
		audioEndMs, _ := data["audio_end_ms"].(float64)
		contentIndex, _ := data["content_index"].(float64)
		if itemID != "" {
			return a.safeWriteJSON(map[string]interface{}{
				"type":          "conversation.item.truncate",
				"item_id":       itemID,
				"content_index": int(contentIndex),
				"audio_end_ms":  int(audioEndMs),
			})
		}
		return nil
	}
	return nil
}

// ReceiveEvents returns channel for receiving events
func (a *OpenAIRealtimeAdapter) ReceiveEvents() <-chan UnifiedEvent {
	return a.eventChan
}

// Close closes the adapter
func (a *OpenAIRealtimeAdapter) Close() error {
	close(a.closeChan)
	return a.conn.Close()
}

// InputSampleRate returns the expected input sample rate for OpenAI (24kHz)
func (a *OpenAIRealtimeAdapter) InputSampleRate() int {
	return 24000
}

// sanitizeForEvent removes or truncates large data from tool results for event storage
func sanitizeForEvent(result interface{}) interface{} {
	return sanitizeValue(result, 0)
}

func sanitizeValue(v interface{}, depth int) interface{} {
	if depth > 10 {
		return "[max depth reached]"
	}
	switch val := v.(type) {
	case map[string]interface{}:
		sanitized := make(map[string]interface{})
		for k, v := range val {
			sanitized[k] = sanitizeValue(v, depth+1)
		}
		return sanitized
	case []interface{}:
		sanitized := make([]interface{}, len(val))
		for i, v := range val {
			sanitized[i] = sanitizeValue(v, depth+1)
		}
		return sanitized
	case string:
		if len(val) > 1000 {
			// Check if base64
			if len(val) > 100 {
				validChars := 0
				for _, c := range val[:100] {
					if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
						validChars++
					}
				}
				if validChars > 90 {
					return fmt.Sprintf("[base64 data truncated, %d bytes]", len(val))
				}
			}
			return val[:500] + fmt.Sprintf("... [truncated, %d bytes total]", len(val))
		}
		return val
	default:
		return v
	}
}
