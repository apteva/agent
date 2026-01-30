package events

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/apteva/agent/config"
)

// TelemetryPayload is the payload sent to the telemetry endpoint
type TelemetryPayload struct {
	AgentID string   `json:"agent_id"`
	Events  []*Event `json:"events"`
	SentAt  string   `json:"sent_at"`
}

// TelemetrySubscriber listens to events and sends them to a remote endpoint
type TelemetrySubscriber struct {
	endpoint      string
	apiKey        string
	buffer        []*Event
	mu            sync.Mutex
	client        *http.Client
	batchSize     int
	flushInterval time.Duration
	flushTicker   *time.Ticker
	stopChan      chan struct{}
	stopped       bool
}

// TelemetryConfig holds configuration for the telemetry subscriber
type TelemetryConfig struct {
	Endpoint      string
	APIKey        string
	BatchSize     int // Default: 10
	FlushInterval int // Seconds, default: 30
}

// NewTelemetrySubscriber creates a new telemetry subscriber
func NewTelemetrySubscriber(cfg TelemetryConfig) *TelemetrySubscriber {
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}

	flushInterval := cfg.FlushInterval
	if flushInterval <= 0 {
		flushInterval = 30
	}

	t := &TelemetrySubscriber{
		endpoint:      cfg.Endpoint,
		apiKey:        cfg.APIKey,
		batchSize:     batchSize,
		flushInterval: time.Duration(flushInterval) * time.Second,
		buffer:        make([]*Event, 0, batchSize),
		client:        &http.Client{Timeout: 5 * time.Second},
		stopChan:      make(chan struct{}),
	}

	// Start flush ticker
	t.flushTicker = time.NewTicker(t.flushInterval)
	go t.flushLoop()

	log.Printf("📡 Telemetry enabled: endpoint=%s, batch_size=%d, flush_interval=%ds",
		cfg.Endpoint, batchSize, flushInterval)

	return t
}

// OnEvent handles incoming events - called by the event bus
func (t *TelemetrySubscriber) OnEvent(event *Event) {
	t.mu.Lock()
	t.buffer = append(t.buffer, event)
	shouldFlush := len(t.buffer) >= t.batchSize
	t.mu.Unlock()

	if shouldFlush {
		go t.flush() // Fire and forget
	}
}

// flushLoop periodically flushes the buffer
func (t *TelemetrySubscriber) flushLoop() {
	for {
		select {
		case <-t.flushTicker.C:
			t.flush()
		case <-t.stopChan:
			t.flushTicker.Stop()
			// Final flush before stopping
			t.flush()
			return
		}
	}
}

// flush sends buffered events to the endpoint
func (t *TelemetrySubscriber) flush() {
	t.mu.Lock()
	if len(t.buffer) == 0 {
		t.mu.Unlock()
		return
	}

	// Take the buffer and reset
	events := t.buffer
	t.buffer = make([]*Event, 0, t.batchSize)
	t.mu.Unlock()

	// Get current agent ID from config (allows dynamic updates)
	cfg := config.GetConfig()
	agentID := cfg.Get().ID

	// Build payload
	payload := TelemetryPayload{
		AgentID: agentID,
		Events:  events,
		SentAt:  time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("📡 Telemetry: failed to marshal payload: %v", err)
		return
	}

	// Fire and forget - send in background, ignore errors
	go func() {
		req, err := http.NewRequest("POST", t.endpoint, bytes.NewReader(data))
		if err != nil {
			return // Silently ignore
		}

		req.Header.Set("Content-Type", "application/json")
		if t.apiKey != "" {
			req.Header.Set("X-API-Key", t.apiKey)
		}

		resp, err := t.client.Do(req)
		if err != nil {
			// Fire and forget - don't log network errors to avoid spam
			return
		}
		resp.Body.Close()

		// Only log success at debug level
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("📡 Telemetry: sent %d events", len(events))
		}
	}()
}

// Stop gracefully stops the telemetry subscriber
func (t *TelemetrySubscriber) Stop() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true
	t.mu.Unlock()

	close(t.stopChan)
}
