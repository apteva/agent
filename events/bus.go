package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Subscriber represents an event subscriber
type Subscriber struct {
	ID         string
	Channel    chan *Event
	Filters    SubscriptionFilters
	BufferSize int
	Active     bool
	mu         sync.RWMutex
}

// SubscriptionFilters defines filtering options for subscribers
type SubscriptionFilters struct {
	Categories []EventCategory
	Types      []EventType
	Levels     []EventLevel
	ThreadID   string
	SessionID  string
}

// EventBus manages event publishing and subscriptions
type EventBus struct {
	subscribers     map[string]*Subscriber
	funcSubscribers map[string]*FuncSubscriber // Callback-based subscribers
	ringBuffer      *RingBuffer
	mu              sync.RWMutex
	running         bool
	stopChan        chan bool
	config          BusConfig
	db              *sql.DB
}

// BusConfig contains configuration for the event bus
type BusConfig struct {
	BufferSize       int           // Size of the ring buffer
	SubscriberBuffer int           // Default buffer size for subscribers
	RetentionPeriod  time.Duration // How long to keep events
	EnablePersistence bool         // Whether to persist events to database
}

// DefaultBusConfig returns default configuration
func DefaultBusConfig() BusConfig {
	return BusConfig{
		BufferSize:        1000, // Reduced from 10000 to limit memory usage
		SubscriberBuffer:  100,
		RetentionPeriod:   24 * time.Hour,
		EnablePersistence: false,
	}
}

var (
	globalBus  *EventBus
	busOnce    sync.Once
)

// GetEventBus returns the global event bus instance
func GetEventBus() *EventBus {
	busOnce.Do(func() {
		globalBus = NewEventBus(DefaultBusConfig())
		globalBus.Start()
	})
	return globalBus
}

// SetEventBusDB sets the database connection for the global event bus
func SetEventBusDB(db *sql.DB) {
	bus := GetEventBus()
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.db = db
}

// EnablePersistence enables database persistence for events
func EnablePersistence(db *sql.DB) {
	bus := GetEventBus()
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.db = db
	bus.config.EnablePersistence = true
}

// NewEventBus creates a new event bus
func NewEventBus(config BusConfig) *EventBus {
	return &EventBus{
		subscribers: make(map[string]*Subscriber),
		ringBuffer:  NewRingBuffer(config.BufferSize),
		stopChan:    make(chan bool, 1),
		config:      config,
	}
}

// Start begins the event bus processing
func (eb *EventBus) Start() error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.running {
		return fmt.Errorf("event bus already running")
	}

	eb.running = true

	// Start cleanup goroutine if retention is enabled
	if eb.config.RetentionPeriod > 0 {
		go eb.cleanupLoop()
	}

	return nil
}

// Stop gracefully stops the event bus
func (eb *EventBus) Stop() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if !eb.running {
		return
	}

	eb.running = false
	close(eb.stopChan)

	// Close all subscriber channels
	for _, sub := range eb.subscribers {
		sub.mu.Lock()
		sub.Active = false
		close(sub.Channel)
		sub.mu.Unlock()
	}
}

// Publish sends an event to all matching subscribers (backwards compatible)
func (eb *EventBus) Publish(event *Event) error {
	return eb.PublishWithContext(nil, event)
}

// PublishWithContext sends an event with trace context from the provided context
func (eb *EventBus) PublishWithContext(ctx context.Context, event *Event) error {
	eb.mu.RLock()
	if !eb.running {
		eb.mu.RUnlock()
		return fmt.Errorf("event bus not running")
	}
	eb.mu.RUnlock()

	// Auto-attach trace/span context - prefer context, fall back to global
	if event.SpanID == "" {
		// First try context-based span
		if ctx != nil {
			if span := SpanFromContext(ctx); span != nil {
				event.TraceID = span.TraceID
				event.SpanID = span.ID
			}
		}
		// Fall back to global span (for backwards compatibility)
		if event.SpanID == "" {
			if span := GetCurrentSpan(); span != nil {
				event.TraceID = span.TraceID
				event.SpanID = span.ID
			}
		}
	}

	// Add to ring buffer
	eb.ringBuffer.Add(event)

	// Persist to database if enabled
	if eb.config.EnablePersistence && eb.db != nil {
		go eb.persistEvent(event)
	}

	// Send to matching subscribers
	eb.mu.RLock()
	subscribers := make([]*Subscriber, 0, len(eb.subscribers))
	for _, sub := range eb.subscribers {
		subscribers = append(subscribers, sub)
	}
	eb.mu.RUnlock()

	for _, sub := range subscribers {
		if eb.matchesFilters(event, sub.Filters) {
			sub.mu.RLock()
			if sub.Active {
				select {
				case sub.Channel <- event:
					// Event sent successfully
				default:
					// Channel full, skip this event
					// In production, might want to log or handle this
				}
			}
			sub.mu.RUnlock()
		}
	}

	// Send to func subscribers (fire-and-forget callbacks)
	eb.mu.RLock()
	funcSubs := make([]*FuncSubscriber, 0, len(eb.funcSubscribers))
	for _, sub := range eb.funcSubscribers {
		funcSubs = append(funcSubs, sub)
	}
	eb.mu.RUnlock()

	for _, sub := range funcSubs {
		if eb.matchesFilters(event, sub.Filters) {
			sub.mu.RLock()
			if sub.Active {
				// Call callback in goroutine to avoid blocking
				callback := sub.Callback
				sub.mu.RUnlock()
				go callback(event)
			} else {
				sub.mu.RUnlock()
			}
		}
	}

	return nil
}

// persistEvent saves an event to the database
func (eb *EventBus) persistEvent(event *Event) error {
	dataJSON, _ := json.Marshal(event.Data)
	metadataJSON, _ := json.Marshal(event.Metadata)

	// Convert empty strings to nil for foreign key columns to avoid constraint violations
	var traceID, spanID, threadID, sessionID interface{}
	if event.TraceID != "" {
		traceID = event.TraceID
	}
	if event.SpanID != "" {
		spanID = event.SpanID
	}
	if event.ThreadID != "" {
		threadID = event.ThreadID
	}
	if event.SessionID != "" {
		sessionID = event.SessionID
	}

	_, err := eb.db.Exec(`
		INSERT INTO events (id, trace_id, span_id, timestamp, category, type, level,
		                    thread_id, session_id, data, metadata, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, traceID, spanID, event.Timestamp, event.Category, event.Type, event.Level,
		threadID, sessionID, string(dataJSON), string(metadataJSON), event.Error)

	return err
}

// Subscribe creates a new subscription with filters
func (eb *EventBus) Subscribe(filters SubscriptionFilters) (*Subscriber, error) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if !eb.running {
		return nil, fmt.Errorf("event bus not running")
	}

	subID := fmt.Sprintf("sub_%d", time.Now().UnixNano())
	sub := &Subscriber{
		ID:         subID,
		Channel:    make(chan *Event, eb.config.SubscriberBuffer),
		Filters:    filters,
		BufferSize: eb.config.SubscriberBuffer,
		Active:     true,
	}

	eb.subscribers[subID] = sub
	return sub, nil
}

// Unsubscribe removes a subscription
func (eb *EventBus) Unsubscribe(subID string) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	sub, exists := eb.subscribers[subID]
	if !exists {
		return fmt.Errorf("subscriber not found: %s", subID)
	}

	sub.mu.Lock()
	sub.Active = false
	close(sub.Channel)
	sub.mu.Unlock()

	delete(eb.subscribers, subID)
	return nil
}

// GetRecentEvents returns recent events from the ring buffer
// Uses efficient iteration to avoid allocating the entire buffer
func (eb *EventBus) GetRecentEvents(count int, filters SubscriptionFilters) []*Event {
	filtered := make([]*Event, 0, count) // Pre-allocate with expected capacity

	// Iterate in reverse (newest first) and stop when we have enough
	eb.ringBuffer.ForEachReverse(func(event *Event) bool {
		if len(filtered) >= count {
			return false // Stop iteration
		}
		if eb.matchesFilters(event, filters) {
			filtered = append(filtered, event)
		}
		return true // Continue iteration
	})

	// Reverse to get chronological order (oldest first)
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	return filtered
}

// matchesFilters checks if an event matches subscription filters
func (eb *EventBus) matchesFilters(event *Event, filters SubscriptionFilters) bool {
	// Check category filter
	if len(filters.Categories) > 0 {
		found := false
		for _, cat := range filters.Categories {
			if event.Category == cat {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check type filter
	if len(filters.Types) > 0 {
		found := false
		for _, typ := range filters.Types {
			if event.Type == typ {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check level filter
	if len(filters.Levels) > 0 {
		found := false
		for _, lvl := range filters.Levels {
			if event.Level == lvl {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check thread ID filter
	if filters.ThreadID != "" && event.ThreadID != filters.ThreadID {
		return false
	}

	// Check session ID filter
	if filters.SessionID != "" && event.SessionID != filters.SessionID {
		return false
	}

	return true
}

// cleanupLoop periodically removes old events
func (eb *EventBus) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			eb.ringBuffer.RemoveOlderThan(eb.config.RetentionPeriod)
		case <-eb.stopChan:
			return
		}
	}
}

// GetStats returns statistics about the event bus
func (eb *EventBus) GetStats() map[string]interface{} {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	return map[string]interface{}{
		"running":          eb.running,
		"subscriber_count": len(eb.subscribers),
		"buffer_size":      eb.ringBuffer.Size(),
		"buffer_capacity":  eb.ringBuffer.Capacity(),
		"config":           eb.config,
	}
}

// SubscriberCount returns the number of active subscribers
func (eb *EventBus) SubscriberCount() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.subscribers)
}

// IsRunning returns whether the event bus is running
func (eb *EventBus) IsRunning() bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return eb.running
}

// EventCallback is a function that handles events
type EventCallback func(event *Event)

// FuncSubscriber wraps a callback function as a subscriber
type FuncSubscriber struct {
	ID       string
	Filters  SubscriptionFilters
	Callback EventCallback
	Active   bool
	mu       sync.RWMutex
}

// SubscribeFunc creates a callback-based subscription (non-blocking, fire-and-forget)
// Returns the subscriber ID for later unsubscription
func (eb *EventBus) SubscribeFunc(filters SubscriptionFilters, callback EventCallback) (string, error) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if !eb.running {
		return "", fmt.Errorf("event bus not running")
	}

	subID := fmt.Sprintf("func_%d", time.Now().UnixNano())
	sub := &FuncSubscriber{
		ID:       subID,
		Filters:  filters,
		Callback: callback,
		Active:   true,
	}

	// Store in a separate map for func subscribers
	if eb.funcSubscribers == nil {
		eb.funcSubscribers = make(map[string]*FuncSubscriber)
	}
	eb.funcSubscribers[subID] = sub

	return subID, nil
}

// UnsubscribeFunc removes a callback-based subscription
func (eb *EventBus) UnsubscribeFunc(subID string) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.funcSubscribers == nil {
		return fmt.Errorf("subscriber not found: %s", subID)
	}

	sub, exists := eb.funcSubscribers[subID]
	if !exists {
		return fmt.Errorf("subscriber not found: %s", subID)
	}

	sub.mu.Lock()
	sub.Active = false
	sub.mu.Unlock()

	delete(eb.funcSubscribers, subID)
	return nil
}