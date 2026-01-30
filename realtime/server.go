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
	log.Printf("🎤 [VOICE] WebSocket connection request from %s", r.RemoteAddr)

	// Check if realtime is enabled in config
	cfg := config.GetConfig()
	realtimeConfig := cfg.Get().Realtime
	if realtimeConfig == nil || !realtimeConfig.Enabled {
		log.Printf("🎤 [VOICE] Realtime not enabled in config")
		http.Error(w, "Realtime voice is not enabled", http.StatusServiceUnavailable)
		return
	}
	log.Printf("🎤 [VOICE] Realtime enabled, provider=%s", realtimeConfig.Provider)

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("🎤 [VOICE] Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()
	log.Printf("🎤 [VOICE] WebSocket upgraded successfully")

	// Create new session
	session := s.createSession(conn)
	defer s.removeSession(session.ID)
	log.Printf("🎤 [VOICE] Session created: %s, thread: %s", session.ID, session.ThreadID)

	// Get realtime provider from config
	provider := "openai" // default
	if realtimeConfig.Provider != "" {
		provider = realtimeConfig.Provider
	}
	session.Provider = provider + "-realtime"

	// Read first message to detect format (JSON vs Binary/Telephony)
	log.Printf("🎤 [VOICE] Waiting for first message to detect format...")
	msgType, firstMsg, err := conn.ReadMessage()
	if err != nil {
		log.Printf("🎤 [VOICE] Failed to read first message: %v", err)
		return
	}
	log.Printf("🎤 [VOICE] First message received: type=%d, len=%d", msgType, len(firstMsg))

	// Detect mode based on first message type
	isTelephonyMode := msgType == websocket.BinaryMessage

	if isTelephonyMode {
		session.Provider = provider + "-realtime-telephony"
		log.Printf("📞 New telephony session: %s (thread: %s, provider: %s)", session.ID, session.ThreadID, provider)
	} else {
		log.Printf("🎙️  New realtime session: %s (thread: %s, provider: %s)", session.ID, session.ThreadID, provider)
	}

	// Publish session created event
	s.eventBus.Publish(events.NewEvent(events.CategorySystem, "realtime_session_created", events.LevelInfo).
		WithData("session_id", session.ID).
		WithData("thread_id", session.ThreadID).
		WithData("provider", provider).
		WithData("telephony_mode", isTelephonyMode))

	// Initialize adapter based on provider
	log.Printf("🎤 [VOICE] Creating %s adapter...", provider)
	var adapter Adapter
	var adapterErr error

	switch provider {
	case "gemini":
		adapter, adapterErr = NewGeminiRealtimeAdapter(session, s.messageSaver, s.eventBus)
	default: // "openai"
		adapter, adapterErr = NewOpenAIRealtimeAdapter(session, s.messageSaver, s.eventBus)
	}

	if adapterErr != nil {
		log.Printf("🎤 [VOICE] ❌ Failed to create %s adapter: %v", provider, adapterErr)
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
	log.Printf("🎤 [VOICE] ✅ %s adapter created successfully", provider)

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
		log.Printf("🎤 [VOICE] JSON mode - sending session_created to client")
		// Send welcome message to client
		welcomeMsg := Message{
			Type:      "session_created",
			SessionID: session.ID,
			Timestamp: time.Now().UnixMilli(),
			Data: map[string]interface{}{
				"session_id": session.ID,
				"thread_id":  session.ThreadID,
				"provider":   provider,
			},
		}
		if err := conn.WriteJSON(welcomeMsg); err != nil {
			log.Printf("🎤 [VOICE] ❌ Failed to send welcome message: %v", err)
			return
		}
		log.Printf("🎤 [VOICE] ✅ session_created sent to client")

		// Process the first JSON message we already read
		var msg Message
		if err := json.Unmarshal(firstMsg, &msg); err == nil {
			log.Printf("🎤 [VOICE] Processing first message: type=%s", msg.Type)
			s.processJSONMessage(session, &msg, adapter)
		} else {
			log.Printf("🎤 [VOICE] First message not valid JSON: %v", err)
		}

		// Start adapter event listener (sends events to client)
		log.Printf("🎤 [VOICE] Starting adapter event listener")
		go s.handleAdapterEvents(session, conn, adapter)

		// Handle incoming messages from client
		log.Printf("🎤 [VOICE] Starting client message handler loop")
		s.handleClientMessages(session, conn, adapter)
		log.Printf("🎤 [VOICE] Client message handler loop ended")
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
	log.Printf("🎤 [VOICE] handleClientMessages starting for session %s", session.ID)
	msgCount := 0
	audioChunks := 0
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("🎤 [VOICE] WebSocket error: %v", err)
			} else {
				log.Printf("🎤 [VOICE] Client disconnected: %v", err)
			}
			break
		}
		msgCount++

		// Update last activity
		session.UpdateLastActivity()

		// Handle message based on type
		switch msg.Type {
		case "audio":
			audioChunks++
			// Only log every 50th audio chunk to avoid spam
			if audioChunks%50 == 1 {
				log.Printf("🎤 [VOICE] Received audio chunk #%d", audioChunks)
			}
			// Extract audio data
			if data, ok := msg.Data.(map[string]interface{}); ok {
				if chunk, ok := data["chunk"].(string); ok {
					if audioChunks%50 == 1 {
						log.Printf("🎤 [VOICE] Sending audio chunk #%d to adapter (len=%d)", audioChunks, len(chunk))
					}
					if err := adapter.SendAudio(chunk); err != nil {
						log.Printf("🎤 [VOICE] ❌ Failed to send audio: %v", err)
					}
				} else {
					log.Printf("🎤 [VOICE] ❌ No 'chunk' in audio data")
				}
			} else {
				log.Printf("🎤 [VOICE] ❌ Audio data not a map: %T", msg.Data)
			}

		case "text":
			log.Printf("🎤 [VOICE] Received text message")
			// Extract text data
			if data, ok := msg.Data.(map[string]interface{}); ok {
				if content, ok := data["content"].(string); ok {
					if err := adapter.SendText(content); err != nil {
						log.Printf("🎤 [VOICE] ❌ Failed to send text: %v", err)
					}
				}
			}

		case "control":
			log.Printf("🎤 [VOICE] Received control message")
			// Handle control messages
			if data, ok := msg.Data.(map[string]interface{}); ok {
				if action, ok := data["action"].(string); ok {
					if err := adapter.HandleControl(action, data); err != nil {
						log.Printf("🎤 [VOICE] ❌ Failed to handle control: %v", err)
					}
				}
			}

		case "start":
			// Start message - just acknowledges session start, already handled
			log.Printf("🎤 [VOICE] Received start message (session already active)")

		default:
			log.Printf("🎤 [VOICE] Unknown message type: %s", msg.Type)
		}
	}
	log.Printf("🎤 [VOICE] handleClientMessages ended - processed %d messages, %d audio chunks", msgCount, audioChunks)
}

// handleAdapterEvents listens for events from the adapter and sends to client
func (s *RealtimeServer) handleAdapterEvents(session *Session, conn *websocket.Conn, adapter Adapter) {
	log.Printf("🎤 [VOICE] handleAdapterEvents starting for session %s", session.ID)
	eventChan := adapter.ReceiveEvents()

	eventCount := 0
	for event := range eventChan {
		eventCount++
		log.Printf("🎤 [VOICE] Event #%d from adapter: type=%s", eventCount, event.Type)

		// Convert unified event to client message
		msg := Message{
			Type:      string(event.Type),
			SessionID: session.ID,
			Timestamp: event.Timestamp.UnixMilli(),
			Data:      event.Data,
		}

		// Send to client
		if err := conn.WriteJSON(msg); err != nil {
			log.Printf("🎤 [VOICE] ❌ Failed to send event to client: %v", err)
			break
		}
		log.Printf("🎤 [VOICE] ✅ Sent event %s to client", event.Type)

		// Publish event to bus for observability
		s.eventBus.Publish(events.NewEvent(events.CategorySystem, "realtime_event", events.LevelDebug).
			WithData("session_id", session.ID).
			WithData("event_type", event.Type).
			WithData("thread_id", session.ThreadID))
	}
	log.Printf("🎤 [VOICE] handleAdapterEvents ended for session %s (processed %d events)", session.ID, eventCount)
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
					log.Printf("📞 Telephony control event: %s", event)
					// Handle common telephony events
					switch event {
					case "start":
						log.Printf("📞 Telephony call started")
					case "stop":
						log.Printf("📞 Telephony call ended")
						return
					case "mark":
						// Audio marker - can be used for synchronization
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
