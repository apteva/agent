package mcp

import (
	"encoding/json"
	"fmt"
)

// Standard MCP Protocol types (JSON-RPC 2.0)
// Reference: https://modelcontextprotocol.io/specification/2025-11-25

// Current MCP protocol version
const MCPProtocolVersion = "2025-11-25"

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// JSONRPCNotification represents a JSON-RPC 2.0 notification (no id field).
// Servers send these for progress, log, and resource change events.
type JSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCMessage is a raw JSON-RPC message used for routing.
// It can represent either a response (has ID) or a notification (has Method, no ID).
// Used in the transport layer to distinguish message types before routing.
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`      // nil for notifications, present for responses
	Method  string          `json:"method,omitempty"`  // present for notifications/requests
	Params  json.RawMessage `json:"params,omitempty"`  // for notifications
	Result  json.RawMessage `json:"result,omitempty"`  // for responses
	Error   *JSONRPCError   `json:"error,omitempty"`   // for error responses
}

// IsNotification returns true if this message is a notification (no id, has method).
func (m *JSONRPCMessage) IsNotification() bool {
	return m.ID == nil && m.Method != ""
}

// IsResponse returns true if this message is a response (has id).
func (m *JSONRPCMessage) IsResponse() bool {
	return m.ID != nil
}

// Standard MCP error codes
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// InitializeParams for the initialize request
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

// ClientCapabilities declares what the client supports
type ClientCapabilities struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

// RootsCapability for filesystem roots
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability for LLM sampling
type SamplingCapability struct{}

// ClientInfo identifies the client
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult from the initialize response
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

// ServerCapabilities declares what the server supports
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
	Logging   *LoggingCapability   `json:"logging,omitempty"`
}

// ToolsCapability for tool support
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability for resource support
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability for prompt support
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// LoggingCapability for logging support
type LoggingCapability struct{}

// ServerInfo identifies the server
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolsListResult from tools/list response
type ToolsListResult struct {
	Tools      []MCPToolDefinition `json:"tools"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

// MCPToolDefinition represents a tool in standard MCP format
type MCPToolDefinition struct {
	Name         string                 `json:"name"`
	Title        string                 `json:"title,omitempty"`        // Human-readable title
	DisplayName  string                 `json:"displayName,omitempty"`  // Alternative display name field
	Description  string                 `json:"description,omitempty"`
	InputSchema  map[string]interface{} `json:"inputSchema"`
	OutputSchema map[string]interface{} `json:"outputSchema,omitempty"` // 2025-06-18 spec: structured output schema
}

// ToolCallParams for tools/call request
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Meta      *ToolCallMeta          `json:"_meta,omitempty"`
}

// ToolCallMeta holds request metadata for tool calls
type ToolCallMeta struct {
	ProgressToken interface{} `json:"progressToken,omitempty"` // string or int per MCP spec
}

// ToolCallResult from tools/call response
type ToolCallResult struct {
	Content           []ContentBlock         `json:"content"`
	IsError           bool                   `json:"isError,omitempty"`
	StructuredContent map[string]interface{} `json:"structuredContent,omitempty"` // 2025-06-18 spec
}

// ContentBlock in tool results
type ContentBlock struct {
	Type        string                 `json:"type"` // "text", "image", "resource"
	Text        string                 `json:"text,omitempty"`
	Data        string                 `json:"data,omitempty"`     // base64 for images
	MimeType    string                 `json:"mimeType,omitempty"` // for images
	Resource    *ResourceReference     `json:"resource,omitempty"` // for embedded resources
	Annotations map[string]interface{} `json:"annotations,omitempty"`
}

// ResourceReference for embedded resources in content
type ResourceReference struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"` // base64
}

// ResourcesListResult from resources/list response
type ResourcesListResult struct {
	Resources  []MCPResourceDefinition `json:"resources"`
	NextCursor string                  `json:"nextCursor,omitempty"`
}

// MCPResourceDefinition represents a resource in standard MCP format
type MCPResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// PromptsListResult from prompts/list response
type PromptsListResult struct {
	Prompts    []MCPPromptDefinition `json:"prompts"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

// MCPPromptDefinition represents a prompt in standard MCP format
type MCPPromptDefinition struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument for prompt parameters
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// --- Notification types ---

// ProgressNotificationParams represents params for notifications/progress
type ProgressNotificationParams struct {
	ProgressToken interface{} `json:"progressToken"`     // matches the token sent in _meta
	Progress      float64     `json:"progress"`          // current progress value
	Total         float64     `json:"total,omitempty"`   // total units (if known)
	Message       string      `json:"message,omitempty"` // human-readable status
}

// LogNotificationParams represents params for notifications/message (server log)
type LogNotificationParams struct {
	Level  string      `json:"level"`            // "debug", "info", "warning", "error"
	Logger string      `json:"logger,omitempty"` // source logger name
	Data   interface{} `json:"data"`             // log content
}

// --- Notification callback types ---

// MCPNotification is a parsed server notification ready for consumption.
// This is the bridge type between the MCP transport layer and external streaming systems.
type MCPNotification struct {
	Type     string  // "progress", "log"
	Message  string  // human-readable message
	Progress float64 // 0.0-1.0 for progress notifications
	Total    float64 // total units if applicable
	Level    string  // for log notifications: "debug", "info", "warning", "error"
}

// MCPNotificationCallback is called when the server sends a notification during a tool call.
// The mcp package defines this type; the stream/processor package provides the implementation.
type MCPNotificationCallback func(notification MCPNotification)

// ParseNotification converts a raw JSONRPCMessage notification into an MCPNotification.
func ParseNotification(msg JSONRPCMessage) MCPNotification {
	switch msg.Method {
	case "notifications/progress":
		var params ProgressNotificationParams
		if msg.Params != nil {
			json.Unmarshal(msg.Params, &params)
		}
		progress := params.Progress
		if params.Total > 0 {
			progress = params.Progress / params.Total
		}
		return MCPNotification{
			Type:     "progress",
			Message:  params.Message,
			Progress: progress,
			Total:    params.Total,
		}
	case "notifications/message":
		var params LogNotificationParams
		if msg.Params != nil {
			json.Unmarshal(msg.Params, &params)
		}
		message := ""
		if s, ok := params.Data.(string); ok {
			message = s
		} else if params.Data != nil {
			raw, _ := json.Marshal(params.Data)
			message = string(raw)
		}
		if message == "" && params.Logger != "" {
			message = fmt.Sprintf("[%s]", params.Logger)
		}
		return MCPNotification{
			Type:    "log",
			Message: message,
			Level:   params.Level,
		}
	default:
		return MCPNotification{
			Type:    "log",
			Message: fmt.Sprintf("[%s]", msg.Method),
		}
	}
}
