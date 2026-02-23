package a2a

import "encoding/json"

// ============================================================================
// JSON-RPC 2.0
// ============================================================================

// JSONRPCRequest is the JSON-RPC 2.0 request envelope.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is the JSON-RPC 2.0 response envelope.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
	ErrCodeServerError    = -32000 // Application-level error
)

// ============================================================================
// Agent Card (served at /.well-known/agent.json)
// ============================================================================

// AgentCard describes an agent's identity, capabilities, and skills.
// Served at /.well-known/agent.json for A2A discovery.
type AgentCard struct {
	Name               string                       `json:"name"`
	Description        string                       `json:"description"`
	URL                string                       `json:"url"`
	Version            string                       `json:"version"`
	IconURL            string                       `json:"iconUrl,omitempty"`
	DocumentationURL   string                       `json:"documentationUrl,omitempty"`
	Provider           *AgentProvider               `json:"provider,omitempty"`
	Capabilities       AgentCapabilities            `json:"capabilities"`
	Skills             []AgentSkill                 `json:"skills"`
	DefaultInputModes  []string                     `json:"defaultInputModes"`
	DefaultOutputModes []string                     `json:"defaultOutputModes"`
	SecuritySchemes    map[string]SecurityScheme     `json:"securitySchemes,omitempty"`
	Security           []map[string][]string         `json:"security,omitempty"`
}

// AgentProvider identifies the organization behind the agent.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// AgentCapabilities declares what protocol features the agent supports.
type AgentCapabilities struct {
	Streaming              bool `json:"streaming"`
	PushNotifications      bool `json:"pushNotifications"`
	StateTransitionHistory bool `json:"stateTransitionHistory"`
}

// AgentSkill describes a specific capability the agent offers.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// SecurityScheme defines an authentication method (aligned with OpenAPI).
type SecurityScheme struct {
	Type        string `json:"type"`                  // "apiKey", "http", "oauth2", "openIdConnect"
	In          string `json:"in,omitempty"`           // "header", "query"
	Name        string `json:"name,omitempty"`         // Header/param name
	Scheme      string `json:"scheme,omitempty"`       // "bearer", "basic"
	Description string `json:"description,omitempty"`
}

// ============================================================================
// Messages and Parts
// ============================================================================

// Message is a single communication turn containing one or more Parts.
type Message struct {
	Role      string `json:"role"`                // "user" or "agent"
	MessageID string `json:"messageId,omitempty"`
	Parts     []Part `json:"parts"`
}

// Part is the fundamental content container. Each holds exactly one content type.
type Part struct {
	Kind     string                 `json:"kind"`               // "text", "file", "data"
	Text     string                 `json:"text,omitempty"`
	File     *FileContent           `json:"file,omitempty"`
	Data     map[string]interface{} `json:"data,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// FileContent represents a file attachment (inline bytes or URI reference).
type FileContent struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Bytes    string `json:"bytes,omitempty"` // base64-encoded
	URI      string `json:"uri,omitempty"`
}

// ============================================================================
// Tasks
// ============================================================================

// Task state constants following the A2A spec.
const (
	TaskStateSubmitted     = "submitted"
	TaskStateWorking       = "working"
	TaskStateCompleted     = "completed"
	TaskStateFailed        = "failed"
	TaskStateCanceled      = "canceled"
	TaskStateInputRequired = "input-required"
	TaskStateAuthRequired  = "auth-required"
	TaskStateRejected      = "rejected"
)

// Task is the A2A representation of a unit of work.
type Task struct {
	Kind      string                 `json:"kind"`                // Always "task"
	ID        string                 `json:"id"`
	ContextID string                 `json:"contextId,omitempty"`
	Status    TaskStatus             `json:"status"`
	History   []Message              `json:"history,omitempty"`
	Artifacts []Artifact             `json:"artifacts,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// TaskStatus represents the current state of a task.
type TaskStatus struct {
	State     string   `json:"state"`
	Message   *Message `json:"message,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
}

// Artifact is a tangible output generated during task execution.
type Artifact struct {
	ArtifactID  string `json:"artifactId,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Parts       []Part `json:"parts"`
	Index       int    `json:"index,omitempty"`
	Append      bool   `json:"append,omitempty"`
	LastChunk   bool   `json:"lastChunk,omitempty"`
}

// ============================================================================
// SSE Event Types (for message/stream)
// ============================================================================

// TaskStatusUpdateEvent is sent via SSE when a task's status changes.
type TaskStatusUpdateEvent struct {
	Kind   string     `json:"kind"`   // "status-update"
	TaskID string     `json:"taskId"`
	Status TaskStatus `json:"status"`
	Final  bool       `json:"final"`
}

// TaskArtifactUpdateEvent is sent via SSE for incremental artifact delivery.
type TaskArtifactUpdateEvent struct {
	Kind     string   `json:"kind"` // "artifact-update"
	TaskID   string   `json:"taskId"`
	Artifact Artifact `json:"artifact"`
}

// ============================================================================
// Push Notifications
// ============================================================================

// PushNotificationConfig configures webhook delivery for task updates.
type PushNotificationConfig struct {
	URL            string      `json:"url"`
	Token          string      `json:"token,omitempty"`
	Authentication *AuthConfig `json:"authentication,omitempty"`
}

// AuthConfig specifies authentication for push notification delivery.
type AuthConfig struct {
	Schemes []string `json:"schemes"`
	Token   string   `json:"token,omitempty"`
}

// ============================================================================
// Method Parameters
// ============================================================================

// MessageSendParams are the params for message/send and message/stream.
type MessageSendParams struct {
	Message       Message                `json:"message"`
	ContextID     string                 `json:"contextId,omitempty"`
	TaskID        string                 `json:"taskId,omitempty"`
	Configuration *SendMessageConfig     `json:"configuration,omitempty"`
}

// SendMessageConfig controls request behavior.
type SendMessageConfig struct {
	AcceptedOutputModes    []string                `json:"acceptedOutputModes,omitempty"`
	Blocking               *bool                   `json:"blocking,omitempty"`
	HistoryLength          *int                    `json:"historyLength,omitempty"`
	PushNotificationConfig *PushNotificationConfig `json:"pushNotificationConfig,omitempty"`
}

// TaskGetParams are the params for tasks/get.
type TaskGetParams struct {
	TaskID        string `json:"id"`
	HistoryLength *int   `json:"historyLength,omitempty"`
}

// TaskCancelParams are the params for tasks/cancel.
type TaskCancelParams struct {
	TaskID string `json:"id"`
}

// PushNotificationConfigParams are used for push notification CRUD operations.
type PushNotificationConfigParams struct {
	TaskID                 string                  `json:"id"`
	PushNotificationConfig *PushNotificationConfig `json:"pushNotificationConfig,omitempty"`
}
