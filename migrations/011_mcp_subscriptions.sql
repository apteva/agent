-- Migration: Create MCP subscriptions table for webhook event subscriptions
-- Version: 011
-- Date: 2025-01-18

CREATE TABLE IF NOT EXISTS mcp_subscriptions (
    id TEXT PRIMARY KEY,
    server TEXT NOT NULL,                    -- MCP server name (e.g., "stripe", "github")
    events TEXT NOT NULL,                    -- JSON array of event names
    prompt TEXT,                             -- Instructions for handling events
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(server)                           -- One subscription per server
);

-- Index for quick lookups
CREATE INDEX IF NOT EXISTS idx_mcp_subscriptions_server ON mcp_subscriptions(server);
