package reflection

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
)

type ReflectionScheduler struct {
	running           bool
	config            *config.ReflectionConfig
	db                *sql.DB
	eventBus          *events.EventBus
	ticker            *time.Ticker
	stopChan          chan bool
	reflecting        bool // prevent concurrent reflections
	conversationCount int  // conversations since last reflection
	lastReflection    time.Time
	startTime         time.Time
	mu                sync.RWMutex
}

type ReflectionStatus struct {
	Enabled           bool                    `json:"enabled"`
	Running           bool                    `json:"running"`
	LastReflection    *time.Time              `json:"last_reflection,omitempty"`
	ConversationCount int                     `json:"conversation_count"`
	Config            *config.ReflectionConfig `json:"config"`
}

var globalReflection *ReflectionScheduler
var reflectionOnce sync.Once

// GetReflectionScheduler returns the global reflection scheduler instance
func GetReflectionScheduler(cfg *config.ReflectionConfig) *ReflectionScheduler {
	reflectionOnce.Do(func() {
		globalReflection = &ReflectionScheduler{
			stopChan: make(chan bool, 1),
			config:   cfg,
			eventBus: events.GetEventBus(),
		}
	})
	return globalReflection
}

// GetGlobalReflection returns the global reflection scheduler (nil if not initialized)
func GetGlobalReflection() *ReflectionScheduler {
	return globalReflection
}

// SetDatabase sets the database connection
func (r *ReflectionScheduler) SetDatabase(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
}

// Start begins the reflection scheduler
func (r *ReflectionScheduler) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return nil
	}

	if !r.config.Enabled {
		log.Println("Reflection is disabled in configuration")
		return nil
	}

	// Parse interval
	interval, err := time.ParseDuration(r.config.Interval)
	if err != nil {
		log.Printf("Invalid reflection interval '%s', using default 24h", r.config.Interval)
		interval = 24 * time.Hour
	}

	r.running = true
	r.startTime = time.Now()
	r.ticker = time.NewTicker(interval)

	// Subscribe to events for triggers
	r.subscribeToEvents()

	go r.tickerLoop()

	// Publish start event
	startEvent := events.NewEvent(events.CategoryReflection, "reflection_started", events.LevelInfo).
		WithData("interval", interval.String()).
		WithData("after_task", r.config.AfterTask).
		WithData("conversation_min", r.config.ConversationMin)
	r.eventBus.Publish(startEvent)

	log.Printf("Reflection scheduler started: interval=%s, after_task=%v, conversation_min=%d",
		interval, r.config.AfterTask, r.config.ConversationMin)
	return nil
}

// Stop gracefully stops the reflection scheduler
func (r *ReflectionScheduler) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return
	}

	r.running = false
	if r.ticker != nil {
		r.ticker.Stop()
	}

	select {
	case r.stopChan <- true:
	default:
	}

	stopEvent := events.NewEvent(events.CategoryReflection, "reflection_stopped", events.LevelInfo).
		WithData("uptime", time.Since(r.startTime).String())
	r.eventBus.Publish(stopEvent)

	log.Println("Reflection scheduler stopped")
}

// UpdateConfig updates the reflection configuration (must be called when stopped)
func (r *ReflectionScheduler) UpdateConfig(cfg *config.ReflectionConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return fmt.Errorf("cannot update config while reflection is running - stop it first")
	}

	r.config = cfg
	log.Printf("Reflection config updated: interval=%s", cfg.Interval)
	return nil
}

// GetStatus returns current reflection scheduler status
func (r *ReflectionScheduler) GetStatus() ReflectionStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	status := ReflectionStatus{
		Enabled:           r.config.Enabled,
		Running:           r.running,
		ConversationCount: r.conversationCount,
		Config:            r.config,
	}

	if !r.lastReflection.IsZero() {
		t := r.lastReflection
		status.LastReflection = &t
	}

	return status
}

// IsRunning returns whether the reflection scheduler is running
func (r *ReflectionScheduler) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// subscribeToEvents subscribes to relevant events for triggering reflections
func (r *ReflectionScheduler) subscribeToEvents() {
	// Subscribe to task completion events (for after_task trigger)
	r.eventBus.SubscribeFunc(events.SubscriptionFilters{
		Categories: []events.EventCategory{events.CategoryTask},
	}, func(event *events.Event) {
		if !r.config.AfterTask {
			return
		}
		if event.Type == "task_completed" {
			log.Println("Reflection: Task completed, triggering post-task reflection")
			go r.performReflection("after_task")
		}
	})

	// Subscribe to chat events (for conversation count trigger)
	r.eventBus.SubscribeFunc(events.SubscriptionFilters{
		Categories: []events.EventCategory{events.CategoryChat},
	}, func(event *events.Event) {
		if r.config.ConversationMin <= 0 {
			return
		}
		if event.Type == "message_received" {
			// Skip reflection-sourced messages
			if source, ok := event.Data["source"].(string); ok && source == "reflection" {
				return
			}
			r.mu.Lock()
			r.conversationCount++
			count := r.conversationCount
			min := r.config.ConversationMin
			r.mu.Unlock()

			if count >= min {
				log.Printf("Reflection: Conversation threshold reached (%d/%d), triggering reflection", count, min)
				go r.performReflection("conversation_threshold")
			}
		}
	})
}

// tickerLoop runs the periodic ticker
func (r *ReflectionScheduler) tickerLoop() {
	for {
		select {
		case <-r.ticker.C:
			log.Println("Reflection: Periodic interval tick")
			r.performReflection("periodic")

		case <-r.stopChan:
			return
		}
	}
}

// TriggerReflection manually triggers a reflection (for API endpoint)
func (r *ReflectionScheduler) TriggerReflection() error {
	r.mu.RLock()
	running := r.running
	r.mu.RUnlock()

	if !running {
		return fmt.Errorf("reflection scheduler is not running")
	}

	go r.performReflection("manual")
	return nil
}

// performReflection gathers context and calls the agent for a reflection session
func (r *ReflectionScheduler) performReflection(triggerType string) {
	// Prevent concurrent reflections
	r.mu.Lock()
	if r.reflecting {
		r.mu.Unlock()
		log.Println("Reflection: Already reflecting, skipping")
		return
	}
	r.reflecting = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.reflecting = false
		r.lastReflection = time.Now()
		r.conversationCount = 0
		r.mu.Unlock()
		log.Println("Reflection: Session completed")
	}()

	// Publish start event
	startEvent := events.NewEvent(events.CategoryReflection, "reflection_session_started", events.LevelInfo).
		WithData("trigger_type", triggerType)
	r.eventBus.Publish(startEvent)

	// Create thread
	threadID := fmt.Sprintf("reflection_%d", time.Now().Unix())
	threadTitle := fmt.Sprintf("Self-Reflection (%s)", triggerType)

	if r.db != nil {
		_, err := r.db.Exec(
			"INSERT INTO threads (id, title, type, created_at, updated_at, metadata) VALUES (?, ?, 'reflection', ?, ?, ?)",
			threadID, threadTitle, time.Now(), time.Now(), "{}",
		)
		if err != nil {
			log.Printf("Reflection: Failed to create thread %s: %v", threadID, err)
		}
	}

	// Gather context
	context := r.gatherContext()
	log.Printf("🪞 Reflection: Gathered context (%d bytes)", len(context))

	// Build the full prompt
	prompt := r.config.Prompt
	if context != "" {
		prompt += "\n\n## Recent Activity Context\n" + context
	}
	log.Printf("🪞 Reflection: Full prompt (%d bytes), source=reflection (config_set tool will be injected)", len(prompt))

	// Call the agent
	chatRequest := map[string]interface{}{
		"message":   prompt,
		"stream":    false,
		"thread_id": threadID,
		"source":    "reflection",
	}

	requestBody, err := json.Marshal(chatRequest)
	if err != nil {
		log.Printf("Reflection: Failed to marshal request: %v", err)
		return
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "4015"
	}
	chatURL := fmt.Sprintf("http://localhost:%s/chat", port)

	log.Printf("Reflection: Calling agent at %s for thread %s (trigger: %s)", chatURL, threadID, triggerType)

	req, err := http.NewRequest("POST", chatURL, bytes.NewReader(requestBody))
	if err != nil {
		log.Printf("Reflection: Failed to create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey := os.Getenv("AGENT_API_KEY"); apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Reflection: Failed to call agent: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Printf("🪞 Reflection: Agent responded OK for thread %s — check logs for config_set calls", threadID)
	} else {
		log.Printf("🪞 Reflection: Agent call failed with status %d for thread %s", resp.StatusCode, threadID)
	}

	// Publish completion event
	completeEvent := events.NewEvent(events.CategoryReflection, "reflection_session_completed", events.LevelInfo).
		WithData("trigger_type", triggerType).
		WithData("thread_id", threadID).
		WithData("status_code", resp.StatusCode)
	r.eventBus.Publish(completeEvent)
}

// gatherContext builds a context summary from recent activity
func (r *ReflectionScheduler) gatherContext() string {
	if r.db == nil {
		return ""
	}

	lookback := r.config.LookbackHours
	if lookback <= 0 {
		lookback = 24
	}
	cutoff := time.Now().Add(-time.Duration(lookback) * time.Hour)

	var context string

	// 1. Recent threads (excluding reflection/scheduler threads)
	threads := r.queryRecentThreads(cutoff)
	if threads != "" {
		context += "### Recent Conversations\n" + threads + "\n"
	}

	// 2. Recent traces
	traces := r.queryRecentTraces(cutoff)
	if traces != "" {
		context += "### Recent Operations\n" + traces + "\n"
	}

	// 3. Tool usage summary
	toolUsage := r.queryToolUsage(cutoff)
	if toolUsage != "" {
		context += "### Tool Usage Summary\n" + toolUsage + "\n"
	}

	// 4. Recent memories
	memories := r.queryRecentMemories()
	if memories != "" {
		context += "### Recent Memories\n" + memories + "\n"
	}

	return context
}

func (r *ReflectionScheduler) queryRecentThreads(cutoff time.Time) string {
	rows, err := r.db.Query(`
		SELECT id, title, activity, created_at
		FROM threads
		WHERE updated_at > ?
		  AND id NOT LIKE 'reflection_%'
		  AND id NOT LIKE 'scheduler_%'
		ORDER BY updated_at DESC
		LIMIT 20
	`, cutoff)
	if err != nil {
		log.Printf("Reflection: Error querying threads: %v", err)
		return ""
	}
	defer rows.Close()

	var result string
	for rows.Next() {
		var id, title string
		var activity sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&id, &title, &activity, &createdAt); err != nil {
			continue
		}
		activityStr := ""
		if activity.Valid && activity.String != "" {
			activityStr = " — " + activity.String
		}
		result += fmt.Sprintf("- **%s** (%s)%s\n", title, createdAt.Format("Jan 2 15:04"), activityStr)
	}
	return result
}

func (r *ReflectionScheduler) queryRecentTraces(cutoff time.Time) string {
	rows, err := r.db.Query(`
		SELECT source, name, status, duration_ms, start_time
		FROM traces
		WHERE start_time > ?
		  AND parent_trace_id IS NULL
		ORDER BY start_time DESC
		LIMIT 20
	`, cutoff)
	if err != nil {
		log.Printf("Reflection: Error querying traces: %v", err)
		return ""
	}
	defer rows.Close()

	var result string
	for rows.Next() {
		var source, name, status string
		var durationMs sql.NullInt64
		var startTime time.Time
		if err := rows.Scan(&source, &name, &status, &durationMs, &startTime); err != nil {
			continue
		}
		duration := ""
		if durationMs.Valid {
			duration = fmt.Sprintf(" (%dms)", durationMs.Int64)
		}
		result += fmt.Sprintf("- [%s] %s: %s%s\n", source, name, status, duration)
	}
	return result
}

func (r *ReflectionScheduler) queryToolUsage(cutoff time.Time) string {
	rows, err := r.db.Query(`
		SELECT name, COUNT(*) as count,
		       SUM(CASE WHEN status='error' THEN 1 ELSE 0 END) as errors
		FROM spans
		WHERE start_time > ?
		  AND kind = 'tool'
		GROUP BY name
		ORDER BY count DESC
		LIMIT 15
	`, cutoff)
	if err != nil {
		log.Printf("Reflection: Error querying tool usage: %v", err)
		return ""
	}
	defer rows.Close()

	var result string
	for rows.Next() {
		var name string
		var count, errors int
		if err := rows.Scan(&name, &count, &errors); err != nil {
			continue
		}
		errorStr := ""
		if errors > 0 {
			errorStr = fmt.Sprintf(" (%d errors)", errors)
		}
		result += fmt.Sprintf("- %s: %d calls%s\n", name, count, errorStr)
	}
	return result
}

func (r *ReflectionScheduler) queryRecentMemories() string {
	rows, err := r.db.Query(`
		SELECT content, category, importance
		FROM memories
		ORDER BY created_at DESC
		LIMIT 10
	`)
	if err != nil {
		log.Printf("Reflection: Error querying memories: %v", err)
		return ""
	}
	defer rows.Close()

	var result string
	for rows.Next() {
		var content, category string
		var importance float64
		if err := rows.Scan(&content, &category, &importance); err != nil {
			continue
		}
		// Truncate long content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		result += fmt.Sprintf("- [%s] %s\n", category, content)
	}
	return result
}
