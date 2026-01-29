# Event Monitoring System

## Overview

The Agent Go server now includes a comprehensive event monitoring system that streams all agent activity through a centralized event bus. This allows real-time monitoring of:

- Chat interactions
- Tool executions
- Database operations
- MCP (Model Context Protocol) activities
- Scheduler tasks
- LLM API calls
- System events and errors

## Architecture

The event system consists of:

1. **Event Bus** (`/events/bus.go`) - Central pub/sub mechanism with ring buffer
2. **Event Types** (`/events/event.go`) - Structured event definitions
3. **SSE Endpoint** (`/events/sse.go`) - Server-Sent Events streaming API
4. **Ring Buffer** (`/events/buffer.go`) - Circular buffer for event history
5. **Event Publishers** - Integrated throughout the codebase

## Event Categories

- `SYSTEM` - Startup, shutdown, configuration changes
- `CHAT` - Message received, response started/completed
- `TOOL` - Tool invocation, results, errors
- `DATABASE` - CRUD operations with timing
- `MCP` - Server connections, tool discovery, execution
- `SCHEDULER` - Task scheduling and completion
- `LLM` - API requests, responses, token usage
- `ERROR` - All system errors and failures

## API Endpoints

### GET /events
Server-Sent Events stream for real-time monitoring.

**Query Parameters:**
- `category` - Filter by categories (comma-separated): `?category=CHAT,TOOL`
- `level` - Filter by severity: `?level=error,warn`
- `thread_id` - Filter by specific thread: `?thread_id=abc123`
- `tail` - Number of historical events on connect: `?tail=100`
- `follow` - Stream new events (default true): `?follow=true`
- `last_event_id` - Resume from specific event ID

### GET /events/stats
Returns event bus statistics.

## Event Structure

```json
{
  "id": "evt_123456",
  "timestamp": "2024-01-20T10:30:00Z",
  "category": "TOOL",
  "type": "tool_invocation",
  "level": "info",
  "thread_id": "thread_abc",
  "data": {
    "tool_name": "get_time",
    "input": {...},
    "duration_ms": 45
  },
  "metadata": {
    "model": "claude-3",
    "provider": "anthropic"
  }
}
```

## Usage Examples

### Using the Web Monitor

1. Start the agent server:
```bash
go run main.go
```

2. Open the event monitor in your browser:
```bash
open examples/event-monitor.html
```

3. Click "Connect" to start streaming events

### Using curl

```bash
# Stream all events
curl -N http://localhost:4015/events

# Filter by category
curl -N "http://localhost:4015/events?category=CHAT,TOOL"

# Get last 50 events and follow
curl -N "http://localhost:4015/events?tail=50&follow=true"

# Filter by thread
curl -N "http://localhost:4015/events?thread_id=abc123"

# Get event statistics
curl http://localhost:4015/events/stats
```

### Using JavaScript

```javascript
const eventSource = new EventSource('http://localhost:4015/events?category=CHAT,TOOL');

eventSource.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log('Event:', data);
};

eventSource.addEventListener('tool_invocation', (event) => {
    const data = JSON.parse(event.data);
    console.log('Tool called:', data.data.tool_name);
});
```

## Performance

- **Ring Buffer Size**: 10,000 events (configurable)
- **Event Retention**: 24 hours (configurable)
- **Subscriber Buffer**: 100 events per subscriber
- **Async Publishing**: Non-blocking event dispatch
- **Automatic Cleanup**: Old events removed hourly

## Configuration

The event bus uses default configuration but can be customized:

```go
config := events.BusConfig{
    BufferSize:       10000,        // Ring buffer capacity
    SubscriberBuffer: 100,          // Per-subscriber buffer
    RetentionPeriod:  24 * time.Hour, // Event retention
    EnablePersistence: false,       // Database persistence (future)
}
```

## Monitoring Use Cases

1. **Debug Tool Executions**
   - Monitor tool invocations and results
   - Track execution times
   - Identify failures

2. **Performance Analysis**
   - Measure database query times
   - Track LLM response latencies
   - Monitor tool execution durations

3. **Error Tracking**
   - Real-time error notifications
   - Error patterns and frequency
   - Failure recovery monitoring

4. **User Activity**
   - Track active threads
   - Monitor message flow
   - Analyze usage patterns

5. **System Health**
   - Scheduler activity
   - MCP server connections
   - Configuration changes

## Security Considerations

- Events may contain sensitive data
- Use authentication in production
- Consider event sanitization
- Implement rate limiting
- Enable HTTPS for SSE streams

## Future Enhancements

- [ ] Event persistence to database
- [ ] Event replay functionality
- [ ] Advanced filtering expressions
- [ ] Event aggregation and metrics
- [ ] WebSocket support
- [ ] Event batching for high throughput
- [ ] Custom event types via plugins
- [ ] Event correlation and tracing

## Troubleshooting

**No events appearing:**
- Check server is running on port 4015
- Verify CORS headers if accessing from browser
- Ensure filters aren't too restrictive

**Events delayed:**
- Check subscriber buffer size
- Monitor event bus statistics
- Consider increasing buffer sizes

**Connection drops:**
- SSE connections may timeout
- Client should auto-reconnect
- Use `last_event_id` to resume