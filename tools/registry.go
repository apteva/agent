package tools

import (
	"fmt"
	"log"
	"regexp"
)

// validToolNameRegex matches Anthropic's required pattern: ^[a-zA-Z0-9_-]{1,128}$
var validToolNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// SanitizeToolName ensures a tool name matches the Anthropic API pattern ^[a-zA-Z0-9_-]{1,128}$
// Invalid characters are replaced with underscores, and the name is truncated to 128 chars
func SanitizeToolName(name string) string {
	if validToolNameRegex.MatchString(name) {
		return name
	}

	// Replace invalid characters with underscores
	sanitized := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			sanitized = append(sanitized, c)
		} else {
			sanitized = append(sanitized, '_')
		}
	}

	// Truncate to 128 characters
	if len(sanitized) > 128 {
		sanitized = sanitized[:128]
	}

	// Ensure not empty
	if len(sanitized) == 0 {
		return "unnamed_tool"
	}

	return string(sanitized)
}

// SanitizeSchemaForAnthropic removes or transforms schema constructs not supported by Anthropic
// Anthropic doesn't support oneOf, allOf, or anyOf at the top level of input_schema
func SanitizeSchemaForAnthropic(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	// Deep copy to avoid modifying the original
	result := deepCopyMap(schema)

	// Handle oneOf at top level - use the first option
	if oneOf, ok := result["oneOf"].([]interface{}); ok && len(oneOf) > 0 {
		log.Printf("⚠️  Schema sanitizer: replacing top-level oneOf with first option")
		if firstOption, ok := oneOf[0].(map[string]interface{}); ok {
			// Replace with the first option
			delete(result, "oneOf")
			for k, v := range firstOption {
				result[k] = v
			}
		}
	}

	// Handle anyOf at top level - use the first option
	if anyOf, ok := result["anyOf"].([]interface{}); ok && len(anyOf) > 0 {
		log.Printf("⚠️  Schema sanitizer: replacing top-level anyOf with first option")
		if firstOption, ok := anyOf[0].(map[string]interface{}); ok {
			delete(result, "anyOf")
			for k, v := range firstOption {
				result[k] = v
			}
		}
	}

	// Handle allOf at top level - merge all options
	if allOf, ok := result["allOf"].([]interface{}); ok && len(allOf) > 0 {
		log.Printf("⚠️  Schema sanitizer: merging top-level allOf into single schema")
		delete(result, "allOf")
		mergedProps := make(map[string]interface{})
		var mergedRequired []string

		for _, item := range allOf {
			if itemMap, ok := item.(map[string]interface{}); ok {
				// Merge properties
				if props, ok := itemMap["properties"].(map[string]interface{}); ok {
					for k, v := range props {
						mergedProps[k] = v
					}
				}
				// Merge required
				if req, ok := itemMap["required"].([]interface{}); ok {
					for _, r := range req {
						if rs, ok := r.(string); ok {
							mergedRequired = append(mergedRequired, rs)
						}
					}
				}
			}
		}

		result["type"] = "object"
		if len(mergedProps) > 0 {
			result["properties"] = mergedProps
		}
		if len(mergedRequired) > 0 {
			result["required"] = mergedRequired
		}
	}

	// Recursively sanitize nested properties
	if props, ok := result["properties"].(map[string]interface{}); ok {
		for key, value := range props {
			if propSchema, ok := value.(map[string]interface{}); ok {
				props[key] = sanitizeNestedSchema(propSchema)
			}
		}
	}

	// Ensure type is set
	if _, hasType := result["type"]; !hasType {
		result["type"] = "object"
	}

	return result
}

// sanitizeNestedSchema handles oneOf/anyOf/allOf in nested properties
func sanitizeNestedSchema(schema map[string]interface{}) map[string]interface{} {
	result := deepCopyMap(schema)

	// Handle oneOf in nested schema - use first option
	if oneOf, ok := result["oneOf"].([]interface{}); ok && len(oneOf) > 0 {
		if firstOption, ok := oneOf[0].(map[string]interface{}); ok {
			delete(result, "oneOf")
			for k, v := range firstOption {
				result[k] = v
			}
		}
	}

	// Handle anyOf in nested schema - use first option
	if anyOf, ok := result["anyOf"].([]interface{}); ok && len(anyOf) > 0 {
		if firstOption, ok := anyOf[0].(map[string]interface{}); ok {
			delete(result, "anyOf")
			for k, v := range firstOption {
				result[k] = v
			}
		}
	}

	// Handle allOf in nested schema - merge
	if allOf, ok := result["allOf"].([]interface{}); ok && len(allOf) > 0 {
		delete(result, "allOf")
		for _, item := range allOf {
			if itemMap, ok := item.(map[string]interface{}); ok {
				for k, v := range itemMap {
					if k != "allOf" && k != "oneOf" && k != "anyOf" {
						result[k] = v
					}
				}
			}
		}
	}

	// Recursively handle nested properties
	if props, ok := result["properties"].(map[string]interface{}); ok {
		for key, value := range props {
			if propSchema, ok := value.(map[string]interface{}); ok {
				props[key] = sanitizeNestedSchema(propSchema)
			}
		}
	}

	// Handle items in arrays
	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = sanitizeNestedSchema(items)
	}

	return result
}

// deepCopyMap creates a deep copy of a map
func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		switch val := v.(type) {
		case map[string]interface{}:
			result[k] = deepCopyMap(val)
		case []interface{}:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// deepCopySlice creates a deep copy of a slice
func deepCopySlice(s []interface{}) []interface{} {
	result := make([]interface{}, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]interface{}:
			result[i] = deepCopyMap(val)
		case []interface{}:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

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

// DynamicDisplayNameProvider is an optional interface that tools can implement
// to provide context-specific display names based on their input parameters.
// For example, call_agent can return "Calling Jarvis" instead of generic "Call Agent".
type DynamicDisplayNameProvider interface {
	DynamicDisplayName(params map[string]interface{}) string
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

// GetDynamicDisplayName returns a context-specific display name for a tool if it supports it.
// Returns empty string if the tool doesn't implement DynamicDisplayNameProvider or isn't found.
func GetDynamicDisplayName(name string, params map[string]interface{}) string {
	if tool, err := GetTool(name); err == nil {
		if dyn, ok := tool.(DynamicDisplayNameProvider); ok {
			if dn := dyn.DynamicDisplayName(params); dn != "" {
				return dn
			}
		}
	}
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
	globalRegistry.RegisterTool(&PingTool{}) // No-input tool for testing
	globalRegistry.RegisterTool(&WaitToolWrapper{})
	globalRegistry.RegisterTool(&GenerateTestImageTool{})
	globalRegistry.RegisterTool(&GenerateTestPDFTool{})
	globalRegistry.RegisterTool(&AnalyzeImageURLTool{})
	// BrowserTool is registered dynamically via RegisterBrowserTool() when operator mode is enabled
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

// ListAllTools returns all registered tools with full details
func ListAllTools() []ToolDefinition {
	return globalRegistry.ListTools()
}