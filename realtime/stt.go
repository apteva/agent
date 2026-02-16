package realtime

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/apteva/agent/config"
	"github.com/gorilla/websocket"
)

// STTProvider transcribes audio to text (batch mode)
type STTProvider interface {
	// Transcribe converts audio data (PCM16 little-endian) to text
	Transcribe(audioData []byte, sampleRate int) (string, error)
	Close() error
}

// StreamingSTTProvider transcribes audio in real-time via streaming
type StreamingSTTProvider interface {
	// SendAudio sends a PCM16 audio chunk to the STT service
	SendAudio(pcm16Data []byte) error
	// Transcripts returns a channel that receives committed (final) transcripts
	Transcripts() <-chan string
	// PartialTranscripts returns a channel that receives interim/partial transcripts
	PartialTranscripts() <-chan string
	Close() error
}

// NewSTTProvider creates a batch STT provider based on config
func NewSTTProvider(cfg *config.STTConfig) (STTProvider, error) {
	provider := "elevenlabs"
	if cfg != nil && cfg.Provider != "" {
		provider = cfg.Provider
	}

	switch provider {
	case "elevenlabs":
		return NewElevenLabsSTT(cfg)
	case "whisper", "openai":
		return NewWhisperSTT(cfg)
	default:
		return nil, fmt.Errorf("unknown STT provider: %s", provider)
	}
}

// NewStreamingSTTProvider creates a streaming STT provider based on config
func NewStreamingSTTProvider(cfg *config.STTConfig) (StreamingSTTProvider, error) {
	provider := "elevenlabs"
	if cfg != nil && cfg.Provider != "" {
		provider = cfg.Provider
	}

	switch provider {
	case "elevenlabs":
		return NewElevenLabsRealtimeSTT(cfg)
	default:
		return nil, fmt.Errorf("streaming STT not supported for provider: %s (only elevenlabs)", provider)
	}
}

// ============================================================================
// OpenAI Whisper STT
// ============================================================================

// WhisperSTT implements STTProvider using OpenAI Whisper API
type WhisperSTT struct {
	apiKey   string
	model    string
	language string
	baseURL  string
}

// NewWhisperSTT creates a new Whisper STT provider
func NewWhisperSTT(cfg *config.STTConfig) (*WhisperSTT, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set (required for Whisper STT)")
	}

	model := "whisper-1"
	language := ""
	baseURL := "https://api.openai.com/v1"

	if cfg != nil {
		if cfg.Model != "" {
			model = cfg.Model
		}
		if cfg.Language != "" {
			language = cfg.Language
		}
		if cfg.BaseURL != "" {
			baseURL = cfg.BaseURL
		}
	}

	return &WhisperSTT{
		apiKey:   apiKey,
		model:    model,
		language: language,
		baseURL:  baseURL,
	}, nil
}

// Transcribe sends PCM16 audio to Whisper API and returns transcript
func (w *WhisperSTT) Transcribe(audioData []byte, sampleRate int) (string, error) {
	if len(audioData) < 100 {
		return "", fmt.Errorf("audio too short (%d bytes)", len(audioData))
	}

	// Wrap raw PCM16 in a WAV container (Whisper requires a file format)
	wavData := pcmToWAV(audioData, sampleRate, 1, 16)

	// Build multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add the audio file
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(wavData); err != nil {
		return "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add model field
	if err := writer.WriteField("model", w.model); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	// Add language if specified
	if w.language != "" {
		if err := writer.WriteField("language", w.language); err != nil {
			return "", fmt.Errorf("failed to write language field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Make HTTP request
	url := w.baseURL + "/audio/transcriptions"
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("whisper API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	log.Printf("🎤 [STT] Whisper transcribed %d bytes → %q", len(audioData), result.Text)
	return result.Text, nil
}

// Close cleans up resources
func (w *WhisperSTT) Close() error {
	return nil
}

// ============================================================================
// ElevenLabs Scribe STT
// ============================================================================

// ElevenLabsSTT implements STTProvider using ElevenLabs Scribe API
type ElevenLabsSTT struct {
	apiKey   string
	model    string
	language string
}

// NewElevenLabsSTT creates a new ElevenLabs STT provider
func NewElevenLabsSTT(cfg *config.STTConfig) (*ElevenLabsSTT, error) {
	apiKey := os.Getenv("ELEVENLABS_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ELEVENLABS_API_KEY environment variable not set (required for ElevenLabs STT)")
	}

	stt := &ElevenLabsSTT{
		apiKey:   apiKey,
		model:    "scribe_v2",
		language: "",
	}

	if cfg != nil {
		if cfg.Model != "" {
			stt.model = cfg.Model
		}
		if cfg.Language != "" {
			stt.language = cfg.Language
		}
	}

	log.Printf("🎤 [STT] ElevenLabs Scribe initialized: model=%s", stt.model)
	return stt, nil
}

// Transcribe sends PCM16 audio to ElevenLabs Scribe API and returns transcript
func (e *ElevenLabsSTT) Transcribe(audioData []byte, sampleRate int) (string, error) {
	if len(audioData) < 100 {
		return "", fmt.Errorf("audio too short (%d bytes)", len(audioData))
	}

	// Wrap raw PCM16 in a WAV container
	wavData := pcmToWAV(audioData, sampleRate, 1, 16)

	// Build multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add the audio file
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(wavData); err != nil {
		return "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add model_id field
	if err := writer.WriteField("model_id", e.model); err != nil {
		return "", fmt.Errorf("failed to write model_id field: %w", err)
	}

	// Add language if specified
	if e.language != "" {
		if err := writer.WriteField("language_code", e.language); err != nil {
			return "", fmt.Errorf("failed to write language_code field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Make HTTP request
	url := "https://api.elevenlabs.io/v1/speech-to-text"
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("xi-api-key", e.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ElevenLabs STT API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("ElevenLabs STT API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	log.Printf("🎤 [STT] ElevenLabs transcribed %d bytes → %q", len(audioData), result.Text)
	return result.Text, nil
}

// Close cleans up resources
func (e *ElevenLabsSTT) Close() error {
	return nil
}

// ============================================================================
// ElevenLabs Realtime STT (WebSocket streaming)
// ============================================================================

// ElevenLabsRealtimeSTT implements StreamingSTTProvider using ElevenLabs WebSocket API.
// Audio chunks are streamed in real-time and ElevenLabs handles VAD server-side,
// returning committed_transcript when it detects the user has finished speaking.
type ElevenLabsRealtimeSTT struct {
	apiKey   string
	model    string
	language string

	conn       *websocket.Conn
	connMu     sync.Mutex
	transcripts chan string
	partials    chan string
	closeChan   chan struct{}
	closeOnce   sync.Once
}

// NewElevenLabsRealtimeSTT creates a streaming STT provider
func NewElevenLabsRealtimeSTT(cfg *config.STTConfig) (*ElevenLabsRealtimeSTT, error) {
	apiKey := os.Getenv("ELEVENLABS_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ELEVENLABS_API_KEY environment variable not set (required for ElevenLabs Realtime STT)")
	}

	stt := &ElevenLabsRealtimeSTT{
		apiKey:      apiKey,
		model:       "scribe_v2",
		language:    "",
		transcripts: make(chan string, 32),
		partials:    make(chan string, 32),
		closeChan:   make(chan struct{}),
	}

	if cfg != nil {
		if cfg.Model != "" {
			stt.model = cfg.Model
		}
		if cfg.Language != "" {
			stt.language = cfg.Language
		}
	}

	// Connect WebSocket
	if err := stt.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to ElevenLabs Realtime STT: %w", err)
	}

	log.Printf("🎤 [STT] ElevenLabs Realtime initialized: model=%s (WebSocket streaming)", stt.model)
	return stt, nil
}

// connect establishes the WebSocket connection to ElevenLabs
func (e *ElevenLabsRealtimeSTT) connect() error {
	// Build WebSocket URL with query params
	wsURL := fmt.Sprintf("wss://api.elevenlabs.io/v1/speech-to-text/realtime?model_id=%s&commit_strategy=vad&audio_format=pcm_16000&vad_silence_threshold_secs=1.2&encoding=pcm_s16le",
		e.model)

	if e.language != "" {
		wsURL += "&language_code=" + e.language
	}

	// Connect with API key header
	header := http.Header{}
	header.Set("xi-api-key", e.apiKey)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("WebSocket dial failed: %w", err)
	}

	e.conn = conn

	// Start reading messages
	go e.readLoop()

	return nil
}

// readLoop reads messages from the ElevenLabs WebSocket
func (e *ElevenLabsRealtimeSTT) readLoop() {
	defer func() {
		e.closeOnce.Do(func() {
			close(e.closeChan)
		})
	}()

	for {
		_, message, err := e.conn.ReadMessage()
		if err != nil {
			select {
			case <-e.closeChan:
				return // Normal close
			default:
				log.Printf("🎤 [STT] WebSocket read error: %v", err)
				return
			}
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("🎤 [STT] Failed to parse message: %v", err)
			continue
		}

		msgType, _ := msg["message_type"].(string)

		switch msgType {
		case "session_started":
			sessionID, _ := msg["session_id"].(string)
			log.Printf("🎤 [STT] Realtime session started: %s", sessionID)

		case "partial_transcript":
			text, _ := msg["text"].(string)
			if text != "" {
				select {
				case e.partials <- text:
				default:
					// Drop if channel full
				}
			}

		case "committed_transcript":
			text, _ := msg["text"].(string)
			if text != "" {
				log.Printf("🎤 [STT] Committed transcript: %q", text)
				select {
				case e.transcripts <- text:
				default:
					log.Printf("🎤 [STT] Warning: transcript channel full, dropping")
				}
			}

		case "committed_transcript_with_timestamps":
			text, _ := msg["text"].(string)
			if text != "" {
				log.Printf("🎤 [STT] Committed transcript (w/ timestamps): %q", text)
				select {
				case e.transcripts <- text:
				default:
					log.Printf("🎤 [STT] Warning: transcript channel full, dropping")
				}
			}

		default:
			// Check for error types
			if errMsg, ok := msg["error"].(string); ok {
				log.Printf("🎤 [STT] ElevenLabs error (%s): %s", msgType, errMsg)
			}
		}
	}
}

// SendAudio sends a PCM16 audio chunk to ElevenLabs via WebSocket
func (e *ElevenLabsRealtimeSTT) SendAudio(pcm16Data []byte) error {
	if len(pcm16Data) == 0 {
		return nil
	}

	e.connMu.Lock()
	defer e.connMu.Unlock()

	if e.conn == nil {
		return fmt.Errorf("WebSocket not connected")
	}

	// ElevenLabs expects: {"message_type": "input_audio_chunk", "audio_base_64": "..."}
	msg := map[string]interface{}{
		"message_type": "input_audio_chunk",
		"audio_base_64": base64.StdEncoding.EncodeToString(pcm16Data),
	}

	return e.conn.WriteJSON(msg)
}

// Transcripts returns the channel of committed transcripts
func (e *ElevenLabsRealtimeSTT) Transcripts() <-chan string {
	return e.transcripts
}

// PartialTranscripts returns the channel of partial/interim transcripts
func (e *ElevenLabsRealtimeSTT) PartialTranscripts() <-chan string {
	return e.partials
}

// Close shuts down the WebSocket connection
func (e *ElevenLabsRealtimeSTT) Close() error {
	e.closeOnce.Do(func() {
		close(e.closeChan)
	})

	e.connMu.Lock()
	defer e.connMu.Unlock()

	if e.conn != nil {
		e.conn.Close()
		e.conn = nil
	}
	return nil
}

// ============================================================================
// Audio format utilities
// ============================================================================

// pcmToWAV wraps raw PCM data in a WAV container
func pcmToWAV(pcmData []byte, sampleRate, numChannels, bitsPerSample int) []byte {
	dataSize := len(pcmData)
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8

	buf := new(bytes.Buffer)

	// RIFF header
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")

	// fmt subchunk
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))          // Subchunk1Size
	binary.Write(buf, binary.LittleEndian, uint16(1))           // AudioFormat (PCM)
	binary.Write(buf, binary.LittleEndian, uint16(numChannels)) // NumChannels
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))  // SampleRate
	binary.Write(buf, binary.LittleEndian, uint32(byteRate))    // ByteRate
	binary.Write(buf, binary.LittleEndian, uint16(blockAlign))  // BlockAlign
	binary.Write(buf, binary.LittleEndian, uint16(bitsPerSample))

	// data subchunk
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	buf.Write(pcmData)

	return buf.Bytes()
}
