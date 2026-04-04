package registry

import (
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := New()
	if r == nil {
		t.Fatal("New() returned nil")
	}
	if len(r.All()) != 0 {
		t.Errorf("new registry should be empty, got %d", len(r.All()))
	}
}

func TestLoadManual(t *testing.T) {
	r := New()
	r.LoadManual(&Tool{Name: "test_tool", Description: "a test", Source: "openapi"})

	if tool := r.Get("test_tool"); tool == nil {
		t.Fatal("Get returned nil for registered tool")
	} else if tool.Description != "a test" {
		t.Errorf("description = %q", tool.Description)
	}

	if r.Get("nonexistent") != nil {
		t.Error("Get should return nil for unknown tool")
	}
}

func TestRemove(t *testing.T) {
	r := New()
	r.LoadManual(&Tool{Name: "a"})
	r.LoadManual(&Tool{Name: "b"})
	r.Remove("a")

	if r.Get("a") != nil {
		t.Error("tool 'a' should be removed")
	}
	if r.Get("b") == nil {
		t.Error("tool 'b' should still exist")
	}
}

func TestLoadMCP(t *testing.T) {
	r := New()
	defs := []MCPToolDef{
		{Name: "read_file", Description: "Read a file", Params: []Param{{Name: "path", In: "body", Type: "string", Required: true}}},
		{Name: "write_file", Description: "Write a file", Params: []Param{{Name: "path", In: "body", Type: "string", Required: true}}},
	}
	r.LoadMCP("filesystem", defs)

	if len(r.All()) != 2 {
		t.Fatalf("tools = %d, want 2", len(r.All()))
	}

	tool := r.Get("filesystem.read_file")
	if tool == nil {
		t.Fatal("tool filesystem.read_file not found")
	}
	if tool.Source != "mcp" {
		t.Errorf("source = %q, want mcp", tool.Source)
	}
	if tool.MCPServer != "filesystem" {
		t.Errorf("mcp_server = %q, want filesystem", tool.MCPServer)
	}
	if tool.Description != "Read a file" {
		t.Errorf("description = %q", tool.Description)
	}
	if len(tool.Params) != 1 || tool.Params[0].Name != "path" {
		t.Errorf("params = %+v", tool.Params)
	}
}

func TestLoadMCPNamespacing(t *testing.T) {
	r := New()
	r.LoadMCP("server-a", []MCPToolDef{{Name: "do_thing"}})
	r.LoadMCP("server-b", []MCPToolDef{{Name: "do_thing"}})

	if len(r.All()) != 2 {
		t.Fatalf("tools = %d, want 2 (no collision)", len(r.All()))
	}
	if r.Get("server-a.do_thing") == nil {
		t.Error("server-a.do_thing not found")
	}
	if r.Get("server-b.do_thing") == nil {
		t.Error("server-b.do_thing not found")
	}
}

func TestRemoveByServer(t *testing.T) {
	r := New()
	r.LoadMCP("fs", []MCPToolDef{{Name: "read"}, {Name: "write"}})
	r.LoadMCP("db", []MCPToolDef{{Name: "query"}})
	r.LoadManual(&Tool{Name: "rest_tool", Source: "openapi"})

	r.RemoveByServer("fs")

	if r.Get("fs.read") != nil || r.Get("fs.write") != nil {
		t.Error("fs tools should be removed")
	}
	if r.Get("db.query") == nil {
		t.Error("db.query should still exist")
	}
	if r.Get("rest_tool") == nil {
		t.Error("rest_tool should still exist")
	}
}

func TestNewMCPToolDef(t *testing.T) {
	def := NewMCPToolDef("my_tool", "does stuff", map[string]MCPPropDef{
		"path":    {Type: "string"},
		"content": {Type: "string"},
		"force":   {Type: "boolean"},
	}, []string{"path"})

	if def.Name != "my_tool" {
		t.Errorf("name = %q", def.Name)
	}
	if def.Description != "does stuff" {
		t.Errorf("description = %q", def.Description)
	}
	if len(def.Params) != 3 {
		t.Fatalf("params = %d, want 3", len(def.Params))
	}

	// Check required flag
	paramMap := make(map[string]Param)
	for _, p := range def.Params {
		paramMap[p.Name] = p
	}
	if !paramMap["path"].Required {
		t.Error("path should be required")
	}
	if paramMap["content"].Required {
		t.Error("content should not be required")
	}
	if paramMap["force"].Required {
		t.Error("force should not be required")
	}
}

func TestMixedSources(t *testing.T) {
	r := New()
	r.LoadManual(&Tool{Name: "get_order", Source: "openapi", Method: "GET", Path: "/orders/{id}"})
	r.LoadMCP("fs", []MCPToolDef{{Name: "read_file"}})

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("tools = %d, want 2", len(all))
	}

	rest := r.Get("get_order")
	if rest.Source != "openapi" {
		t.Errorf("rest source = %q", rest.Source)
	}

	mcp := r.Get("fs.read_file")
	if mcp.Source != "mcp" {
		t.Errorf("mcp source = %q", mcp.Source)
	}
}
