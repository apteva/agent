package stream

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/apteva/agent/config"
)

// Summarizer generates short thread titles and activity summaries using a small LLM
type Summarizer struct {
	db       *sql.DB
	provider string
	apiKey   string
	model    string
	endpoint string
}

// SummaryResult holds the generated title and activity
type SummaryResult struct {
	Title    string `json:"title"`
	Activity string `json:"activity"`
}

// NewSummarizer creates a new Summarizer instance that auto-detects the best provider
func NewSummarizer(db *sql.DB, mainProvider string) *Summarizer {
	provider := config.GetAvailableDecisionProvider(mainProvider)
	if provider == "" {
		log.Printf("⚠️  Summarizer: no provider available for thread activity")
		return &Summarizer{db: db}
	}

	apiKey := config.GetAPIKey(provider)
	model := config.GetSmallModel(provider)
	endpoint := config.GetCompletionEndpoint(provider)

	log.Printf("📝 Summarizer: using %s/%s for thread activity", provider, model)

	return &Summarizer{
		db:       db,
		provider: provider,
		apiKey:   apiKey,
		model:    model,
		endpoint: endpoint,
	}
}

// GenerateThreadSummary generates a title and activity line from recent messages.
// Returns title, activity, and error. Title may be empty if it shouldn't be updated.
func (s *Summarizer) GenerateThreadSummary(threadID string, messages []Message) (*SummaryResult, error) {
	if s.provider == "" || s.apiKey == "" {
		return nil, fmt.Errorf("no provider/API key configured for summarization")
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages to summarize")
	}

	isTaskThread := strings.HasPrefix(threadID, "task_") || strings.HasPrefix(threadID, "spawn_") || strings.HasPrefix(threadID, "scheduler_")

	// For task/scheduler threads, use more messages to capture what was actually done
	maxMessages := 6
	if isTaskThread {
		maxMessages = 12
	}

	// Take the last few messages to keep input small
	start := 0
	if len(messages) > maxMessages {
		start = len(messages) - maxMessages
	}
	recent := messages[start:]

	// Build a numbered conversation snippet so the LLM can see ordering
	var snippet strings.Builder
	totalMessages := len(messages)
	truncateLen := 500
	if isTaskThread {
		truncateLen = 800 // allow more detail for task threads
	}
	for i, msg := range recent {
		role := strings.ToUpper(msg.Role)
		content := extractTextForSummary(msg)
		if content != "" {
			// Truncate individual messages to keep tokens low
			if len(content) > truncateLen {
				content = content[:truncateLen] + "..."
			}
			msgNum := totalMessages - len(recent) + i + 1
			snippet.WriteString(fmt.Sprintf("#%d [%s]: %s\n", msgNum, role, content))
		}
	}
	snippet.WriteString(fmt.Sprintf("\n(Total messages: %d. Message #%d is the MOST RECENT.)\n", totalMessages, totalMessages))

	var prompt string
	if isTaskThread {
		prompt = fmt.Sprintf(`TASK EXECUTION THREAD:
%s
Summarize as a JSON object:
- "title": 3-6 word task goal
- "activity": 2-5 words about the outcome. Be specific but very short. Do NOT say "task completed" or "executed successfully".

Examples:
{"title": "Daily News Summary", "activity": "Fetched 5 AI articles"}
{"title": "Competitor Analysis", "activity": "Found 3 pricing changes"}
{"title": "Database Cleanup", "activity": "Deleted 234 expired sessions"}
{"title": "Email Campaign Draft", "activity": "Failed, auth error"}
{"title": "Dummy Task", "activity": "Ping test successful"}

Your JSON:`, snippet.String())
	} else {
		prompt = fmt.Sprintf(`CONVERSATION:
%s
Summarize this conversation as a JSON object with two fields:
- "title": 3-6 word thread topic
- "activity": 2-5 words about the LAST exchange only

Examples:
{"title": "PR Review Auth Module", "activity": "Reviewed auth PR"}
{"title": "Weather Forecast Query", "activity": "Fetched weather data"}
{"title": "Task Management", "activity": "Created recurring task"}

Your JSON:`, snippet.String())
	}

	return s.callAndParse(prompt)
}

// callAndParse calls the LLM API and parses the JSON response
func (s *Summarizer) callAndParse(prompt string) (*SummaryResult, error) {
	response, err := s.callAPI(prompt)
	if err != nil {
		return nil, fmt.Errorf("API call failed: %w", err)
	}

	// Strip <think>...</think> tags from reasoning models (Qwen3, DeepSeek, etc.)
	response = stripThinkTags(response)

	log.Printf("📝 DEBUG Summarizer raw response (%s/%s): %s", s.provider, s.model, truncate(response, 300))

	// Parse the JSON response
	var result SummaryResult
	// Try to extract JSON from response (LLM might add markdown fences)
	jsonStr := extractJSON(response)
	log.Printf("📝 DEBUG Summarizer extractJSON: %s", truncate(jsonStr, 300))
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		log.Printf("⚠️  Summarizer JSON parse failed: %v — raw: %s", err, truncate(response, 200))
		// Fallback: use the raw response as activity
		return &SummaryResult{
			Activity: truncate(response, 100),
		}, nil
	}

	log.Printf("📝 Summarizer result: title=%q activity=%q", result.Title, result.Activity)
	return &result, nil
}

// UpdateThread updates the thread's activity and optionally its title
func (s *Summarizer) UpdateThread(threadID string, result *SummaryResult, isNewThread bool) error {
	if result == nil {
		return nil
	}

	now := time.Now()
	if isNewThread && result.Title != "" {
		_, err := s.db.Exec(
			"UPDATE threads SET activity = ?, title = ?, updated_at = ? WHERE id = ?",
			result.Activity, result.Title, now, threadID,
		)
		return err
	}

	_, err := s.db.Exec(
		"UPDATE threads SET activity = ?, updated_at = ? WHERE id = ?",
		result.Activity, now, threadID,
	)
	return err
}

func (s *Summarizer) callAPI(prompt string) (string, error) {
	if s.provider == "anthropic" {
		return s.callAnthropicAPI(prompt)
	}
	return s.callOpenAICompatibleAPI(prompt)
}

func (s *Summarizer) callAnthropicAPI(prompt string) (string, error) {
	requestBody := map[string]interface{}{
		"model":      s.model,
		"max_tokens": 300,
		"temperature": 0.0,
		"system":     "You summarize conversations. Always include a JSON object in your response with exactly these fields: title, activity.",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Anthropic API error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}

	if len(response.Content) == 0 {
		return "", fmt.Errorf("empty response from Anthropic")
	}

	return response.Content[0].Text, nil
}

func (s *Summarizer) callOpenAICompatibleAPI(prompt string) (string, error) {
	requestBody := map[string]interface{}{
		"model":      s.model,
		"max_tokens": 2000,
		"stream":     false,
		"temperature": 0.0,
		"messages": []map[string]interface{}{
			{"role": "system", "content": "You summarize conversations. Always include a JSON object in your response with exactly these fields: title, activity."},
			{"role": "user", "content": prompt},
		},
	}

	// Disable thinking/reasoning for providers that support it — summarization
	// doesn't need chain-of-thought and thinking wastes ~94% of tokens.
	if s.provider == "fireworks" {
		requestBody["reasoning_effort"] = "none"
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	endpoint := s.endpoint
	if endpoint == "" {
		return "", fmt.Errorf("no completion endpoint for provider %s", s.provider)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	log.Printf("📝 DEBUG Summarizer API response status=%d body_len=%d", resp.StatusCode, len(body))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s API error %d: %s", s.provider, resp.StatusCode, string(body))
	}

	// Log raw body for debugging (truncated)
	log.Printf("📝 DEBUG Summarizer API raw body: %s", truncate(string(body), 500))

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("JSON unmarshal failed: %w — body: %s", err, truncate(string(body), 300))
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("empty response from %s — body: %s", s.provider, truncate(string(body), 300))
	}

	log.Printf("📝 DEBUG Summarizer finish_reason=%s content_len=%d", response.Choices[0].FinishReason, len(response.Choices[0].Message.Content))
	return response.Choices[0].Message.Content, nil
}

// extractTextForSummary extracts readable text from a message for summarization
func extractTextForSummary(msg Message) string {
	var parts []string

	switch content := msg.Content.(type) {
	case string:
		return content
	case []ContentBlock:
		for _, block := range content {
			switch block.Type {
			case "text":
				parts = append(parts, block.Text)
			case "tool_use":
				parts = append(parts, fmt.Sprintf("[Used tool: %s]", block.Name))
			case "tool_result":
				if contentStr, ok := block.Content.(string); ok {
					if len(contentStr) > 200 {
						contentStr = contentStr[:200] + "..."
					}
					parts = append(parts, fmt.Sprintf("[Result: %s]", contentStr))
				}
			}
		}
	case []interface{}:
		for _, block := range content {
			if blockMap, ok := block.(map[string]interface{}); ok {
				blockType, _ := blockMap["type"].(string)
				switch blockType {
				case "text":
					if text, ok := blockMap["text"].(string); ok {
						parts = append(parts, text)
					}
				case "tool_use":
					name, _ := blockMap["name"].(string)
					parts = append(parts, fmt.Sprintf("[Used tool: %s]", name))
				case "tool_result":
					if content, ok := blockMap["content"]; ok {
						contentStr := fmt.Sprintf("%v", content)
						if len(contentStr) > 200 {
							contentStr = contentStr[:200] + "..."
						}
						parts = append(parts, fmt.Sprintf("[Result: %s]", contentStr))
					}
				}
			}
		}
	}

	return strings.Join(parts, " ")
}

// stripThinkTags removes <think>...</think> blocks from model output
// (used by reasoning models like Qwen3, DeepSeek, etc.)
func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			// Unclosed tag — strip from <think> to end
			s = s[:start]
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

// extractJSON tries to extract a JSON object from a string that may contain markdown fences
func extractJSON(s string) string {
	s = stripThinkTags(s)
	s = strings.TrimSpace(s)
	// Strip markdown code fences if present
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock {
				jsonLines = append(jsonLines, line)
			}
		}
		if len(jsonLines) > 0 {
			return strings.Join(jsonLines, "\n")
		}
	}
	// Find first { and last }
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
