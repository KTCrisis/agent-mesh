package mcp

import (
	"context"
	"fmt"
	"sync"
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

func TestManagerConcurrentAccess(t *testing.T) {
	m := NewManager()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c := &MCPClient{Name: fmt.Sprintf("server-%d", n), Transport: "stdio"}
			c.status = "ready"
			c.tools = []MCPTool{{Name: "tool"}}
			c.done = newDoneChan()
			m.Add(c)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.All()
			m.ServerStatuses()
		}()
	}

	wg.Wait()

	if len(m.All()) != 20 {
		t.Errorf("clients = %d, want 20", len(m.All()))
	}
}

func TestManagerServerStatusError(t *testing.T) {
	m := NewManager()

	c := &MCPClient{Name: "broken", Transport: "stdio"}
	c.status = "error"
	c.lastError = "connection refused"
	c.tools = []MCPTool{}
	c.done = newDoneChan()
	m.Add(c)

	statuses := m.ServerStatuses()
	list := statuses.([]ServerStatus)
	if list[0].Status != "error" {
		t.Errorf("status = %q, want error", list[0].Status)
	}
	if list[0].Error != "connection refused" {
		t.Errorf("error = %q", list[0].Error)
	}
}
