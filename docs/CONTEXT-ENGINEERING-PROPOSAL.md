# Context Engineering Improvements

Based on Google's 70-page whitepaper on Context Engineering (Nov 2025).

## Source

- [Google Whitepaper PDF](https://drive.google.com/file/d/1JW6Q_wwvBjMz9xzOtTldFfPiF7BrdEeQ/view)
- Summary: "Context Engineering is assembling exactly the right information at exactly the right time"

## Current Agent State vs. Google's Framework

| Google Concept | We Have | Gap |
|----------------|---------|-----|
| Sessions | ✅ Threads | - |
| Memory Storage | ✅ Vector embeddings | - |
| Memory Retrieval | ✅ Semantic search | Could improve |
| File Ingestion | ✅ RAG chunking | - |
| Memory Types | ❌ Single type | Need declarative/procedural |
| Provenance | ⚠️ Basic source | Need confidence scoring |
| Push/Pull Retrieval | ❌ All reactive | Need proactive memories |
| LLM Memory Extraction | ❌ Manual only | Need auto-extraction |
| Memory Consolidation | ❌ None | Need deduplication |

---

## Proposed Improvements

### 1. Memory Categories (High Impact)

Add `category` field with types:

```go
type MemoryCategory string

const (
    CategoryDeclarative MemoryCategory = "declarative" // Facts/preferences
    CategoryProcedural  MemoryCategory = "procedural"  // Work patterns
    CategoryEpisodic    MemoryCategory = "episodic"    // Events/history
    CategoryDocument    MemoryCategory = "document"    // RAG chunks
)
```

**Examples:**
- `declarative`: "I'm vegan", "Use TypeScript", "Timezone is EST"
- `procedural`: "Debug by checking logs first", "Prefer code before explanation"
- `episodic`: "Deployed v2.0 on Dec 1", "Had issue with auth last week"
- `document`: Current RAG file chunks

**Benefits:**
- Different retrieval strategies per category
- Procedural memories inform HOW to respond
- Declarative memories inform WHAT to include

---

### 2. Confidence Scoring (High Impact)

Track memory reliability with dynamic scoring:

```go
type Memory struct {
    // ... existing fields
    Confidence    float64   `json:"confidence"`     // 0.0 - 1.0
    MentionCount  int       `json:"mention_count"`  // Times reinforced
    LastReinforced time.Time `json:"last_reinforced"`
}
```

**Rules:**
- Initial confidence: 0.5 (single mention)
- +0.1 for each reinforcement (cap at 1.0)
- -0.05 decay per week if not reinforced
- Filter out memories below 0.3 confidence

**Example:**
```
"User is vegan" - confidence: 0.9 (mentioned 5+ times)
"User likes jazz" - confidence: 0.4 (mentioned once, might be contextual)
```

---

### 3. Proactive vs Reactive Retrieval (High Impact)

**Proactive (Push):** Always included in context
- User name/identity
- Core preferences (language, timezone)
- Active project context
- Safety info (allergies, restrictions)

**Reactive (Pull):** Retrieved via semantic search
- Historical patterns
- Past project details
- Procedural knowledge (only when task-relevant)

```go
type Memory struct {
    // ... existing fields
    Proactive bool `json:"proactive"` // Always include in context
}
```

**Implementation:**
```go
func (m *MemoryManager) GetContextMemories(query string, threadID string) ([]*Memory, error) {
    // 1. Get all proactive memories (always included)
    proactive := m.GetProactiveMemories()

    // 2. Semantic search for reactive memories
    reactive := m.SearchSimilar(query, limit - len(proactive))

    // 3. Combine and deduplicate
    return append(proactive, reactive...), nil
}
```

---

### 4. LLM-Driven Memory Extraction (Medium Impact)

After each conversation turn, automatically extract memories:

```go
func (m *MemoryManager) ExtractMemoriesFromConversation(messages []Message) error {
    prompt := `Analyze this conversation and extract any information worth remembering about the user.

Categories:
- declarative: Facts, preferences, constraints (e.g., "prefers Python", "works at startup")
- procedural: Work patterns, decision styles (e.g., "likes seeing code first", "decides with pros/cons")
- episodic: Notable events (e.g., "deployed new feature", "had outage last week")

Return JSON array of memories to store. Only extract genuinely useful information.
Skip generic or one-time contextual statements.`

    // Call LLM with conversation
    // Parse response
    // Store extracted memories
}
```

**Trigger:** Run after assistant response, async/background

---

### 5. Memory Consolidation (Medium Impact)

Periodically merge and deduplicate memories:

```go
func (m *MemoryManager) ConsolidateMemories() error {
    // 1. Find semantically similar memories (cosine > 0.9)
    // 2. Merge into single memory with combined confidence
    // 3. Handle conflicts via timestamp precedence
    // 4. Delete redundant entries
}
```

**Conflict Resolution:**
- Newer memory takes precedence
- Keep provenance trail of merged sources
- Flag conflicts for potential user review

**Example:**
```
Before:
  - "User prefers Python" (confidence: 0.6, 2024-01-15)
  - "User uses Python for scripts" (confidence: 0.5, 2024-02-20)

After consolidation:
  - "User prefers Python for scripting" (confidence: 0.8, merged from 2 sources)
```

---

### 6. Memory Decay/Expiration (Low Impact)

Implement automatic memory lifecycle:

```go
func (m *MemoryManager) DecayMemories() {
    // Run weekly
    for _, memory := range memories {
        daysSinceAccess := time.Since(memory.LastAccessed).Hours() / 24

        if daysSinceAccess > 30 {
            memory.Confidence -= 0.05
        }
        if daysSinceAccess > 90 {
            memory.Confidence -= 0.1
        }

        // Archive if confidence drops too low
        if memory.Confidence < 0.2 {
            m.ArchiveMemory(memory.ID)
        }
    }
}
```

---

## Implementation Priority

| # | Feature | Impact | Effort | Priority |
|---|---------|--------|--------|----------|
| 1 | Memory Categories | High | Medium | P0 |
| 2 | Confidence Scoring | High | Low | P0 |
| 3 | Proactive Retrieval | High | Medium | P0 |
| 4 | LLM Memory Extraction | Medium | High | P1 |
| 5 | Memory Consolidation | Medium | Medium | P1 |
| 6 | Memory Decay | Low | Low | P2 |

---

## Database Schema Changes

```sql
ALTER TABLE memories ADD COLUMN category TEXT DEFAULT 'document';
ALTER TABLE memories ADD COLUMN confidence REAL DEFAULT 0.5;
ALTER TABLE memories ADD COLUMN proactive BOOLEAN DEFAULT FALSE;
ALTER TABLE memories ADD COLUMN mention_count INTEGER DEFAULT 1;
ALTER TABLE memories ADD COLUMN last_reinforced TIMESTAMP;

CREATE INDEX idx_memories_proactive ON memories(proactive) WHERE proactive = TRUE;
CREATE INDEX idx_memories_category ON memories(category);
CREATE INDEX idx_memories_confidence ON memories(confidence);
```

---

## API Changes

### GET /memories

Add filters:
```
GET /memories?category=declarative&proactive=true&min_confidence=0.5
```

### POST /memories

Add fields:
```json
{
  "content": "User prefers TypeScript",
  "category": "declarative",
  "proactive": true,
  "confidence": 0.7
}
```

---

## References

- Google Context Engineering Whitepaper (Nov 2025)
- Key insight: "AI products are stateful. They learn. Remember. Get better. Magic compounds."
- Session = one task, one workbench
- Memory persists beyond sessions
- Proactive = must-haves, Reactive = semantic search
