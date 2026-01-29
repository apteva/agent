-- Migration: Create tasks table for task management system
-- Version: 002
-- Date: 2024-12-20

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    thread_id TEXT,
    
    -- Core fields
    title TEXT NOT NULL,
    description TEXT,
    type TEXT NOT NULL DEFAULT 'once' CHECK (type IN ('once', 'recurring')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    priority INTEGER DEFAULT 5 CHECK (priority >= 1 AND priority <= 10),
    
    -- Scheduling
    execute_at DATETIME,                     -- When to execute (NULL = immediate)
    executed_at DATETIME,                    -- Last execution time
    
    -- Recurrence (simple)
    recurrence TEXT CHECK (recurrence IN ('daily', 'weekly', 'monthly', NULL)),
    next_run DATETIME,                       -- Next scheduled run for recurring
    
    -- Result (agent decides what to store)
    result TEXT,                             -- JSON - can contain output, error, logs, etc.
    
    -- Metadata
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    
    FOREIGN KEY (thread_id) REFERENCES threads (id) ON DELETE SET NULL
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_execute_at ON tasks(execute_at);
CREATE INDEX IF NOT EXISTS idx_tasks_next_run ON tasks(next_run);
CREATE INDEX IF NOT EXISTS idx_tasks_thread ON tasks(thread_id);
CREATE INDEX IF NOT EXISTS idx_tasks_type ON tasks(type);