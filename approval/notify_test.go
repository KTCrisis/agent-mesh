package approval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNotifyOnSubmit(t *testing.T) {
	var mu sync.Mutex
	var received []notifyPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p notifyPayload
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	s := NewStore(5 * time.Second)
	s.Notifier = NewNotifier(srv.URL)

	s.Submit("claude", "write_file", "rule-1", map[string]any{"path": "/tmp/x"}, "")

	// Wait for async POST
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("got %d notifications, want 1", len(received))
	}
	if received[0].Event != "pending" {
		t.Errorf("event = %q, want pending", received[0].Event)
	}
	if received[0].Tool != "write_file" {
		t.Errorf("tool = %q, want write_file", received[0].Tool)
	}
}

func TestCallbackOnResolve(t *testing.T) {
	var mu sync.Mutex
	var received []notifyPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p notifyPayload
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	s := NewStore(5 * time.Second)
	s.Notifier = NewNotifier("") // no notify URL — only callback

	pa := s.Submit("claude", "gmail.send", "rule-1", nil, srv.URL)

	s.Approve(pa.ID, "admin")

	// Wait for async POST
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("got %d callbacks, want 1", len(received))
	}
	if received[0].Event != "approved" {
		t.Errorf("event = %q, want approved", received[0].Event)
	}
	if received[0].ResolvedBy != "admin" {
		t.Errorf("resolved_by = %q, want admin", received[0].ResolvedBy)
	}
}

func TestNotifyAndCallback(t *testing.T) {
	var mu sync.Mutex
	var received []notifyPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p notifyPayload
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	s := NewStore(5 * time.Second)
	s.Notifier = NewNotifier(srv.URL)

	// Agent provides callback URL = same test server
	pa := s.Submit("claude", "write_file", "rule-1", nil, srv.URL)
	time.Sleep(50 * time.Millisecond)

	s.Deny(pa.ID, "security")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("got %d events, want 2 (pending + denied)", len(received))
	}
	if received[0].Event != "pending" {
		t.Errorf("first event = %q, want pending", received[0].Event)
	}
	if received[1].Event != "denied" {
		t.Errorf("second event = %q, want denied", received[1].Event)
	}
}

func TestNoNotifierNoPanic(t *testing.T) {
	s := NewStore(5 * time.Second)
	// Notifier is nil — should not panic
	pa := s.Submit("claude", "tool", "r", nil, "")
	s.Approve(pa.ID, "tester")
	<-pa.Result
}

func TestCallbackOnTimeout(t *testing.T) {
	var mu sync.Mutex
	var received []notifyPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p notifyPayload
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	s := NewStore(50 * time.Millisecond)
	s.Notifier = NewNotifier("")

	s.Submit("claude", "tool", "r", nil, srv.URL)

	// Wait for timeout + async POST
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("got %d callbacks, want 1 (timeout)", len(received))
	}
	if received[0].Event != "timeout" {
		t.Errorf("event = %q, want timeout", received[0].Event)
	}
}
