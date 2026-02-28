package tools

import (
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/apteva/agent/config"
)

// ToolLoader handles on-demand tool loading based on configured strategy
type ToolLoader struct {
	config   *config.ToolLoadingConfig
	registry *Registry
	bm25     *BM25Index
}

// NewToolLoader creates a new tool loader with the given config
func NewToolLoader(cfg *config.ToolLoadingConfig, registry *Registry) *ToolLoader {
	loader := &ToolLoader{
		config:   cfg,
		registry: registry,
	}

	// Initialize BM25 index if strategy is bm25
	if cfg != nil && cfg.Strategy == "bm25" {
		loader.bm25 = NewBM25Index(registry)
	}

	return loader
}

// LoadTools returns tool definitions based on the configured strategy and user message
// If mode is "full" or not configured, returns all tools
func (l *ToolLoader) LoadTools(userMessage string, allTools []ToolDefinition) []ToolDefinition {
	// Default to full mode
	if l.config == nil || l.config.Mode == "" || l.config.Mode == "full" {
		return allTools
	}

	if l.config.Mode != "on_demand" {
		return allTools
	}

	// Build map of all tools for quick lookup
	toolMap := make(map[string]ToolDefinition)
	for _, t := range allTools {
		toolMap[t.Name] = t
	}

	// Start with always-loaded tools
	loadedTools := make(map[string]ToolDefinition)
	for _, name := range l.config.AlwaysLoad {
		if t, ok := toolMap[name]; ok {
			loadedTools[t.Name] = t
		} else if matches := l.expandGlob(name, toolMap); len(matches) > 0 {
			for _, m := range matches {
				loadedTools[m.Name] = m
			}
		}
	}

	// Apply strategy
	var strategyTools []ToolDefinition
	switch l.config.Strategy {
	case "regex":
		strategyTools = l.loadByRegex(userMessage, toolMap)
	case "bm25":
		strategyTools = l.loadByBM25(userMessage, allTools)
	case "keyword":
		strategyTools = l.loadByKeyword(userMessage, toolMap)
	default:
		// Unknown strategy, return all
		log.Printf("⚠️  Unknown tool loading strategy: %s, falling back to full", l.config.Strategy)
		return allTools
	}

	// Merge strategy results
	for _, t := range strategyTools {
		loadedTools[t.Name] = t
	}

	// Convert map to slice (sorted by name for cache stability)
	result := make([]ToolDefinition, 0, len(loadedTools))
	for _, t := range loadedTools {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	// Apply max tools cap
	maxTools := l.config.MaxTools
	if maxTools <= 0 {
		maxTools = 15 // Default
	}
	if len(result) > maxTools {
		result = result[:maxTools]
	}

	log.Printf("🔧 Tool loading: mode=%s, strategy=%s, loaded=%d/%d tools",
		l.config.Mode, l.config.Strategy, len(result), len(allTools))

	return result
}

// loadByRegex matches user message against regex patterns
func (l *ToolLoader) loadByRegex(message string, toolMap map[string]ToolDefinition) []ToolDefinition {
	if l.config.Regex == nil {
		return nil
	}

	var result []ToolDefinition
	loaded := make(map[string]bool)

	for _, rule := range l.config.Regex.Rules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			log.Printf("⚠️  Invalid regex pattern: %s, error: %v", rule.Pattern, err)
			continue
		}

		if re.MatchString(message) {
			for _, toolPattern := range rule.Tools {
				if matches := l.expandGlob(toolPattern, toolMap); len(matches) > 0 {
					for _, t := range matches {
						if !loaded[t.Name] {
							loaded[t.Name] = true
							result = append(result, t)
						}
					}
				}
			}
		}
	}

	return result
}

// loadByBM25 uses BM25 scoring to find relevant tools
func (l *ToolLoader) loadByBM25(message string, allTools []ToolDefinition) []ToolDefinition {
	if l.bm25 == nil {
		log.Printf("🔍 BM25: index is nil")
		return nil
	}

	// Use defaults if BM25 config not provided
	topK := 5
	threshold := 1.5 // Balanced threshold

	if l.config.BM25 != nil {
		if l.config.BM25.TopK > 0 {
			topK = l.config.BM25.TopK
		}
		if l.config.BM25.Threshold > 0 {
			threshold = l.config.BM25.Threshold
		}
	}

	log.Printf("🔍 BM25 search: query=%q, topK=%d, threshold=%.2f", message, topK, threshold)
	result := l.bm25.GetTopK(message, topK, threshold, allTools)
	log.Printf("🔍 BM25 results: %d tools matched", len(result))
	return result
}

// loadByKeyword matches user message against keyword lists
func (l *ToolLoader) loadByKeyword(message string, toolMap map[string]ToolDefinition) []ToolDefinition {
	if l.config.Keyword == nil {
		return nil
	}

	var result []ToolDefinition
	loaded := make(map[string]bool)
	messageLower := strings.ToLower(message)

	for _, rule := range l.config.Keyword.Rules {
		matched := false
		for _, keyword := range rule.Keywords {
			if strings.Contains(messageLower, strings.ToLower(keyword)) {
				matched = true
				break
			}
		}

		if matched {
			for _, toolPattern := range rule.Tools {
				if matches := l.expandGlob(toolPattern, toolMap); len(matches) > 0 {
					for _, t := range matches {
						if !loaded[t.Name] {
							loaded[t.Name] = true
							result = append(result, t)
						}
					}
				}
			}
		}
	}

	return result
}

// expandGlob expands a tool pattern like "stripe:*" to matching tools
func (l *ToolLoader) expandGlob(pattern string, toolMap map[string]ToolDefinition) []ToolDefinition {
	// Direct match
	if t, ok := toolMap[pattern]; ok {
		return []ToolDefinition{t}
	}

	// Glob pattern (e.g., "stripe:*")
	if strings.Contains(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		var matches []ToolDefinition
		for name, t := range toolMap {
			if strings.HasPrefix(name, prefix) {
				matches = append(matches, t)
			}
		}
		// Sort for deterministic ordering (cache stability)
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].Name < matches[j].Name
		})
		return matches
	}

	return nil
}

// GetToolManifest returns a lightweight list of all available tools
func (l *ToolLoader) GetToolManifest(allTools []ToolDefinition) []ToolManifestEntry {
	entries := make([]ToolManifestEntry, len(allTools))
	for i, t := range allTools {
		entries[i] = ToolManifestEntry{
			Name:    t.Name,
			Summary: extractFirstSentence(t.Description),
		}
	}
	return entries
}

// GenerateManifestPrompt creates a system prompt section listing available tools
func (l *ToolLoader) GenerateManifestPrompt(allTools []ToolDefinition) string {
	if l.config == nil || l.config.Mode != "on_demand" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Available Tools\n")
	sb.WriteString("You have access to the following tools. If you need a tool not shown in detail below, mention it by name.\n\n")

	for _, t := range allTools {
		summary := extractFirstSentence(t.Description)
		sb.WriteString("- **")
		sb.WriteString(t.Name)
		sb.WriteString("**: ")
		sb.WriteString(summary)
		sb.WriteString("\n")
	}

	return sb.String()
}

// Global loader instance
var globalLoader *ToolLoader

// InitToolLoader initializes the global tool loader
func InitToolLoader(cfg *config.ToolLoadingConfig) {
	globalLoader = NewToolLoader(cfg, globalRegistry)
}

// GetToolLoader returns the global tool loader
func GetToolLoader() *ToolLoader {
	return globalLoader
}

// GenerateToolManifest creates a concise list of all available tools for system prompt injection
func GenerateToolManifest(allTools []ToolDefinition) string {
	if len(allTools) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, t := range allTools {
		summary := extractFirstSentence(t.Description)
		sb.WriteString("- **")
		sb.WriteString(t.Name)
		sb.WriteString("**: ")
		sb.WriteString(summary)
		sb.WriteString("\n")
	}
	return sb.String()
}

// LoadToolsForMessage is a convenience function to load tools for a user message
// It reads the current config each time to pick up runtime changes
func LoadToolsForMessage(userMessage string, allTools []ToolDefinition) []ToolDefinition {
	// Get current config (may have been updated at runtime)
	cfg := config.GetConfig().GetLLMConfig().ToolLoading

	// If no config or full mode, return all tools
	if cfg == nil || cfg.Mode == "" || cfg.Mode == "full" {
		return allTools
	}

	// Create a temporary loader with current config
	// Use nil registry since we'll build BM25 index from allTools directly
	loader := NewToolLoaderWithTools(cfg, allTools)
	return loader.LoadTools(userMessage, allTools)
}

// NewToolLoaderWithTools creates a tool loader with BM25 index built from provided tools
func NewToolLoaderWithTools(cfg *config.ToolLoadingConfig, allTools []ToolDefinition) *ToolLoader {
	loader := &ToolLoader{
		config: cfg,
	}

	// Initialize BM25 index from all available tools (including MCP)
	if cfg != nil && cfg.Strategy == "bm25" {
		loader.bm25 = NewBM25IndexFromTools(allTools)
		log.Printf("🔍 BM25 index built with %d tools (including MCP)", len(allTools))
	}

	return loader
}
