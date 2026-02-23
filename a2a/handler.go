package a2a

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/apteva/agent/config"
	"github.com/apteva/agent/events"
)

const (
	// A2AProtocolVersion is the A2A protocol version we implement.
	A2AProtocolVersion = "0.3"
)

// Handler serves A2A protocol endpoints.
type Handler struct {
	config   *config.Config
	db       *sql.DB
	eventBus *events.EventBus
	port     string // localhost port for internal /chat calls
}

// NewHandler creates a new A2A protocol handler.
func NewHandler(cfg *config.Config, db *sql.DB, eventBus *events.EventBus, port string) *Handler {
	return &Handler{
		config:   cfg,
		db:       db,
		eventBus: eventBus,
		port:     port,
	}
}

// HandleAgentCard serves the A2A Agent Card at /.well-known/agent.json
func (h *Handler) HandleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agentCfg := h.config.Get()
	baseURL := agentCfg.PublicURL
	if baseURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, r.Host)
	}

	card := BuildAgentCard(&agentCfg, baseURL)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300") // 5 min cache
	json.NewEncoder(w).Encode(card)
}

// HandleJSONRPC is the main A2A endpoint handling JSON-RPC 2.0 requests.
func (h *Handler) HandleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and echo A2A-Version header
	a2aVersion := r.Header.Get("A2A-Version")
	if a2aVersion == "" {
		a2aVersion = A2AProtocolVersion
	}
	w.Header().Set("A2A-Version", a2aVersion)

	// Parse JSON-RPC request
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, ErrCodeParseError, "Parse error: "+err.Error())
		return
	}

	if req.JSONRPC != "2.0" {
		writeRPCError(w, req.ID, ErrCodeInvalidRequest, "Invalid request: jsonrpc must be \"2.0\"")
		return
	}

	log.Printf("📨 A2A request: method=%s id=%v", req.Method, req.ID)

	// Publish event
	if h.eventBus != nil {
		event := events.NewEvent(events.CategoryAgent, "a2a_request", events.LevelInfo).
			WithData("method", req.Method)
		h.eventBus.Publish(event)
	}

	// Dispatch by method
	switch req.Method {
	case "message/send":
		h.handleMessageSend(w, r, req)
	case "message/stream":
		h.handleMessageStream(w, r, req)
	case "tasks/get":
		h.handleTasksGet(w, r, req)
	case "tasks/cancel":
		h.handleTasksCancel(w, r, req)
	case "tasks/pushNotificationConfig/create":
		h.handlePushConfigCreate(w, r, req)
	case "tasks/pushNotificationConfig/get":
		h.handlePushConfigGet(w, r, req)
	case "tasks/pushNotificationConfig/list":
		h.handlePushConfigList(w, r, req)
	case "tasks/pushNotificationConfig/delete":
		h.handlePushConfigDelete(w, r, req)
	default:
		writeRPCError(w, req.ID, ErrCodeMethodNotFound, "Method not found: "+req.Method)
	}
}

// writeRPCResponse writes a successful JSON-RPC 2.0 response.
func writeRPCResponse(w http.ResponseWriter, id interface{}, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

// writeRPCError writes a JSON-RPC 2.0 error response.
func writeRPCError(w http.ResponseWriter, id interface{}, code int, message string) {
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

// writeSSEEvent writes a single SSE event.
func writeSSEEvent(w http.ResponseWriter, data interface{}) {
	jsonData, err := json.Marshal(JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  data,
	})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// Push notification stub handlers (will be implemented in push.go)

func (h *Handler) handlePushConfigCreate(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	writeRPCError(w, req.ID, ErrCodeInternalError, "push notifications not yet implemented")
}

func (h *Handler) handlePushConfigGet(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	writeRPCError(w, req.ID, ErrCodeInternalError, "push notifications not yet implemented")
}

func (h *Handler) handlePushConfigList(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	writeRPCError(w, req.ID, ErrCodeInternalError, "push notifications not yet implemented")
}

func (h *Handler) handlePushConfigDelete(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	writeRPCError(w, req.ID, ErrCodeInternalError, "push notifications not yet implemented")
}
