package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// IsJSON checks if content appears to be JSON
func IsJSON(content string) bool {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return false
	}
	return (content[0] == '{' || content[0] == '[') &&
		(content[len(content)-1] == '}' || content[len(content)-1] == ']')
}

// PreprocessContent converts JSON to plain text for chunking
// Returns the flattened text and true if conversion happened
func PreprocessContent(content, mimeType, resourceName string) (string, bool) {
	isJSON := strings.Contains(mimeType, "json") || IsJSON(content)
	if !isJSON {
		return content, false
	}

	prose := ConvertJSONToProse(content, resourceName)
	if prose == "" {
		return content, false
	}

	return prose, true
}

// ConvertJSONToProse flattens ANY JSON to readable text lines
// Universal - works on any JSON structure without pattern matching
func ConvertJSONToProse(content string, resourceName string) string {
	content = strings.TrimSpace(content)

	var data interface{}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return ""
	}

	var lines []string
	flattenJSON(data, "", &lines)

	if len(lines) == 0 {
		return ""
	}

	return strings.Join(lines, "\n")
}

// flattenJSON recursively converts any JSON to text lines
func flattenJSON(data interface{}, prefix string, lines *[]string) {
	switch v := data.(type) {
	case []interface{}:
		// Array - process each item
		for i, item := range v {
			itemPrefix := prefix
			if itemPrefix != "" {
				itemPrefix += "."
			}
			itemPrefix += fmt.Sprintf("[%d]", i)

			// For array items that are objects, try to get a meaningful one-liner
			if obj, ok := item.(map[string]interface{}); ok {
				line := objectToLine(obj)
				if line != "" {
					*lines = append(*lines, line)
					continue
				}
			}
			flattenJSON(item, itemPrefix, lines)
		}

	case map[string]interface{}:
		// Check for common wrapper patterns with data arrays
		// e.g., {"success": true, "data": [...]} or {"items": [...], "total": 100}
		arrayKeys := []string{"data", "items", "results", "records", "tools", "list", "entries", "contents"}
		for _, key := range arrayKeys {
			if arr, ok := v[key].([]interface{}); ok {
				// Found a data array - flatten it
				flattenJSON(arr, prefix, lines)
				return
			}
		}

		// Check for single-field wrapper with any array
		if len(v) == 1 {
			for _, val := range v {
				if arr, ok := val.([]interface{}); ok {
					flattenJSON(arr, prefix, lines)
					return
				}
			}
		}

		// Multi-field object - flatten to one line if small, else recurse
		line := objectToLine(v)
		if line != "" && len(line) < 500 {
			*lines = append(*lines, line)
		} else {
			// Large object - flatten each field
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, key := range keys {
				newPrefix := key
				if prefix != "" {
					newPrefix = prefix + "." + key
				}
				flattenJSON(v[key], newPrefix, lines)
			}
		}

	case string:
		if v != "" {
			if prefix != "" {
				*lines = append(*lines, fmt.Sprintf("%s: %s", prefix, v))
			} else {
				*lines = append(*lines, v)
			}
		}

	case float64:
		val := fmt.Sprintf("%v", v)
		if v == float64(int64(v)) {
			val = fmt.Sprintf("%.0f", v)
		}
		if prefix != "" {
			*lines = append(*lines, fmt.Sprintf("%s: %s", prefix, val))
		} else {
			*lines = append(*lines, val)
		}

	case bool:
		if prefix != "" {
			*lines = append(*lines, fmt.Sprintf("%s: %v", prefix, v))
		} else {
			*lines = append(*lines, fmt.Sprintf("%v", v))
		}

	case nil:
		// Skip nulls
	}
}

// objectToLine converts a JSON object to a single readable line
// Extracts the most meaningful fields
func objectToLine(obj map[string]interface{}) string {
	var parts []string

	// Priority order for meaningful fields
	nameFields := []string{"name", "title", "label", "id", "key", "type"}
	descFields := []string{"description", "summary", "content", "text", "message", "body", "value"}

	// Get name-like field
	var name string
	for _, field := range nameFields {
		if val, ok := obj[field].(string); ok && val != "" {
			name = val
			break
		}
	}

	// Get description-like field
	var desc string
	for _, field := range descFields {
		if val, ok := obj[field].(string); ok && val != "" {
			desc = val
			break
		}
	}

	if name != "" {
		parts = append(parts, name)
	}
	if desc != "" {
		// Truncate long descriptions
		if len(desc) > 200 {
			desc = desc[:200] + "..."
		}
		parts = append(parts, desc)
	}

	// If we have name+desc, that's enough
	if len(parts) >= 1 {
		// Add a few extra fields for context
		extras := []string{}
		for key, val := range obj {
			// Skip already processed and complex fields
			if contains(nameFields, key) || contains(descFields, key) {
				continue
			}
			if strVal := simpleValue(val); strVal != "" && len(strVal) < 50 {
				extras = append(extras, fmt.Sprintf("%s=%s", key, strVal))
			}
		}
		if len(extras) > 0 {
			if len(extras) > 3 {
				extras = extras[:3]
			}
			parts = append(parts, "["+strings.Join(extras, ", ")+"]")
		}
		return strings.Join(parts, " - ")
	}

	// Fallback: flatten all simple fields
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if strVal := simpleValue(obj[key]); strVal != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", key, strVal))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	result := strings.Join(parts, ", ")
	if len(result) > 500 {
		result = result[:500] + "..."
	}
	return result
}

// simpleValue converts simple JSON values to string
func simpleValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		if len(v) > 100 {
			return v[:100] + "..."
		}
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%v", v)
	case bool:
		return fmt.Sprintf("%v", v)
	case nil:
		return ""
	default:
		return ""
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
