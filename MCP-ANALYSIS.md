# MCP (Model Context Protocol) Backend Analysis

## Overview

The MCP implementation provides a **plugin architecture** that allows the agent to dynamically discover and execute external tools from MCP-compliant servers. This enables extensibility without modifying the agent codebase.

---

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────┐
│                    Agent Main Process                    │
│  ┌────────────┐  ┌──────────────┐  ┌─────────────────┐ │
│  │   Config   │→│  MCP Client  │→│   Tool Cache    │ │
│  └────────────┘  └──────────────┘  └─────────────────┘ │
│         ↓               ↓                    ↓          │
│  ┌────────────┐  ┌──────────────┐  ┌─────────────────┐ │
│  │ LLM Stream │→│ MCP Executor │→│  Event System   │ │
│  └────────────┘  └──────────────┘  └─────────────────┘ │
└─────────────────────────────────────────────────────────┘
                           ↓
                    HTTP POST/GET
                           ↓
┌─────────────────────────────────────────────────────────┐
│              External MCP Server (Backend)               │
│  ┌────────────┐  ┌──────────────┐  ┌─────────────────┐ │
│  │   /servers │  │  /tools/list │  │  /tools/call    │ │
│  └────────────┘  └──────────────┘  └─────────────────┘ │
│         ↓               ↓                    ↓          │
│  ┌─────────────────────────────────────────────────────┐│
│  │     Tool Implementations (geocode, customers, etc)  ││
│  └─────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────┘
```

---

## Core Files

### 1. **mcp/client.go** (334 lines)
**Purpose**: HTTP client for communicating with MCP servers

**Key Structures**:
```go
type MCPServer struct {
    ID          int
    Name        string
    DisplayName string
    Description string
    Version     string
    Status      string
    LoadedAt    time.Time
}

type MCPTool struct {
    Name                 string
    DisplayName          string
    Description          string
    InputSchema          map[string]interface{}
    RequiresCredentials  bool
    ServerID             int
    ServerName           string
    LoadedAt             time.Time
}

type MCPCache struct {
    Servers     map[int]MCPServer         // By ID
    Tools       map[string]MCPTool        // By "server:tool"
    LastRefresh time.Time
    mu          sync.RWMutex              // Thread-safe
}
```

**Key Functions**:
- `LoadServers()` - GET `/servers` - Discover available MCP servers
- `LoadTools()` - POST `/tools/list` - List all tools from all servers
- `HealthCheck()` - GET `/servers` - Verify connectivity
- `RefreshMCPCache()` - Reload servers + tools into cache
- **Retry Logic**: Exponential backoff (1s, 2s, 3s...)
- **Cache**: Thread-safe in-memory storage with `sync.RWMutex`

**API Format**:
- **Headers**: `X-API-Key: {apiKey}`, `Content-Type: application/json`
- **Response**: `{"success": true, "servers": [...]}` or `{"success": true, "data": [...]}`

---

### 2. **mcp/executor.go** (149 lines)
**Purpose**: Execute MCP tools with parameters

**Key Functions**:
```go
func ExecuteTool(toolName string, params map[string]interface{}, cfg *config.MCPConfig) (interface{}, error)
```

**Execution Flow**:
1. **Find tool in cache** - Verify tool exists
2. **Resolve credentials** - Check if `credential_id` in params, else lookup from config
3. **Build request**:
   ```json
   {
     "name": "geocode",
     "arguments": {
       "address": "123 Main St",
       "credential_id": "cred_xyz"
     }
   }
   ```
4. **POST to `/tools/call`** with retry logic
5. **Parse response**:
   ```json
   {
     "success": true,
     "data": {...}
   }
   ```
6. **Return data** or error

**Credential Resolution**:
- First checks `params["credential_id"]`
- Falls back to config credentials where `use_for` matches tool name
- Auto-injects credential_id into params

---

### 3. **mcp/tools.go** (133 lines)
**Purpose**: Convert MCP tools to LLM-specific formats

**Key Functions**:

**Filter Enabled Tools**:
```go
func GetEnabledMCPTools(mcpConfig *config.MCPConfig) []MCPTool
```
- Returns only tools listed in `config.tools: ["geocode", "list-customers"]`
- If `config.tools` is empty, returns no tools

**Format Conversion**:
```go
// For Anthropic
func ConvertMCPToolsToAnthropicFormat(mcpTools []MCPTool) []map[string]interface{}
// Returns: [{"name": "...", "description": "...", "input_schema": {...}}]

// For OpenAI
func ConvertMCPToolsToOpenAIFormat(mcpTools []MCPTool) []map[string]interface{}
// Returns: [{"type": "function", "function": {"name": "...", "parameters": {...}}}]

// Generic
func ConvertMCPToolsToToolDefinitions(mcpTools []MCPTool) []tools.ToolDefinition
```

**Tool Lookup**:
```go
func IsMCPTool(toolName string, mcpConfig *config.MCPConfig) bool
func GetMCPToolByName(toolName string) *MCPTool
```

---

## Configuration

### Agent Config Structure

```json
{
  "agent": {
    "mcp": {
      "enabled": true,
      "base_url": "http://localhost:3100",
      "api_key": "your-mcp-api-key",
      "timeout": "30s",
      "retry_count": 3,
      "cache_ttl": "5m",
      "tools": ["geocode", "list-customers", "get-weather"],
      "credentials": {
        "stripe": {
          "credential_id": "cred_stripe_123",
          "name": "Stripe Production",
          "description": "Production Stripe API",
          "scope": ["payment"],
          "use_for": ["create-payment", "list-customers"],
          "active": true
        }
      }
    }
  }
}
```

**Fields**:
- `enabled` - Master switch
- `base_url` - MCP server endpoint
- `api_key` - Optional authentication
- `timeout` - HTTP timeout (default: 30s)
- `retry_count` - Number of retries (default: 3)
- `cache_ttl` - Cache refresh interval
- `tools` - Whitelist of enabled tools (empty = none enabled)
- `credentials` - Credential mapping for tools requiring auth

---

## Integration with Agent

### 1. **Initialization** (main.go)

```go
// At startup
if mcpConfig := cfg.Get().MCP; mcpConfig != nil && mcpConfig.Enabled {
    // Initial cache load
    if err := mcp.RefreshMCPCache(mcpConfig); err != nil {
        log.Printf("Failed to refresh MCP cache: %v", err)
    }

    // Start periodic refresh goroutine
    startMCPPeriodicRefresh(mcpConfig)
}
```

**Periodic Refresh**:
```go
func startMCPPeriodicRefresh(mcpConfig *config.MCPConfig) {
    ttl, _ := time.ParseDuration(mcpConfig.CacheTTL) // e.g., "5m"
    ticker := time.NewTicker(ttl)
    go func() {
        for range ticker.C {
            mcp.RefreshMCPCache(mcpConfig)
        }
    }()
}
```

### 2. **Tool Discovery** (main.go:1212)

```go
// When building LLM request
mcpConfig := cfg.Get().MCP
if mcpConfig != nil && mcpConfig.Enabled {
    mcpToolDefs := mcp.ConvertMCPToolsToToolDefinitions(mcp.GetEnabledMCPTools(mcpConfig))
    customTools = append(customTools, mcpToolDefs...)
}
```

This adds MCP tools to the LLM's available tools list alongside custom tools like `send_notification`, `get_time`, etc.

### 3. **Tool Execution** (stream/processor.go:441)

```go
// During LLM conversation when tool_use event occurs
if isMCPTool(event.ToolName) {
    // Publish event
    toolStartTime := time.Now()
    eventBus.Publish(NewEvent(CategoryMCP, TypeMCPToolExecution, LevelInfo))

    // Execute
    result, err := mcp.ExecuteTool(event.ToolName, event.ToolInput, mcpConfig)

    if err != nil {
        toolResultContent = fmt.Sprintf("Error: %s", err.Error())
        eventBus.Publish(NewEvent(CategoryMCP, TypeMCPError, LevelError))
    } else {
        toolResultContent = json.Marshal(result)
        eventBus.Publish(successEvent)
    }

    // Return result to LLM
    toolResult := StreamEvent{
        Type:    "tool_result",
        ToolID:  event.ToolID,
        Content: toolResultContent,
    }
}
```

---

## Event System Integration

### Event Categories

```go
events.CategoryMCP = "MCP"

// Event Types
events.TypeMCPToolExecution  // Tool called
events.TypeMCPError          // Execution failed
```

### Events Published

**1. Tool Invocation**:
```go
{
    "category": "MCP",
    "type": "mcp_tool_execution",
    "level": "info",
    "thread_id": "thread_abc",
    "data": {
        "tool_name": "geocode",
        "tool_id": "toolu_123",
        "input": {"address": "123 Main St"}
    },
    "timestamp": 1729771234567
}
```

**2. Tool Success**:
```go
{
    "category": "MCP",
    "type": "mcp_tool_execution",
    "level": "info",
    "data": {
        "tool_name": "geocode",
        "result": {...},
        "success": true
    },
    "duration_ms": 1234
}
```

**3. Tool Error**:
```go
{
    "category": "MCP",
    "type": "mcp_error",
    "level": "error",
    "data": {
        "tool_name": "geocode",
        "error": "Connection timeout"
    },
    "duration_ms": 30000
}
```

---

## API Endpoints

### External MCP Server Must Implement:

**1. GET `/servers`**
```json
Response:
{
  "success": true,
  "servers": [
    {
      "id": 1,
      "name": "my-tools",
      "display_name": "My Tools",
      "description": "Custom business tools",
      "version": "1.0.0",
      "status": "active"
    }
  ]
}
```

**2. POST `/tools/list`**
```json
Request: {}

Response:
{
  "success": true,
  "data": [
    {
      "name": "geocode",
      "display_name": "Geocode Address",
      "description": "Convert address to lat/lng",
      "inputSchema": {
        "type": "object",
        "properties": {
          "address": {"type": "string"}
        },
        "required": ["address"]
      },
      "requires_credentials": false,
      "server_id": 1
    }
  ]
}
```

**3. POST `/tools/call`**
```json
Request:
{
  "name": "geocode",
  "arguments": {
    "address": "123 Main St, NY",
    "credential_id": "cred_xyz"  // Optional
  }
}

Response:
{
  "success": true,
  "data": {
    "latitude": 40.7128,
    "longitude": -74.0060
  }
}
```

---

## Agent Endpoints

### GET `/mcp/servers`
Returns cached servers from MCP backend

```bash
curl http://localhost:4015/mcp/servers
```

Response:
```json
{
  "servers": [
    {
      "id": 1,
      "name": "my-tools",
      "display_name": "My Tools",
      "status": "active",
      "loaded_at": "2024-10-24T10:00:00Z"
    }
  ]
}
```

### GET `/mcp/tools`
Returns cached tools with optional server filter

```bash
curl http://localhost:4015/mcp/tools?server=my-tools
```

Response:
```json
{
  "tools": [
    {
      "name": "geocode",
      "display_name": "Geocode Address",
      "server_name": "my-tools",
      "requires_credentials": false
    }
  ]
}
```

### POST `/mcp/refresh`
Manually trigger cache refresh

```bash
curl -X POST http://localhost:4015/mcp/refresh
```

### GET `/mcp/status`
Check MCP connection and cache status

```bash
curl http://localhost:4015/mcp/status
```

Response:
```json
{
  "enabled": true,
  "connected": true,
  "cache_last_refresh": "2024-10-24T10:05:00Z",
  "servers_count": 1,
  "tools_count": 5,
  "enabled_tools": ["geocode", "list-customers"]
}
```

---

## Strengths

### ✅ **1. Clean Architecture**
- **Separation of concerns**: Client, Executor, Tools are separate modules
- **Thread-safe caching**: Uses `sync.RWMutex` properly
- **Global singleton pattern**: Cache and executor instances

### ✅ **2. Robust Error Handling**
- **Retry logic** with exponential backoff
- **Graceful degradation**: Errors don't crash agent
- **Event publishing**: All errors tracked in event system

### ✅ **3. Provider Agnostic**
- Converts tools to Anthropic, OpenAI, or generic formats
- Easy to add new providers

### ✅ **4. Security**
- **API key authentication** via headers
- **Credential mapping**: Tools can request specific credentials
- **Tool whitelisting**: Only enabled tools are exposed

### ✅ **5. Observability**
- **Event bus integration**: All MCP operations published
- **Timing metrics**: Duration tracking for tool execution
- **Status endpoints**: Easy debugging via `/mcp/status`

### ✅ **6. Periodic Refresh**
- **Automatic cache updates** based on `cache_ttl`
- **Background goroutine** doesn't block main thread

---

## Areas for Improvement

### ⚠️ **1. Cache Invalidation**
**Issue**: Cache only refreshes on timer, no manual invalidation

**Current**:
```go
// Cache refreshes every 5 minutes automatically
// No way to invalidate specific tools/servers
```

**Suggestion**:
```go
// Add cache invalidation methods
func (cache *MCPCache) InvalidateTool(toolName string)
func (cache *MCPCache) InvalidateServer(serverID int)

// Add webhook endpoint for backend to notify cache changes
POST /mcp/invalidate
{
  "type": "tool",
  "name": "geocode"
}
```

### ⚠️ **2. Credential Management**
**Issue**: Credentials stored in config file (not ideal for secrets)

**Current**:
```json
{
  "credentials": {
    "stripe": {
      "credential_id": "cred_123"  // Stored in JSON!
    }
  }
}
```

**Suggestion**:
```go
// Support environment variables
"credential_id": "${STRIPE_CREDENTIAL_ID}"

// Or integrate with secret manager
type SecretResolver interface {
    Resolve(credentialID string) (string, error)
}
```

### ⚠️ **3. Tool Versioning**
**Issue**: No version checking for tools

**Suggestion**:
```go
type MCPTool struct {
    Version string `json:"version"`  // Add version field
}

// Warn if tool version changes
if cachedTool.Version != newTool.Version {
    log.Printf("Warning: Tool %s version changed from %s to %s",
               toolName, cachedTool.Version, newTool.Version)
}
```

### ⚠️ **4. Tool Discovery Race Condition**
**Issue**: Tools might be called before cache is loaded

**Current**:
```go
// Initial load is async, LLM might get empty tool list
go func() {
    mcp.RefreshMCPCache(mcpConfig)
}()
```

**Suggestion**:
```go
// Block until initial cache load completes
if err := mcp.RefreshMCPCache(mcpConfig); err != nil {
    log.Printf("Warning: MCP cache not loaded: %v", err)
    // Optionally disable MCP if initial load fails
}
```

### ⚠️ **5. Rate Limiting**
**Issue**: No rate limiting for tool calls

**Suggestion**:
```go
type RateLimiter struct {
    calls    map[string]int       // tool -> count
    window   time.Duration
    maxCalls int
}

func (r *RateLimiter) Allow(toolName string) bool {
    // Check if tool has exceeded rate limit
}
```

### ⚠️ **6. Tool Execution Timeout**
**Issue**: Uses HTTP client timeout only, no per-tool timeout

**Suggestion**:
```go
type MCPTool struct {
    ExecutionTimeout string `json:"execution_timeout"` // "10s"
}

// Override HTTP timeout per tool
if tool.ExecutionTimeout != "" {
    timeout, _ := time.ParseDuration(tool.ExecutionTimeout)
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    // Execute with context
}
```

### ⚠️ **7. Bulk Tool Execution**
**Issue**: Tools executed one at a time, no batching

**Suggestion**:
```go
// Add batch execution endpoint in MCP server
POST /tools/batch
{
  "calls": [
    {"name": "geocode", "arguments": {"address": "123 Main"}},
    {"name": "get-weather", "arguments": {"location": "NY"}}
  ]
}

// Execute in parallel with goroutines
results := make([]interface{}, len(toolNames))
var wg sync.WaitGroup
for i, toolName := range toolNames {
    wg.Add(1)
    go func(idx int, name string) {
        defer wg.Done()
        results[idx], _ = mcp.ExecuteTool(name, params[idx], cfg)
    }(i, toolName)
}
wg.Wait()
```

### ⚠️ **8. Tool Response Validation**
**Issue**: No schema validation on tool responses

**Suggestion**:
```go
type MCPTool struct {
    OutputSchema map[string]interface{} `json:"output_schema"` // JSON schema
}

func (e *MCPToolExecutor) ValidateResponse(tool *MCPTool, result interface{}) error {
    // Validate result against output_schema
    return validateJSONSchema(result, tool.OutputSchema)
}
```

---

## Comparison: MCP vs Agent-to-Agent Communication

| Feature | MCP Tools | Agent Communication |
|---------|-----------|---------------------|
| **Purpose** | External tool execution | Agent delegation |
| **Protocol** | HTTP REST | HTTP REST + SSE |
| **Discovery** | Dynamic via `/tools/list` | Static in config |
| **Response** | Synchronous JSON | Synchronous or streaming |
| **State** | Stateless tool calls | Stateful conversations |
| **Use Case** | Single operations | Complex reasoning |
| **Credentials** | Per-tool credentials | Per-agent API keys |
| **Events** | `CategoryMCP` | `CategoryAgent` |

**Similarities**:
- Both use HTTP client with retry logic
- Both cache available tools/agents
- Both integrate with event system
- Both support credential/API key auth

**When to Use**:
- **MCP**: Simple operations (geocode, fetch data, calculations)
- **Agent Communication**: Complex tasks requiring reasoning (write code, analyze data, research)

---

## Example Use Cases

### 1. **Geocoding Tool**
```json
// MCP server exposes geocode tool
{
  "name": "geocode",
  "inputSchema": {
    "type": "object",
    "properties": {
      "address": {"type": "string"}
    }
  }
}

// LLM uses it
User: "Where is 123 Main St?"
LLM: *calls geocode tool*
Tool Result: {"lat": 40.7128, "lng": -74.0060}
LLM: "That address is in New York City."
```

### 2. **Customer Lookup**
```json
// MCP server exposes customer tool
{
  "name": "list-customers",
  "requires_credentials": true,
  "inputSchema": {
    "properties": {
      "query": {"type": "string"},
      "credential_id": {"type": "string"}
    }
  }
}

// Agent config maps credential
{
  "credentials": {
    "stripe": {
      "credential_id": "cred_stripe_prod",
      "use_for": ["list-customers"]
    }
  }
}

// LLM uses it automatically
User: "Show me customers named John"
LLM: *calls list-customers with auto-injected credential*
Tool Result: [{"name": "John Doe", "email": "..."}]
LLM: "I found 3 customers named John..."
```

---

## Security Considerations

### ✅ **Current Security**
1. **API key authentication** for MCP server
2. **HTTPS support** (if base_url uses https://)
3. **Credential ID indirection** (not storing actual secrets in tool params)
4. **Tool whitelisting** (only enabled tools are accessible)

### ⚠️ **Recommendations**
1. **Validate tool schemas** before caching
2. **Sanitize tool inputs** before sending to MCP server
3. **Rate limit tool execution** per thread/user
4. **Audit logging** for sensitive tool calls
5. **Encrypt credentials** in config file
6. **Support mTLS** for MCP server communication

---

## Performance

### Cache Performance
- **Read**: O(1) - map lookup by tool name
- **Write**: O(n) - replaces entire cache on refresh
- **Thread-safety**: RWMutex allows multiple concurrent readers

### Tool Execution
- **HTTP call**: ~50-500ms depending on tool
- **Retry overhead**: 1s + 2s + 3s = 6s max additional latency
- **JSON parsing**: Negligible (~1ms)

### Optimization Opportunities
1. **Partial cache updates** - Only update changed tools
2. **Connection pooling** - Reuse HTTP connections
3. **Response compression** - gzip responses from MCP server
4. **Local caching** - Cache tool results if deterministic

---

## Testing Recommendations

### Unit Tests
```go
// Test cache updates
func TestMCPCache_UpdateCache(t *testing.T)

// Test credential resolution
func TestMCPExecutor_ResolveCredentials(t *testing.T)

// Test format conversion
func TestConvertToAnthropicFormat(t *testing.T)
```

### Integration Tests
```go
// Test with mock MCP server
func TestMCPIntegration_ToolExecution(t *testing.T) {
    mockServer := httptest.NewServer(/* ... */)
    cfg := &config.MCPConfig{BaseURL: mockServer.URL}

    result, err := mcp.ExecuteTool("test-tool", params, cfg)
    assert.NoError(t, err)
    assert.Equal(t, expectedResult, result)
}
```

### End-to-End Tests
```go
// Test full LLM conversation with MCP tool
func TestE2E_LLMUsesGeocodeTool(t *testing.T) {
    // Start agent with MCP enabled
    // Send user message requiring geocode
    // Verify tool_use → geocode → tool_result flow
    // Verify final LLM response contains lat/lng
}
```

---

## Summary

### Overall Assessment: **Strong Foundation** 🟢

The MCP implementation is **well-designed** with:
- Clean separation of concerns
- Robust error handling and retries
- Good observability via events
- Provider-agnostic architecture
- Thread-safe caching

### Key Strengths
1. **Extensibility** - Easy to add new tools without code changes
2. **Reliability** - Retry logic and graceful error handling
3. **Monitoring** - Full event bus integration
4. **Security** - Credential mapping and API key auth

### Priority Improvements
1. **Credential security** - Use env vars or secret manager
2. **Cache invalidation** - Add manual invalidation API
3. **Initial load blocking** - Ensure cache loads before agent starts
4. **Tool response validation** - Validate against output schemas
5. **Rate limiting** - Prevent abuse of expensive tools

### Comparison to Agent Communication
- **MCP** is for **stateless operations** (like function calls)
- **Agent Communication** is for **stateful reasoning** (like asking an expert)
- Both complement each other nicely!

The MCP backend is production-ready with minor improvements needed for enterprise use cases.
