-- Migration: Create files table for agent file system
-- Version: 006
-- Date: 2025-11-04
-- Description: Stores file metadata and content to avoid passing large base64 strings in message history

CREATE TABLE IF NOT EXISTS files (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    thread_id TEXT,
    message_id TEXT,

    -- File properties
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    file_type TEXT NOT NULL,  -- 'image', 'document', 'audio', 'video', etc.
    size_bytes INTEGER NOT NULL,
    checksum TEXT,  -- SHA-256 hash for deduplication

    -- Storage (database only for now)
    data BLOB NOT NULL,  -- Actual file content

    -- Media metadata (for images/videos)
    width INTEGER,
    height INTEGER,
    duration INTEGER,  -- For audio/video (seconds)
    metadata TEXT DEFAULT '{}',  -- JSON metadata

    -- Source tracking
    source TEXT DEFAULT 'unknown',  -- 'user_upload', 'mcp_tool', 'assistant_generated', 'unknown'
    source_tool TEXT,  -- MCP tool name if applicable

    -- Lifecycle
    access_count INTEGER DEFAULT 0,
    last_accessed TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,  -- Auto-cleanup based on retention policy

    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE SET NULL
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_files_thread ON files(thread_id);
CREATE INDEX IF NOT EXISTS idx_files_message ON files(message_id);
CREATE INDEX IF NOT EXISTS idx_files_checksum ON files(checksum);
CREATE INDEX IF NOT EXISTS idx_files_created ON files(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_files_expires ON files(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_files_type ON files(file_type);
CREATE INDEX IF NOT EXISTS idx_files_source ON files(source);
