# Cron Expression Support for Task Recurrence - Implementation Proposal

**Date**: 2025-11-07
**Version**: 1.0
**Author**: System Architecture
**Status**: Proposal

---

## Executive Summary

This proposal adds support for cron expressions in the task recurrence system while maintaining backward compatibility with the existing simple patterns (`daily`, `weekly`, `monthly`). This enables flexible scheduling like "every 5 minutes" (`*/5 * * * *`) or "weekdays at 9am" (`0 9 * * 1-5`).

---

## Current Limitations

### What We Have Now

```go
recurrence TEXT CHECK (recurrence IN ('daily', 'weekly', 'monthly', NULL))
```

**Supported:**
- `daily` → Every 24 hours
- `weekly` → Every 7 days
- `monthly` → Every 30 days

**NOT Supported:**
- Custom intervals (every 5 minutes, every 2 hours)
- Complex schedules (weekdays only, specific times)
- Cron expressions

### User Pain Points

1. **Cannot schedule frequent tasks** - "Every 5 minutes" requires workarounds
2. **No time-of-day control** - Cannot specify "every day at 3am"
3. **No weekday/weekend logic** - Cannot do "weekdays only"
4. **No multiple times per day** - Cannot do "9am, 1pm, 5pm daily"

---

## Proposed Solution

### Design Principles

1. **Backward Compatible** - Existing `daily`/`weekly`/`monthly` continue to work
2. **Standard Cron Syntax** - Use industry-standard cron expressions
3. **Validated Input** - Reject invalid cron expressions at creation
4. **Clear Naming** - Use `cron:` prefix to distinguish from simple patterns
5. **Robust Parsing** - Use proven library (robfig/cron)

### Syntax

Users can pass cron expressions with a `cron:` prefix:

```json
{
  "title": "5-minute check",
  "recurrence": "cron:*/5 * * * *"
}
```

**Format:**
```
cron:<expression>
```

**Examples:**
```
cron:*/5 * * * *       → Every 5 minutes
cron:0 */2 * * *       → Every 2 hours
cron:0 9 * * 1-5       → Weekdays at 9am
cron:0 0 1 * *         → First of month at midnight
cron:30 2 * * 0        → Sundays at 2:30am
```

---

## Architecture Changes

### 1. Database Schema

**Current:**
```sql
recurrence TEXT CHECK (recurrence IN ('daily', 'weekly', 'monthly', NULL))
```

**Proposed:**
```sql
recurrence TEXT  -- Remove CHECK constraint to allow any string
```

**Migration:**
```sql
-- Drop the constraint
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_recurrence_check;

-- Add comment
COMMENT ON COLUMN tasks.recurrence IS
  'Recurrence pattern: "daily", "weekly", "monthly", or "cron:<expression>"';
```

### 2. Add Cron Library Dependency

**Add to go.mod:**
```bash
go get github.com/robfig/cron/v3
```

**go.mod addition:**
```go
require (
    github.com/robfig/cron/v3 v3.0.1
)
```

**Why robfig/cron?**
- Industry standard (13k+ stars)
- Used by Kubernetes, Docker, etc.
- Supports standard cron syntax
- Well-tested and maintained
- Zero external dependencies

### 3. Tool Schema Update

**tools/task_tools.go:32-36**

**Current:**
```go
"recurrence": map[string]interface{}{
    "type":        "string",
    "enum":        []string{"daily", "weekly", "monthly"},
    "description": "Optional recurrence",
}
```

**Proposed:**
```go
"recurrence": map[string]interface{}{
    "type":        "string",
    "description": "Recurrence pattern: 'daily', 'weekly', 'monthly', or 'cron:<expression>' (e.g., 'cron:*/5 * * * *' for every 5 minutes)",
    "examples": []string{
        "daily",
        "weekly",
        "monthly",
        "cron:*/5 * * * *",
        "cron:0 9 * * 1-5",
        "cron:0 0 1 * *",
    },
}
```

**Remove enum restriction** to allow cron expressions.

### 4. Validation Function

**New file: tools/cron_validator.go**

```go
package tools

import (
    "fmt"
    "strings"
    "github.com/robfig/cron/v3"
)

// ValidateRecurrence validates a recurrence pattern
func ValidateRecurrence(recurrence string) error {
    if recurrence == "" {
        return nil // Allow empty for one-time tasks
    }

    // Check simple patterns first
    if recurrence == "daily" || recurrence == "weekly" || recurrence == "monthly" {
        return nil
    }

    // Check if it's a cron expression
    if strings.HasPrefix(recurrence, "cron:") {
        cronExpr := strings.TrimPrefix(recurrence, "cron:")

        // Validate cron expression
        parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
        _, err := parser.Parse(cronExpr)
        if err != nil {
            return fmt.Errorf("invalid cron expression '%s': %w", cronExpr, err)
        }

        return nil
    }

    return fmt.Errorf("invalid recurrence pattern '%s'. Must be 'daily', 'weekly', 'monthly', or 'cron:<expression>'", recurrence)
}

// IsCronExpression checks if a recurrence is a cron expression
func IsCronExpression(recurrence string) bool {
    return strings.HasPrefix(recurrence, "cron:")
}

// ParseCronExpression extracts the cron part from "cron:*/5 * * * *"
func ParseCronExpression(recurrence string) string {
    return strings.TrimPrefix(recurrence, "cron:")
}
```

### 5. Next Run Calculation

**Update tools/task_management.go:556-578**

**Current:**
```go
func calculateNextRun(pattern string, from *string) *string {
    baseTime := time.Now()
    if from != nil {
        if t, err := time.Parse(time.RFC3339, *from); err == nil {
            baseTime = t
        }
    }

    var next time.Time
    switch pattern {
    case "daily":
        next = baseTime.AddDate(0, 0, 1)
    case "weekly":
        next = baseTime.AddDate(0, 0, 7)
    case "monthly":
        next = baseTime.AddDate(0, 1, 0)
    default:
        return nil
    }

    result := next.Format(time.RFC3339)
    return &result
}
```

**Proposed:**
```go
func calculateNextRun(pattern string, from *string) *string {
    baseTime := time.Now()
    if from != nil {
        if t, err := time.Parse(time.RFC3339, *from); err == nil {
            baseTime = t
        }
    }

    var next time.Time

    // Handle cron expressions
    if IsCronExpression(pattern) {
        cronExpr := ParseCronExpression(pattern)
        parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
        schedule, err := parser.Parse(cronExpr)
        if err != nil {
            log.Printf("Error parsing cron expression '%s': %v", cronExpr, err)
            return nil
        }

        // Get next occurrence after baseTime
        next = schedule.Next(baseTime)

        result := next.Format(time.RFC3339)
        return &result
    }

    // Handle simple patterns (backward compatibility)
    switch pattern {
    case "daily":
        next = baseTime.AddDate(0, 0, 1)
    case "weekly":
        next = baseTime.AddDate(0, 0, 7)
    case "monthly":
        next = baseTime.AddDate(0, 1, 0)
    default:
        return nil
    }

    result := next.Format(time.RFC3339)
    return &result
}
```

### 6. Task Creation Validation

**Update tools/task_management.go:44-96**

Add validation before creating task:

```go
func CreateTaskTool(input map[string]interface{}) (map[string]interface{}, error) {
    // ... existing code ...

    // Handle recurrence
    if rec, ok := input["recurrence"].(string); ok && rec != "" {
        // VALIDATE RECURRENCE PATTERN
        if err := ValidateRecurrence(rec); err != nil {
            return nil, fmt.Errorf("invalid recurrence: %w", err)
        }

        taskType = "recurring"
        recurrence = &rec
        // Calculate next run based on recurrence
        nextRun = calculateNextRun(rec, executeAt)
    }

    // ... rest of existing code ...
}
```

---

## Implementation Steps

### Phase 1: Foundation (1-2 hours)

1. **Add dependency**
   ```bash
   cd /Users/marcoschwartz/Documents/code/frontends/apteva/agent
   go get github.com/robfig/cron/v3
   ```

2. **Create validator**
   - New file: `tools/cron_validator.go`
   - Implement `ValidateRecurrence()`, `IsCronExpression()`, `ParseCronExpression()`

3. **Write tests**
   - Test valid patterns: `daily`, `weekly`, `cron:*/5 * * * *`
   - Test invalid patterns: `invalid`, `cron:bad syntax`

### Phase 2: Core Logic (2-3 hours)

4. **Update calculateNextRun()**
   - Add cron expression parsing
   - Maintain backward compatibility
   - Handle edge cases

5. **Update CreateTaskTool()**
   - Add validation call
   - Test with cron expressions

6. **Update tool schema**
   - Remove enum restriction
   - Add examples in description

### Phase 3: Database (30 minutes)

7. **Create migration**
   - File: `migrations/003_cron_support.sql`
   - Drop CHECK constraint
   - Add column comment

8. **Test migration**
   - Run on test database
   - Verify constraint removed

### Phase 4: Testing (1-2 hours)

9. **Integration tests**
   - Create tasks with cron expressions
   - Verify next_run calculation
   - Test scheduler execution

10. **Edge cases**
    - Invalid cron syntax
    - Mixed old/new patterns
    - Timezone handling

### Phase 5: Documentation (1 hour)

11. **Update docs**
    - Add cron examples
    - Migration guide
    - API reference

---

## Examples

### Simple Patterns (Existing - No Change)

```bash
# Daily at current time
curl -X POST http://localhost:4015/tasks \
  -d '{"title":"Daily","recurrence":"daily"}'

# Weekly
curl -X POST http://localhost:4015/tasks \
  -d '{"title":"Weekly","recurrence":"weekly"}'
```

### Cron Expressions (NEW)

**Every 5 minutes:**
```bash
curl -X POST http://localhost:4015/tasks \
  -d '{
    "title": "5-minute health check",
    "description": "Check system health",
    "recurrence": "cron:*/5 * * * *"
  }'
```

**Every 2 hours:**
```bash
curl -X POST http://localhost:4015/tasks \
  -d '{
    "title": "Bi-hourly sync",
    "recurrence": "cron:0 */2 * * *"
  }'
```

**Weekdays at 9am:**
```bash
curl -X POST http://localhost:4015/tasks \
  -d '{
    "title": "Morning report",
    "recurrence": "cron:0 9 * * 1-5",
    "execute_at": "2025-11-08T09:00:00Z"
  }'
```

**First of month at midnight:**
```bash
curl -X POST http://localhost:4015/tasks \
  -d '{
    "title": "Monthly invoice",
    "recurrence": "cron:0 0 1 * *"
  }'
```

**Multiple times daily (9am, 1pm, 5pm):**
```bash
curl -X POST http://localhost:4015/tasks \
  -d '{
    "title": "Three daily checks",
    "recurrence": "cron:0 9,13,17 * * *"
  }'
```

**Sundays at 2:30am:**
```bash
curl -X POST http://localhost:4015/tasks \
  -d '{
    "title": "Weekly backup",
    "recurrence": "cron:30 2 * * 0"
  }'
```

---

## Cron Syntax Reference

### Format
```
* * * * *
│ │ │ │ │
│ │ │ │ └─── Day of week (0-6, Sunday=0)
│ │ │ └───── Month (1-12)
│ │ └─────── Day of month (1-31)
│ └───────── Hour (0-23)
└─────────── Minute (0-59)
```

### Special Characters

| Character | Meaning | Example |
|-----------|---------|---------|
| `*` | Any value | `* * * * *` = every minute |
| `,` | List | `0 9,17 * * *` = 9am and 5pm |
| `-` | Range | `0 9-17 * * *` = 9am to 5pm |
| `/` | Step | `*/5 * * * *` = every 5 minutes |

### Common Patterns

| Expression | Description |
|------------|-------------|
| `*/5 * * * *` | Every 5 minutes |
| `0 * * * *` | Every hour |
| `0 */2 * * *` | Every 2 hours |
| `0 9 * * *` | Every day at 9am |
| `0 9 * * 1-5` | Weekdays at 9am |
| `0 0 1 * *` | First of month |
| `0 0 * * 0` | Every Sunday |

---

## Backward Compatibility

### Existing Tasks Continue Working

All existing tasks with `daily`, `weekly`, `monthly` will work exactly as before:

```sql
SELECT id, recurrence, next_run FROM tasks WHERE type = 'recurring';
```

**Output:**
```
task_abc | daily          | 2025-11-08 09:00:00  ✅ Still works
task_def | weekly         | 2025-11-14 09:00:00  ✅ Still works
task_ghi | cron:*/5 * * * * | 2025-11-07 10:05:00  ✅ NEW!
```

### No Breaking Changes

- Existing API calls work unchanged
- Old recurrence patterns validated as before
- Database constraint removed (more permissive, not restrictive)
- calculateNextRun() handles both old and new formats

---

## Error Handling

### Invalid Cron Expression

```bash
curl -X POST http://localhost:4015/tasks \
  -d '{"title":"Bad","recurrence":"cron:invalid syntax"}'
```

**Response:**
```json
{
  "error": "invalid recurrence: invalid cron expression 'invalid syntax': expected 5 fields, got 2"
}
```

### Invalid Pattern

```bash
curl -X POST http://localhost:4015/tasks \
  -d '{"title":"Bad","recurrence":"every-5-minutes"}'
```

**Response:**
```json
{
  "error": "invalid recurrence pattern 'every-5-minutes'. Must be 'daily', 'weekly', 'monthly', or 'cron:<expression>'"
}
```

### Missing Cron Prefix

```bash
curl -X POST http://localhost:4015/tasks \
  -d '{"title":"Bad","recurrence":"*/5 * * * *"}'
```

**Response:**
```json
{
  "error": "invalid recurrence pattern '*/5 * * * *'. Must be 'daily', 'weekly', 'monthly', or 'cron:<expression>'"
}
```

**Fix:** Add `cron:` prefix → `"cron:*/5 * * * *"`

---

## Testing Strategy

### Unit Tests

**tools/cron_validator_test.go:**
```go
func TestValidateRecurrence(t *testing.T) {
    tests := []struct {
        input    string
        wantErr  bool
    }{
        {"daily", false},
        {"weekly", false},
        {"monthly", false},
        {"cron:*/5 * * * *", false},
        {"cron:0 9 * * 1-5", false},
        {"cron:invalid", true},
        {"invalid", true},
        {"*/5 * * * *", true}, // Missing cron: prefix
    }

    for _, tt := range tests {
        err := ValidateRecurrence(tt.input)
        if (err != nil) != tt.wantErr {
            t.Errorf("ValidateRecurrence(%q) error = %v, wantErr %v",
                tt.input, err, tt.wantErr)
        }
    }
}
```

### Integration Tests

**Test script: test-cron-tasks.sh**
```bash
#!/bin/bash

echo "1. Create task with cron expression"
TASK_ID=$(curl -s -X POST http://localhost:4015/tasks \
  -d '{"title":"5min","recurrence":"cron:*/5 * * * *"}' | jq -r '.task_id')

echo "2. Verify next_run calculated correctly"
curl -s http://localhost:4015/tasks/$TASK_ID | jq '.next_run'

echo "3. Wait for execution..."
sleep 300

echo "4. Verify task executed"
curl -s http://localhost:4015/tasks/$TASK_ID | jq '.executed_at'

echo "5. Verify new task created"
curl -s http://localhost:4015/tasks?type=recurring | jq '.tasks | length'
```

---

## Migration Guide

### For Existing Deployments

**Step 1: Backup database**
```bash
sqlite3 app.db ".backup backup-before-cron.db"
```

**Step 2: Run migration**
```bash
POSTGRES_USER=admin POSTGRES_PASSWORD=pass node scripts/execute-sql.js \
  migrations/003_cron_support.sql
```

**Step 3: Update binary**
```bash
go build -o server main.go
./server
```

**Step 4: Test**
```bash
# Create cron task
curl -X POST http://localhost:4015/tasks \
  -d '{"title":"Test","recurrence":"cron:*/5 * * * *"}'

# Verify existing tasks still work
curl http://localhost:4015/tasks?type=recurring
```

---

## Performance Considerations

### Cron Parsing Performance

**robfig/cron benchmarks:**
- Parse: ~1-2 microseconds
- Next calculation: ~500 nanoseconds

**Impact:** Negligible - parsing happens once at task creation.

### Scheduler Impact

No additional overhead - scheduler still checks tasks the same way:
```sql
SELECT * FROM tasks
WHERE status = 'pending'
  AND execute_at <= datetime('now')
```

The cron library only calculates `next_run` when task completes.

---

## Security Considerations

### Input Validation

✅ **All recurrence patterns validated** before database insertion
✅ **SQL injection prevented** - using parameterized queries
✅ **DoS prevention** - invalid cron rejected immediately
✅ **No arbitrary code execution** - cron parser only does time math

### Rate Limiting

**Recommendation:** Add minimum interval check:

```go
func ValidateRecurrence(recurrence string) error {
    // ... existing validation ...

    if IsCronExpression(recurrence) {
        cronExpr := ParseCronExpression(recurrence)
        schedule, err := parser.Parse(cronExpr)
        if err != nil {
            return err
        }

        // Check minimum interval (prevent abuse)
        now := time.Now()
        next1 := schedule.Next(now)
        next2 := schedule.Next(next1)
        interval := next2.Sub(next1)

        if interval < 1*time.Minute {
            return fmt.Errorf("minimum interval is 1 minute (got %v)", interval)
        }
    }

    return nil
}
```

---

## Documentation Updates

### API Reference

Add to **docs/TASKS.md**:

```markdown
## Recurrence Patterns

### Simple Patterns
- `daily` - Every 24 hours
- `weekly` - Every 7 days
- `monthly` - Every 30 days

### Cron Expressions
Use `cron:` prefix with standard cron syntax:

**Format:** `cron:* * * * *`

**Examples:**
- `cron:*/5 * * * *` - Every 5 minutes
- `cron:0 */2 * * *` - Every 2 hours
- `cron:0 9 * * 1-5` - Weekdays at 9am
- `cron:0 0 1 * *` - First of month at midnight
```

### User Guide

Add to **TASK-SCHEDULING-GUIDE.md**:

```markdown
# Task Scheduling Guide

## Creating Recurring Tasks

### Simple Patterns (Quick)
Use for common intervals:
- Daily reports: `"recurrence": "daily"`
- Weekly backups: `"recurrence": "weekly"`

### Cron Expressions (Advanced)
Use for custom schedules:
- Every 5 minutes: `"recurrence": "cron:*/5 * * * *"`
- Business hours: `"recurrence": "cron:0 9-17 * * 1-5"`
```

---

## Rollout Plan

### Phase 1: Internal Testing (Week 1)
- Implement on development branch
- Internal testing with various cron patterns
- Performance benchmarking

### Phase 2: Beta Release (Week 2)
- Deploy to staging environment
- Invite beta testers
- Monitor for issues

### Phase 3: Production Release (Week 3)
- Deploy to production
- Monitor error rates
- Gather user feedback

### Phase 4: Documentation & Support (Week 4)
- Complete documentation
- Create tutorial videos
- Update API examples

---

## Success Metrics

### Technical Metrics
- ✅ Zero breaking changes to existing tasks
- ✅ Cron validation error rate < 5%
- ✅ Task execution accuracy within 30 seconds
- ✅ No performance degradation

### User Metrics
- 🎯 50%+ of new recurring tasks use cron expressions
- 🎯 Reduction in external cron job usage
- 🎯 Positive user feedback
- 🎯 Feature adoption within 30 days

---

## Risks & Mitigations

### Risk 1: Breaking Existing Tasks
**Likelihood:** Low
**Impact:** High
**Mitigation:** Comprehensive backward compatibility testing

### Risk 2: Invalid Cron Syntax
**Likelihood:** Medium
**Impact:** Low
**Mitigation:** Robust validation, clear error messages

### Risk 3: Scheduler Overload
**Likelihood:** Low
**Impact:** Medium
**Mitigation:** Rate limiting, minimum interval enforcement

### Risk 4: Timezone Confusion
**Likelihood:** Medium
**Impact:** Medium
**Mitigation:** Document timezone behavior, use UTC consistently

---

## Future Enhancements

### Phase 2 Features (Future)
1. **Named timezones**
   ```json
   {"recurrence": "cron:0 9 * * *", "timezone": "America/New_York"}
   ```

2. **Exclusion dates**
   ```json
   {"recurrence": "daily", "exclude_dates": ["2025-12-25", "2026-01-01"]}
   ```

3. **Retry policies**
   ```json
   {"recurrence": "daily", "retry_on_failure": true, "max_retries": 3}
   ```

4. **Notification preferences**
   ```json
   {"recurrence": "daily", "notify_on_success": false, "notify_on_failure": true}
   ```

---

## Questions & Answers

### Q: Will this break existing tasks?
**A:** No. All existing `daily`, `weekly`, `monthly` tasks continue working exactly as before.

### Q: What if someone enters an invalid cron expression?
**A:** The task creation will fail with a clear error message explaining the issue.

### Q: Can I use seconds in cron expressions?
**A:** No. Standard cron (5 fields) only supports minute-level precision. This is intentional to prevent abuse.

### Q: What timezone are cron expressions evaluated in?
**A:** UTC. All times in the system use UTC. Future enhancement may add timezone support.

### Q: Can I mix simple and cron patterns?
**A:** Yes! Each task can use either simple (`daily`) or cron (`cron:*/5 * * * *`) independently.

### Q: What's the minimum interval allowed?
**A:** Recommendation is 1 minute, but configurable. Too frequent intervals may overload the system.

---

## Conclusion

This proposal adds powerful cron expression support to the task system while maintaining full backward compatibility. Implementation is straightforward, risk is low, and user value is high.

**Recommendation:** ✅ **APPROVE and implement**

**Timeline:** 1-2 weeks from approval to production

**Effort:** ~8-12 hours development + testing

**Dependencies:**
- `github.com/robfig/cron/v3` (proven, reliable library)
- Database migration (non-breaking)
- Documentation updates

**Next Steps:**
1. Review and approve proposal
2. Create implementation branch
3. Add cron library dependency
4. Implement validation + calculation logic
5. Write comprehensive tests
6. Deploy to staging
7. Production rollout

---

**END OF PROPOSAL**
