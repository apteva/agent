package threads

import (
	"time"
)

// Thread represents a conversation thread
type Thread struct {
	ID           string                 `json:"id"`
	Title        *string                `json:"title"`
	Activity     *string                `json:"activity"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Metadata     map[string]interface{} `json:"metadata"`
	MessageCount int                    `json:"message_count"`
}

// Message represents a single message in a thread
type Message struct {
	ID        string                 `json:"id"`
	ThreadID  string                 `json:"thread_id"`
	Role      string                 `json:"role"`
	Content   interface{}            `json:"content"` // Can be string or []ContentBlock
	Model     *string                `json:"model"`
	CreatedAt time.Time              `json:"created_at"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// MessageSaver interface defines methods for saving and retrieving messages
type MessageSaver interface {
	SaveMessage(threadID, role string, content interface{}, model *string, metadata map[string]interface{}) error
	CreateThread(title string) (string, error)
	GetThreadMessages(threadID string) ([]Message, error)
	GetThreadTaskID(threadID string) string
	UpdateThreadMetadata(threadID string, metadata map[string]interface{}) error
	MergeThreadMetadata(threadID string, newMetadata map[string]interface{}) error
	UpdateThreadActivity(threadID, activity string, title *string) error
}
