# Agent-to-Agent Communication Testing

This scenario demonstrates and tests the agent-to-agent communication feature where one agent can call another agent to delegate specialized tasks.

## Overview

**Agent A (Research Assistant)** - Port 4015
- Has agent communication enabled
- Can call Agent B for code implementation tasks
- Tools: `call_agent`, `list_available_agents`, `get_time`, `send_notification`

**Agent B (Code Assistant)** - Port 4016
- Does NOT have agent communication (simple responder)
- Specialized in writing clean code
- Tools: `get_time`

## Quick Start

### 1. Start Both Agents

```bash
cd scenarios
./start-two-agents.sh
```

This will:
- Start Agent B (Code) on port 4016
- Start Agent A (Research) on port 4015
- Show logs from both agents
- Display helpful commands

**Keep this terminal running** - it will show live logs from both agents.

### 2. Run Tests (in a new terminal)

```bash
cd scenarios
./test-agent-communication.sh
```

This runs 5 automated tests:
1. List available agents
2. Delegate coding task to Agent B
3. Check event logs
4. Direct request to Agent B
5. Complex research + implementation scenario

### 3. Manual Testing

While agents are running, try these commands:

**List available agents:**
```bash
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "What other agents can you work with?", "stream": false}' | jq '.'
```

**Delegate a coding task:**
```bash
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Ask the Code Assistant to write a Python function that reverses a string", "stream": false}' | jq '.'
```

**Monitor agent communication events:**
```bash
curl -N 'http://localhost:4015/events?category=AGENT'
```

**Check Agent A's threads:**
```bash
curl -s http://localhost:4015/threads | jq '.'
```

## Test Scenarios

### Scenario 1: Simple Delegation

**User to Agent A:**
> "Ask the Code Assistant to write a hello world function in Python"

**Expected flow:**
1. Agent A receives request
2. Agent A uses `call_agent` tool with `agent_id: "agent-code-001"`
3. Agent A calls `http://localhost:4016/chat` with `stream: false`
4. Agent B responds with Python code
5. Agent A returns code to user

**Events published by Agent A:**
- `agent_call_started` - Before calling Agent B
- `agent_call_completed` - After receiving response
- Duration metrics logged

### Scenario 2: List Available Agents

**User to Agent A:**
> "What agents can you communicate with?"

**Expected flow:**
1. Agent A uses `list_available_agents` tool
2. Returns Agent B's info (name, capabilities, tags)

**Response includes:**
```json
{
  "agents": [
    {
      "id": "agent-code-001",
      "name": "Code Assistant",
      "capabilities": ["coding", "python", "javascript", "go", "debugging"],
      "tags": ["development", "code", "implementation"],
      "status": "available"
    }
  ]
}
```

### Scenario 3: Complex Multi-Step

**User to Agent A:**
> "Design a data pipeline and implement it"

**Expected flow:**
1. Agent A analyzes requirements
2. Agent A plans the solution
3. Agent A delegates implementation to Agent B
4. Agent A combines both and presents to user

### Scenario 4: Error Handling

**Test agent timeout:**
- Stop Agent B
- Request Agent A to call Agent B
- Expect timeout error after 60s
- Agent A should report error to user

**Test invalid agent ID:**
```bash
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Call agent with id nonexistent-agent"}'
```

Expected: Error message about agent not found

## Monitoring

### Event Stream

Watch all agent communication in real-time:
```bash
curl -N 'http://localhost:4015/events?category=AGENT'
```

**Event types:**
- `agent_comm_initialized` - System started
- `agent_call_started` - Outgoing call initiated
- `agent_call_retry` - Retry attempt
- `agent_call_completed` - Success
- `agent_call_failed` - Failure

**Example event:**
```json
{
  "id": "evt_123456",
  "timestamp": "2024-01-20T10:30:00Z",
  "category": "AGENT",
  "type": "agent_call_completed",
  "level": "info",
  "data": {
    "target_agent": "Code Assistant",
    "duration_ms": 2340,
    "response_length": 523,
    "thread_id": "thread_xyz"
  }
}
```

### Event Statistics

Get event bus stats:
```bash
curl -s http://localhost:4015/events/stats | jq '.'
```

### Logs

Agent logs are written to:
- `scenarios/logs/agent-a.log` - Agent A (Research)
- `scenarios/logs/agent-b.log` - Agent B (Code)

**Tail logs:**
```bash
tail -f scenarios/logs/agent-a.log
tail -f scenarios/logs/agent-b.log
```

**Search for agent calls:**
```bash
grep "agent_call" scenarios/logs/agent-a.log
```

## Configuration Details

### Agent A Config (`configs/agent-a-research.json`)

```json
{
  "agents": {
    "enabled": true,
    "available_agents": [
      {
        "id": "agent-code-001",
        "name": "Code Assistant",
        "url": "http://localhost:4016",
        "timeout": "60s",
        "capabilities": ["coding", "python", "javascript"],
        "tags": ["development"],
        "enabled": true
      }
    ],
    "settings": {
      "default_timeout": "30s",
      "retry_count": 2,
      "retry_delay": "2s",
      "track_call_chain": true
    }
  }
}
```

### Agent B Config (`configs/agent-b-code.json`)

```json
{
  "agents": {
    // Not included - Agent B doesn't have agent communication
  }
}
```

## Common Issues

### Port Already in Use

```bash
# Check what's using the ports
lsof -i :4015
lsof -i :4016

# Kill processes
kill $(lsof -ti:4015)
kill $(lsof -ti:4016)
```

### Agent Won't Start

Check logs:
```bash
cat scenarios/logs/agent-a.log
cat scenarios/logs/agent-b.log
```

Common issues:
- Missing `ANTHROPIC_API_KEY` environment variable
- Database file permissions
- Port conflicts

### Timeout Errors

If Agent B is slow to respond:
- Increase timeout in `agent-a-research.json`
- Check Agent B's logs for errors
- Verify Agent B is running: `curl http://localhost:4016/health`

### Connection Refused

If Agent A can't reach Agent B:
- Verify Agent B is running on port 4016
- Check firewall settings
- Ensure URL in config is correct: `http://localhost:4016`

## Cleanup

**Stop agents:**
```bash
# If using start-two-agents.sh (Ctrl+C in that terminal)
# Or manually:
kill $(cat scenarios/.agent-pids)
```

**Remove databases:**
```bash
rm scenarios/agent-a.db scenarios/agent-b.db
```

**Remove logs:**
```bash
rm -rf scenarios/logs/
```

## Advanced Testing

### Test with Streaming

Agent A can stream responses while calling Agent B synchronously:

```bash
curl -N -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Ask Code Assistant to write a sorting function"}'
```

Watch the SSE stream - you'll see:
1. Agent A thinking
2. `tool_use` event for `call_agent`
3. Wait period (Agent A calling Agent B)
4. `tool_result` with Agent B's response
5. Agent A's final response incorporating the code

### Test Retry Logic

1. Start only Agent A
2. Request Agent A to call Agent B
3. Watch retry attempts in logs
4. After retries exhausted, error returned to user

### Test API Key Authentication

Modify `agent-a-research.json`:
```json
{
  "available_agents": [
    {
      "api_key": "test-key-123"
    }
  ]
}
```

Agent A will send `X-Agent-Key: test-key-123` header to Agent B.

### Performance Testing

Time a delegation request:
```bash
time curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Ask Code Assistant to write bubble sort", "stream": false}' | jq '.'
```

Expected: 2-5 seconds total (network + both LLM calls)

## Next Steps

1. **Add More Agents**: Create Agent C (Data Analyst) on port 4017
2. **Chain Calls**: Agent A → Agent B → Agent C
3. **Add Authentication**: Test with API keys
4. **Load Testing**: Concurrent agent calls
5. **Error Scenarios**: Network failures, invalid responses
6. **Integration Tests**: Write Go tests in `agent_communication_test.go`

## Example Go Test

```go
func TestAgentCommunication(t *testing.T) {
    // Start Agent B
    serverB := startServerWithConfig(t, "configs/agent-b-code.json", 4016)
    defer serverB.stop()

    // Start Agent A
    serverA := startServerWithConfig(t, "configs/agent-a-research.json", 4015)
    defer serverA.stop()

    // Send request to Agent A
    req := map[string]interface{}{
        "message": "Ask Code Assistant to write fibonacci",
        "stream": false,
    }
    resp := sendChatRequest(t, req, 4015)

    // Verify response contains code from Agent B
    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)

    response := result["response"].(string)
    if !strings.Contains(response, "def fibonacci") {
        t.Errorf("Expected Agent B's code in response")
    }
}
```

## Resources

- [Agent Communication Proposal](../AGENT_COMMUNICATION_PROPOSAL.md)
- [Event System Documentation](../EVENT_MONITORING.md)
- [Main README](../README.md)
