# Token Usage Capture - Comprehensive Proposal

## Executive Summary

**Current State**: 90% infrastructure complete, but token usage data is being discarded during streaming.

**Goal**: Capture token usage from all LLM providers (Anthropic, OpenAI, Groq), store in traces/spans, and stream via event bus for real-time monitoring and billing.

**Effort**: ~150-200 lines of code across 5 files, 2-4 hours implementation.

---

## 1. Architecture Overview

### Data Flow
```
LLM Stream Response
    ↓ (contains usage data in final events)
StreamProcessor.ProcessLine()
    ↓ (extract usage → StreamEvent with token fields)
processStreamWithToolsAndSave()
    ↓ (publish usage event + update span)
    ├──→ EventBus.Publish(TypeLLMTokens)  → SSE stream to clients
    └──→ Span.WithTokenUsage()            → Database persistence
```

### Storage Strategy

**Spans (Primary Storage)**
- `token_usage_input` - Prompt/input tokens
- `token_usage_output` - Completion/output tokens
- `cost_usd` - Calculated cost
- Stored per LLM call iteration
- Queryable for analytics and billing

**Events (Real-time Stream)**
- Published to event bus for live monitoring
- SSE streaming to connected clients
- Ephemeral (ring buffer retention)
- Used for dashboards and alerts

---

## 2. Provider-Specific Implementation

### 2.1 Anthropic (Claude)

**Streaming Format**:
```
event: message_start
data: {"type":"message_start","message":{"id":"msg_123",...}}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}

event: message_delta  ← USAGE DATA HERE
data: {
  "type":"message_delta",
  "delta":{"stop_reason":"end_turn"},
  "usage":{"output_tokens":15}
}

event: message_stop
data: {"type":"message_stop"}
```

**Key Points**:
- Usage comes in `message_delta` event (second-to-last event)
- Contains: `{"usage": {"input_tokens": X, "output_tokens": Y}}`
- Currently **skipped** at line 273 in `anthropic_processor.go`

**Changes Needed**:
1. Add `Usage` field to `AnthropicStreamData` struct
2. Parse `message_delta` event and extract usage
3. Return new `StreamEvent` with type `"usage"` containing token counts

### 2.2 OpenAI / Groq

**Streaming Format**:
```
data: {"id":"chatcmpl-123","choices":[{"delta":{"content":"Hello"}}]}

data: {"id":"chatcmpl-123","choices":[{"delta":{"content":" world"}}]}

data: {  ← USAGE DATA HERE
  "id":"chatcmpl-123",
  "choices":[{"delta":{},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}
}

data: [DONE]
```

**Key Points**:
- Usage comes in final chunk before `[DONE]`
- Contains: `{"usage": {"prompt_tokens": X, "completion_tokens": Y, "total_tokens": Z}}`
- Currently **ignored** (no special handling for usage field)

**Changes Needed**:
1. Add `Usage` field to `OpenAIStreamData` struct
2. Check for usage field when processing chunks
3. Return new `StreamEvent` with type `"usage"` containing token counts

---

## 3. Implementation Plan

### Phase 1: Core Changes (MVP)

#### 3.1 Update StreamEvent Structure
**File**: `stream/processor.go:69-78`

```go
type StreamEvent struct {
    Type              string                 `json:"type"`        // Add "usage" type
    Content           string                 `json:"content"`
    ContentBlocks     interface{}            `json:"content_blocks,omitempty"`
    ToolName          string                 `json:"tool_name,omitempty"`
    ToolID            string                 `json:"tool_id,omitempty"`
    ToolInput         map[string]interface{} `json:"tool_input"`
    ToolResult        interface{}            `json:"tool_result,omitempty"`
    Timestamp         int64                  `json:"timestamp"`

    // NEW: Token usage fields
    InputTokens       int                    `json:"input_tokens,omitempty"`
    OutputTokens      int                    `json:"output_tokens,omitempty"`
    TotalTokens       int                    `json:"total_tokens,omitempty"`
}
```

#### 3.2 Anthropic Processor Changes
**File**: `stream/anthropic_processor.go`

**A. Add Usage struct (after line 26)**:
```go
type AnthropicUsage struct {
    InputTokens  int `json:"input_tokens"`
    OutputTokens int `json:"output_tokens"`
}
```

**B. Update AnthropicStreamData (line 18)**:
```go
type AnthropicStreamData struct {
    Type         string                 `json:"type"`
    Index        int                    `json:"index,omitempty"`
    Delta        AnthropicDelta         `json:"delta,omitempty"`
    Message      AnthropicMessage       `json:"message,omitempty"`
    ContentBlock AnthropicContentBlock  `json:"content_block,omitempty"`
    ToolUseID    string                 `json:"tool_use_id,omitempty"`
    Content      interface{}            `json:"content,omitempty"`
    Usage        *AnthropicUsage        `json:"usage,omitempty"` // NEW
}
```

**C. Update ProcessLine (replace lines 273-275)**:
```go
case "message_delta":
    // Extract usage information from message_delta event
    if anthropicData.Usage != nil {
        return &StreamEvent{
            Type:         "usage",
            InputTokens:  anthropicData.Usage.InputTokens,
            OutputTokens: anthropicData.Usage.OutputTokens,
            TotalTokens:  anthropicData.Usage.InputTokens + anthropicData.Usage.OutputTokens,
            Content:      "",
        }, nil
    }
    // Skip other message delta events
    return nil, nil
```

#### 3.3 OpenAI Processor Changes
**File**: `stream/openai_processor.go`

**A. Add Usage struct (after line 14)**:
```go
type OpenAIUsage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

**B. Update OpenAIStreamData (line 16)**:
```go
type OpenAIStreamData struct {
    ID      string         `json:"id"`
    Object  string         `json:"object"`
    Created int64          `json:"created"`
    Model   string         `json:"model"`
    Choices []OpenAIChoice `json:"choices"`
    Usage   *OpenAIUsage   `json:"usage,omitempty"` // NEW
}
```

**C. Update ProcessLine (after line 65, before checking choices)**:
```go
func (p *OpenAIProcessor) ProcessLine(line string) (*StreamEvent, error) {
    // ... existing parsing code ...

    // Check for usage data (comes in final chunk before [DONE])
    if openaiData.Usage != nil {
        return &StreamEvent{
            Type:         "usage",
            InputTokens:  openaiData.Usage.PromptTokens,
            OutputTokens: openaiData.Usage.CompletionTokens,
            TotalTokens:  openaiData.Usage.TotalTokens,
            Content:      "",
        }, nil
    }

    // ... rest of existing code ...
}
```

#### 3.4 Wire Up Span Usage Tracking
**File**: `stream/processor.go`

**A. Track usage in processStreamWithToolsAndSave (after line 545)**:
```go
func processStreamWithToolsAndSave(...) ([]StreamEvent, string, error) {
    var toolResults []StreamEvent
    var assistantContent string
    var preToolContent string
    toolInputs := make(map[string]map[string]interface{})
    toolNames := make(map[string]string)
    threadIDSent := false

    // NEW: Track usage data
    var usageInputTokens, usageOutputTokens int

    scanner := bufio.NewScanner(rawReader)
    for scanner.Scan() {
        line := scanner.Text()
        event, err := processor.ProcessLine(line)
        // ... existing code ...

        if event != nil {
            // NEW: Capture usage event
            if event.Type == "usage" {
                usageInputTokens = event.InputTokens
                usageOutputTokens = event.OutputTokens

                // Publish usage event to event bus
                eventBus := events.GetEventBus()
                usageEvent := events.NewEvent(events.CategoryLLM, events.TypeLLMTokens, events.LevelInfo).
                    WithThread(threadID).
                    WithData("input_tokens", event.InputTokens).
                    WithData("output_tokens", event.OutputTokens).
                    WithData("total_tokens", event.TotalTokens)

                // Attach to current span if available
                if span := events.GetCurrentSpan(); span != nil {
                    usageEvent.WithSpan(span.ID)
                }
                if trace := events.GetCurrentTrace(); trace != nil {
                    usageEvent.WithTrace(trace.ID)
                }

                eventBus.Publish(usageEvent)

                // Stream to client
                fmt.Fprintf(w, "data: {\"type\":\"usage\",\"input_tokens\":%d,\"output_tokens\":%d,\"total_tokens\":%d,\"timestamp\":%d}\n\n",
                    event.InputTokens, event.OutputTokens, event.TotalTokens, time.Now().UnixMilli())
                flusher.Flush()

                continue // Don't add to tool results
            }

            // ... existing event handling ...
        }
    }

    // ... end of function, return with usage ...

    // Before return, store usage in a way caller can access
    // Option 1: Add to last tool result metadata
    // Option 2: Return as separate value (requires signature change)
    // Option 3: Store in context/global var (not recommended)

    return toolResults, assistantContent, usageInputTokens, usageOutputTokens, nil
}
```

**B. Update function signature (line 528)**:
```go
func processStreamWithToolsAndSave(w http.ResponseWriter, rawReader io.Reader, processor StreamProcessor, messageSaver MessageSaver, threadID string, model *string) ([]StreamEvent, string, int, int, error)
```

**C. Update UnifiedToolConversationWithBuiltins to use usage (line 231)**:
```go
// Process the stream and collect tool uses, save messages
toolResults, assistantMessage, inputTokens, outputTokens, err := processStreamWithToolsAndSave(w, rawStream, processor, messageSaver, threadID, model)
rawStream.Close()

if err != nil {
    log.Printf("🔴 Error processing stream: %v", err)
    if llmSpan != nil {
        llmSpan.RecordError(err).End()
    }
    return err
}

// End LLM span successfully with token usage
if llmSpan != nil {
    llmSpan.
        WithAttribute("tool_calls", len(toolResults)).
        WithTokenUsage(inputTokens, outputTokens). // NEW
        End()
}
```

### Phase 2: Cost Calculation (Optional Enhancement)

#### 3.5 Add Cost Calculator
**File**: `stream/cost_calculator.go` (NEW FILE)

```go
package stream

// PricingTier represents different pricing models
type PricingTier struct {
    InputPer1M  float64 // Cost per 1M input tokens
    OutputPer1M float64 // Cost per 1M output tokens
}

// ModelPricing maps model names to pricing
var ModelPricing = map[string]PricingTier{
    // Anthropic Claude
    "claude-3-5-sonnet-20241022": {InputPer1M: 3.00, OutputPer1M: 15.00},
    "claude-3-5-haiku-20241022":  {InputPer1M: 0.80, OutputPer1M: 4.00},
    "claude-3-opus-20240229":     {InputPer1M: 15.00, OutputPer1M: 75.00},

    // OpenAI GPT
    "gpt-4-turbo":                {InputPer1M: 10.00, OutputPer1M: 30.00},
    "gpt-4o":                     {InputPer1M: 2.50, OutputPer1M: 10.00},
    "gpt-4o-mini":                {InputPer1M: 0.15, OutputPer1M: 0.60},
    "gpt-3.5-turbo":              {InputPer1M: 0.50, OutputPer1M: 1.50},

    // Groq (often free/subsidized, use placeholder)
    "llama-3.1-70b-versatile":    {InputPer1M: 0.00, OutputPer1M: 0.00},
    "mixtral-8x7b-32768":         {InputPer1M: 0.00, OutputPer1M: 0.00},
}

// CalculateCost calculates USD cost for token usage
func CalculateCost(model string, inputTokens, outputTokens int) float64 {
    pricing, ok := ModelPricing[model]
    if !ok {
        // Unknown model, return 0 cost
        return 0.0
    }

    inputCost := (float64(inputTokens) / 1_000_000.0) * pricing.InputPer1M
    outputCost := (float64(outputTokens) / 1_000_000.0) * pricing.OutputPer1M

    return inputCost + outputCost
}
```

#### 3.6 Integrate Cost Calculation
**File**: `stream/processor.go`

In `UnifiedToolConversationWithBuiltins`, update span creation:

```go
// Get model for cost calculation
cfg := config.GetConfig()
llmConfig := cfg.GetLLMConfig()
cost := CalculateCost(llmConfig.Model, inputTokens, outputTokens)

// End LLM span successfully with token usage and cost
if llmSpan != nil {
    llmSpan.
        WithAttribute("tool_calls", len(toolResults)).
        WithTokenUsage(inputTokens, outputTokens).
        WithCost(cost). // NEW
        End()
}

// Also add cost to usage event
usageEvent := events.NewEvent(events.CategoryLLM, events.TypeLLMTokens, events.LevelInfo).
    WithThread(threadID).
    WithData("input_tokens", inputTokens).
    WithData("output_tokens", outputTokens).
    WithData("total_tokens", inputTokens + outputTokens).
    WithData("cost_usd", cost).
    WithData("model", llmConfig.Model)
```

---

## 4. Testing Strategy

### 4.1 Unit Tests

**Test File**: `stream/processor_test.go`

```go
func TestAnthropicUsageExtraction(t *testing.T) {
    processor := &AnthropicProcessor{}

    // Simulate message_delta event with usage
    line := `data: {"type":"message_delta","usage":{"input_tokens":100,"output_tokens":50}}`

    event, err := processor.ProcessLine(line)
    assert.NoError(t, err)
    assert.NotNil(t, event)
    assert.Equal(t, "usage", event.Type)
    assert.Equal(t, 100, event.InputTokens)
    assert.Equal(t, 50, event.OutputTokens)
    assert.Equal(t, 150, event.TotalTokens)
}

func TestOpenAIUsageExtraction(t *testing.T) {
    processor := &OpenAIProcessor{}

    // Simulate final chunk with usage
    line := `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":80,"completion_tokens":120,"total_tokens":200}}`

    event, err := processor.ProcessLine(line)
    assert.NoError(t, err)
    assert.NotNil(t, event)
    assert.Equal(t, "usage", event.Type)
    assert.Equal(t, 80, event.InputTokens)
    assert.Equal(t, 120, event.OutputTokens)
    assert.Equal(t, 200, event.TotalTokens)
}

func TestCostCalculation(t *testing.T) {
    // Test Claude Sonnet pricing
    cost := CalculateCost("claude-3-5-sonnet-20241022", 1_000_000, 1_000_000)
    assert.InDelta(t, 18.00, cost, 0.01) // $3 + $15

    // Test GPT-4o pricing
    cost = CalculateCost("gpt-4o", 1_000_000, 1_000_000)
    assert.InDelta(t, 12.50, cost, 0.01) // $2.50 + $10

    // Test unknown model
    cost = CalculateCost("unknown-model", 1_000_000, 1_000_000)
    assert.Equal(t, 0.0, cost)
}
```

### 4.2 Integration Tests

**Test File**: `test-token-usage.sh`

```bash
#!/bin/bash

echo "Testing Anthropic token usage capture..."
response=$(curl -s -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Say hello in 5 words",
    "stream": true
  }')

# Check for usage event in stream
if echo "$response" | grep -q '"type":"usage"'; then
    echo "✅ Anthropic usage event found"
else
    echo "❌ Anthropic usage event missing"
fi

echo "Testing OpenAI token usage capture..."
# Configure to use OpenAI provider
curl -s -X PUT http://localhost:8080/config \
  -H "Content-Type: application/json" \
  -d '{"llm":{"provider":"openai","model":"gpt-4o-mini"}}'

response=$(curl -s -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Say hello in 5 words",
    "stream": true
  }')

if echo "$response" | grep -q '"type":"usage"'; then
    echo "✅ OpenAI usage event found"
else
    echo "❌ OpenAI usage event missing"
fi

# Check database for stored token usage
echo "Checking database for token usage..."
sqlite3 app.db "SELECT name, token_usage_input, token_usage_output, cost_usd FROM spans WHERE kind='llm' ORDER BY start_time DESC LIMIT 5"
```

---

## 5. Database Queries for Analytics

### 5.1 Token Usage by Thread
```sql
SELECT
    s.thread_id,
    COUNT(*) as llm_calls,
    SUM(s.token_usage_input) as total_input_tokens,
    SUM(s.token_usage_output) as total_output_tokens,
    SUM(s.token_usage_input + s.token_usage_output) as total_tokens,
    SUM(s.cost_usd) as total_cost
FROM spans s
WHERE s.kind = 'llm'
  AND s.thread_id IS NOT NULL
GROUP BY s.thread_id
ORDER BY total_tokens DESC;
```

### 5.2 Token Usage by Date
```sql
SELECT
    DATE(s.start_time) as date,
    COUNT(*) as llm_calls,
    SUM(s.token_usage_input) as input_tokens,
    SUM(s.token_usage_output) as output_tokens,
    SUM(s.cost_usd) as cost
FROM spans s
WHERE s.kind = 'llm'
  AND s.start_time >= datetime('now', '-30 days')
GROUP BY DATE(s.start_time)
ORDER BY date DESC;
```

### 5.3 Token Usage by Model
```sql
SELECT
    JSON_EXTRACT(s.attributes, '$.model') as model,
    COUNT(*) as calls,
    SUM(s.token_usage_input) as input_tokens,
    SUM(s.token_usage_output) as output_tokens,
    AVG(s.token_usage_input) as avg_input,
    AVG(s.token_usage_output) as avg_output,
    SUM(s.cost_usd) as total_cost
FROM spans s
WHERE s.kind = 'llm'
GROUP BY model
ORDER BY calls DESC;
```

---

## 6. API Endpoints (Future Enhancement)

### 6.1 Get Thread Token Usage
```
GET /observability/threads/{thread_id}/usage
```

Response:
```json
{
  "thread_id": "thread_abc123",
  "llm_calls": 5,
  "total_input_tokens": 1250,
  "total_output_tokens": 830,
  "total_tokens": 2080,
  "total_cost_usd": 0.0416,
  "by_model": {
    "claude-3-5-sonnet-20241022": {
      "calls": 5,
      "tokens": 2080,
      "cost_usd": 0.0416
    }
  }
}
```

### 6.2 Get Global Usage Stats
```
GET /observability/usage?start_date=2024-01-01&end_date=2024-01-31
```

Response:
```json
{
  "period": {
    "start": "2024-01-01",
    "end": "2024-01-31"
  },
  "total_llm_calls": 1250,
  "total_input_tokens": 156000,
  "total_output_tokens": 89000,
  "total_tokens": 245000,
  "total_cost_usd": 12.45,
  "by_provider": {
    "anthropic": {"calls": 800, "cost": 9.20},
    "openai": {"calls": 450, "cost": 3.25}
  },
  "by_day": [
    {"date": "2024-01-01", "calls": 45, "tokens": 8900, "cost": 0.42},
    ...
  ]
}
```

---

## 7. Event Bus Streaming (Real-time Monitoring)

### 7.1 Subscribe to Token Usage Events

Clients can subscribe to real-time token usage via SSE:

```javascript
const eventSource = new EventSource('/events/subscribe');

eventSource.addEventListener('message', (event) => {
  const evt = JSON.parse(event.data);

  if (evt.category === 'LLM' && evt.type === 'llm_tokens') {
    console.log('Token usage:', evt.data);
    // Update dashboard with:
    // - evt.data.input_tokens
    // - evt.data.output_tokens
    // - evt.data.cost_usd
    // - evt.thread_id (if present)
  }
});
```

### 7.2 Dashboard Integration

Real-time token usage display:
```
┌─────────────────────────────────────┐
│ Live Token Usage                    │
├─────────────────────────────────────┤
│ Current Session:                    │
│   Input tokens:  1,250              │
│   Output tokens: 830                │
│   Total tokens:  2,080              │
│   Estimated cost: $0.0416           │
│                                     │
│ Last LLM Call:                      │
│   Model: claude-3-5-sonnet          │
│   Tokens: 245 → 89                  │
│   Cost: $0.0021                     │
└─────────────────────────────────────┘
```

---

## 8. Benefits

### 8.1 Immediate Benefits
- **Cost Tracking**: Know exactly how much each conversation costs
- **Usage Monitoring**: Track token usage per thread/user/model
- **Optimization**: Identify high-cost operations and optimize prompts
- **Budgeting**: Set usage limits and alerts

### 8.2 Analytics Benefits
- **Model Comparison**: Compare token efficiency across models
- **Prompt Engineering**: A/B test prompts and measure token usage
- **Tool Usage**: Understand token cost of tool-using vs. non-tool conversations
- **Trend Analysis**: Track usage over time

### 8.3 Billing Benefits
- **Usage-Based Billing**: Charge users based on actual token usage
- **Cost Attribution**: Allocate costs to specific projects/users
- **Budget Alerts**: Notify when usage exceeds thresholds
- **Audit Trail**: Complete record of all token usage

---

## 9. Migration & Rollout

### 9.1 Backward Compatibility
- All changes are additive (new fields, new event types)
- Existing code continues to work
- No breaking changes to APIs or database schema

### 9.2 Rollout Steps
1. Deploy code with token capture (Phase 1)
2. Verify tokens are being captured in logs
3. Verify spans have token usage in database
4. Enable event streaming to monitor real-time
5. Add cost calculation (Phase 2)
6. Build analytics dashboard (Phase 3 - separate project)

### 9.3 Monitoring
- Check logs for "usage" events being processed
- Query database for spans with non-zero token counts
- Subscribe to event bus and verify token events streaming
- Monitor for missing usage data (some providers may not always include it)

---

## 10. Known Limitations & Edge Cases

### 10.1 Limitations
- **Caching**: Some providers (Anthropic) support prompt caching which affects costs - not captured in this proposal
- **Streaming Interruptions**: If stream is interrupted, usage data may not arrive
- **Non-Streaming Mode**: This proposal focuses on streaming; non-streaming may need separate handling
- **Rate Limits**: Usage data doesn't include rate limit information

### 10.2 Edge Cases
- **Zero Tokens**: Some requests may return 0 tokens (errors, empty responses)
- **Missing Usage**: Not all providers always include usage (may need fallback estimation)
- **Tool Iterations**: Multi-turn tool conversations accumulate tokens across iterations
- **Context Limits**: Token counts help track approaching context limits

### 10.3 Mitigation Strategies
- Log warnings when usage data is missing
- Provide fallback estimation based on response length
- Track accumulated tokens across tool iterations in trace metadata
- Add alerts for approaching context limits

---

## 11. Success Metrics

After implementation, verify:
- ✅ All LLM calls have token usage in spans table
- ✅ Token usage events appear in event bus
- ✅ SSE streams include usage events
- ✅ Costs are calculated correctly for known models
- ✅ Query endpoints return accurate usage data
- ✅ No performance degradation (token capture adds <1ms overhead)

---

## 12. Next Steps

1. **Review & Approve** this proposal
2. **Implement Phase 1** (MVP - capture & store)
3. **Test** with all providers (Anthropic, OpenAI, Groq)
4. **Implement Phase 2** (cost calculation) - optional
5. **Build Analytics** dashboard - separate project
6. **Document** for users how to access usage data

---

## Appendix: Code Snippets Summary

### Files to Modify
1. `stream/processor.go` - Add token fields to StreamEvent, update processStreamWithToolsAndSave signature
2. `stream/anthropic_processor.go` - Parse message_delta usage
3. `stream/openai_processor.go` - Parse final chunk usage
4. `stream/cost_calculator.go` - NEW FILE (Phase 2)

### Files to Test
1. `stream/processor_test.go` - Unit tests for usage extraction
2. `test-token-usage.sh` - Integration tests

### Estimated LOC
- StreamEvent updates: ~10 lines
- Anthropic processor: ~30 lines
- OpenAI processor: ~30 lines
- processStreamWithToolsAndSave: ~50 lines
- UnifiedToolConversationWithBuiltins: ~15 lines
- Cost calculator (Phase 2): ~50 lines
- Tests: ~100 lines

**Total: ~285 lines** (185 production + 100 test)

---

**Questions? Let's discuss implementation details or prioritization.**
