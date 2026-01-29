package scheduler

type SchedulerConfig struct {
	Enabled  bool   `json:"enabled"`
	Interval string `json:"interval"`  // "30s", "1m", "5m"
	MaxTasks int    `json:"max_tasks"` // Future: task limit
}

// GetDefaultSchedulerConfig returns default scheduler configuration
func GetDefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		Enabled:  true,
		Interval: "24h",
		MaxTasks: 100,
	}
}