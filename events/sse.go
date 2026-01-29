package events

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SSEHandler handles Server-Sent Events endpoint
func SSEHandler(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Parse query parameters for filtering
	filters := parseFilters(r)

	// Get tail parameter (number of historical events to send first)
	tailStr := r.URL.Query().Get("tail")
	tail := 0
	if tailStr != "" {
		if t, err := strconv.Atoi(tailStr); err == nil && t > 0 {
			tail = t
		}
	}

	// Get follow parameter (whether to stream new events)
	followStr := r.URL.Query().Get("follow")
	follow := followStr == "" || followStr == "true"

	// Get last event ID for resuming streams
	lastEventID := r.URL.Query().Get("last_event_id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	bus := GetEventBus()

	// Send historical events if requested
	if tail > 0 {
		var historicalEvents []*Event
		if lastEventID != "" {
			// Get events since last event ID
			historicalEvents = bus.ringBuffer.GetSince(lastEventID)
		} else {
			// Get last N events
			historicalEvents = bus.GetRecentEvents(tail, filters)
		}

		for _, event := range historicalEvents {
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}

	// If not following, we're done
	if !follow {
		return
	}

	// Subscribe to new events
	sub, err := bus.Subscribe(filters)
	if err != nil {
		http.Error(w, "Failed to subscribe", http.StatusInternalServerError)
		return
	}
	defer bus.Unsubscribe(sub.ID)

	// Send initial connection event
	connEvent := NewEvent(CategorySystem, "sse_connected", LevelInfo).
		WithData("subscriber_id", sub.ID).
		WithData("filters", filters)

	if err := writeSSEEvent(w, connEvent); err != nil {
		return
	}
	flusher.Flush()

	// Create heartbeat ticker
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	// Stream events
	for {
		select {
		case event, ok := <-sub.Channel:
			if !ok {
				// Channel closed
				return
			}

			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()

		case <-heartbeat.C:
			// Send heartbeat to keep connection alive
			heartbeatEvent := NewEvent(CategorySystem, "heartbeat", LevelDebug).
				WithData("timestamp", time.Now())

			if err := writeSSEEvent(w, heartbeatEvent); err != nil {
				return
			}
			flusher.Flush()

		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}

// parseFilters extracts subscription filters from request query parameters
func parseFilters(r *http.Request) SubscriptionFilters {
	query := r.URL.Query()
	filters := SubscriptionFilters{}

	// Parse categories
	if cats := query.Get("category"); cats != "" {
		catList := strings.Split(cats, ",")
		for _, cat := range catList {
			filters.Categories = append(filters.Categories, EventCategory(strings.TrimSpace(cat)))
		}
	}

	// Parse types
	if types := query.Get("type"); types != "" {
		typeList := strings.Split(types, ",")
		for _, typ := range typeList {
			filters.Types = append(filters.Types, EventType(strings.TrimSpace(typ)))
		}
	}

	// Parse levels
	if levels := query.Get("level"); levels != "" {
		levelList := strings.Split(levels, ",")
		for _, lvl := range levelList {
			filters.Levels = append(filters.Levels, EventLevel(strings.TrimSpace(lvl)))
		}
	}

	// Parse thread ID
	filters.ThreadID = query.Get("thread_id")

	// Parse session ID
	filters.SessionID = query.Get("session_id")

	return filters
}

// writeSSEEvent writes an event in SSE format
func writeSSEEvent(w http.ResponseWriter, event *Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	// Write event ID for resuming
	if _, err := fmt.Fprintf(w, "id: %s\n", event.ID); err != nil {
		return err
	}

	// Write event type for client-side routing
	if _, err := fmt.Fprintf(w, "event: %s\n", event.Type); err != nil {
		return err
	}

	// Write event data
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}

	return nil
}

// EventStatsHandler returns event bus statistics
func EventStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bus := GetEventBus()
	stats := bus.GetStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}