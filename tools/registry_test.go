package tools

import (
	"testing"
)

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid name", "my_tool", "my_tool"},
		{"with spaces", "my tool", "my_tool"},
		{"with dots", "my.tool.name", "my_tool_name"},
		{"with special chars", "my@tool#name!", "my_tool_name_"},
		{"empty name", "", "unnamed_tool"},
		{"very long name", string(make([]byte, 200)), ""}, // Will be truncated
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeToolName(tt.input)
			if tt.name == "very long name" {
				if len(result) > 128 {
					t.Errorf("expected length <= 128, got %d", len(result))
				}
			} else if result != tt.expected {
				t.Errorf("SanitizeToolName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeSchemaForAnthropic_OneOf(t *testing.T) {
	schema := map[string]interface{}{
		"oneOf": []interface{}{
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"name"},
			},
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{"type": "integer"},
				},
			},
		},
	}

	result := SanitizeSchemaForAnthropic(schema)

	// Should use first option
	if result["type"] != "object" {
		t.Errorf("expected type=object, got %v", result["type"])
	}

	// oneOf should be removed
	if _, hasOneOf := result["oneOf"]; hasOneOf {
		t.Error("oneOf should have been removed")
	}

	// Should have properties from first option
	props, ok := result["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, hasName := props["name"]; !hasName {
		t.Error("expected 'name' property from first oneOf option")
	}
}

func TestSanitizeSchemaForAnthropic_AnyOf(t *testing.T) {
	schema := map[string]interface{}{
		"anyOf": []interface{}{
			map[string]interface{}{
				"type": "string",
			},
			map[string]interface{}{
				"type": "number",
			},
		},
	}

	result := SanitizeSchemaForAnthropic(schema)

	// anyOf should be removed
	if _, hasAnyOf := result["anyOf"]; hasAnyOf {
		t.Error("anyOf should have been removed")
	}

	// Should use first option type
	if result["type"] != "string" {
		t.Errorf("expected type=string from first anyOf option, got %v", result["type"])
	}
}

func TestSanitizeSchemaForAnthropic_AllOf(t *testing.T) {
	schema := map[string]interface{}{
		"allOf": []interface{}{
			map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"name"},
			},
			map[string]interface{}{
				"properties": map[string]interface{}{
					"age": map[string]interface{}{"type": "integer"},
				},
				"required": []interface{}{"age"},
			},
		},
	}

	result := SanitizeSchemaForAnthropic(schema)

	// allOf should be removed
	if _, hasAllOf := result["allOf"]; hasAllOf {
		t.Error("allOf should have been removed")
	}

	// Should merge properties
	props, ok := result["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, hasName := props["name"]; !hasName {
		t.Error("expected 'name' property merged from allOf")
	}
	if _, hasAge := props["age"]; !hasAge {
		t.Error("expected 'age' property merged from allOf")
	}

	// Should merge required
	required, ok := result["required"].([]string)
	if !ok {
		t.Fatal("expected required array")
	}
	if len(required) != 2 {
		t.Errorf("expected 2 required fields, got %d", len(required))
	}
}

func TestSanitizeSchemaForAnthropic_NestedOneOf(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"data": map[string]interface{}{
				"oneOf": []interface{}{
					map[string]interface{}{"type": "string"},
					map[string]interface{}{"type": "number"},
				},
			},
		},
	}

	result := SanitizeSchemaForAnthropic(schema)

	props, ok := result["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map")
	}

	dataProp, ok := props["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data property map")
	}

	// Nested oneOf should be removed
	if _, hasOneOf := dataProp["oneOf"]; hasOneOf {
		t.Error("nested oneOf should have been removed")
	}

	// Should use first option type
	if dataProp["type"] != "string" {
		t.Errorf("expected type=string from first oneOf option, got %v", dataProp["type"])
	}
}

func TestSanitizeSchemaForAnthropic_ArrayItems(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"items": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"oneOf": []interface{}{
						map[string]interface{}{"type": "string"},
						map[string]interface{}{"type": "number"},
					},
				},
			},
		},
	}

	result := SanitizeSchemaForAnthropic(schema)

	props := result["properties"].(map[string]interface{})
	itemsProp := props["items"].(map[string]interface{})
	arrayItems := itemsProp["items"].(map[string]interface{})

	// oneOf in array items should be removed
	if _, hasOneOf := arrayItems["oneOf"]; hasOneOf {
		t.Error("oneOf in array items should have been removed")
	}

	if arrayItems["type"] != "string" {
		t.Errorf("expected type=string from first oneOf option, got %v", arrayItems["type"])
	}
}

func TestSanitizeSchemaForAnthropic_NilSchema(t *testing.T) {
	result := SanitizeSchemaForAnthropic(nil)

	if result["type"] != "object" {
		t.Errorf("expected type=object for nil schema, got %v", result["type"])
	}

	if result["properties"] == nil {
		t.Error("expected empty properties map for nil schema")
	}
}

func TestSanitizeSchemaForAnthropic_NoModification(t *testing.T) {
	original := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
			"age":  map[string]interface{}{"type": "integer"},
		},
		"required": []interface{}{"name"},
	}

	// Make a copy to verify original isn't modified
	originalCopy := deepCopyMap(original)

	result := SanitizeSchemaForAnthropic(original)

	// Result should equal original (no changes needed)
	if result["type"] != "object" {
		t.Errorf("expected type=object, got %v", result["type"])
	}

	// Original should not be modified
	if original["type"] != originalCopy["type"] {
		t.Error("original schema was modified")
	}
}

func TestDeepCopyMap(t *testing.T) {
	original := map[string]interface{}{
		"nested": map[string]interface{}{
			"value": "test",
		},
		"array": []interface{}{"a", "b"},
	}

	copy := deepCopyMap(original)

	// Modify the copy
	copy["nested"].(map[string]interface{})["value"] = "modified"

	// Original should not be affected
	if original["nested"].(map[string]interface{})["value"] != "test" {
		t.Error("deepCopyMap did not create independent copy")
	}
}
