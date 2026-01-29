# MCP Test Mode Scenario

Multi-agent scenario demonstrating MCP tools with `test_mode=true` flag.

## Overview

This scenario shows how to test MCP tool integrations without requiring real API credentials by using the `test_mode` flag.

**Coordinator Agent** (Port 4015)
- No MCP tools
- Delegates tasks to Worker Agent
- Tools: `get_time`, `call_agent`, `list_available_agents`

**Worker Agent** (Port 4016)
- MCP enabled with `test_mode: true`
- Has `send-notification` MCP tool
- Returns mock responses (no credentials needed)

## Prerequisites

**Assumes services are already running:**
- API Gateway on port 3000
- PostgreSQL database
- MCP endpoints available at http://localhost:3000/mcp

## Quick Start

```bash
# Terminal 2 - Run the demo
cd scenarios
./demo-mcp-test-mode.sh
```

This will:
1. Check MCP API is available
2. Start Worker Agent with MCP test mode
3. Start Coordinator Agent without MCP
4. Send test request: "Send a notification"
5. Show complete flow with mock responses
6. Display logs and events

## What Happens

```
User Request
    ↓
Coordinator Agent (no MCP)
    ↓ [uses call_agent tool]
Worker Agent (MCP test_mode=true)
    ↓ [uses send-notification MCP tool]
MCP Server (receives test_mode=true)
    ↓ [returns mock response]
Worker Agent ← {_mock: true, ...}
    ↓
Coordinator Agent
    ↓
User Response
```

## Key Features

### 1. Test Mode Flag

**Worker Agent Config:**
```json
{
  "mcp": {
    "enabled": true,
    "test_mode": true,
    "tools": ["send-notification"]
  }
}
```

### 2. Mock Responses

When `test_mode=true`, MCP server returns:
```json
{
  "success": true,
  "data": {
    "message": "Notification sent",
    "_mock": true,
    "_session": "session-123",
    "_tool": "send-notification",
    "_timestamp": "2024-01-20T10:30:00Z"
  }
}
```

### 3. No Credentials Needed

Test mode bypasses:
- API key validation
- Authentication checks
- Real external API calls
- Credential requirements

## Event Flow

Events you'll see:

1. **CHAT** - Coordinator receives user message
2. **TOOL** - Coordinator uses `call_agent`
3. **CHAT** - Worker receives delegated request
4. **MCP** - Worker calls `send-notification` with `test_mode=true`
5. **MCP** - Mock response returned
6. **TOOL** - Worker returns result to Coordinator
7. **CHAT** - Coordinator responds to user

## Manual Testing

### Start Agents Manually

**Terminal 1 - Worker:**
```bash
cd /path/to/agent
CONFIG_PATH=scenarios/configs/agent-worker-mcp-test.json \
PORT=4016 \
DB_PATH=scenarios/worker.db \
go run main.go
```

**Terminal 2 - Coordinator:**
```bash
CONFIG_PATH=scenarios/configs/agent-coordinator-mcp-test.json \
PORT=4015 \
DB_PATH=scenarios/coordinator.db \
go run main.go
```

### Send Test Request

```bash
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Please ask the Notification Worker to send a notification saying: Test from CLI",
    "stream": false
  }' | jq '.'
```

### Monitor Events

```bash
# Watch all categories
curl -N 'http://localhost:4015/events'

# Watch specific categories
curl -N 'http://localhost:4015/events?category=AGENT,TOOL,MCP'

# Watch Worker MCP events
curl -N 'http://localhost:4016/events?category=MCP'
```

### Check Logs

```bash
# Coordinator activity
tail -f scenarios/logs/coordinator.log

# Worker MCP calls
tail -f scenarios/logs/worker.log | grep -i "test_mode\|_mock"
```

## Configuration Files

### Coordinator Config
`scenarios/configs/agent-coordinator-mcp-test.json`

- No MCP tools
- Agent communication enabled
- Knows about Worker agent

### Worker Config
`scenarios/configs/agent-worker-mcp-test.json`

- MCP enabled
- `test_mode: true` (KEY!)
- `send-notification` tool available

## Testing Different Scenarios

### 1. Direct Worker Test

Test Worker directly (bypass Coordinator):

```bash
curl -X POST http://localhost:4016/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Send a notification saying: Direct test",
    "stream": false
  }'
```

### 2. Streaming Mode

```bash
curl -N -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Send a notification via the Worker"
  }'
```

Watch SSE stream show:
- Coordinator thinking
- `call_agent` tool use
- Worker processing
- MCP tool execution
- Final response

### 3. Multiple Notifications

```bash
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Ask the Worker to send 3 notifications with different messages",
    "stream": false
  }'
```

## Switching to Production Mode

To test with real credentials:

1. Update Worker config:
```json
{
  "mcp": {
    "test_mode": false,  // Change to false
    "credentials": {
      "pushover": {
        "credential_id": "real-cred-id"
      }
    }
  }
}
```

2. Add real credentials to MCP credential store
3. Restart Worker agent

## Troubleshooting

### Worker can't call MCP

Check Worker logs for MCP initialization:
```bash
grep -i "mcp" scenarios/logs/worker.log
```

Should see:
- "MCP initialized"
- "test_mode: true"

### No mock responses

Verify test_mode in request:
```bash
grep "test_mode.*true" scenarios/logs/worker.log
```

### Coordinator can't reach Worker

Check Worker is running:
```bash
curl http://localhost:4016/health
```

Verify Coordinator config has correct URL:
```json
{
  "available_agents": [{
    "url": "http://localhost:4016"
  }]
}
```

## Advanced: Add More MCP Tools

Add more tools to Worker config:

```json
{
  "mcp": {
    "test_mode": true,
    "tools": [
      "send-notification",
      "send-email",
      "create-task"
    ]
  }
}
```

All will return mock responses in test mode!

## Resources

- [MCP Test Mode Implementation](../mcp/executor.go)
- [Agent Communication](AGENT_COMMUNICATION.md)
- [Event Monitoring](../EVENT_MONITORING.md)
