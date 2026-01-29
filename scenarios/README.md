# Scenario Testing

End-to-end scenario tests that spawn a real server and validate agent behavior through API calls.

## Structure

```
scenarios/
├── tool_usage_test.go      # Tool usage scenarios
├── fixtures/
│   └── configs/            # Test configurations
└── README.md
```

## Quick Start

### Running Automated Tests

```bash
# Run all scenarios
go test ./scenarios/... -v

# Run specific scenario
go test ./scenarios -run TestSendNotification -v

# Run with timeout
go test ./scenarios/... -v -timeout=2m
```

### Running Live Scenarios (Manual/Interactive)

Watch scenarios execute in real-time with colored output in your terminal!

**Single Command (easiest):**

```bash
cd scenarios

# List available scenarios
./live.sh

# Run a scenario (auto-starts server, runs scenario, stops server)
./live.sh notification
./live.sh multi-tool
./live.sh operator
```

The `live.sh` script handles everything: starts server, runs scenario, cleans up.

**Available Scenarios:**
- `notification` - Basic tool usage (send notification)
- `multi-tool` - Multiple tool calls in one turn (send 2 notifications)
- `operator` - Browser automation (navigate + click)
- **`agent-communication`** - Agent-to-agent delegation (NEW!)

**Agent-to-Agent Communication (NEW!):**

Demo multi-agent collaboration where one agent delegates tasks to specialists:

```bash
# Quick demo (one command - starts agents, tests, cleans up)
./demo-agent-communication.sh

# Manual mode (keeps agents running)
./start-two-agents.sh        # Terminal 1
./test-agent-communication.sh # Terminal 2
```

See [AGENT_COMMUNICATION.md](AGENT_COMMUNICATION.md) for details.

**Advanced: Manual Control (2 terminals):**

If you want to keep the server running between scenarios:

Terminal 1:
```bash
cd scenarios
./start-server.sh configs/basic-tools.json
```

Terminal 2:
```bash
cd scenarios
go run run-scenario.go -scenario notification
go run run-scenario.go -scenario multi-tool
```

**Custom messages:**
```bash
go run run-scenario.go -message "Your custom message"
```

## How It Works

1. **Spawn Server** - Starts agent server on test port (4016)
2. **Send Request** - Makes HTTP POST to `/chat` endpoint
3. **Parse Stream** - Reads SSE response stream
4. **Evaluate** - Checks if scenario succeeded

## Writing Scenarios

### Basic Example

```go
func TestMyScenario(t *testing.T) {
    // Start server
    server := startServer(t)
    defer server.stop()
    waitForServer(t)

    // Send chat request
    req := map[string]interface{}{
        "message": "Your test prompt here",
    }
    resp := sendChatRequest(t, req)
    defer resp.Body.Close()

    // Parse SSE stream
    conversation := parseSSEStream(t, resp.Body)

    // Evaluate
    t.Run("CheckToolUsed", func(t *testing.T) {
        if !hasToolUse(conversation, "tool_name") {
            t.Errorf("Expected tool_name to be used")
        }
    })

    t.Run("CheckResponse", func(t *testing.T) {
        response := getAssistantResponse(conversation)
        if !strings.Contains(response, "expected text") {
            t.Errorf("Expected response to contain 'expected text'")
        }
    })
}
```

## Built-in Helpers

### Server Management

```go
server := startServer(t)        // Starts server on port 4016
defer server.stop()             // Cleanup
waitForServer(t)                // Wait until ready
```

### Making Requests

```go
req := map[string]interface{}{
    "message": "Send a notification",
}
resp := sendChatRequest(t, req)
defer resp.Body.Close()
```

### Parsing Responses

```go
conversation := parseSSEStream(t, resp.Body)
// Returns []SSEEvent
```

### Evaluation Helpers

```go
// Check if tool was used
hasToolUse(conversation, "send_notification")

// Check if tool succeeded
hasSuccessfulToolResult(conversation, "send_notification")

// Get assistant's response text
response := getAssistantResponse(conversation)
```

## Example Scenarios

### Tool Usage

```go
func TestSendNotificationScenario(t *testing.T) {
    server := startServer(t)
    defer server.stop()
    waitForServer(t)

    req := map[string]interface{}{
        "message": "Send a notification saying 'Hello'",
    }

    resp := sendChatRequest(t, req)
    defer resp.Body.Close()

    conversation := parseSSEStream(t, resp.Body)

    // Validate tool was used
    if !hasToolUse(conversation, "send_notification") {
        t.Errorf("Expected send_notification tool to be used")
    }

    // Validate tool succeeded
    if !hasSuccessfulToolResult(conversation, "send_notification") {
        t.Errorf("Tool execution failed")
    }

    // Validate response
    response := getAssistantResponse(conversation)
    if !strings.Contains(response, "notification") {
        t.Errorf("Response should mention notification")
    }
}
```

### Multi-Turn Conversation

```go
func TestMultiTurnScenario(t *testing.T) {
    server := startServer(t)
    defer server.stop()
    waitForServer(t)

    // First turn
    req1 := map[string]interface{}{
        "message": "What's the current time?",
    }
    resp1 := sendChatRequest(t, req1)
    conversation1 := parseSSEStream(t, resp1.Body)
    resp1.Body.Close()

    // Get thread ID from first response
    var threadID string
    for _, event := range conversation1 {
        if event.Type == "thread_id" {
            threadID = event.Content
            break
        }
    }

    // Second turn (same thread)
    req2 := map[string]interface{}{
        "message": "Now send a notification",
        "thread_id": threadID,
    }
    resp2 := sendChatRequest(t, req2)
    conversation2 := parseSSEStream(t, resp2.Body)
    resp2.Body.Close()

    // Validate second turn
    if !hasToolUse(conversation2, "send_notification") {
        t.Errorf("Expected send_notification in second turn")
    }
}
```

## SSE Event Types

| Type | Description | Fields |
|------|-------------|--------|
| `start` | Conversation started | - |
| `thread_id` | Thread ID for conversation | `content` |
| `content` | Assistant text response | `content` |
| `tool_use` | Tool is being called | `tool_name`, `tool_id` |
| `tool_result` | Tool execution result | `tool_id`, `content` |
| `stop` | Conversation ended | - |

## Configuration

Tests use the agent's default configuration from `agent-config.json`. To test with specific config:

1. Create test config in `fixtures/configs/`
2. Copy to `../agent-config.json` before starting server
3. Restore original config after test

## Debugging

### View Full Conversation

```go
for i, event := range conversation {
    t.Logf("[%d] %s: %s", i, event.Type, event.Content)
}
```

### Check Server Logs

Server stdout/stderr are captured. Access via:

```go
server := startServer(t)
// Read server.stdout or server.stderr if needed
```

### Manual Testing

Start server manually and use curl:

```bash
go run main.go &
curl -N -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Send a notification"}'
```

## Best Practices

### 1. Use Unique Ports

Tests use port 4016 to avoid conflicts with dev server (4015).

### 2. Always Cleanup

```go
server := startServer(t)
defer server.stop()  // Always stop server
```

### 3. Wait for Ready

```go
waitForServer(t)  // Don't skip this
```

### 4. Parse Full Stream

Parse until `stop` event to ensure complete conversation.

### 5. Sub-Tests for Clarity

```go
t.Run("ToolUsed", func(t *testing.T) { ... })
t.Run("ToolSucceeded", func(t *testing.T) { ... })
t.Run("ResponseQuality", func(t *testing.T) { ... })
```

## Common Patterns

### Check Tool Input Parameters

```go
// Find tool_use event
for _, event := range conversation {
    if event.Type == "tool_use" && event.ToolName == "send_notification" {
        // Parse tool input from content
        var toolUse map[string]interface{}
        json.Unmarshal([]byte(event.Content), &toolUse)

        input := toolUse["input"].(map[string]interface{})
        if input["message"] != "expected message" {
            t.Errorf("Wrong tool input")
        }
    }
}
```

### Check Response Quality

```go
response := getAssistantResponse(conversation)

// Check length
if len(response) < 10 {
    t.Errorf("Response too short")
}

// Check contains expected info
requiredPhrases := []string{"notification", "sent"}
for _, phrase := range requiredPhrases {
    if !strings.Contains(response, phrase) {
        t.Errorf("Missing phrase: %s", phrase)
    }
}
```

### Measure Performance

```go
start := time.Now()

resp := sendChatRequest(t, req)
conversation := parseSSEStream(t, resp.Body)
resp.Body.Close()

duration := time.Since(start)
t.Logf("Scenario completed in %v", duration)

if duration > 30*time.Second {
    t.Errorf("Scenario took too long: %v", duration)
}
```

## Running in CI/CD

```yaml
# GitHub Actions example
- name: Run Scenario Tests
  run: |
    go test ./scenarios/... -v -timeout=5m
  env:
    ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
```

## Troubleshooting

### Server Won't Start

- Check port 4016 is not in use: `lsof -i :4016`
- Check server builds: `go build main.go`
- Check environment variables are set

### Timeout Waiting for Server

- Increase wait time in `waitForServer`
- Check server logs for startup errors

### Tests Hang

- Ensure `defer server.stop()` is called
- Check for deadlocks in stream parsing
- Use `go test -timeout=2m` to force timeout

### Flaky Tests

- LLM responses vary - make assertions flexible
- Use temperature=0 in config for deterministic behavior
- Don't assert exact text matches, use `Contains`

## Next Steps

1. Run existing test: `go test ./scenarios -run TestSendNotification -v`
2. Add more scenarios for your use cases
3. Test edge cases (errors, timeouts, invalid input)
4. Add computer use scenarios when ready
