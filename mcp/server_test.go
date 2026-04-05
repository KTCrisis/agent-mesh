package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/KTCrisis/agent-mesh/approval"
	"github.com/KTCrisis/agent-mesh/config"
	"github.com/KTCrisis/agent-mesh/policy"
	"github.com/KTCrisis/agent-mesh/proxy"
	"github.com/KTCrisis/agent-mesh/registry"
	"github.com/KTCrisis/agent-mesh/trace"
)

func testServer() *Server {
	reg := registry.New()
	reg.LoadManual(&registry.Tool{
		Name: "get_order", Description: "Get an order", Source: "openapi",
		Params: []registry.Param{{Name: "order_id", In: "path", Type: "string", Required: true}},
	})
	reg.LoadMCP("fs", []registry.MCPToolDef{
		{Name: "read_file", Description: "Read a file", Params: []registry.Param{{Name: "path", In: "body", Type: "string", Required: true}}},
	})

	pol := policy.NewEngine([]config.Policy{
		{Name: "allow-reads", Agent: "claude", Rules: []config.Rule{
			{Tools: []string{"get_order", "fs.read_file"}, Action: "allow"},
		}},
		{Name: "default", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"*"}, Action: "deny"},
		}},
	})

	traces := trace.NewStore(100)
	handler := proxy.NewHandler(reg, pol, traces)

	return &Server{
		Registry: reg,
		Policy:   pol,
		Traces:   traces,
		Handler:  handler,
		AgentID:  "claude",
	}
}

func sendRPC(t *testing.T, s *Server, requests ...rpcRequest) []rpcResponse {
	t.Helper()

	var input bytes.Buffer
	for _, req := range requests {
		data, _ := json.Marshal(req)
		input.Write(data)
		input.WriteByte('\n')
	}

	var output bytes.Buffer
	err := s.Serve(&input, &output)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	var responses []rpcResponse
	decoder := json.NewDecoder(&output)
	for {
		var resp rpcResponse
		if err := decoder.Decode(&resp); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode response: %v", err)
		}
		responses = append(responses, resp)
	}
	return responses
}

func TestServerInitialize(t *testing.T) {
	s := testServer()
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "initialize",
		Params: map[string]any{"protocolVersion": "2024-11-05"},
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", resp.Result)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "agent-mesh" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}
}

func TestServerToolsList(t *testing.T) {
	s := testServer()
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/list",
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}

	result, _ := responses[0].Result.(map[string]any)
	tools, _ := result["tools"].([]any)
	// 2 registry tools + 2 virtual tools (approval.resolve, approval.pending)
	if len(tools) != 4 {
		t.Errorf("tools = %d, want 4", len(tools))
	}
}

func TestServerToolsCallDeny(t *testing.T) {
	s := testServer()
	// Agent is "claude" but calling a tool not in allow list → deny via default
	s.AgentID = "anonymous"
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{
			"name":      "get_order",
			"arguments": map[string]any{"order_id": "123"},
		},
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}

	// Denied calls return result with "Policy denied" text, not an RPC error
	result, _ := responses[0].Result.(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content in response")
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	if !strings.Contains(text, "Policy denied") {
		t.Errorf("expected 'Policy denied', got: %s", text)
	}
}

func TestServerToolsCallUnknown(t *testing.T) {
	s := testServer()
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{
			"name": "nonexistent",
		},
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("expected RPC error for unknown tool")
	}
	if responses[0].Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", responses[0].Error.Code)
	}
}

func TestServerPing(t *testing.T) {
	s := testServer()
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "ping",
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("error: %v", responses[0].Error)
	}
}

func TestServerUnknownMethod(t *testing.T) {
	s := testServer()
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "nonexistent/method",
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if responses[0].Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", responses[0].Error.Code)
	}
}

func TestServerParseError(t *testing.T) {
	s := testServer()

	input := bytes.NewBufferString("not valid json\n")
	var output bytes.Buffer
	s.Serve(input, &output)

	var resp rpcResponse
	json.NewDecoder(&output).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected parse error")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700", resp.Error.Code)
	}
}

func TestServerNotificationSkipped(t *testing.T) {
	s := testServer()
	// notifications/initialized should not produce a response
	responses := sendRPC(t, s,
		rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"},
		rpcRequest{JSONRPC: "2.0", ID: float64(1), Method: "ping"},
	)

	// Should only get 1 response (ping), not 2
	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1 (notification should be skipped)", len(responses))
	}
}

func TestServerMultipleRequests(t *testing.T) {
	s := testServer()
	responses := sendRPC(t, s,
		rpcRequest{JSONRPC: "2.0", ID: float64(1), Method: "initialize"},
		rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"},
		rpcRequest{JSONRPC: "2.0", ID: float64(2), Method: "tools/list"},
		rpcRequest{JSONRPC: "2.0", ID: float64(3), Method: "ping"},
	)

	if len(responses) != 3 {
		t.Fatalf("responses = %d, want 3", len(responses))
	}
}

func TestServerToolsCallMissingName(t *testing.T) {
	s := testServer()
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{},
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("expected error for missing tool name")
	}
}

// --- Approval tests (non-blocking flow) ---

func approvalServer() *Server {
	reg := registry.New()
	reg.LoadManual(&registry.Tool{
		Name: "risky_tool", Description: "Risky", Source: "openapi",
	})

	pol := policy.NewEngine([]config.Policy{
		{Name: "approval-policy", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"risky_tool"}, Action: "human_approval"},
		}},
	})

	traces := trace.NewStore(100)
	handler := proxy.NewHandler(reg, pol, traces)

	return &Server{
		Registry:  reg,
		Policy:    pol,
		Traces:    traces,
		Approvals: approval.NewStore(5 * time.Second),
		Handler:   handler,
		AgentID:   "claude",
	}
}

// extractText extracts the text from an MCP content response.
func extractText(t *testing.T, resp rpcResponse) string {
	t.Helper()
	result, _ := resp.Result.(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content in response")
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func TestServerApprovalReturnsImmediately(t *testing.T) {
	s := approvalServer()

	// Calling a tool that requires approval should return immediately (non-blocking)
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{
			"name":      "risky_tool",
			"arguments": map[string]any{},
		},
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}

	text := extractText(t, responses[0])
	if !strings.Contains(text, "Approval required") {
		t.Errorf("expected 'Approval required', got: %s", text)
	}
	if !strings.Contains(text, "approval.resolve") {
		t.Errorf("expected instructions for approval.resolve, got: %s", text)
	}

	// Should have one pending approval
	pending := s.Approvals.ListPending()
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
}

func TestServerApprovalResolveApprove(t *testing.T) {
	s := approvalServer()

	// Step 1: trigger approval
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{
			"name":      "risky_tool",
			"arguments": map[string]any{},
		},
	})
	text := extractText(t, responses[0])
	if !strings.Contains(text, "Approval required") {
		t.Fatalf("expected approval prompt, got: %s", text)
	}

	pending := s.Approvals.ListPending()
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	shortID := pending[0].ID[:8]

	// Step 2: approve via virtual tool
	responses = sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(2), Method: "tools/call",
		Params: map[string]any{
			"name":      "approval.resolve",
			"arguments": map[string]any{"id": shortID, "decision": "approve"},
		},
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("RPC error: %v", responses[0].Error)
	}

	// Should be resolved now
	remaining := s.Approvals.ListPending()
	if len(remaining) != 0 {
		t.Errorf("pending after approve = %d, want 0", len(remaining))
	}
}

func TestServerApprovalResolveDeny(t *testing.T) {
	s := approvalServer()

	// Step 1: trigger approval
	sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{
			"name":      "risky_tool",
			"arguments": map[string]any{},
		},
	})

	pending := s.Approvals.ListPending()
	shortID := pending[0].ID[:8]

	// Step 2: deny
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(2), Method: "tools/call",
		Params: map[string]any{
			"name":      "approval.resolve",
			"arguments": map[string]any{"id": shortID, "decision": "deny"},
		},
	})

	text := extractText(t, responses[0])
	if !strings.Contains(text, "Denied") {
		t.Errorf("expected 'Denied', got: %s", text)
	}
}

func TestServerApprovalPendingList(t *testing.T) {
	s := approvalServer()

	// No pending initially
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{
			"name":      "approval.pending",
			"arguments": map[string]any{},
		},
	})
	text := extractText(t, responses[0])
	if !strings.Contains(text, "No pending") {
		t.Errorf("expected 'No pending', got: %s", text)
	}

	// Trigger an approval
	sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(2), Method: "tools/call",
		Params: map[string]any{
			"name":      "risky_tool",
			"arguments": map[string]any{},
		},
	})

	// Now should have 1 pending
	responses = sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(3), Method: "tools/call",
		Params: map[string]any{
			"name":      "approval.pending",
			"arguments": map[string]any{},
		},
	})
	text = extractText(t, responses[0])
	if !strings.Contains(text, "risky_tool") {
		t.Errorf("expected 'risky_tool' in pending list, got: %s", text)
	}
}

func TestServerApprovalResolveNotFound(t *testing.T) {
	s := approvalServer()

	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{
			"name":      "approval.resolve",
			"arguments": map[string]any{"id": "nonexist", "decision": "approve"},
		},
	})

	if responses[0].Error == nil {
		t.Fatal("expected error for unknown approval ID")
	}
	if !strings.Contains(responses[0].Error.Message, "not found") {
		t.Errorf("expected 'not found', got: %s", responses[0].Error.Message)
	}
}

func TestServerApprovalResolveInvalidDecision(t *testing.T) {
	s := approvalServer()

	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{
			"name":      "approval.resolve",
			"arguments": map[string]any{"id": "abc", "decision": "maybe"},
		},
	})

	if responses[0].Error == nil {
		t.Fatal("expected error for invalid decision")
	}
	if !strings.Contains(responses[0].Error.Message, "Invalid") {
		t.Errorf("expected 'Invalid', got: %s", responses[0].Error.Message)
	}
}

func TestServerToolsListIncludesVirtualTools(t *testing.T) {
	s := approvalServer()
	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/list",
	})

	result, _ := responses[0].Result.(map[string]any)
	tools, _ := result["tools"].([]any)

	found := map[string]bool{}
	for _, t := range tools {
		tool, _ := t.(map[string]any)
		name, _ := tool["name"].(string)
		found[name] = true
	}

	if !found["approval.resolve"] {
		t.Error("approval.resolve not in tools list")
	}
	if !found["approval.pending"] {
		t.Error("approval.pending not in tools list")
	}
}

func TestServerApprovalFallbackNoStore(t *testing.T) {
	s := testServer()
	// Add a tool with human_approval policy but no Approvals store
	s.Registry.LoadManual(&registry.Tool{Name: "needs_approval", Source: "openapi"})
	s.Policy = policy.NewEngine([]config.Policy{
		{Name: "test", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"needs_approval"}, Action: "human_approval"},
		}},
	})

	responses := sendRPC(t, s, rpcRequest{
		JSONRPC: "2.0", ID: float64(1), Method: "tools/call",
		Params: map[string]any{
			"name":      "needs_approval",
			"arguments": map[string]any{},
		},
	})

	if len(responses) != 1 {
		t.Fatalf("responses = %d, want 1", len(responses))
	}

	text := extractText(t, responses[0])
	if !strings.Contains(text, "human approval") {
		t.Errorf("expected fallback text, got: %s", text)
	}
}
