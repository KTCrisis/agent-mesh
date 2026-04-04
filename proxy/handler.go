package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KTCrisis/agent-mesh/policy"
	"github.com/KTCrisis/agent-mesh/registry"
	"github.com/KTCrisis/agent-mesh/trace"
)

// ToolCallRequest is the JSON body sent by the agent.
type ToolCallRequest struct {
	Params map[string]any `json:"params"`
}

// ToolCallResponse is returned to the agent.
type ToolCallResponse struct {
	Result    any    `json:"result,omitempty"`
	TraceID   string `json:"trace_id"`
	Policy    string `json:"policy"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// MCPForwarder is the interface for forwarding calls to upstream MCP servers.
type MCPForwarder interface {
	CallTool(ctx context.Context, serverName string, toolName string, arguments map[string]any) (any, error)
	ServerStatuses() any
}

// Handler is the HTTP handler for the sidecar proxy.
type Handler struct {
	Registry   *registry.Registry
	Policy     *policy.Engine
	Traces     *trace.Store
	Client       *http.Client
	MCPForwarder MCPForwarder
}

func NewHandler(reg *registry.Registry, pol *policy.Engine, traces *trace.Store) *Handler {
	return &Handler{
		Registry: reg,
		Policy:   pol,
		Traces:   traces,
		Client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// ServeHTTP routes requests to the appropriate handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/tool/"):
		h.handleToolCall(w, r)
	case r.Method == "GET" && r.URL.Path == "/tools":
		h.handleListTools(w, r)
	case r.Method == "GET" && r.URL.Path == "/traces":
		h.handleTraces(w, r)
	case r.Method == "GET" && r.URL.Path == "/mcp-servers":
		h.handleMCPServers(w, r)
	case r.Method == "GET" && r.URL.Path == "/health":
		h.handleHealth(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleToolCall(w http.ResponseWriter, r *http.Request) {
	toolName := strings.TrimPrefix(r.URL.Path, "/tool/")
	agentID := extractAgentID(r)
	start := time.Now()

	// 1. Parse request body
	var req ToolCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, ToolCallResponse{Error: "invalid JSON body", Policy: "error"})
		return
	}

	// 2. Look up tool in registry
	tool := h.Registry.Get(toolName)
	if tool == nil {
		writeJSON(w, 404, ToolCallResponse{Error: fmt.Sprintf("unknown tool: %s", toolName), Policy: "error"})
		return
	}

	// 3. Evaluate policy
	decision := h.Policy.Evaluate(agentID, toolName, req.Params)
	slog.Info("policy evaluated",
		"agent", agentID, "tool", toolName,
		"action", decision.Action, "rule", decision.Rule,
	)

	if decision.Action == "deny" {
		entry := trace.Entry{
			AgentID:    agentID,
			Tool:       toolName,
			Params:     req.Params,
			Policy:     "deny",
			PolicyRule: decision.Rule,
			LatencyMs:  time.Since(start).Milliseconds(),
		}
		h.Traces.Record(entry)
		writeJSON(w, 403, ToolCallResponse{
			TraceID: entry.TraceID,
			Policy:  "deny",
			Error:   decision.Reason,
		})
		return
	}

	if decision.Action == "human_approval" {
		entry := trace.Entry{
			AgentID:    agentID,
			Tool:       toolName,
			Params:     req.Params,
			Policy:     "human_approval",
			PolicyRule: decision.Rule,
			LatencyMs:  time.Since(start).Milliseconds(),
		}
		h.Traces.Record(entry)
		writeJSON(w, 202, ToolCallResponse{
			TraceID: entry.TraceID,
			Policy:  "human_approval",
			Error:   "action requires human approval",
		})
		return
	}

	// 4. Forward to backend
	result, statusCode, err := h.Forward(tool, req.Params)
	latency := time.Since(start).Milliseconds()

	// 5. Trace
	entry := trace.Entry{
		AgentID:    agentID,
		Tool:       toolName,
		Params:     req.Params,
		Policy:     "allow",
		PolicyRule: decision.Rule,
		StatusCode: statusCode,
		LatencyMs:  latency,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	h.Traces.Record(entry)

	// 6. Respond
	resp := ToolCallResponse{
		Result:    result,
		TraceID:   entry.TraceID,
		Policy:    "allow",
		LatencyMs: latency,
	}
	if err != nil {
		resp.Error = err.Error()
		writeJSON(w, 502, resp)
		return
	}
	writeJSON(w, 200, resp)
}

// Forward sends the request to the appropriate backend (HTTP or MCP).
func (h *Handler) Forward(tool *registry.Tool, params map[string]any) (any, int, error) {
	if tool.Source == "mcp" {
		return h.forwardMCP(tool, params)
	}
	return h.forwardHTTP(tool, params)
}

// forwardHTTP sends the request to a REST backend.
func (h *Handler) forwardHTTP(tool *registry.Tool, params map[string]any) (any, int, error) {
	// Build URL with path params (URL-encoded)
	reqURL := tool.BaseURL + tool.Path
	for k, v := range params {
		placeholder := "{" + k + "}"
		if strings.Contains(reqURL, placeholder) {
			reqURL = strings.Replace(reqURL, placeholder, url.PathEscape(fmt.Sprintf("%v", v)), 1)
		}
	}

	// Build query params for GET/DELETE (URL-encoded)
	var body io.Reader
	if tool.Method == "GET" || tool.Method == "DELETE" {
		q := url.Values{}
		for k, v := range params {
			if !strings.Contains(tool.Path, "{"+k+"}") {
				q.Set(k, fmt.Sprintf("%v", v))
			}
		}
		if encoded := q.Encode(); encoded != "" {
			sep := "?"
			if strings.Contains(reqURL, "?") {
				sep = "&"
			}
			reqURL += sep + encoded
		}
	} else {
		// POST/PUT/PATCH: send params as JSON body
		jsonBody, err := json.Marshal(params)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal params: %w", err)
		}
		body = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(tool.Method, reqURL, body)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range tool.Headers {
		req.Header.Set(k, v)
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("backend error: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	var result any
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Non-JSON response — return as string
		result = string(respBody)
	}

	return result, resp.StatusCode, nil
}

// forwardMCP forwards the call to an upstream MCP server.
func (h *Handler) forwardMCP(tool *registry.Tool, params map[string]any) (any, int, error) {
	if h.MCPForwarder == nil {
		return nil, 0, fmt.Errorf("no MCP forwarder configured")
	}

	// Strip namespace prefix to get the original tool name
	originalName := strings.TrimPrefix(tool.Name, tool.MCPServer+".")

	ctx := context.Background()
	result, err := h.MCPForwarder.CallTool(ctx, tool.MCPServer, originalName, params)
	if err != nil {
		return nil, 502, err
	}
	return result, 200, nil
}

func (h *Handler) handleListTools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, h.Registry.All())
}

func (h *Handler) handleTraces(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	tool := r.URL.Query().Get("tool")
	writeJSON(w, 200, h.Traces.Query(agent, tool, 100))
}

func (h *Handler) handleMCPServers(w http.ResponseWriter, _ *http.Request) {
	if h.MCPForwarder == nil {
		writeJSON(w, 200, []any{})
		return
	}
	writeJSON(w, 200, h.MCPForwarder.ServerStatuses())
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"status": "ok",
		"tools":  len(h.Registry.All()),
		"traces": h.Traces.Stats(),
	})
}

// extractAgentID reads the agent ID from the Authorization header.
// Format: "Bearer agent:<agent-id>" or just "Bearer <agent-id>"
func extractAgentID(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	auth = strings.TrimPrefix(auth, "Bearer ")
	auth = strings.TrimPrefix(auth, "agent:")
	if auth == "" {
		return "anonymous"
	}
	return auth
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
