# Agent Server

Go-based AI agent server with multi-provider LLM support, tool execution, and conversation management.

## Quick Start

```bash
# Run locally
go run main.go

# Or with Docker
docker build -t agent-core .
docker run -p 4015:4015 agent-core
```

Server starts on port `4015`. Web UI available at `http://localhost:4015`.

## Configuration

Create `agent-config.json`:

```json
{
  "agent": {
    "name": "My Agent",
    "public_url": "http://localhost:4015",
    "llm": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-5",
      "max_tokens": 8000,
      "temperature": 0.7,
      "system_prompt": "You are a helpful AI assistant."
    }
  }
}
```

### Supported Providers

| Provider | Models |
|----------|--------|
| `anthropic` | claude-opus-4-5, claude-sonnet-4-5, claude-haiku-4-5 |
| `openai` | gpt-4o, gpt-4o-mini, o1, o1-mini |
| `gemini` | gemini-2.0-flash, gemini-1.5-pro |
| `groq` | llama-3.3-70b-versatile, llama-3.1-8b-instant |

## API Endpoints

### Chat

**POST /chat**

Send a message (simple text):
```json
{"message": "Hello, how are you?"}
```

Send with image/file (content blocks):
```json
{
  "message": [
    {"type": "text", "text": "What is in this image?"},
    {
      "type": "image",
      "source": {
        "type": "base64",
        "media_type": "image/png",
        "data": "iVBORw0KGgo..."
      }
    }
  ]
}
```

Send with PDF:
```json
{
  "message": [
    {"type": "text", "text": "Summarize this document"},
    {
      "type": "document",
      "source": {
        "type": "base64",
        "media_type": "application/pdf",
        "data": "JVBERi0xLjQK..."
      }
    }
  ]
}
```

Send with URL:
```json
{
  "message": [
    {"type": "text", "text": "Describe this"},
    {
      "type": "image",
      "source": {"type": "url", "url": "https://example.com/image.jpg"}
    }
  ]
}
```

Full request options:
```json
{
  "message": "string or content blocks array",
  "thread_id": "optional-uuid",
  "stream": true,
  "raw_mode": false,
  "system": "optional additional system context"
}
```

### Usage & Cost Tracking

**GET /usage**

Query parameters:
- `start_time` - RFC3339 timestamp (e.g., `2025-12-01T00:00:00Z`)
- `end_time` - RFC3339 timestamp
- `thread_id` - Filter by thread
- `group_by` - `day` or `thread`

```bash
# Total usage
curl http://localhost:4015/usage

# Usage for date range
curl "http://localhost:4015/usage?start_time=2025-12-01T00:00:00Z&end_time=2025-12-09T23:59:59Z"

# Group by day
curl "http://localhost:4015/usage?group_by=day"
```

Response:
```json
{"usage":{"total_input_tokens":15420,"total_output_tokens":8350,"total_tokens":23770,"total_cost_usd":0.1245,"call_count":45},"thread_id":""}
```

### Threads

- `GET /threads` - List all threads
- `GET /threads/{id}` - Get thread with messages
- `DELETE /threads/{id}` - Delete thread
- `POST /reset` - Clear current thread

### Health & Config

- `GET /health` - Health check
- `GET /config` - Get current configuration
- `PUT /config` - Update configuration

### Observability

- `GET /traces` - List traces
- `GET /spans` - List spans
- `GET /events` - SSE event stream
- `GET /events/query` - Query stored events

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 4015 | Server port |
| `ANTHROPIC_API_KEY` | - | Anthropic API key |
| `OPENAI_API_KEY` | - | OpenAI API key |
| `GOOGLE_API_KEY` | - | Google/Gemini API key |
| `GROQ_API_KEY` | - | Groq API key |

## Features

- **Multi-provider LLM** - Switch between Claude, GPT, Gemini, Groq
- **Vision** - Image and PDF analysis
- **Tool execution** - Built-in and custom tools with parallel execution
- **Conversation threads** - Persistent conversation history
- **Token tracking** - Usage and cost monitoring per thread/day
- **MCP integration** - Model Context Protocol support
- **Real-time voice** - WebSocket-based voice chat (optional)
- **Web UI** - Built-in interface for testing

## Version

Current: 1.18.0
