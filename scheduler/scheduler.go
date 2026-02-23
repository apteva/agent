package scheduler

import (
	"github.com/apteva/agent/events"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type Scheduler struct {
	running         bool
	startTime       time.Time
	tickCount       int64
	lastTick        time.Time
	mu              sync.RWMutex
	stopChan        chan bool
	config          SchedulerConfig
	db              *sql.DB
	interval        time.Duration // Main tick interval (from config)
	mainTicker      *time.Ticker  // Main interval ticker (e.g., 24h)
	checkTicker     *time.Ticker  // Quick check every 30s
	agentProcessing bool          // Prevents concurrent agent calls
}

type SchedulerStatus struct {
	Running   bool      `json:"running"`
	Uptime    string    `json:"uptime"`
	TickCount int64     `json:"tick_count"`
	LastTick  time.Time `json:"last_tick"`
	StartTime time.Time `json:"start_time"`
}

var globalScheduler *Scheduler
var schedulerOnce sync.Once

// GetScheduler returns the global scheduler instance
func GetScheduler(config SchedulerConfig) *Scheduler {
	schedulerOnce.Do(func() {
		globalScheduler = &Scheduler{
			stopChan: make(chan bool, 1),
			config:   config,
		}
	})
	return globalScheduler
}

// SetDatabase sets the database connection for the scheduler
func (s *Scheduler) SetDatabase(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
}

// UpdateConfig updates the scheduler configuration (must be called when stopped)
func (s *Scheduler) UpdateConfig(config SchedulerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("cannot update config while scheduler is running - stop it first")
	}

	// Parse interval
	interval, err := time.ParseDuration(config.Interval)
	if err != nil {
		return fmt.Errorf("invalid interval '%s': %w", config.Interval, err)
	}

	s.config = config
	s.interval = interval
	log.Printf("Scheduler config updated: interval=%s, max_tasks=%d", interval, config.MaxTasks)
	return nil
}

// Start begins the scheduler ticker
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil // Already running
	}

	if !s.config.Enabled {
		log.Println("Scheduler is disabled in configuration")
		return nil
	}

	// Parse interval from config
	interval, err := time.ParseDuration(s.config.Interval)
	if err != nil {
		log.Printf("Invalid scheduler interval '%s', using default 24h", s.config.Interval)
		interval = 24 * time.Hour
	}
	s.interval = interval

	s.running = true
	s.startTime = time.Now()
	s.tickCount = 0
	s.mainTicker = time.NewTicker(interval)      // Main interval (e.g., 24h)
	s.checkTicker = time.NewTicker(30 * time.Second) // Quick check every 30s

	go s.tickerLoop()

	// Publish scheduler start event
	eventBus := events.GetEventBus()
	startEvent := events.NewEvent(events.CategoryScheduler, "scheduler_started", events.LevelInfo).
		WithData("main_interval", interval.String()).
		WithData("check_interval", "30s").
		WithData("max_tasks", s.config.MaxTasks)
	eventBus.Publish(startEvent)

	log.Printf("Scheduler started: main interval=%s, check interval=30s", interval)
	return nil
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.running = false
	if s.mainTicker != nil {
		s.mainTicker.Stop()
	}
	if s.checkTicker != nil {
		s.checkTicker.Stop()
	}

	select {
	case s.stopChan <- true:
	default:
	}

	// Publish scheduler stop event
	eventBus := events.GetEventBus()
	stopEvent := events.NewEvent(events.CategoryScheduler, "scheduler_stopped", events.LevelInfo).
		WithData("uptime", time.Since(s.startTime).String()).
		WithData("total_ticks", s.tickCount)
	eventBus.Publish(stopEvent)

	log.Println("Scheduler stopped")
}

// GetStatus returns current scheduler status
func (s *Scheduler) GetStatus() SchedulerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var uptime string

	if s.running {
		uptime = time.Since(s.startTime).String()
	}

	return SchedulerStatus{
		Running:   s.running,
		Uptime:    uptime,
		TickCount: s.tickCount,
		LastTick:  s.lastTick,
		StartTime: s.startTime,
	}
}

// IsRunning returns whether the scheduler is currently running
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// tickerLoop runs the main ticker loop in a goroutine
func (s *Scheduler) tickerLoop() {
	for {
		select {
		case <-s.mainTicker.C:
			// Main interval tick (e.g., 24h) - only if there are pending tasks
			if s.hasPendingTasks() {
				log.Println("Scheduler: Main interval tick - pending tasks found")
				s.callAgent()
			} else {
				log.Println("Scheduler: Main interval tick - no pending tasks, skipping")
			}

		case <-s.checkTicker.C:
			// Quick check every 30s - only if there are pending tasks
			if s.hasPendingTasks() {
				s.callAgent()
			}

		case <-s.stopChan:
			return
		}
	}
}

// onTick is called on each scheduler tick (used by TriggerTick API)
func (s *Scheduler) onTick() {
	s.mu.Lock()
	s.tickCount++
	s.lastTick = time.Now()
	currentTick := s.tickCount
	s.mu.Unlock()

	// Publish tick event every 100 ticks for monitoring
	if currentTick%100 == 0 {
		eventBus := events.GetEventBus()
		tickEvent := events.NewEvent(events.CategoryScheduler, "scheduler_tick", events.LevelDebug).
			WithData("tick_count", currentTick).
			WithData("uptime", time.Since(s.startTime).String())
		eventBus.Publish(tickEvent)
	}

	// Call the agent to handle scheduled tasks
	s.callAgent()
}

// hasPendingTasks checks if there are any pending tasks that need execution
func (s *Scheduler) hasPendingTasks() bool {
	if s.db == nil {
		return false
	}

	query := `
		SELECT COUNT(*)
		FROM tasks
		WHERE status = 'pending'
		  AND (execute_at IS NOT NULL OR next_run IS NOT NULL)
		  AND (
		    (execute_at IS NOT NULL AND datetime(SUBSTR(execute_at, 1, 19)) <= datetime('now', 'localtime'))
		    OR (next_run IS NOT NULL AND datetime(SUBSTR(next_run, 1, 19)) <= datetime('now', 'localtime'))
		  )
		  AND (executed_at IS NULL OR datetime(executed_at) < datetime('now', 'localtime', '-10 seconds'))
	`

	var count int
	err := s.db.QueryRow(query).Scan(&count)
	if err != nil {
		log.Printf("Scheduler: Error checking for pending tasks: %v", err)
		return false
	}

	return count > 0
}

// TriggerTick manually triggers a scheduler tick (for API endpoint)
func (s *Scheduler) TriggerTick() error {
	s.mu.RLock()
	running := s.running
	s.mu.RUnlock()

	if !running {
		return fmt.Errorf("scheduler is not running")
	}

	log.Println("Manually triggering scheduler tick")

	// Publish manual tick event
	eventBus := events.GetEventBus()
	manualEvent := events.NewEvent(events.CategoryScheduler, "scheduler_manual_tick", events.LevelInfo)
	eventBus.Publish(manualEvent)

	// Call onTick directly
	s.onTick()
	return nil
}

// Job represents a scheduled task
type Job struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`
	ExecuteAt   *string `json:"execute_at,omitempty"`
	NextRun     *string `json:"next_run,omitempty"`
	Recurrence  *string `json:"recurrence,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

// GetJobs returns all scheduled tasks from the database
func (s *Scheduler) GetJobs() ([]Job, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	query := `
		SELECT id, title, description, status, execute_at, next_run, recurrence, created_at
		FROM tasks
		WHERE status IN ('pending', 'running')
		   OR (recurrence IS NOT NULL AND recurrence != '')
		ORDER BY COALESCE(execute_at, next_run, created_at) ASC
		LIMIT 100
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var job Job
		err := rows.Scan(&job.ID, &job.Title, &job.Description, &job.Status, &job.ExecuteAt, &job.NextRun, &job.Recurrence, &job.CreatedAt)
		if err != nil {
			log.Printf("Scheduler: Error scanning job row: %v", err)
			continue
		}
		jobs = append(jobs, job)
	}

	if jobs == nil {
		jobs = []Job{}
	}

	return jobs, nil
}

// callAgent calls the agent to check and execute scheduled tasks
func (s *Scheduler) callAgent() {
	// Only call agent if scheduler is configured to do so
	if !s.config.Enabled {
		return
	}

	// Check if agent is already processing
	s.mu.Lock()
	if s.agentProcessing {
		s.mu.Unlock()
		log.Println("Scheduler: Agent already processing tasks, skipping duplicate call")
		return
	}
	s.agentProcessing = true
	s.mu.Unlock()

	// Create a special scheduler thread
	threadID := fmt.Sprintf("scheduler_%d", time.Now().Unix())
	threadTitle := "Scheduler: Periodic Task Check"

	// Create the thread in the database if we have a database connection
	if s.db != nil {
		_, err := s.db.Exec(
			"INSERT INTO threads (id, title, type, created_at, updated_at, metadata) VALUES (?, ?, 'scheduler', ?, ?, ?)",
			threadID, threadTitle, time.Now(), time.Now(), "{}",
		)
		if err != nil {
			log.Printf("Scheduler: Failed to create thread %s: %v", threadID, err)
			// Continue anyway - the chat endpoint will create it if needed
		} else {
			log.Printf("Scheduler: Created thread %s in database", threadID)
		}
	}

	// Call the agent with a scheduler prompt
	prompt := `You are running autonomously as the task scheduler. There is no user present.

INSTRUCTIONS:
1. Check for pending tasks using list_tasks with status="pending"
2. For each pending task where execute_at or next_run is in the past or now, execute it using execute_task with sync=true so you wait for the task to complete before proceeding
3. Do NOT poll with get_task — sync=true already waits for completion
4. After a recurring task completes successfully, schedule its next occurrence:
   - Use create_task with the same title, description, and recurrence pattern
   - The system will automatically calculate the next_run based on the recurrence
5. Do NOT ask questions - make your best judgment and proceed
6. Provide a brief summary of what was done

Begin now.`

	// Prepare the chat request
	chatRequest := map[string]interface{}{
		"message":   prompt,
		"stream":    false,
		"thread_id": threadID,
		"source":    "task",
	}

	// Make HTTP call to chat endpoint
	go func() {
		defer func() {
			// Clear processing flag when done
			s.mu.Lock()
			s.agentProcessing = false
			s.mu.Unlock()
			log.Println("Scheduler: Agent processing completed")
		}()

		log.Printf("Scheduler calling agent with thread %s", threadID)

		requestBody, err := json.Marshal(chatRequest)
		if err != nil {
			log.Printf("Scheduler: Failed to marshal chat request: %v", err)
			return
		}

		// Get the port from environment variable (same port the server is running on)
		port := os.Getenv("PORT")
		if port == "" {
			port = "4015"
		}
		chatURL := fmt.Sprintf("http://localhost:%s/chat", port)

		log.Printf("Scheduler: Calling agent at %s for thread %s", chatURL, threadID)
		req, err := http.NewRequest("POST", chatURL, bytes.NewReader(requestBody))
		if err != nil {
			log.Printf("Scheduler: Failed to create request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		// Add API key for internal auth
		if apiKey := os.Getenv("AGENT_API_KEY"); apiKey != "" {
			req.Header.Set("X-API-Key", apiKey)
		}

		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Scheduler: Failed to call agent: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			log.Printf("Scheduler: Agent called successfully for thread %s", threadID)
		} else {
			log.Printf("Scheduler: Agent call failed with status %d for thread %s", resp.StatusCode, threadID)
		}

		// Publish scheduler agent call event
		eventBus := events.GetEventBus()
		agentEvent := events.NewEvent(events.CategoryScheduler, "scheduler_agent_called", events.LevelInfo).
			WithData("thread_id", threadID).
			WithData("status_code", resp.StatusCode)
		eventBus.Publish(agentEvent)
	}()
}

