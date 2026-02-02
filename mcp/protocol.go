package mcp

// Standard MCP Protocol types (JSON-RPC 2.0)
// Reference: https://modelcontextprotocol.io/specification/2025-03-26

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int              `json:"id"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *JSONRPCError    `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
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
	Name        string                 `json:"name"`
	Title       string                 `json:"title,omitempty"`       // Human-readable title (e.g., "Add email for auth user")
	DisplayName string                 `json:"displayName,omitempty"` // Alternative display name field
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolCallParams for tools/call request
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// ToolCallResult from tools/call response
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock in tool results
type ContentBlock struct {
	Type     string                 `json:"type"` // "text", "image", "resource"
	Text     string                 `json:"text,omitempty"`
	Data     string                 `json:"data,omitempty"`     // base64 for images
	MimeType string                 `json:"mimeType,omitempty"` // for images
	Resource *ResourceReference     `json:"resource,omitempty"` // for embedded resources
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
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Arguments   []PromptArgument      `json:"arguments,omitempty"`
}

// PromptArgument for prompt parameters
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Current MCP protocol version
const MCPProtocolVersion = "2025-03-26"
