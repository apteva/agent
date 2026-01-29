package tools

import (
	"fmt"
	"log"
)

type Tool interface {
	Name() string
	DisplayName() string // Human-readable name like "Create Task"
	Description() string
	InputSchema() map[string]interface{}
	Execute(params map[string]interface{}) (interface{}, error)
}

type ToolDefinition struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"-"` // Internal use only - not sent to LLMs
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ToolManifestEntry is a lightweight tool reference for on-demand loading
type ToolManifestEntry struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// GetDisplayName returns the display name for a built-in tool
// Returns empty string if tool not found (caller should check MCP tools or use fallback)
func GetDisplayName(name string) string {
	if tool, err := GetTool(name); err == nil {
		return tool.DisplayName()
	}
	// Return empty so caller can check MCP tools before falling back to title-case
	return ""
}

// toTitleCase converts "create-task" to "Create Task"
func toTitleCase(s string) string {
	words := make([]byte, 0, len(s))
	capitalizeNext := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '-' || c == '_' {
			words = append(words, ' ')
			capitalizeNext = true
		} else if capitalizeNext && c >= 'a' && c <= 'z' {
			words = append(words, c-32) // uppercase
			capitalizeNext = false
		} else {
			words = append(words, c)
			capitalizeNext = false
		}
	}
	return string(words)
}

type Registry struct {
	tools map[string]Tool
}

var globalRegistry *Registry

func init() {
	globalRegistry = NewRegistry()
	// Register built-in tools
	globalRegistry.RegisterTool(&SendNotificationTool{})
	globalRegistry.RegisterTool(&GetTimeTool{})
	globalRegistry.RegisterTool(&WaitToolWrapper{})
	globalRegistry.RegisterTool(&GenerateTestImageTool{})
	globalRegistry.RegisterTool(&AnalyzeImageURLTool{})
	globalRegistry.RegisterTool(&BrowserTool{})
	globalRegistry.RegisterTool(&DocumentSearchTool{})
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

func (r *Registry) RegisterTool(tool Tool) {
	r.tools[tool.Name()] = tool
}

func (r *Registry) GetTool(name string) (Tool, error) {
	tool, exists := r.tools[name]
	if !exists {
		return nil, fmt.Errorf("tool '%s' not found", name)
	}
	return tool, nil
}

func (r *Registry) ListTools() []ToolDefinition {
	var definitions []ToolDefinition
	for _, tool := range r.tools {
		definitions = append(definitions, ToolDefinition{
			Name:        tool.Name(),
			DisplayName: tool.DisplayName(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}
	return definitions
}

func (r *Registry) GetToolsForNames(names []string) []ToolDefinition {
	var definitions []ToolDefinition
	for _, name := range names {
		if tool, exists := r.tools[name]; exists {
			definitions = append(definitions, ToolDefinition{
				Name:        tool.Name(),
				DisplayName: tool.DisplayName(),
				Description: tool.Description(),
				InputSchema: tool.InputSchema(),
			})
		} else {
			log.Printf("⚠️  Tool '%s' requested but not found in registry. Available tools: %v", name, r.ListToolNames())
		}
	}
	return definitions
}

func (r *Registry) ListToolNames() []string {
	var names []string
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// GetToolManifest returns lightweight tool entries for on-demand loading
func (r *Registry) GetToolManifest() []ToolManifestEntry {
	var entries []ToolManifestEntry
	for _, tool := range r.tools {
		entries = append(entries, ToolManifestEntry{
			Name:    tool.Name(),
			Summary: GetSummary(tool),
		})
	}
	return entries
}

// GetSummary extracts first sentence from tool description for manifests
func GetSummary(tool Tool) string {
	return extractFirstSentence(tool.Description())
}

// extractFirstSentence gets the first sentence from a description
func extractFirstSentence(desc string) string {
	// Find first period, question mark, or newline
	for i, c := range desc {
		if c == '.' || c == '?' || c == '\n' {
			if i > 0 {
				return desc[:i]
			}
		}
		// Cap at ~100 chars if no sentence break
		if i > 100 {
			// Find last space before 100
			for j := i; j > 50; j-- {
				if desc[j] == ' ' {
					return desc[:j] + "..."
				}
			}
			return desc[:100] + "..."
		}
	}
	return desc
}

// Global functions for easy access
func GetGlobalRegistry() *Registry {
	return globalRegistry
}

func GetTool(name string) (Tool, error) {
	return globalRegistry.GetTool(name)
}

func GetToolsForNames(names []string) []ToolDefinition {
	return globalRegistry.GetToolsForNames(names)
}