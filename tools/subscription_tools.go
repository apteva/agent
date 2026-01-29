package tools

// Subscription tool wrappers that implement the Tool interface
// These allow the agent to subscribe to external events from services like Stripe, GitHub, etc.

// MCPWebhooksListToolWrapper wraps the mcp_webhooks_list function
type MCPWebhooksListToolWrapper struct{}

func (t *MCPWebhooksListToolWrapper) Name() string {
	return "mcp_webhooks_list"
}

func (t *MCPWebhooksListToolWrapper) DisplayName() string {
	return "List Available Event Subscriptions"
}

func (t *MCPWebhooksListToolWrapper) Description() string {
	return `List available events you can subscribe to from external services.

Returns a list of services (like Stripe, GitHub) and the events they can notify you about.
Use this to discover what events you can subscribe to, such as payment completions,
new customers, code pushes, etc.`
}

func (t *MCPWebhooksListToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"random_string": map[string]interface{}{
				"type":        "string",
				"description": "Dummy parameter, pass any value",
			},
		},
		"required": []string{"random_string"},
	}
}

func (t *MCPWebhooksListToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return MCPWebhooksListTool(params)
}

// SubscribeToolWrapper wraps the subscribe function
type SubscribeToolWrapper struct{}

func (t *SubscribeToolWrapper) Name() string {
	return "subscribe"
}

func (t *SubscribeToolWrapper) DisplayName() string {
	return "Subscribe to Events"
}

func (t *SubscribeToolWrapper) Description() string {
	return `Subscribe to events from external services like Stripe, GitHub, etc.

When events occur, they will be delivered to this agent and create a new thread
with the event data. You can optionally provide a prompt with instructions for
how to handle incoming events.

Example: subscribe to Stripe payment events to automatically send thank you emails,
or subscribe to GitHub push events to run code reviews.`
}

func (t *SubscribeToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"server": map[string]interface{}{
				"type":        "string",
				"description": "MCP server name (e.g., 'stripe', 'github')",
			},
			"events": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "List of event names to subscribe to (e.g., ['payment_intent.succeeded', 'customer.created'])",
			},
			"credential_id": map[string]interface{}{
				"type":        "integer",
				"description": "ID of the credential to use for this subscription (required for services that need authentication)",
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "A short title describing what this subscription is for (e.g., 'Payment notifications', 'PR review alerts'). Required.",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Optional instructions for handling events. This prompt will be included when events arrive.",
			},
		},
		"required": []string{"server", "events", "credential_id", "title"},
	}
}

func (t *SubscribeToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return SubscribeTool(params)
}

// SubscriptionsListToolWrapper wraps the subscriptions_list function
type SubscriptionsListToolWrapper struct{}

func (t *SubscriptionsListToolWrapper) Name() string {
	return "subscriptions_list"
}

func (t *SubscriptionsListToolWrapper) DisplayName() string {
	return "List Subscriptions"
}

func (t *SubscriptionsListToolWrapper) Description() string {
	return `List all your active event subscriptions.

Shows which services you're subscribed to, what events you're listening for,
and statistics like how many times events have been received.`
}

func (t *SubscriptionsListToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"random_string": map[string]interface{}{
				"type":        "string",
				"description": "Dummy parameter, pass any value",
			},
			"server": map[string]interface{}{
				"type":        "string",
				"description": "Optional: filter by server name",
			},
		},
		"required": []string{"random_string"},
	}
}

func (t *SubscriptionsListToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return SubscriptionsListTool(params)
}

// UpdateSubscriptionToolWrapper wraps the update_subscription function
type UpdateSubscriptionToolWrapper struct{}

func (t *UpdateSubscriptionToolWrapper) Name() string {
	return "update_subscription"
}

func (t *UpdateSubscriptionToolWrapper) DisplayName() string {
	return "Update Subscription"
}

func (t *UpdateSubscriptionToolWrapper) Description() string {
	return `Update an existing event subscription.

Identify the subscription by its title, then update the prompt, events, or rename it.
Allows you to change the prompt/instructions for how events should be handled,
or update which events you're subscribed to, without having to unsubscribe and resubscribe.`
}

func (t *UpdateSubscriptionToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "The title of the subscription to update (used to identify it)",
			},
			"new_title": map[string]interface{}{
				"type":        "string",
				"description": "Optional: rename the subscription to a new title",
			},
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "New instructions for handling events",
			},
			"events": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional: update the list of events to subscribe to",
			},
		},
		"required": []string{"title"},
	}
}

func (t *UpdateSubscriptionToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return UpdateSubscriptionTool(params)
}

// UnsubscribeToolWrapper wraps the unsubscribe function
type UnsubscribeToolWrapper struct{}

func (t *UnsubscribeToolWrapper) Name() string {
	return "unsubscribe"
}

func (t *UnsubscribeToolWrapper) DisplayName() string {
	return "Unsubscribe from Events"
}

func (t *UnsubscribeToolWrapper) Description() string {
	return `Cancel an event subscription by its title.

Stops receiving events for this subscription. Existing threads
created by previous events are not affected. Other subscriptions
from the same server are not affected.`
}

func (t *UnsubscribeToolWrapper) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "The title of the subscription to cancel",
			},
		},
		"required": []string{"title"},
	}
}

func (t *UnsubscribeToolWrapper) Execute(params map[string]interface{}) (interface{}, error) {
	return UnsubscribeTool(params)
}

// RegisterSubscriptionTools registers all subscription management tools
func RegisterSubscriptionTools() {
	registry := GetGlobalRegistry()
	registry.RegisterTool(&MCPWebhooksListToolWrapper{})
	registry.RegisterTool(&SubscribeToolWrapper{})
	registry.RegisterTool(&SubscriptionsListToolWrapper{})
	registry.RegisterTool(&UpdateSubscriptionToolWrapper{})
	registry.RegisterTool(&UnsubscribeToolWrapper{})
}
