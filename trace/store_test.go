package trace

import (
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
