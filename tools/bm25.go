package tools

import (
	"log"
	"math"
	"regexp"
	"sort"
	"strings"
)

// BM25Index implements BM25 ranking for tool relevance scoring
type BM25Index struct {
	documents []toolDocument
	avgDocLen float64
	docFreqs  map[string]int // term -> number of docs containing term
	k1        float64        // term saturation parameter (default: 1.5)
	b         float64        // length normalization parameter (default: 0.75)
}

type toolDocument struct {
	toolName string
	terms    []string
	termFreq map[string]int
	length   int
}

type scoredTool struct {
	name  string
	score float64
}

// NewBM25Index creates a new BM25 index from the tool registry
func NewBM25Index(registry *Registry) *BM25Index {
	tools := registry.ListTools()
	return NewBM25IndexFromTools(tools)
}

// NewBM25IndexFromTools creates a new BM25 index from a list of tool definitions
func NewBM25IndexFromTools(tools []ToolDefinition) *BM25Index {
	idx := &BM25Index{
		docFreqs: make(map[string]int),
		k1:       1.5,
		b:        0.75,
	}

	totalLen := 0

	for _, tool := range tools {
		doc := idx.indexTool(tool)
		idx.documents = append(idx.documents, doc)
		totalLen += doc.length

		// Update document frequencies
		seen := make(map[string]bool)
		for _, term := range doc.terms {
			if !seen[term] {
				idx.docFreqs[term]++
				seen[term] = true
			}
		}
	}

	if len(idx.documents) > 0 {
		idx.avgDocLen = float64(totalLen) / float64(len(idx.documents))
	}

	return idx
}

// indexTool creates a document from a tool definition
func (idx *BM25Index) indexTool(tool ToolDefinition) toolDocument {
	// Combine tool name, description, and parameter names into searchable text
	var textParts []string

	// Tool name (split on : and - for MCP tools like "stripe:create-customer")
	nameParts := regexp.MustCompile(`[:\-_]`).Split(tool.Name, -1)
	textParts = append(textParts, nameParts...)

	// Description
	textParts = append(textParts, tool.Description)

	// Parameter names from input schema
	if props, ok := tool.InputSchema["properties"].(map[string]interface{}); ok {
		for paramName := range props {
			textParts = append(textParts, paramName)
		}
	}

	// Tokenize
	text := strings.Join(textParts, " ")
	terms := tokenize(text)

	// Build term frequency map
	termFreq := make(map[string]int)
	for _, term := range terms {
		termFreq[term]++
	}

	return toolDocument{
		toolName: tool.Name,
		terms:    terms,
		termFreq: termFreq,
		length:   len(terms),
	}
}

// tokenize splits text into lowercase terms
func tokenize(text string) []string {
	// Convert to lowercase
	text = strings.ToLower(text)

	// Split on non-alphanumeric characters
	re := regexp.MustCompile(`[^a-z0-9]+`)
	parts := re.Split(text, -1)

	// Filter empty strings and very short terms
	var terms []string
	for _, p := range parts {
		if len(p) >= 2 {
			terms = append(terms, p)
		}
	}

	return terms
}

// Score calculates BM25 score for a query against a document
func (idx *BM25Index) score(query []string, doc toolDocument) float64 {
	score := 0.0
	n := float64(len(idx.documents))

	for _, term := range query {
		// Term frequency in document
		tf := float64(doc.termFreq[term])
		if tf == 0 {
			continue
		}

		// Document frequency
		df := float64(idx.docFreqs[term])
		if df == 0 {
			continue
		}

		// IDF component
		idf := math.Log((n - df + 0.5) / (df + 0.5))

		// BM25 term score
		docLen := float64(doc.length)
		numerator := tf * (idx.k1 + 1)
		denominator := tf + idx.k1*(1-idx.b+idx.b*(docLen/idx.avgDocLen))

		score += idf * (numerator / denominator)
	}

	return score
}

// GetTopK returns the top K tools above the threshold for a query
func (idx *BM25Index) GetTopK(query string, k int, threshold float64, allTools []ToolDefinition) []ToolDefinition {
	queryTerms := tokenize(query)
	log.Printf("🔍 BM25 query terms: %v", queryTerms)
	if len(queryTerms) == 0 {
		log.Printf("🔍 BM25: no query terms after tokenization")
		return nil
	}

	// Score all documents
	var scored []scoredTool
	var allScores []scoredTool // for logging
	for _, doc := range idx.documents {
		s := idx.score(queryTerms, doc)
		allScores = append(allScores, scoredTool{name: doc.toolName, score: s})
		if s >= threshold {
			scored = append(scored, scoredTool{name: doc.toolName, score: s})
		}
	}

	// Sort all scores for logging
	sort.Slice(allScores, func(i, j int) bool {
		return allScores[i].score > allScores[j].score
	})

	// Log matched tools (above threshold only)
	log.Printf("🔍 BM25 matched %d tools (threshold=%.2f):", len(scored), threshold)
	for i, st := range scored {
		if i >= 10 {
			break
		}
		log.Printf("   %.3f %s", st.score, st.name)
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Take top K
	if len(scored) > k {
		scored = scored[:k]
	}

	// Build tool map for quick lookup
	toolMap := make(map[string]ToolDefinition)
	for _, t := range allTools {
		toolMap[t.Name] = t
	}

	// Return matching tool definitions
	var result []ToolDefinition
	for _, st := range scored {
		if t, ok := toolMap[st.name]; ok {
			result = append(result, t)
		}
	}

	return result
}

// RebuildIndex rebuilds the BM25 index (call after adding new tools)
func (idx *BM25Index) RebuildIndex(registry *Registry) {
	newIdx := NewBM25Index(registry)
	idx.documents = newIdx.documents
	idx.avgDocLen = newIdx.avgDocLen
	idx.docFreqs = newIdx.docFreqs
}
