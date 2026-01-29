# Proposal: Admin API Key Authentication

## Problem

All agent endpoints are publicly accessible - anyone with the URL can modify config, prompts, delete memories, etc.

## Solution

Add an `admin_api_key` that's required for sensitive operations.

## Flow

1. **Agent created** → Admin key auto-generated (e.g., `agk_` + random string)
2. **Stored in config** → `"admin_api_key": "agk_abc123..."`
3. **Config changes** → Must provide `X-Admin-Key: agk_abc123...` header
4. **Chat/public endpoints** → No key needed

```
┌─────────────────┐     ┌─────────────────┐
│  Create Agent   │────▶│  admin_api_key  │
│                 │     │  = agk_xyz...   │
└─────────────────┘     └────────┬────────┘
                                 │
                                 ▼
┌─────────────────┐     ┌─────────────────┐
│ PATCH /config   │────▶│ Check header    │
│ X-Admin-Key: ?  │     │ matches config  │
└─────────────────┘     └─────────────────┘
                                 │
                        ┌────────┴────────┐
                        ▼                 ▼
                    ✅ 200 OK         ❌ 401 Unauthorized
```

## Implementation

### 1. Add to config

```go
type AgentConfig struct {
    // ... existing fields
    AdminAPIKey string `json:"admin_api_key,omitempty"` // Required for config changes
}
```

### 2. Auto-generate on agent creation

```go
func generateAdminKey() string {
    b := make([]byte, 24)
    rand.Read(b)
    return "agk_" + base64.URLEncoding.EncodeToString(b)
}
```

### 3. Middleware for protected endpoints

```go
func AdminAuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        configKey := agentConfig.AdminAPIKey
        if configKey == "" {
            // No key configured = no protection (backwards compatible)
            next.ServeHTTP(w, r)
            return
        }

        providedKey := r.Header.Get("X-Admin-Key")
        if providedKey != configKey {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### 4. Protected endpoints (require admin key)

- `PATCH /config` - Modify agent config
- `DELETE /memory/*` - Delete memories
- `POST /mcp/resources/sync` - Trigger sync
- `DELETE /threads/*` - Delete threads
- Any other mutating admin operations

### 5. Public endpoints (no auth needed)

- `GET /config` - Read config (redact admin_api_key in response)
- `GET /health` - Health check
- `POST /chat` - Chat (main function)
- `GET /threads` - List threads
- `POST /threads` - Create thread
- `POST /messages` - Send message

### 6. Redact key from GET /config response

```go
func (h *ConfigHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
    config := *currentConfig
    config.AdminAPIKey = "" // Redact from response
    json.NewEncoder(w).Encode(config)
}
```

### 7. Environment variable fallback

```go
func getAdminKey() string {
    if agentConfig.AdminAPIKey != "" {
        return agentConfig.AdminAPIKey
    }
    return os.Getenv("AGENT_ADMIN_KEY")
}
```

## Alternatives Considered

| Approach | Pros | Cons |
|----------|------|------|
| Admin API Key | Simple, stateless | Need to manage/distribute key |
| Separate admin port | Strong isolation | More complex deployment |
| IP whitelist | No secrets needed | Hard to manage in dynamic envs |
| JWT/OAuth | Standard, flexible | Overkill for single agent |

## Optional Enhancements

- Rate limiting on failed auth attempts
- Audit logging for admin operations
- Key rotation endpoint
- Multiple admin keys with different permissions

## Status

- [ ] Implementation pending
- [ ] Testing
- [ ] Documentation
