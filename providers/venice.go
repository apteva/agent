package providers

import (
	"io"
	"log"
	"os"
	"strings"

	"agent-server/config"
	"agent-server/stream"
	"agent-server/tools"
)

// VeniceProvider wraps the OpenAI-compatible base provider with Venice AI configuration
// Venice AI provides uncensored and private AI models
// API Documentation: https://docs.venice.ai/
type VeniceProvider struct {
	*OpenAICompatibleProvider
}

// NewVeniceProvider creates a new Venice AI provider instance
// Requires VENICE_API_KEY environment variable to be set
//
// Popular models:
// - zai-org-glm-4.7 (GLM 4.7 Private, 203K context, $0.20/M in, $0.90/M out)
// - venice-uncensored (Uncensored model, no tool call support)
func NewVeniceProvider() *VeniceProvider {
	apiKey := os.Getenv("VENICE_API_KEY")
	return &VeniceProvider{
		OpenAICompatibleProvider: NewOpenAICompatibleProvider(
			apiKey,
			"https://api.venice.ai/api/v1",
			"Authorization",
			"Bearer ",
			"VENICE_API_KEY",
		),
	}
}

// veniceModelSupportsTools checks if a Venice model supports tool calls
func veniceModelSupportsTools(model string) bool {
	model = strings.ToLower(model)
	if strings.Contains(model, "venice-uncensored") {
		return false
	}
	return true
}

// GetRawStream overrides the base provider to strip tools for models that don't support them
func (p *VeniceProvider) GetRawStream(messages []stream.Message, customTools []tools.ToolDefinition, builtinTools []interface{}) (io.ReadCloser, error) {
	cfg := config.GetConfig()
	model := cfg.GetLLMConfig().Model

	if !veniceModelSupportsTools(model) {
		log.Printf("Venice: model %s does not support tool calls, stripping tools", model)
		return p.OpenAICompatibleProvider.GetRawStream(messages, nil, nil)
	}

	return p.OpenAICompatibleProvider.GetRawStream(messages, customTools, builtinTools)
}
