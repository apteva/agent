-- Migration: Add trace hierarchy support (parent_trace_id and source columns)
-- Created: 2026-01-20

-- Add parent_trace_id column for linking child traces to parent traces
ALTER TABLE traces ADD COLUMN parent_trace_id TEXT REFERENCES traces(id) ON DELETE SET NULL;

-- Add source column to track how trace was initiated
ALTER TABLE traces ADD COLUMN source TEXT DEFAULT 'chat';

-- Add index for efficient parent lookups
CREATE INDEX IF NOT EXISTS idx_traces_parent ON traces(parent_trace_id);

-- Add index for source filtering
CREATE INDEX IF NOT EXISTS idx_traces_source ON traces(source);
