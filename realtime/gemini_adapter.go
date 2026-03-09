package realtime

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
	"github.com/apteva/agent/handlers/threads"
	"github.com/apteva/agent/mcp"
	"github.com/apteva/agent/tools"

	"github.com/gorilla/websocket"
)

// GeminiRealtimeAdapter implements the Adapter interface for Gemini Live API
type GeminiRealtimeAdapter struct {
	session      *Session
	conn         *websocket.Conn
	eventChan    chan UnifiedEvent
	toolRegistry *tools.Registry
	mcpConfig    *config.MCPConfig
	closeChan    chan struct{}
	mu           sync.RWMutex

	// Gemini-specific state
	resumptionToken string
	pendingToolCall map[string]string // callID -> functionName
}

// NewGeminiRealtimeAdapter creates a new Gemini Live adapter
func NewGeminiRealtimeAdapter(session *Session, messageSaver threads.MessageSaver,
	eventBus *events.EventBus) (*GeminiRealtimeAdapter, error) {

	log.Printf("🎤 [GEMINI] Creating Gemini Live adapter...")

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Printf("🎤 [GEMINI] ❌ GEMINI_API_KEY not set!")
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}
	log.Printf("🎤 [GEMINI] API key found: %s...", apiKey[:10])

	// Get configuration
	cfg := config.GetConfig()
	agentConfig := cfg.Get()
	llmConfig := agentConfig.LLM

	// Connect to Gemini Live API
	geminiURL := fmt.Sprintf(
		"wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent?key=%s",
		apiKey,
	)
	log.Printf("🎤 [GEMINI] Connecting to Gemini Live API...")

	conn, resp, err := websocket.DefaultDialer.Dial(geminiURL, nil)
	if err != nil {
		if resp != nil {
			log.Printf("🎤 [GEMINI] ❌ Connection failed: %v (HTTP %d)", err, resp.StatusCode)
		} else {
			log.Printf("🎤 [GEMINI] ❌ Connection failed: %v", err)
		}
		return nil, fmt.Errorf("failed to connect to Gemini Live API: %w", err)
	}
	log.Printf("🎤 [GEMINI] ✅ Connected to Gemini Live API")

	adapter := &GeminiRealtimeAdapter{
		session:         session,
		conn:            conn,
		eventChan:       make(chan UnifiedEvent, 100),
		toolRegistry:    tools.GetGlobalRegistry(),
		mcpConfig:       agentConfig.MCP,
		closeChan:       make(chan struct{}),
		pendingToolCall: make(map[string]string),
	}

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

	// Convert to Gemini function declarations format
	functionDeclarations := make([]map[string]interface{}, len(customTools))
	for i, tool := range customTools {
		functionDeclarations[i] = map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.InputSchema,
		}
	}

	// Get voice from config or default
	voice := "Kore"
	if agentConfig.Realtime != nil && agentConfig.Realtime.GeminiVoice != "" {
		voice = agentConfig.Realtime.GeminiVoice
	}

	// Build tools array
	toolsConfig := []map[string]interface{}{}
	if len(functionDeclarations) > 0 {
		toolsConfig = append(toolsConfig, map[string]interface{}{
			"functionDeclarations": functionDeclarations,
		})
	}

	// Optionally enable Google Search grounding
	if agentConfig.Realtime != nil && agentConfig.Realtime.GoogleSearch {
		toolsConfig = append(toolsConfig, map[string]interface{}{
			"googleSearch": map[string]interface{}{},
		})
	}

	// Get model from config or default
	model := "gemini-2.5-flash-native-audio-preview-12-2025"
	if agentConfig.Realtime != nil && agentConfig.Realtime.GeminiModel != "" {
		model = agentConfig.Realtime.GeminiModel
	}

	// Send setup message
	setupMsg := map[string]interface{}{
		"setup": map[string]interface{}{
			"model": model,
			"generationConfig": map[string]interface{}{
				"responseModalities": []string{"AUDIO"},
				"speechConfig": map[string]interface{}{
					"voiceConfig": map[string]interface{}{
						"prebuiltVoiceConfig": map[string]interface{}{
							"voiceName": voice,
						},
					},
				},
				"thinkingConfig": map[string]interface{}{
					"thinkingBudget": 0, // Disable thinking for faster responses
				},
			},
			// VAD config for faster, more responsive turn-taking
			"realtimeInputConfig": map[string]interface{}{
				"automaticActivityDetection": map[string]interface{}{
					"startOfSpeechSensitivity": "START_SENSITIVITY_HIGH",
					"endOfSpeechSensitivity":   "END_SENSITIVITY_HIGH",
					"prefixPaddingMs":          100, // Faster speech start detection
					"silenceDurationMs":        300, // Respond faster after silence
				},
			},
			"systemInstruction": map[string]interface{}{
				"parts": []map[string]interface{}{
					{"text": llmConfig.SystemPrompt},
				},
			},
			"tools": toolsConfig,
		},
	}

	setupJSON, _ := json.MarshalIndent(setupMsg, "", "  ")
	log.Printf("🎤 [GEMINI] Setup message: %s", string(setupJSON))

	if err := conn.WriteJSON(setupMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send setup message: %w", err)
	}

	// Start event listener
	go adapter.listenForEvents()

	log.Printf("✅ Gemini Live adapter created (model: %s, voice: %s, tools: %d)", model, voice, len(functionDeclarations))

	return adapter, nil
}

// listenForEvents processes events from Gemini
func (a *GeminiRealtimeAdapter) listenForEvents() {
	defer close(a.eventChan)

	msgCount := 0
	for {
		select {
		case <-a.closeChan:
			return
		default:
			_, message, err := a.conn.ReadMessage()
			if err != nil {
				log.Printf("🎤 [GEMINI] Read error: %v", err)
				return
			}

			msgCount++
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("🎤 [GEMINI] Failed to parse message: %v", err)
				continue
			}

			// Log message types (not full content for audio)
			msgKeys := make([]string, 0, len(msg))
			for k := range msg {
				msgKeys = append(msgKeys, k)
			}
			log.Printf("🎤 [GEMINI] Message #%d keys: %v", msgCount, msgKeys)

			a.handleGeminiEvent(msg)
		}
	}
}

// handleGeminiEvent routes Gemini events to appropriate handlers
func (a *GeminiRealtimeAdapter) handleGeminiEvent(msg map[string]interface{}) {
	// Check for setupComplete
	if _, ok := msg["setupComplete"]; ok {
		log.Printf("✅ Gemini session setup complete")
		a.eventChan <- UnifiedEvent{
			Type:      EventTypeSessionCreated,
			SessionID: a.session.ID,
			Timestamp: time.Now(),
			Data:      msg,
		}
		return
	}

	// Check for serverContent (audio/text responses)
	if serverContent, ok := msg["serverContent"].(map[string]interface{}); ok {
		a.handleServerContent(serverContent)
		return
	}

	// Check for toolCall
	if toolCall, ok := msg["toolCall"].(map[string]interface{}); ok {
		a.handleToolCall(toolCall)
		return
	}

	// Check for toolCallCancellation
	if cancellation, ok := msg["toolCallCancellation"].(map[string]interface{}); ok {
		a.handleToolCallCancellation(cancellation)
		return
	}

	// Check for sessionResumptionUpdate
	if resumption, ok := msg["sessionResumptionUpdate"].(map[string]interface{}); ok {
		if handle, ok := resumption["handle"].(string); ok {
			a.mu.Lock()
			a.resumptionToken = handle
			a.mu.Unlock()
			log.Printf("📝 Gemini session resumption token received")
		}
		return
	}

	// Check for goAway (server requesting disconnect)
	if goAway, ok := msg["goAway"].(map[string]interface{}); ok {
		log.Printf("⚠️ Gemini GoAway received: %v", goAway)
		a.eventChan <- UnifiedEvent{
			Type:      EventTypeError,
			SessionID: a.session.ID,
			Timestamp: time.Now(),
			Data: ErrorData{
				Code:    "go_away",
				Message: fmt.Sprintf("Server requested disconnect: %v", goAway),
			},
		}
		return
	}

	// Log unknown events for debugging
	log.Printf("📥 Gemini event (unhandled): %v", msg)
}

// handleServerContent processes model responses
func (a *GeminiRealtimeAdapter) handleServerContent(content map[string]interface{}) {
	// Debug log full content
	contentJSON, _ := json.Marshal(content)
	log.Printf("🎤 [GEMINI] serverContent: %s", string(contentJSON))
	// Check for interruption
	if interrupted, ok := content["interrupted"].(bool); ok && interrupted {
		log.Printf("🛑 Gemini: User interrupted")
		a.session.SetTurnState("idle")
		return
	}

	// Check for turn complete
	if turnComplete, ok := content["turnComplete"].(bool); ok && turnComplete {
		log.Printf("✅ Gemini: Turn complete")
		a.eventChan <- UnifiedEvent{
			Type:      EventTypeTurnEnd,
			SessionID: a.session.ID,
			Timestamp: time.Now(),
			Data:      content,
		}
		return
	}

	// Process modelTurn
	if modelTurn, ok := content["modelTurn"].(map[string]interface{}); ok {
		if parts, ok := modelTurn["parts"].([]interface{}); ok {
			for _, part := range parts {
				partMap, ok := part.(map[string]interface{})
				if !ok {
					continue
				}

				// Handle inline audio data
				if inlineData, ok := partMap["inlineData"].(map[string]interface{}); ok {
					if data, ok := inlineData["data"].(string); ok {
						a.session.SetTurnState("assistant_speaking")
						a.eventChan <- UnifiedEvent{
							Type:      EventTypeAudioDelta,
							SessionID: a.session.ID,
							Timestamp: time.Now(),
							Data: AudioDeltaData{
								Format:     "pcm16",
								SampleRate: 24000,
								Chunk:      data,
							},
						}
					}
				}

				// Handle text response
				if text, ok := partMap["text"].(string); ok {
					// Save transcript
					model := "gemini-live"
					a.session.messageSaver.SaveMessage(
						a.session.ThreadID,
						"assistant",
						text,
						&model,
						nil,
					)

					a.eventChan <- UnifiedEvent{
						Type:      EventTypeTranscript,
						SessionID: a.session.ID,
						Timestamp: time.Now(),
						Data: TranscriptData{
							Role:    "assistant",
							Content: text,
						},
					}
				}
			}
		}
	}

	// Check for input transcription
	if inputTranscription, ok := content["inputTranscription"].(map[string]interface{}); ok {
		if text, ok := inputTranscription["text"].(string); ok {
			// Save user transcript
			a.session.messageSaver.SaveMessage(
				a.session.ThreadID,
				"user",
				text,
				nil,
				nil,
			)

			a.eventChan <- UnifiedEvent{
				Type:      EventTypeTranscript,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: TranscriptData{
					Role:    "user",
					Content: text,
				},
			}
		}
	}

	// Check for output transcription
	if outputTranscription, ok := content["outputTranscription"].(map[string]interface{}); ok {
		if text, ok := outputTranscription["text"].(string); ok {
			a.eventChan <- UnifiedEvent{
				Type:      EventTypeTranscript,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: TranscriptData{
					Role:    "assistant",
					Content: text,
				},
			}
		}
	}
}

// handleToolCall processes function call requests from Gemini
func (a *GeminiRealtimeAdapter) handleToolCall(toolCall map[string]interface{}) {
	functionCalls, ok := toolCall["functionCalls"].([]interface{})
	if !ok {
		return
	}

	for _, fc := range functionCalls {
		call, ok := fc.(map[string]interface{})
		if !ok {
			continue
		}

		callID, _ := call["id"].(string)
		functionName, _ := call["name"].(string)
		args, _ := call["args"].(map[string]interface{})

		log.Printf("🔧 Gemini function call: %s (id: %s)", functionName, callID)

		// Track pending tool call
		a.mu.Lock()
		a.pendingToolCall[callID] = functionName
		a.mu.Unlock()

		// Update session state
		a.session.SetTurnState("tool_executing")
		a.session.AddPendingTool(&ToolExecution{
			CallID:    callID,
			Name:      functionName,
			Arguments: args,
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
				Input: args,
			},
		}

		// Execute tool asynchronously
		go a.executeToolAndRespond(callID, functionName, args)
	}
}

// handleToolCallCancellation handles cancelled tool calls
func (a *GeminiRealtimeAdapter) handleToolCallCancellation(cancellation map[string]interface{}) {
	if ids, ok := cancellation["ids"].([]interface{}); ok {
		for _, id := range ids {
			if callID, ok := id.(string); ok {
				log.Printf("🛑 Gemini: Tool call cancelled: %s", callID)
				a.session.UpdateToolStatus(callID, "cancelled")

				a.mu.Lock()
				delete(a.pendingToolCall, callID)
				a.mu.Unlock()
			}
		}
	}
}

// executeToolAndRespond executes the tool and sends result back to Gemini
func (a *GeminiRealtimeAdapter) executeToolAndRespond(callID, functionName string,
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
		WithData("mode", "realtime_voice_gemini")
	a.session.eventBus.Publish(toolEvent)

	// Check if MCP tool
	if mcp.IsMCPTool(functionName, a.mcpConfig) {
		log.Printf("🔧 Executing MCP tool: %s", functionName)
		result, err = mcp.ExecuteTool(functionName, arguments, a.mcpConfig, a.session.ThreadID)
	} else {
		// Execute custom tool
		log.Printf("🔧 Executing custom tool: %s", functionName)
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
	} else {
		resultEvent := events.NewEvent(events.CategoryTool, events.TypeToolResult, events.LevelInfo).
			WithThread(a.session.ThreadID).
			WithData("tool_name", functionName).
			WithData("result", sanitizeForEvent(result)).
			WithDuration(toolStartTime)
		a.session.eventBus.Publish(resultEvent)
	}

	// Send tool response to Gemini
	a.sendToolResponse(callID, result, err)

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

	// Clean up pending tool
	a.mu.Lock()
	delete(a.pendingToolCall, callID)
	a.mu.Unlock()
}

// sendToolResponse sends tool result back to Gemini
func (a *GeminiRealtimeAdapter) sendToolResponse(callID string, result interface{}, err error) {
	var response interface{}
	if err != nil {
		response = map[string]interface{}{
			"error": err.Error(),
		}
	} else {
		response = result
	}

	toolResponse := map[string]interface{}{
		"toolResponse": map[string]interface{}{
			"functionResponses": []map[string]interface{}{
				{
					"id":       callID,
					"response": response,
				},
			},
		},
	}

	if err := a.conn.WriteJSON(toolResponse); err != nil {
		log.Printf("Failed to send tool response: %v", err)
	} else {
		log.Printf("✅ Sent tool response for call_id: %s", callID)
	}
}

// SendAudio sends audio chunk to Gemini
var geminiAudioSent int

func (a *GeminiRealtimeAdapter) SendAudio(base64Audio string) error {
	msg := map[string]interface{}{
		"realtimeInput": map[string]interface{}{
			"mediaChunks": []map[string]interface{}{
				{
					"mimeType": "audio/pcm",
					"data":     base64Audio,
				},
			},
		},
	}

	if err := a.conn.WriteJSON(msg); err != nil {
		log.Printf("🎤 [GEMINI] ❌ Failed to send audio: %v", err)
		return err
	}
	geminiAudioSent++
	if geminiAudioSent%50 == 1 {
		// Log audio data size and first few bytes to debug
		log.Printf("🎤 [GEMINI] ✅ Sent audio chunk #%d (base64 len=%d, first 20 chars: %s...)",
			geminiAudioSent, len(base64Audio), base64Audio[:min(20, len(base64Audio))])
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SendText sends text message to Gemini
func (a *GeminiRealtimeAdapter) SendText(text string) error {
	msg := map[string]interface{}{
		"clientContent": map[string]interface{}{
			"turns": []map[string]interface{}{
				{
					"role": "user",
					"parts": []map[string]interface{}{
						{"text": text},
					},
				},
			},
			"turnComplete": true,
		},
	}

	return a.conn.WriteJSON(msg)
}

// HandleControl handles control messages
func (a *GeminiRealtimeAdapter) HandleControl(action string, data map[string]interface{}) error {
	switch action {
	case "commit_audio":
		// Gemini uses turnComplete in clientContent, not a separate commit
		// This is handled automatically when audio stops
		return nil

	case "interrupt":
		// Send interrupt signal
		return a.conn.WriteJSON(map[string]interface{}{
			"clientContent": map[string]interface{}{
				"turnComplete": true,
			},
		})

	case "end_session":
		// Gracefully end the session
		return a.Close()
	}

	return nil
}

// ReceiveEvents returns channel for receiving events
func (a *GeminiRealtimeAdapter) ReceiveEvents() <-chan UnifiedEvent {
	return a.eventChan
}

// Close closes the adapter
func (a *GeminiRealtimeAdapter) Close() error {
	close(a.closeChan)
	return a.conn.Close()
}

// InputSampleRate returns the expected input sample rate for Gemini (16kHz)
func (a *GeminiRealtimeAdapter) InputSampleRate() int {
	return 16000
}

// GetResumptionToken returns the session resumption token (Gemini-specific)
func (a *GeminiRealtimeAdapter) GetResumptionToken() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.resumptionToken
}
