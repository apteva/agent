package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/apteva/agent/tools"
)

func TestToolRegistry(t *testing.T) {
	t.Run("GetGlobalRegistry", func(t *testing.T) {
		registry := tools.GetGlobalRegistry()
		AssertNotNil(t, registry)

		// Test that built-in tools are registered
		toolDefs := registry.ListTools()
		AssertEqual(t, true, len(toolDefs) >= 2) // At least get_time and send_notification

		// Check for specific tools
		foundGetTime := false
		foundSendNotification := false

		for _, def := range toolDefs {
			if def.Name == "get_time" {
				foundGetTime = true
				AssertEqual(t, "Get the current date and time in various formats", def.Description)
			}
			if def.Name == "send_notification" {
				foundSendNotification = true
				AssertEqual(t, "Send a notification message to the user", def.Description)
			}
		}

		AssertEqual(t, true, foundGetTime)
		AssertEqual(t, true, foundSendNotification)
	})

	t.Run("GetTool", func(t *testing.T) {
		// Test getting existing tool
		tool, err := tools.GetTool("get_time")
		AssertNil(t, err)
		AssertNotNil(t, tool)
		AssertEqual(t, "get_time", tool.Name())

		// Test getting non-existent tool
		_, err = tools.GetTool("non_existent_tool")
		AssertNotNil(t, err)
		AssertContains(t, err.Error(), "not found")
	})

	t.Run("RegisterNewTool", func(t *testing.T) {
		registry := tools.NewRegistry()

		mockTool := &MockTool{
			ToolName:        "test_registry_tool",
			ToolDescription: "A tool for testing the registry",
			Schema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"input": map[string]interface{}{
						"type": "string",
					},
				},
			},
			Result: "test result",
		}

		registry.RegisterTool(mockTool)

		// Test retrieval
		retrievedTool, err := registry.GetTool("test_registry_tool")
		AssertNil(t, err)
		AssertEqual(t, "test_registry_tool", retrievedTool.Name())
		AssertEqual(t, "A tool for testing the registry", retrievedTool.Description())
	})

	t.Run("GetToolsForNames", func(t *testing.T) {
		names := []string{"get_time", "send_notification", "non_existent"}
		definitions := tools.GetToolsForNames(names)

		AssertEqual(t, 2, len(definitions)) // Only existing tools should be returned

		foundNames := make(map[string]bool)
		for _, def := range definitions {
			foundNames[def.Name] = true
		}

		AssertEqual(t, true, foundNames["get_time"])
		AssertEqual(t, true, foundNames["send_notification"])
		AssertEqual(t, false, foundNames["non_existent"])
	})
}

func TestGetTimeTool(t *testing.T) {
	tool, err := tools.GetTool("get_time")
	AssertNil(t, err)

	t.Run("BasicExecution", func(t *testing.T) {
		result, err := tool.Execute(map[string]interface{}{})
		AssertNil(t, err)
		AssertNotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		AssertEqual(t, true, ok)

		// Check required fields
		AssertNotNil(t, resultMap["current_time"])
		AssertNotNil(t, resultMap["timezone"])
		AssertNotNil(t, resultMap["format"])
		AssertEqual(t, "readable", resultMap["format"])

		// Check additional fields
		AssertNotNil(t, resultMap["iso_date"])
		AssertNotNil(t, resultMap["iso_time"])
		AssertNotNil(t, resultMap["day_of_week"])
		AssertNotNil(t, resultMap["day_of_year"])
		AssertNotNil(t, resultMap["week_number"])
	})

	t.Run("RFC3339Format", func(t *testing.T) {
		params := map[string]interface{}{
			"format": "rfc3339",
		}

		result, err := tool.Execute(params)
		AssertNil(t, err)

		resultMap := result.(map[string]interface{})
		AssertEqual(t, "rfc3339", resultMap["format"])

		// Verify RFC3339 format
		timeStr, ok := resultMap["current_time"].(string)
		AssertEqual(t, true, ok)
		_, err = time.Parse(time.RFC3339, timeStr)
		AssertNil(t, err)
	})

	t.Run("UnixFormat", func(t *testing.T) {
		params := map[string]interface{}{
			"format": "unix",
		}

		result, err := tool.Execute(params)
		AssertNil(t, err)

		resultMap := result.(map[string]interface{})
		AssertEqual(t, "unix", resultMap["format"])

		// Verify unix timestamp
		timestamp, ok := resultMap["current_time"].(int64)
		AssertEqual(t, true, ok)
		AssertEqual(t, true, timestamp > 0)
	})

	t.Run("TimezoneHandling", func(t *testing.T) {
		params := map[string]interface{}{
			"timezone": "UTC",
			"format":   "rfc3339",
		}

		result, err := tool.Execute(params)
		AssertNil(t, err)

		resultMap := result.(map[string]interface{})
		AssertContains(t, resultMap["timezone"].(string), "UTC")

		// Test with invalid timezone
		params["timezone"] = "Invalid/Timezone"
		_, err = tool.Execute(params)
		AssertNotNil(t, err)
		AssertContains(t, err.Error(), "invalid timezone")
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		params := map[string]interface{}{
			"format": "invalid_format",
		}

		_, err := tool.Execute(params)
		AssertNotNil(t, err)
		AssertContains(t, err.Error(), "invalid format")
	})

	t.Run("ToolSchema", func(t *testing.T) {
		schema := tool.InputSchema()
		AssertNotNil(t, schema)

		// Check schema structure
		AssertEqual(t, "object", schema["type"])
		properties, ok := schema["properties"].(map[string]interface{})
		AssertEqual(t, true, ok)

		// Check timezone property
		timezone, ok := properties["timezone"].(map[string]interface{})
		AssertEqual(t, true, ok)
		AssertEqual(t, "string", timezone["type"])

		// Check format property
		format, ok := properties["format"].(map[string]interface{})
		AssertEqual(t, true, ok)
		AssertEqual(t, "string", format["type"])

		// Check enum values
		enumValues, ok := format["enum"].([]string)
		AssertEqual(t, true, ok)
		AssertEqual(t, 3, len(enumValues))
	})
}

func TestSendNotificationTool(t *testing.T) {
	tool, err := tools.GetTool("send_notification")
	AssertNil(t, err)

	t.Run("BasicExecution", func(t *testing.T) {
		params := map[string]interface{}{
			"message": "Test notification message",
		}

		result, err := tool.Execute(params)
		AssertNil(t, err)
		AssertNotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		AssertEqual(t, true, ok)

		// Check required fields
		AssertEqual(t, "sent", resultMap["status"])
		AssertEqual(t, "Test notification message", resultMap["message"])
		AssertEqual(t, "medium", resultMap["urgency"]) // default
		AssertEqual(t, "Notification", resultMap["title"]) // default
		AssertNotNil(t, resultMap["notification_id"])
		AssertNotNil(t, resultMap["timestamp"])
		AssertEqual(t, "mock_system", resultMap["delivery_method"])
	})

	t.Run("CustomParameters", func(t *testing.T) {
		params := map[string]interface{}{
			"message": "Custom notification",
			"urgency": "high",
			"title":   "Important Alert",
		}

		result, err := tool.Execute(params)
		AssertNil(t, err)

		resultMap := result.(map[string]interface{})
		AssertEqual(t, "Custom notification", resultMap["message"])
		AssertEqual(t, "high", resultMap["urgency"])
		AssertEqual(t, "Important Alert", resultMap["title"])
	})

	t.Run("MissingMessage", func(t *testing.T) {
		params := map[string]interface{}{
			"urgency": "high",
		}

		_, err := tool.Execute(params)
		AssertNotNil(t, err)
		AssertContains(t, err.Error(), "message parameter is required")
	})

	t.Run("InvalidMessageType", func(t *testing.T) {
		params := map[string]interface{}{
			"message": 123, // Should be string
		}

		_, err := tool.Execute(params)
		AssertNotNil(t, err)
		AssertContains(t, err.Error(), "must be a string")
	})

	t.Run("NotificationID", func(t *testing.T) {
		params := map[string]interface{}{
			"message": "Test message",
		}

		result1, err := tool.Execute(params)
		AssertNil(t, err)

		// Small delay to ensure different timestamps
		time.Sleep(time.Millisecond * 10)

		result2, err := tool.Execute(params)
		AssertNil(t, err)

		// IDs should be different (timestamp-based)
		id1 := result1.(map[string]interface{})["notification_id"]
		id2 := result2.(map[string]interface{})["notification_id"]
		AssertEqual(t, false, id1 == id2)

		// IDs should follow expected format
		idStr1, ok := id1.(string)
		AssertEqual(t, true, ok)
		AssertContains(t, idStr1, "notif_")
	})

	t.Run("ToolSchema", func(t *testing.T) {
		schema := tool.InputSchema()
		AssertNotNil(t, schema)

		// Check schema structure
		AssertEqual(t, "object", schema["type"])
		properties, ok := schema["properties"].(map[string]interface{})
		AssertEqual(t, true, ok)

		// Check message property
		message, ok := properties["message"].(map[string]interface{})
		AssertEqual(t, true, ok)
		AssertEqual(t, "string", message["type"])

		// Check urgency property
		urgency, ok := properties["urgency"].(map[string]interface{})
		AssertEqual(t, true, ok)
		AssertEqual(t, "string", urgency["type"])

		enumValues, ok := urgency["enum"].([]string)
		AssertEqual(t, true, ok)
		AssertEqual(t, 3, len(enumValues))

		// Check required fields
		required, ok := schema["required"].([]string)
		AssertEqual(t, true, ok)
		AssertEqual(t, 1, len(required))
		AssertEqual(t, "message", required[0])
	})
}

func TestToolExecution(t *testing.T) {
	t.Run("ConcurrentToolExecution", func(t *testing.T) {
		tool, err := tools.GetTool("get_time")
		AssertNil(t, err)

		numGoroutines := 5
		done := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(index int) {
				defer func() { done <- true }()

				params := map[string]interface{}{
					"format": "unix",
				}

				result, err := tool.Execute(params)
				if err != nil {
					t.Errorf("Goroutine %d failed: %v", index, err)
					return
				}

				resultMap := result.(map[string]interface{})
				if resultMap["format"] != "unix" {
					t.Errorf("Goroutine %d got wrong format: %v", index, resultMap["format"])
				}
			}(i)
		}

		for i := 0; i < numGoroutines; i++ {
			<-done
		}
	})

	t.Run("ToolChaining", func(t *testing.T) {
		// Get time first
		timeTool, err := tools.GetTool("get_time")
		AssertNil(t, err)

		timeResult, err := timeTool.Execute(map[string]interface{}{
			"format": "readable",
		})
		AssertNil(t, err)

		timeMap := timeResult.(map[string]interface{})
		timeStr := timeMap["current_time"].(string)

		// Use time in notification
		notifyTool, err := tools.GetTool("send_notification")
		AssertNil(t, err)

		notifyResult, err := notifyTool.Execute(map[string]interface{}{
			"message": fmt.Sprintf("Current time is: %s", timeStr),
			"title":   "Time Report",
		})
		AssertNil(t, err)

		notifyMap := notifyResult.(map[string]interface{})
		AssertEqual(t, "sent", notifyMap["status"])
		AssertContains(t, notifyMap["message"].(string), timeStr)
	})
}

func TestMockTool(t *testing.T) {
	t.Run("MockToolCreation", func(t *testing.T) {
		mockResult := map[string]interface{}{
			"data":   "test data",
			"status": "success",
		}

		mockTool := RegisterMockTool("test_mock", "A mock tool for testing", mockResult)
		AssertEqual(t, "test_mock", mockTool.Name())
		AssertEqual(t, "A mock tool for testing", mockTool.Description())

		// Test execution
		result, err := mockTool.Execute(map[string]interface{}{"input": "test"})
		AssertNil(t, err)
		AssertEqual(t, mockResult, result)
	})

	t.Run("MockToolError", func(t *testing.T) {
		mockTool := &MockTool{
			ToolName:        "failing_mock",
			ToolDescription: "A mock tool that fails",
			Schema:          map[string]interface{}{},
			ExecuteError:    fmt.Errorf("mock tool error"),
		}

		registry := tools.GetGlobalRegistry()
		registry.RegisterTool(mockTool)

		tool, err := tools.GetTool("failing_mock")
		AssertNil(t, err)

		_, err = tool.Execute(map[string]interface{}{})
		AssertNotNil(t, err)
		AssertContains(t, err.Error(), "mock tool error")
	})
}

func TestToolDefinition(t *testing.T) {
	t.Run("ToolDefinitionCreation", func(t *testing.T) {
		definition := tools.ToolDefinition{
			Name:        "test_definition",
			Description: "A test tool definition",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"param1": map[string]interface{}{
						"type": "string",
					},
				},
			},
		}

		AssertEqual(t, "test_definition", definition.Name)
		AssertEqual(t, "A test tool definition", definition.Description)
		AssertNotNil(t, definition.InputSchema)

		// Check schema structure
		schema := definition.InputSchema
		AssertEqual(t, "object", schema["type"])
		properties, ok := schema["properties"].(map[string]interface{})
		AssertEqual(t, true, ok)
		AssertNotNil(t, properties["param1"])
	})
}

func BenchmarkToolExecution(b *testing.B) {
	tool, err := tools.GetTool("get_time")
	if err != nil {
		b.Fatalf("Failed to get tool: %v", err)
	}

	params := map[string]interface{}{
		"format": "unix",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Execute(params)
		if err != nil {
			b.Fatalf("Tool execution failed: %v", err)
		}
	}
}

func BenchmarkToolRegistryAccess(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tools.GetTool("get_time")
		if err != nil {
			b.Fatalf("Tool retrieval failed: %v", err)
		}
	}
}