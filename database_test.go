package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDatabaseMessageSaver(t *testing.T) {
	testDB := NewTestDatabase(t)
	defer testDB.Close()

	saver := testDB.GetMessageSaver()

	t.Run("CreateThread", func(t *testing.T) {
		threadID, err := saver.CreateThread("Test Thread")
		AssertNil(t, err)
		AssertNotNil(t, threadID)

		// Verify thread was created
		var count int
		err = testDB.DB.QueryRow("SELECT COUNT(*) FROM threads WHERE id = ?", threadID).Scan(&count)
		AssertNil(t, err)
		AssertEqual(t, 1, count)
	})

	t.Run("SaveMessage", func(t *testing.T) {
		threadID := CreateTestThread(t, saver)

		// Test string content
		err := saver.SaveMessage(threadID, "user", "Hello, world!", nil, nil)
		AssertNil(t, err)

		// Test structured content
		contentBlocks := []ContentBlock{
			{Type: "text", Text: "Hello"},
			{Type: "text", Text: "World"},
		}
		err = saver.SaveMessage(threadID, "assistant", contentBlocks, nil, nil)
		AssertNil(t, err)

		// Verify messages were saved
		var count int
		err = testDB.DB.QueryRow("SELECT COUNT(*) FROM messages WHERE thread_id = ?", threadID).Scan(&count)
		AssertNil(t, err)
		AssertEqual(t, 2, count)
	})

	t.Run("GetThreadMessages", func(t *testing.T) {
		threadID := CreateTestThread(t, saver)

		// Create test messages
		CreateTestMessage(t, saver, threadID, "user", "Hello")
		CreateTestMessage(t, saver, threadID, "assistant", "Hi there!")

		// Retrieve messages
		messages, err := saver.GetThreadMessages(threadID)
		AssertNil(t, err)
		AssertEqual(t, 2, len(messages))

		// Verify order (should be chronological)
		AssertEqual(t, "user", messages[0].Role)
		AssertEqual(t, "Hello", messages[0].Content)
		AssertEqual(t, "assistant", messages[1].Role)
		AssertEqual(t, "Hi there!", messages[1].Content)
	})

	t.Run("SaveAndRetrieveToolMessage", func(t *testing.T) {
		threadID := CreateTestThread(t, saver)

		// Create tool use message
		toolUseContent := map[string]interface{}{
			"type":  "tool_use",
			"id":    "tool_123",
			"name":  "test_tool",
			"input": map[string]interface{}{"query": "test"},
		}

		err := saver.SaveMessage(threadID, "assistant", toolUseContent, nil, nil)
		AssertNil(t, err)

		// Create tool result message
		toolResultContent := map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": "tool_123",
			"content":     "Tool executed successfully",
			"is_error":    false,
		}

		err = saver.SaveMessage(threadID, "user", toolResultContent, nil, nil)
		AssertNil(t, err)

		// Retrieve and verify
		messages, err := saver.GetThreadMessages(threadID)
		AssertNil(t, err)
		AssertEqual(t, 2, len(messages))

		// Check tool use message
		toolUseMsg := messages[0]
		AssertEqual(t, "assistant", toolUseMsg.Role)

		// Content should be parsed as map
		contentMap, ok := toolUseMsg.Content.(map[string]interface{})
		AssertEqual(t, true, ok)
		AssertEqual(t, "tool_use", contentMap["type"])
		AssertEqual(t, "tool_123", contentMap["id"])

		// Check tool result message
		toolResultMsg := messages[1]
		AssertEqual(t, "user", toolResultMsg.Role)

		resultMap, ok := toolResultMsg.Content.(map[string]interface{})
		AssertEqual(t, true, ok)
		AssertEqual(t, "tool_result", resultMap["type"])
		AssertEqual(t, "tool_123", resultMap["tool_use_id"])
	})

	t.Run("MessageWithMetadata", func(t *testing.T) {
		threadID := CreateTestThread(t, saver)

		metadata := map[string]interface{}{
			"user_id":   "123",
			"timestamp": time.Now().Unix(),
		}

		model := "claude-sonnet-4-20250514"
		err := saver.SaveMessage(threadID, "user", "Test message", &model, metadata)
		AssertNil(t, err)

		messages, err := saver.GetThreadMessages(threadID)
		AssertNil(t, err)
		AssertEqual(t, 1, len(messages))

		message := messages[0]
		AssertEqual(t, "claude-sonnet-4-20250514", *message.Model)
		AssertNotNil(t, message.Metadata)
		AssertEqual(t, "123", message.Metadata["user_id"])
	})
}

func TestDatabaseOperations(t *testing.T) {
	testDB := NewTestDatabase(t)
	defer testDB.Close()

	t.Run("ThreadOperations", func(t *testing.T) {
		// Test direct database operations
		threadID := uuid.New().String()
		title := "Direct DB Test"

		_, err := testDB.DB.Exec(
			"INSERT INTO threads (id, title, created_at, updated_at, metadata) VALUES (?, ?, ?, ?, ?)",
			threadID, title, time.Now(), time.Now(), "{}",
		)
		AssertNil(t, err)

		// Verify insertion
		var retrievedTitle string
		err = testDB.DB.QueryRow("SELECT title FROM threads WHERE id = ?", threadID).Scan(&retrievedTitle)
		AssertNil(t, err)
		AssertEqual(t, title, retrievedTitle)

		// Test update
		newTitle := "Updated Title"
		_, err = testDB.DB.Exec("UPDATE threads SET title = ? WHERE id = ?", newTitle, threadID)
		AssertNil(t, err)

		err = testDB.DB.QueryRow("SELECT title FROM threads WHERE id = ?", threadID).Scan(&retrievedTitle)
		AssertNil(t, err)
		AssertEqual(t, newTitle, retrievedTitle)
	})

	t.Run("MessageOperations", func(t *testing.T) {
		// Create thread first
		threadID := uuid.New().String()
		_, err := testDB.DB.Exec(
			"INSERT INTO threads (id, title, created_at, updated_at, metadata) VALUES (?, ?, ?, ?, ?)",
			threadID, "Test Thread", time.Now(), time.Now(), "{}",
		)
		AssertNil(t, err)

		// Insert message
		messageID := uuid.New().String()
		content := "Test message content"

		_, err = testDB.DB.Exec(
			"INSERT INTO messages (id, thread_id, role, content, model, created_at, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)",
			messageID, threadID, "user", content, nil, time.Now(), "{}",
		)
		AssertNil(t, err)

		// Verify insertion
		var retrievedContent string
		err = testDB.DB.QueryRow("SELECT content FROM messages WHERE id = ?", messageID).Scan(&retrievedContent)
		AssertNil(t, err)
		AssertEqual(t, content, retrievedContent)

		// Test cascade delete
		_, err = testDB.DB.Exec("DELETE FROM threads WHERE id = ?", threadID)
		AssertNil(t, err)

		// Verify message was also deleted due to foreign key constraint
		var count int
		err = testDB.DB.QueryRow("SELECT COUNT(*) FROM messages WHERE thread_id = ?", threadID).Scan(&count)
		AssertNil(t, err)
		AssertEqual(t, 0, count)
	})

	t.Run("JSONContentHandling", func(t *testing.T) {
		threadID := uuid.New().String()
		_, err := testDB.DB.Exec(
			"INSERT INTO threads (id, title, created_at, updated_at, metadata) VALUES (?, ?, ?, ?, ?)",
			threadID, "JSON Test", time.Now(), time.Now(), "{}",
		)
		AssertNil(t, err)

		// Test JSON content storage and retrieval
		contentBlocks := []ContentBlock{
			{Type: "text", Text: "Hello"},
			{Type: "tool_use", ID: "123", Name: "test", Input: map[string]interface{}{"key": "value"}},
		}

		contentJSON, err := json.Marshal(contentBlocks)
		AssertNil(t, err)

		messageID := uuid.New().String()
		_, err = testDB.DB.Exec(
			"INSERT INTO messages (id, thread_id, role, content, model, created_at, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)",
			messageID, threadID, "assistant", string(contentJSON), nil, time.Now(), "{}",
		)
		AssertNil(t, err)

		// Retrieve and parse
		var retrievedContent string
		err = testDB.DB.QueryRow("SELECT content FROM messages WHERE id = ?", messageID).Scan(&retrievedContent)
		AssertNil(t, err)

		var parsedBlocks []ContentBlock
		err = json.Unmarshal([]byte(retrievedContent), &parsedBlocks)
		AssertNil(t, err)
		AssertEqual(t, 2, len(parsedBlocks))
		AssertEqual(t, "text", parsedBlocks[0].Type)
		AssertEqual(t, "Hello", parsedBlocks[0].Text)
		AssertEqual(t, "tool_use", parsedBlocks[1].Type)
		AssertEqual(t, "123", parsedBlocks[1].ID)
	})
}

func TestDatabaseConcurrency(t *testing.T) {
	testDB := NewTestDatabase(t)
	defer testDB.Close()

	saver := testDB.GetMessageSaver()

	t.Run("ConcurrentMessageCreation", func(t *testing.T) {
		threadID := CreateTestThread(t, saver)

		// Create multiple messages concurrently
		done := make(chan bool, 5)

		for i := 0; i < 5; i++ {
			go func(index int) {
				defer func() { done <- true }()

				content := fmt.Sprintf("Message %d", index)
				err := saver.SaveMessage(threadID, "user", content, nil, nil)
				if err != nil {
					t.Errorf("Failed to save message %d: %v", index, err)
				}
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 5; i++ {
			<-done
		}

		// Verify all messages were saved
		messages, err := saver.GetThreadMessages(threadID)
		AssertNil(t, err)
		AssertEqual(t, 5, len(messages))
	})

	t.Run("ConcurrentThreadCreation", func(t *testing.T) {
		done := make(chan string, 3)

		for i := 0; i < 3; i++ {
			go func(index int) {
				title := fmt.Sprintf("Thread %d", index)
				threadID, err := saver.CreateThread(title)
				if err != nil {
					t.Errorf("Failed to create thread %d: %v", index, err)
					done <- ""
				} else {
					done <- threadID
				}
			}(i)
		}

		// Collect results
		var threadIDs []string
		for i := 0; i < 3; i++ {
			threadID := <-done
			if threadID != "" {
				threadIDs = append(threadIDs, threadID)
			}
		}

		AssertEqual(t, 3, len(threadIDs))

		// Verify all threads are unique
		uniqueIDs := make(map[string]bool)
		for _, id := range threadIDs {
			AssertEqual(t, false, uniqueIDs[id]) // Should not exist yet
			uniqueIDs[id] = true
		}
	})
}