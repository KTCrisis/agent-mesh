package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

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
	if len(tools) != 2 {
		t.Errorf("tools = %d, want 2", len(tools))
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
