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

	"github.com/KTCrisis/agent-mesh/approval"
	"github.com/KTCrisis/agent-mesh/policy"
	"github.com/KTCrisis/agent-mesh/ratelimit"
	"github.com/KTCrisis/agent-mesh/registry"
	"github.com/KTCrisis/agent-mesh/trace"
)

// ToolCallRequest is the JSON body sent by the agent.
type ToolCallRequest struct {
	Params map[string]any `json:"params"`
}

// ToolCallResponse is returned to the agent.
type ToolCallResponse struct {
	Result     any    `json:"result,omitempty"`
	TraceID    string `json:"trace_id"`
	ApprovalID string `json:"approval_id,omitempty"`
	Policy     string `json:"policy"`
	LatencyMs  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

// MCPForwarder is the interface for forwarding calls to upstream MCP servers.
type MCPForwarder interface {
	CallTool(ctx context.Context, serverName string, toolName string, arguments map[string]any) (any, error)
	ServerStatuses() any
}

// Handler is the HTTP handler for the sidecar proxy.
type Handler struct {
	Registry     *registry.Registry
	Policy       *policy.Engine
	Traces       *trace.Store
	Approvals    *approval.Store
	RateLimiter  *ratelimit.Limiter
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
	case r.Method == "GET" && r.URL.Path == "/approvals":
		h.handleListApprovals(w, r)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/approvals/") && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/approvals/"), "/"):
		h.handleGetApproval(w, r)
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/approve") && strings.HasPrefix(r.URL.Path, "/approvals/"):
		h.handleApproveAction(w, r)
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/deny") && strings.HasPrefix(r.URL.Path, "/approvals/"):
		h.handleDenyAction(w, r)
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

	// 3. Rate limit check (before policy — fail fast)
	if h.RateLimiter != nil {
		paramsKey := fmt.Sprintf("%v", req.Params)
		// Pre-check with a preliminary policy match to get the policy name
		preDecision := h.Policy.Evaluate(agentID, toolName, req.Params)
		if err := h.RateLimiter.Check(agentID, preDecision.Rule, toolName, paramsKey); err != nil {
			entry := trace.Entry{
				AgentID:    agentID,
				Tool:       toolName,
				Params:     req.Params,
				Policy:     "rate_limited",
				PolicyRule: preDecision.Rule,
				LatencyMs:  time.Since(start).Milliseconds(),
				Error:      err.Error(),
			}
			h.Traces.Record(entry)
			writeJSON(w, 429, ToolCallResponse{
				TraceID: entry.TraceID,
				Policy:  "rate_limited",
				Error:   err.Error(),
			})
			return
		}
	}

	// 4. Evaluate policy
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
		if h.Approvals == nil {
			// Fallback: no approval store configured
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

		pending := h.Approvals.Submit(agentID, toolName, decision.Rule, req.Params)

		entry := trace.Entry{
			AgentID:    agentID,
			Tool:       toolName,
			Params:     req.Params,
			Policy:     "human_approval",
			PolicyRule: decision.Rule,
			ApprovalID: pending.ID,
		}
		h.Traces.Record(entry)

		slog.Info("awaiting human approval",
			"approval_id", pending.ID, "agent", agentID, "tool", toolName)

		// Block until resolved
		resolution := <-pending.Result
		approvalMs := time.Since(start).Milliseconds()

		h.Traces.Update(entry.TraceID, func(e *trace.Entry) {
			e.ApprovalStatus = string(resolution.Status)
			e.ApprovedBy = resolution.ResolvedBy
			e.ApprovalMs = approvalMs
		})

		switch resolution.Status {
		case approval.StatusApproved:
			if h.RateLimiter != nil {
				h.RateLimiter.Record(agentID, toolName, fmt.Sprintf("%v", req.Params))
			}
			result, statusCode, err := h.Forward(tool, req.Params)
			totalMs := time.Since(start).Milliseconds()
			h.Traces.Update(entry.TraceID, func(e *trace.Entry) {
				e.StatusCode = statusCode
				e.LatencyMs = totalMs
				if err != nil {
					e.Error = err.Error()
				}
			})
			resp := ToolCallResponse{
				Result:     result,
				TraceID:    entry.TraceID,
				ApprovalID: pending.ID,
				Policy:     "human_approval",
				LatencyMs:  totalMs,
			}
			if err != nil {
				resp.Error = err.Error()
				writeJSON(w, 502, resp)
				return
			}
			writeJSON(w, 200, resp)

		case approval.StatusDenied:
			writeJSON(w, 403, ToolCallResponse{
				TraceID:    entry.TraceID,
				ApprovalID: pending.ID,
				Policy:     "human_approval",
				LatencyMs:  approvalMs,
				Error:      "approval denied by " + resolution.ResolvedBy,
			})

		case approval.StatusTimeout:
			writeJSON(w, 408, ToolCallResponse{
				TraceID:    entry.TraceID,
				ApprovalID: pending.ID,
				Policy:     "human_approval",
				LatencyMs:  approvalMs,
				Error:      "approval timed out",
			})
		}
		return
	}

	// 5. Record rate limit usage
	if h.RateLimiter != nil {
		h.RateLimiter.Record(agentID, toolName, fmt.Sprintf("%v", req.Params))
	}

	// 6. Forward to backend
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

// --- Approval endpoints ---

type approvalView struct {
	ID         string         `json:"id"`
	AgentID    string         `json:"agent_id"`
	Tool       string         `json:"tool"`
	Params     map[string]any `json:"params"`
	PolicyRule string         `json:"policy_rule"`
	Status     string         `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	Remaining  string         `json:"remaining,omitempty"`
	ResolvedBy string         `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty"`
}

func (h *Handler) toApprovalView(pa *approval.PendingApproval) approvalView {
	v := approvalView{
		ID:         pa.ID,
		AgentID:    pa.AgentID,
		Tool:       pa.Tool,
		Params:     pa.Params,
		PolicyRule: pa.PolicyRule,
		Status:     string(pa.Status),
		CreatedAt:  pa.CreatedAt,
		ResolvedBy: pa.ResolvedBy,
	}
	if pa.Status == approval.StatusPending && h.Approvals != nil {
		v.Remaining = pa.Remaining(h.Approvals.Timeout()).Truncate(time.Second).String()
	}
	if !pa.ResolvedAt.IsZero() {
		v.ResolvedAt = &pa.ResolvedAt
	}
	return v
}

func (h *Handler) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	if h.Approvals == nil {
		writeJSON(w, 200, []any{})
		return
	}
	status := r.URL.Query().Get("status")
	var list []*approval.PendingApproval
	if status == "pending" {
		list = h.Approvals.ListPending()
	} else {
		list = h.Approvals.List()
	}
	views := make([]approvalView, len(list))
	for i, pa := range list {
		views[i] = h.toApprovalView(pa)
	}
	writeJSON(w, 200, views)
}

func (h *Handler) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/approvals/")
	if h.Approvals == nil {
		writeJSON(w, 404, map[string]string{"error": "approval system not configured"})
		return
	}
	pa := h.Approvals.Get(id)
	if pa == nil {
		writeJSON(w, 404, map[string]string{"error": "approval not found"})
		return
	}
	writeJSON(w, 200, h.toApprovalView(pa))
}

type resolveRequest struct {
	ResolvedBy string `json:"resolved_by"`
}

func (h *Handler) handleApproveAction(w http.ResponseWriter, r *http.Request) {
	h.handleResolveAction(w, r, approval.StatusApproved)
}

func (h *Handler) handleDenyAction(w http.ResponseWriter, r *http.Request) {
	h.handleResolveAction(w, r, approval.StatusDenied)
}

func (h *Handler) handleResolveAction(w http.ResponseWriter, r *http.Request, status approval.Status) {
	// Extract ID from /approvals/{id}/approve or /approvals/{id}/deny
	path := strings.TrimPrefix(r.URL.Path, "/approvals/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]

	if h.Approvals == nil {
		writeJSON(w, 404, map[string]string{"error": "approval system not configured"})
		return
	}

	var req resolveRequest
	json.NewDecoder(r.Body).Decode(&req) // ignore error — body is optional
	if req.ResolvedBy == "" {
		req.ResolvedBy = "http:" + r.RemoteAddr
	}

	err := h.Approvals.Resolve(id, status, req.ResolvedBy)
	if err == approval.ErrNotFound {
		writeJSON(w, 404, map[string]string{"error": "approval not found"})
		return
	}
	if err == approval.ErrAlreadyResolved {
		writeJSON(w, 409, map[string]string{"error": "approval already resolved"})
		return
	}

	writeJSON(w, 200, map[string]string{"status": string(status), "id": id})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
