package realtime

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/apteva/agent/config"
	"github.com/gorilla/websocket"
)

// TTSProvider converts text to speech audio
type TTSProvider interface {
	// Synthesize converts text to PCM16 audio chunks streamed via channel.
	// The channel receives raw PCM16 little-endian byte slices.
	// Channel is closed when synthesis is complete.
	Synthesize(text string) (<-chan []byte, error)

	// SynthesizeFull converts text to a single PCM16 audio buffer (non-streaming)
	SynthesizeFull(text string) ([]byte, error)

	// GetSampleRate returns the output sample rate
	GetSampleRate() int

	Close() error
}

// StreamingTTSProvider converts text to speech via streaming WebSocket.
// Text chunks can be sent progressively as LLM tokens arrive.
type StreamingTTSProvider interface {
	// SendText sends a text chunk to the TTS service (call multiple times as LLM streams)
	SendText(text string) error
	// Flush forces generation of any buffered text
	Flush() error
	// Finish signals end of text input and waits for all audio
	Finish() error
	// AudioChunks returns a channel that receives PCM16 audio chunks
	AudioChunks() <-chan []byte
	// GetSampleRate returns the output sample rate
	GetSampleRate() int
	Close() error
}

// NewTTSProvider creates a batch TTS provider based on config
func NewTTSProvider(cfg *config.TTSConfig) (TTSProvider, error) {
	provider := "elevenlabs"
	if cfg != nil && cfg.Provider != "" {
		provider = cfg.Provider
	}

	switch provider {
	case "elevenlabs":
		return NewElevenLabsTTS(cfg)
	default:
		return nil, fmt.Errorf("unknown TTS provider: %s", provider)
	}
}

// NewStreamingTTSProvider creates a streaming TTS provider based on config
func NewStreamingTTSProvider(cfg *config.TTSConfig) (StreamingTTSProvider, error) {
	provider := "elevenlabs"
	if cfg != nil && cfg.Provider != "" {
		provider = cfg.Provider
	}

	switch provider {
	case "elevenlabs":
		return NewElevenLabsRealtimeTTS(cfg)
	default:
		return nil, fmt.Errorf("streaming TTS not supported for provider: %s (only elevenlabs)", provider)
	}
}

// ============================================================================
// ElevenLabs TTS
// ============================================================================

// ElevenLabsTTS implements TTSProvider using ElevenLabs streaming API
type ElevenLabsTTS struct {
	apiKey     string
	voiceID    string
	model      string
	sampleRate int
	stability  float64
	similarity float64
	speed      float64
}

// NewElevenLabsTTS creates a new ElevenLabs TTS provider
func NewElevenLabsTTS(cfg *config.TTSConfig) (*ElevenLabsTTS, error) {
	apiKey := os.Getenv("ELEVENLABS_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ELEVENLABS_API_KEY environment variable not set")
	}

	tts := &ElevenLabsTTS{
		apiKey:     apiKey,
		voiceID:    "21m00Tcm4TlvDq8ikWAM", // Rachel (default)
		model:      "eleven_turbo_v2_5",
		sampleRate: 24000,
		stability:  0.5,
		similarity: 0.75,
		speed:      1.0,
	}

	if cfg != nil {
		if cfg.Voice != "" {
			tts.voiceID = cfg.Voice
		}
		if cfg.Model != "" {
			tts.model = cfg.Model
		}
		if cfg.Speed > 0 {
			tts.speed = cfg.Speed
		}
		if cfg.SampleRate > 0 {
			tts.sampleRate = cfg.SampleRate
		}
		if cfg.Stability > 0 {
			tts.stability = cfg.Stability
		}
		if cfg.Similarity > 0 {
			tts.similarity = cfg.Similarity
		}
	}

	log.Printf("🔊 [TTS] ElevenLabs initialized: voice=%s, model=%s, rate=%dHz",
		tts.voiceID, tts.model, tts.sampleRate)

	return tts, nil
}

// Synthesize streams PCM16 audio chunks from ElevenLabs
func (e *ElevenLabsTTS) Synthesize(text string) (<-chan []byte, error) {
	if text == "" {
		ch := make(chan []byte)
		close(ch)
		return ch, nil
	}

	// Build request
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s/stream?output_format=pcm_%d",
		e.voiceID, e.sampleRate)

	body := map[string]interface{}{
		"text":     text,
		"model_id": e.model,
		"voice_settings": map[string]interface{}{
			"stability":        e.stability,
			"similarity_boost": e.similarity,
			"speed":            e.speed,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", e.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ElevenLabs API request failed: %w", err)
	}

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ElevenLabs API error (HTTP %d): %s", resp.StatusCode, string(errBody))
	}

	// Stream audio chunks
	ch := make(chan []byte, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		buf := make([]byte, 4096) // Read in 4KB chunks
		totalBytes := 0

		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				ch <- chunk
				totalBytes += n
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("🔊 [TTS] Read error: %v", err)
				}
				break
			}
		}

		log.Printf("🔊 [TTS] Streamed %d bytes of PCM audio", totalBytes)
	}()

	return ch, nil
}

// SynthesizeFull returns all audio as a single buffer
func (e *ElevenLabsTTS) SynthesizeFull(text string) ([]byte, error) {
	ch, err := e.Synthesize(text)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	for chunk := range ch {
		buf.Write(chunk)
	}

	return buf.Bytes(), nil
}

// GetSampleRate returns the configured output sample rate
func (e *ElevenLabsTTS) GetSampleRate() int {
	return e.sampleRate
}

// Close cleans up resources
func (e *ElevenLabsTTS) Close() error {
	return nil
}

// ============================================================================
// ElevenLabs Realtime TTS (WebSocket streaming)
// ============================================================================

// ElevenLabsRealtimeTTS implements StreamingTTSProvider using ElevenLabs WebSocket API.
// Text chunks are sent as they arrive from the LLM, and audio is streamed back
// progressively for ultra-low latency text-to-speech.
type ElevenLabsRealtimeTTS struct {
	apiKey     string
	voiceID    string
	model      string
	sampleRate int
	stability  float64
	similarity float64
	speed      float64

	conn      *websocket.Conn
	connMu    sync.Mutex
	audioCh   chan []byte
	closeChan chan struct{}
	closeOnce sync.Once
	done      chan struct{} // Signals when isFinal received
}

// NewElevenLabsRealtimeTTS creates a streaming TTS provider
func NewElevenLabsRealtimeTTS(cfg *config.TTSConfig) (*ElevenLabsRealtimeTTS, error) {
	apiKey := os.Getenv("ELEVENLABS_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ELEVENLABS_API_KEY environment variable not set")
	}

	tts := &ElevenLabsRealtimeTTS{
		apiKey:     apiKey,
		voiceID:    "21m00Tcm4TlvDq8ikWAM", // Rachel (default)
		model:      "eleven_turbo_v2_5",
		sampleRate: 24000,
		stability:  0.5,
		similarity: 0.75,
		speed:      1.0,
		audioCh:    make(chan []byte, 64),
		closeChan:  make(chan struct{}),
		done:       make(chan struct{}),
	}

	if cfg != nil {
		if cfg.Voice != "" {
			tts.voiceID = cfg.Voice
		}
		if cfg.Model != "" {
			tts.model = cfg.Model
		}
		if cfg.Speed > 0 {
			tts.speed = cfg.Speed
		}
		if cfg.SampleRate > 0 {
			tts.sampleRate = cfg.SampleRate
		}
		if cfg.Stability > 0 {
			tts.stability = cfg.Stability
		}
		if cfg.Similarity > 0 {
			tts.similarity = cfg.Similarity
		}
	}

	// Connect WebSocket
	if err := tts.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to ElevenLabs TTS WebSocket: %w", err)
	}

	log.Printf("🔊 [TTS] ElevenLabs Realtime initialized: voice=%s, model=%s, rate=%dHz (WebSocket streaming)",
		tts.voiceID, tts.model, tts.sampleRate)

	return tts, nil
}

// connect establishes the WebSocket connection and sends the init message
func (e *ElevenLabsRealtimeTTS) connect() error {
	wsURL := fmt.Sprintf("wss://api.elevenlabs.io/v1/text-to-speech/%s/stream-input?model_id=%s&output_format=pcm_%d&inactivity_timeout=60",
		e.voiceID, e.model, e.sampleRate)

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

	// Send initialization message (required first message with space text)
	initMsg := map[string]interface{}{
		"text": " ",
		"voice_settings": map[string]interface{}{
			"stability":        e.stability,
			"similarity_boost": e.similarity,
			"speed":            e.speed,
		},
		"generation_config": map[string]interface{}{
			"chunk_length_schedule": []int{120, 160, 250, 290},
		},
	}

	if err := conn.WriteJSON(initMsg); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send init message: %w", err)
	}

	// Start reading audio responses
	go e.readLoop()

	return nil
}

// readLoop reads audio chunks from the ElevenLabs WebSocket
func (e *ElevenLabsRealtimeTTS) readLoop() {
	defer func() {
		// Close audio channel so consumers know we're done
		close(e.audioCh)
		// Signal done when read loop exits
		select {
		case <-e.done:
		default:
			close(e.done)
		}
	}()

	for {
		_, message, err := e.conn.ReadMessage()
		if err != nil {
			select {
			case <-e.closeChan:
				return // Normal close
			default:
				log.Printf("🔊 [TTS] WebSocket read error: %v", err)
				return
			}
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("🔊 [TTS] Failed to parse message: %v", err)
			continue
		}

		// Check for final message
		if isFinal, ok := msg["isFinal"].(bool); ok && isFinal {
			log.Printf("🔊 [TTS] Received isFinal signal")
			select {
			case <-e.done:
			default:
				close(e.done)
			}
			return
		}

		// Extract audio data
		audioB64, ok := msg["audio"].(string)
		if !ok || audioB64 == "" {
			continue
		}

		audioData, err := base64.StdEncoding.DecodeString(audioB64)
		if err != nil {
			log.Printf("🔊 [TTS] Failed to decode audio: %v", err)
			continue
		}

		select {
		case e.audioCh <- audioData:
		case <-e.closeChan:
			return
		}
	}
}

// SendText sends a text chunk to the TTS WebSocket
func (e *ElevenLabsRealtimeTTS) SendText(text string) error {
	if text == "" {
		return nil
	}

	e.connMu.Lock()
	defer e.connMu.Unlock()

	if e.conn == nil {
		return fmt.Errorf("WebSocket not connected")
	}

	// Text should end with a space per ElevenLabs docs
	msg := map[string]interface{}{
		"text":                 text + " ",
		"try_trigger_generation": false,
	}

	return e.conn.WriteJSON(msg)
}

// Flush forces generation of any buffered text
func (e *ElevenLabsRealtimeTTS) Flush() error {
	e.connMu.Lock()
	defer e.connMu.Unlock()

	if e.conn == nil {
		return fmt.Errorf("WebSocket not connected")
	}

	msg := map[string]interface{}{
		"text":  " ",
		"flush": true,
	}

	return e.conn.WriteJSON(msg)
}

// Finish signals end of text and waits for all audio to be received
func (e *ElevenLabsRealtimeTTS) Finish() error {
	e.connMu.Lock()
	if e.conn == nil {
		e.connMu.Unlock()
		return nil
	}

	// Send empty text to signal end
	msg := map[string]interface{}{
		"text": "",
	}
	err := e.conn.WriteJSON(msg)
	e.connMu.Unlock()

	if err != nil {
		return fmt.Errorf("failed to send finish: %w", err)
	}

	// Wait for isFinal or timeout
	select {
	case <-e.done:
		return nil
	case <-e.closeChan:
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for TTS completion")
	}
}

// AudioChunks returns the channel of PCM16 audio chunks
func (e *ElevenLabsRealtimeTTS) AudioChunks() <-chan []byte {
	return e.audioCh
}

// GetSampleRate returns the configured output sample rate
func (e *ElevenLabsRealtimeTTS) GetSampleRate() int {
	return e.sampleRate
}

// Close shuts down the WebSocket connection
func (e *ElevenLabsRealtimeTTS) Close() error {
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
