# Apteva Agent

Open-source AI agent with multi-provider LLM support, MCP integration, and extensible tool system.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev/)

## Features

- **Multi-provider LLM** - Anthropic Claude, OpenAI GPT, Google Gemini, Groq, and more
- **MCP Support** - Connect to external MCP servers (Composio, Smithery, custom)
- **Built-in Tools** - Tasks, notifications, subscriptions, file management
- **Memory System** - Vector embeddings for semantic memory retrieval
- **Vision** - Image and PDF analysis
- **Real-time Voice** - WebSocket-based voice chat
- **Operator Mode** - Browser automation via headless Chrome
- **Web UI** - Built-in debug interface

## Quick Start

### Installation

```bash
# Clone the repo
git clone https://github.com/apteva/agent.git
cd agent

# Copy example config
cp agent-config.example.json agent-config.json

# Edit config with your API keys
# Set ANTHROPIC_API_KEY, OPENAI_API_KEY, etc. in environment

# Run
go run main.go
```

### Docker

```bash
docker build -t apteva-agent .
docker run -p 4015:4015 \
  -e ANTHROPIC_API_KEY=your-key \
  -v $(pwd)/agent-config.json:/app/agent-config.json \
  apteva-agent
```

Server starts on port `4015`. Web UI available at `http://localhost:4015/debug`.

## Configuration

Create `agent-config.json` (see `agent-config.example.json` for full options):

```json
{
  "agent": {
    "name": "My Agent",
    "llm": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-20250514",
      "max_tokens": 8000,
      "system_prompt": "You are a helpful AI assistant."
    }
  }
}
```

### Supported Providers

| Provider | Models | Env Variable |
|----------|--------|--------------|
| `anthropic` | claude-opus-4, claude-sonnet-4, claude-haiku-4 | `ANTHROPIC_API_KEY` |
| `openai` | gpt-4o, gpt-4o-mini, o1, o3-mini | `OPENAI_API_KEY` |
| `gemini` | gemini-2.0-flash, gemini-1.5-pro | `GOOGLE_API_KEY` |
| `groq` | llama-3.3-70b, llama-3.1-8b | `GROQ_API_KEY` |
| `xai` | grok-2, grok-beta | `XAI_API_KEY` |

### External MCP Servers

Connect to MCP servers like Composio for additional tools:

```json
{
  "agent": {
    "mcp": {
      "enabled": true,
      "servers": [
        {
          "name": "composio",
          "url": "https://backend.composio.dev/v3/mcp/YOUR_ID/mcp",
          "headers": {"x-api-key": "YOUR_KEY"},
          "enabled": true
        }
      ]
    }
  }
}
```

All tools from connected MCP servers are automatically available to the agent.

## API Endpoints

### Chat

```bash
# Simple message
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello!"}'

# With streaming
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Tell me a story", "stream": true}'
```

### Threads

- `GET /threads` - List all threads
- `GET /threads/{id}` - Get thread with messages
- `DELETE /threads/{id}` - Delete thread
- `POST /reset` - Clear current thread

### Config

- `GET /config` - Get current configuration
- `POST /config` - Update configuration

### MCP

- `GET /mcp/status` - MCP connection status
- `GET /mcp/tools` - List available MCP tools
- `POST /mcp/refresh` - Refresh MCP cache

### Observability

- `GET /health` - Health check
- `GET /usage` - Token usage and costs
- `GET /traces` - List traces
- `GET /events` - SSE event stream

## Environment Variables

| Variable | Description |
|----------|-------------|
| `PORT` | Server port (default: 4015) |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `OPENAI_API_KEY` | OpenAI API key |
| `GOOGLE_API_KEY` | Google/Gemini API key |
| `GROQ_API_KEY` | Groq API key |
| `XAI_API_KEY` | xAI API key |

## License

MIT License - see [LICENSE](LICENSE) for details.
