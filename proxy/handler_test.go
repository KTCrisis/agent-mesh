package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KTCrisis/agent-mesh/config"
	"github.com/KTCrisis/agent-mesh/policy"
	"github.com/KTCrisis/agent-mesh/registry"
	"github.com/KTCrisis/agent-mesh/trace"
)

// mockMCPForwarder implements MCPForwarder for testing.
type mockMCPForwarder struct {
	callResult any
	callErr    error
	statuses   any
}

func (m *mockMCPForwarder) CallTool(_ context.Context, serverName, toolName string, arguments map[string]any) (any, error) {
	if m.callErr != nil {
		return nil, m.callErr
	}
	return m.callResult, nil
}

func (m *mockMCPForwarder) ServerStatuses() any {
	return m.statuses
}

func setupHandler(t *testing.T) (*Handler, *httptest.Server) {
	t.Helper()

	// Backend API mock
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "doggie"})
	}))
	t.Cleanup(backend.Close)

	reg := registry.New()
	reg.LoadManual(&registry.Tool{
		Name:    "get_pet",
		Method:  "GET",
		Path:    "/pet/1",
		BaseURL: backend.URL,
		Source:  "openapi",
	})

	pol := policy.NewEngine([]config.Policy{
		{Name: "allow-all", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"*"}, Action: "allow"},
		}},
	})

	traces := trace.NewStore(100)
	handler := NewHandler(reg, pol, traces)
	return handler, backend
}

func TestHandleToolCallOpenAPI(t *testing.T) {
	handler, _ := setupHandler(t)

	req := httptest.NewRequest("POST", "/tool/get_pet", strings.NewReader(`{"params":{}}`))
	req.Header.Set("Authorization", "Bearer test-agent")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	var resp ToolCallResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Policy != "allow" {
		t.Errorf("policy = %q, want allow", resp.Policy)
	}
	if resp.Result == nil {
		t.Error("result should not be nil")
	}
}

func TestHandleToolCallUnknown(t *testing.T) {
	handler, _ := setupHandler(t)

	req := httptest.NewRequest("POST", "/tool/nonexistent", strings.NewReader(`{"params":{}}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleToolCallDenied(t *testing.T) {
	reg := registry.New()
	reg.LoadManual(&registry.Tool{Name: "secret_tool", Source: "openapi"})

	pol := policy.NewEngine([]config.Policy{
		{Name: "deny-all", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"*"}, Action: "deny"},
		}},
	})

	handler := NewHandler(reg, pol, trace.NewStore(100))

	req := httptest.NewRequest("POST", "/tool/secret_tool", strings.NewReader(`{"params":{}}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestHandleToolCallMCP(t *testing.T) {
	reg := registry.New()
	reg.LoadMCP("filesystem", []registry.MCPToolDef{
		{Name: "read_file", Description: "Read a file"},
	})

	pol := policy.NewEngine([]config.Policy{
		{Name: "allow-all", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"*"}, Action: "allow"},
		}},
	})

	handler := NewHandler(reg, pol, trace.NewStore(100))
	handler.MCPForwarder = &mockMCPForwarder{
		callResult: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "file contents here"},
			},
		},
	}

	req := httptest.NewRequest("POST", "/tool/filesystem.read_file",
		strings.NewReader(`{"params":{"path":"/tmp/test.txt"}}`))
	req.Header.Set("Authorization", "Bearer test-agent")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	var resp ToolCallResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Policy != "allow" {
		t.Errorf("policy = %q, want allow", resp.Policy)
	}
	if resp.Result == nil {
		t.Error("result should not be nil for MCP tool call")
	}
}

func TestHandleToolCallMCPError(t *testing.T) {
	reg := registry.New()
	reg.LoadMCP("broken", []registry.MCPToolDef{{Name: "fail"}})

	pol := policy.NewEngine([]config.Policy{
		{Name: "allow-all", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"*"}, Action: "allow"},
		}},
	})

	handler := NewHandler(reg, pol, trace.NewStore(100))
	handler.MCPForwarder = &mockMCPForwarder{callErr: fmt.Errorf("connection lost")}

	req := httptest.NewRequest("POST", "/tool/broken.fail", strings.NewReader(`{"params":{}}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 502 {
		t.Errorf("status = %d, want 502", w.Code)
	}
}

func TestHandleListTools(t *testing.T) {
	handler, _ := setupHandler(t)

	req := httptest.NewRequest("GET", "/tools", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	var tools []registry.Tool
	json.NewDecoder(w.Body).Decode(&tools)
	if len(tools) != 1 {
		t.Errorf("tools = %d, want 1", len(tools))
	}
}

func TestHandleMCPServersEmpty(t *testing.T) {
	handler, _ := setupHandler(t)

	req := httptest.NewRequest("GET", "/mcp-servers", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestHandleMCPServersWithForwarder(t *testing.T) {
	handler, _ := setupHandler(t)
	handler.MCPForwarder = &mockMCPForwarder{
		statuses: []map[string]any{
			{"name": "fs", "transport": "stdio", "status": "ready", "tools": []string{"read_file"}},
		},
	}

	req := httptest.NewRequest("GET", "/mcp-servers", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	var statuses []map[string]any
	json.NewDecoder(w.Body).Decode(&statuses)
	if len(statuses) != 1 {
		t.Errorf("statuses = %d, want 1", len(statuses))
	}
}

func TestHandleHealth(t *testing.T) {
	handler, _ := setupHandler(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	var health map[string]any
	json.NewDecoder(w.Body).Decode(&health)
	if health["status"] != "ok" {
		t.Errorf("status = %v", health["status"])
	}
}

func TestHandleTraces(t *testing.T) {
	handler, _ := setupHandler(t)

	// Generate a trace
	req := httptest.NewRequest("POST", "/tool/get_pet", strings.NewReader(`{"params":{}}`))
	req.Header.Set("Authorization", "Bearer test-agent")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Query traces
	req = httptest.NewRequest("GET", "/traces?agent=test-agent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	var traces []trace.Entry
	json.NewDecoder(w.Body).Decode(&traces)
	if len(traces) != 1 {
		t.Fatalf("traces = %d, want 1", len(traces))
	}
	if traces[0].Tool != "get_pet" {
		t.Errorf("tool = %q", traces[0].Tool)
	}
}

func TestHandle404(t *testing.T) {
	handler, _ := setupHandler(t)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestExtractAgentID(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer support-bot", "support-bot"},
		{"Bearer agent:admin-1", "admin-1"},
		{"", "anonymous"},
		{"Bearer ", "anonymous"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		if tt.header != "" {
			r.Header.Set("Authorization", tt.header)
		}
		got := extractAgentID(r)
		if got != tt.want {
			t.Errorf("extractAgentID(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestHandleToolCallInvalidJSON(t *testing.T) {
	handler, _ := setupHandler(t)

	req := httptest.NewRequest("POST", "/tool/get_pet", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleToolCallEmptyToolName(t *testing.T) {
	handler, _ := setupHandler(t)

	req := httptest.NewRequest("POST", "/tool/", strings.NewReader(`{"params":{}}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Empty tool name → not found in registry → 404
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleToolCallMCPNoForwarder(t *testing.T) {
	reg := registry.New()
	reg.LoadMCP("orphan", []registry.MCPToolDef{{Name: "tool"}})

	pol := policy.NewEngine([]config.Policy{
		{Name: "allow-all", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"*"}, Action: "allow"},
		}},
	})

	// No MCPForwarder set
	handler := NewHandler(reg, pol, trace.NewStore(100))

	req := httptest.NewRequest("POST", "/tool/orphan.tool", strings.NewReader(`{"params":{}}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 502 {
		t.Errorf("status = %d, want 502 (no forwarder)", w.Code)
	}
}

func TestHandleToolCallHumanApproval(t *testing.T) {
	reg := registry.New()
	reg.LoadManual(&registry.Tool{Name: "risky_tool", Source: "openapi"})

	pol := policy.NewEngine([]config.Policy{
		{Name: "approval", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"risky_tool"}, Action: "human_approval"},
		}},
	})

	handler := NewHandler(reg, pol, trace.NewStore(100))

	req := httptest.NewRequest("POST", "/tool/risky_tool", strings.NewReader(`{"params":{}}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 202 {
		t.Errorf("status = %d, want 202", w.Code)
	}

	var resp ToolCallResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Policy != "human_approval" {
		t.Errorf("policy = %q, want human_approval", resp.Policy)
	}
}

func TestForwardHTTPSpecialCharsInParams(t *testing.T) {
	// Backend that echoes the request URL
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"url": r.URL.String()})
	}))
	defer backend.Close()

	reg := registry.New()
	reg.LoadManual(&registry.Tool{
		Name:    "search",
		Method:  "GET",
		Path:    "/search",
		BaseURL: backend.URL,
		Source:  "openapi",
	})

	pol := policy.NewEngine([]config.Policy{
		{Name: "allow", Agent: "*", Rules: []config.Rule{
			{Tools: []string{"*"}, Action: "allow"},
		}},
	})

	handler := NewHandler(reg, pol, trace.NewStore(100))

	req := httptest.NewRequest("POST", "/tool/search",
		strings.NewReader(`{"params":{"q":"hello world&foo=bar"}}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp ToolCallResponse
	json.NewDecoder(w.Body).Decode(&resp)
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", resp.Result)
	}
	urlStr, _ := result["url"].(string)
	// The & should be encoded, not splitting query params
	if strings.Contains(urlStr, "foo=bar") {
		t.Errorf("URL params not properly encoded: %s", urlStr)
	}
}
