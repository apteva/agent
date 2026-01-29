# Simple Selective Memory Proposal v2

## Problem
Agent currently shares all memories across all users - no privacy boundaries.

## Solution: Namespace-Based Memory Scoping

Add `namespace` field for flexible, hierarchical access control:

```go
type Memory struct {
    ID           string
    Content      string
    Namespace    string   // NEW: "shared", "context:user123", "context:org456:team789"
    ThreadID     string
    // ... rest of fields
}
```

## Namespace Format

Uses colon-separated hierarchical paths:

- **`shared`** - Public knowledge (facts, general info)
- **`context:{id}`** - Private to any context (user, org, session, etc.)
- **`context:{org}:{team}`** - Hierarchical contexts
- **`context:{session}`** - Temporary session memory

## Examples

```
shared                    → Everyone can access
context:user123          → Only user123
context:org456           → Everyone in org456
context:org456:team789   → Only team789 in org456
context:session:abc      → Only this session
```

## Implementation

### 1. Store with namespace
```go
func (m *MemoryManager) Store(content string, namespace string, threadID string) {
    memory := Memory{
        Content:   content,
        Namespace: namespace,
        ThreadID:  threadID,
    }
    // ... save
}
```

### 2. Retrieve with namespace matching
```go
func (m *MemoryManager) RetrieveRelevant(query string, allowedNamespaces []string) []*Memory {
    results := m.collection.Query(query, 10)

    filtered := []Memory{}
    for _, mem := range results {
        if canAccess(mem.Namespace, allowedNamespaces) {
            filtered = append(filtered, mem)
        }
    }
    return filtered
}

func canAccess(memNamespace string, allowed []string) bool {
    for _, ns := range allowed {
        if memNamespace == ns || memNamespace == "shared" {
            return true
        }
    }
    return false
}
```

### 3. API changes
```bash
POST /chat
{
  "message": "...",
  "context": ["user123", "org456"],  # NEW: flexible contexts
  "thread_id": "thread456"
}
```

## Use Cases

- **Single user**: `context:user123`
- **Multi-tenant SaaS**: `context:tenant456`, `context:tenant456:workspace789`
- **Organizations**: `context:org123`, `context:org123:team456`
- **Sessions**: `context:session:abc123`
- **Anonymous**: `shared` only

## Benefits
- ✅ Generic - works for any context (user, org, session, custom)
- ✅ Hierarchical - supports nested scopes
- ✅ Simple - just string matching
- ✅ Extensible - add new context types without code changes
- ✅ No breaking changes - defaults to "shared"

**Implementation time: ~30 minutes**
