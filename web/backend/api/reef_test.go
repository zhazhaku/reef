// Package api provides tests for Reef swarm API endpoints.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockReefProvider implements ReefProvider for testing.
type mockReefProvider struct {
	status  ReefStatus
	tasks   []ReefTask
	clients []ReefClient
}

func (m *mockReefProvider) Status() (ReefStatus, error) {
	return m.status, nil
}

func (m *mockReefProvider) Tasks(filter ReefTaskFilter) ([]ReefTask, int, error) {
	result := m.tasks
	if filter.Status != "" {
		filtered := make([]ReefTask, 0)
		for _, t := range result {
			if t.Status == filter.Status {
				filtered = append(filtered, t)
			}
		}
		result = filtered
	}
	if filter.Limit > 0 && filter.Limit < len(result) {
		result = result[:filter.Limit]
	}
	return result, len(result), nil
}

func (m *mockReefProvider) TaskByID(id string) (*ReefTask, error) {
	for _, t := range m.tasks {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, nil
}

func (m *mockReefProvider) SubTasks(id string) ([]ReefTask, error) {
	var subs []ReefTask
	for _, t := range m.tasks {
		if t.ParentTaskID == id {
			subs = append(subs, t)
		}
	}
	return subs, nil
}

func (m *mockReefProvider) Clients() ([]ReefClient, error) {
	return m.clients, nil
}

func (m *mockReefProvider) CancelTask(id string) error {
	return nil
}

func setupReefTest() (*Handler, *mockReefProvider) {
	h := NewHandler("/tmp/test-config")
	mp := &mockReefProvider{
		status: ReefStatus{
			ServerVersion:    "1.0.0",
			Uptime:           "1h23m",
			ConnectedClients: 2,
			QueuedTasks:      1,
			RunningTasks:     1,
			CompletedTasks:   42,
			FailedTasks:      3,
			TotalTasks:       47,
			StartedAt:        time.Now(),
		},
		tasks: []ReefTask{
			{ID: "t1", Instruction: "summarise doc", Status: "completed", RequiredRole: "executor", CreatedAt: time.Now()},
			{ID: "t2", Instruction: "translate text", Status: "running", RequiredRole: "executor", AssignedClient: "c1", CreatedAt: time.Now()},
			{ID: "t3", Instruction: "analyse data", Status: "queued", RequiredRole: "analyst", CreatedAt: time.Now()},
			{ID: "t4", Instruction: "sub-task A", Status: "completed", ParentTaskID: "t1", CreatedAt: time.Now()},
		},
		clients: []ReefClient{
			{ID: "c1", Role: "executor", Skills: []string{"python", "bash"}, State: "connected", Capacity: 10, CurrentLoad: 1},
			{ID: "c2", Role: "analyst", Skills: []string{"sql", "pandas"}, State: "stale", Capacity: 5, CurrentLoad: 0},
		},
	}
	SetReefProvider(mp)
	return h, mp
}

func TestReefStatus_OK(t *testing.T) {
	h, _ := setupReefTest()
	req := httptest.NewRequest(http.MethodGet, "/api/reef/status", nil)
	w := httptest.NewRecorder()
	h.handleReefStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, expected 200", w.Code)
	}
	var status ReefStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.CompletedTasks != 42 {
		t.Errorf("completed = %d, expected 42", status.CompletedTasks)
	}
}

func TestReefTasks_All(t *testing.T) {
	h, _ := setupReefTest()
	req := httptest.NewRequest(http.MethodGet, "/api/reef/tasks", nil)
	w := httptest.NewRecorder()
	h.handleReefTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	tasks, ok := resp["tasks"].([]interface{})
	if !ok || len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %v", resp)
	}
}

func TestReefTasks_FilterStatus(t *testing.T) {
	h, _ := setupReefTest()
	req := httptest.NewRequest(http.MethodGet, "/api/reef/tasks?status=running", nil)
	w := httptest.NewRecorder()
	h.handleReefTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	tasks := resp["tasks"].([]interface{})
	if len(tasks) != 1 {
		t.Fatalf("expected 1 running task, got %d", len(tasks))
	}
}

func TestReefTask_ByID(t *testing.T) {
	h, _ := setupReefTest()
	req := httptest.NewRequest(http.MethodGet, "/api/reef/tasks/t1", nil)
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	h.handleReefTask(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var task ReefTask
	json.NewDecoder(w.Body).Decode(&task)
	if task.ID != "t1" {
		t.Errorf("id = %s", task.ID)
	}
}

func TestReefSubTasks(t *testing.T) {
	h, _ := setupReefTest()
	req := httptest.NewRequest(http.MethodGet, "/api/reef/tasks/t1/subtasks", nil)
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	h.handleReefSubTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	tasks := resp["tasks"].([]interface{})
	if len(tasks) != 1 {
		t.Fatalf("expected 1 sub-task, got %d", len(tasks))
		task := tasks[0].(map[string]interface{})
		if task["id"] != "t4" {
			t.Errorf("unexpected task %v", task)
		}
	}
}

func TestReefClients(t *testing.T) {
	h, _ := setupReefTest()
	req := httptest.NewRequest(http.MethodGet, "/api/reef/clients", nil)
	w := httptest.NewRecorder()
	h.handleReefClients(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	clients := resp["clients"].([]interface{})
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(clients))
	}
}

func TestReefEvents_SSE(t *testing.T) {
	h, _ := setupReefTest()
	req := httptest.NewRequest(http.MethodGet, "/api/reef/events", nil)
	// Use a context with cancel so we can stop the stream
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	// Run in goroutine, cancel after getting first data
	done := make(chan struct{})
	go func() {
		h.handleReefEvents(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if !strings.Contains(w.Body.String(), "event: connected") {
		t.Errorf("expected SSE connected event, got: %s", w.Body.String())
	}
}
