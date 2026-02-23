package tools

import (
	"strings"
	"testing"

	"github.com/apteva/agent/config"
)

// ============================================================================
// Shared test fixture: 50 tools simulating a realistic multi-MCP setup
// ============================================================================

func createTestToolSet() []ToolDefinition {
	return []ToolDefinition{
		// --- DevOps Monitoring (8 tools) ---
		{Name: "check-server-health", Description: "Monitor server health metrics including CPU usage, memory consumption, disk space, and active processes. Returns current values and threshold warnings.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"server_id": m("string"), "metrics": m("array"), "include_historical": m("boolean")},
		}},
		{Name: "get-error-logs", Description: "Pull and analyze application error logs filtered by severity, service, and timeframe. Returns structured log entries.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"severity": m("string"), "service": m("string"), "last_hours": m("integer")},
		}},
		{Name: "query-metrics", Description: "Fetch API response times, request counts, error rates, and throughput metrics for monitoring dashboards.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"endpoint": m("string"), "metric_type": m("string"), "period": m("string")},
		}},
		{Name: "send-alert", Description: "Send alert notifications to Slack, email, or PagerDuty channels for critical infrastructure events.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"channel": m("string"), "message": m("string"), "severity": m("string")},
		}},
		{Name: "get-deployment-status", Description: "Check current deployment versions, health status, and rollout progress across all environments.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"environment": m("string"), "service": m("string")},
		}},
		{Name: "restart-service", Description: "Restart a failed or unresponsive service container with optional health check verification.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"service_name": m("string"), "force": m("boolean")},
		}},
		{Name: "check-ssl-certificates", Description: "Verify SSL certificate expiration dates and validity across all configured domains.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"domain": m("string")},
		}},
		{Name: "analyze-traffic-patterns", Description: "Review traffic spikes, anomalies, and geographic distribution patterns for web services.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"service": m("string"), "timeframe": m("string")},
		}},

		// --- Stripe MCP (8 tools) ---
		{Name: "stripe:create-customer", Description: "Create a new Stripe customer with email, name, and payment method details.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"email": m("string"), "name": m("string"), "payment_method": m("string")},
		}},
		{Name: "stripe:create-payment", Description: "Create a payment intent or charge for a customer with specified amount and currency.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"customer_id": m("string"), "amount": m("integer"), "currency": m("string")},
		}},
		{Name: "stripe:list-charges", Description: "List recent charges and payment history for a customer or across the account.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"customer_id": m("string"), "limit": m("integer")},
		}},
		{Name: "stripe:manage-subscription", Description: "Create, update, or cancel customer subscriptions with plan and billing cycle management.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"customer_id": m("string"), "plan_id": m("string"), "action": m("string")},
		}},
		{Name: "stripe:create-invoice", Description: "Generate and send invoices to customers for one-time or recurring charges.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"customer_id": m("string"), "items": m("array")},
		}},
		{Name: "stripe:refund-payment", Description: "Process a full or partial refund for a completed payment charge.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"charge_id": m("string"), "amount": m("integer"), "reason": m("string")},
		}},
		{Name: "stripe:get-balance", Description: "Retrieve the current Stripe account balance including pending and available funds.", InputSchema: map[string]interface{}{}},
		{Name: "stripe:list-disputes", Description: "List open payment disputes and chargebacks requiring attention.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"status": m("string")},
		}},

		// --- GitHub MCP (8 tools) ---
		{Name: "github:create-issue", Description: "Create a new issue in a GitHub repository with title, body, labels, and assignees.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"repo": m("string"), "title": m("string"), "body": m("string"), "labels": m("array")},
		}},
		{Name: "github:create-pull-request", Description: "Create a pull request with title, description, source branch, and target branch.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"repo": m("string"), "title": m("string"), "head": m("string"), "base": m("string")},
		}},
		{Name: "github:list-repositories", Description: "List repositories for an organization or user with filtering and sorting.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"org": m("string"), "sort": m("string")},
		}},
		{Name: "github:get-file-contents", Description: "Read file contents from a repository at a specific path and branch.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"repo": m("string"), "path": m("string"), "ref": m("string")},
		}},
		{Name: "github:search-code", Description: "Search for code across GitHub repositories using query strings and filters.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"query": m("string"), "language": m("string")},
		}},
		{Name: "github:list-actions-runs", Description: "List recent GitHub Actions workflow runs with status and timing information.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"repo": m("string"), "status": m("string")},
		}},
		{Name: "github:merge-pull-request", Description: "Merge an approved pull request using merge, squash, or rebase strategy.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"repo": m("string"), "pull_number": m("integer"), "merge_method": m("string")},
		}},
		{Name: "github:create-release", Description: "Create a new release with tag, release notes, and optional binary assets.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"repo": m("string"), "tag": m("string"), "body": m("string")},
		}},

		// --- Database Tools (6 tools) ---
		{Name: "db:query", Description: "Execute a read-only SQL query against the database and return results as JSON.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"sql": m("string"), "database": m("string")},
		}},
		{Name: "db:insert-record", Description: "Insert a new record into a database table with specified column values.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"table": m("string"), "data": m("object")},
		}},
		{Name: "db:update-record", Description: "Update an existing database record by ID with new column values.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"table": m("string"), "id": m("string"), "data": m("object")},
		}},
		{Name: "db:delete-record", Description: "Delete a record from a database table by ID.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"table": m("string"), "id": m("string")},
		}},
		{Name: "db:list-tables", Description: "List all tables in the database with their column schemas and row counts.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"database": m("string")},
		}},
		{Name: "db:run-migration", Description: "Execute a database migration script to update schema or seed data.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"migration_file": m("string")},
		}},

		// --- Slack MCP (6 tools) ---
		{Name: "slack:send-message", Description: "Send a message to a Slack channel or direct message conversation.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"channel": m("string"), "text": m("string")},
		}},
		{Name: "slack:list-channels", Description: "List available Slack channels with member counts and topic information.", InputSchema: map[string]interface{}{}},
		{Name: "slack:search-messages", Description: "Search Slack message history across channels using keywords and filters.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"query": m("string"), "channel": m("string")},
		}},
		{Name: "slack:create-channel", Description: "Create a new Slack channel with name, description, and initial members.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"name": m("string"), "description": m("string")},
		}},
		{Name: "slack:upload-file", Description: "Upload a file to a Slack channel with optional comment.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"channel": m("string"), "file_path": m("string"), "comment": m("string")},
		}},
		{Name: "slack:set-status", Description: "Set the bot or user status message and emoji in Slack.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"text": m("string"), "emoji": m("string")},
		}},

		// --- File & Content Tools (6 tools) ---
		{Name: "read-file", Description: "Read the contents of a file from the local filesystem or remote storage.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"path": m("string")},
		}},
		{Name: "write-file", Description: "Write content to a file on the local filesystem, creating it if necessary.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"path": m("string"), "content": m("string")},
		}},
		{Name: "generate-pdf", Description: "Generate a PDF document from HTML content or markdown with custom styling.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"content": m("string"), "format": m("string")},
		}},
		{Name: "convert-image", Description: "Convert images between formats, resize, or apply transformations.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"input_path": m("string"), "output_format": m("string"), "width": m("integer")},
		}},
		{Name: "parse-csv", Description: "Parse a CSV file into structured JSON data with header detection and type inference.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"file_path": m("string"), "delimiter": m("string")},
		}},
		{Name: "extract-text", Description: "Extract text content from PDF, DOCX, or image files using OCR.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"file_path": m("string"), "method": m("string")},
		}},

		// --- Web & Search Tools (4 tools) ---
		{Name: "web-search", Description: "Search the web using a query string and return relevant results with snippets.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"query": m("string"), "num_results": m("integer")},
		}},
		{Name: "fetch-url", Description: "Fetch content from a URL and return the page text, HTML, or JSON response.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"url": m("string"), "format": m("string")},
		}},
		{Name: "screenshot-url", Description: "Capture a screenshot of a web page at a specified URL and viewport size.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"url": m("string"), "width": m("integer"), "height": m("integer")},
		}},
		{Name: "check-url-status", Description: "Check HTTP status code and response headers for a URL endpoint.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"url": m("string")},
		}},

		// --- Email Tools (4 tools) ---
		{Name: "email:send", Description: "Send an email message with recipients, subject, body, and optional attachments.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"to": m("string"), "subject": m("string"), "body": m("string")},
		}},
		{Name: "email:list-inbox", Description: "List recent emails in the inbox with sender, subject, and date information.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"limit": m("integer"), "unread_only": m("boolean")},
		}},
		{Name: "email:search", Description: "Search emails by sender, subject, date range, or full-text content.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"query": m("string"), "from": m("string"), "date_after": m("string")},
		}},
		{Name: "email:create-draft", Description: "Create an email draft without sending, for review or scheduled delivery.", InputSchema: map[string]interface{}{
			"properties": map[string]interface{}{"to": m("string"), "subject": m("string"), "body": m("string")},
		}},
	}
}

// Helper to create a simple schema property
func m(typeName string) map[string]interface{} {
	return map[string]interface{}{"type": typeName}
}

// ============================================================================
// Tool Loader Tests
// ============================================================================

func TestToolLoaderFullMode(t *testing.T) {
	allTools := createTestToolSet()

	t.Run("nil config returns all tools", func(t *testing.T) {
		loader := NewToolLoaderWithTools(nil, allTools)
		result := loader.LoadTools("anything", allTools)
		if len(result) != len(allTools) {
			t.Errorf("Expected %d tools, got %d", len(allTools), len(result))
		}
	})

	t.Run("empty mode returns all tools", func(t *testing.T) {
		loader := NewToolLoaderWithTools(&config.ToolLoadingConfig{Mode: ""}, allTools)
		result := loader.LoadTools("anything", allTools)
		if len(result) != len(allTools) {
			t.Errorf("Expected %d tools, got %d", len(allTools), len(result))
		}
	})

	t.Run("full mode returns all tools", func(t *testing.T) {
		loader := NewToolLoaderWithTools(&config.ToolLoadingConfig{Mode: "full"}, allTools)
		result := loader.LoadTools("anything", allTools)
		if len(result) != len(allTools) {
			t.Errorf("Expected %d tools, got %d", len(allTools), len(result))
		}
	})
}

func TestToolLoaderAlwaysLoad(t *testing.T) {
	allTools := createTestToolSet()

	t.Run("always_load tools are always included", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:       "on_demand",
			Strategy:   "bm25",
			AlwaysLoad: []string{"send-alert", "check-server-health"},
			BM25:       &config.BM25Config{TopK: 3, Threshold: 0.5},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		// Query about something completely unrelated (email)
		result := loader.LoadTools("send an email to john", allTools)

		foundAlert := false
		foundHealth := false
		for _, t := range result {
			if t.Name == "send-alert" {
				foundAlert = true
			}
			if t.Name == "check-server-health" {
				foundHealth = true
			}
		}
		if !foundAlert {
			t.Error("send-alert should always be loaded")
		}
		if !foundHealth {
			t.Error("check-server-health should always be loaded")
		}
	})

	t.Run("always_load with glob pattern", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:       "on_demand",
			Strategy:   "bm25",
			AlwaysLoad: []string{"stripe:*"},
			BM25:       &config.BM25Config{TopK: 3, Threshold: 0.5},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("check server health", allTools)

		stripeCount := 0
		for _, t := range result {
			if strings.HasPrefix(t.Name, "stripe:") {
				stripeCount++
			}
		}
		if stripeCount != 8 {
			t.Errorf("Expected all 8 stripe tools from glob, got %d", stripeCount)
		}
	})
}

func TestToolLoaderMaxToolsCap(t *testing.T) {
	allTools := createTestToolSet()

	t.Run("respects max_tools cap", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:       "on_demand",
			Strategy:   "bm25",
			AlwaysLoad: []string{"stripe:*"}, // 8 tools
			MaxTools:   5,
			BM25:       &config.BM25Config{TopK: 10, Threshold: 0.1},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("payment customer", allTools)
		if len(result) > 5 {
			t.Errorf("Expected at most 5 tools (max_tools=5), got %d", len(result))
		}
	})

	t.Run("default max_tools is 15", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "bm25",
			BM25:     &config.BM25Config{TopK: 100, Threshold: 0.0},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		// Broad query that matches many tools
		result := loader.LoadTools("check status list create", allTools)
		if len(result) > 15 {
			t.Errorf("Expected at most 15 tools (default max), got %d", len(result))
		}
	})
}

func TestToolLoaderBM25Strategy(t *testing.T) {
	allTools := createTestToolSet()

	t.Run("selects DevOps tools for infrastructure query", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "bm25",
			BM25:     &config.BM25Config{TopK: 5, Threshold: 0.5},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("My API server is down and returning 500 errors, check health and logs", allTools)

		names := toolNames(result)
		t.Logf("BM25 selected for infra query: %v", names)

		// Should select DevOps tools, not Stripe/Slack/Email tools
		hasDevOps := false
		hasIrrelevant := false
		for _, r := range result {
			if r.Name == "check-server-health" || r.Name == "get-error-logs" || r.Name == "query-metrics" {
				hasDevOps = true
			}
			if strings.HasPrefix(r.Name, "stripe:") || strings.HasPrefix(r.Name, "email:") {
				hasIrrelevant = true
			}
		}
		if !hasDevOps {
			t.Error("Expected DevOps tools in results for infrastructure query")
		}
		if hasIrrelevant {
			t.Errorf("Did not expect Stripe or Email tools for infrastructure query, got %v", names)
		}
	})

	t.Run("selects GitHub tools for code query", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "bm25",
			BM25:     &config.BM25Config{TopK: 5, Threshold: 0.5},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("create a pull request and merge it into main branch", allTools)

		names := toolNames(result)
		t.Logf("BM25 selected for code query: %v", names)

		hasGitHub := false
		for _, r := range result {
			if r.Name == "github:create-pull-request" || r.Name == "github:merge-pull-request" {
				hasGitHub = true
				break
			}
		}
		if !hasGitHub {
			t.Errorf("Expected GitHub PR tools for code query, got %v", names)
		}
	})

	t.Run("selects database tools for SQL query", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "bm25",
			BM25:     &config.BM25Config{TopK: 5, Threshold: 0.5},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("query the database for all users and list tables", allTools)

		names := toolNames(result)
		t.Logf("BM25 selected for DB query: %v", names)

		hasDB := false
		for _, r := range result {
			if strings.HasPrefix(r.Name, "db:") {
				hasDB = true
				break
			}
		}
		if !hasDB {
			t.Errorf("Expected database tools for SQL query, got %v", names)
		}
	})

	t.Run("selects Slack tools for messaging query", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "bm25",
			BM25:     &config.BM25Config{TopK: 5, Threshold: 0.5},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("send a message to the #engineering Slack channel", allTools)

		names := toolNames(result)
		t.Logf("BM25 selected for Slack query: %v", names)

		hasSlack := false
		for _, r := range result {
			if strings.HasPrefix(r.Name, "slack:") {
				hasSlack = true
				break
			}
		}
		if !hasSlack {
			t.Errorf("Expected Slack tools for messaging query, got %v", names)
		}
	})
}

func TestToolLoaderRegexStrategy(t *testing.T) {
	allTools := createTestToolSet()

	t.Run("matches regex rules", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "regex",
			Regex: &config.RegexConfig{
				Rules: []config.RegexRule{
					{Pattern: `(?i)server|health|cpu|memory`, Tools: []string{"check-server-health", "get-error-logs"}},
					{Pattern: `(?i)payment|charge|stripe`, Tools: []string{"stripe:*"}},
					{Pattern: `(?i)deploy|release`, Tools: []string{"get-deployment-status", "github:create-release"}},
				},
			},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("check server health and CPU usage", allTools)
		names := toolNames(result)
		t.Logf("Regex selected for server query: %v", names)

		foundHealth := false
		foundLogs := false
		for _, r := range result {
			if r.Name == "check-server-health" {
				foundHealth = true
			}
			if r.Name == "get-error-logs" {
				foundLogs = true
			}
		}
		if !foundHealth || !foundLogs {
			t.Errorf("Expected health and logs tools, got %v", names)
		}
	})

	t.Run("regex glob expands stripe:*", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "regex",
			Regex: &config.RegexConfig{
				Rules: []config.RegexRule{
					{Pattern: `(?i)payment|charge|stripe`, Tools: []string{"stripe:*"}},
				},
			},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("process a payment", allTools)
		stripeCount := 0
		for _, r := range result {
			if strings.HasPrefix(r.Name, "stripe:") {
				stripeCount++
			}
		}
		if stripeCount != 8 {
			t.Errorf("Expected all 8 stripe tools from glob, got %d", stripeCount)
		}
	})

	t.Run("no match returns empty (plus always_load)", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "regex",
			Regex: &config.RegexConfig{
				Rules: []config.RegexRule{
					{Pattern: `(?i)server|health`, Tools: []string{"check-server-health"}},
				},
			},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("send an email to someone", allTools)
		if len(result) != 0 {
			t.Errorf("Expected 0 tools for unmatched query, got %d: %v", len(result), toolNames(result))
		}
	})
}

func TestToolLoaderKeywordStrategy(t *testing.T) {
	allTools := createTestToolSet()

	t.Run("matches keyword rules", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "keyword",
			Keyword: &config.KeywordConfig{
				Rules: []config.KeywordRule{
					{Keywords: []string{"email", "mail", "inbox"}, Tools: []string{"email:*"}},
					{Keywords: []string{"file", "read", "write", "pdf"}, Tools: []string{"read-file", "write-file", "generate-pdf"}},
				},
			},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("check my email inbox", allTools)
		emailCount := 0
		for _, r := range result {
			if strings.HasPrefix(r.Name, "email:") {
				emailCount++
			}
		}
		if emailCount != 4 {
			t.Errorf("Expected 4 email tools, got %d", emailCount)
		}
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "keyword",
			Keyword: &config.KeywordConfig{
				Rules: []config.KeywordRule{
					{Keywords: []string{"PDF"}, Tools: []string{"generate-pdf"}},
				},
			},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)

		result := loader.LoadTools("generate a pdf document", allTools)
		found := false
		for _, r := range result {
			if r.Name == "generate-pdf" {
				found = true
			}
		}
		if !found {
			t.Error("Expected generate-pdf to match case-insensitively")
		}
	})
}

// ============================================================================
// Tool Manifest Tests
// ============================================================================

func TestGenerateToolManifest(t *testing.T) {
	allTools := createTestToolSet()

	t.Run("includes all tool names", func(t *testing.T) {
		manifest := GenerateToolManifest(allTools)
		for _, tool := range allTools {
			if !strings.Contains(manifest, tool.Name) {
				t.Errorf("Manifest missing tool: %s", tool.Name)
			}
		}
	})

	t.Run("uses summarized descriptions", func(t *testing.T) {
		manifest := GenerateToolManifest(allTools)
		// Full description should NOT appear (they have multiple sentences)
		if strings.Contains(manifest, "Returns current values and threshold warnings") {
			t.Error("Manifest should use first sentence only, not full description")
		}
	})

	t.Run("empty tools returns empty string", func(t *testing.T) {
		manifest := GenerateToolManifest([]ToolDefinition{})
		if manifest != "" {
			t.Errorf("Expected empty manifest for empty tools, got %q", manifest)
		}
	})

	t.Run("format is markdown list", func(t *testing.T) {
		manifest := GenerateToolManifest(allTools)
		lines := strings.Split(strings.TrimSpace(manifest), "\n")
		for _, line := range lines {
			if !strings.HasPrefix(line, "- **") {
				t.Errorf("Expected markdown list format, got: %s", line)
			}
		}
	})
}

func TestGenerateManifestPrompt(t *testing.T) {
	allTools := createTestToolSet()

	t.Run("returns empty for full mode", func(t *testing.T) {
		loader := NewToolLoaderWithTools(&config.ToolLoadingConfig{Mode: "full"}, allTools)
		prompt := loader.GenerateManifestPrompt(allTools)
		if prompt != "" {
			t.Error("Expected empty manifest prompt for full mode")
		}
	})

	t.Run("returns manifest for on_demand mode", func(t *testing.T) {
		cfg := &config.ToolLoadingConfig{
			Mode:     "on_demand",
			Strategy: "bm25",
			BM25:     &config.BM25Config{TopK: 5, Threshold: 0.5},
		}
		loader := NewToolLoaderWithTools(cfg, allTools)
		prompt := loader.GenerateManifestPrompt(allTools)

		if !strings.Contains(prompt, "## Available Tools") {
			t.Error("Manifest prompt should contain header")
		}
		if !strings.Contains(prompt, "check-server-health") {
			t.Error("Manifest prompt should list tools")
		}
	})
}

// ============================================================================
// extractFirstSentence Tests
// ============================================================================

func TestExtractFirstSentence(t *testing.T) {
	t.Run("stops at period", func(t *testing.T) {
		result := extractFirstSentence("Monitor server health. Returns current values.")
		if result != "Monitor server health" {
			t.Errorf("Expected first sentence, got %q", result)
		}
	})

	t.Run("stops at question mark", func(t *testing.T) {
		result := extractFirstSentence("Is the server healthy? Check now.")
		if result != "Is the server healthy" {
			t.Errorf("Expected first sentence, got %q", result)
		}
	})

	t.Run("stops at newline", func(t *testing.T) {
		result := extractFirstSentence("Monitor server health\nReturns values")
		if result != "Monitor server health" {
			t.Errorf("Expected first line, got %q", result)
		}
	})

	t.Run("truncates long text", func(t *testing.T) {
		long := strings.Repeat("word ", 50) // ~250 chars, no period
		result := extractFirstSentence(long)
		if len(result) > 110 { // 100 + "..."
			t.Errorf("Expected truncated text, got length %d", len(result))
		}
		if !strings.HasSuffix(result, "...") {
			t.Errorf("Expected ... suffix for truncated text, got %q", result)
		}
	})

	t.Run("returns full short text", func(t *testing.T) {
		result := extractFirstSentence("Short text")
		if result != "Short text" {
			t.Errorf("Expected full text, got %q", result)
		}
	})
}

// ============================================================================
// Expand Glob Tests
// ============================================================================

func TestExpandGlob(t *testing.T) {
	allTools := createTestToolSet()
	toolMap := make(map[string]ToolDefinition)
	for _, tool := range allTools {
		toolMap[tool.Name] = tool
	}

	loader := &ToolLoader{}

	t.Run("direct match", func(t *testing.T) {
		result := loader.expandGlob("send-alert", toolMap)
		if len(result) != 1 || result[0].Name != "send-alert" {
			t.Errorf("Expected direct match for send-alert, got %v", toolNames(result))
		}
	})

	t.Run("glob pattern matches prefix", func(t *testing.T) {
		result := loader.expandGlob("github:*", toolMap)
		if len(result) != 8 {
			t.Errorf("Expected 8 github tools, got %d: %v", len(result), toolNames(result))
		}
	})

	t.Run("no match returns nil", func(t *testing.T) {
		result := loader.expandGlob("nonexistent-tool", toolMap)
		if len(result) != 0 {
			t.Errorf("Expected no match, got %v", toolNames(result))
		}
	})

	t.Run("glob with no matches returns nil", func(t *testing.T) {
		result := loader.expandGlob("notion:*", toolMap)
		if len(result) != 0 {
			t.Errorf("Expected no match for notion glob, got %v", toolNames(result))
		}
	})
}

// ============================================================================
// Agent Simulation: 50 tools, on-demand selection
// ============================================================================

func TestAgentToolSelectionWith50Tools(t *testing.T) {
	allTools := createTestToolSet()
	if len(allTools) != 50 {
		t.Fatalf("Expected 50 tools in test set, got %d", len(allTools))
	}

	cfg := &config.ToolLoadingConfig{
		Mode:       "on_demand",
		Strategy:   "bm25",
		AlwaysLoad: []string{"send-alert"},
		MaxTools:   10,
		BM25:       &config.BM25Config{TopK: 8, Threshold: 0.5},
	}
	loader := NewToolLoaderWithTools(cfg, allTools)

	scenarios := []struct {
		name          string
		query         string
		mustInclude   []string // tools that MUST be selected
		mustExclude   []string // tools that should NOT be selected
		alwaysPresent []string // always_load tools
	}{
		{
			name:          "DevOps: server down investigation",
			query:         "The production API server is returning 500 errors and CPU is at 95%. Check health, pull error logs, and alert the team.",
			mustInclude:   []string{"check-server-health"},
			mustExclude:   []string{"stripe:create-payment", "github:create-issue", "email:send", "generate-pdf"},
			alwaysPresent: []string{"send-alert"},
		},
		{
			name:          "Stripe: payment processing",
			query:         "Create a new customer in Stripe, charge them $99, and generate an invoice for the subscription.",
			mustInclude:   []string{"stripe:create-customer"},
			mustExclude:   []string{"check-server-health", "get-error-logs", "slack:send-message", "db:query"},
			alwaysPresent: []string{"send-alert"},
		},
		{
			name:          "GitHub: release workflow",
			query:         "Create a new release on GitHub with tag v2.0.0, then create a pull request to merge into main.",
			mustInclude:   []string{"github:create-release"},
			mustExclude:   []string{"stripe:create-payment", "email:send", "restart-service", "db:query"},
			alwaysPresent: []string{"send-alert"},
		},
		{
			name:          "Database: data operations",
			query:         "Query the users table in the database and list all available tables with their schemas.",
			mustInclude:   []string{"db:query"},
			mustExclude:   []string{"stripe:create-customer", "slack:send-message", "screenshot-url"},
			alwaysPresent: []string{"send-alert"},
		},
		{
			name:          "Slack: team communication",
			query:         "Send a message to the engineering Slack channel about the deployment status.",
			mustInclude:   []string{"slack:send-message"},
			mustExclude:   []string{"stripe:create-payment", "generate-pdf", "db:query"},
			alwaysPresent: []string{"send-alert"},
		},
		{
			name:          "Email: outreach",
			query:         "Search my email inbox for messages from the client, then draft a reply about the project update.",
			mustInclude:   []string{"email:search"},
			mustExclude:   []string{"check-server-health", "stripe:create-payment", "github:create-issue"},
			alwaysPresent: []string{"send-alert"},
		},
		{
			name:          "File: document processing",
			query:         "Read the CSV data file, parse it, and generate a PDF report from the results.",
			mustInclude:   []string{"generate-pdf"},
			mustExclude:   []string{"stripe:create-payment", "slack:send-message", "check-server-health"},
			alwaysPresent: []string{"send-alert"},
		},
		{
			name:          "Web: page analysis",
			query:         "Take a screenshot of the homepage, fetch the HTML content, and check the URL status code.",
			mustInclude:   []string{"screenshot-url"},
			mustExclude:   []string{"stripe:create-payment", "db:query", "email:send"},
			alwaysPresent: []string{"send-alert"},
		},
		{
			name:          "SSL: certificate check",
			query:         "Check if our SSL certificates are expiring soon across all domains.",
			mustInclude:   []string{"check-ssl-certificates"},
			mustExclude:   []string{"stripe:create-payment", "slack:send-message", "generate-pdf"},
			alwaysPresent: []string{"send-alert"},
		},
		{
			name:          "Deploy: rollout status",
			query:         "What's the deployment status? Check if the latest release rolled out successfully to production.",
			mustInclude:   []string{"get-deployment-status"},
			mustExclude:   []string{"stripe:create-payment", "email:send", "parse-csv"},
			alwaysPresent: []string{"send-alert"},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			result := loader.LoadTools(sc.query, allTools)
			names := toolNames(result)
			t.Logf("Query: %s", sc.query)
			t.Logf("Selected %d/%d tools: %v", len(result), len(allTools), names)

			// Verify tool count is reasonable (not all 50)
			if len(result) > 10 {
				t.Errorf("Selected too many tools (%d) — on-demand should filter aggressively", len(result))
			}

			// Verify must-include tools
			nameSet := make(map[string]bool)
			for _, n := range names {
				nameSet[n] = true
			}

			for _, must := range sc.mustInclude {
				if !nameSet[must] {
					t.Errorf("MUST include %q but it was not selected. Got: %v", must, names)
				}
			}

			// Verify must-exclude tools
			for _, exclude := range sc.mustExclude {
				if nameSet[exclude] {
					t.Errorf("Should NOT include %q for this query. Got: %v", exclude, names)
				}
			}

			// Verify always-present tools
			for _, always := range sc.alwaysPresent {
				if !nameSet[always] {
					t.Errorf("Always-load tool %q missing from results. Got: %v", always, names)
				}
			}
		})
	}
}

// ============================================================================
// Token savings estimation
// ============================================================================

func TestTokenSavingsEstimate(t *testing.T) {
	allTools := createTestToolSet()

	cfg := &config.ToolLoadingConfig{
		Mode:       "on_demand",
		Strategy:   "bm25",
		AlwaysLoad: []string{"send-alert"},
		MaxTools:   10,
		BM25:       &config.BM25Config{TopK: 5, Threshold: 0.5},
	}
	loader := NewToolLoaderWithTools(cfg, allTools)

	// Estimate tokens: ~4 chars per token (rough heuristic)
	estimateTokens := func(tools []ToolDefinition) int {
		total := 0
		for _, tool := range tools {
			total += len(tool.Name) + len(tool.Description)
			// Rough schema estimate
			if props, ok := tool.InputSchema["properties"].(map[string]interface{}); ok {
				total += len(props) * 50 // ~50 chars per property
			}
		}
		return total / 4
	}

	fullTokens := estimateTokens(allTools)
	t.Logf("Full mode: %d estimated tokens for %d tools", fullTokens, len(allTools))

	queries := []string{
		"check server health",
		"create a stripe payment",
		"send a slack message",
		"query the database",
		"check SSL certificates",
	}

	totalSaved := 0
	for _, query := range queries {
		selected := loader.LoadTools(query, allTools)
		selectedTokens := estimateTokens(selected)
		saved := fullTokens - selectedTokens
		pctSaved := float64(saved) / float64(fullTokens) * 100

		t.Logf("Query: %q → %d tools, ~%d tokens (saved ~%d tokens, %.0f%%)",
			query, len(selected), selectedTokens, saved, pctSaved)

		totalSaved += saved

		if pctSaved < 40 {
			t.Errorf("Expected at least 40%% token savings, got %.0f%% for query %q", pctSaved, query)
		}
	}

	avgSaved := float64(totalSaved) / float64(len(queries))
	avgPctSaved := avgSaved / float64(fullTokens) * 100
	t.Logf("\nAverage savings: ~%.0f tokens (%.0f%%) per query across %d queries", avgSaved, avgPctSaved, len(queries))
}

// ============================================================================
// End-to-end: manifest knows all tools, LLM only gets filtered subset
// ============================================================================

func TestManifestKnowsAllToolsButLLMGetsFiltered(t *testing.T) {
	allTools := createTestToolSet()

	cfg := &config.ToolLoadingConfig{
		Mode:       "on_demand",
		Strategy:   "bm25",
		AlwaysLoad: []string{"send-alert"},
		MaxTools:   10,
		BM25:       &config.BM25Config{TopK: 5, Threshold: 0.5},
	}
	loader := NewToolLoaderWithTools(cfg, allTools)

	scenarios := []struct {
		name  string
		query string
	}{
		{"DevOps query", "check server health and pull error logs"},
		{"Stripe query", "create a customer and charge a payment in Stripe"},
		{"GitHub query", "create a pull request on GitHub"},
		{"Database query", "query the users table in the database"},
		{"Slack query", "send a message to the engineering Slack channel"},
		{"Email query", "search inbox for messages from the client"},
		{"File query", "read the CSV file and generate a PDF report"},
		{"Web query", "take a screenshot of the homepage"},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// 1. Generate manifest (what goes into system prompt — agent sees ALL tools)
			manifest := loader.GenerateManifestPrompt(allTools)
			manifestEntries := loader.GetToolManifest(allTools)

			// 2. Load tools for LLM (what gets sent as full tool definitions)
			llmTools := loader.LoadTools(sc.query, allTools)

			// --- Verify manifest knows ALL 50 tools ---
			if len(manifestEntries) != len(allTools) {
				t.Errorf("Manifest should list all %d tools, got %d", len(allTools), len(manifestEntries))
			}

			// Every tool name must appear in manifest text
			for _, tool := range allTools {
				if !strings.Contains(manifest, tool.Name) {
					t.Errorf("Manifest missing tool %q — agent won't know it exists", tool.Name)
				}
			}

			// --- Verify LLM gets only a filtered subset ---
			if len(llmTools) >= len(allTools) {
				t.Errorf("LLM should get filtered tools (%d), not all %d", len(llmTools), len(allTools))
			}
			if len(llmTools) > 10 {
				t.Errorf("LLM should get at most 10 tools (max_tools), got %d", len(llmTools))
			}

			// --- Verify manifest has summaries, not full descriptions ---
			for _, entry := range manifestEntries {
				if entry.Summary == "" {
					t.Errorf("Manifest entry for %q has empty summary", entry.Name)
				}
				// Summary should be shorter than full description (first sentence only)
				for _, tool := range allTools {
					if tool.Name == entry.Name && len(tool.Description) > 50 {
						if len(entry.Summary) >= len(tool.Description) {
							t.Errorf("Manifest summary for %q should be shorter than full description (%d >= %d)",
								entry.Name, len(entry.Summary), len(tool.Description))
						}
					}
				}
			}

			// --- Verify LLM tools have FULL definitions (not summaries) ---
			for _, llmTool := range llmTools {
				if llmTool.Description == "" {
					t.Errorf("LLM tool %q missing full description", llmTool.Name)
				}
				if llmTool.InputSchema == nil {
					t.Errorf("LLM tool %q missing input schema", llmTool.Name)
				}
			}

			// --- Verify filtered tools are a subset of all tools ---
			allToolNames := make(map[string]bool)
			for _, tool := range allTools {
				allToolNames[tool.Name] = true
			}
			for _, llmTool := range llmTools {
				if !allToolNames[llmTool.Name] {
					t.Errorf("LLM tool %q not in master tool list — phantom tool!", llmTool.Name)
				}
			}

			// --- Log the split ---
			llmNames := toolNames(llmTools)
			t.Logf("Query: %s", sc.query)
			t.Logf("Manifest: %d tools (summaries in system prompt)", len(manifestEntries))
			t.Logf("LLM:      %d tools (full definitions sent to model): %v", len(llmTools), llmNames)
			t.Logf("Ratio:    %.0f%% of tools filtered out", float64(len(allTools)-len(llmTools))/float64(len(allTools))*100)
		})
	}
}
