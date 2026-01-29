package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	baseURL = "http://localhost:4016"
)

// ANSI color codes
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorGray    = "\033[90m"
	colorBold    = "\033[1m"
)

type SSEEvent struct {
	Type     string `json:"type"`
	Content  string `json:"content"`
	ToolID   string `json:"tool_id,omitempty"`
	ToolName string `json:"tool_name,omitempty"`
}

type Scenario struct {
	Name    string
	Message string
	Config  string
}

var scenarios = map[string]Scenario{
	"notification": {
		Name:    "Send Notification",
		Message: "Please send a notification with the message 'Test from scenario'",
		Config:  "configs/basic-tools.json",
	},
	"multi-tool": {
		Name:    "Multiple Tool Calls",
		Message: "Send me two notifications: first one saying 'First notification' and second one saying 'Second notification'",
		Config:  "configs/basic-tools.json",
	},
	"operator": {
		Name:    "Operator Mode - Browser Automation",
		Message: "Go to https://practicetestautomation.com/practice-test-login/ and login with username 'student' and password 'Password123'. After successful login, verify you see the success message.",
		Config:  "configs/operator-mode.json",
	},
	"mcp-notification": {
		Name:    "MCP Multi-Tool - Weather and Notification",
		Message: "First, get the current weather for London using the MCP get-current-weather tool. Then, send that weather information via Pushover using the MCP send-notification tool with credential ID 6.",
		Config:  "configs/mcp-tools.json",
	},
	"task-management": {
		Name:    "Task Management Workflow",
		Message: "First, create a task to send a notification with message 'Hello from scheduled task' but don't execute it yet. Then update the task description to 'Updated: Send notification with greeting'. Then execute the task synchronously (use sync=true). Finally, list all tasks to verify it was executed.",
		Config:  "configs/task-management.json",
	},
}

func loadEnvFile() {
	// Try to load .env from parent directory (../env)
	envPath := filepath.Join("..", ".env")
	file, err := os.Open(envPath)
	if err != nil {
		return // .env file is optional
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Only set if not already in environment
			if os.Getenv(key) == "" {
				os.Setenv(key, value)
			}
		}
	}
}

func main() {
	// Load environment variables from .env file
	loadEnvFile()

	scenario := flag.String("scenario", "", "Scenario to run: notification, multi-tool, operator")
	message := flag.String("message", "", "Custom message to send (overrides scenario)")
	config := flag.String("config", "", "Config file to use (overrides scenario)")
	list := flag.Bool("list", false, "List available scenarios")
	flag.Parse()

	if *list {
		printScenarios()
		os.Exit(0)
	}

	var selectedScenario Scenario

	if *scenario != "" {
		s, ok := scenarios[*scenario]
		if !ok {
			fmt.Printf("%s❌ Unknown scenario: %s%s\n", colorRed, *scenario, colorReset)
			fmt.Printf("Run with -list to see available scenarios\n")
			os.Exit(1)
		}
		selectedScenario = s
	} else if *message != "" {
		selectedScenario = Scenario{
			Name:    "Custom Message",
			Message: *message,
		}
	} else {
		fmt.Println("Usage:")
		fmt.Println("  go run run-scenario.go -scenario <name>")
		fmt.Println("  go run run-scenario.go -message \"Your message\"")
		fmt.Println("  go run run-scenario.go -list")
		fmt.Println("")
		printScenarios()
		os.Exit(1)
	}

	// Override config if specified
	if *config != "" {
		selectedScenario.Config = *config
	}

	// Print header
	printHeader(selectedScenario.Name)

	// Print config info
	if selectedScenario.Config != "" {
		fmt.Printf("%s📋 Config: %s%s\n", colorGray, selectedScenario.Config, colorReset)
	}

	// Wait for server
	fmt.Printf("%s⏳ Waiting for server at %s...%s\n", colorYellow, baseURL, colorReset)
	if !waitForServer() {
		fmt.Printf("%s❌ Server not ready. Make sure to start the server first:%s\n", colorRed, colorReset)
		fmt.Printf("   PORT=4016 go run main.go\n")
		if selectedScenario.Config != "" {
			fmt.Printf("   (copy %s to ../agent-config.json first)\n", selectedScenario.Config)
		}
		os.Exit(1)
	}
	fmt.Printf("%s✅ Server ready!%s\n\n", colorGreen, colorReset)

	// Send message
	fmt.Printf("%s┌─ USER MESSAGE ────────────────────────────────────────────┐%s\n", colorCyan+colorBold, colorReset)
	fmt.Printf("%s│%s %s\n", colorCyan, colorReset, selectedScenario.Message)
	fmt.Printf("%s└───────────────────────────────────────────────────────────┘%s\n\n", colorCyan, colorReset)

	// Make request
	req := map[string]interface{}{
		"message": selectedScenario.Message,
	}

	jsonData, _ := json.Marshal(req)
	resp, err := http.Post(baseURL+"/chat", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("%s❌ Error: %v%s\n", colorRed, err, colorReset)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Parse SSE stream
	fmt.Printf("%s┌─ AGENT RESPONSE ──────────────────────────────────────────┐%s\n", colorGreen+colorBold, colorReset)
	events := parseSSEStream(resp.Body)
	fmt.Printf("%s└───────────────────────────────────────────────────────────┘%s\n\n", colorGreen, colorReset)

	// Run LLM judge evaluation
	if *scenario != "" {
		evaluateScenario(selectedScenario, events)
	}

	printFooter()
}

func printHeader(scenarioName string) {
	fmt.Println()
	fmt.Printf("%s╔══════════════════════════════════════════════════════════╗%s\n", colorBlue+colorBold, colorReset)
	fmt.Printf("%s║           SCENARIO RUNNER - LIVE MODE                   ║%s\n", colorBlue+colorBold, colorReset)
	fmt.Printf("%s╠══════════════════════════════════════════════════════════╣%s\n", colorBlue+colorBold, colorReset)
	fmt.Printf("%s║  %s%-54s%s  ║%s\n", colorBlue+colorBold, colorReset, scenarioName, colorBlue+colorBold, colorReset)
	fmt.Printf("%s╚══════════════════════════════════════════════════════════╝%s\n", colorBlue+colorBold, colorReset)
	fmt.Println()
}

func printScenarios() {
	fmt.Println("Available scenarios:")
	fmt.Println()
	for key, scenario := range scenarios {
		fmt.Printf("  %s%-15s%s - %s\n", colorCyan, key, colorReset, scenario.Name)
		fmt.Printf("    %sConfig: %s%s\n", colorGray, scenario.Config, colorReset)
		fmt.Printf("    %sMessage: %s%s\n", colorGray, scenario.Message, colorReset)
		fmt.Println()
	}
}

func printFooter() {
	fmt.Printf("%s✨ Scenario completed%s\n\n", colorGreen, colorReset)
}

func waitForServer() bool {
	maxAttempts := 30
	for i := 0; i < maxAttempts; i++ {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func parseSSEStream(body io.Reader) []SSEEvent {
	var events []SSEEvent
	scanner := bufio.NewScanner(body)

	// Increase buffer size for large tool results
	const maxScanTokenSize = 10 * 1024 * 1024
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	var currentContent strings.Builder
	toolCount := 0

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			jsonStr := strings.TrimPrefix(line, "data: ")

			var event SSEEvent
			if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
				continue
			}

			events = append(events, event)

			switch event.Type {
			case "start":
				fmt.Printf("%s│%s %s🚀 Starting...%s\n", colorGreen, colorReset, colorGray, colorReset)

			case "thread_id":
				fmt.Printf("%s│%s %s🔗 Thread: %s%s\n", colorGreen, colorReset, colorGray, event.Content, colorReset)

			case "content":
				currentContent.WriteString(event.Content)
				// Print content as it streams
				fmt.Printf("%s", event.Content)

			case "tool_input_delta":
				// Don't print these, just accumulate

			case "tool_use":
				toolCount++
				if currentContent.Len() > 0 {
					fmt.Printf("\n")
					currentContent.Reset()
				}
				fmt.Printf("%s│%s\n", colorGreen, colorReset)
				fmt.Printf("%s│%s %s🔧 Tool #%d: %s%s%s\n", colorGreen, colorReset, colorYellow, toolCount, colorBold, event.ToolName, colorReset)

			case "tool_result":
				// Truncate long results (like screenshots)
				result := event.Content
				if len(result) > 200 {
					result = result[:200] + "... [truncated]"
				}
				fmt.Printf("%s│%s %s✓ Result: %s%s\n", colorGreen, colorReset, colorGray, result, colorReset)

			case "stop":
				if currentContent.Len() > 0 {
					fmt.Printf("\n")
					currentContent.Reset()
				}
				fmt.Printf("%s│%s\n", colorGreen, colorReset)
				fmt.Printf("%s│%s %s🏁 Done (used %d tools)%s\n", colorGreen, colorReset, colorGray, toolCount, colorReset)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("%s│%s %s⚠ Scanner error: %v%s\n", colorGreen, colorReset, colorRed, err, colorReset)
	}

	return events
}

type JudgeResult struct {
	Pass     bool   `json:"pass"`
	Score    int    `json:"score"`
	Reason   string `json:"reason"`
	Feedback string `json:"feedback"`
}

func evaluateScenario(scenario Scenario, events []SSEEvent) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Printf("%s⚠️  Skipping LLM judge (OPENAI_API_KEY not set)%s\n", colorYellow, colorReset)
		return
	}

	// Build conversation summary
	var conversationText strings.Builder
	for _, event := range events {
		switch event.Type {
		case "content":
			conversationText.WriteString(fmt.Sprintf("Assistant: %s\n", event.Content))
		case "tool_use":
			conversationText.WriteString(fmt.Sprintf("Tool Used: %s\n", event.ToolName))
		case "tool_result":
			result := event.Content
			if len(result) > 200 {
				result = result[:200] + "..."
			}
			conversationText.WriteString(fmt.Sprintf("Tool Result: %s\n", result))
		}
	}

	// Get criteria for scenario
	criteria := getCriteriaForScenario(scenario.Name)

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

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0.0,
		"max_tokens":  500,
	}

	jsonData, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	fmt.Printf("%s┌─ LLM JUDGE (gpt-4o) ──────────────────────────────────────┐%s\n", colorMagenta+colorBold, colorReset)
	fmt.Printf("%s│%s %sEvaluating...%s\n", colorMagenta, colorReset, colorGray, colorReset)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("%s│%s %s❌ Error: %v%s\n", colorMagenta, colorReset, colorRed, err, colorReset)
		fmt.Printf("%s└───────────────────────────────────────────────────────────┘%s\n\n", colorMagenta, colorReset)
		return
	}
	defer resp.Body.Close()

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		fmt.Printf("%s│%s %s❌ Parse error: %v%s\n", colorMagenta, colorReset, colorRed, err, colorReset)
		fmt.Printf("%s└───────────────────────────────────────────────────────────┘%s\n\n", colorMagenta, colorReset)
		return
	}

	if len(apiResp.Choices) == 0 {
		fmt.Printf("%s│%s %s❌ No response from judge%s\n", colorMagenta, colorReset, colorRed, colorReset)
		fmt.Printf("%s└───────────────────────────────────────────────────────────┘%s\n\n", colorMagenta, colorReset)
		return
	}

	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result JudgeResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		fmt.Printf("%s│%s %s❌ JSON parse error: %v%s\n", colorMagenta, colorReset, colorRed, err, colorReset)
		fmt.Printf("%s└───────────────────────────────────────────────────────────┘%s\n\n", colorMagenta, colorReset)
		return
	}

	// Print verdict
	passIcon := "❌"
	passColor := colorRed
	if result.Pass {
		passIcon = "✅"
		passColor = colorGreen
	}

	scoreColor := colorRed
	if result.Score >= 70 {
		scoreColor = colorYellow
	}
	if result.Score >= 90 {
		scoreColor = colorGreen
	}

	fmt.Printf("%s│%s\n", colorMagenta, colorReset)
	fmt.Printf("%s│%s %s%s VERDICT: %s%s\n", colorMagenta, colorReset, passColor+colorBold, passIcon, result.Reason, colorReset)
	fmt.Printf("%s│%s %s📊 SCORE: %d/100%s\n", colorMagenta, colorReset, scoreColor+colorBold, result.Score, colorReset)
	fmt.Printf("%s│%s\n", colorMagenta, colorReset)

	// Word wrap feedback
	feedback := result.Feedback
	maxWidth := 58
	words := strings.Fields(feedback)
	var lines []string
	currentLine := ""

	for _, word := range words {
		if len(currentLine)+len(word)+1 > maxWidth {
			lines = append(lines, currentLine)
			currentLine = word
		} else {
			if currentLine != "" {
				currentLine += " "
			}
			currentLine += word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	fmt.Printf("%s│%s %sFeedback:%s\n", colorMagenta, colorReset, colorGray, colorReset)
	for _, line := range lines {
		fmt.Printf("%s│%s %s%s\n", colorMagenta, colorReset, line, "")
	}

	fmt.Printf("%s└───────────────────────────────────────────────────────────┘%s\n\n", colorMagenta, colorReset)
}

func getCriteriaForScenario(scenarioName string) string {
	switch scenarioName {
	case "Send Notification":
		return `1. The agent understood the user's request to send a notification
2. The agent used the send_notification tool appropriately
3. The agent's response was clear and helpful
4. The notification message matches what was requested: "Test from scenario"`

	case "Multiple Tool Calls":
		return `1. The agent used send_notification tool at least twice
2. The first notification message should say 'First notification'
3. The second notification message should say 'Second notification'
4. The agent's response demonstrates proper understanding of sending multiple notifications
5. Both notifications were sent successfully`

	case "Operator Mode - Browser Automation":
		return `1. The agent created an operator session to start browser automation
2. The agent navigated to https://practicetestautomation.com/practice-test-login/
3. The agent located and filled in the username field with 'student'
4. The agent located and filled in the password field with 'Password123'
5. The agent clicked the submit/login button
6. The agent verified the successful login by checking for the success message
7. Multiple computer tool actions were performed (create session, screenshot, type, click)
8. The agent successfully completed the entire login flow`

	case "MCP Multi-Tool - Weather and Notification":
		return `1. The agent used the MCP get-current-weather tool to fetch weather for London
2. The agent successfully retrieved weather information
3. The agent used the MCP send-notification tool (not the local send_notification tool)
4. The agent provided credential ID 6 for the Pushover notification
5. The notification message contains weather information from London
6. The agent demonstrated proper MCP multi-tool workflow (weather → notification)
7. Both MCP tools were used successfully in sequence`

	default:
		return "Evaluate if the agent successfully completed the requested task."
	}
}
