package events

import (
	"sync"
	"time"
)

// RingBuffer is a circular buffer for storing events
type RingBuffer struct {
	buffer   []*Event
	capacity int
	head     int
	tail     int
	size     int
	mu       sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with the specified capacity
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		buffer:   make([]*Event, capacity),
		capacity: capacity,
		head:     0,
		tail:     0,
		size:     0,
	}
}

// Add adds an event to the buffer
func (rb *RingBuffer) Add(event *Event) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.buffer[rb.tail] = event
	rb.tail = (rb.tail + 1) % rb.capacity

	if rb.size < rb.capacity {
		rb.size++
	} else {
		// Buffer is full, move head forward
		rb.head = (rb.head + 1) % rb.capacity
	}
}

// GetAll returns all events in the buffer in chronological order
func (rb *RingBuffer) GetAll() []*Event {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		return []*Event{}
	}

	result := make([]*Event, rb.size)
	for i := 0; i < rb.size; i++ {
		idx := (rb.head + i) % rb.capacity
		result[i] = rb.buffer[idx]
	}

	return result
}

// GetLast returns the last N events from the buffer
func (rb *RingBuffer) GetLast(n int) []*Event {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.size {
		n = rb.size
	}

	if n <= 0 {
		return []*Event{}
	}

	result := make([]*Event, n)
	startIdx := (rb.tail - n + rb.capacity) % rb.capacity

	for i := 0; i < n; i++ {
		idx := (startIdx + i) % rb.capacity
		result[i] = rb.buffer[idx]
	}

	return result
}

// RemoveOlderThan removes events older than the specified duration
func (rb *RingBuffer) RemoveOlderThan(duration time.Duration) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.size == 0 {
		return 0
	}

	cutoff := time.Now().Add(-duration)
	removed := 0

	for rb.size > 0 {
		headEvent := rb.buffer[rb.head]
		if headEvent == nil || headEvent.Timestamp.After(cutoff) {
			break
		}

		rb.buffer[rb.head] = nil
		rb.head = (rb.head + 1) % rb.capacity
		rb.size--
		removed++
	}

	return removed
}

// Size returns the current number of events in the buffer
func (rb *RingBuffer) Size() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size
}

// Capacity returns the maximum capacity of the buffer
func (rb *RingBuffer) Capacity() int {
	return rb.capacity
}

// Clear removes all events from the buffer
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for i := 0; i < rb.capacity; i++ {
		rb.buffer[i] = nil
	}

	rb.head = 0
	rb.tail = 0
	rb.size = 0
}

// GetSince returns all events since the specified event ID
func (rb *RingBuffer) GetSince(eventID string) []*Event {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		return []*Event{}
	}

	// Find the event with the specified ID
	foundIdx := -1
	for i := 0; i < rb.size; i++ {
		idx := (rb.head + i) % rb.capacity
		if rb.buffer[idx] != nil && rb.buffer[idx].ID == eventID {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		// Event not found, return all events
		return rb.GetAll()
	}

	// Return events after the found event
	remaining := rb.size - foundIdx - 1
	if remaining <= 0 {
		return []*Event{}
	}

	result := make([]*Event, remaining)
	for i := 0; i < remaining; i++ {
		idx := (rb.head + foundIdx + 1 + i) % rb.capacity
		result[i] = rb.buffer[idx]
	}

	return result
}

// ForEach iterates over all events in chronological order
// The callback function receives each event and should return true to continue, false to stop
func (rb *RingBuffer) ForEach(fn func(*Event) bool) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	for i := 0; i < rb.size; i++ {
		idx := (rb.head + i) % rb.capacity
		if rb.buffer[idx] != nil {
			if !fn(rb.buffer[idx]) {
				break
			}
		}
	}
}

// ForEachReverse iterates over all events in reverse chronological order (newest first)
// The callback function receives each event and should return true to continue, false to stop
func (rb *RingBuffer) ForEachReverse(fn func(*Event) bool) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	for i := rb.size - 1; i >= 0; i-- {
		idx := (rb.head + i) % rb.capacity
		if rb.buffer[idx] != nil {
			if !fn(rb.buffer[idx]) {
				break
			}
		}
	}
}