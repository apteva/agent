# Global Test Mode Proposal

## Overview
Add a **global test mode** flag at the agent level that automatically enables test mode for all subsystems (MCP, Operator, future components) with a single configuration toggle.

## Problem Statement

Currently, to enable test mode for multiple subsystems, you need to configure each one individually:

```json
{
  "mcp": {
    "test_mode": true
  },
  "operator": {
    "test_mode": true
  },
  "future_subsystem": {
    "test_mode": true
  }
}
```

**Issues:**
- ❌ Repetitive configuration
- ❌ Easy to miss a subsystem
- ❌ Inconsistent test state
- ❌ Verbose for CI/CD
- ❌ Error-prone

**Common Use Cases:**
- Development environment (all subsystems should use mocks)
- CI/CD pipelines (no external dependencies)
- Demo scenarios (predictable behavior)
- Agent testing (isolated from real services)

## Proposed Solution: Global Test Mode

Add a top-level `test_mode` flag to `AgentConfig` that overrides all subsystem test modes.

### Architecture

```
AgentConfig.TestMode (global)
    ↓
    ├── MCP.TestMode (local override)
    ├── Operator.TestMode (local override)
    └── Future subsystems...

Resolution Logic:
1. Check local subsystem test_mode
2. If not set, use global AgentConfig.TestMode
3. Default: false (production)
```

## Implementation

### 1. Configuration Structure

#### Add Global TestMode to AgentConfig

```go
type AgentConfig struct {
    ID          string             `json:"id"`
    Name        string             `json:"name"`
    Description string             `json:"description"`
    TestMode    bool               `json:"test_mode"`  // NEW: Global test mode
    PublicURL   string             `json:"public_url,omitempty"`
    LLM         LLMConfig          `json:"llm"`
    MCP         *MCPConfig         `json:"mcp,omitempty"`
    Scheduler   *SchedulerConfig   `json:"scheduler,omitempty"`
    Tasks       *TasksConfig       `json:"tasks,omitempty"`
    Memory      *MemoryConfig      `json:"memory,omitempty"`
    Operator    *OperatorConfig    `json:"operator,omitempty"`
    Context     *ContextConfig     `json:"context,omitempty"`
    Agents      *AgentsConfig      `json:"agents,omitempty"`
    FileSystem  *FileSystemConfig  `json:"filesystem,omitempty"`
    Realtime    *RealtimeConfig    `json:"realtime,omitempty"`
    Version     string             `json:"version"`
}
```

#### Update Subsystem Configs

**Option A: Keep existing test_mode fields (Recommended)**
```go
type MCPConfig struct {
    Enabled     bool   `json:"enabled"`
    TestMode    *bool  `json:"test_mode,omitempty"`  // nil = use global, true/false = override
    // ... rest
}

type OperatorConfig struct {
    Enabled     bool   `json:"enabled"`
    TestMode    *bool  `json:"test_mode,omitempty"`  // nil = use global, true/false = override
    // ... rest
}
```

**Option B: Remove local test_mode (Simpler, less flexible)**
```go
// Remove TestMode from subsystems, only use global
type MCPConfig struct {
    Enabled     bool   `json:"enabled"`
    // No TestMode field
    // ... rest
}
```

**Recommendation: Option A** - Allows global default with subsystem overrides

### 2. Helper Function

Add centralized test mode resolution:

```go
// IsTestMode checks if test mode is enabled for a subsystem
// Priority: subsystem.test_mode > global.test_mode > false
func IsTestMode(globalTestMode bool, subsystemTestMode *bool) bool {
    if subsystemTestMode != nil {
        return *subsystemTestMode  // Explicit subsystem override
    }
    return globalTestMode  // Use global default
}
```

### 3. Usage in Subsystems

#### MCP Example
```go
// In mcp/executor.go
func ExecuteTool(toolName string, input map[string]interface{}, mcpConfig *config.MCPConfig, threadID string) (interface{}, error) {
    cfg := config.GetConfig()
    agentConfig := cfg.Get()

    // Check test mode (subsystem override or global)
    testMode := config.IsTestMode(agentConfig.TestMode, mcpConfig.TestMode)

    if testMode {
        return executeMockTool(toolName, input)
    }

    // Real execution...
}
```

#### Operator Example
```go
// In operator/operator.go
func HandleComputerTool(input map[string]interface{}) (map[string]interface{}, error) {
    cfg := config.GetConfig()
    agentConfig := cfg.Get()
    operatorConfig := agentConfig.Operator

    // Check test mode (subsystem override or global)
    testMode := config.IsTestMode(agentConfig.TestMode, operatorConfig.TestMode)

    if testMode {
        return handleComputerToolMock(input)
    }

    // Real browser commands...
}
```

### 4. Default Configuration

```go
func (c *Config) loadDefaults() {
    c.Agent = AgentConfig{
        ID:          uuid.New().String(),
        Name:        "AI Assistant",
        Description: "An intelligent AI assistant...",
        TestMode:    false,  // NEW: Default to production mode
        LLM: LLMConfig{
            // ...
        },
        MCP: &MCPConfig{
            Enabled:  true,
            TestMode: nil,  // nil = use global
            // ...
        },
        Operator: &OperatorConfig{
            Enabled:  false,
            TestMode: nil,  // nil = use global
            // ...
        },
    }
}
```

## Configuration Examples

### Example 1: Global Test Mode (Simplest)

Enable test mode for everything:

```json
{
  "agent": {
    "test_mode": true,
    "mcp": {
      "enabled": true
    },
    "operator": {
      "enabled": true
    }
  }
}
```

Result: Both MCP and Operator use mocks.

### Example 2: Global with Subsystem Override

Test mode globally, but use real browser:

```json
{
  "agent": {
    "test_mode": true,
    "mcp": {
      "enabled": true
    },
    "operator": {
      "enabled": true,
      "test_mode": false
    }
  }
}
```

Result: MCP uses mocks, Operator uses real browser.

### Example 3: Production with Selective Testing

Production mode globally, but test MCP:

```json
{
  "agent": {
    "test_mode": false,
    "mcp": {
      "enabled": true,
      "test_mode": true
    },
    "operator": {
      "enabled": true
    }
  }
}
```

Result: MCP uses mocks, Operator uses real browser.

### Example 4: Environment Variable

```bash
# Enable global test mode via environment
AGENT_TEST_MODE=true ./agent-go

# Or in .env
AGENT_TEST_MODE=true
```

## API Usage

### Enable Global Test Mode
```bash
curl -X PUT http://localhost:4015/config \
  -H "Content-Type: application/json" \
  -d '{
    "test_mode": true
  }'
```

### Check Current Test Mode
```bash
curl http://localhost:4015/config | jq '{
  global: .test_mode,
  mcp: .mcp.test_mode,
  operator: .operator.test_mode
}'
```

### Override Specific Subsystem
```bash
curl -X PUT http://localhost:4015/config \
  -H "Content-Type: application/json" \
  -d '{
    "test_mode": true,
    "operator": {
      "test_mode": false
    }
  }'
```

## Event Tracking

Events should indicate both global and effective test mode:

```go
event := events.NewEvent("mcp", "tool_execution", events.LevelInfo).
    WithData("tool_name", toolName).
    WithData("global_test_mode", agentConfig.TestMode).
    WithData("subsystem_test_mode", mcpConfig.TestMode).
    WithData("effective_test_mode", testMode).
    WithData("mock", testMode)
eventBus.Publish(event)
```

## Logging

Clear indicators in logs:

```go
if testMode {
    log.Printf("🧪 Test Mode: %s (global=%v, subsystem=%v)",
        subsystemName,
        agentConfig.TestMode,
        subsystemTestMode)
}
```

## Benefits

### Development
✅ **One config change** enables test mode everywhere
✅ **Consistent environment** across all subsystems
✅ **Quick toggling** for debugging
✅ **Clear intent** - "this is a test environment"

### CI/CD
✅ **Simple setup** - `AGENT_TEST_MODE=true`
✅ **No external dependencies** needed
✅ **Faster pipelines** - all mocks enabled
✅ **Deterministic tests** - predictable behavior

### Testing
✅ **Isolated unit tests** without real services
✅ **Integration tests** with selective overrides
✅ **Demo scenarios** with reproducible state
✅ **Multi-agent testing** without infrastructure

### Operations
✅ **Environment parity** - dev vs staging vs prod
✅ **Debugging aid** - compare mock vs real
✅ **Gradual rollout** - test one subsystem at a time
✅ **Documentation** - clear test vs production config

## Migration Strategy

### Phase 1: Add Global TestMode (Non-Breaking)
1. Add `TestMode bool` to `AgentConfig`
2. Change subsystem `TestMode bool` to `TestMode *bool`
3. Add `IsTestMode()` helper function
4. Default everything to `nil` (no change in behavior)

### Phase 2: Update Subsystems
1. Update MCP to use `IsTestMode()`
2. Update Operator to use `IsTestMode()`
3. Update any future subsystems

### Phase 3: Documentation & Testing
1. Update configuration examples
2. Add test cases for test mode resolution
3. Document in README and guides
4. Update CI/CD configurations

### Phase 4: Deprecation (Optional, Future)
If we want to simplify later:
1. Remove subsystem-level test mode overrides
2. Use only global test mode
3. Breaking change - requires major version bump

## Backwards Compatibility

### Option A: Fully Compatible (Recommended)
```go
// Old config still works
{
  "mcp": {
    "test_mode": true  // Still honored
  }
}

// New config also works
{
  "test_mode": true  // Applies to all
}
```

### Option B: Migration Required
```go
// Old config needs migration
{
  "mcp": {
    "test_mode": true  // No longer valid
  }
}

// Must become
{
  "test_mode": true
}
```

**Recommendation: Option A** for smooth transition

## Edge Cases

### 1. Subsystem Disabled
```go
if !mcpConfig.Enabled {
    return nil, fmt.Errorf("MCP not enabled")
}
// Test mode only matters if subsystem is enabled
```

### 2. Nil Config
```go
if operatorConfig == nil {
    return nil, fmt.Errorf("operator config is nil")
}
// Always check config exists before checking test mode
```

### 3. Environment Variable Override
```go
// Environment variable takes highest priority
if testModeEnv := os.Getenv("AGENT_TEST_MODE"); testModeEnv == "true" {
    return true
}
return config.IsTestMode(globalTestMode, subsystemTestMode)
```

## Comparison: Current vs Proposed

| Aspect | Current | Proposed |
|--------|---------|----------|
| Config Lines | 6+ per subsystem | 1 global + overrides |
| Consistency | Manual per subsystem | Automatic from global |
| CI/CD Setup | Multiple flags | Single env var |
| Flexibility | Per-subsystem only | Global + overrides |
| Clarity | Scattered config | Clear global intent |
| Maintenance | Update each subsystem | Update once |

## Testing Strategy

### Test Cases

1. **Global test mode enabled**
   - MCP uses mocks ✓
   - Operator uses mocks ✓
   - Events show `test_mode: true` ✓

2. **Global test mode disabled**
   - MCP uses real API ✓
   - Operator uses real browser ✓
   - Events show `test_mode: false` ✓

3. **Subsystem override**
   - Global: true, MCP override: false
   - MCP uses real API ✓
   - Operator uses mocks ✓

4. **Nil subsystem test mode**
   - Global: true, MCP: nil
   - MCP inherits global (mocks) ✓

5. **Environment variable**
   - `AGENT_TEST_MODE=true` overrides config ✓

### Test Script
```bash
#!/bin/bash
# test-global-test-mode.sh

echo "Test 1: Global test mode"
echo '{"test_mode": true}' > test-config.json
./agent-go -config test-config.json &
# Verify both MCP and Operator use mocks

echo "Test 2: Subsystem override"
echo '{"test_mode": true, "operator": {"test_mode": false}}' > test-config.json
# Verify MCP mocks, Operator real

echo "Test 3: Environment override"
AGENT_TEST_MODE=true ./agent-go
# Verify all subsystems use mocks
```

## Implementation Checklist

- [ ] Add `TestMode bool` to `AgentConfig`
- [ ] Change subsystem `TestMode bool` to `*bool`
- [ ] Add `IsTestMode()` helper function
- [ ] Update MCP to use helper
- [ ] Add `TestMode *bool` to `OperatorConfig`
- [ ] Update Operator to use helper
- [ ] Add environment variable support
- [ ] Update default configuration
- [ ] Add event tracking
- [ ] Add logging indicators
- [ ] Update documentation
- [ ] Add test cases
- [ ] Update API examples
- [ ] Update CI/CD configs

## Estimated Effort

**Phase 1 (Config & Helper):** 30 minutes
**Phase 2 (Update Subsystems):** 1 hour
**Phase 3 (Testing):** 30 minutes
**Phase 4 (Documentation):** 30 minutes

**Total: 2.5 hours**

## Conclusion

Global test mode provides:

1. **Simplicity** - One flag to rule them all
2. **Consistency** - Unified test environment
3. **Flexibility** - Override when needed
4. **Clarity** - Clear test vs production state
5. **Efficiency** - Faster CI/CD and development

**Recommendation:** Implement with Option A (backwards compatible, subsystem overrides supported).

**Priority:** Medium-High - Significantly improves developer experience and CI/CD workflows.
