package main

import (
	"database/sql"
	"log"
)

// runMigrations ensures the database schema is up to date
// Called on every startup - migrations are idempotent
func runMigrations(db *sql.DB) {
	migrations := []struct {
		name string
		sql  string
	}{
		// Tasks table migrations
		{"tasks_add_source", "ALTER TABLE tasks ADD COLUMN source TEXT DEFAULT 'user'"},
		{"tasks_add_delegator_id", "ALTER TABLE tasks ADD COLUMN delegator_id TEXT"},

		// Thread activity summary
		{"threads_add_activity", "ALTER TABLE threads ADD COLUMN activity TEXT"},

		// A2A protocol support for tasks
		{"tasks_add_context_id", "ALTER TABLE tasks ADD COLUMN context_id TEXT"},
		{"tasks_add_a2a_task_id", "ALTER TABLE tasks ADD COLUMN a2a_task_id TEXT"},
		{"tasks_add_artifacts", "ALTER TABLE tasks ADD COLUMN artifacts TEXT"},
		{"tasks_add_history", "ALTER TABLE tasks ADD COLUMN history TEXT"},
		{"tasks_add_push_config", "ALTER TABLE tasks ADD COLUMN push_config TEXT"},

		// Thread type and parent hierarchy
		{"threads_add_type", "ALTER TABLE threads ADD COLUMN type TEXT DEFAULT 'chat'"},
		{"threads_add_parent_id", "ALTER TABLE threads ADD COLUMN parent_id TEXT"},

		// Backfill existing threads based on ID prefix convention
		{"threads_backfill_type_scheduler", "UPDATE threads SET type = 'scheduler' WHERE id LIKE 'scheduler_%' AND type = 'chat'"},
		{"threads_backfill_type_task", "UPDATE threads SET type = 'task' WHERE id LIKE 'task_%' AND type = 'chat'"},
		{"threads_backfill_type_reflection", "UPDATE threads SET type = 'reflection' WHERE id LIKE 'reflection_%' AND type = 'chat'"},
	}

	for _, m := range migrations {
		_, err := db.Exec(m.sql)
		if err != nil {
			// Column already exists - this is expected and fine
			log.Printf("Migration %s: already applied", m.name)
		} else {
			log.Printf("Migration %s: applied successfully", m.name)
		}
	}
}
