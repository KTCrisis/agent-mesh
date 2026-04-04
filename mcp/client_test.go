package mcp

import (
	"context"
	"testing"
	"time"
)

func TestNewStdioClient(t *testing.T) {
	c := NewStdioClient("test", "echo", []string{"hello"}, map[string]string{"FOO": "bar"})
	if c.Name != "test" {
		t.Errorf("name = %q", c.Name)
	}
	if c.Transport != "stdio" {
		t.Errorf("transport = %q", c.Transport)
	}
	status, _ := c.Status()
	if status != "connecting" {
		t.Errorf("initial status = %q, want connecting", status)
	}
}

func TestConnectBadCommand(t *testing.T) {
	c := NewStdioClient("bad", "/nonexistent/binary", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.Connect(ctx)
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}

	status, lastErr := c.Status()
	if status != "error" {
		t.Errorf("status = %q, want error", status)
	}
	if lastErr == "" {
		t.Error("lastError should be set")
	}
}

func TestConnectTimeoutHandshake(t *testing.T) {
	// sleep will start but never write to stdout → handshake times out
	c := NewStdioClient("slow", "sleep", []string{"60"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := c.Connect(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// Verify subprocess is cleaned up
	status, _ := c.Status()
	if status != "closed" {
		t.Errorf("status = %q, want closed (cleanup after failed connect)", status)
	}
}

func TestCloseIdempotent(t *testing.T) {
	c := NewStdioClient("test", "cat", nil, nil)
	// Close without ever connecting should not panic
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("double Close: %v", err)
	}
}

func TestToolsReturnsCopy(t *testing.T) {
	c := &MCPClient{
		Name: "test",
		done: make(chan struct{}),
	}
	c.tools = []MCPTool{{Name: "a"}, {Name: "b"}}

	tools := c.Tools()
	tools[0].Name = "mutated"

	// Original should be unchanged
	if c.tools[0].Name != "a" {
		t.Error("Tools() should return a copy, not a reference")
	}
}

func TestFailAllPending(t *testing.T) {
	c := &MCPClient{
		pending: make(map[int64]chan rpcResponse),
		done:    make(chan struct{}),
	}

	ch1 := make(chan rpcResponse, 1)
	ch2 := make(chan rpcResponse, 1)
	c.pending[1] = ch1
	c.pending[2] = ch2

	c.failAllPending("test failure")

	// Both channels should receive error responses
	resp1 := <-ch1
	if resp1.Error == nil || resp1.Error.Message != "test failure" {
		t.Errorf("ch1 error = %v", resp1.Error)
	}

	resp2 := <-ch2
	if resp2.Error == nil || resp2.Error.Message != "test failure" {
		t.Errorf("ch2 error = %v", resp2.Error)
	}

	// Pending map should be empty
	if len(c.pending) != 0 {
		t.Errorf("pending = %d, want 0", len(c.pending))
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		input any
		want  int64
		ok    bool
	}{
		{float64(42), 42, true},
		{int64(42), 42, true},
		{int(42), 42, true},
		{"not a number", 0, false},
		{nil, 0, false},
		{true, 0, false},
	}
	for _, tt := range tests {
		got, ok := toInt64(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("toInt64(%v) = (%d, %v), want (%d, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}
