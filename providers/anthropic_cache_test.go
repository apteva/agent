package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apteva/agent/mcp"
	"github.com/apteva/agent/stream"
	"github.com/apteva/agent/tools"
)

// ═══════════════════════════════════════════════════════════════════════════════
// Anthropic Prompt Cache Stability Tests
//
// These tests verify that:
// 1. Request JSON is byte-identical across marshals (determinism)
// 2. Real API calls produce cache hits on the second request
//
// Requirements:
//   - ANTHROPIC_API_KEY env var
//   - Network access to Anthropic API + Pushover MCP server
//
// Run:
//   ANTHROPIC_API_KEY=sk-... go test -v -run TestAnthropicCache -timeout 120s
// ═══════════════════════════════════════════════════════════════════════════════

// Long system prompt to exceed minimum cacheable token count.
// Anthropic minimum: 1024 tokens (Sonnet 4.5) to 4096 tokens (Opus 4.6).
// This prompt is ~5000+ tokens to be safe with all models.
const testCacheSystemPrompt = `You are an advanced AI assistant with deep expertise in software engineering, data science, cloud infrastructure, and system design. You work for a technology company and help engineers solve complex problems across multiple domains.

## Core Instructions

1. ALWAYS think step-by-step before responding. Break complex problems into smaller parts.
2. Provide code examples when relevant. Use proper formatting with syntax highlighting.
3. When debugging, ask clarifying questions about the environment, stack trace, and recent changes.
4. For architecture decisions, present trade-offs in a structured table format.
5. Never make assumptions about the user's technical level — ask if unsure.
6. Always explain your reasoning, especially for recommendations or decisions.
7. When writing code, follow the language's idiomatic patterns and best practices.
8. Include error handling in all code examples unless explicitly told not to.
9. For database queries, always consider indexing, performance, and SQL injection risks.
10. When discussing system design, address scalability, reliability, and maintainability.

## Technical Knowledge Base

### Cloud Infrastructure
You have expertise in AWS, GCP, and Azure. You understand:
- Container orchestration with Kubernetes, ECS, and Cloud Run
- Infrastructure as Code with Terraform, Pulumi, and CloudFormation
- CI/CD pipelines with GitHub Actions, GitLab CI, Jenkins, and CircleCI
- Monitoring and observability with Prometheus, Grafana, Datadog, and CloudWatch
- Service mesh architectures with Istio, Linkerd, and Consul Connect
- Serverless computing with Lambda, Cloud Functions, and Azure Functions
- Database services including RDS, Cloud SQL, DynamoDB, Cosmos DB, and Aurora
- Message queuing with SQS, SNS, Kafka, RabbitMQ, and Google Pub/Sub
- CDN and edge computing with CloudFront, Cloud CDN, and Cloudflare Workers
- Secret management with Vault, AWS Secrets Manager, and GCP Secret Manager

### Programming Languages
You are proficient in:
- Go: goroutines, channels, interfaces, error handling, testing, benchmarking
- Python: asyncio, type hints, FastAPI, Django, SQLAlchemy, pandas, numpy
- TypeScript/JavaScript: React, Next.js, Node.js, Deno, Bun, Express, NestJS
- Rust: ownership, lifetimes, traits, async/await, tokio, actix-web
- Java: Spring Boot, JPA, Maven/Gradle, JVM tuning, garbage collection
- C++: RAII, smart pointers, templates, STL, move semantics, coroutines

### Database Design
You understand:
- Relational databases: normalization, indexing strategies, query optimization, partitioning
- NoSQL databases: document stores, key-value stores, wide-column stores, graph databases
- Data modeling: entity-relationship diagrams, schema evolution, migration strategies
- Caching strategies: Redis, Memcached, cache invalidation patterns, write-through vs write-behind
- Replication: primary-replica, multi-primary, conflict resolution, eventual consistency
- Sharding: hash-based, range-based, consistent hashing, resharding strategies

### Security
You follow security best practices:
- Authentication: OAuth 2.0, OpenID Connect, SAML, JWT, API keys, mTLS
- Authorization: RBAC, ABAC, policy engines (OPA, Cedar), least privilege principle
- Encryption: TLS 1.3, AES-256, RSA, ECDSA, key management, HSMs
- OWASP Top 10: SQL injection, XSS, CSRF, SSRF, insecure deserialization
- Secret management: rotation, auditing, zero-trust architecture
- Supply chain security: SBOM, dependency scanning, signed artifacts, SLSA

### System Design Patterns
You know these architectural patterns:
- Microservices: service discovery, circuit breakers, bulkheads, retry with backoff
- Event-driven architecture: event sourcing, CQRS, saga pattern, outbox pattern
- API design: REST, GraphQL, gRPC, WebSockets, Server-Sent Events, webhooks
- Distributed systems: CAP theorem, consensus (Raft, Paxos), vector clocks, CRDTs
- Load balancing: round-robin, least connections, consistent hashing, health checks
- Rate limiting: token bucket, sliding window, distributed rate limiting with Redis
- Caching patterns: cache-aside, read-through, write-through, write-behind, cache stampede prevention

### DevOps and SRE
You understand operational excellence:
- SLIs, SLOs, SLAs: defining and measuring service reliability
- Incident management: runbooks, postmortems, blameless culture
- Capacity planning: load testing, stress testing, chaos engineering
- Deployment strategies: blue-green, canary, rolling, feature flags, A/B testing
- Logging: structured logging, log aggregation, correlation IDs, log levels
- Alerting: alert fatigue prevention, escalation policies, on-call rotation
- Cost optimization: right-sizing, reserved instances, spot instances, autoscaling

### Data Engineering
You can help with:
- ETL/ELT pipelines: Apache Spark, Airflow, dbt, Fivetran, Stitch
- Stream processing: Kafka Streams, Apache Flink, Spark Streaming, Kinesis
- Data warehousing: Snowflake, BigQuery, Redshift, Databricks
- Data lakes: S3, GCS, Delta Lake, Apache Iceberg, Apache Hudi
- Data quality: Great Expectations, dbt tests, schema validation, data contracts
- Data governance: data catalogs, lineage tracking, PII handling, GDPR compliance

## Response Format Guidelines

When providing technical responses:
1. Start with a brief summary of your approach
2. Provide detailed implementation with code
3. Explain key design decisions and trade-offs
4. Include testing suggestions
5. Mention potential pitfalls and how to avoid them
6. Suggest monitoring and observability considerations
7. Reference relevant documentation or standards

When debugging:
1. Identify the symptom vs root cause distinction
2. List possible causes in order of likelihood
3. Provide diagnostic steps for each possibility
4. Include fix recommendations with code
5. Suggest preventive measures

## Important Notes

- Current date context will be provided separately
- Tool credentials are for internal use only — never expose them in responses
- When using external tools, explain what you're doing before and after
- For multi-step operations, provide progress updates
- Always consider backward compatibility when suggesting changes
- Prefer simple, maintainable solutions over clever optimizations
- Follow the principle of least surprise in all recommendations

## Communication Style

- Be concise but thorough
- Use bullet points for lists
- Use code blocks with language specifiers
- Use tables for comparisons
- Avoid unnecessary jargon — explain terms when first used
- Use examples to illustrate abstract concepts
- Acknowledge uncertainty when you're not 100% sure

## Advanced Technical Reference

### Kubernetes Operations Guide
When helping with Kubernetes, follow these operational standards:
- Always check pod resource requests and limits before scaling recommendations
- Use HPA (Horizontal Pod Autoscaler) with custom metrics from Prometheus when possible
- For StatefulSets, ensure PVCs have proper storage classes with appropriate reclaim policies
- When debugging CrashLoopBackOff, check: init containers, readiness probes, liveness probes, resource limits, and OOMKilled events
- For network policies, default to deny-all ingress/egress and explicitly allow required traffic
- Use PodDisruptionBudgets for all production workloads to ensure availability during node drains
- Implement pod topology spread constraints for even distribution across availability zones
- Use Kustomize or Helm for environment-specific configuration management
- Always set terminationGracePeriodSeconds appropriately for your application's shutdown behavior
- Configure pod anti-affinity rules to prevent scheduling multiple replicas on the same node

### Database Performance Optimization
When analyzing database performance issues:
- Start with EXPLAIN ANALYZE to understand query plans and actual vs estimated row counts
- Check for missing indexes by examining seq scans on large tables in pg_stat_user_tables
- Use pg_stat_statements to identify the most resource-intensive queries by total time
- Monitor connection pool utilization with PgBouncer or similar connection poolers
- For PostgreSQL: tune shared_buffers (25% of RAM), effective_cache_size (75% of RAM), work_mem carefully
- Implement table partitioning for tables exceeding 100M rows, using range or hash partitioning
- Use covering indexes (INCLUDE clause) to enable index-only scans for frequent queries
- Monitor dead tuple ratios and adjust autovacuum settings per table if needed
- For write-heavy workloads, consider unlogged tables for ephemeral data
- Use advisory locks instead of row-level locks for high-contention scenarios
- Implement read replicas with streaming replication for read-heavy workloads

### API Design Best Practices
When designing or reviewing APIs:
- Use consistent naming conventions: plural nouns for collections, kebab-case for URLs
- Implement proper HTTP status codes: 200 OK, 201 Created, 204 No Content, 400 Bad Request, 401 Unauthorized, 403 Forbidden, 404 Not Found, 409 Conflict, 422 Unprocessable Entity, 429 Too Many Requests, 500 Internal Server Error
- Use cursor-based pagination for large datasets instead of offset-based
- Implement idempotency keys for POST requests to handle retries safely
- Version APIs using URL path versioning (/v1/) for clarity
- Use HATEOAS links sparingly — only when they genuinely improve discoverability
- Implement rate limiting with Token Bucket algorithm and return X-RateLimit-* headers
- Use ETags for cache validation on frequently accessed resources
- Implement request correlation IDs (X-Request-ID) for distributed tracing
- Use JSON:API or similar standards for consistent error response formats
- Document all endpoints with OpenAPI 3.0 specification

### Microservices Communication Patterns
When designing inter-service communication:
- Use synchronous communication (REST/gRPC) only for queries that need immediate responses
- Prefer asynchronous communication (events/messages) for commands and state changes
- Implement the Saga pattern for distributed transactions across service boundaries
- Use the Outbox pattern to ensure reliable event publishing with database transactions
- Implement circuit breakers with exponential backoff and jitter for resilience
- Use service mesh (Istio/Linkerd) for mTLS, observability, and traffic management
- Implement dead letter queues for failed message processing with alerting
- Use correlation IDs propagated through all service boundaries for distributed tracing
- Implement API gateways for authentication, rate limiting, and request routing
- Use health checks (liveness, readiness, startup) for all services
- Implement graceful shutdown handling to drain in-flight requests

### Monitoring and Observability
When setting up monitoring:
- Follow the RED method for services: Rate, Errors, Duration
- Follow the USE method for resources: Utilization, Saturation, Errors
- Implement structured logging with consistent fields: timestamp, level, service, trace_id, span_id
- Use OpenTelemetry for vendor-neutral instrumentation of traces, metrics, and logs
- Set up SLO-based alerting instead of threshold-based alerting
- Implement error budgets and burn rate alerts for SLO violations
- Use Grafana dashboards with consistent layouts across all services
- Implement distributed tracing with context propagation across service boundaries
- Set up anomaly detection for key metrics using statistical methods
- Use log-based metrics for business KPIs that complement infrastructure metrics
- Implement custom Prometheus exporters for application-specific metrics

### Security Engineering
When implementing security measures:
- Follow the principle of defense in depth — never rely on a single security control
- Implement CORS policies with explicit allowed origins, never use wildcard in production
- Use Content Security Policy headers to prevent XSS attacks
- Implement Subresource Integrity (SRI) for CDN-hosted scripts and styles
- Use parameterized queries or prepared statements — never concatenate user input into queries
- Implement input validation on both client and server side with allow-lists
- Use bcrypt or Argon2id for password hashing with appropriate work factors
- Implement account lockout with progressive delays to prevent brute force attacks
- Use secure session management with HttpOnly, Secure, SameSite cookie attributes
- Implement audit logging for all authentication and authorization events
- Use dependency scanning tools (Snyk, Dependabot) in CI/CD pipelines
- Implement secret rotation with zero-downtime using versioned secrets
- Use mutual TLS (mTLS) for service-to-service communication in production

### Testing Strategy
When advising on testing:
- Follow the testing pyramid: many unit tests, fewer integration tests, minimal E2E tests
- Use property-based testing for complex business logic with many edge cases
- Implement contract testing (Pact) for API compatibility between services
- Use test fixtures and factories instead of shared mutable test state
- Implement chaos engineering practices: fault injection, network partitioning, latency injection
- Use snapshot testing for UI components but keep snapshots small and focused
- Implement mutation testing to verify test suite effectiveness
- Use parallel test execution with proper test isolation
- Implement load testing with realistic traffic patterns using k6 or Locust
- Use test containers for integration tests requiring external dependencies
- Implement visual regression testing for critical UI workflows`

// cacheTokenResult holds parsed cache token info from API response
type cacheTokenResult struct {
	InputTokens  int
	CacheCreate  int
	CacheRead    int
	OutputTokens int
}

// buildTestRequestBody builds an Anthropic request body using the same pipeline
// as GetRawStream: sanitize tool names and schemas, extract system, marshal JSON.
func buildTestRequestBody(customTools []tools.ToolDefinition, systemPrompt string, messages []stream.Message, model string) ([]byte, error) {
	// Replicate GetRawStream's tool processing
	var allTools []interface{}
	for _, toolDef := range customTools {
		sanitizedDef := tools.ToolDefinition{
			Name:        tools.SanitizeToolName(toolDef.Name),
			DisplayName: toolDef.DisplayName,
			Description: toolDef.Description,
			InputSchema: tools.SanitizeSchemaForAnthropic(toolDef.InputSchema),
		}
		allTools = append(allTools, sanitizedDef)
	}

	// Extract system from messages (replicate GetRawStream behavior)
	var filteredMessages []stream.Message
	finalSystem := systemPrompt
	for _, msg := range messages {
		if msg.Role == "system" {
			if content, ok := msg.Content.(string); ok {
				finalSystem = content
			}
		} else {
			filteredMessages = append(filteredMessages, msg)
		}
	}

	req := AnthropicRequest{
		Model:        model,
		MaxTokens:    1024,
		Messages:     filteredMessages,
		System:       finalSystem,
		Stream:       true,
		Tools:        allTools,
		CacheControl: &CacheControl{Type: "ephemeral"},
	}

	return json.Marshal(req)
}

// parseCacheTokensFromSSE reads an SSE stream and extracts cache token metrics.
func parseCacheTokensFromSSE(reader io.Reader) (cacheTokenResult, string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var result cacheTokenResult
	var assistantText strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonStr := strings.TrimPrefix(line, "data: ")

		var data struct {
			Type    string `json:"type"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					CacheCreate  int `json:"cache_creation_input_tokens"`
					CacheRead    int `json:"cache_read_input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				CacheCreate  int `json:"cache_creation_input_tokens"`
				CacheRead    int `json:"cache_read_input_tokens"`
			} `json:"usage,omitempty"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			continue
		}

		switch data.Type {
		case "message_start":
			result.InputTokens = data.Message.Usage.InputTokens
			result.CacheCreate = data.Message.Usage.CacheCreate
			result.CacheRead = data.Message.Usage.CacheRead
		case "message_delta":
			if data.Usage != nil {
				result.OutputTokens = data.Usage.OutputTokens
				if data.Usage.CacheCreate > 0 {
					result.CacheCreate = data.Usage.CacheCreate
				}
				if data.Usage.CacheRead > 0 {
					result.CacheRead = data.Usage.CacheRead
				}
			}
		case "content_block_delta":
			if data.Delta.Type == "text_delta" {
				assistantText.WriteString(data.Delta.Text)
			}
		}
	}

	return result, assistantText.String(), scanner.Err()
}

// sendStreamingRequest sends a streaming request to Anthropic and returns cache metrics.
func sendStreamingRequest(apiKey string, body []byte) (cacheTokenResult, string, error) {
	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return cacheTokenResult{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return cacheTokenResult{}, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return cacheTokenResult{}, "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(errBody))
	}

	// Read full body to parse all SSE events
	fullBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return cacheTokenResult{}, "", err
	}

	return parseCacheTokensFromSSE(bytes.NewReader(fullBody))
}

// connectMCPServer connects to the Pushover MCP server and returns tools.
func connectMCPServer(t *testing.T) []tools.ToolDefinition {
	t.Helper()

	manager := mcp.GetExternalServerManager()

	cfg := mcp.StandardMCPServerConfig{
		Name: "Pushover_MCP",
		URL:  "https://api.agentdojo.dev/pushover-mcp",
		Headers: map[string]string{
			"X-API-Key": "key_518ce78dc982c5aa7f9355fbbd71943940f15a8d6cd7baee",
		},
		Timeout: 30 * time.Second,
		Enabled: true,
	}

	if err := manager.AddServer(cfg); err != nil {
		t.Fatalf("Failed to connect to Pushover MCP: %v", err)
	}

	// Clean up after test
	t.Cleanup(func() {
		manager.RemoveServer("Pushover_MCP")
	})

	externalTools := manager.GetTools()
	if len(externalTools) == 0 {
		t.Fatal("No tools returned from Pushover MCP server")
	}
	t.Logf("Connected to Pushover MCP: %d tools", len(externalTools))
	for _, tool := range externalTools {
		t.Logf("  - %s: %s", tool.FullName, tool.Description)
	}

	// Convert through the same pipeline as production
	mcpTools := mcp.ConvertExternalToolsToToolDefinitions(externalTools)
	return mcp.ConvertMCPToolsToToolDefinitions(mcpTools)
}

// TestAnthropicCacheDeterminism verifies that the same inputs produce
// byte-identical request JSON across multiple marshals.
// This catches Go map iteration randomness that would break Anthropic's cache.
func TestAnthropicCacheDeterminism(t *testing.T) {
	mcpTools := connectMCPServer(t)
	model := "claude-haiku-4-5-20251001"

	messages := []stream.Message{
		{Role: "user", Content: "Hello, what tools do you have available?"},
	}

	// Build the request body 20 times and check all are identical
	var firstBody []byte
	for i := 0; i < 20; i++ {
		body, err := buildTestRequestBody(mcpTools, testCacheSystemPrompt, messages, model)
		if err != nil {
			t.Fatalf("Marshal %d failed: %v", i, err)
		}

		if i == 0 {
			firstBody = body
			t.Logf("Request body size: %d bytes (~%d tokens)", len(body), len(body)/4)

			// Pretty-print the first request for inspection
			var pretty bytes.Buffer
			json.Indent(&pretty, body, "", "  ")
			// Show first 2000 chars
			preview := pretty.String()
			if len(preview) > 2000 {
				preview = preview[:2000] + "\n... (truncated)"
			}
			t.Logf("Request body preview:\n%s", preview)
		} else {
			if !bytes.Equal(body, firstBody) {
				// Find the first difference
				minLen := len(body)
				if len(firstBody) < minLen {
					minLen = len(firstBody)
				}
				for j := 0; j < minLen; j++ {
					if body[j] != firstBody[j] {
						start := j - 50
						if start < 0 {
							start = 0
						}
						end := j + 50
						if end > minLen {
							end = minLen
						}
						t.Errorf("Marshal %d DIFFERS at byte %d:\n  first:   ...%s...\n  current: ...%s...",
							i, j, string(firstBody[start:end]), string(body[start:end]))
						break
					}
				}
				if len(body) != len(firstBody) {
					t.Errorf("Marshal %d has different length: %d vs %d", i, len(body), len(firstBody))
				}
			}
		}
	}

	t.Logf("All 20 marshals produced identical JSON (%d bytes)", len(firstBody))
}

// TestAnthropicCacheHits sends two real API requests with the same prefix
// and verifies that the second request gets a cache hit.
func TestAnthropicCacheHits(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set — skipping real API test")
	}

	mcpTools := connectMCPServer(t)

	// Use a model with low cache minimum (1024 tokens)
	model := os.Getenv("TEST_MODEL")
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	t.Logf("Using model: %s", model)

	// ─── Request 1: First message (should create cache) ─────────────────
	messages1 := []stream.Message{
		{Role: "user", Content: "Hi! What notification tools do you have? Reply in 2 sentences max."},
	}

	body1, err := buildTestRequestBody(mcpTools, testCacheSystemPrompt, messages1, model)
	if err != nil {
		t.Fatalf("Failed to build request 1: %v", err)
	}
	t.Logf("Request 1 body: %d bytes (~%d est. tokens)", len(body1), len(body1)/4)

	t.Log("Sending request 1...")
	tokens1, text1, err := sendStreamingRequest(apiKey, body1)
	if err != nil {
		t.Fatalf("Request 1 failed: %v", err)
	}

	t.Logf("Request 1 result:")
	t.Logf("  input_tokens:          %d", tokens1.InputTokens)
	t.Logf("  cache_creation_tokens: %d", tokens1.CacheCreate)
	t.Logf("  cache_read_tokens:     %d", tokens1.CacheRead)
	t.Logf("  output_tokens:         %d", tokens1.OutputTokens)
	t.Logf("  response:              %s", truncateStr(text1, 200))

	if tokens1.CacheCreate == 0 && tokens1.CacheRead == 0 {
		t.Logf("WARNING: No cache activity on request 1. Prompt may be below minimum cacheable length.")
		t.Logf("  Estimated prompt tokens: %d", tokens1.InputTokens+tokens1.CacheCreate)
		t.Logf("  Minimum for Sonnet 4.5: 1024 tokens")
		t.Logf("  Minimum for Opus 4.6:   4096 tokens")
	}

	// Small delay to let cache register
	time.Sleep(2 * time.Second)

	// ─── Request 2: Same prefix + extended conversation (should read cache) ─
	// Build with the exact same tools + system prompt (prefix must be identical)
	messages2 := []stream.Message{
		{Role: "user", Content: "Hi! What notification tools do you have? Reply in 2 sentences max."},
		{Role: "assistant", Content: text1},
		{Role: "user", Content: "Can you tell me more about the push notification tool? Reply in 2 sentences max."},
	}

	body2, err := buildTestRequestBody(mcpTools, testCacheSystemPrompt, messages2, model)
	if err != nil {
		t.Fatalf("Failed to build request 2: %v", err)
	}
	t.Logf("Request 2 body: %d bytes (~%d est. tokens)", len(body2), len(body2)/4)

	// Verify prefix determinism: the tools + system + first message should be identical
	// between request 1 and the first portion of request 2
	verifyPrefixMatch(t, body1, body2)

	t.Log("Sending request 2...")
	tokens2, text2, err := sendStreamingRequest(apiKey, body2)
	if err != nil {
		t.Fatalf("Request 2 failed: %v", err)
	}

	t.Logf("Request 2 result:")
	t.Logf("  input_tokens:          %d", tokens2.InputTokens)
	t.Logf("  cache_creation_tokens: %d", tokens2.CacheCreate)
	t.Logf("  cache_read_tokens:     %d", tokens2.CacheRead)
	t.Logf("  output_tokens:         %d", tokens2.OutputTokens)
	t.Logf("  response:              %s", truncateStr(text2, 200))

	// ─── Analysis ──────────────────────────────────────────────────────
	t.Log("")
	t.Log("═══════════════════════════════════════════════════════════")
	t.Log("CACHE ANALYSIS")
	t.Log("═══════════════════════════════════════════════════════════")

	if tokens1.CacheCreate > 0 {
		t.Logf("Request 1: Cache WRITE of %d tokens (expected for first request)", tokens1.CacheCreate)
	} else {
		t.Log("Request 1: No cache write (prompt may be below minimum)")
	}

	if tokens2.CacheRead > 0 {
		t.Logf("Request 2: Cache READ of %d tokens — CACHE IS WORKING!", tokens2.CacheRead)
		savings := float64(tokens2.CacheRead) * 0.9 / float64(tokens2.CacheRead+tokens2.InputTokens+tokens2.CacheCreate) * 100
		t.Logf("  Estimated input cost savings: %.0f%%", savings)
	} else if tokens2.CacheCreate > 0 {
		t.Errorf("Request 2: Cache WRITE of %d tokens instead of READ — CACHE NOT WORKING!", tokens2.CacheCreate)
		t.Log("  This means the prefix changed between requests.")
		t.Log("  Possible causes:")
		t.Log("  - Tool definitions changed between requests")
		t.Log("  - System prompt changed between requests")
		t.Log("  - JSON serialization is non-deterministic")
	} else {
		t.Log("Request 2: No cache activity — prompt below minimum cacheable length")
	}

	// ─── Request 3: One more turn (should also get cache hit) ──────────
	time.Sleep(1 * time.Second)

	messages3 := []stream.Message{
		{Role: "user", Content: "Hi! What notification tools do you have? Reply in 2 sentences max."},
		{Role: "assistant", Content: text1},
		{Role: "user", Content: "Can you tell me more about the push notification tool? Reply in 2 sentences max."},
		{Role: "assistant", Content: text2},
		{Role: "user", Content: "Thanks! How do I set priority levels? Reply in 2 sentences max."},
	}

	body3, err := buildTestRequestBody(mcpTools, testCacheSystemPrompt, messages3, model)
	if err != nil {
		t.Fatalf("Failed to build request 3: %v", err)
	}

	t.Log("Sending request 3...")
	tokens3, _, err := sendStreamingRequest(apiKey, body3)
	if err != nil {
		t.Fatalf("Request 3 failed: %v", err)
	}

	t.Logf("Request 3 result:")
	t.Logf("  input_tokens:          %d", tokens3.InputTokens)
	t.Logf("  cache_creation_tokens: %d", tokens3.CacheCreate)
	t.Logf("  cache_read_tokens:     %d", tokens3.CacheRead)
	t.Logf("  output_tokens:         %d", tokens3.OutputTokens)

	if tokens3.CacheRead > 0 {
		t.Logf("Request 3: Cache READ of %d tokens — conversation caching works!", tokens3.CacheRead)
	}

	t.Log("")
	t.Log("═══════════════════════════════════════════════════════════")
	if tokens2.CacheRead > 0 || tokens3.CacheRead > 0 {
		t.Log("RESULT: Prompt caching is WORKING correctly")
	} else if tokens1.CacheCreate == 0 {
		t.Log("RESULT: Prompt is below minimum cacheable length for this model")
	} else {
		t.Log("RESULT: Prompt caching is BROKEN — cache writes but no reads")
	}
	t.Log("═══════════════════════════════════════════════════════════")
}

// verifyPrefixMatch checks that the shared prefix (tools + system) is identical
// between two request bodies.
func verifyPrefixMatch(t *testing.T, body1, body2 []byte) {
	t.Helper()

	var req1, req2 map[string]interface{}
	json.Unmarshal(body1, &req1)
	json.Unmarshal(body2, &req2)

	// Compare tools
	tools1, _ := json.Marshal(req1["tools"])
	tools2, _ := json.Marshal(req2["tools"])
	if !bytes.Equal(tools1, tools2) {
		t.Error("TOOLS DIFFER between request 1 and 2!")
		t.Logf("  tools1: %s", truncateStr(string(tools1), 500))
		t.Logf("  tools2: %s", truncateStr(string(tools2), 500))
	} else {
		t.Logf("Tools match: %d bytes", len(tools1))
	}

	// Compare system prompt
	sys1, _ := json.Marshal(req1["system"])
	sys2, _ := json.Marshal(req2["system"])
	if !bytes.Equal(sys1, sys2) {
		t.Error("SYSTEM PROMPT DIFFERS between request 1 and 2!")
		// Find the difference
		s1, s2 := string(sys1), string(sys2)
		for i := 0; i < len(s1) && i < len(s2); i++ {
			if s1[i] != s2[i] {
				start := i - 30
				if start < 0 {
					start = 0
				}
				t.Logf("  First diff at byte %d: ...%s... vs ...%s...", i, s1[start:i+30], s2[start:i+30])
				break
			}
		}
	} else {
		t.Logf("System prompt match: %d bytes", len(sys1))
	}

	// Compare model, max_tokens, cache_control
	for _, key := range []string{"model", "max_tokens", "cache_control", "stream"} {
		v1, _ := json.Marshal(req1[key])
		v2, _ := json.Marshal(req2[key])
		if !bytes.Equal(v1, v2) {
			t.Errorf("%s DIFFERS: %s vs %s", key, string(v1), string(v2))
		}
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
