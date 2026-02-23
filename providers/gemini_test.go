package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeminiToolConfig_JSONKeys(t *testing.T) {
	cfg := GeminiToolConfig{
		FunctionDeclarations: []GeminiFunction{
			{
				Name:        "get_weather",
				Description: "Get weather for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "City name",
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Gemini API requires camelCase "functionDeclarations", not snake_case "function_declarations"
	if !strings.Contains(jsonStr, `"functionDeclarations"`) {
		t.Errorf("expected camelCase 'functionDeclarations', got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"function_declarations"`) {
		t.Errorf("found snake_case 'function_declarations' — Gemini API will silently ignore tools: %s", jsonStr)
	}
}

func TestGeminiPart_JSONKeys(t *testing.T) {
	part := GeminiPart{
		InlineData: &GeminiInlineData{
			MimeType: "image/png",
			Data:     "base64data",
		},
	}

	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Gemini API requires camelCase "inlineData", not snake_case "inline_data"
	if !strings.Contains(jsonStr, `"inlineData"`) {
		t.Errorf("expected camelCase 'inlineData', got: %s", jsonStr)
	}
	if strings.Contains(jsonStr, `"inline_data"`) {
		t.Errorf("found snake_case 'inline_data' — Gemini API will ignore media: %s", jsonStr)
	}

	// Also verify mimeType is camelCase
	if !strings.Contains(jsonStr, `"mimeType"`) {
		t.Errorf("expected camelCase 'mimeType', got: %s", jsonStr)
	}
}

func TestGeminiPart_FunctionCall_JSONKeys(t *testing.T) {
	part := GeminiPart{
		FunctionCall: &GeminiFunctionCall{
			Name: "get_weather",
			Args: map[string]interface{}{"location": "Berlin"},
		},
	}

	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	if !strings.Contains(jsonStr, `"functionCall"`) {
		t.Errorf("expected camelCase 'functionCall', got: %s", jsonStr)
	}
}

func TestGeminiPart_FunctionResponse_JSONKeys(t *testing.T) {
	part := GeminiPart{
		FunctionResponse: &GeminiFunctionResponse{
			Name:     "get_weather",
			Response: map[string]interface{}{"temp": 22},
		},
	}

	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	if !strings.Contains(jsonStr, `"functionResponse"`) {
		t.Errorf("expected camelCase 'functionResponse', got: %s", jsonStr)
	}
}

func TestGeminiRequest_FullSerialization(t *testing.T) {
	req := GeminiRequest{
		Contents: []GeminiContent{
			{
				Role: "user",
				Parts: []GeminiPart{
					{Text: "What's the weather?"},
				},
			},
		},
		Tools: []GeminiToolConfig{
			{
				FunctionDeclarations: []GeminiFunction{
					{
						Name:        "get_weather",
						Description: "Get weather",
						Parameters:  map[string]interface{}{"type": "object"},
					},
				},
			},
		},
		GenerationConfig: &GeminiGenerationConfig{
			MaxOutputTokens: 1024,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify the full request uses correct Gemini API field names
	checks := map[string]string{
		"functionDeclarations": "tools[].functionDeclarations",
		"generationConfig":     "generationConfig",
		"maxOutputTokens":      "generationConfig.maxOutputTokens",
	}

	for key, context := range checks {
		if !strings.Contains(jsonStr, `"`+key+`"`) {
			t.Errorf("missing camelCase key %q (%s) in request JSON", key, context)
		}
	}

	// Verify no snake_case leaks
	snakeChecks := []string{"function_declarations", "inline_data", "max_output_tokens", "generation_config"}
	for _, key := range snakeChecks {
		if strings.Contains(jsonStr, `"`+key+`"`) {
			t.Errorf("found snake_case key %q — Gemini API uses camelCase", key)
		}
	}
}

func TestCleanSchemaForGemini(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":     "string",
				"examples": []string{"hello"},
			},
		},
		"$schema":  "http://json-schema.org/draft-07/schema#",
		"required": []string{"query"},
	}

	cleaned := cleanSchemaForGemini(schema)

	if _, ok := cleaned["$schema"]; ok {
		t.Error("$schema should be removed")
	}
	if _, ok := cleaned["required"]; !ok {
		t.Error("required should be preserved")
	}

	props := cleaned["properties"].(map[string]interface{})
	queryProp := props["query"].(map[string]interface{})
	if _, ok := queryProp["examples"]; ok {
		t.Error("examples should be removed from nested properties")
	}
	if queryProp["type"] != "string" {
		t.Error("type should be preserved")
	}
}
