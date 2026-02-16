package realtime

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
	"github.com/apteva/agent/handlers/threads"
)

// voiceSystemContext is injected into the LLM system prompt so the model knows
// its response will be spoken aloud and should be conversational, not formatted.
const voiceSystemContext = `You are currently in a VOICE conversation. Your response will be converted to speech and played as audio.

IMPORTANT voice mode rules:
- Respond naturally and conversationally, as if speaking to someone
- Do NOT use markdown formatting, bullet points, numbered lists, headers, or code blocks
- Do NOT use special characters like asterisks, dashes for lists, or brackets
- Keep responses concise and to the point — spoken responses should be brief
- Use natural speech patterns: "You have 7 tasks. 4 are pending and 3 are completed." instead of formatted lists
- If listing items, use natural language: "The pending ones are: review project documentation, send weekly status report, and the two dummy tasks."
- Avoid saying things like "here's a list" and then formatting it — just say it naturally`

// StandardVoiceAdapter implements the Adapter interface using STT + Core LLM + TTS.
// This allows any LLM provider (Anthropic, OpenAI, Gemini, Fireworks, Groq, etc.)
// to be used for voice conversations without requiring a dedicated realtime API.
//
// Full streaming pipeline:
//   Audio → WebSocket STT (realtime) → transcript
//                                        ↓
//                                     LLM (SSE stream)
//                                        ↓ tokens
//                                     WebSocket TTS (realtime) → audio chunks → client
type StandardVoiceAdapter struct {
	session      *Session
	streamingSTT StreamingSTTProvider
	ttsCfg       *config.TTSConfig // Saved config for creating per-turn TTS sessions
	eventChan    chan UnifiedEvent

	// Processing state
	processing bool
	procMu     sync.Mutex

	closeChan chan struct{}
	closeOnce sync.Once
}

// NewStandardVoiceAdapter creates a new standard voice adapter
func NewStandardVoiceAdapter(session *Session, messageSaver threads.MessageSaver,
	eventBus *events.EventBus) (*StandardVoiceAdapter, error) {

	log.Printf("🎤 [STANDARD] Creating Standard Voice adapter (WS STT + SSE LLM + WS TTS)...")

	cfg := config.GetConfig()
	agentConfig := cfg.Get()

	// Initialize streaming STT provider (ElevenLabs Realtime WebSocket)
	streamingSTT, err := NewStreamingSTTProvider(agentConfig.Realtime.STT)
	if err != nil {
		return nil, fmt.Errorf("failed to create streaming STT provider: %w", err)
	}
	log.Printf("🎤 [STANDARD] Streaming STT provider initialized (WebSocket)")

	// Validate TTS config by doing a quick check (don't keep a persistent connection
	// since each turn needs its own WebSocket session — ElevenLabs closes after Finish)
	ttsCfg := agentConfig.Realtime.TTS
	if os.Getenv("ELEVENLABS_API_KEY") == "" {
		streamingSTT.Close()
		return nil, fmt.Errorf("ELEVENLABS_API_KEY environment variable not set (required for TTS)")
	}
	log.Printf("🎤 [STANDARD] TTS config validated (WebSocket per turn)")

	adapter := &StandardVoiceAdapter{
		session:      session,
		streamingSTT: streamingSTT,
		ttsCfg:       ttsCfg,
		eventChan:    make(chan UnifiedEvent, 100),
		closeChan:    make(chan struct{}),
	}

	// Start listening for transcripts from the streaming STT
	go adapter.transcriptLoop()

	// Emit session created event
	adapter.eventChan <- UnifiedEvent{
		Type:      EventTypeSessionCreated,
		SessionID: session.ID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"session_id": session.ID,
			"thread_id":  session.ThreadID,
			"provider":   "standard",
			"mode":       "ws_stt+sse_llm+ws_tts",
		},
	}

	log.Printf("✅ Standard Voice adapter created (session: %s, thread: %s)", session.ID, session.ThreadID)
	return adapter, nil
}

// transcriptLoop listens for transcripts from the streaming STT and processes them
func (a *StandardVoiceAdapter) transcriptLoop() {
	transcripts := a.streamingSTT.Transcripts()
	partials := a.streamingSTT.PartialTranscripts()

	for {
		select {
		case <-a.closeChan:
			return

		case text, ok := <-transcripts:
			if !ok {
				return
			}
			if strings.TrimSpace(text) == "" {
				continue
			}

			// Check if we're already processing
			a.procMu.Lock()
			if a.processing {
				a.procMu.Unlock()
				log.Printf("🎤 [STANDARD] Dropping transcript (already processing): %q", text)
				continue
			}
			a.processing = true
			a.procMu.Unlock()

			log.Printf("🎤 [STANDARD] Committed transcript: %q", text)

			// Emit turn start
			a.eventChan <- UnifiedEvent{
				Type:      EventTypeTurnStart,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data:      map[string]interface{}{"speaker": "user"},
			}

			// Emit user transcript
			a.eventChan <- UnifiedEvent{
				Type:      EventTypeTranscript,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: TranscriptData{
					Role:    "user",
					Content: text,
				},
			}

			// Save user message
			a.session.messageSaver.SaveMessage(a.session.ThreadID, "user", text, nil, nil)

			// Process through LLM + TTS (blocking so we don't overlap)
			a.processLLMResponse(text)

			a.procMu.Lock()
			a.processing = false
			a.procMu.Unlock()

		case partial, ok := <-partials:
			if !ok {
				continue
			}
			if strings.TrimSpace(partial) == "" {
				continue
			}

			// Emit partial transcript so UI can show what user is saying
			a.eventChan <- UnifiedEvent{
				Type:      EventTypeTranscript,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: TranscriptData{
					Role:    "user",
					Content: partial,
					Partial: true,
				},
			}
		}
	}
}

// SendAudio receives base64-encoded PCM16 audio and forwards directly to streaming STT
func (a *StandardVoiceAdapter) SendAudio(base64Audio string) error {
	decoded, err := base64.StdEncoding.DecodeString(base64Audio)
	if err != nil {
		return fmt.Errorf("failed to decode audio: %w", err)
	}

	// Forward directly to streaming STT — ElevenLabs handles VAD
	return a.streamingSTT.SendAudio(decoded)
}

// SendText sends a text message directly to the LLM (bypasses STT)
func (a *StandardVoiceAdapter) SendText(text string) error {
	if text == "" {
		return nil
	}

	a.procMu.Lock()
	if a.processing {
		a.procMu.Unlock()
		return fmt.Errorf("already processing a turn")
	}
	a.processing = true
	a.procMu.Unlock()

	// Emit user transcript
	a.eventChan <- UnifiedEvent{
		Type:      EventTypeTranscript,
		SessionID: a.session.ID,
		Timestamp: time.Now(),
		Data: TranscriptData{
			Role:    "user",
			Content: text,
		},
	}

	// Save user message
	a.session.messageSaver.SaveMessage(a.session.ThreadID, "user", text, nil, nil)

	// Process through LLM + TTS
	go func() {
		a.processLLMResponse(text)
		a.procMu.Lock()
		a.processing = false
		a.procMu.Unlock()
	}()

	return nil
}

// HandleControl handles control messages
func (a *StandardVoiceAdapter) HandleControl(action string, data map[string]interface{}) error {
	switch action {
	case "interrupt":
		log.Printf("🎤 [STANDARD] Interrupt received (noted)")
		return nil
	}
	return nil
}

// ReceiveEvents returns the event channel
func (a *StandardVoiceAdapter) ReceiveEvents() <-chan UnifiedEvent {
	return a.eventChan
}

// Close cleans up all resources
func (a *StandardVoiceAdapter) Close() error {
	a.closeOnce.Do(func() {
		close(a.closeChan)
		a.streamingSTT.Close()
	})
	return nil
}

// processLLMResponse streams LLM response via SSE and pipes text to WebSocket TTS in real-time.
// Flow: SSE tokens → accumulate sentences → send to TTS WebSocket → audio chunks → client
//
// When tool calls happen, the current TTS session is finished and audio drained before
// the tool executes. After tool results, a new TTS session is created for the post-tool
// response, creating a natural audible break.
func (a *StandardVoiceAdapter) processLLMResponse(userMessage string) {
	a.session.SetTurnState("assistant_speaking")

	// --- TTS session management ---
	// We create/destroy TTS sessions as needed. Each "segment" of speech
	// (before tool call, after tool result) gets its own TTS WebSocket.
	var tts StreamingTTSProvider
	var audioForwardDone chan struct{}

	startTTS := func() error {
		var err error
		tts, err = NewStreamingTTSProvider(a.ttsCfg)
		if err != nil {
			return fmt.Errorf("failed to create TTS session: %w", err)
		}

		// Start forwarding TTS audio chunks to client in background
		audioForwardDone = make(chan struct{})
		ttsSampleRate := tts.GetSampleRate()
		go func(t StreamingTTSProvider, done chan struct{}) {
			defer close(done)
			for chunk := range t.AudioChunks() {
				select {
				case <-a.closeChan:
					return
				default:
				}
				encoded := base64.StdEncoding.EncodeToString(chunk)
				a.eventChan <- UnifiedEvent{
					Type:      EventTypeAudioDelta,
					SessionID: a.session.ID,
					Timestamp: time.Now(),
					Data: AudioDeltaData{
						Format:     "pcm16",
						SampleRate: ttsSampleRate,
						Chunk:      encoded,
					},
				}
			}
		}(tts, audioForwardDone)

		return nil
	}

	// finishTTS completes the current TTS session and waits for all audio to drain
	finishTTS := func() {
		if tts == nil {
			return
		}
		tts.Finish()
		<-audioForwardDone // Wait for all audio chunks to be forwarded
		tts.Close()
		tts = nil
		audioForwardDone = nil
	}

	// Start initial TTS session
	if err := startTTS(); err != nil {
		log.Printf("🎤 [STANDARD] %v", err)
		a.emitError("tts_error", err.Error())
		return
	}
	defer func() {
		if tts != nil {
			tts.Close()
		}
	}()

	// --- LLM streaming ---
	port := os.Getenv("PORT")
	if port == "" {
		port = "4015"
	}
	chatURL := fmt.Sprintf("http://localhost:%s/chat", port)

	streamTrue := true
	chatReq := struct {
		Message  string `json:"message"`
		ThreadID string `json:"thread_id"`
		Stream   *bool  `json:"stream"`
		Source   string `json:"source"`
		System   string `json:"system,omitempty"`
	}{
		Message:  userMessage,
		ThreadID: a.session.ThreadID,
		Stream:   &streamTrue,
		Source:   "voice",
		System:   voiceSystemContext,
	}

	reqBody, err := json.Marshal(chatReq)
	if err != nil {
		log.Printf("🎤 [STANDARD] Failed to marshal chat request: %v", err)
		a.emitError("llm_error", "Failed to build request")
		return
	}

	req, err := http.NewRequest("POST", chatURL, bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("🎤 [STANDARD] Failed to create HTTP request: %v", err)
		a.emitError("llm_error", "Failed to create request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	if apiKey := os.Getenv("AGENT_API_KEY"); apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("🎤 [STANDARD] Chat request failed: %v", err)
		a.emitError("llm_error", "Chat request failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		log.Printf("🎤 [STANDARD] Chat returned HTTP %d: %s", resp.StatusCode, string(errBody))
		a.emitError("llm_error", fmt.Sprintf("Chat returned HTTP %d", resp.StatusCode))
		return
	}

	// --- SSE processing with sentence chunking ---
	var fullResponse strings.Builder
	var sentenceBuffer strings.Builder

	flushSentenceBuffer := func() {
		if tts == nil {
			sentenceBuffer.Reset()
			return
		}
		text := strings.TrimSpace(sentenceBuffer.String())
		if text != "" {
			if err := tts.SendText(text); err != nil {
				log.Printf("🔊 [STANDARD] TTS send error (flush): %v", err)
			}
			sentenceBuffer.Reset()
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		jsonData := line[6:]

		var event struct {
			Type     string `json:"type"`
			Content  string `json:"content"`
			ToolName string `json:"tool_name"`
			ToolID   string `json:"tool_id"`
		}
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content":
			if event.Content == "" {
				continue
			}
			fullResponse.WriteString(event.Content)
			sentenceBuffer.WriteString(event.Content)

			if tts == nil {
				continue // No TTS session (between tool call and start)
			}

			// Check if we have a complete sentence to send to TTS
			bufText := sentenceBuffer.String()
			lastSentenceEnd := findLastSentenceEnd(bufText)
			if lastSentenceEnd >= 0 {
				toSend := strings.TrimSpace(bufText[:lastSentenceEnd+1])
				if toSend != "" {
					if err := tts.SendText(toSend); err != nil {
						log.Printf("🔊 [STANDARD] TTS send error: %v", err)
					}
				}
				sentenceBuffer.Reset()
				remaining := bufText[lastSentenceEnd+1:]
				sentenceBuffer.WriteString(remaining)
			}

		case "tool_call":
			// tool_call fires first (before input streaming). Flush text and finish TTS
			// so pre-tool speech plays fully before the pause during tool execution.
			if tts != nil {
				flushSentenceBuffer()
				log.Printf("🎤 [STANDARD] Tool call: %s — finishing TTS segment", event.ToolName)
				finishTTS()
			}

			a.eventChan <- UnifiedEvent{
				Type:      EventTypeToolCall,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: ToolCallData{
					ID:   event.ToolID,
					Name: event.ToolName,
				},
			}

		case "tool_use":
			// tool_use fires after tool_call with full input — TTS already finished,
			// just skip to avoid duplicate processing

		case "tool_result":
			log.Printf("🎤 [STANDARD] Tool result: %s", event.ToolID)
			a.eventChan <- UnifiedEvent{
				Type:      EventTypeToolResult,
				SessionID: a.session.ID,
				Timestamp: time.Now(),
				Data: ToolResultData{
					CallID: event.ToolID,
					Name:   event.ToolName,
				},
			}

		case "start":
			// New LLM turn after tool results — create fresh TTS session
			sentenceBuffer.Reset()
			if tts == nil {
				log.Printf("🎤 [STANDARD] New turn — starting fresh TTS session")
				if err := startTTS(); err != nil {
					log.Printf("🎤 [STANDARD] %v", err)
					a.emitError("tts_error", err.Error())
				}
			}

		case "done":
			log.Printf("🎤 [STANDARD] LLM stream done")
		}
	}

	// Flush remaining text and finish the last TTS session
	flushSentenceBuffer()
	finishTTS()

	fullText := fullResponse.String()
	if fullText == "" {
		log.Printf("🎤 [STANDARD] Empty LLM response")
		a.emitError("llm_error", "Empty response from LLM")
		return
	}

	log.Printf("🎤 [STANDARD] LLM response: %d chars", len(fullText))

	// Emit assistant transcript (full text after all audio sent)
	a.eventChan <- UnifiedEvent{
		Type:      EventTypeTranscript,
		SessionID: a.session.ID,
		Timestamp: time.Now(),
		Data: TranscriptData{
			Role:    "assistant",
			Content: fullText,
		},
	}

	// Signal audio complete
	a.eventChan <- UnifiedEvent{
		Type:      EventTypeAudioComplete,
		SessionID: a.session.ID,
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"source": "standard_voice"},
	}

	// Signal turn end
	a.session.SetTurnState("idle")
	a.eventChan <- UnifiedEvent{
		Type:      EventTypeTurnEnd,
		SessionID: a.session.ID,
		Timestamp: time.Now(),
		Data:      map[string]interface{}{},
	}

	log.Printf("🎤 [STANDARD] Turn complete")
}

// findLastSentenceEnd returns the index of the last sentence-ending character in text,
// or -1 if none found. Used to split streaming LLM output at natural boundaries.
func findLastSentenceEnd(text string) int {
	lastIdx := -1
	runes := []rune(text)
	for i, r := range runes {
		if isSentenceEnd(r) {
			// Make sure it's a real boundary (not e.g. "3.14")
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) || unicode.IsUpper(runes[i+1]) {
				lastIdx = i
			}
		}
	}
	// Convert rune index to byte index
	if lastIdx >= 0 {
		return len(string(runes[:lastIdx+1])) - 1
	}
	return -1
}

// emitError sends an error event
func (a *StandardVoiceAdapter) emitError(code, message string) {
	a.eventChan <- UnifiedEvent{
		Type:      EventTypeError,
		SessionID: a.session.ID,
		Timestamp: time.Now(),
		Data: ErrorData{
			Code:    code,
			Message: message,
		},
	}
}

// splitSentences splits text into sentences at natural boundaries for TTS chunking.
// This provides lower latency by sending sentences to TTS as they complete rather
// than waiting for the full response.
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		// Check for sentence boundaries
		if isSentenceEnd(runes[i]) {
			// Look ahead: if next char is space or end, it's a boundary
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) || unicode.IsUpper(runes[i+1]) {
				s := strings.TrimSpace(current.String())
				if s != "" {
					sentences = append(sentences, s)
				}
				current.Reset()
			}
		}
	}

	// Flush remaining text
	s := strings.TrimSpace(current.String())
	if s != "" {
		sentences = append(sentences, s)
	}

	return sentences
}

// isSentenceEnd checks if a rune is a sentence-ending punctuation
func isSentenceEnd(r rune) bool {
	return r == '.' || r == '!' || r == '?' || r == '\n'
}
