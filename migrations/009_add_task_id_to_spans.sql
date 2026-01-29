-- Migration: Add task_id column to spans table
-- Created: 2025-01-05
-- Purpose: Enable linking spans to tasks for usage tracking

-- Add task_id column to spans table
ALTER TABLE spans ADD COLUMN task_id TEXT;

-- Create index for task_id lookups and grouping
CREATE INDEX IF NOT EXISTS idx_spans_task ON spans(task_id);
