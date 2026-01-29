-- Add source and delegator_id columns to tasks table
-- These support task delegation between agents

ALTER TABLE tasks ADD COLUMN source TEXT DEFAULT 'user';
ALTER TABLE tasks ADD COLUMN delegator_id TEXT;
