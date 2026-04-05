package ratelimit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAllowWithinLimit(t *testing.T) {
	l := New()
	defer l.cleanup.Stop()

	l.SetLimit("test-policy", Limit{MaxPerMinute: 10, MaxTotal: 100})

	for i := 0; i < 10; i++ {
		params := fmt.Sprintf(`{"i":%d}`, i)
		if err := l.Check("agent-1", "test-policy", "tool-a", params); err != nil {
			t.Fatalf("call %d should be allowed: %v", i, err)
		}
		l.Record("agent-1", "tool-a", params)
	}
}

func TestDenyOverPerMinute(t *testing.T) {
	l := New()
	defer l.cleanup.Stop()

	l.SetLimit("test-policy", Limit{MaxPerMinute: 5})

	for i := 0; i < 5; i++ {
		l.Check("agent-1", "test-policy", "tool-a", "{}")
		l.Record("agent-1", "tool-a", "{}")
	}

	err := l.Check("agent-1", "test-policy", "tool-a", "{}")
	if err == nil {
		t.Fatal("6th call should be denied")
	}
	if !strings.Contains(err.Error(), "rate_limit") {
		t.Fatalf("expected rate_limit error, got: %v", err)
	}
}

func TestDenyOverTotal(t *testing.T) {
	l := New()
	defer l.cleanup.Stop()

	l.SetLimit("test-policy", Limit{MaxTotal: 3})

	for i := 0; i < 3; i++ {
		l.Check("agent-1", "test-policy", "tool-a", "{}")
		l.Record("agent-1", "tool-a", "{}")
	}

	err := l.Check("agent-1", "test-policy", "tool-a", "{}")
	if err == nil {
		t.Fatal("4th call should be denied (total budget)")
	}
	if !strings.Contains(err.Error(), "total budget") {
		t.Fatalf("expected total budget error, got: %v", err)
	}
}

func TestLoopDetection(t *testing.T) {
	l := New()
	defer l.cleanup.Stop()

	l.SetLimit("test-policy", Limit{MaxPerMinute: 100})

	params := `{"query":"same"}`
	for i := 0; i < 3; i++ {
		l.Check("agent-1", "test-policy", "search", params)
		l.Record("agent-1", "search", params)
	}

	err := l.Check("agent-1", "test-policy", "search", params)
	if err == nil {
		t.Fatal("4th identical call in 10s should be denied as loop")
	}
	if !strings.Contains(err.Error(), "loop_detected") {
		t.Fatalf("expected loop_detected, got: %v", err)
	}
}

func TestLoopAllowsDifferentParams(t *testing.T) {
	l := New()
	defer l.cleanup.Stop()

	l.SetLimit("test-policy", Limit{MaxPerMinute: 100})

	for i := 0; i < 5; i++ {
		params := `{"page":` + string(rune('0'+i)) + `}`
		if err := l.Check("agent-1", "test-policy", "search", params); err != nil {
			t.Fatalf("call %d with different params should be allowed: %v", i, err)
		}
		l.Record("agent-1", "search", params)
	}
}

func TestNoLimitConfigured(t *testing.T) {
	l := New()
	defer l.cleanup.Stop()

	// No SetLimit call — should always allow
	for i := 0; i < 100; i++ {
		if err := l.Check("agent-1", "unknown-policy", "tool-a", "{}"); err != nil {
			t.Fatalf("should allow when no limit configured: %v", err)
		}
		l.Record("agent-1", "tool-a", "{}")
	}
}

func TestIsolationBetweenAgents(t *testing.T) {
	l := New()
	defer l.cleanup.Stop()

	l.SetLimit("test-policy", Limit{MaxPerMinute: 3})

	for i := 0; i < 3; i++ {
		l.Check("agent-1", "test-policy", "tool-a", "{}")
		l.Record("agent-1", "tool-a", "{}")
	}

	// agent-2 should still be allowed
	if err := l.Check("agent-2", "test-policy", "tool-a", "{}"); err != nil {
		t.Fatalf("agent-2 should not be affected by agent-1 limit: %v", err)
	}
}

func TestStats(t *testing.T) {
	l := New()
	defer l.cleanup.Stop()

	l.SetLimit("test-policy", Limit{MaxPerMinute: 10, MaxTotal: 100})

	for i := 0; i < 5; i++ {
		l.Record("agent-1", "tool-a", "{}")
	}

	stats := l.Stats("agent-1", "test-policy")
	if stats == nil {
		t.Fatal("stats should not be nil")
	}
	if stats["total_calls"] != 5 {
		t.Fatalf("expected total_calls=5, got %v", stats["total_calls"])
	}
	if stats["remaining_total"] != 95 {
		t.Fatalf("expected remaining_total=95, got %v", stats["remaining_total"])
	}

	_ = time.Now() // keep import
}
