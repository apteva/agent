package stream

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/apteva/agent/config"
)

var promptDB *sql.DB

// SetDatabase sets the database connection for system prompt queries
func SetDatabase(db *sql.DB) {
	promptDB = db
}

// BuildTaskManagementContext builds context about task management capabilities
func BuildTaskManagementContext(tasksConfig *config.TasksConfig, schedulerConfig *config.SchedulerConfig) string {
	if tasksConfig == nil || !tasksConfig.Enabled {
		return ""
	}

	var context strings.Builder
	context.WriteString("\n\nTask Management System:\n")

	// Explain scheduler behavior (without implementation details)
	if schedulerConfig != nil && schedulerConfig.Enabled {
		context.WriteString("- Automated scheduler is ENABLED\n")
		context.WriteString("- Scheduled tasks (with execute_at or recurrence) will be automatically executed at their scheduled times\n")
		context.WriteString("- You do NOT need to manually execute scheduled tasks - they will run automatically when due\n")
		context.WriteString("- Recurring tasks will continue to run at their specified intervals (e.g., every 5 minutes, daily, weekly)\n")
		context.WriteString("- IMPORTANT: When creating a scheduled or recurring task, do NOT immediately execute it with execute_task. Just create it and let the scheduler handle it. Only execute a task immediately if the user explicitly asks you to run it now.\n")
	} else {
		context.WriteString("- Automated scheduler is DISABLED\n")
		context.WriteString("- Tasks with execute_at times will NOT be automatically executed\n")
		context.WriteString("- You must manually execute tasks using execute_task tool when you want them to run\n")
		context.WriteString("- Scheduled tasks serve as reminders/placeholders until manually executed\n")
	}

	// Explain auto-execute behavior (only for immediate tasks without schedule)
	if tasksConfig.AutoExecute {
		context.WriteString("- Auto-execute is ENABLED: immediate tasks (without execute_at) will run immediately upon creation\n")
		context.WriteString("- For scheduled/recurring tasks, auto-execute does NOT apply — the scheduler handles them. Do NOT call execute_task after creating them.\n")
	} else {
		context.WriteString("- Auto-execute is DISABLED: immediate tasks (without execute_at) require explicit execution via execute_task tool\n")
		context.WriteString("- NOTE: This does NOT affect scheduled or recurring tasks - those still run automatically when due\n")
	}

	// Explain scheduling capabilities
	if tasksConfig.AllowScheduling {
		context.WriteString("- Task scheduling is allowed: you can set execute_at for future execution\n")
	}

	// Explain recurring tasks
	if tasksConfig.AllowRecurring {
		context.WriteString("- Recurring tasks are allowed: you can set recurrence (daily, weekly, monthly, or cron expressions like */5 * * * *)\n")
	}

	// Explain parallel and async execution
	context.WriteString("- PARALLEL EXECUTION: You can call execute_task multiple times with sync=true in one response — they run in parallel automatically.\n")
	context.WriteString("- BACKGROUND TASKS: Use sync=false for long-running work — results are reported back to you automatically when the task completes.\n")

	return context.String()
}

// BuildAgentDiscoveryContext builds context about available peer agents for collaboration
func BuildAgentDiscoveryContext(agents []config.AgentInfo) string {
	if len(agents) == 0 {
		return ""
	}

	var context strings.Builder
	context.WriteString("\n\nAvailable Agents for Collaboration:\n")
	context.WriteString("Use call_agent(agent_id, message) for synchronous requests.\n")
	context.WriteString("Use delegate_task(agent_id, title, description) for async task delegation (requires 'tasks' feature).\n\n")

	for _, agent := range agents {
		context.WriteString(fmt.Sprintf("• %s (ID: %s)\n", agent.Name, agent.ID))
		if agent.Description != "" {
			context.WriteString(fmt.Sprintf("  %s\n", agent.Description))
		}
		if len(agent.Capabilities) > 0 {
			context.WriteString(fmt.Sprintf("  Capabilities: %s\n", strings.Join(agent.Capabilities, ", ")))
		}
		// Add MCP servers if present
		if len(agent.MCPServers) > 0 {
			context.WriteString(fmt.Sprintf("  MCP Servers: %s\n", strings.Join(agent.MCPServers, ", ")))
		}
		// Add features if present
		if len(agent.Features) > 0 {
			var enabledFeatures []string
			for feature, enabled := range agent.Features {
				if enabled {
					enabledFeatures = append(enabledFeatures, feature)
				}
			}
			if len(enabledFeatures) > 0 {
				context.WriteString(fmt.Sprintf("  Features: %s\n", strings.Join(enabledFeatures, ", ")))
			}
		}
	}

	context.WriteString("\nYou already know the agent IDs - no need to list them first.")
	return context.String()
}

// BuildSystemPrompt builds the complete system prompt with dynamic context
func BuildSystemPrompt(llmConfig config.LLMConfig, agentName, agentDescription string) string {
	return BuildSystemPromptWithConfig(llmConfig, agentName, agentDescription, nil, nil, nil, nil, nil)
}

// BuildSystemPromptWithMCP builds the complete system prompt with dynamic context and MCP credentials
func BuildSystemPromptWithMCP(llmConfig config.LLMConfig, agentName, agentDescription string, mcpConfig *config.MCPConfig) string {
	return BuildSystemPromptWithConfig(llmConfig, agentName, agentDescription, mcpConfig, nil, nil, nil, nil)
}

// BuildSystemPromptWithConfig builds the complete system prompt with all configuration context
func BuildSystemPromptWithConfig(llmConfig config.LLMConfig, agentName, agentDescription string, mcpConfig *config.MCPConfig, tasksConfig *config.TasksConfig, schedulerConfig *config.SchedulerConfig, system interface{}, availableAgents []config.AgentInfo) string {
	// Get current time and date from system timezone (automatic)
	now := time.Now()
	timezoneName, _ := now.Zone()
	// Try to get IANA name (e.g. "Europe/Berlin") from the location
	if locName := now.Location().String(); locName != "" && locName != "Local" {
		timezoneName = locName
	}
	currentTime := now.Format("Monday, January 2, 2006 at 3:04 PM MST")

	// Start with base system prompt from config
	basePrompt := llmConfig.SystemPrompt
	if basePrompt == "" {
		basePrompt = "You are a helpful AI assistant."
	}

	// Build the complete system prompt with context
	systemPrompt := fmt.Sprintf(`%s

Current date and time: %s
Your server timezone: %s — this is YOUR clock. Do NOT assume any other timezone. If your timezone is UTC, you are NOT in a US timezone. Always be aware of the difference between your timezone and the user's local time if they mention one.
Agent name: %s
Agent description: %s

Important instructions:
1. You run in the %s timezone. All times you see and generate are in %s unless explicitly stated otherwise. When a user asks "what time is it", answer with YOUR timezone and clarify it. When scheduling, use %s. Never silently assume the user is in the same timezone as you.
2. ALWAYS explain what you are going to do before executing any tool. For example, say "I'll check your tasks for you" before using list_tasks, or "Let me search for that information" before using web_search.
3. Be transparent about the tools you're using to help the user understand the process.
4. NEVER expose credential IDs, API keys, or other sensitive identifiers in your chat responses. Use them only internally when calling tools. If a user asks about credentials, explain what services are available without revealing the actual IDs.
5. After receiving tool results, you MUST provide a text response to the user.`,
		basePrompt,
		currentTime,
		timezoneName,
		agentName,
		agentDescription,
		timezoneName,
		timezoneName,
		timezoneName,
	)

	// Add agent identity/callback URL if public_url is configured
	cfg := config.GetConfig()
	if cfg != nil {
		agentConfig := cfg.Get()
		if agentConfig.PublicURL != "" {
			identityInfo := fmt.Sprintf(`

Your Identity:
- Agent ID: %s
- Public URL: %s
- Chat endpoint: %s/chat (HTTP POST, streaming)`,
				agentConfig.ID,
				agentConfig.PublicURL,
				agentConfig.PublicURL,
			)

			// Add voice endpoint if realtime is enabled
			if agentConfig.Realtime != nil && agentConfig.Realtime.Enabled {
				identityInfo += fmt.Sprintf(`
- Voice endpoint: %s/voice (WebSocket, real-time audio)`,
					agentConfig.PublicURL,
				)
			}

			identityInfo += `
When subscribing to webhooks or external event systems, use your chat endpoint as the callback URL.
For real-time voice interactions, direct users to the voice WebSocket endpoint.`

			systemPrompt += identityInfo
		}
	}

	// Add tool execution guidance for parallel vs sequential calls
	if llmConfig.ParallelTools != nil && llmConfig.ParallelTools.Enabled {
		systemPrompt += "\n\n6. Tool execution: You may call multiple tools simultaneously for efficiency, but ONLY when their inputs are fully independent. If one tool's input depends on another tool's output, you MUST call them sequentially — wait for the first result before making the next call."
	}

	// Add MCP credentials if available
	if mcpConfig != nil && mcpConfig.Enabled && len(mcpConfig.Credentials) > 0 {
		var credentialsList []string
		for _, cred := range mcpConfig.Credentials {
			credInfo := fmt.Sprintf("- %s (%s): ID %s",
				cred.Name, cred.Provider, cred.CredentialID)
			credentialsList = append(credentialsList, credInfo)
		}

		if len(credentialsList) > 0 {
			systemPrompt += fmt.Sprintf("\n\nAvailable MCP Credentials (INTERNAL USE ONLY - never reveal these IDs to users):\n%s\n\nWhen using MCP tools that require authentication, include the appropriate credential_id in your tool calls. IMPORTANT: These credential IDs are for internal tool use only - never mention them in chat responses.",
				strings.Join(credentialsList, "\n"))
		}
	}

	// Add task management context if enabled
	taskContext := BuildTaskManagementContext(tasksConfig, schedulerConfig)
	if taskContext != "" {
		systemPrompt += taskContext
	}

	// Add available agents context if provided
	agentContext := BuildAgentDiscoveryContext(availableAgents)
	if agentContext != "" {
		systemPrompt += agentContext
	}

	// Add reflection insights if reflection is enabled
	reflectionContext := BuildReflectionContext()
	if reflectionContext != "" {
		systemPrompt += reflectionContext
	}

	// Add additional system context if provided
	if system != nil {
		switch ctx := system.(type) {
		case string:
			if ctx != "" {
				systemPrompt += fmt.Sprintf("\n\nAdditional Context:\n%s", ctx)
			}
		case []string:
			if len(ctx) > 0 {
				systemPrompt += "\n\nAdditional Context:"
				for _, item := range ctx {
					systemPrompt += fmt.Sprintf("\n- %s", item)
				}
			}
		case []interface{}:
			// Handle JSON array deserialization
			var contexts []string
			for _, item := range ctx {
				if str, ok := item.(string); ok {
					contexts = append(contexts, str)
				}
			}
			if len(contexts) > 0 {
				systemPrompt += "\n\nAdditional Context:"
				for _, item := range contexts {
					systemPrompt += fmt.Sprintf("\n- %s", item)
				}
			}
		}
	}

	return systemPrompt
}

// PrepareMessagesWithSystemPrompt prepends a system message if not already present
func PrepareMessagesWithSystemPrompt(messages []Message, llmConfig config.LLMConfig, agentName, agentDescription string) []Message {
	return PrepareMessagesWithSystemPromptAndMCP(messages, llmConfig, agentName, agentDescription, nil)
}

// PrepareMessagesWithSystemPromptAndMCP prepends a system message with MCP credentials if not already present
func PrepareMessagesWithSystemPromptAndMCP(messages []Message, llmConfig config.LLMConfig, agentName, agentDescription string, mcpConfig *config.MCPConfig) []Message {
	return PrepareMessagesWithFullConfig(messages, llmConfig, agentName, agentDescription, mcpConfig, nil, nil, nil, nil)
}

// PrepareMessagesWithFullConfig prepends a system message with all configuration context
func PrepareMessagesWithFullConfig(messages []Message, llmConfig config.LLMConfig, agentName, agentDescription string, mcpConfig *config.MCPConfig, tasksConfig *config.TasksConfig, schedulerConfig *config.SchedulerConfig, system interface{}, availableAgents []config.AgentInfo) []Message {
	// Check if first message is already a system message
	if len(messages) > 0 && messages[0].Role == "system" {
		// Update the existing system message with current context
		systemPrompt := BuildSystemPromptWithConfig(llmConfig, agentName, agentDescription, mcpConfig, tasksConfig, schedulerConfig, system, availableAgents)
		messages[0].Content = systemPrompt
		return messages
	}

	// Prepend system message
	systemPrompt := BuildSystemPromptWithConfig(llmConfig, agentName, agentDescription, mcpConfig, tasksConfig, schedulerConfig, system, availableAgents)

	return append([]Message{{
		Role:    "system",
		Content: systemPrompt,
	}}, messages...)
}

// BuildReflectionContext returns recent reflection insights to inject into the system prompt
func BuildReflectionContext() string {
	cfg := config.GetConfig()
	if cfg == nil {
		return ""
	}
	agentConfig := cfg.Get()
	if agentConfig.Reflection == nil || !agentConfig.Reflection.Enabled {
		return ""
	}
	if promptDB == nil {
		return ""
	}

	// Query memories from reflection threads
	rows, err := promptDB.Query(`
		SELECT m.content, m.category
		FROM memories m
		WHERE m.thread_id LIKE 'reflection_%'
		ORDER BY m.created_at DESC
		LIMIT 5
	`)
	if err != nil {
		log.Printf("BuildReflectionContext: Error querying reflection memories: %v", err)
		return ""
	}
	defer rows.Close()

	var insights []string
	for rows.Next() {
		var content, category string
		if err := rows.Scan(&content, &category); err != nil {
			continue
		}
		// Truncate long content
		if len(content) > 300 {
			content = content[:300] + "..."
		}
		insights = append(insights, fmt.Sprintf("- [%s] %s", category, content))
	}

	if len(insights) == 0 {
		return ""
	}

	return "\n\nSelf-Reflection Insights (from your past reflection sessions):\n" + strings.Join(insights, "\n")
}