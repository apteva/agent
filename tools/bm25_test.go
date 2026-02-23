package tools

import (
	"testing"
)

// --- Tokenizer Tests ---

func TestTokenize(t *testing.T) {
	t.Run("basic text", func(t *testing.T) {
		tokens := tokenize("Check server health metrics")
		expected := []string{"check", "server", "health", "metrics"}
		assertStringSliceEqual(t, expected, tokens)
	})

	t.Run("filters short tokens", func(t *testing.T) {
		tokens := tokenize("a to be or not")
		// "a" is filtered (len < 2), "to", "be", "or" are kept, "not" is kept
		for _, tok := range tokens {
			if len(tok) < 2 {
				t.Errorf("Token %q should have been filtered (len < 2)", tok)
			}
		}
	})

	t.Run("splits on special characters", func(t *testing.T) {
		tokens := tokenize("stripe:create-customer_v2")
		// Should split on : - _
		assertContains(t, tokens, "stripe")
		assertContains(t, tokens, "create")
		assertContains(t, tokens, "customer")
		assertContains(t, tokens, "v2")
	})

	t.Run("lowercases everything", func(t *testing.T) {
		tokens := tokenize("Check SERVER Health")
		for _, tok := range tokens {
			if tok != toLower(tok) {
				t.Errorf("Token %q should be lowercase", tok)
			}
		}
	})

	t.Run("empty input", func(t *testing.T) {
		tokens := tokenize("")
		if len(tokens) != 0 {
			t.Errorf("Expected empty tokens, got %v", tokens)
		}
	})
}

// --- BM25 Index Tests ---

func TestNewBM25IndexFromTools(t *testing.T) {
	tools := createTestToolSet()
	idx := NewBM25IndexFromTools(tools)

	t.Run("indexes all tools", func(t *testing.T) {
		if len(idx.documents) != len(tools) {
			t.Errorf("Expected %d documents, got %d", len(tools), len(idx.documents))
		}
	})

	t.Run("computes average doc length", func(t *testing.T) {
		if idx.avgDocLen <= 0 {
			t.Error("Average doc length should be > 0")
		}
	})

	t.Run("builds document frequencies", func(t *testing.T) {
		if len(idx.docFreqs) == 0 {
			t.Error("Document frequencies should not be empty")
		}
	})

	t.Run("uses correct BM25 parameters", func(t *testing.T) {
		if idx.k1 != 1.5 {
			t.Errorf("Expected k1=1.5, got %f", idx.k1)
		}
		if idx.b != 0.75 {
			t.Errorf("Expected b=0.75, got %f", idx.b)
		}
	})
}

func TestBM25Score(t *testing.T) {
	tools := createTestToolSet()
	idx := NewBM25IndexFromTools(tools)

	t.Run("relevant query scores higher than irrelevant", func(t *testing.T) {
		query := tokenize("server health CPU memory")
		var healthScore, paymentScore float64

		for _, doc := range idx.documents {
			s := idx.score(query, doc)
			if doc.toolName == "check-server-health" {
				healthScore = s
			}
			if doc.toolName == "stripe:create-payment" {
				paymentScore = s
			}
		}

		if healthScore <= paymentScore {
			t.Errorf("Health tool (%.3f) should score higher than payment tool (%.3f) for server health query",
				healthScore, paymentScore)
		}
	})

	t.Run("exact term match gives positive score", func(t *testing.T) {
		// "deployment" tokenizes to "deployment", so query must match
		query := tokenize("deployment status")
		for _, doc := range idx.documents {
			if doc.toolName == "get-deployment-status" {
				s := idx.score(query, doc)
				if s <= 0 {
					t.Errorf("Deployment query should score > 0 for deployment tool, got %.3f", s)
				}
			}
		}
	})

	t.Run("completely unrelated query scores zero", func(t *testing.T) {
		query := tokenize("xyzzyplugh") // nonsense
		for _, doc := range idx.documents {
			s := idx.score(query, doc)
			if s != 0 {
				t.Errorf("Nonsense query should score 0 for %s, got %.3f", doc.toolName, s)
			}
		}
	})
}

func TestBM25GetTopK(t *testing.T) {
	tools := createTestToolSet()
	idx := NewBM25IndexFromTools(tools)

	t.Run("returns relevant tools for server query", func(t *testing.T) {
		results := idx.GetTopK("check server health status", 5, 0.5, tools)
		if len(results) == 0 {
			t.Fatal("Expected at least one result for server health query")
		}
		// check-server-health should be in results
		found := false
		for _, r := range results {
			if r.Name == "check-server-health" {
				found = true
				break
			}
		}
		if !found {
			names := toolNames(results)
			t.Errorf("Expected check-server-health in results, got %v", names)
		}
	})

	t.Run("returns payment tools for payment query", func(t *testing.T) {
		results := idx.GetTopK("create a payment charge for customer", 5, 0.5, tools)
		if len(results) == 0 {
			t.Fatal("Expected at least one result for payment query")
		}
		foundPayment := false
		for _, r := range results {
			if r.Name == "stripe:create-payment" || r.Name == "stripe:list-charges" {
				foundPayment = true
				break
			}
		}
		if !foundPayment {
			names := toolNames(results)
			t.Errorf("Expected stripe payment tool in results, got %v", names)
		}
	})

	t.Run("respects topK limit", func(t *testing.T) {
		results := idx.GetTopK("tool", 3, 0.0, tools)
		if len(results) > 3 {
			t.Errorf("Expected at most 3 results, got %d", len(results))
		}
	})

	t.Run("respects threshold", func(t *testing.T) {
		// Very high threshold should return fewer or no results
		highThreshold := idx.GetTopK("server", 10, 100.0, tools)
		lowThreshold := idx.GetTopK("server", 10, 0.1, tools)

		if len(highThreshold) >= len(lowThreshold) && len(lowThreshold) > 0 {
			t.Errorf("High threshold (%d) should return fewer results than low threshold (%d)",
				len(highThreshold), len(lowThreshold))
		}
	})

	t.Run("returns empty for nonsense query", func(t *testing.T) {
		results := idx.GetTopK("xyzzyplugh", 5, 0.5, tools)
		if len(results) != 0 {
			t.Errorf("Expected 0 results for nonsense query, got %d", len(results))
		}
	})

	t.Run("returns empty for empty query", func(t *testing.T) {
		results := idx.GetTopK("", 5, 0.5, tools)
		if len(results) != 0 {
			t.Errorf("Expected 0 results for empty query, got %d", len(results))
		}
	})

	t.Run("MCP namespaced tools are searchable", func(t *testing.T) {
		results := idx.GetTopK("stripe customer subscription", 5, 0.5, tools)
		foundMCP := false
		for _, r := range results {
			if r.Name == "stripe:create-customer" || r.Name == "stripe:manage-subscription" {
				foundMCP = true
				break
			}
		}
		if !foundMCP {
			names := toolNames(results)
			t.Errorf("Expected MCP stripe tools for stripe query, got %v", names)
		}
	})
}

func TestBM25IndexTool(t *testing.T) {
	idx := &BM25Index{k1: 1.5, b: 0.75, docFreqs: make(map[string]int)}

	t.Run("indexes tool name parts", func(t *testing.T) {
		tool := ToolDefinition{
			Name:        "mcp__server__create-resource",
			Description: "Creates a new resource",
			InputSchema: map[string]interface{}{},
		}
		doc := idx.indexTool(tool)

		assertContains(t, doc.terms, "create")
		assertContains(t, doc.terms, "resource")
		assertContains(t, doc.terms, "server")
	})

	t.Run("indexes parameter names", func(t *testing.T) {
		tool := ToolDefinition{
			Name:        "search-logs",
			Description: "Search application logs",
			InputSchema: map[string]interface{}{
				"properties": map[string]interface{}{
					"severity": map[string]interface{}{"type": "string"},
					"service":  map[string]interface{}{"type": "string"},
					"timeframe": map[string]interface{}{"type": "string"},
				},
			},
		}
		doc := idx.indexTool(tool)

		assertContains(t, doc.terms, "severity")
		assertContains(t, doc.terms, "service")
		assertContains(t, doc.terms, "timeframe")
	})
}

// --- Helpers ---

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
}

func assertContains(t *testing.T, slice []string, item string) {
	t.Helper()
	for _, s := range slice {
		if s == item {
			return
		}
	}
	t.Errorf("Expected %v to contain %q", slice, item)
}

func assertStringSliceEqual(t *testing.T, expected, actual []string) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Errorf("Length mismatch: expected %d, got %d\nExpected: %v\nActual:   %v",
			len(expected), len(actual), expected, actual)
		return
	}
	for i := range expected {
		if expected[i] != actual[i] {
			t.Errorf("Mismatch at index %d: expected %q, got %q", i, expected[i], actual[i])
		}
	}
}

func toolNames(tools []ToolDefinition) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}
