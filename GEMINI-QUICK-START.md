# Gemini Provider Quick Start

Google Gemini has been successfully integrated into the agent server. This guide shows you how to use it.

## Setup

### 1. Get Gemini API Key

Get your API key from: https://aistudio.google.com/app/apikey

### 2. Set Environment Variable

```bash
export GEMINI_API_KEY=your-api-key-here
```

Or add to your `.env` file:
```
GEMINI_API_KEY=your-api-key-here
```

### 3. Configure Agent

Update your `agent-config.json`:

```json
{
  "llm": {
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "max_tokens": 8192,
    "temperature": 1.0
  }
}
```

Or use the example config:
```bash
cp agent-config-gemini-example.json agent-config.json
```

## Available Models

| Model | Description | Context Window | Best For |
|-------|-------------|----------------|----------|
| `gemini-2.5-flash` | Fast, cost-effective with thinking mode | 1M tokens | General tasks, coding |
| `gemini-2.5-pro` | Advanced reasoning with thinking mode | 2M tokens | Complex reasoning, analysis |
| `gemini-1.5-flash` | Previous gen, fast | 1M tokens | Legacy/stable |
| `gemini-1.5-pro` | Previous gen, advanced | 2M tokens | Legacy/stable |

## Pricing

**Gemini 2.5 Flash:**
- Input: $0.075 per 1M tokens
- Output: $0.30 per 1M tokens

**Gemini 2.5 Pro:**
- Input: $1.25 per 1M tokens
- Output: $5.00 per 1M tokens

## Usage

### Start the Server

```bash
./agent-server
```

Or build and run:
```bash
go build -o agent-server main.go
./agent-server
```

### Test with curl

**Basic chat:**
```bash
curl -X POST http://localhost:4015/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Explain quantum computing in simple terms"
  }'
```

**With streaming:**
```bash
curl -X POST http://localhost:4015/api/chat \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{
    "message": "Write a Python function to sort a list"
  }' --no-buffer
```

**With tools:**
```bash
curl -X POST http://localhost:4015/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What time is it?",
    "tools": ["get_current_time"]
  }'
```

**Multimodal (with image):**
```bash
curl -X POST http://localhost:4015/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": [
      {"type": "text", "text": "What is in this image?"},
      {
        "type": "image",
        "source": {
          "type": "base64",
          "media_type": "image/jpeg",
          "data": "<base64-encoded-image>"
        }
      }
    ]
  }'
```

## Features

### ✅ Supported

- **Text generation** - Streaming and non-streaming
- **Long context** - Up to 1M-2M tokens
- **Multimodal** - Images, documents via base64
- **Custom tools** - Function calling with your tools
- **Thinking mode** - Enhanced reasoning (2.5 models, disabled by default for speed)
- **Token usage** - Automatic tracking and reporting
- **Conversation history** - Multi-turn conversations with context

### ⚠️ Limitations

- **No built-in tools** - Unlike Anthropic (web_search, computer_use), Gemini requires custom tools
- **Tool IDs** - Generated locally (Gemini API doesn't provide them)
- **Temperature** - Gemini 3 models work best at default 1.0

## Architecture

**Files created:**
- `providers/gemini.go` - Main provider implementation
- `providers/translator.go` - Updated with Gemini types and translator
- `stream/gemini_processor.go` - Stream response processor
- `main.go` - Updated with Gemini integration

**How it works:**

1. **Message Translation**: Universal format → Gemini `contents` with `parts`
2. **Role Mapping**: `assistant` → `model`, `user` → `user`, `system` → `system_instruction`
3. **Tool Mapping**: `tool_use` → `functionCall`, `tool_result` → `functionResponse`
4. **Streaming**: SSE format with `data:` prefix, parsed by GeminiStreamProcessor
5. **Token Usage**: Extracted from `usageMetadata` in final stream chunk

## Thinking Mode

Gemini 2.5 models have "thinking mode" enabled by default for enhanced reasoning. This is **disabled by default** in our implementation for faster responses.

To enable thinking (future enhancement):
- Add `thinking_budget` parameter to generation config
- Higher budget = more thinking, slower response
- Set to 0 to disable (current default)

## Switching Providers

To switch from Anthropic/OpenAI to Gemini, just update config:

```json
{
  "llm": {
    "provider": "gemini",  // <- Change this
    "model": "gemini-2.5-flash"
  }
}
```

No code changes needed - tools, conversation history, and features work automatically.

## Troubleshooting

**Error: `GEMINI_API_KEY not set`**
- Set the environment variable: `export GEMINI_API_KEY=your-key`
- Or add to `.env` file

**Error: `401 Unauthorized`**
- Check your API key is valid
- Verify it's set correctly in environment

**Error: `400 Bad Request`**
- Check model name is correct (e.g., `gemini-2.5-flash`)
- Verify request format matches Gemini API spec

**Slow responses:**
- Thinking mode may be enabled
- Use `gemini-2.5-flash` instead of `gemini-2.5-pro`
- Reduce context window size

**Tool results not working:**
- Ensure tool names match between `tool_use` and `tool_result`
- Check tool result format is valid JSON

## Examples

### 1. Basic Q&A
```bash
curl -X POST http://localhost:4015/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "What is the capital of France?"}'
```

### 2. Code Generation
```bash
curl -X POST http://localhost:4015/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Write a Python function to calculate fibonacci numbers"}'
```

### 3. With System Prompt
Update config with custom system prompt, then:
```bash
curl -X POST http://localhost:4015/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello!"}'
```

### 4. Multi-turn Conversation
Use the same `thread_id` for context:
```bash
# First message
curl -X POST http://localhost:4015/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "My name is Alice"}'

# Follow-up (use thread_id from response)
curl -X POST http://localhost:4015/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "What is my name?", "thread_id": "<thread-id-from-above>"}'
```

## Next Steps

1. **Test basic chat** - Verify it works with simple queries
2. **Try tools** - Test function calling with custom tools
3. **Multimodal** - Send images and documents
4. **Performance** - Compare with Anthropic/OpenAI for your use case
5. **Production** - Monitor token usage and costs

## Support

For issues or questions:
- Check logs: `./agent-server` output
- Review Gemini API docs: https://ai.google.dev/gemini-api/docs
- Test with curl first before integrating

Enjoy using Gemini! 🚀
