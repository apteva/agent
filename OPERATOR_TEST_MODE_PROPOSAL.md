# Operator Test Mode Proposal

## Overview
Add test mode support for Operator (browser automation) to allow testing the `computer` tool without requiring a real browser service. Similar to MCP test mode, this enables development, testing, and CI/CD without external dependencies.

## Current Architecture Analysis

### What is Operator Mode?
Operator mode enables Claude's `computer` tool (formerly Computer Use) which provides browser automation capabilities:

**Current Flow:**
```
Claude requests computer action
  ↓
HandleComputerTool() in operator/operator.go
  ↓
FindBrowserSession() - finds session for agent
  ↓
ExecuteBrowserCommand() - calls external browser service
  ↓
HTTP POST to BrowserService (port 3000)
  ↓
Real browser executes action
  ↓
Screenshot/result returned to Claude
```

### Current Configuration
```go
type OperatorConfig struct {
    Enabled           bool
    DisplayWidth      int
    DisplayHeight     int
    BrowserService    *BrowserServiceConfig
    AllowedDomains    []string
    BlockedDomains    []string
    MaxActionsPerTurn int
}

type BrowserServiceConfig struct {
    BaseURL        string  // "http://localhost:3000/browser"
    APIKey         string
    Timeout        string
    SessionTimeout string
}
```

### Supported Actions
The `computer` tool supports these actions:
- `screenshot` - Capture screen
- `left_click` - Click at coordinates
- `right_click` - Right-click at coordinates
- `middle_click` - Middle-click
- `double_click` - Double-click
- `type` - Type text
- `key` - Press keyboard key
- `cursor_position` - Get cursor position
- `mouse_move` - Move mouse to coordinates

### Current Dependencies
1. **External Browser Service** - Must be running on port 3000
2. **PostgreSQL** - For session management
3. **Browser Instance** - Created via BrowserEngine API
4. **Active Session** - Session must exist before computer tool can work

## Problem Statement

**Testing operator mode currently requires:**
- ✗ Running full BrowserEngine service
- ✗ PostgreSQL database
- ✗ Real browser instances
- ✗ Network connectivity
- ✗ API credentials
- ✗ Significant setup time

**This blocks:**
- Quick local development
- CI/CD pipelines
- Unit testing
- Demo scenarios
- Agent-to-agent testing without browser service

## Proposed Solution: Operator Test Mode

Add `test_mode` flag to `OperatorConfig` that bypasses real browser service and returns mock responses.

### 1. Configuration Changes

#### Add TestMode Field
```go
type OperatorConfig struct {
    Enabled           bool
    TestMode          bool                  `json:"test_mode"`  // NEW: Enable mock browser
    DisplayWidth      int
    DisplayHeight     int
    BrowserService    *BrowserServiceConfig
    AllowedDomains    []string
    BlockedDomains    []string
    MaxActionsPerTurn int
}
```

#### Default Configuration
```go
Operator: &OperatorConfig{
    Enabled:           false,
    TestMode:          false,  // Default: use real browser
    DisplayWidth:      1024,
    DisplayHeight:     768,
    BrowserService: &BrowserServiceConfig{
        BaseURL:        "http://localhost:3000/browser",
        APIKey:         "brw_test1234567890abcdef1234567890",
        Timeout:        "30s",
        SessionTimeout: "300s",
    },
    AllowedDomains:    []string{},
    BlockedDomains:    []string{},
    MaxActionsPerTurn: 5,
}
```

### 2. Implementation Strategy

#### Option A: Lightweight Mock (Recommended)
Create simple mock responses in `operator/operator.go`:

```go
func HandleComputerTool(input map[string]interface{}) (map[string]interface{}, error) {
    cfg := config.GetConfig()
    operatorConfig := cfg.Get().Operator

    // Check test mode first
    if operatorConfig.TestMode {
        return handleComputerToolMock(input)
    }

    // Existing real browser logic...
}

func handleComputerToolMock(input map[string]interface{}) (map[string]interface{}, error) {
    action, ok := input["action"].(string)
    if !ok {
        return nil, fmt.Errorf("missing action parameter")
    }

    switch action {
    case "screenshot":
        return mockScreenshot()
    case "left_click":
        return mockClick(input)
    case "type":
        return mockType(input)
    case "key":
        return mockKey(input)
    case "mouse_move":
        return mockMouseMove(input)
    default:
        return mockGenericAction(action)
    }
}
```

#### Option B: Separate Mock Package (Future Enhancement)
Create `operator/mock/` package with full mock browser:
- Simulated DOM
- Virtual coordinates
- Screenshot generation
- Click tracking

**Recommendation:** Start with Option A (simple mocks), evolve to Option B if needed.

### 3. Mock Response Design

#### Screenshot Response
```go
func mockScreenshot() (map[string]interface{}, error) {
    // Return minimal mock screenshot (1x1 pixel data URL or placeholder)
    mockImage := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

    return map[string]interface{}{
        "success": true,
        "data": map[string]interface{}{
            "screenshot": mockImage,
            "width":      1024,
            "height":     768,
            "_mock":      true,
            "_timestamp": time.Now().Format(time.RFC3339),
        },
    }, nil
}
```

#### Click Response
```go
func mockClick(input map[string]interface{}) (map[string]interface{}, error) {
    coordinate, _ := input["coordinate"].([]interface{})
    x, _ := coordinate[0].(float64)
    y, _ := coordinate[1].(float64)

    return map[string]interface{}{
        "success": true,
        "data": map[string]interface{}{
            "action":     "click",
            "x":          int(x),
            "y":          int(y),
            "clicked":    true,
            "_mock":      true,
            "_timestamp": time.Now().Format(time.RFC3339),
        },
    }, nil
}
```

#### Type Text Response
```go
func mockType(input map[string]interface{}) (map[string]interface{}, error) {
    text, _ := input["text"].(string)

    return map[string]interface{}{
        "success": true,
        "data": map[string]interface{}{
            "action": "type",
            "text":   text,
            "typed":  len(text),
            "_mock":  true,
        },
    }, nil
}
```

### 4. Mock Session Management

When test_mode is enabled, skip browser service calls:

```go
func FindBrowserSession(agentID string) (map[string]interface{}, error) {
    cfg := config.GetConfig()
    operatorConfig := cfg.Get().Operator

    if operatorConfig.TestMode {
        // Return mock session
        return map[string]interface{}{
            "id":       fmt.Sprintf("mock-session-%s", agentID[:8]),
            "url":      "https://example.com",
            "status":   "active",
            "_mock":    true,
        }, nil
    }

    // Real session lookup...
}
```

### 5. Configuration Examples

#### Enable Test Mode
```json
{
  "operator": {
    "enabled": true,
    "test_mode": true,
    "display_width": 1024,
    "display_height": 768
  }
}
```

#### Via API
```bash
curl -X PUT http://localhost:4015/config \
  -H "Content-Type: application/json" \
  -d '{
    "operator": {
      "enabled": true,
      "test_mode": true
    }
  }'
```

### 6. Event Tracking

Mock responses should emit events with `_mock: true`:

```go
operatorEvent := events.NewEvent("operator", "computer_action", events.LevelInfo).
    WithData("action", action).
    WithData("mock", true).
    WithData("test_mode", true)
eventBus.Publish(operatorEvent)
```

## Benefits

### Development
- ✅ Test operator mode without browser service
- ✅ Faster iteration cycles
- ✅ No external dependencies
- ✅ Work offline

### Testing
- ✅ Unit tests for operator logic
- ✅ CI/CD pipeline support
- ✅ Integration tests without browser
- ✅ Deterministic responses

### Demos & Scenarios
- ✅ Quick demonstrations
- ✅ Multi-agent scenarios
- ✅ Training and tutorials
- ✅ Agent communication testing

### Production Debugging
- ✅ Toggle test mode for specific agents
- ✅ Compare mock vs real behavior
- ✅ Fallback when browser service unavailable

## Implementation Plan

### Phase 1: Basic Mock Support (30 minutes)
1. Add `TestMode bool` to `OperatorConfig`
2. Add test mode check in `HandleComputerTool()`
3. Implement basic mock responses for:
   - `screenshot`
   - `left_click`
   - `type`
4. Update default configuration

### Phase 2: Complete Mock Coverage (1 hour)
5. Add mocks for all computer actions
6. Mock session management
7. Add event tracking with `_mock` flag
8. Update documentation

### Phase 3: Testing & Validation (30 minutes)
9. Create test script `test-operator-mode.sh`
10. Add configuration examples
11. Test with Claude's computer tool
12. Verify in CI/CD

### Phase 4: Documentation (30 minutes)
13. Update OPERATOR_MODE.md
14. Add troubleshooting guide
15. Create demo scenario
16. Add to main README

**Total Estimated Time: 2.5 hours**

## Testing Strategy

### Test Script
```bash
#!/bin/bash
# test-operator-test-mode.sh

# Start agent with operator test mode
CONFIG='{
  "operator": {
    "enabled": true,
    "test_mode": true
  }
}'

echo "$CONFIG" > test-operator-config.json

PORT=4015 CONFIG_PATH=test-operator-config.json ./agent-go &
AGENT_PID=$!

sleep 2

# Test computer tool
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Take a screenshot of the current page",
    "stream": false
  }'

# Verify mock response
curl http://localhost:4015/events?category=operator

kill $AGENT_PID
```

### Validation Checklist
- [ ] Test mode flag in config
- [ ] All computer actions return mocks
- [ ] No browser service calls made
- [ ] Events include `_mock: true`
- [ ] Can toggle test mode via API
- [ ] Screenshots return placeholder
- [ ] Clicks track coordinates
- [ ] Type actions track text
- [ ] Works without browser service running

## Comparison with MCP Test Mode

| Feature | MCP Test Mode | Operator Test Mode |
|---------|---------------|-------------------|
| Configuration | `mcp.test_mode` | `operator.test_mode` |
| Default Value | `false` | `false` |
| Mock Indicator | `_mock: true` | `_mock: true` |
| Skips External Service | ✅ MCP API | ✅ Browser Service |
| Returns Mock Data | ✅ | ✅ |
| Event Tracking | ✅ | ✅ |
| No Credentials Needed | ✅ | ✅ |

## Edge Cases & Considerations

### 1. Session State
**Issue:** Real browser maintains state between actions (typed text, navigation)
**Solution:** Mock session can maintain simple state map if needed, or be stateless

### 2. Screenshot Quality
**Issue:** Mock screenshots are minimal
**Solution:** Could generate simple HTML-rendered images in future, or use placeholder

### 3. Coordinate Validation
**Issue:** Real browser validates click coordinates
**Solution:** Mock can validate coordinates against display dimensions

### 4. Timing
**Issue:** Real browser has realistic delays
**Solution:** Mock can add configurable delays (default: instant)

### 5. Error Simulation
**Issue:** Need to test error handling
**Solution:** Add `operator.test_mode_errors: true` to simulate failures

## Future Enhancements

### Advanced Mock Features
- Simulated DOM structure
- Element detection at coordinates
- Screenshot generation from HTML
- State persistence between actions
- Realistic timing delays

### Test Scenarios
- Pre-configured test pages
- Scripted interaction sequences
- Assertion helpers
- Visual regression testing

### Integration
- Share mock logic with MCP test mode
- Unified test mode configuration
- Cross-tool test scenarios

## Security Considerations

- Mock mode should be clearly indicated in logs
- Production deployments should default to `test_mode: false`
- API responses must include `_mock: true` flag
- Events must clearly mark test mode actions
- Configuration validation to prevent accidental production mocks

## Conclusion

Operator test mode follows the proven pattern from MCP test mode, providing:

1. **Quick Development** - Test operator features without browser service
2. **Reliable Testing** - Deterministic mock responses for CI/CD
3. **Easy Demos** - Showcase computer tool without setup
4. **Flexible Debugging** - Toggle between mock and real for comparison

**Recommendation:** Implement Phase 1 (basic mocks) immediately, expand to complete coverage as needed.

**Complexity:** Low - follows existing MCP test mode pattern
**Risk:** Low - isolated to operator package, no breaking changes
**Value:** High - significantly improves development experience
