-- Migration: Add delegation support to tasks table
-- Version: 008
-- Date: 2025-01-05

-- Add source column to track where task originated
ALTER TABLE tasks ADD COLUMN source TEXT DEFAULT 'local' CHECK (source IN ('local', 'delegated'));

-- Add delegator_id to track which agent delegated the task
ALTER TABLE tasks ADD COLUMN delegator_id TEXT;

-- Index for querying delegated tasks
CREATE INDEX IF NOT EXISTS idx_tasks_source ON tasks(source);
CREATE INDEX IF NOT EXISTS idx_tasks_delegator ON tasks(delegator_id);
