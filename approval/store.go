package approval

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

// Status represents the resolution state of an approval.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusDenied   Status = "denied"
	StatusTimeout  Status = "timeout"
)

// Resolution is the outcome sent through the channel.
type Resolution struct {
	Status     Status
	ResolvedBy string
	ResolvedAt time.Time
}

// PendingApproval represents a tool call waiting for human decision.
type PendingApproval struct {
	ID         string         `json:"id"`
	AgentID    string         `json:"agent_id"`
	Tool       string         `json:"tool"`
	Params     map[string]any `json:"params"`
	PolicyRule string         `json:"policy_rule"`
	TraceID    string         `json:"trace_id,omitempty"`
	Status     Status         `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	ResolvedBy string         `json:"resolved_by,omitempty"`
	ResolvedAt time.Time      `json:"resolved_at,omitempty"`
	Result     chan Resolution `json:"-"`
}

// Remaining returns how much time is left before timeout.
func (p *PendingApproval) Remaining(timeout time.Duration) time.Duration {
	remaining := timeout - time.Since(p.CreatedAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Store manages pending approvals with channel-based blocking.
type Store struct {
	mu      sync.RWMutex
	pending map[string]*PendingApproval
	timeout time.Duration
}

// NewStore creates an approval store with the given default timeout.
func NewStore(timeout time.Duration) *Store {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Store{
		pending: make(map[string]*PendingApproval),
		timeout: timeout,
	}
}

// Timeout returns the configured default timeout.
func (s *Store) Timeout() time.Duration {
	return s.timeout
}

// Submit creates a pending approval and starts a timeout goroutine.
// The caller should block on the returned PendingApproval.Result channel.
func (s *Store) Submit(agentID, tool, policyRule string, params map[string]any) *PendingApproval {
	pa := &PendingApproval{
		ID:         newID(),
		AgentID:    agentID,
		Tool:       tool,
		Params:     params,
		PolicyRule: policyRule,
		Status:     StatusPending,
		CreatedAt:  time.Now().UTC(),
		Result:     make(chan Resolution, 1),
	}

	s.mu.Lock()
	s.pending[pa.ID] = pa
	s.mu.Unlock()

	// Timeout goroutine
	go func() {
		time.Sleep(s.timeout)
		s.mu.Lock()
		defer s.mu.Unlock()
		if pa.Status != StatusPending {
			return // already resolved
		}
		pa.Status = StatusTimeout
		pa.ResolvedAt = time.Now().UTC()
		pa.Result <- Resolution{
			Status:     StatusTimeout,
			ResolvedBy: "system:timeout",
			ResolvedAt: pa.ResolvedAt,
		}
	}()

	return pa
}

var (
	ErrNotFound        = errors.New("approval not found")
	ErrAlreadyResolved = errors.New("approval already resolved")
)

// Resolve sets the status of a pending approval and unblocks the handler.
// Supports prefix matching: if id is not an exact match, it tries to find
// a unique entry whose ID starts with the given prefix.
func (s *Store) Resolve(id string, status Status, resolvedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pa := s.findLocked(id)
	if pa == nil {
		return ErrNotFound
	}
	if pa.Status != StatusPending {
		return ErrAlreadyResolved
	}

	now := time.Now().UTC()
	pa.Status = status
	pa.ResolvedBy = resolvedBy
	pa.ResolvedAt = now
	pa.Result <- Resolution{
		Status:     status,
		ResolvedBy: resolvedBy,
		ResolvedAt: now,
	}
	return nil
}

// Approve is a convenience wrapper for Resolve with StatusApproved.
func (s *Store) Approve(id, resolvedBy string) error {
	return s.Resolve(id, StatusApproved, resolvedBy)
}

// Deny is a convenience wrapper for Resolve with StatusDenied.
func (s *Store) Deny(id, resolvedBy string) error {
	return s.Resolve(id, StatusDenied, resolvedBy)
}

// Get returns a pending approval by ID or prefix, or nil if not found.
func (s *Store) Get(id string) *PendingApproval {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.findLocked(id)
}

// findLocked finds an approval by exact ID or unique prefix match.
// Caller must hold s.mu (read or write).
func (s *Store) findLocked(id string) *PendingApproval {
	// Exact match first
	if pa, ok := s.pending[id]; ok {
		return pa
	}
	// Prefix match — must be unique
	var match *PendingApproval
	for key, pa := range s.pending {
		if strings.HasPrefix(key, id) {
			if match != nil {
				return nil // ambiguous prefix
			}
			match = pa
		}
	}
	return match
}

// List returns all approvals (pending + resolved), most recent first.
func (s *Store) List() []*PendingApproval {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*PendingApproval, 0, len(s.pending))
	for _, pa := range s.pending {
		result = append(result, pa)
	}

	// Sort most recent first
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].CreatedAt.After(result[i].CreatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// ListPending returns only pending approvals, most recent first.
func (s *Store) ListPending() []*PendingApproval {
	all := s.List()
	result := make([]*PendingApproval, 0)
	for _, pa := range all {
		if pa.Status == StatusPending {
			result = append(result, pa)
		}
	}
	return result
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
