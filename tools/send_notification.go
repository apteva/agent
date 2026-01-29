package tools

import (
	"fmt"
	"time"
)

type SendNotificationTool struct{}

func (t *SendNotificationTool) Name() string {
	return "send_notification"
}

func (t *SendNotificationTool) DisplayName() string {
	return "Send Notification"
}

func (t *SendNotificationTool) Description() string {
	return "Send a notification message to the user"
}

func (t *SendNotificationTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "The notification message to send",
			},
			"urgency": map[string]interface{}{
				"type":        "string",
				"description": "The urgency level of the notification",
				"enum":        []string{"low", "medium", "high"},
			},
			"title": map[string]interface{}{
				"type":        "string",
				"description": "Optional title for the notification",
			},
		},
		"required": []string{"message"},
	}
}

func (t *SendNotificationTool) Execute(params map[string]interface{}) (interface{}, error) {
	message, ok := params["message"].(string)
	if !ok {
		return nil, fmt.Errorf("message parameter is required and must be a string")
	}

	urgency := "medium"
	if u, ok := params["urgency"].(string); ok {
		urgency = u
	}

	title := "Notification"
	if t, ok := params["title"].(string); ok {
		title = t
	}

	// Mock notification - in real implementation this would send to notification service
	notificationID := fmt.Sprintf("notif_%d", time.Now().UnixNano())

	result := map[string]interface{}{
		"status":          "sent",
		"notification_id": notificationID,
		"title":           title,
		"message":         message,
		"urgency":         urgency,
		"timestamp":       time.Now().Format(time.RFC3339),
		"delivery_method": "mock_system",
	}

	return result, nil
}