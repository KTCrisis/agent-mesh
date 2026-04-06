package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/KTCrisis/agent-mesh/approval"
	"github.com/KTCrisis/agent-mesh/policy"
	"github.com/KTCrisis/agent-mesh/proxy"
	"github.com/KTCrisis/agent-mesh/registry"
	"github.com/KTCrisis/agent-mesh/trace"
)

// JSON-RPC 2.0 types

type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPTool is the MCP tool format.
type MCPTool struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	InputSchema MCPSchema `json:"inputSchema"`
}

// MCPSchema describes the input parameters of an MCP tool.
type MCPSchema struct {
	Type       string                `json:"type"`
	Properties map[string]MCPProp    `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
}

// MCPProp describes a single property in an MCP tool schema.
type MCPProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// Server runs the MCP stdio protocol.
type Server struct {
	Registry  *registry.Registry
	Policy    *policy.Engine
	Traces    *trace.Store
	Approvals *approval.Store
	Handler   *proxy.Handler
	AgentID   string // agent ID for policy evaluation in MCP mode
}

// Run starts the MCP server on stdin/stdout.
func (s *Server) Run() error {
	return s.Serve(os.Stdin, os.Stdout)
}

// Serve runs the MCP server on the given reader/writer.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	slog.Info("MCP server starting", "agent", s.AgentID, "tools", len(s.Registry.All()))

	reader := bufio.NewReader(r)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				slog.Info("MCP server: stdin closed")
				return nil
			}
			return fmt.Errorf("read stdin: %w", err)
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(w, nil, -32700, "Parse error")
			continue
		}

		slog.Debug("MCP request", "method", req.Method, "id", req.ID)

		var resp rpcResponse
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "initialize":
			resp.Result = s.handleInitialize()
		case "notifications/initialized":
			// Client ack — no response needed
			continue
		case "tools/list":
			resp.Result = s.handleToolsList()
		case "tools/call":
			resp.Result, resp.Error = s.handleToolsCall(req.Params)
		case "ping":
			resp.Result = map[string]any{}
		default:
			resp.Error = &rpcError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)}
		}

		s.writeResponse(w, resp)
	}
}

func (s *Server) handleInitialize() map[string]any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "agent-mesh",
			"version": "0.1.0",
		},
	}
}

func (s *Server) handleToolsList() map[string]any {
	tools := s.Registry.All()
	mcpTools := make([]MCPTool, 0, len(tools))

	for _, t := range tools {
		// Build input schema from tool params
		props := make(map[string]MCPProp)
		var required []string

		for _, p := range t.Params {
			propType := p.Type
			if propType == "" || propType == "integer" || propType == "number" {
				if propType == "" {
					propType = "string"
				}
			}
			props[p.Name] = MCPProp{
				Type:        propType,
				Description: fmt.Sprintf("%s parameter (%s)", p.Name, p.In),
			}
			if p.Required {
				required = append(required, p.Name)
			}
		}

		mcpTools = append(mcpTools, MCPTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: MCPSchema{
				Type:       "object",
				Properties: props,
				Required:   required,
			},
		})
	}

	// Append virtual approval tools (no policy evaluation)
	mcpTools = append(mcpTools, MCPTool{
		Name:        "approval.resolve",
		Description: "Approve or deny a pending approval request",
		InputSchema: MCPSchema{
			Type: "object",
			Properties: map[string]MCPProp{
				"id":       {Type: "string", Description: "Approval ID (full or 8-char prefix)"},
				"decision": {Type: "string", Description: "Decision: approve or deny"},
			},
			Required: []string{"id", "decision"},
		},
	}, MCPTool{
		Name:        "approval.pending",
		Description: "List all pending approval requests",
		InputSchema: MCPSchema{
			Type:       "object",
			Properties: map[string]MCPProp{},
		},
	})

	// Append virtual grant tools
	mcpTools = append(mcpTools, MCPTool{
		Name:        "grant.create",
		Description: "Create a temporal grant — temporarily allow a tool pattern without approval. Like sudo for agents.",
		InputSchema: MCPSchema{
			Type: "object",
			Properties: map[string]MCPProp{
				"tools":    {Type: "string", Description: "Tool glob pattern (e.g. filesystem.write_*, gmail.*)"},
				"duration": {Type: "string", Description: "Duration (e.g. 30m, 2h, 1h30m)"},
			},
			Required: []string{"tools", "duration"},
		},
	}, MCPTool{
		Name:        "grant.list",
		Description: "List all active temporal grants",
		InputSchema: MCPSchema{
			Type:       "object",
			Properties: map[string]MCPProp{},
		},
	}, MCPTool{
		Name:        "grant.revoke",
		Description: "Revoke an active temporal grant",
		InputSchema: MCPSchema{
			Type: "object",
			Properties: map[string]MCPProp{
				"id": {Type: "string", Description: "Grant ID (full or prefix)"},
			},
			Required: []string{"id"},
		},
	})

	return map[string]any{"tools": mcpTools}
}

func (s *Server) handleToolsCall(params map[string]any) (any, *rpcError) {
	toolName, _ := params["name"].(string)
	arguments, _ := params["arguments"].(map[string]any)

	if toolName == "" {
		return nil, &rpcError{Code: -32602, Message: "Missing tool name"}
	}

	// Virtual tools — handled before registry lookup, no policy evaluation
	switch toolName {
	case "approval.resolve":
		return s.handleApprovalResolve(arguments)
	case "approval.pending":
		return s.handleApprovalPending()
	case "grant.create":
		return s.handleGrantCreate(arguments)
	case "grant.list":
		return s.handleGrantList()
	case "grant.revoke":
		return s.handleGrantRevoke(arguments)
	}

	// Look up tool
	tool := s.Registry.Get(toolName)
	if tool == nil {
		return nil, &rpcError{Code: -32602, Message: fmt.Sprintf("Unknown tool: %s", toolName)}
	}

	// Evaluate policy
	decision := s.Policy.Evaluate(s.AgentID, toolName, arguments)
	slog.Info("MCP policy evaluated",
		"agent", s.AgentID, "tool", toolName,
		"action", decision.Action, "rule", decision.Rule,
	)

	if decision.Action == "deny" {
		s.Traces.Record(trace.Entry{
			AgentID:    s.AgentID,
			Tool:       toolName,
			Params:     arguments,
			Policy:     "deny",
			PolicyRule: decision.Rule,
		})
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("Policy denied: %s", decision.Reason)},
			},
		}, nil
	}

	if decision.Action == "human_approval" {
		entry := trace.Entry{
			AgentID:    s.AgentID,
			Tool:       toolName,
			Params:     arguments,
			Policy:     "human_approval",
			PolicyRule: decision.Rule,
		}

		// Try TTY prompt first (interactive terminal), fall back to approval store
		approved, resolvedBy := s.promptTTY(toolName, arguments)
		if approved != nil {
			s.Traces.Record(entry)
			if *approved {
				entry.ApprovalStatus = string(approval.StatusApproved)
				entry.ApprovedBy = resolvedBy
				s.Traces.Update(entry.TraceID, func(e *trace.Entry) {
					e.ApprovalStatus = string(approval.StatusApproved)
					e.ApprovedBy = resolvedBy
				})

				result, statusCode, err := s.Handler.Forward(tool, arguments)
				s.Traces.Update(entry.TraceID, func(e *trace.Entry) {
					e.StatusCode = statusCode
					if err != nil {
						e.Error = err.Error()
					}
				})
				if err != nil {
					return map[string]any{
						"content": []map[string]any{
							{"type": "text", "text": fmt.Sprintf("Backend error: %s", err.Error())},
						},
					}, nil
				}
				resultJSON, _ := json.MarshalIndent(result, "", "  ")
				return map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": string(resultJSON)},
					},
				}, nil
			}
			// Denied via TTY
			s.Traces.Update(entry.TraceID, func(e *trace.Entry) {
				e.ApprovalStatus = string(approval.StatusDenied)
				e.ApprovedBy = resolvedBy
			})
			return map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "Approval denied by " + resolvedBy},
				},
			}, nil
		}

		// No TTY available — block on approval store, resolve via HTTP API or mesh CLI
		if s.Approvals == nil {
			s.Traces.Record(entry)
			return map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "This action requires human approval but no approval store is configured."},
				},
			}, nil
		}

		pending := s.Approvals.Submit(s.AgentID, toolName, decision.Rule, arguments, "")
		entry.ApprovalID = pending.ID
		s.Traces.Record(entry)
		pending.TraceID = entry.TraceID

		shortID := pending.ID[:8]
		slog.Info("approval pending (non-blocking)",
			"approval_id", shortID, "agent", s.AgentID, "tool", toolName,
			"resolve_via", fmt.Sprintf("approval.resolve {id: %s, decision: approve} OR mesh approve %s", shortID, shortID))

		// Non-blocking: return immediately, let the caller resolve via approval.resolve tool
		remaining := pending.Remaining(s.Approvals.Timeout())
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf(
					"Approval required (id: %s). Tool: %s. Timeout: %ds.\n"+
						"Use approval.resolve with id=%s and decision=approve or deny.",
					shortID, toolName, int(remaining.Seconds()), shortID)},
			},
		}, nil
	}

	// Forward to backend
	result, statusCode, err := s.Handler.Forward(tool, arguments)

	// Trace
	entry := trace.Entry{
		AgentID:    s.AgentID,
		Tool:       toolName,
		Params:     arguments,
		Policy:     "allow",
		PolicyRule: decision.Rule,
		StatusCode: statusCode,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	s.Traces.Record(entry)

	if err != nil {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("Backend error: %s", err.Error())},
			},
		}, nil
	}

	// Serialize result as text
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("Failed to serialize result: %s", err.Error())},
			},
		}, nil
	}
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(resultJSON)},
		},
	}, nil
}

func (s *Server) handleApprovalResolve(args map[string]any) (any, *rpcError) {
	id, _ := args["id"].(string)
	decision, _ := args["decision"].(string)

	if id == "" {
		return nil, &rpcError{Code: -32602, Message: "Missing 'id' parameter"}
	}
	if decision != "approve" && decision != "deny" {
		return nil, &rpcError{Code: -32602, Message: "Invalid 'decision': must be 'approve' or 'deny'"}
	}

	if s.Approvals == nil {
		return nil, &rpcError{Code: -32000, Message: "No approval store configured"}
	}

	pa := s.Approvals.Get(id)
	if pa == nil {
		return nil, &rpcError{Code: -32602, Message: fmt.Sprintf("Approval not found: %s", id)}
	}

	resolvedBy := "mcp:" + s.AgentID

	if decision == "deny" {
		if err := s.Approvals.Deny(id, resolvedBy); err != nil {
			return map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": fmt.Sprintf("Cannot resolve: %s", err.Error())},
				},
			}, nil
		}
		if pa.TraceID != "" {
			s.Traces.Update(pa.TraceID, func(e *trace.Entry) {
				e.ApprovalStatus = string(approval.StatusDenied)
				e.ApprovedBy = resolvedBy
				e.ApprovalMs = time.Since(pa.CreatedAt).Milliseconds()
			})
		}
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("Denied. Tool %s was not executed.", pa.Tool)},
			},
		}, nil
	}

	// Approve: resolve then replay the original tool call
	if err := s.Approvals.Approve(id, resolvedBy); err != nil {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("Cannot resolve: %s", err.Error())},
			},
		}, nil
	}

	tool := s.Registry.Get(pa.Tool)
	if tool == nil {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("Approved but tool %s no longer exists in registry.", pa.Tool)},
			},
		}, nil
	}

	result, statusCode, err := s.Handler.Forward(tool, pa.Params)
	if pa.TraceID != "" {
		s.Traces.Update(pa.TraceID, func(e *trace.Entry) {
			e.ApprovalStatus = string(approval.StatusApproved)
			e.ApprovedBy = resolvedBy
			e.ApprovalMs = time.Since(pa.CreatedAt).Milliseconds()
			e.StatusCode = statusCode
			if err != nil {
				e.Error = err.Error()
			}
		})
	}

	if err != nil {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("Approved but backend error: %s", err.Error())},
			},
		}, nil
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(resultJSON)},
		},
	}, nil
}

func (s *Server) handleApprovalPending() (any, *rpcError) {
	if s.Approvals == nil {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "No approval store configured."},
			},
		}, nil
	}

	pending := s.Approvals.ListPending()
	if len(pending) == 0 {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "No pending approvals."},
			},
		}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Pending approvals (%d):\n", len(pending))
	for _, pa := range pending {
		age := time.Since(pa.CreatedAt).Truncate(time.Second)
		fmt.Fprintf(&sb, "- ID: %s  tool: %s  agent: %s  age: %s\n", pa.ID[:8], pa.Tool, pa.AgentID, age)
	}
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": sb.String()},
		},
	}, nil
}

func (s *Server) handleGrantCreate(args map[string]any) (any, *rpcError) {
	if s.Handler == nil || s.Handler.Grants == nil {
		return nil, &rpcError{Code: -32603, Message: "Grant store not configured"}
	}
	tools, _ := args["tools"].(string)
	duration, _ := args["duration"].(string)
	if tools == "" || duration == "" {
		return nil, &rpcError{Code: -32602, Message: "tools and duration are required"}
	}
	dur, err := time.ParseDuration(duration)
	if err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid duration: " + err.Error()}
	}
	g := s.Handler.Grants.Add(s.AgentID, tools, "mcp:"+s.AgentID, dur)
	slog.Info("grant created via MCP",
		"id", g.ID, "agent", g.Agent, "tools", g.Tools, "duration", duration)
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": fmt.Sprintf("Grant created: %s\n  agent: %s\n  tools: %s\n  expires: %s\n  remaining: %s",
				g.ID, g.Agent, g.Tools, g.ExpiresAt.Format(time.RFC3339), g.Remaining().Truncate(time.Second))},
		},
	}, nil
}

func (s *Server) handleGrantList() (any, *rpcError) {
	if s.Handler == nil || s.Handler.Grants == nil {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "No grant store configured."},
			},
		}, nil
	}
	grants := s.Handler.Grants.List()
	if len(grants) == 0 {
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "No active grants."},
			},
		}, nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Active grants (%d):\n", len(grants))
	for _, g := range grants {
		fmt.Fprintf(&sb, "- ID: %s  tools: %s  agent: %s  remaining: %s\n",
			g.ID[:8], g.Tools, g.Agent, g.Remaining().Truncate(time.Second))
	}
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": sb.String()},
		},
	}, nil
}

func (s *Server) handleGrantRevoke(args map[string]any) (any, *rpcError) {
	if s.Handler == nil || s.Handler.Grants == nil {
		return nil, &rpcError{Code: -32603, Message: "Grant store not configured"}
	}
	id, _ := args["id"].(string)
	if id == "" {
		return nil, &rpcError{Code: -32602, Message: "id is required"}
	}
	if !s.Handler.Grants.Revoke(id) {
		return nil, &rpcError{Code: -32602, Message: "grant not found: " + id}
	}
	slog.Info("grant revoked via MCP", "id", id)
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Grant revoked: " + id},
		},
	}, nil
}

func (s *Server) writeResponse(w io.Writer, resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		slog.Error("MCP server: failed to marshal response", "error", err)
		return
	}
	if _, err := fmt.Fprintf(w, "%s\n", data); err != nil {
		slog.Error("MCP server: failed to write response", "error", err)
	}
}

// promptTTY tries to prompt the user directly via /dev/tty.
// Returns (approved *bool, resolvedBy string). If TTY is unavailable, returns (nil, "").
func (s *Server) promptTTY(toolName string, params map[string]any) (*bool, string) {
	if runtime.GOOS == "windows" {
		return nil, ""
	}

	// Skip TTY prompt when stdin is a pipe (MCP stdio mode via Claude Code / agent).
	// The TTY would open the agent-mesh terminal, not the caller's terminal,
	// causing an invisible blocking prompt.
	if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		slog.Debug("stdin is a pipe, skipping TTY prompt — use HTTP API or mesh CLI to approve")
		return nil, ""
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		slog.Debug("TTY not available, falling back to approval store", "error", err)
		return nil, ""
	}
	defer tty.Close()

	// Display approval prompt
	fmt.Fprintf(tty, "\n\033[1;33m>> APPROVAL REQUIRED\033[0m\n")
	fmt.Fprintf(tty, "   agent: %s\n", s.AgentID)
	fmt.Fprintf(tty, "   tool:  %s\n", toolName)
	for k, v := range params {
		str := fmt.Sprintf("%v", v)
		if len(str) > 80 {
			str = str[:80] + "..."
		}
		fmt.Fprintf(tty, "   %s: %s\n", k, str)
	}
	fmt.Fprintf(tty, "\n   \033[1m[a]pprove / [d]eny ?\033[0m ")

	// Read response
	reader := bufio.NewReader(tty)
	line, _ := reader.ReadString('\n')
	input := strings.TrimSpace(strings.ToLower(line))

	resolvedBy := "tty:" + os.Getenv("USER")
	switch input {
	case "a", "approve":
		fmt.Fprintf(tty, "   \033[32mApproved\033[0m\n\n")
		approved := true
		return &approved, resolvedBy
	default:
		fmt.Fprintf(tty, "   \033[31mDenied\033[0m\n\n")
		approved := false
		return &approved, resolvedBy
	}
}

func (s *Server) writeError(w io.Writer, id any, code int, msg string) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
	s.writeResponse(w, resp)
}
