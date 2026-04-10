package trace

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
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

	// CLI fields (populated when source = cli)
	ExitCode *int `json:"exit_code,omitempty"`

	// Approval fields (populated when policy = human_approval)
	ApprovalID     string `json:"approval_id,omitempty"`
	ApprovalStatus string `json:"approval_status,omitempty"` // approved, denied, timeout
	ApprovedBy     string `json:"approved_by,omitempty"`
	ApprovalMs     int64  `json:"approval_ms,omitempty"`

	// Supervisor fields (populated when resolved by a supervisor agent)
	SupervisorReasoning  string  `json:"supervisor_reasoning,omitempty"`
	SupervisorConfidence float64 `json:"supervisor_confidence,omitempty"`

	// Token counts — real when the provider exposes them (LLM tools),
	// chars/4 estimate otherwise. TokensSource says which.
	EstimatedInputTokens  int    `json:"estimated_input_tokens,omitempty"`
	EstimatedOutputTokens int    `json:"estimated_output_tokens,omitempty"`
	TokensSource          string `json:"tokens_source,omitempty"` // "real" | "estimate"
}

// Store is a thread-safe trace store with optional JSONL file persistence.
type Store struct {
	mu      sync.RWMutex
	entries []Entry
	maxSize int

	// JSONL file persistence (nil = in-memory only)
	file        *os.File
	writer      *bufio.Writer
	filePath    string
	fileSize    int64
	maxFileSize int64 // 0 = no rotation

	// OTEL exporter (nil = disabled)
	OTEL *OTELExporter
}

// NewStore creates an in-memory trace store.
func NewStore(maxSize int) *Store {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &Store{
		entries: make([]Entry, 0, 256),
		maxSize: maxSize,
	}
}

// NewPersistentStore creates a trace store that appends to a JSONL file.
// Existing entries are loaded from the file on startup.
// maxFileBytes controls file rotation (0 = no rotation, default 10MB).
func NewPersistentStore(maxSize int, path string) (*Store, error) {
	s := NewStore(maxSize)
	s.filePath = path
	s.maxFileSize = 10 * 1024 * 1024 // 10MB default

	// Load existing entries from file
	if err := s.loadFromFile(path); err != nil {
		slog.Warn("trace: could not load existing traces", "path", path, "error", err)
	}

	// Open file for appending
	if err := s.openFile(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) openFile() error {
	f, err := os.OpenFile(s.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	s.file = f
	s.writer = bufio.NewWriter(f)
	s.fileSize = info.Size()
	return nil
}

// Record adds a trace entry.
func (s *Store) Record(e Entry) {
	if e.TraceID == "" {
		e.TraceID = NewID()
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

	// Export to OTEL (async to not block Record)
	if s.OTEL != nil {
		go s.OTEL.Export(e)
	}

	// Append to JSONL file
	if s.writer != nil {
		data, err := json.Marshal(e)
		if err != nil {
			slog.Error("trace: failed to marshal entry", "error", err)
			return
		}
		n, _ := s.writer.Write(data)
		s.writer.WriteByte('\n')
		s.writer.Flush()
		s.fileSize += int64(n + 1)

		// Rotate if file exceeds max size
		if s.maxFileSize > 0 && s.fileSize >= s.maxFileSize {
			s.rotate()
		}
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

// Update finds a trace entry by TraceID and applies fn to mutate it.
// Returns true if the entry was found and updated.
func (s *Store) Update(traceID string, fn func(*Entry)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := len(s.entries) - 1; i >= 0; i-- {
		if s.entries[i].TraceID == traceID {
			fn(&s.entries[i])
			return true
		}
	}
	return false
}

// Stats returns aggregate counts.
func (s *Store) Stats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]int{
		"total":                   len(s.entries),
		"allowed":                 0,
		"denied":                  0,
		"human_approval":          0,
		"errors":                  0,
		"estimated_input_tokens":  0,
		"estimated_output_tokens": 0,
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
		stats["estimated_input_tokens"] += e.EstimatedInputTokens
		stats["estimated_output_tokens"] += e.EstimatedOutputTokens
	}
	return stats
}

// rotate renames the current file to .old and opens a new one.
// Must be called with mu held.
func (s *Store) rotate() {
	if s.writer != nil {
		s.writer.Flush()
	}
	if s.file != nil {
		s.file.Close()
	}

	oldPath := s.filePath + ".old"
	os.Remove(oldPath)
	if err := os.Rename(s.filePath, oldPath); err != nil {
		slog.Error("trace: failed to rotate file", "error", err)
		return
	}

	slog.Info("trace: rotated", "old", oldPath, "new", s.filePath)

	if err := s.openFile(); err != nil {
		slog.Error("trace: failed to reopen after rotation", "error", err)
	}
}

// Close flushes and closes the trace file.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer != nil {
		s.writer.Flush()
	}
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// loadFromFile reads existing JSONL entries into memory.
func (s *Store) loadFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file yet, start fresh
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer for potentially large trace lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	loaded := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			slog.Warn("trace: skipping malformed line", "error", err)
			continue
		}
		s.entries = append(s.entries, e)
		loaded++
	}

	// Apply max size limit
	if len(s.entries) > s.maxSize {
		s.entries = s.entries[len(s.entries)-s.maxSize:]
	}

	if loaded > 0 {
		slog.Info("trace: loaded from file", "path", path, "entries", loaded, "kept", len(s.entries))
	}

	return scanner.Err()
}

// NewID generates a random 16-byte trace ID (32 hex chars, W3C compatible).
func NewID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
