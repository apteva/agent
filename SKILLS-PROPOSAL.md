# Skills System Proposal

## Overview

Skills are **instruction sets that reference existing MCP tools by name**. They follow the same pattern as MCP tools configuration - simple string references in agent config.

```
┌─────────────────────────────────────────────────────────┐
│                     MCP Server                          │
│  ┌─────────────────┐    ┌─────────────────────────┐    │
│  │   mcp_tools     │◄───│       skills            │    │
│  │  (already have) │    │  (references tools by   │    │
│  │                 │    │   name, adds instructions)│   │
│  └─────────────────┘    └─────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
              ┌─────────────────────┐
              │       Agent         │
              │  config.skills:     │
              │  ["slack-workflows",│
              │   "jira-management"]│
              └─────────────────────┘
```

---

## Database Schema (Single Table)

```sql
CREATE TABLE skills (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,        -- "slack-workflows"
    display_name VARCHAR(200) NOT NULL,       -- "Slack Workflows"
    description TEXT NOT NULL,
    version VARCHAR(20) DEFAULT '1.0.0',

    -- Core content
    instructions TEXT NOT NULL,               -- Skill instructions/guidelines

    -- References to existing MCP tools (by name)
    tools TEXT[] DEFAULT '{}',                -- ["slack-send-message", "slack-list-channels"]

    -- Claude-specific (optional, for native skills API)
    claude_native JSONB,                      -- Native Claude skill format if different

    -- Metadata
    category VARCHAR(50),                     -- "integration", "workflow", "domain"
    tags TEXT[],
    icon VARCHAR(10),                         -- Emoji

    -- Multi-tenant (optional)
    organization_id INTEGER,                  -- NULL = global/public

    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_skills_name ON skills(name);
CREATE INDEX idx_skills_org ON skills(organization_id);
```

---

## Example Skill Record

```json
{
  "id": 1,
  "name": "slack-workflows",
  "display_name": "Slack Workflows",
  "description": "Send messages and manage Slack channels",
  "version": "1.0.0",

  "instructions": "## Slack Workflows\n\nWhen working with Slack:\n\n1. Always confirm the channel before sending\n2. Use Slack markdown: *bold*, `code`, >quote\n3. For urgent messages, prefix with 🚨\n4. Check channel exists before posting\n\n### Message Guidelines\n- Status updates: Start with 📊\n- Alerts: Start with 🚨\n- Success: Start with ✅",

  "tools": ["slack-send-message", "slack-list-channels", "slack-get-user"],

  "claude_native": {
    "name": "slack-workflows",
    "description": "Send messages and manage Slack channels",
    "instructions": "...(same as above)..."
  },

  "category": "integration",
  "tags": ["slack", "messaging"],
  "icon": "💬",
  "organization_id": null,
  "is_active": true
}
```

---

## Agent Config (Like MCP Tools)

```json
{
  "agent": {
    "llm": {
      "tools": ["get_time", "send_notification"],
      "provider": "anthropic",
      "model": "claude-sonnet-4"
    },
    "mcp": {
      "enabled": true,
      "base_url": "http://localhost:3000/mcp",
      "tools": ["slack-send-message", "stripe-list-invoices"]
    },
    "skills": {
      "enabled": true,
      "names": ["slack-workflows", "jira-management", "code-review"]
    }
  }
}
```

---

## MCP Server API (2 New Functions)

```
functions/skills/
├── skill-list/index.js      GET  /mcp/skills?names=slack-workflows,jira-management
└── skill-upsert/index.js    POST /mcp/skills  (create/update)
```

### GET /mcp/skills

**Request:**
```
GET /mcp/skills?names=slack-workflows,jira-management
```

**Response:**
```json
{
  "success": true,
  "skills": [
    {
      "name": "slack-workflows",
      "display_name": "Slack Workflows",
      "description": "Send messages and manage Slack channels",
      "instructions": "## Slack Workflows\n\nWhen working with Slack...",
      "tools": ["slack-send-message", "slack-list-channels"],
      "claude_native": { ... }
    },
    {
      "name": "jira-management",
      "display_name": "Jira Management",
      "description": "Create and manage Jira tickets",
      "instructions": "## Jira Management\n\nWhen creating tickets...",
      "tools": ["jira-create-ticket", "jira-update-status"],
      "claude_native": { ... }
    }
  ]
}
```

---

## Agent Integration

### 1. Config Structure

```go
// config/config.go

type SkillsConfig struct {
    Enabled bool     `json:"enabled"`
    Names   []string `json:"names,omitempty"`  // ["slack-workflows", "jira-management"]
}

type AgentConfig struct {
    // ... existing fields
    Skills *SkillsConfig `json:"skills,omitempty"`
}
```

### 2. Skill Fetching (in MCP client)

```go
// mcp/skills.go

type Skill struct {
    Name         string                 `json:"name"`
    DisplayName  string                 `json:"display_name"`
    Description  string                 `json:"description"`
    Instructions string                 `json:"instructions"`
    Tools        []string               `json:"tools"`
    ClaudeNative map[string]interface{} `json:"claude_native,omitempty"`
}

func (c *MCPClient) FetchSkills(names []string) ([]*Skill, error) {
    // GET /mcp/skills?names=skill1,skill2
    resp, err := c.httpGet("/skills?names=" + strings.Join(names, ","))
    if err != nil {
        return nil, err
    }
    return resp.Skills, nil
}
```

### 3. Skill Injection (Provider-Aware)

```go
// mcp/skills.go

func InjectSkills(
    provider string,
    systemPrompt *string,
    request interface{},
    skills []*Skill,
) {
    if provider == "anthropic" && supportsNativeSkills(request) {
        // Claude: Use native skills API
        injectClaudeNativeSkills(request, skills)
    } else {
        // OpenAI/Gemini/Others: Inject into system prompt
        *systemPrompt += buildSkillsPromptSection(skills)
    }
}

func buildSkillsPromptSection(skills []*Skill) string {
    var sb strings.Builder
    sb.WriteString("\n\n## Active Skills\n")

    for _, skill := range skills {
        sb.WriteString(fmt.Sprintf("\n### %s\n", skill.DisplayName))
        sb.WriteString(skill.Instructions)
        sb.WriteString("\n")
    }

    return sb.String()
}

func injectClaudeNativeSkills(request *anthropic.Request, skills []*Skill) {
    for _, skill := range skills {
        if skill.ClaudeNative != nil {
            request.Skills = append(request.Skills, skill.ClaudeNative)
        }
    }
}
```

### 4. Auto-Enable Skill Tools

```go
// config/config.go - in GetLLMConfig() or similar

func (c *AgentConfig) ResolveSkillTools(skills []*Skill) []string {
    toolSet := make(map[string]bool)

    for _, skill := range skills {
        for _, tool := range skill.Tools {
            toolSet[tool] = true
        }
    }

    tools := make([]string, 0, len(toolSet))
    for tool := range toolSet {
        tools = append(tools, tool)
    }
    return tools
}
```

---

## Integration Flow

```
1. Agent starts
   │
2. Load config: skills.names = ["slack-workflows", "jira-management"]
   │
3. Fetch skills from MCP: GET /mcp/skills?names=slack-workflows,jira-management
   │
4. Cache skills in memory
   │
5. On chat request:
   │
   ├─► Claude provider?
   │   └─► Inject native skills + auto-enable referenced MCP tools
   │
   └─► Other provider?
       └─► Inject instructions into system prompt + auto-enable tools
```

---

## Provider-Specific Handling

### Claude (Native Skills Support)

When using Claude API, skills can be passed natively:

```json
{
  "model": "claude-sonnet-4-20250514",
  "messages": [...],
  "skills": [
    {
      "name": "slack-workflows",
      "description": "Send messages and manage Slack channels",
      "instructions": "## Slack Workflows\n\nWhen working with Slack..."
    }
  ],
  "tools": [
    { "name": "slack-send-message", ... }
  ]
}
```

### OpenAI / Gemini / Others (System Prompt Injection)

For providers without native skills support, inject into system prompt:

```
[Original System Prompt]

## Active Skills

### Slack Workflows
When working with Slack:
1. Always confirm the channel before sending
2. Use Slack markdown: *bold*, `code`, >quote
...

### Jira Management
When creating tickets:
1. Always include a clear summary
...
```

---

## Summary

| Aspect | Approach |
|--------|----------|
| **Storage** | Single `skills` table on MCP server |
| **Tools** | Reference existing MCP tools by name |
| **Config** | `skills.names: ["skill1", "skill2"]` (like MCP tools) |
| **Assignment** | In agent config, not backend |
| **Claude** | Use native skills API when available |
| **Others** | Inject instructions into system prompt |
| **API** | Just 2 endpoints: list + upsert |

---

## Implementation Effort

| Task | Effort |
|------|--------|
| Database migration | 30 min |
| `skill-list` function | 1 hour |
| `skill-upsert` function | 1 hour |
| Agent config + fetching | 2 hours |
| Provider-aware injection | 2 hours |
| **Total** | ~1 day |

---

## Future Enhancements

- Skill versioning and rollback
- Skill marketplace / sharing between organizations
- Skill analytics (usage tracking)
- Skill dependencies (skill A requires skill B)
- Skill testing framework
