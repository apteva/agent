package stream

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Summarizer generates short thread titles and activity summaries using a small LLM
type Summarizer struct {
	db           *sql.DB
	anthropicKey string
	model        string
}

// SummaryResult holds the generated title and activity
type SummaryResult struct {
	Title    string `json:"title"`
	Activity string `json:"activity"`
}

// NewSummarizer creates a new Summarizer instance
func NewSummarizer(db *sql.DB, anthropicKey string, model string) *Summarizer {
	if model == "" {
		model = "claude-haiku-4-5"
	}
	return &Summarizer{
		db:           db,
		anthropicKey: anthropicKey,
		model:        model,
	}
}

// GenerateThreadSummary generates a title and activity line from recent messages.
// Returns title, activity, and error. Title may be empty if it shouldn't be updated.
func (s *Summarizer) GenerateThreadSummary(threadID string, messages []Message) (*SummaryResult, error) {
	if s.anthropicKey == "" {
		return nil, fmt.Errorf("no Anthropic API key configured")
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages to summarize")
	}

	// Take the last few messages (up to 6) to keep input small
	start := 0
	if len(messages) > 6 {
		start = len(messages) - 6
	}
	recent := messages[start:]

	// Build a compact conversation snippet
	var snippet strings.Builder
	for _, msg := range recent {
		role := strings.ToUpper(msg.Role)
		content := extractTextForSummary(msg)
		if content != "" {
			// Truncate individual messages to keep tokens low
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			snippet.WriteString(fmt.Sprintf("[%s]: %s\n", role, content))
		}
	}

	prompt := fmt.Sprintf(`Given this conversation snippet, produce a JSON object with:
- "title": A short title (3-6 words) for the thread topic
- "activity": A very brief status (2-4 words) of what just happened

Examples:
{"title": "PR Review Auth Module", "activity": "Completed PR review"}
{"title": "Monthly Sales Report", "activity": "Generated report"}
{"title": "Debug Login Timeout", "activity": "Identified root cause"}
{"title": "Deploy API Gateway", "activity": "Deployed successfully"}
{"title": "Customer Billing Issue", "activity": "Resolved ticket"}

Keep activity to 2-4 words. Respond with ONLY the JSON object.

CONVERSATION:
%s`, snippet.String())

	response, err := s.callAPI(prompt)
	if err != nil {
		return nil, fmt.Errorf("API call failed: %w", err)
	}

	// Parse the JSON response
	var result SummaryResult
	// Try to extract JSON from response (LLM might add markdown fences)
	jsonStr := extractJSON(response)
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Fallback: use the raw response as activity
		return &SummaryResult{
			Activity: truncate(response, 100),
		}, nil
	}

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
	requestBody := map[string]interface{}{
		"model":      s.model,
		"max_tokens": 150,
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
	req.Header.Set("x-api-key", s.anthropicKey)
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

// extractJSON tries to extract a JSON object from a string that may contain markdown fences
func extractJSON(s string) string {
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
