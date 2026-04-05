package grant

import (
	"testing"
	"time"
)

func TestGrantAndCheck(t *testing.T) {
	s := NewStore()
	s.Add("claude", "filesystem.write_*", "user", 30*time.Minute)

	if g := s.Check("claude", "filesystem.write_file"); g == nil {
		t.Fatal("grant should match filesystem.write_file")
	}
	if g := s.Check("claude", "filesystem.read_file"); g != nil {
		t.Fatal("grant should not match filesystem.read_file")
	}
	if g := s.Check("other-agent", "filesystem.write_file"); g != nil {
		t.Fatal("grant should not match other agent")
	}
}

func TestGrantWildcardAgent(t *testing.T) {
	s := NewStore()
	s.Add("*", "gmail.*", "admin", 10*time.Minute)

	if g := s.Check("claude", "gmail.send_email"); g == nil {
		t.Fatal("wildcard agent grant should match any agent")
	}
	if g := s.Check("support-bot", "gmail.read_email"); g == nil {
		t.Fatal("wildcard agent grant should match any agent")
	}
}

func TestGrantExpiration(t *testing.T) {
	s := NewStore()
	s.Add("claude", "filesystem.*", "user", 1*time.Millisecond)

	time.Sleep(5 * time.Millisecond)

	if g := s.Check("claude", "filesystem.write_file"); g != nil {
		t.Fatal("expired grant should not match")
	}
}

func TestGrantRevoke(t *testing.T) {
	s := NewStore()
	g := s.Add("claude", "filesystem.*", "user", 30*time.Minute)

	if !s.Revoke(g.ID) {
		t.Fatal("revoke should return true")
	}
	if s.Check("claude", "filesystem.write_file") != nil {
		t.Fatal("revoked grant should not match")
	}
}

func TestGrantRevokeByPrefix(t *testing.T) {
	s := NewStore()
	g := s.Add("claude", "filesystem.*", "user", 30*time.Minute)

	prefix := g.ID[:6]
	if !s.Revoke(prefix) {
		t.Fatal("revoke by prefix should return true")
	}
	if len(s.List()) != 0 {
		t.Fatal("grant list should be empty after revoke")
	}
}

func TestGrantList(t *testing.T) {
	s := NewStore()
	s.Add("claude", "filesystem.*", "user", 30*time.Minute)
	s.Add("claude", "gmail.*", "user", 10*time.Minute)
	s.Add("claude", "expired.*", "user", 1*time.Millisecond)

	time.Sleep(5 * time.Millisecond)

	active := s.List()
	if len(active) != 2 {
		t.Fatalf("expected 2 active grants, got %d", len(active))
	}
}

func TestGrantCleanup(t *testing.T) {
	s := NewStore()
	s.Add("claude", "a.*", "user", 1*time.Millisecond)
	s.Add("claude", "b.*", "user", 1*time.Millisecond)
	s.Add("claude", "c.*", "user", 30*time.Minute)

	time.Sleep(5 * time.Millisecond)

	removed := s.Cleanup()
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	if len(s.List()) != 1 {
		t.Fatalf("expected 1 active grant, got %d", len(s.List()))
	}
}

func TestGrantRemaining(t *testing.T) {
	s := NewStore()
	g := s.Add("claude", "filesystem.*", "user", 10*time.Minute)

	r := g.Remaining()
	if r < 9*time.Minute || r > 10*time.Minute {
		t.Fatalf("remaining should be ~10min, got %v", r)
	}
}
