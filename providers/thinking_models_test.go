package providers

import (
	"testing"
)

func TestIsThinkingModel(t *testing.T) {
	testCases := []struct {
		model    string
		expected bool
	}{
		// Kimi thinking models
		{"moonshotai/Kimi-K2-Thinking", true},
		{"moonshotai/Kimi-K2.5", true},
		{"kimi-k2-5", true},
		{"accounts/fireworks/models/kimi-k2-thinking", true},
		{"accounts/fireworks/models/kimi-k2p5", true},
		{"accounts/fireworks/models/kimi_k25", true},

		// GLM thinking models
		{"glm-4-thinking", true},
		{"glm-reasoning-thinking", true},

		// DeepSeek R1
		{"deepseek-ai/DeepSeek-R1", true},
		{"deepseek-r1", true},

		// MiniMax
		{"MiniMax-M2.5", true},
		{"accounts/fireworks/models/minimax-m2p5", true},
		{"minimax-something", true},

		// Qwen thinking
		{"qwen3-vl-235b-a22b-thinking", true},
		{"accounts/fireworks/models/qwen3-vl-235b-a22b-thinking", true},

		// Generic thinking/reasoning patterns
		{"some-model-thinking", true},
		{"another-model-reasoning", true},

		// Non-thinking models
		{"gpt-4o", false},
		{"gpt-5", false},
		{"claude-3-opus", false},
		{"llama-3.1-8b", false},
		{"mistral-large", false},
		{"gemini-2.5-pro", false},
		{"deepseek-v2", false}, // Not R1
		{"glm-4", false},       // Not thinking variant
	}

	for _, tc := range testCases {
		t.Run(tc.model, func(t *testing.T) {
			result := isThinkingModel(tc.model)
			if result != tc.expected {
				t.Errorf("isThinkingModel(%q) = %v, want %v", tc.model, result, tc.expected)
			}
		})
	}
}

func TestNeedsThinkTagsInContent(t *testing.T) {
	testCases := []struct {
		name     string
		baseURL  string
		model    string
		expected bool
	}{
		{
			name:     "Fireworks with thinking model - no think tags needed",
			baseURL:  "https://api.fireworks.ai/inference/v1",
			model:    "accounts/fireworks/models/minimax-m2p5",
			expected: false,
		},
		{
			name:     "Together with thinking model - no think tags needed",
			baseURL:  "https://api.together.xyz/v1",
			model:    "moonshotai/Kimi-K2-Thinking",
			expected: false,
		},
		{
			name:     "MiniMax direct - no think tags needed (uses reasoning_details)",
			baseURL:  "https://api.minimax.io/v1",
			model:    "MiniMax-M2.5",
			expected: false,
		},
		{
			name:     "Moonshot direct - no think tags needed",
			baseURL:  "https://api.moonshot.ai/v1",
			model:    "kimi-k2.5",
			expected: false,
		},
		{
			name:     "Unknown provider with thinking model - reconstruct think tags",
			baseURL:  "https://api.custom-provider.com/v1",
			model:    "custom-model-thinking",
			expected: true,
		},
		{
			name:     "Unknown provider with non-thinking model - no think tags",
			baseURL:  "https://api.custom-provider.com/v1",
			model:    "gpt-4o",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := needsThinkTagsInContent(tc.baseURL, tc.model)
			if result != tc.expected {
				t.Errorf("needsThinkTagsInContent(%q, %q) = %v, want %v", tc.baseURL, tc.model, result, tc.expected)
			}
		})
	}
}

func TestNeedsFixedTemperature(t *testing.T) {
	testCases := []struct {
		model    string
		expected bool
	}{
		{"kimi-k2.5", true},
		{"kimi-k2.5-preview", true},
		{"accounts/fireworks/models/kimi_k25", true},
		{"kimi-k2-thinking", true},
		{"gpt-4o", false},
		{"minimax-m2p5", false},
	}

	for _, tc := range testCases {
		t.Run(tc.model, func(t *testing.T) {
			result := needsFixedTemperature(tc.model)
			if result != tc.expected {
				t.Errorf("needsFixedTemperature(%q) = %v, want %v", tc.model, result, tc.expected)
			}
		})
	}
}
