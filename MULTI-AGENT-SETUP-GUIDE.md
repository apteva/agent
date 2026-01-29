# Multi-Agent Communication - Setup Guide

## Overview
Enable your agent to delegate tasks to other specialized agents. This allows for complex workflows where agents collaborate and specialize in different areas.

## 🎯 Quick Enable/Disable

### Method 1: Web UI (Easiest!)

1. **Open**: `http://localhost:4015`
2. **Go to**: Config tab → Quick Settings
3. **Find**: 🤝 Agent Communication
4. **Toggle**: Check the box to enable, uncheck to disable
5. **Status**: Shows "Enabled" or "Disabled"

**Location in UI**: Config tab > Quick Settings > 🤝 Agent Communication (web/index.html:1160)

### Method 2: API Call

**Enable:**
```bash
curl -X POST http://localhost:4015/config \
  -H "Content-Type: application/json" \
  -d '{
    "agents": {
      "enabled": true
    }
  }'
```

**Disable:**
```bash
curl -X POST http://localhost:4015/config \
  -H "Content-Type: application/json" \
  -d '{
    "agents": {
      "enabled": false
    }
  }'
```

### Method 3: Config File

Edit `agent-config.json`:
```json
{
  "agent": {
    "agents": {
      "enabled": true  ← Change this
    }
  }
}
```

---

## 📋 Full Configuration Setup

### Step 1: Enable Agent Communication

Use one of the methods above to enable it.

### Step 2: Add Available Agents

**Via API:**
```bash
curl -X POST http://localhost:4015/config \
  -H "Content-Type: application/json" \
  -d '{
    "agents": {
      "enabled": true,
      "available_agents": [
        {
          "id": "agent-code-001",
          "name": "Code Assistant",
          "description": "Expert Python/JavaScript developer",
          "url": "http://localhost:4016",
          "capabilities": ["coding", "python", "javascript", "debugging"],
          "tags": ["development", "code"],
          "enabled": true,
          "timeout": "60s",
          "api_key": ""
        },
        {
          "id": "agent-data-001",
          "name": "Data Analyst",
          "description": "Expert in data analysis and SQL",
          "url": "http://localhost:4017",
          "capabilities": ["data-analysis", "sql", "statistics"],
          "tags": ["analytics", "data"],
          "enabled": true,
          "timeout": "60s",
          "api_key": ""
        }
      ],
      "settings": {
        "default_timeout": "30s",
        "retry_count": 2,
        "retry_delay": "2s",
        "track_call_chain": true
      }
    },
    "llm": {
      "tools": ["call_agent", "list_available_agents"]
    }
  }'
```

**Via Config File:**
Add to `agent-config.json`:
```json
{
  "agent": {
    "llm": {
      "tools": ["call_agent", "list_available_agents", "create_task", ...]
    },
    "agents": {
      "enabled": true,
      "available_agents": [
        {
          "id": "agent-code-001",
          "name": "Code Assistant",
          "description": "Expert in writing clean, well-documented code",
          "url": "http://localhost:4016",
          "capabilities": ["coding", "python", "javascript", "go", "debugging"],
          "tags": ["development", "code", "implementation"],
          "enabled": true,
          "timeout": "60s",
          "api_key": ""
        }
      ],
      "settings": {
        "default_timeout": "30s",
        "retry_count": 2,
        "retry_delay": "2s",
        "max_call_depth": 2,
        "track_call_chain": true
      }
    }
  }
}
```

---

## 🚀 Running Multiple Agents

### Option 1: Demo Script (Easiest)

```bash
cd /Users/marcoschwartz/Documents/code/frontends/apteva/agent/scenarios
./demo-agent-communication.sh
```

This automatically:
- Starts Agent A (Research) on port 4015
- Starts Agent B (Code) on port 4016
- Runs test scenarios
- Shows results
- Cleans up

### Option 2: Manual Setup

**Terminal 1 - Agent A (Coordinator):**
```bash
cd /Users/marcoschwartz/Documents/code/frontends/apteva/agent
PORT=4015 CONFIG=scenarios/configs/agent-a-research.json ./agent-go
```

**Terminal 2 - Agent B (Code Specialist):**
```bash
cd /Users/marcoschwartz/Documents/code/frontends/apteva/agent
PORT=4016 CONFIG=scenarios/configs/agent-b-code.json ./agent-go
```

**Terminal 3 - Test It:**
```bash
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Ask the Code Assistant to write a hello world function"
  }'
```

---

## 🔧 Configuration Fields Explained

### Agent Definition:
```json
{
  "id": "agent-code-001",           // Unique ID for this agent
  "name": "Code Assistant",          // Display name
  "description": "What it does",     // Capabilities description
  "url": "http://localhost:4016",    // Where it's running
  "capabilities": ["coding", "python"], // Skill tags
  "tags": ["development"],           // Category tags
  "enabled": true,                   // Enable/disable
  "timeout": "60s",                  // Max wait time
  "api_key": ""                      // Optional auth (blank = no auth)
}
```

### Settings:
```json
{
  "default_timeout": "30s",      // Default timeout for all agents
  "retry_count": 2,              // How many retries on failure
  "retry_delay": "2s",           // Wait between retries
  "max_call_depth": 2,           // Prevent infinite loops (A→B→C max)
  "track_call_chain": true       // Track which agent called which
}
```

---

## 📖 Tools Available When Enabled

### `call_agent`
Call another agent to delegate a task.

**Example:**
```json
{
  "agent_id": "agent-code-001",
  "message": "Write a Python function to calculate fibonacci",
  "context": "User wants efficient implementation",
  "thread_id": "optional-existing-thread"
}
```

**Returns:**
```json
{
  "success": true,
  "response": "Here's the fibonacci function...",
  "agent_id": "agent-code-001",
  "agent_name": "Code Assistant",
  "duration_ms": 2340
}
```

### `list_available_agents`
Discover what other agents exist.

**Example:**
```json
{
  "filter_tags": ["development"],
  "filter_capabilities": ["python"]
}
```

**Returns:**
```json
{
  "success": true,
  "agents": [
    {
      "id": "agent-code-001",
      "name": "Code Assistant",
      "description": "...",
      "capabilities": ["coding", "python"],
      "tags": ["development"],
      "status": "available"
    }
  ],
  "count": 1
}
```

---

## 🎬 Example Scenarios

### Scenario 1: Simple Delegation

**User asks Agent A:**
```
"Write a hello world function in Python"
```

**Agent A thinks:**
```
I see the user wants code. Let me check available agents.
→ Calls: list_available_agents
→ Finds: Code Assistant (agent-code-001)
→ Calls: call_agent with message "Write hello world in Python"
```

**Agent B responds:**
```python
def hello_world():
    print("Hello, World!")
```

**Agent A returns:**
```
I've asked the Code Assistant to help. Here's the function:

def hello_world():
    print("Hello, World!")
```

### Scenario 2: Multi-Step Workflow

**User asks Agent A:**
```
"Create a data pipeline: fetch CSV, process it, and save results"
```

**Agent A orchestrates:**
```
1. Design the pipeline (Agent A does this)
2. Call Data Agent to process CSV → get code
3. Call Code Agent to implement pipeline → get implementation
4. Combine and return complete solution
```

---

## 📊 Monitoring Agent Communication

### Event Stream (Real-time)
```bash
curl -N 'http://localhost:4015/events?category=AGENT'
```

**Events you'll see:**
- `agent_comm_initialized` - System ready
- `agent_call_started` - Starting call to another agent
- `agent_call_completed` - Call succeeded
- `agent_call_failed` - Call failed
- `agent_call_retry` - Retrying failed call

### Event Statistics
```bash
curl http://localhost:4015/events/stats
```

Shows:
- Total agent calls
- Success/failure rates
- Average duration
- Error breakdown

---

## 🔐 Security & Authentication

### API Key Protection

If your secondary agent requires authentication:

```json
{
  "available_agents": [
    {
      "id": "secure-agent-001",
      "url": "https://api.example.com/agent",
      "api_key": "agt_secure_key_here"  ← Add this
    }
  ]
}
```

The system will send: `X-Agent-Key: agt_secure_key_here` in the request.

### Recursion Protection

Prevent infinite loops:
```json
{
  "settings": {
    "max_call_depth": 2  ← A can call B, B can call C, but C cannot call another
  }
}
```

---

## ✅ Verification

### Check if Enabled
```bash
curl -s http://localhost:4015/config | grep -A 5 '"agents"'
```

### Test List Agents
```bash
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "List all available agents you can work with"
  }'
```

### Test Call Agent
```bash
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Call agent-code-001 and ask what they can do"
  }'
```

---

## 🎯 Quick Summary

### In Web UI (NEW!):
1. Open `http://localhost:4015`
2. Config tab → Quick Settings
3. Toggle: **🤝 Agent Communication**
4. Status shows: Enabled/Disabled

### What You Get:
- ✅ `call_agent` tool - Delegate to other agents
- ✅ `list_available_agents` tool - Discover agents
- ✅ Event monitoring - Track all inter-agent calls
- ✅ Retry logic - Handle failures gracefully
- ✅ Timeout protection - Prevent hanging
- ✅ Error handling - Clear error messages

### Next Steps:
1. Enable the toggle in web UI
2. Add other agents to `available_agents` array via API or config file
3. Make sure those agents are running on their respective ports
4. Start delegating tasks!

---

## 📚 Documentation References

- **Implementation Details**: `AGENT_COMMUNICATION_IMPLEMENTATION.md`
- **Testing Guide**: `scenarios/AGENT_COMMUNICATION.md`
- **Demo Scripts**: `scenarios/demo-agent-communication.sh`
- **Example Configs**: `scenarios/configs/agent-a-research.json`

---

**The agent communication toggle is now live in the web UI!** 🎉
