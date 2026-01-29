# Real-Time Voice Agent - Setup Guide

## Overview

The real-time voice agent enables natural voice-to-voice conversations with your AI assistant, with full support for tool calling (custom tools and MCP tools).

## Features

✅ **Voice-to-voice conversation** - Natural speech input and output
✅ **Real-time transcription** - See what you said and what AI said
✅ **Tool calling** - All existing tools work in voice mode
✅ **MCP integration** - MCP tools work seamlessly
✅ **Session persistence** - Conversations saved to database
✅ **Event observability** - Full tracing and logging

---

## Prerequisites

1. **OpenAI API Key** with Realtime API access
2. **Go 1.21+** installed
3. **Modern browser** (Chrome, Safari, or Edge recommended)
4. **Microphone** access permissions

---

## Setup Instructions

### 1. Set Environment Variables

```bash
export OPENAI_API_KEY=sk-...
```

### 2. Use Voice Agent Configuration

```bash
# Copy the voice agent config
cp agent-config-voice.json agent-config.json

# Or set config path
export CONFIG_PATH=agent-config-voice.json
```

### 3. Start the Server

```bash
cd /Users/marcoschwartz/Documents/code/frontends/apteva/agent
go run main.go
```

You should see:
```
🎙️  Real-time voice enabled at /voice
Server starting on port :4015
```

### 4. Open the Voice Interface

Navigate to:
```
http://localhost:4015/voice.html
```

### 5. Start Talking!

1. Click **"Start Conversation"**
2. Allow microphone access when prompted
3. Wait for status to show "Ready - Start speaking!"
4. Start talking naturally
5. See real-time transcripts and tool executions
6. Click **"Stop Conversation"** when done

---

## Configuration

### Realtime Settings

```json
{
  "realtime": {
    "enabled": true,
    "voice": "marin",      // Options: alloy, marin, ash, ballad, coral, echo, sage, shimmer, verse
    "vad_type": "semantic_vad"  // Options: semantic_vad, server_vad
  }
}
```

### Available Voices

- **alloy** - Neutral, balanced
- **marin** - Warm, conversational (recommended)
- **ash** - Clear, articulate
- **ballad** - Smooth, melodic
- **coral** - Friendly, upbeat
- **echo** - Confident, direct
- **sage** - Calm, measured
- **shimmer** - Bright, energetic
- **verse** - Warm, engaging

### VAD Types

- **semantic_vad** (recommended) - Smart turn detection based on meaning
- **server_vad** - Traditional voice activity detection

---

## Example Conversations

### 1. Get Current Time

```
You: "What time is it?"

AI: "Let me check that for you."
[Tool: get_time()]
AI: "It's currently 3:45 PM."
```

### 2. Create a Task

```
You: "Remind me to call John tomorrow at 2pm"

AI: "I'll set that up for you."
[Tool: create_task({title: "Call John", execute_at: "2024-01-16 14:00"})]
AI: "I've created a reminder to call John tomorrow at 2 PM."
```

### 3. Weather Query (MCP Tool)

```
You: "What's the weather in Paris?"

AI: "Let me check that for you."
[Tool: geocode({location: "Paris"})]
[Tool: get-weather({lat: 48.8566, lon: 2.3522})]
AI: "It's currently 18 degrees Celsius and partly cloudy in Paris."
```

---

## Tool Integration

### All Existing Tools Work!

Any tool configured in your `agent-config.json` will work in voice mode:

- **Custom tools** (`tools/`)
- **MCP tools** (configured in `mcp.tools`)
- **Task management** (`create_task`, `list_tasks`, etc.)
- **Notifications** (`send_notification`)
- **Time** (`get_time`)

### Example: Enable More Tools

```json
{
  "llm": {
    "tools": [
      "get_time",
      "create_task",
      "list_tasks",
      "send_notification",
      "wait"  // Add any tool you want!
    ]
  },
  "mcp": {
    "enabled": true,
    "tools": [
      "geocode",
      "get-weather",
      "list-customers"  // MCP tools work too!
    ]
  }
}
```

---

## Troubleshooting

### Issue: "Failed to connect to OpenAI Realtime API"

**Solution:**
- Check `OPENAI_API_KEY` is set correctly
- Ensure you have Realtime API access enabled
- Check internet connection

### Issue: No audio output

**Solution:**
- Check browser audio permissions
- Ensure speakers/headphones are working
- Try refreshing the page
- Check browser console for errors

### Issue: Microphone not working

**Solution:**
- Grant microphone permissions when prompted
- Check browser settings → Site settings → Microphone
- Try a different browser (Chrome/Safari recommended)

### Issue: Tools not executing

**Solution:**
- Check tools are registered in `agent-config.json`
- Verify tool names match exactly
- Check server logs for errors
- Ensure database is accessible

### Issue: High latency

**Solution:**
- Use `semantic_vad` for better turn detection
- Check internet connection speed
- Reduce `system_prompt` length
- Consider using fewer tools

---

## API Endpoints

### WebSocket Connection

```
ws://localhost:4015/voice
```

### Session Management

```
GET http://localhost:4015/realtime/sessions
```

Returns list of active voice sessions.

---

## Cost Estimate

**Per minute of conversation:**
- Input audio: ~$0.06/min (cached) to $0.15/min
- Output audio: ~$0.24/min
- **Total: ~$0.30-0.40/min**

**Tips to reduce costs:**
- Keep conversations focused
- Use shorter system prompts
- Enable caching when possible
- Monitor usage via OpenAI dashboard

---

## Architecture

```
Browser (Microphone)
    ↓ WebSocket
Server (/voice endpoint)
    ↓ WebSocket
OpenAI Realtime API
    ↓ Function calls
Your Tools (custom + MCP)
    ↓ Results
OpenAI Realtime API
    ↓ Audio response
Browser (Speakers)
```

---

## Database Schema

Conversations are automatically saved to the database:

- **threads** - Voice conversation threads
- **messages** - User and assistant messages with transcripts
- **traces** - Tool execution traces
- **events** - All system events

---

## Development

### Add a New Tool

1. Create tool in `tools/`
2. Register in `tools/registry.go`
3. Add to `agent-config.json` → `llm.tools`
4. Restart server
5. Tool is now available in voice!

### Debug Mode

Check server logs for detailed information:
```bash
go run main.go 2>&1 | grep -E "(🎙️|🔧|✅|❌)"
```

---

## Next Steps

1. Try different voices to find your preference
2. Add custom tools specific to your use case
3. Integrate with phone systems (Vonage, Twilio)
4. Build custom UIs for specific workflows
5. Add authentication for production use

---

## Support

- Check server logs for errors
- Review `/events` endpoint for observability
- Test with `/health` endpoint
- Verify config with `/config` endpoint

---

## Security Notes

⚠️ **For development only!**

For production:
- Add authentication to `/voice` endpoint
- Use HTTPS for WebSocket (wss://)
- Validate user permissions
- Rate limit API calls
- Monitor costs closely
- Add session timeouts
- Implement proper CORS policies

---

Enjoy your voice agent! 🎙️
