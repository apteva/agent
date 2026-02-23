package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// SubThreadResult represents a completed background task result
type SubThreadResult struct {
	ThreadID    string    `json:"thread_id"`
	TaskID      string    `json:"task_id"`
	Title       string    `json:"title"`
	Status      string    `json:"status"` // "completed" | "failed"
	Response    string    `json:"response,omitempty"`
	Error       string    `json:"error,omitempty"`
	Priority    int       `json:"priority"` // 0=normal, 1=high (for future interrupt support)
	CompletedAt time.Time `json:"completed_at"`
}

// SubThreadResultCollector accumulates results from background tasks for a parent thread
type SubThreadResultCollector struct {
	parentThreadID string
	results        []SubThreadResult
	mu             sync.Mutex
	lastActivity   time.Time
}

// Global registry of collectors keyed by parent thread ID
var (
	collectors   = make(map[string]*SubThreadResultCollector)
	collectorsMu sync.RWMutex
)

// Active thread tracking — which threads are currently in a conversation loop
var activeThreads sync.Map // threadID → struct{}

// init starts the auto-expire goroutine
func init() {
	go collectorCleanupLoop()
}

// GetOrCreateCollector returns the collector for a parent thread, creating if needed
func GetOrCreateCollector(parentThreadID string) *SubThreadResultCollector {
	collectorsMu.Lock()
	defer collectorsMu.Unlock()

	if c, ok := collectors[parentThreadID]; ok {
		c.lastActivity = time.Now()
		return c
	}

	c := &SubThreadResultCollector{
		parentThreadID: parentThreadID,
		lastActivity:   time.Now(),
	}
	collectors[parentThreadID] = c
	return c
}

// GetCollector returns the collector for a parent thread, or nil if none exists
func GetCollector(parentThreadID string) *SubThreadResultCollector {
	collectorsMu.RLock()
	defer collectorsMu.RUnlock()

	if c, ok := collectors[parentThreadID]; ok {
		return c
	}
	return nil
}

// CleanupCollector removes the collector for a parent thread
func CleanupCollector(parentThreadID string) {
	collectorsMu.Lock()
	defer collectorsMu.Unlock()
	delete(collectors, parentThreadID)
}

// Add appends a result to the collector (thread-safe)
func (c *SubThreadResultCollector) Add(result SubThreadResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results = append(c.results, result)
	c.lastActivity = time.Now()
	log.Printf("📬 Collector[%s]: added result for task %s (%s)", c.parentThreadID, result.TaskID, result.Status)
}

// Drain atomically returns all pending results and clears the buffer
func (c *SubThreadResultCollector) Drain() []SubThreadResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.results) == 0 {
		return nil
	}

	results := c.results
	c.results = nil
	c.lastActivity = time.Now()
	log.Printf("📬 Collector[%s]: drained %d results", c.parentThreadID, len(results))
	return results
}

// Pending returns the number of pending results without draining
func (c *SubThreadResultCollector) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.results)
}

// MarkThreadActive marks a thread as currently in a conversation loop
func MarkThreadActive(threadID string) {
	activeThreads.Store(threadID, struct{}{})
}

// MarkThreadIdle marks a thread as no longer in a conversation loop
func MarkThreadIdle(threadID string) {
	activeThreads.Delete(threadID)
}

// IsThreadActive returns true if the thread is currently in a conversation loop
func IsThreadActive(threadID string) bool {
	_, ok := activeThreads.Load(threadID)
	return ok
}

// FormatPendingResults formats background task results for injection into the LLM conversation
func FormatPendingResults(results []SubThreadResult) string {
	var b strings.Builder
	b.WriteString("[Background Task Results]\n")
	b.WriteString("The following tasks you started in the background have completed:\n\n")

	for i, r := range results {
		if r.Status == "completed" {
			response := r.Response
			if len(response) > 500 {
				response = response[:500] + "... (truncated)"
			}
			b.WriteString(fmt.Sprintf("%d. \"%s\" (task %s): COMPLETED", i+1, r.Title, r.TaskID))
			if response != "" {
				b.WriteString(fmt.Sprintf(" — %s", response))
			}
			b.WriteString("\n")
		} else {
			b.WriteString(fmt.Sprintf("%d. \"%s\" (task %s): FAILED — %s\n", i+1, r.Title, r.TaskID, r.Error))
		}
	}

	b.WriteString("\nAcknowledge these results and decide on next steps.")
	return b.String()
}

// PostChatAsync fires a /chat request to the agent's own endpoint (fire-and-forget)
// Used to deliver background task results to an idle parent thread
func PostChatAsync(threadID, message, source string) {
	go func() {
		port := os.Getenv("PORT")
		if port == "" {
			port = "4015"
		}
		chatURL := fmt.Sprintf("http://localhost:%s/chat", port)

		chatReq := map[string]interface{}{
			"message":   message,
			"thread_id": threadID,
			"stream":    true,
			"source":    source,
		}

		body, err := json.Marshal(chatReq)
		if err != nil {
			log.Printf("📬 PostChatAsync: failed to marshal request: %v", err)
			return
		}

		req, err := http.NewRequest("POST", chatURL, bytes.NewReader(body))
		if err != nil {
			log.Printf("📬 PostChatAsync: failed to create request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey := os.Getenv("AGENT_API_KEY"); apiKey != "" {
			req.Header.Set("X-API-Key", apiKey)
		}

		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("📬 PostChatAsync: failed to call /chat: %v", err)
			return
		}
		resp.Body.Close()
		log.Printf("📬 PostChatAsync: delivered result to thread %s (status %d)", threadID, resp.StatusCode)
	}()
}

// collectorCleanupLoop runs in the background and expires inactive collectors
func collectorCleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		collectorsMu.Lock()
		now := time.Now()
		for id, c := range collectors {
			c.mu.Lock()
			if now.Sub(c.lastActivity) > 1*time.Hour {
				log.Printf("📬 Collector[%s]: expired (inactive for >1h)", id)
				delete(collectors, id)
			}
			c.mu.Unlock()
		}
		collectorsMu.Unlock()
	}
}
