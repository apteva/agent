-- Add source tracking to memories table for RAG support
-- Allows memories to come from different sources: chat, file, manual

-- Add source tracking columns
ALTER TABLE memories ADD COLUMN source TEXT DEFAULT 'chat';
ALTER TABLE memories ADD COLUMN source_id TEXT;
ALTER TABLE memories ADD COLUMN source_name TEXT;

-- Add chunking columns for document memories
ALTER TABLE memories ADD COLUMN parent_id TEXT;
ALTER TABLE memories ADD COLUMN chunk_index INTEGER DEFAULT 0;
ALTER TABLE memories ADD COLUMN total_chunks INTEGER DEFAULT 1;

-- Indexes for efficient querying by source
CREATE INDEX IF NOT EXISTS idx_memories_source ON memories(source);
CREATE INDEX IF NOT EXISTS idx_memories_source_id ON memories(source_id);
CREATE INDEX IF NOT EXISTS idx_memories_parent_id ON memories(parent_id);

-- Update existing memories to have source = 'chat'
UPDATE memories SET source = 'chat' WHERE source IS NULL;
