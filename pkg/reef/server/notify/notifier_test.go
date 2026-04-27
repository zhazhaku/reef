package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/reef"
)

func testAlert() Alert {
	return Alert{
		Event:           "task_escalated",
		TaskID:          "task-1",
		Status:          "Escalated",
		Instruction:     "Write a function",
		RequiredRole:    "coder",
		Error:           &reef.TaskError{Type: "timeout", Message: "task timed out"},
		EscalationCount: 2,
		MaxEscalations:  2,
		Timestamp:       time.Now(),
	}
}

func TestManager_AddAndCount(t *testing.T) {
	m := NewManager(nil)
	if m.Count() != 0 {
		t.Errorf("expected 0 notifiers, got %d", m.Count())
	}
	m.Add(NewWebhookNotifier([]string{"http://example.com"}))
	m.Add(NewSlackNotifier("http://example.com"))
	if m.Count() != 2 {
		t.Errorf("expected 2 notifiers, got %d", m.Count())
	}
}

func TestManager_NotifyAll_Fanout(t *testing.T) {
	var mu sync.Mutex
	var received []string

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, "srv1")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, "srv2")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()

	m := NewManager(nil)
	m.Add(NewWebhookNotifier([]string{srv1.URL}))
	m.Add(NewWebhookNotifier([]string{srv2.URL}))

	m.NotifyAll(context.Background(), testAlert())

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Errorf("expected 2 notifications, got %d", len(received))
	}
}

func TestManager_NotifyAll_OneFails(t *testing.T) {
	var mu sync.Mutex
	var received bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = true
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := NewManager(nil)
	m.Add(NewWebhookNotifier([]string{"http://invalid.example.com"})) // will fail
	m.Add(NewWebhookNotifier([]string{srv.URL}))                      // will succeed

	m.NotifyAll(context.Background(), testAlert())

	mu.Lock()
	defer mu.Unlock()
	if !received {
		t.Error("expected srv to receive notification despite other failing")
	}
}

func TestWebhookNotifier_Success(t *testing.T) {
	var received Alert
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier([]string{srv.URL})
	err := n.Notify(context.Background(), testAlert())
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if received.TaskID != "task-1" {
		t.Errorf("expected task-1, got %s", received.TaskID)
	}
}

func TestWebhookNotifier_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewWebhookNotifier([]string{srv.URL})
	err := n.Notify(context.Background(), testAlert())
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestSlackNotifier_Success(t *testing.T) {
	var received json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewSlackNotifier(srv.URL)
	err := n.Notify(context.Background(), testAlert())
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if received == nil {
		t.Error("expected message to be sent")
	}
}

func TestFeishuNotifier_Success(t *testing.T) {
	var received feishuCard
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewFeishuNotifier(srv.URL)
	err := n.Notify(context.Background(), testAlert())
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if received.MsgType != "interactive" {
		t.Errorf("expected interactive, got %s", received.MsgType)
	}
}

func TestWeComNotifier_Success(t *testing.T) {
	var received wecomMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWeComNotifier(srv.URL)
	err := n.Notify(context.Background(), testAlert())
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if received.MsgType != "markdown" {
		t.Errorf("expected markdown, got %s", received.MsgType)
	}
}
