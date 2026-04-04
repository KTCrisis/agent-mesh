package mcp

import (
	"context"
	"testing"
)

func TestManagerAddGet(t *testing.T) {
	m := NewManager()

	c := &MCPClient{Name: "test-server", Transport: "stdio"}
	c.status = "ready"
	c.tools = []MCPTool{{Name: "do_thing", Description: "does a thing"}}

	m.Add(c)

	if got := m.Get("test-server"); got != c {
		t.Error("Get should return the added client")
	}
	if got := m.Get("nonexistent"); got != nil {
		t.Error("Get should return nil for unknown server")
	}
}

func TestManagerAll(t *testing.T) {
	m := NewManager()
	m.Add(&MCPClient{Name: "a"})
	m.Add(&MCPClient{Name: "b"})

	all := m.All()
	if len(all) != 2 {
		t.Errorf("All = %d, want 2", len(all))
	}
}

func TestManagerServerStatuses(t *testing.T) {
	m := NewManager()

	c := &MCPClient{Name: "fs", Transport: "stdio"}
	c.status = "ready"
	c.tools = []MCPTool{
		{Name: "read_file"},
		{Name: "write_file"},
	}
	m.Add(c)

	statuses := m.ServerStatuses()
	list, ok := statuses.([]ServerStatus)
	if !ok {
		t.Fatalf("ServerStatuses returned %T, want []ServerStatus", statuses)
	}
	if len(list) != 1 {
		t.Fatalf("statuses = %d, want 1", len(list))
	}

	s := list[0]
	if s.Name != "fs" {
		t.Errorf("name = %q", s.Name)
	}
	if s.Transport != "stdio" {
		t.Errorf("transport = %q", s.Transport)
	}
	if s.Status != "ready" {
		t.Errorf("status = %q", s.Status)
	}
	if len(s.Tools) != 2 {
		t.Errorf("tools = %d, want 2", len(s.Tools))
	}
}

func TestManagerCallToolNotFound(t *testing.T) {
	m := NewManager()
	_, err := m.CallTool(context.Background(), "nonexistent", "tool", nil)
	if err == nil {
		t.Error("expected error for unknown server")
	}
}
