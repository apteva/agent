-- Migration: Add tracing system (traces, spans, events)
-- Created: 2025-11-01

-- Traces table: Top-level request/operation tracking
CREATE TABLE IF NOT EXISTS traces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    thread_id TEXT,
    service_name TEXT DEFAULT 'agent-core',
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    duration_ms INTEGER,
    status TEXT CHECK (status IN ('ok', 'error', 'unfinished')) DEFAULT 'unfinished',
    root_span_id TEXT,
    span_count INTEGER DEFAULT 0,
    error_count INTEGER DEFAULT 0,
    metadata TEXT DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE SET NULL
);

-- Spans table: Individual operations within traces
CREATE TABLE IF NOT EXISTS spans (
    id TEXT PRIMARY KEY,
    trace_id TEXT NOT NULL,
    parent_span_id TEXT,
    name TEXT NOT NULL,
    kind TEXT CHECK (kind IN ('internal', 'server', 'client', 'tool', 'llm', 'agent')) DEFAULT 'internal',

    -- Timing
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    duration_ms INTEGER,

    -- Status
    status TEXT CHECK (status IN ('ok', 'error', 'running')) DEFAULT 'running',
    status_message TEXT,

    -- Context
    category TEXT,
    thread_id TEXT,
    session_id TEXT,
    agent_id TEXT,

    -- Data (JSON)
    attributes TEXT DEFAULT '{}',
    input_data TEXT,
    output_data TEXT,

    -- Metrics
    token_usage_input INTEGER,
    token_usage_output INTEGER,
    cost_usd REAL,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (trace_id) REFERENCES traces(id) ON DELETE CASCADE,
    FOREIGN KEY (parent_span_id) REFERENCES spans(id) ON DELETE CASCADE,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE SET NULL
);

-- Events table: Point-in-time occurrences (persisted from EventBus)
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    trace_id TEXT,
    span_id TEXT,
    timestamp TIMESTAMP NOT NULL,

    category TEXT NOT NULL,
    type TEXT NOT NULL,
    level TEXT NOT NULL,

    thread_id TEXT,
    session_id TEXT,

    data TEXT DEFAULT '{}',
    metadata TEXT DEFAULT '{}',
    error TEXT,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (trace_id) REFERENCES traces(id) ON DELETE CASCADE,
    FOREIGN KEY (span_id) REFERENCES spans(id) ON DELETE CASCADE,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE SET NULL
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_traces_thread ON traces(thread_id);
CREATE INDEX IF NOT EXISTS idx_traces_start_time ON traces(start_time DESC);
CREATE INDEX IF NOT EXISTS idx_traces_status ON traces(status);
CREATE INDEX IF NOT EXISTS idx_traces_service ON traces(service_name);

CREATE INDEX IF NOT EXISTS idx_spans_trace ON spans(trace_id);
CREATE INDEX IF NOT EXISTS idx_spans_parent ON spans(parent_span_id);
CREATE INDEX IF NOT EXISTS idx_spans_start_time ON spans(start_time DESC);
CREATE INDEX IF NOT EXISTS idx_spans_thread ON spans(thread_id);
CREATE INDEX IF NOT EXISTS idx_spans_kind ON spans(kind);
CREATE INDEX IF NOT EXISTS idx_spans_status ON spans(status);

CREATE INDEX IF NOT EXISTS idx_events_trace ON events(trace_id);
CREATE INDEX IF NOT EXISTS idx_events_span ON events(span_id);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_events_category ON events(category);
CREATE INDEX IF NOT EXISTS idx_events_thread ON events(thread_id);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
CREATE INDEX IF NOT EXISTS idx_events_level ON events(level);
