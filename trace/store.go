package trace

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Entry represents a single traced tool call.
type Entry struct {
	TraceID    string         `json:"trace_id"`
	AgentID    string         `json:"agent_id"`
	Tool       string         `json:"tool"`
	Params     map[string]any `json:"params"`
	Policy     string         `json:"policy"`     // allow, deny, human_approval
	PolicyRule string         `json:"policy_rule"` // which rule matched
	StatusCode int            `json:"status_code"` // backend response status
	LatencyMs  int64          `json:"latency_ms"`
	Error      string         `json:"error,omitempty"`
	Timestamp  time.Time      `json:"timestamp"`
}

// Store is a thread-safe in-memory trace store.
type Store struct {
	mu      sync.RWMutex
	entries []Entry
	maxSize int
}

func NewStore(maxSize int) *Store {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &Store{
		entries: make([]Entry, 0, 256),
		maxSize: maxSize,
	}
}

// Record adds a trace entry.
func (s *Store) Record(e Entry) {
	if e.TraceID == "" {
		e.TraceID = newID()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, e)

	// Evict oldest if over max
	if len(s.entries) > s.maxSize {
		s.entries = s.entries[len(s.entries)-s.maxSize:]
	}
}

// Query returns the last n traces, optionally filtered by agent and/or tool.
func (s *Store) Query(agent string, tool string, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	var result []Entry
	// Iterate in reverse (most recent first)
	for i := len(s.entries) - 1; i >= 0 && len(result) < limit; i-- {
		e := s.entries[i]
		if agent != "" && e.AgentID != agent {
			continue
		}
		if tool != "" && e.Tool != tool {
			continue
		}
		result = append(result, e)
	}
	return result
}

// Stats returns aggregate counts.
func (s *Store) Stats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]int{
		"total":          len(s.entries),
		"allowed":        0,
		"denied":         0,
		"human_approval": 0,
		"errors":         0,
	}
	for _, e := range s.entries {
		switch e.Policy {
		case "allow":
			stats["allowed"]++
		case "deny":
			stats["denied"]++
		case "human_approval":
			stats["human_approval"]++
		}
		if e.Error != "" {
			stats["errors"]++
		}
	}
	return stats
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
