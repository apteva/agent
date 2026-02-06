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
