package trace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecord(t *testing.T) {
	s := NewStore(100)
	s.Record(Entry{AgentID: "bot", Tool: "get_order", Policy: "allow"})

	entries := s.Query("", "", 10)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].AgentID != "bot" {
		t.Errorf("agent_id = %q", entries[0].AgentID)
	}
	if entries[0].TraceID == "" {
		t.Error("trace_id should be auto-generated")
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("timestamp should be auto-set")
	}
}

func TestQueryFilterAgent(t *testing.T) {
	s := NewStore(100)
	s.Record(Entry{AgentID: "bot-a", Tool: "x", Policy: "allow"})
	s.Record(Entry{AgentID: "bot-b", Tool: "y", Policy: "allow"})
	s.Record(Entry{AgentID: "bot-a", Tool: "z", Policy: "deny"})

	entries := s.Query("bot-a", "", 10)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
}

func TestQueryFilterTool(t *testing.T) {
	s := NewStore(100)
	s.Record(Entry{AgentID: "bot", Tool: "get_order", Policy: "allow"})
	s.Record(Entry{AgentID: "bot", Tool: "delete_order", Policy: "deny"})

	entries := s.Query("", "get_order", 10)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
}

func TestQueryLimit(t *testing.T) {
	s := NewStore(100)
	for i := 0; i < 20; i++ {
		s.Record(Entry{AgentID: "bot", Tool: "x", Policy: "allow"})
	}
	entries := s.Query("", "", 5)
	if len(entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(entries))
	}
}

func TestEviction(t *testing.T) {
	s := NewStore(10)
	for i := 0; i < 20; i++ {
		s.Record(Entry{AgentID: "bot", Tool: "x", Policy: "allow"})
	}
	stats := s.Stats()
	if stats["total"] != 10 {
		t.Errorf("total = %d, want 10 (evicted)", stats["total"])
	}
}

func TestStats(t *testing.T) {
	s := NewStore(100)
	s.Record(Entry{Policy: "allow"})
	s.Record(Entry{Policy: "allow"})
	s.Record(Entry{Policy: "deny"})
	s.Record(Entry{Policy: "human_approval"})
	s.Record(Entry{Policy: "allow", Error: "timeout"})

	stats := s.Stats()
	if stats["total"] != 5 {
		t.Errorf("total = %d", stats["total"])
	}
	if stats["allowed"] != 3 {
		t.Errorf("allowed = %d", stats["allowed"])
	}
	if stats["denied"] != 1 {
		t.Errorf("denied = %d", stats["denied"])
	}
	if stats["human_approval"] != 1 {
		t.Errorf("human_approval = %d", stats["human_approval"])
	}
	if stats["errors"] != 1 {
		t.Errorf("errors = %d", stats["errors"])
	}
}

func TestPersistentStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traces.jsonl")

	// Create store, write entries
	s, err := NewPersistentStore(100, path)
	if err != nil {
		t.Fatalf("NewPersistentStore: %v", err)
	}
	s.Record(Entry{AgentID: "bot", Tool: "get_order", Policy: "allow"})
	s.Record(Entry{AgentID: "bot", Tool: "delete_order", Policy: "deny"})
	s.Close()

	// Verify file exists and has content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("trace file is empty")
	}

	// Reopen store — should reload entries
	s2, err := NewPersistentStore(100, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	entries := s2.Query("", "", 10)
	if len(entries) != 2 {
		t.Fatalf("reloaded entries = %d, want 2", len(entries))
	}

	// Most recent first
	if entries[0].Tool != "delete_order" {
		t.Errorf("first entry = %q, want delete_order (most recent)", entries[0].Tool)
	}
	if entries[1].Tool != "get_order" {
		t.Errorf("second entry = %q, want get_order", entries[1].Tool)
	}
}

func TestPersistentStoreAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traces.jsonl")

	// Session 1: write 2 entries
	s1, _ := NewPersistentStore(100, path)
	s1.Record(Entry{AgentID: "bot", Tool: "a", Policy: "allow"})
	s1.Record(Entry{AgentID: "bot", Tool: "b", Policy: "allow"})
	s1.Close()

	// Session 2: write 1 more entry
	s2, _ := NewPersistentStore(100, path)
	s2.Record(Entry{AgentID: "bot", Tool: "c", Policy: "deny"})
	s2.Close()

	// Session 3: should see all 3
	s3, _ := NewPersistentStore(100, path)
	defer s3.Close()

	entries := s3.Query("", "", 10)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
}

func TestPersistentStoreEviction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traces.jsonl")

	// Write 20 entries to file
	s1, _ := NewPersistentStore(100, path)
	for i := 0; i < 20; i++ {
		s1.Record(Entry{AgentID: "bot", Tool: "x", Policy: "allow"})
	}
	s1.Close()

	// Reload with maxSize 10 — should keep only last 10
	s2, _ := NewPersistentStore(10, path)
	defer s2.Close()

	stats := s2.Stats()
	if stats["total"] != 10 {
		t.Errorf("total = %d, want 10 (evicted on load)", stats["total"])
	}
}

func TestPersistentStoreNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-traces.jsonl")

	// File doesn't exist yet — should create it
	s, err := NewPersistentStore(100, path)
	if err != nil {
		t.Fatalf("NewPersistentStore: %v", err)
	}
	defer s.Close()

	s.Record(Entry{AgentID: "bot", Tool: "x", Policy: "allow"})

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("trace file should have been created")
	}
}

func TestPersistentStoreRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traces.jsonl")

	s, _ := NewPersistentStore(10000, path)
	s.maxFileSize = 500 // rotate after 500 bytes

	// Write entries until rotation triggers
	for i := 0; i < 20; i++ {
		s.Record(Entry{AgentID: "bot", Tool: "tool_with_long_name", Policy: "allow"})
	}
	s.Close()

	// The .old file should exist
	oldPath := path + ".old"
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		t.Fatal("rotated .old file should exist")
	}

	// Current file should be smaller than max
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() > 500 {
		t.Errorf("current file size = %d, should be < 500 after rotation", info.Size())
	}
}

func TestCloseIdempotent(t *testing.T) {
	s := NewStore(100)
	// Close on in-memory store should not panic
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("double Close: %v", err)
	}
}
