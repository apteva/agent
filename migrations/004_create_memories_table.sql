-- Create memories table for long-term agent memory
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    content TEXT NOT NULL,           -- The actual memory content
    summary TEXT,                     -- Brief summary of the memory
    embedding BLOB,                   -- Vector embedding (stored as bytes)
    thread_id TEXT,                   -- Source thread
    message_id TEXT,                  -- Source message
    importance REAL DEFAULT 0.5,     -- Importance score (0-1)
    category TEXT,                    -- Category: fact, preference, instruction, context
    metadata TEXT,                    -- JSON metadata
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_accessed TIMESTAMP,
    access_count INTEGER DEFAULT 0,

    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_memories_thread ON memories(thread_id);
CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category);
CREATE INDEX IF NOT EXISTS idx_memories_importance ON memories(importance DESC);
CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memories_accessed ON memories(last_accessed DESC);