# Token Usage Capture - Quick Reference

## 🎯 Goal
Capture token usage from all LLM providers, store in spans, stream via events.

## 📊 Current Status

| Component | Status | Notes |
|-----------|--------|-------|
| Database Schema | ✅ Complete | `token_usage_input`, `token_usage_output`, `cost_usd` columns exist |
| Span API | ✅ Complete | `WithTokenUsage()` method exists |
| Event Type | ✅ Complete | `TypeLLMTokens` defined |
| Usage Extraction | ❌ Missing | Data is being discarded |
| Event Publishing | ❌ Missing | Never published |
| Span Population | ❌ Missing | `WithTokenUsage()` never called |

**Summary**: Infrastructure 90% complete, just need to extract & wire up the data.

---

## 🔧 What Needs to Change

### 1. StreamEvent Structure (5 lines)
```go
// stream/processor.go:69
type StreamEvent struct {
    // ... existing fields ...
    InputTokens  int `json:"input_tokens,omitempty"`  // NEW
    OutputTokens int `json:"output_tokens,omitempty"` // NEW
    TotalTokens  int `json:"total_tokens,omitempty"`  // NEW
}
```

### 2. Anthropic Processor (30 lines)

**Add Usage struct:**
```go
type AnthropicUsage struct {
    InputTokens  int `json:"input_tokens"`
    OutputTokens int `json:"output_tokens"`
}
```

**Update ProcessLine to handle message_delta:**
```go
case "message_delta":
    if anthropicData.Usage != nil {
        return &StreamEvent{
            Type:         "usage",
            InputTokens:  anthropicData.Usage.InputTokens,
            OutputTokens: anthropicData.Usage.OutputTokens,
            TotalTokens:  anthropicData.Usage.InputTokens + anthropicData.Usage.OutputTokens,
        }, nil
    }
    return nil, nil
```

### 3. OpenAI Processor (30 lines)

**Add Usage struct:**
```go
type OpenAIUsage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

**Check for usage in ProcessLine:**
```go
// After parsing JSON, before processing choices
if openaiData.Usage != nil {
    return &StreamEvent{
        Type:         "usage",
        InputTokens:  openaiData.Usage.PromptTokens,
        OutputTokens: openaiData.Usage.CompletionTokens,
        TotalTokens:  openaiData.Usage.TotalTokens,
    }, nil
}
```

### 4. Wire Up to Spans (50 lines)

**In processStreamWithToolsAndSave:**
```go
var usageInputTokens, usageOutputTokens int

// In event processing loop:
if event.Type == "usage" {
    usageInputTokens = event.InputTokens
    usageOutputTokens = event.OutputTokens

    // Publish event
    eventBus.Publish(
        events.NewEvent(events.CategoryLLM, events.TypeLLMTokens, events.LevelInfo).
            WithThread(threadID).
            WithData("input_tokens", event.InputTokens).
            WithData("output_tokens", event.OutputTokens)
    )

    // Stream to client
    fmt.Fprintf(w, "data: {\"type\":\"usage\",\"input_tokens\":%d,\"output_tokens\":%d}\n\n",
        event.InputTokens, event.OutputTokens)
}

// Return usage with results
return toolResults, assistantMessage, usageInputTokens, usageOutputTokens, nil
```

**In UnifiedToolConversationWithBuiltins:**
```go
toolResults, assistantMessage, inputTokens, outputTokens, err := processStreamWithToolsAndSave(...)

if llmSpan != nil {
    llmSpan.
        WithTokenUsage(inputTokens, outputTokens).
        End()
}
```

---

## 📋 Files to Modify

| File | Changes | Lines |
|------|---------|-------|
| `stream/processor.go` | Add token fields, update signature | ~65 |
| `stream/anthropic_processor.go` | Parse message_delta | ~30 |
| `stream/openai_processor.go` | Parse usage field | ~30 |
| **Total Production Code** | | **~125** |

---

## 🧪 Testing

### Quick Manual Test
```bash
# Test Anthropic
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Say hello"}' | grep '"type":"usage"'

# Check database
sqlite3 app.db "SELECT token_usage_input, token_usage_output FROM spans WHERE kind='llm' LIMIT 1"
```

### Expected Stream Output
```
data: {"type":"start","content":"","timestamp":1234567890}
data: {"type":"content","content":"Hello","timestamp":1234567891}
data: {"type":"content","content":" world","timestamp":1234567892}
data: {"type":"usage","input_tokens":10,"output_tokens":5,"total_tokens":15,"timestamp":1234567893}
data: {"type":"stop","content":"","timestamp":1234567894}
data: {"type":"done","content":"","timestamp":1234567895}
```

---

## 📈 Usage Queries

### Total tokens by thread
```sql
SELECT
    thread_id,
    SUM(token_usage_input) as input,
    SUM(token_usage_output) as output
FROM spans
WHERE kind = 'llm'
GROUP BY thread_id;
```

### Daily usage
```sql
SELECT
    DATE(start_time) as date,
    COUNT(*) as calls,
    SUM(token_usage_input + token_usage_output) as total_tokens
FROM spans
WHERE kind = 'llm'
GROUP BY DATE(start_time);
```

---

## 🎨 Event Bus Integration

### Subscribe to Token Events
```javascript
const eventSource = new EventSource('/events/subscribe');

eventSource.addEventListener('message', (event) => {
  const evt = JSON.parse(event.data);

  if (evt.type === 'llm_tokens') {
    console.log(`Tokens used: ${evt.data.input_tokens} → ${evt.data.output_tokens}`);
  }
});
```

---

## ⚠️ Provider-Specific Notes

### Anthropic
- Usage in `message_delta` event (second-to-last event)
- Format: `{"usage": {"input_tokens": X, "output_tokens": Y}}`
- Currently skipped at line 273

### OpenAI / Groq
- Usage in final chunk before `[DONE]`
- Format: `{"usage": {"prompt_tokens": X, "completion_tokens": Y, "total_tokens": Z}}`
- Currently ignored

### All Providers
- Usage data may be missing in error cases
- Tool-using conversations have multiple LLM calls
- Each iteration gets its own span with token usage

---

## 💰 Cost Calculation (Optional Phase 2)

```go
// Simple pricing map
var ModelPricing = map[string]struct{InputPer1M, OutputPer1M float64}{
    "claude-3-5-sonnet-20241022": {3.00, 15.00},
    "gpt-4o":                     {2.50, 10.00},
    "gpt-4o-mini":                {0.15, 0.60},
}

func CalculateCost(model string, input, output int) float64 {
    pricing := ModelPricing[model]
    return (float64(input)/1e6 * pricing.InputPer1M) +
           (float64(output)/1e6 * pricing.OutputPer1M)
}

// Use in span:
cost := CalculateCost(model, inputTokens, outputTokens)
llmSpan.WithTokenUsage(inputTokens, outputTokens).WithCost(cost).End()
```

---

## ✅ Success Criteria

After implementation:
- [ ] All Anthropic chats show usage events in stream
- [ ] All OpenAI chats show usage events in stream
- [ ] Database spans have non-zero token counts
- [ ] Event bus receives `TypeLLMTokens` events
- [ ] Can query total tokens per thread
- [ ] Can query daily token usage
- [ ] No performance degradation

---

## 🚀 Implementation Time

- Phase 1 (MVP - capture & store): **2-3 hours**
- Phase 2 (cost calculation): **1 hour**
- Testing: **1 hour**
- **Total: 4-5 hours**

---

## 📞 Questions?

See full proposal in `TOKEN_USAGE_PROPOSAL.md` for:
- Detailed implementation guide
- Analytics query examples
- API endpoint designs
- Dashboard integration examples
- Edge cases and limitations
