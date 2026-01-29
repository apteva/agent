package scenarios

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

const (
	serverPort = "4016" // Use different port for tests
	baseURL    = "http://localhost:" + serverPort
)

// TestSendNotificationScenario tests a complete scenario: ask agent to send notification
func TestSendNotificationScenario(t *testing.T) {
	// Load .env file
	godotenv.Load("../.env")

	// Start the server with basic tools config
	server := startServerWithConfig(t, "configs/basic-tools.json")
	defer server.stop()

	// Wait for server to be ready
	waitForServer(t)

	// Send chat message asking to send notification
	req := map[string]interface{}{
		"message": "Please send a notification with the message 'Test from scenario'",
	}

	resp := sendChatRequest(t, req)
	defer resp.Body.Close()

	// Parse SSE stream
	conversation := parseSSEStream(t, resp.Body)

	// Evaluate scenario
	t.Run("ToolWasUsed", func(t *testing.T) {
		if !hasToolUse(conversation, "send_notification") {
			t.Errorf("Expected send_notification tool to be used")
		}
	})

	t.Run("ToolSucceeded", func(t *testing.T) {
		if !hasSuccessfulToolResult(conversation, "send_notification") {
			t.Errorf("Expected send_notification to succeed")
		}
	})

	t.Run("ResponseMentionsNotification", func(t *testing.T) {
		response := getAssistantResponse(conversation)
		if !strings.Contains(strings.ToLower(response), "notification") {
			t.Errorf("Expected response to mention 'notification', got: %s", response)
		}
	})

	t.Run("LLMJudgeEvaluation", func(t *testing.T) {
		criteria := `
1. The agent understood the user's request to send a notification
2. The agent used the send_notification tool appropriately
3. The agent's response was clear and helpful
4. The notification message matches what was requested: "Test from scenario"
`
		result := evaluateWithLLM(t, conversation, criteria)

		if !result.Pass {
			t.Errorf("LLM Judge failed evaluation: %s\nFeedback: %s", result.Reason, result.Feedback)
		}

		if result.Score < 70 {
			t.Errorf("LLM Judge score too low: %d/100\nFeedback: %s", result.Score, result.Feedback)
		}
	})

	// Print conversation for debugging
	t.Logf("Conversation turns: %d", len(conversation))
	for i, event := range conversation {
		t.Logf("  [%d] %s: %s", i, event.Type, truncate(event.Content, 100))
	}
}

// TestMultiToolScenario tests a scenario with multiple tool calls
func TestMultiToolScenario(t *testing.T) {
	// Load .env file
	godotenv.Load("../.env")

	// Start the server with basic tools config
	server := startServerWithConfig(t, "configs/basic-tools.json")
	defer server.stop()

	// Wait for server to be ready
	waitForServer(t)

	// Send chat message asking to use tool multiple times
	req := map[string]interface{}{
		"message": "Send me two notifications: first one saying 'First notification' and second one saying 'Second notification'",
	}

	resp := sendChatRequest(t, req)
	defer resp.Body.Close()

	// Parse SSE stream
	conversation := parseSSEStream(t, resp.Body)

	// Evaluate scenario
	t.Run("SendNotificationUsed", func(t *testing.T) {
		if !hasToolUse(conversation, "send_notification") {
			t.Errorf("Expected send_notification tool to be used")
		}
	})

	t.Run("MultipleToolCalls", func(t *testing.T) {
		count := countToolUses(conversation, "send_notification")
		if count < 2 {
			t.Errorf("Expected send_notification to be used at least 2 times, got %d", count)
		}
	})

	t.Run("ResponseMentionsNotifications", func(t *testing.T) {
		response := getAssistantResponse(conversation)
		if !strings.Contains(strings.ToLower(response), "notification") {
			t.Errorf("Expected response to mention 'notification', got: %s", response)
		}
	})

	t.Run("LLMJudgeEvaluation", func(t *testing.T) {
		criteria := `
1. The agent used send_notification tool at least twice
2. The first notification message should say 'First notification'
3. The second notification message should say 'Second notification'
4. The agent's response demonstrates proper understanding of sending multiple notifications
5. Both notifications were sent successfully
`
		result := evaluateWithLLM(t, conversation, criteria)

		if !result.Pass {
			t.Errorf("LLM Judge failed evaluation: %s\nFeedback: %s", result.Reason, result.Feedback)
		}

		if result.Score < 70 {
			t.Errorf("LLM Judge score too low: %d/100\nFeedback: %s", result.Score, result.Feedback)
		}
	})

	// Print conversation for debugging
	t.Logf("Conversation turns: %d", len(conversation))
	for i, event := range conversation {
		t.Logf("  [%d] %s: %s", i, event.Type, truncate(event.Content, 100))
	}
}

// TestOperatorModeScenario tests computer use / operator mode
func TestOperatorModeScenario(t *testing.T) {
	// Load .env file
	godotenv.Load("../.env")

	// Start the server with operator mode config
	server := startServerWithConfig(t, "configs/operator-mode.json")
	defer server.stop()

	// Wait for server to be ready
	waitForServer(t)

	// Send chat message asking to browse and click
	req := map[string]interface{}{
		"message": "Go to example.com and click on the 'More information...' link",
	}

	resp := sendChatRequest(t, req)
	defer resp.Body.Close()

	// Parse SSE stream
	conversation := parseSSEStream(t, resp.Body)

	// Evaluate scenario
	t.Run("ComputerToolUsed", func(t *testing.T) {
		if !hasToolUse(conversation, "computer") {
			t.Errorf("Expected computer tool to be used")
		}
	})

	t.Run("MultipleComputerActions", func(t *testing.T) {
		count := countToolUses(conversation, "computer")
		if count < 2 {
			t.Errorf("Expected computer tool to be used at least 2 times (navigate + click), got %d", count)
		}
	})

	t.Run("ToolsSucceeded", func(t *testing.T) {
		if !hasSuccessfulToolResult(conversation, "computer") {
			t.Errorf("Expected computer tool to succeed")
		}
	})

	t.Run("LLMJudgeEvaluation", func(t *testing.T) {
		criteria := `
1. The agent used the computer tool to navigate to example.com
2. The agent used the computer tool to click on the 'More information...' link
3. The agent successfully completed the browsing task
4. The agent demonstrated understanding of computer use / operator mode
5. Multiple computer tool actions were performed (at least navigate and click)
`
		result := evaluateWithLLM(t, conversation, criteria)

		if !result.Pass {
			t.Errorf("LLM Judge failed evaluation: %s\nFeedback: %s", result.Reason, result.Feedback)
		}

		if result.Score < 70 {
			t.Errorf("LLM Judge score too low: %d/100\nFeedback: %s", result.Score, result.Feedback)
		}
	})

	// Print conversation for debugging
	t.Logf("Conversation turns: %d", len(conversation))
	for i, event := range conversation {
		if event.Type == "tool_use" {
			t.Logf("  [%d] %s (tool: %s): %s", i, event.Type, event.ToolName, truncate(event.Content, 200))
		} else if event.Type == "tool_result" {
			t.Logf("  [%d] %s: %s", i, event.Type, truncate(event.Content, 300))
		} else {
			t.Logf("  [%d] %s: %s", i, event.Type, truncate(event.Content, 200))
		}
	}
}

// Server management
type testServer struct {
	cmd    *exec.Cmd
	t      *testing.T
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func startServer(t *testing.T) *testServer {
	return startServerWithConfig(t, "")
}

func startServerWithConfig(t *testing.T, configPath string) *testServer {
	t.Helper()

	// If custom config specified, copy it to agent-config.json before starting server
	if configPath != "" {
		configData, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("Failed to read config file %s: %v", configPath, err)
		}

		// Parse JSON and update agent ID to be unique per test run
		var config map[string]interface{}
		if err := json.Unmarshal(configData, &config); err != nil {
			t.Fatalf("Failed to parse config JSON: %v", err)
		}

		// Generate unique agent ID
		if agent, ok := config["agent"].(map[string]interface{}); ok {
			timestamp := time.Now().UnixNano()
			agent["id"] = fmt.Sprintf("agent-test-%d", timestamp)
			t.Logf("Generated unique agent ID: %s", agent["id"])
		}

		// Write modified config
		modifiedConfig, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal modified config: %v", err)
		}

		targetConfig := "../agent-config.json"
		if err := os.WriteFile(targetConfig, modifiedConfig, 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", targetConfig, err)
		}
		t.Logf("Using config from: %s (copied to %s)", configPath, targetConfig)
	}

	// Use go run (inherits all environment including API keys)
	cmd := exec.Command("go", "run", "main.go")
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "PORT="+serverPort)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	t.Logf("Server started with PID %d on port %s", cmd.Process.Pid, serverPort)

	return &testServer{
		cmd:    cmd,
		t:      t,
		stdout: stdout,
		stderr: stderr,
	}
}

func (s *testServer) stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		s.t.Logf("Stopping server PID %d", s.cmd.Process.Pid)
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
}

func waitForServer(t *testing.T) {
	t.Helper()

	maxAttempts := 30
	for i := 0; i < maxAttempts; i++ {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Logf("Server ready after %d attempts", i+1)
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("Server did not become ready after %d attempts", maxAttempts)
}

func sendChatRequest(t *testing.T, payload map[string]interface{}) *http.Response {
	t.Helper()

	jsonData, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	resp, err := http.Post(baseURL+"/chat", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to send chat request: %v", err)
	}

	return resp
}

// SSE event
type SSEEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	ToolID  string `json:"tool_id,omitempty"`
	ToolName string `json:"tool_name,omitempty"`
}

func parseSSEStream(t *testing.T, body io.Reader) []SSEEvent {
	t.Helper()

	var events []SSEEvent
	scanner := bufio.NewScanner(body)

	// Increase buffer size for large tool results (e.g., screenshots with base64 images)
	const maxScanTokenSize = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	lineCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		lineCount++

		// SSE format: "data: {...}"
		if strings.HasPrefix(line, "data: ") {
			jsonStr := strings.TrimPrefix(line, "data: ")

			var event SSEEvent
			if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
				continue // Skip malformed events
			}

			events = append(events, event)
		}
	}

	if err := scanner.Err(); err != nil {
		t.Logf("Warning: scanner error: %v", err)
	}
	return events
}

// Evaluation helpers
func hasToolUse(events []SSEEvent, toolName string) bool {
	for _, event := range events {
		if event.Type == "tool_use" && event.ToolName == toolName {
			return true
		}
	}
	return false
}

func countToolUses(events []SSEEvent, toolName string) int {
	count := 0
	for _, event := range events {
		if event.Type == "tool_use" && event.ToolName == toolName {
			count++
		}
	}
	return count
}

func hasSuccessfulToolResult(events []SSEEvent, toolName string) bool {
	// Collect all tool_use IDs for this tool
	toolIDs := make(map[string]bool)
	for _, event := range events {
		if event.Type == "tool_use" && event.ToolName == toolName {
			toolIDs[event.ToolID] = true
		}
	}

	if len(toolIDs) == 0 {
		return false
	}

	// Check if ANY of these tools has a successful result
	for _, event := range events {
		if event.Type == "tool_result" && toolIDs[event.ToolID] {
			// Check if content contains error
			if !strings.Contains(strings.ToLower(event.Content), "error") {
				return true // Found at least one successful result
			}
		}
	}

	return false
}

func getAssistantResponse(events []SSEEvent) string {
	var response strings.Builder

	for _, event := range events {
		if event.Type == "content" {
			response.WriteString(event.Content)
		}
	}

	return response.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// LLM Judge evaluation
type JudgeResult struct {
	Pass     bool   `json:"pass"`
	Score    int    `json:"score"`
	Reason   string `json:"reason"`
	Feedback string `json:"feedback"`
}

func evaluateWithLLM(t *testing.T, conversation []SSEEvent, criteria string) JudgeResult {
	t.Helper()

	// Build conversation summary
	var conversationText strings.Builder
	for _, event := range conversation {
		switch event.Type {
		case "content":
			conversationText.WriteString(fmt.Sprintf("Assistant: %s\n", event.Content))
		case "tool_use":
			conversationText.WriteString(fmt.Sprintf("Tool Used: %s\n", event.ToolName))
		case "tool_result":
			conversationText.WriteString(fmt.Sprintf("Tool Result: %s\n", truncate(event.Content, 200)))
		}
	}

	// Build judge prompt
	prompt := fmt.Sprintf(`You are evaluating an AI agent's conversation.

CONVERSATION:
%s

EVALUATION CRITERIA:
%s

Respond ONLY with valid JSON in this exact format (no markdown, no extra text):
{
  "pass": true/false,
  "score": 0-100,
  "reason": "brief explanation",
  "feedback": "detailed feedback"
}`, conversationText.String(), criteria)

	// Call OpenAI API
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set, skipping LLM judge evaluation")
	}

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0.0,
		"max_tokens":  500,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal judge request: %v", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create judge request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to call OpenAI API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("OpenAI API error: %d - %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		t.Fatalf("Failed to decode OpenAI response: %v", err)
	}

	if len(apiResp.Choices) == 0 {
		t.Fatalf("No choices in OpenAI response")
	}

	// Parse judge result
	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)

	// Remove markdown code blocks if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result JudgeResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("Failed to parse judge result: %v\nContent: %s", err, content)
	}

	t.Logf("LLM Judge: Pass=%v, Score=%d, Reason=%s", result.Pass, result.Score, result.Reason)
	return result
}
