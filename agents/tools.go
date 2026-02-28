package agents

import (
	"github.com/apteva/agent/tools"
)

// CallAgentTool implements the call_agent tool with streaming support
type CallAgentTool struct {
	Client *AgentClient
}

func (t *CallAgentTool) Name() string {
	return "call_agent"
}

func (t *CallAgentTool) DisplayName() string {
	return "Call Agent"
}

func (t *CallAgentTool) Description() string {
	return "Call another agent to get their help or expertise on a specific task. Available agent IDs are listed in your system prompt. The other agent will receive your message and respond with their analysis, code, or recommendations."
}

func (t *CallAgentTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"agent_id": map[string]interface{}{
				"type":        "string",
				"description": "ID of the agent to call (available agent IDs are in your system prompt)",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Message to send to the agent. Be clear and specific about what you need.",
			},
			"context": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"none", "recent", "full"},
				"description": "Amount of conversation context to share with the agent (default: none)",
				"default":     "none",
			},
			"thread_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: specific thread ID to use on the other agent",
			},
			"_metadata": map[string]interface{}{
				"type":        "string",
				"description": "Internal use only",
				"default":     "",
			},
		},
		"required": []string{"agent_id", "message"},
	}
}

// DynamicDisplayName returns "Calling {AgentName}" based on the input params
func (t *CallAgentTool) DynamicDisplayName(params map[string]interface{}) string {
	agentID, _ := params["agent_id"].(string)
	if agentID != "" && t.Client != nil {
		name := t.Client.GetAgentName(agentID)
		return "Calling " + name
	}
	return ""
}

// SupportsStreaming returns true - this tool streams agent responses
func (t *CallAgentTool) SupportsStreaming() bool {
	return true
}

// ExecuteStreaming calls another agent and streams the response
func (t *CallAgentTool) ExecuteStreaming(params map[string]interface{}, callback tools.StreamCallback) (interface{}, error) {
	// Extract parameters
	agentID, _ := params["agent_id"].(string)
	message, _ := params["message"].(string)
	contextType, _ := params["context"].(string)
	threadID, _ := params["thread_id"].(string)

	// Validate required parameters
	if agentID == "" || message == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "agent_id and message are required parameters",
		}, nil
	}

	// Default context type
	if contextType == "" {
		contextType = "none"
	}

	// Call the agent with streaming
	result, err := t.Client.CallAgentStreaming(agentID, message, contextType, threadID, callback)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	// Return result
	response := map[string]interface{}{
		"success":     result.Success,
		"agent_id":    result.AgentID,
		"agent_name":  result.AgentName,
		"duration_ms": result.DurationMS,
	}

	if result.Success {
		response["response"] = result.Response
		response["thread_id"] = result.ThreadID
		if result.TokensUsed > 0 {
			response["tokens_used"] = result.TokensUsed
		}
	} else {
		response["error"] = result.Error
	}

	return response, nil
}

// Execute falls back to non-streaming for backwards compatibility
func (t *CallAgentTool) Execute(params map[string]interface{}) (interface{}, error) {
	// Extract parameters
	agentID, _ := params["agent_id"].(string)
	message, _ := params["message"].(string)
	contextType, _ := params["context"].(string)
	threadID, _ := params["thread_id"].(string)

	// Validate required parameters
	if agentID == "" || message == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "agent_id and message are required parameters",
		}, nil
	}

	// Default context type
	if contextType == "" {
		contextType = "none"
	}

	// Call the agent synchronously (no streaming)
	result, err := t.Client.CallAgent(agentID, message, contextType, threadID)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	// Return result
	response := map[string]interface{}{
		"success":     result.Success,
		"agent_id":    result.AgentID,
		"agent_name":  result.AgentName,
		"duration_ms": result.DurationMS,
	}

	if result.Success {
		response["response"] = result.Response
		response["thread_id"] = result.ThreadID
		if result.TokensUsed > 0 {
			response["tokens_used"] = result.TokensUsed
		}
	} else {
		response["error"] = result.Error
	}

	return response, nil
}

// ListAvailableAgentsTool implements list_available_agents
type ListAvailableAgentsTool struct {
	Client *AgentClient
}

func (t *ListAvailableAgentsTool) Name() string {
	return "list_available_agents"
}

func (t *ListAvailableAgentsTool) DisplayName() string {
	return "List Available Agents"
}

func (t *ListAvailableAgentsTool) Description() string {
	return "Get a list of other agents you can communicate with, including their capabilities and specializations. Use this to find the right agent for a specific task."
}

func (t *ListAvailableAgentsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"status": map[string]interface{}{
				"type":        "string",
				"description": "Always set to 'active' to list active agents",
				"enum":        []string{"active"},
				"default":     "active",
			},
		},
		"required": []string{"status"},
	}
}

func (t *ListAvailableAgentsTool) Execute(params map[string]interface{}) (interface{}, error) {
	// Get all agents (filtering can be added later if needed)
	// For now, just return all available agents
	agents := t.Client.GetAvailableAgents([]string{}, []string{})

	return map[string]interface{}{
		"success": true,
		"agents":  agents,
		"count":   len(agents),
	}, nil
}

// DelegateTaskTool implements the delegate_task tool for async task delegation
type DelegateTaskTool struct {
	Client *AgentClient
}

func (t *DelegateTaskTool) Name() string {
	return "delegate_task"
}

func (t *DelegateTaskTool) DisplayName() string {
	return "Delegate Task"
}

func (t *DelegateTaskTool) Description() string {
	return "Delegate a task to another agent for asynchronous execution. The target agent must have the 'tasks' feature enabled. Unlike call_agent which is synchronous, delegate_task creates a task on the remote agent that will be executed by their scheduler. Use this for long-running work that doesn't need immediate response."
}

// DynamicDisplayName returns "Delegating to {AgentName}" based on the input params
func (t *DelegateTaskTool) DynamicDisplayName(params map[string]interface{}) string {
	agentID, _ := params["agent_id"].(string)
	if agentID != "" && t.Client != nil {
		name := t.Client.GetAgentName(agentID)
		return "Delegating to " + name
	}
	return ""
}

func (t *DelegateTaskTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"agent_id": map[string]interface{}{
				"type":        "string",
				"description": "ID of the target agent (must have tasks feature enabled)",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Task title - clear and descriptive",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Detailed task instructions for the agent",
			},
			"priority": map[string]interface{}{
				"type":        "integer",
				"description": "Task priority (1-10, higher is more urgent)",
				"minimum":     1,
				"maximum":     10,
				"default":     5,
			},
			"execute_at": map[string]interface{}{
				"type":        "string",
				"description": "Optional: ISO datetime for scheduled execution (empty = as soon as possible)",
			},
		},
		"required": []string{"agent_id", "title", "description"},
	}
}

func (t *DelegateTaskTool) Execute(params map[string]interface{}) (interface{}, error) {
	agentID, _ := params["agent_id"].(string)
	title, _ := params["title"].(string)
	description, _ := params["description"].(string)
	priority, _ := params["priority"].(float64)
	executeAt, _ := params["execute_at"].(string)

	if agentID == "" || title == "" || description == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "agent_id, title, and description are required",
		}, nil
	}

	if priority == 0 {
		priority = 5
	}

	result, err := t.Client.DelegateTask(agentID, title, description, int(priority), executeAt)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	return result, nil
}

// CheckDelegatedTaskTool implements the check_delegated_task tool
type CheckDelegatedTaskTool struct {
	Client *AgentClient
}

func (t *CheckDelegatedTaskTool) Name() string {
	return "check_delegated_task"
}

func (t *CheckDelegatedTaskTool) DisplayName() string {
	return "Check Delegated Task"
}

func (t *CheckDelegatedTaskTool) Description() string {
	return "Check the status and result of a task previously delegated to another agent using delegate_task."
}

// DynamicDisplayName returns "Checking task on {AgentName}" based on the input params
func (t *CheckDelegatedTaskTool) DynamicDisplayName(params map[string]interface{}) string {
	agentID, _ := params["agent_id"].(string)
	if agentID != "" && t.Client != nil {
		name := t.Client.GetAgentName(agentID)
		return "Checking task on " + name
	}
	return ""
}

func (t *CheckDelegatedTaskTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"agent_id": map[string]interface{}{
				"type":        "string",
				"description": "ID of the agent where the task was delegated",
			},
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "Task ID returned from delegate_task",
			},
		},
		"required": []string{"agent_id", "task_id"},
	}
}

func (t *CheckDelegatedTaskTool) Execute(params map[string]interface{}) (interface{}, error) {
	agentID, _ := params["agent_id"].(string)
	taskID, _ := params["task_id"].(string)

	if agentID == "" || taskID == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "agent_id and task_id are required",
		}, nil
	}

	result, err := t.Client.CheckDelegatedTask(agentID, taskID)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	return result, nil
}

// GetAgentActivityTool implements the get_agent_activity tool for coordinators
type GetAgentActivityTool struct {
	Client *AgentClient
}

func (t *GetAgentActivityTool) Name() string {
	return "get_agent_activity"
}

func (t *GetAgentActivityTool) DisplayName() string {
	return "Get Agent Activity"
}

func (t *GetAgentActivityTool) Description() string {
	return "Get a summary of what another agent has been doing recently. Returns active threads, message counts, tool usage, and per-thread activity summaries. Use this to check on worker agents and understand their recent activity without interrupting them."
}

// DynamicDisplayName returns "Checking activity of {AgentName}" based on the input params
func (t *GetAgentActivityTool) DynamicDisplayName(params map[string]interface{}) string {
	agentID, _ := params["agent_id"].(string)
	if agentID != "" && t.Client != nil {
		name := t.Client.GetAgentName(agentID)
		return "Checking activity of " + name
	}
	return ""
}

func (t *GetAgentActivityTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"agent_id": map[string]interface{}{
				"type":        "string",
				"description": "ID of the agent to check activity for (available agent IDs are in your system prompt)",
			},
			"since": map[string]interface{}{
				"type":        "string",
				"description": "How far back to look (e.g. '1h', '6h', '24h', '7d'). Default: '1h'",
				"default":     "1h",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of recent threads to return (default 20, max 100)",
				"default":     20,
			},
		},
		"required": []string{"agent_id"},
	}
}

func (t *GetAgentActivityTool) Execute(params map[string]interface{}) (interface{}, error) {
	agentID, _ := params["agent_id"].(string)
	since, _ := params["since"].(string)
	limit, _ := params["limit"].(float64)

	if agentID == "" {
		return map[string]interface{}{
			"success": false,
			"error":   "agent_id is required",
		}, nil
	}

	if since == "" {
		since = "1h"
	}
	if limit == 0 {
		limit = 20
	}

	result, err := t.Client.GetAgentActivity(agentID, since, int(limit))
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	return result, nil
}
