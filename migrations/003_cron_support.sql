-- Migration: Add cron expression support for task recurrence
-- Version: 003
-- Date: 2025-11-07
-- Description: Remove CHECK constraint to allow cron expressions in recurrence field

-- SQLite migration: Recreate table without recurrence CHECK constraint
-- Reference: https://www.sqlite.org/lang_altertable.html

-- Step 1: Create new table without the recurrence CHECK constraint
CREATE TABLE tasks_new (
    id TEXT PRIMARY KEY,
    thread_id TEXT,

    -- Core fields
    title TEXT NOT NULL,
    description TEXT,
    type TEXT NOT NULL DEFAULT 'once' CHECK (type IN ('once', 'recurring')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    priority INTEGER DEFAULT 5 CHECK (priority >= 1 AND priority <= 10),

    -- Scheduling
    execute_at DATETIME,
    executed_at DATETIME,

    -- Recurrence (NO CHECK CONSTRAINT - allows cron expressions!)
    recurrence TEXT,
    next_run DATETIME,

    -- Result
    result TEXT,

    -- Metadata
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (thread_id) REFERENCES threads (id) ON DELETE SET NULL
);

-- Step 2: Copy data from old table to new table
INSERT INTO tasks_new
SELECT id, thread_id, title, description, type, status, priority,
       execute_at, executed_at, recurrence, next_run, result,
       created_at, updated_at
FROM tasks;

-- Step 3: Drop old table
DROP TABLE tasks;

-- Step 4: Rename new table to original name
ALTER TABLE tasks_new RENAME TO tasks;

-- Step 5: Recreate indexes
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_execute_at ON tasks(execute_at);
CREATE INDEX IF NOT EXISTS idx_tasks_next_run ON tasks(next_run);
CREATE INDEX IF NOT EXISTS idx_tasks_thread ON tasks(thread_id);
CREATE INDEX IF NOT EXISTS idx_tasks_type ON tasks(type);
