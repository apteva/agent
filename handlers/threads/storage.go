package threads

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"agent-server/events"

	"github.com/google/uuid"
)

// DatabaseMessageSaver implements MessageSaver using a SQL database
type DatabaseMessageSaver struct {
	db *sql.DB
}

// NewDatabaseMessageSaver creates a new DatabaseMessageSaver
func NewDatabaseMessageSaver(database *sql.DB) *DatabaseMessageSaver {
	return &DatabaseMessageSaver{db: database}
}

// CreateThread creates a new conversation thread
func (d *DatabaseMessageSaver) CreateThread(title string) (string, error) {
	threadID := uuid.New().String()
	createdAt := time.Now()

	dbStart := time.Now()
	result, err := d.db.Exec(
		"INSERT INTO threads (id, title, created_at, updated_at, metadata) VALUES (?, ?, ?, ?, ?)",
		threadID, title, createdAt, createdAt, "{}",
	)
	if err != nil {
		return "", fmt.Errorf("failed to create thread: %w", err)
	}

	// Get affected rows
	rowsAffected, _ := result.RowsAffected()

	// Publish detailed database insert event
	eventBus := events.GetEventBus()
	dbEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBInsert, events.LevelInfo).
		WithData("table", "threads").
		WithData("operation", "INSERT").
		WithData("thread_id", threadID).
		WithData("title", title).
		WithData("rows_affected", rowsAffected).
		WithData("query", "INSERT INTO threads (id, title, created_at, updated_at, metadata)").
		WithDuration(dbStart)
	eventBus.Publish(dbEvent)

	return threadID, nil
}

// SaveMessage saves a message to a thread
func (d *DatabaseMessageSaver) SaveMessage(threadID, role string, content interface{}, model *string, metadata map[string]interface{}) error {
	messageID := uuid.New().String()

	// Convert content to JSON string for storage
	var contentStr string
	switch v := content.(type) {
	case string:
		// For string content, store directly without JSON encoding
		contentStr = v
	default:
		// For structured content (content blocks), marshal to JSON
		contentJSON, err := json.Marshal(content)
		if err != nil {
			return fmt.Errorf("failed to marshal content: %w", err)
		}
		contentStr = string(contentJSON)
	}

	// Convert metadata to JSON string
	metadataJSON := "{}"
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataJSON = string(metadataBytes)
	}

	// Insert message
	dbInsertStart := time.Now()
	createdAt := time.Now()
	result, err := d.db.Exec(
		"INSERT INTO messages (id, thread_id, role, content, model, created_at, metadata) VALUES (?, ?, ?, ?, ?, ?, ?)",
		messageID, threadID, role, contentStr, model, createdAt, metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	// Get affected rows
	rowsAffected, _ := result.RowsAffected()

	// Publish detailed database insert event
	eventBus := events.GetEventBus()
	insertEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBInsert, events.LevelInfo).
		WithThread(threadID).
		WithData("table", "messages").
		WithData("operation", "INSERT").
		WithData("message_id", messageID).
		WithData("role", role).
		WithData("content_length", len(contentStr)).
		WithData("model", model).
		WithData("rows_affected", rowsAffected).
		WithData("query", "INSERT INTO messages (id, thread_id, role, content, model, created_at, metadata)").
		WithDuration(dbInsertStart)
	eventBus.Publish(insertEvent)

	// Update thread timestamp
	updateStart := time.Now()
	updatedAt := time.Now()
	updateResult, err := d.db.Exec("UPDATE threads SET updated_at = ? WHERE id = ?", updatedAt, threadID)
	if err != nil {
		log.Printf("Warning: failed to update thread timestamp: %v", err)
	} else {
		// Get affected rows
		rowsAffected, _ := updateResult.RowsAffected()

		// Publish detailed database update event
		updateEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBUpdate, events.LevelDebug).
			WithThread(threadID).
			WithData("table", "threads").
			WithData("operation", "UPDATE").
			WithData("fields", map[string]interface{}{
				"updated_at": updatedAt.Format(time.RFC3339),
			}).
			WithData("where_clause", fmt.Sprintf("id = '%s'", threadID)).
			WithData("rows_affected", rowsAffected).
			WithData("query", "UPDATE threads SET updated_at = ? WHERE id = ?").
			WithDuration(updateStart)
		eventBus.Publish(updateEvent)
	}

	return nil
}

// UpdateThreadMetadata updates the metadata field of a thread
func (d *DatabaseMessageSaver) UpdateThreadMetadata(threadID string, metadata map[string]interface{}) error {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = d.db.Exec("UPDATE threads SET metadata = ?, updated_at = ? WHERE id = ?",
		string(metadataJSON), time.Now(), threadID)
	if err != nil {
		return fmt.Errorf("failed to update thread metadata: %w", err)
	}

	return nil
}

// MergeThreadMetadata merges new metadata into existing thread metadata
func (d *DatabaseMessageSaver) MergeThreadMetadata(threadID string, newMetadata map[string]interface{}) error {
	// Get existing metadata
	var existingMetadataStr string
	err := d.db.QueryRow("SELECT COALESCE(metadata, '{}') FROM threads WHERE id = ?", threadID).Scan(&existingMetadataStr)
	if err != nil {
		return fmt.Errorf("failed to get existing metadata: %w", err)
	}

	var existingMetadata map[string]interface{}
	if err := json.Unmarshal([]byte(existingMetadataStr), &existingMetadata); err != nil {
		existingMetadata = make(map[string]interface{})
	}

	// Merge new metadata
	for k, v := range newMetadata {
		existingMetadata[k] = v
	}

	return d.UpdateThreadMetadata(threadID, existingMetadata)
}

// GetThreadTaskID retrieves the task_id from thread metadata if present
func (d *DatabaseMessageSaver) GetThreadTaskID(threadID string) string {
	var metadataStr string
	err := d.db.QueryRow("SELECT metadata FROM threads WHERE id = ?", threadID).Scan(&metadataStr)
	if err != nil {
		return ""
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
		return ""
	}

	if taskID, ok := metadata["task_id"].(string); ok {
		return taskID
	}
	return ""
}

// GetThreadMessages retrieves all messages for a thread
func (d *DatabaseMessageSaver) GetThreadMessages(threadID string) ([]Message, error) {
	dbQueryStart := time.Now()
	query := "SELECT id, thread_id, role, content, model, created_at, metadata FROM messages WHERE thread_id = ? ORDER BY created_at ASC"
	rows, err := d.db.Query(query, threadID)
	if err != nil {
		// Publish database error event
		eventBus := events.GetEventBus()
		errorEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBError, events.LevelError).
			WithThread(threadID).
			WithData("table", "messages").
			WithData("operation", "SELECT").
			WithData("query", query).
			WithError(err).
			WithDuration(dbQueryStart)
		eventBus.Publish(errorEvent)
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var message Message
		var contentStr, metadataStr string

		err := rows.Scan(&message.ID, &message.ThreadID, &message.Role, &contentStr, &message.Model, &message.CreatedAt, &metadataStr)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}

		// Parse content - could be string or JSON content blocks
		var content interface{}

		// First check if it looks like JSON (starts with { or [)
		trimmedContent := strings.TrimSpace(contentStr)
		if strings.HasPrefix(trimmedContent, "{") || strings.HasPrefix(trimmedContent, "[") {
			// Try to parse as JSON
			if err := json.Unmarshal([]byte(contentStr), &content); err != nil {
				// If JSON parsing fails, treat as plain string
				content = contentStr
			} else {
				// Successfully parsed as JSON - keep the parsed structure
				// This will be a map[string]interface{} for tool_use/tool_result objects
				// or []interface{} for arrays

				// DEBUG: Log computer tool content from database
				if blocks, ok := content.([]interface{}); ok {
					for _, block := range blocks {
						if blockMap, ok := block.(map[string]interface{}); ok {
							if blockMap["type"] == "tool_use" && blockMap["name"] == "computer" {
								blockJSON, _ := json.Marshal(blockMap)
								log.Printf("🖥️  COMPUTER TOOL - Raw from DB (message %s): %s", message.ID, string(blockJSON))
							}
						}
					}
				}
			}
		} else {
			// Plain string content
			content = contentStr
		}
		message.Content = content

		// Parse metadata JSON
		if err := json.Unmarshal([]byte(metadataStr), &message.Metadata); err != nil {
			message.Metadata = make(map[string]interface{})
		}

		messages = append(messages, message)
	}

	// Publish detailed database query event
	eventBus := events.GetEventBus()
	queryEvent := events.NewEvent(events.CategoryDatabase, events.TypeDBQuery, events.LevelInfo).
		WithThread(threadID).
		WithData("table", "messages").
		WithData("operation", "SELECT").
		WithData("rows_returned", len(messages)).
		WithData("where_clause", fmt.Sprintf("thread_id = '%s'", threadID)).
		WithData("order_by", "created_at ASC").
		WithData("query", "SELECT id, thread_id, role, content, model, created_at, metadata FROM messages WHERE thread_id = ?").
		WithDuration(dbQueryStart)
	eventBus.Publish(queryEvent)

	return messages, nil
}
