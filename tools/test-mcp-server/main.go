// Simple test MCP server using Streamable HTTP transport
// Deploy this to test the external MCP client implementation
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// JSON-RPC types
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

var sessionCounter int64

func main() {
	http.HandleFunc("/mcp", handleMCP)
	http.HandleFunc("/health", handleHealth)

	port := "8099"
	log.Printf("🔌 Test MCP Server starting on :%s", port)
	log.Printf("   Endpoint: http://localhost:%s/mcp", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, nil, -32700, "Parse error")
		return
	}

	log.Printf("📨 Request: %s (id=%v)", req.Method, req.ID)

	switch req.Method {
	case "initialize":
		handleInitialize(w, req)
	case "notifications/initialized":
		// No response needed for notifications
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		handleToolsList(w, req)
	case "tools/call":
		handleToolsCall(w, req)
	default:
		sendError(w, req.ID, -32601, "Method not found: "+req.Method)
	}
}

func handleInitialize(w http.ResponseWriter, req JSONRPCRequest) {
	sessionID := fmt.Sprintf("session-%d", atomic.AddInt64(&sessionCounter, 1))
	w.Header().Set("Mcp-Session-Id", sessionID)

	sendResult(w, req.ID, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "test-mcp-server",
			"version": "1.0.0",
		},
	})
}

func handleToolsList(w http.ResponseWriter, req JSONRPCRequest) {
	tools := []map[string]interface{}{
		{
			"name":        "echo",
			"description": "Echoes back the input message",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message": map[string]interface{}{
						"type":        "string",
						"description": "The message to echo back",
					},
				},
				"required": []string{"message"},
			},
		},
		{
			"name":        "get_time",
			"description": "Returns the current server time",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "add",
			"description": "Adds two numbers together",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":        "number",
						"description": "First number",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "Second number",
					},
				},
				"required": []string{"a", "b"},
			},
		},
	}

	sendResult(w, req.ID, map[string]interface{}{
		"tools": tools,
	})
}

func handleToolsCall(w http.ResponseWriter, req JSONRPCRequest) {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		sendError(w, req.ID, -32602, "Invalid params")
		return
	}

	toolName, _ := params["name"].(string)
	arguments, _ := params["arguments"].(map[string]interface{})

	var result string
	var isError bool

	switch toolName {
	case "echo":
		message, _ := arguments["message"].(string)
		result = fmt.Sprintf("Echo: %s", message)

	case "get_time":
		result = fmt.Sprintf("Current time: %s", time.Now().Format(time.RFC3339))

	case "add":
		a, aOk := arguments["a"].(float64)
		b, bOk := arguments["b"].(float64)
		if !aOk || !bOk {
			result = "Error: Both 'a' and 'b' must be numbers"
			isError = true
		} else {
			result = fmt.Sprintf("Result: %.2f + %.2f = %.2f", a, b, a+b)
		}

	default:
		result = fmt.Sprintf("Error: Unknown tool '%s'", toolName)
		isError = true
	}

	sendResult(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": result,
			},
		},
		"isError": isError,
	})
}

func sendResult(w http.ResponseWriter, id interface{}, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func sendError(w http.ResponseWriter, id interface{}, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	})
}
