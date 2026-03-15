package realtime

import (
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
	"github.com/apteva/agent/handlers/threads"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// RealtimeServer manages WebSocket connections for real-time voice communication
type RealtimeServer struct {
	sessions     map[string]*Session
	mu           sync.RWMutex
	db           *sql.DB
	messageSaver threads.MessageSaver
	eventBus     *events.EventBus
}

// Message represents a unified message format for client-server communication
type Message struct {
	Type      string      `json:"type"`       // "audio", "text", "control", event types
	SessionID string      `json:"session_id"` // Session identifier
	Data      interface{} `json:"data"`       // Payload (varies by type)
	Timestamp int64       `json:"timestamp"`  // Unix milliseconds
}

// NewServer creates a new realtime server instance
func NewServer(db *sql.DB, messageSaver threads.MessageSaver, eventBus *events.EventBus) *RealtimeServer {
	return &RealtimeServer{
		sessions:     make(map[string]*Session),
		db:           db,
		messageSaver: messageSaver,
		eventBus:     eventBus,
	}
}

// HandleWebSocket handles WebSocket connections for real-time communication
// Auto-detects format: JSON (browser/app) or Binary (telephony like Vonage/Twilio)
func (s *RealtimeServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check if realtime is enabled in config
	cfg := config.GetConfig()
	realtimeConfig := cfg.Get().Realtime
	if realtimeConfig == nil || !realtimeConfig.Enabled {
		http.Error(w, "Realtime voice is not enabled", http.StatusServiceUnavailable)
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Create new session
	session := s.createSession(conn)
	defer s.removeSession(session.ID)

	// Get realtime provider from config
	provider := "openai" // default
	if realtimeConfig.Provider != "" {
		provider = realtimeConfig.Provider
	}
	session.Provider = provider + "-realtime"

	// Read first message to detect format (JSON vs Binary/Telephony)
	msgType, firstMsg, err := conn.ReadMessage()
	if err != nil {
		log.Printf("❌ Failed to read first message: %v", err)
		return
	}

	// Detect mode based on first message type
	isTelephonyMode := msgType == websocket.BinaryMessage

	if isTelephonyMode {
		session.Provider = provider + "-realtime-telephony"
	}

	log.Printf("Voice session %s started (provider: %s, telephony: %v)", session.ID, provider, isTelephonyMode)

	// Publish session created event
	s.eventBus.Publish(events.NewEvent(events.CategorySystem, "realtime_session_created", events.LevelInfo).
		WithData("session_id", session.ID).
		WithData("thread_id", session.ThreadID).
		WithData("provider", provider).
		WithData("telephony_mode", isTelephonyMode))

	// Initialize adapter based on provider
	var adapter Adapter
	var adapterErr error

	switch provider {
	case "gemini":
		adapter, adapterErr = NewGeminiRealtimeAdapter(session, s.messageSaver, s.eventBus)
	case "standard":
		adapter, adapterErr = NewStandardVoiceAdapter(session, s.messageSaver, s.eventBus)
	default: // "openai"
		adapter, adapterErr = NewOpenAIRealtimeAdapter(session, s.messageSaver, s.eventBus)
	}

	if adapterErr != nil {
		log.Printf("❌ Failed to create %s adapter: %v", provider, adapterErr)
		if isTelephonyMode {
			// Can't send JSON error in telephony mode, just close
			return
		}
		errorMsg := Message{
			Type:      "error",
			SessionID: session.ID,
			Timestamp: time.Now().UnixMilli(),
			Data: map[string]interface{}{
				"error":    adapterErr.Error(),
				"provider": provider,
			},
		}
		conn.WriteJSON(errorMsg)
		return
	}
	defer adapter.Close()

	if isTelephonyMode {
		// Telephony mode: raw binary PCM at 16kHz
		// Process the first message we already read
		if len(firstMsg) > 0 {
			s.processTelephonyAudio(firstMsg, adapter)
		}

		// Start telephony event listener (converts adapter output to raw binary)
		go s.handleTelephonyAdapterEvents(session, conn, adapter)

		// Handle incoming binary messages
		s.handleTelephonyMessages(session, conn, adapter)
	} else {
		// JSON mode: existing behavior
		// Note: session_created is sent by the adapter once the provider confirms the connection
		// (not duplicated here to avoid double session_created on the client)

		// Process the first JSON message we already read
		var msg Message
		if err := json.Unmarshal(firstMsg, &msg); err == nil {
			s.processJSONMessage(session, &msg, adapter)
		}

		// Start adapter event listener and client message handler
		go s.handleAdapterEvents(session, conn, adapter)
		s.handleClientMessages(session, conn, adapter)
	}
}

// createSession creates a new session
func (s *RealtimeServer) createSession(conn *websocket.Conn) *Session {
	sessionID := uuid.New().String()

	// Create thread for conversation
	threadID, err := s.messageSaver.CreateThread("Voice conversation " + time.Now().Format("2006-01-02 15:04:05"))
	if err != nil {
		log.Printf("Failed to create thread: %v", err)
		threadID = uuid.New().String()
	}

	session := &Session{
		ID:           sessionID,
		ThreadID:     threadID,
		Provider:     "openai-realtime",
		PendingTools: make(map[string]*ToolExecution),
		clientConn:   conn,
		messageSaver: s.messageSaver,
		eventBus:     s.eventBus,
		State: SessionState{
			IsActive:  true,
			TurnState: "idle",
		},
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	s.mu.Lock()
	s.sessions[sessionID] = session
	s.mu.Unlock()

	return session
}

// removeSession removes a session
func (s *RealtimeServer) removeSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, ok := s.sessions[sessionID]; ok {
		session.Close()
		delete(s.sessions, sessionID)

		log.Printf("🎙️  Closed realtime session: %s", sessionID)

		// Publish session closed event
		s.eventBus.Publish(events.NewEvent(events.CategorySystem, "realtime_session_closed", events.LevelInfo).
			WithData("session_id", sessionID))
	}
}

// handleClientMessages handles incoming messages from the client
func (s *RealtimeServer) handleClientMessages(session *Session, conn *websocket.Conn, adapter Adapter) {
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error (session %s): %v", session.ID, err)
			}
			break
		}

		switch msg.Type {
		case "audio":
			// Hot path — skip UpdateLastActivity for high-frequency audio messages
			if data, ok := msg.Data.(map[string]interface{}); ok {
				if chunk, ok := data["chunk"].(string); ok {
					clientRate := 24000
					if sr, ok := data["sample_rate"].(float64); ok && sr > 0 {
						clientRate = int(sr)
					}
					targetRate := adapter.InputSampleRate()

					audioToSend := chunk
					if clientRate != targetRate {
						rawBytes, err := base64.StdEncoding.DecodeString(chunk)
						if err == nil {
							inputSamples := bytesToSamples(rawBytes)
							outputSamples := resampleAudioServer(inputSamples, clientRate, targetRate)
							audioToSend = base64.StdEncoding.EncodeToString(samplesToBytes(outputSamples))
						}
					}
					if err := adapter.SendAudio(audioToSend); err != nil {
						log.Printf("❌ Failed to send audio: %v", err)
					}
				}
			}

		case "text":
			session.UpdateLastActivity()
			if data, ok := msg.Data.(map[string]interface{}); ok {
				if content, ok := data["content"].(string); ok {
					if err := adapter.SendText(content); err != nil {
						log.Printf("❌ Failed to send text: %v", err)
					}
				}
			}

		case "control":
			session.UpdateLastActivity()
			if data, ok := msg.Data.(map[string]interface{}); ok {
				if action, ok := data["action"].(string); ok {
					if err := adapter.HandleControl(action, data); err != nil {
						log.Printf("❌ Failed to handle control %s: %v", action, err)
					}
				}
			}

		case "start":
			session.UpdateLastActivity()
		}
	}
}

// handleAdapterEvents listens for events from the adapter and sends to client.
// Uses write deadlines for audio deltas to prevent backpressure from stalling the pipeline.
func (s *RealtimeServer) handleAdapterEvents(session *Session, conn *websocket.Conn, adapter Adapter) {
	for event := range adapter.ReceiveEvents() {
		// Inject sample_rate into session_created so client can match capture rate
		if event.Type == EventTypeSessionCreated {
			if dataMap, ok := event.Data.(map[string]interface{}); ok {
				dataMap["sample_rate"] = adapter.InputSampleRate()
			}
		}

		msg := Message{
			Type:      string(event.Type),
			SessionID: session.ID,
			Timestamp: event.Timestamp.UnixMilli(),
			Data:      event.Data,
		}

		// Set write deadline — if client is too slow, drop audio and move on
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(msg); err != nil {
			// For audio deltas, log and continue (expendable)
			if event.Type == EventTypeAudioDelta {
				log.Printf("⚠️ Dropped audio delta (slow client): %v", err)
				continue
			}
			// For control events, break the loop (connection likely dead)
			break
		}
	}
	// Clear deadline
	conn.SetWriteDeadline(time.Time{})
}

// GetSessions returns all active sessions
func (s *RealtimeServer) GetSessions() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// HandleSessions handles HTTP requests for session management
func HandleSessions(server *RealtimeServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessions := server.GetSessions()

		// Convert to API format
		response := make([]map[string]interface{}, len(sessions))
		for i, session := range sessions {
			response[i] = map[string]interface{}{
				"id":            session.ID,
				"thread_id":     session.ThreadID,
				"provider":      session.Provider,
				"is_active":     session.State.IsActive,
				"turn_state":    session.State.TurnState,
				"created_at":    session.CreatedAt,
				"last_activity": session.LastActivity,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// ==================== Telephony Support (Vonage/Twilio) ====================

// processJSONMessage processes a single JSON message (extracted for reuse)
func (s *RealtimeServer) processJSONMessage(session *Session, msg *Message, adapter Adapter) {
	session.UpdateLastActivity()

	switch msg.Type {
	case "audio":
		if data, ok := msg.Data.(map[string]interface{}); ok {
			if chunk, ok := data["chunk"].(string); ok {
				if err := adapter.SendAudio(chunk); err != nil {
					log.Printf("Failed to send audio: %v", err)
				}
			}
		}

	case "text":
		if data, ok := msg.Data.(map[string]interface{}); ok {
			if content, ok := data["content"].(string); ok {
				if err := adapter.SendText(content); err != nil {
					log.Printf("Failed to send text: %v", err)
				}
			}
		}

	case "control":
		if data, ok := msg.Data.(map[string]interface{}); ok {
			if action, ok := data["action"].(string); ok {
				if err := adapter.HandleControl(action, data); err != nil {
					log.Printf("Failed to handle control: %v", err)
				}
			}
		}
	}
}

// processTelephonyAudio converts raw 16kHz PCM to 24kHz base64 and sends to adapter
func (s *RealtimeServer) processTelephonyAudio(rawPCM []byte, adapter Adapter) {
	// Convert raw bytes to 16-bit samples
	samples16k := bytesToSamples(rawPCM)

	// Resample from 16kHz to 24kHz (ratio 2:3)
	samples24k := resample16kTo24k(samples16k)

	// Convert back to bytes
	resampled := samplesToBytes(samples24k)

	// Base64 encode for the adapter
	base64Audio := base64.StdEncoding.EncodeToString(resampled)

	// Send to adapter
	if err := adapter.SendAudio(base64Audio); err != nil {
		log.Printf("Failed to send telephony audio: %v", err)
	}
}

// handleTelephonyMessages handles incoming binary messages from telephony clients
func (s *RealtimeServer) handleTelephonyMessages(session *Session, conn *websocket.Conn, adapter Adapter) {
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Telephony WebSocket error: %v", err)
			}
			break
		}

		session.UpdateLastActivity()

		if msgType == websocket.BinaryMessage {
			// Raw PCM audio at 16kHz
			s.processTelephonyAudio(data, adapter)
		} else if msgType == websocket.TextMessage {
			// Some telephony providers send JSON control messages
			var controlMsg map[string]interface{}
			if err := json.Unmarshal(data, &controlMsg); err == nil {
				if event, ok := controlMsg["event"].(string); ok {
					switch event {
					case "stop":
						return
					}
				}
			}
		}
	}
}

// handleTelephonyAdapterEvents converts adapter events to raw binary for telephony clients
func (s *RealtimeServer) handleTelephonyAdapterEvents(session *Session, conn *websocket.Conn, adapter Adapter) {
	eventChan := adapter.ReceiveEvents()

	for event := range eventChan {
		switch event.Type {
		case EventTypeAudioDelta:
			// Convert audio from adapter (24kHz base64) to telephony format (16kHz raw)
			if audioData, ok := event.Data.(AudioDeltaData); ok {
				// Decode base64
				decoded, err := base64.StdEncoding.DecodeString(audioData.Chunk)
				if err != nil {
					log.Printf("Failed to decode audio: %v", err)
					continue
				}

				// Convert to samples
				samples24k := bytesToSamples(decoded)

				// Resample from 24kHz to 16kHz (ratio 3:2)
				samples16k := resample24kTo16k(samples24k)

				// Convert back to bytes
				rawPCM := samplesToBytes(samples16k)

				// Send as raw binary
				if err := conn.WriteMessage(websocket.BinaryMessage, rawPCM); err != nil {
					log.Printf("Failed to send telephony audio: %v", err)
					return
				}
			}

		case EventTypeTranscript:
			// Optionally send transcripts as JSON text messages
			if transcriptData, ok := event.Data.(TranscriptData); ok {
				msg := map[string]interface{}{
					"event":      "transcript",
					"role":       transcriptData.Role,
					"content":    transcriptData.Content,
					"session_id": session.ID,
				}
				if err := conn.WriteJSON(msg); err != nil {
					log.Printf("Failed to send transcript: %v", err)
				}
			}

		case EventTypeError:
			// Send errors as JSON
			if errData, ok := event.Data.(ErrorData); ok {
				msg := map[string]interface{}{
					"event":   "error",
					"code":    errData.Code,
					"message": errData.Message,
				}
				conn.WriteJSON(msg)
			}
		}

		// Publish event to bus for observability
		s.eventBus.Publish(events.NewEvent(events.CategorySystem, "realtime_telephony_event", events.LevelDebug).
			WithData("session_id", session.ID).
			WithData("event_type", event.Type).
			WithData("thread_id", session.ThreadID))
	}
}

// ==================== Audio Resampling Utilities ====================

// bytesToSamples converts raw PCM bytes to 16-bit signed samples
func bytesToSamples(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := 0; i < len(samples); i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return samples
}

// samplesToBytes converts 16-bit signed samples to raw PCM bytes
func samplesToBytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	return data
}

// resampleAudioServer resamples PCM16 audio between any two sample rates.
// For downsampling, uses averaging (low-pass filter) to prevent aliasing.
// For upsampling, uses linear interpolation.
func resampleAudioServer(samples []int16, fromRate, toRate int) []int16 {
	if len(samples) == 0 || fromRate == toRate {
		return samples
	}

	outputLen := int(float64(len(samples)) * float64(toRate) / float64(fromRate))
	output := make([]int16, outputLen)

	if fromRate > toRate {
		// Downsampling — average source samples to prevent aliasing.
		// For 48k→24k this averages pairs; for 48k→16k averages triplets; etc.
		ratio := float64(fromRate) / float64(toRate)
		for i := 0; i < outputLen; i++ {
			srcStart := float64(i) * ratio
			srcEnd := float64(i+1) * ratio
			startIdx := int(srcStart)
			endIdx := int(srcEnd)
			if endIdx > len(samples) {
				endIdx = len(samples)
			}
			if startIdx >= len(samples) {
				startIdx = len(samples) - 1
			}
			// Average all source samples that map to this output sample
			var sum int32
			count := 0
			for j := startIdx; j < endIdx; j++ {
				sum += int32(samples[j])
				count++
			}
			if count > 0 {
				output[i] = int16(sum / int32(count))
			}
		}
	} else {
		// Upsampling — linear interpolation (no aliasing concern)
		for i := 0; i < outputLen; i++ {
			srcPos := float64(i) * float64(fromRate) / float64(toRate)
			srcIdx := int(srcPos)
			frac := srcPos - float64(srcIdx)
			if srcIdx >= len(samples)-1 {
				output[i] = samples[len(samples)-1]
			} else {
				sample1 := float64(samples[srcIdx])
				sample2 := float64(samples[srcIdx+1])
				output[i] = int16(sample1 + frac*(sample2-sample1))
			}
		}
	}

	return output
}

// resample16kTo24k resamples audio from 16kHz to 24kHz using linear interpolation
// Ratio: 16000:24000 = 2:3 (for every 2 input samples, output 3 samples)
func resample16kTo24k(samples []int16) []int16 {
	if len(samples) == 0 {
		return samples
	}

	// Output length: input * 24000 / 16000 = input * 1.5
	outputLen := (len(samples) * 3) / 2
	output := make([]int16, outputLen)

	for i := 0; i < outputLen; i++ {
		// Calculate the corresponding position in the input
		srcPos := float64(i) * 16000.0 / 24000.0
		srcIdx := int(srcPos)
		frac := srcPos - float64(srcIdx)

		if srcIdx >= len(samples)-1 {
			output[i] = samples[len(samples)-1]
		} else {
			// Linear interpolation
			sample1 := float64(samples[srcIdx])
			sample2 := float64(samples[srcIdx+1])
			output[i] = int16(sample1 + frac*(sample2-sample1))
		}
	}

	return output
}

// resample24kTo16k resamples audio from 24kHz to 16kHz using linear interpolation
// Ratio: 24000:16000 = 3:2 (for every 3 input samples, output 2 samples)
func resample24kTo16k(samples []int16) []int16 {
	if len(samples) == 0 {
		return samples
	}

	// Output length: input * 16000 / 24000 = input * 2/3
	outputLen := (len(samples) * 2) / 3
	output := make([]int16, outputLen)

	for i := 0; i < outputLen; i++ {
		// Calculate the corresponding position in the input
		srcPos := float64(i) * 24000.0 / 16000.0
		srcIdx := int(srcPos)
		frac := srcPos - float64(srcIdx)

		if srcIdx >= len(samples)-1 {
			output[i] = samples[len(samples)-1]
		} else {
			// Linear interpolation
			sample1 := float64(samples[srcIdx])
			sample2 := float64(samples[srcIdx+1])
			output[i] = int16(sample1 + frac*(sample2-sample1))
		}
	}

	return output
}
