package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

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
	Registry *registry.Registry
	Policy   *policy.Engine
	Traces   *trace.Store
	Handler  *proxy.Handler
	AgentID  string // agent ID for policy evaluation in MCP mode
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

	return map[string]any{"tools": mcpTools}
}

func (s *Server) handleToolsCall(params map[string]any) (any, *rpcError) {
	toolName, _ := params["name"].(string)
	arguments, _ := params["arguments"].(map[string]any)

	if toolName == "" {
		return nil, &rpcError{Code: -32602, Message: "Missing tool name"}
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
		s.Traces.Record(trace.Entry{
			AgentID:    s.AgentID,
			Tool:       toolName,
			Params:     arguments,
			Policy:     "human_approval",
			PolicyRule: decision.Rule,
		})
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "This action requires human approval. Please confirm in the dashboard."},
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

func (s *Server) writeError(w io.Writer, id any, code int, msg string) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
	s.writeResponse(w, resp)
}
