package ui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

// ---------------------------------------------------------------------------
// Mock implementations for testing
// ---------------------------------------------------------------------------

type mockRegistry struct {
	mu      sync.RWMutex
	clients []*reef.ClientInfo
}

func (m *mockRegistry) List() []*reef.ClientInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*reef.ClientInfo, len(m.clients))
	copy(out, m.clients)
	return out
}

func (m *mockRegistry) add(c *reef.ClientInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients = append(m.clients, c)
}

type mockScheduler struct {
	mu    sync.RWMutex
	tasks []*reef.Task
}

func (m *mockScheduler) TasksSnapshot() []*reef.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*reef.Task, len(m.tasks))
	copy(out, m.tasks)
	return out
}

func (m *mockScheduler) GetTask(id string) *reef.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (m *mockScheduler) add(t *reef.Task) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = append(m.tasks, t)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestSetup() (*Handler, *mockRegistry, *mockScheduler) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	reg := &mockRegistry{}
	sch := &mockScheduler{}
	startTime := time.Now().Add(-5 * time.Minute)
	handler := NewHandler(reg, sch, startTime, logger)
	return handler, reg, sch
}

func newTestMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestUI_Redirect(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("expected 301, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/ui/" {
		t.Errorf("expected redirect to /ui/, got %s", loc)
	}
}

func TestUI_StaticHTML(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected HTML content type, got %s", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Reef Dashboard") {
		t.Error("expected HTML to contain 'Reef Dashboard'")
	}
}

func TestV2Status_Empty(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp V2StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ServerVersion != reef.ProtocolVersion {
		t.Errorf("server_version = %s, want %s", resp.ServerVersion, reef.ProtocolVersion)
	}
	if resp.ClientCount != 0 {
		t.Errorf("connected_clients = %d, want 0", resp.ClientCount)
	}
	if resp.UptimeMs < 0 {
		t.Errorf("uptime_ms = %d, want >= 0", resp.UptimeMs)
	}
	if resp.TaskStats.Queued != 0 || resp.TaskStats.Running != 0 {
		t.Errorf("expected empty task stats, got %+v", resp.TaskStats)
	}
}

func TestV2Status_WithClientsAndTasks(t *testing.T) {
	h, reg, sch := newTestSetup()
	mux := newTestMux(h)

	// Register clients
	reg.add(&reef.ClientInfo{
		ID:            "c1",
		Role:          "coder",
		Skills:        []string{"go"},
		Capacity:      2,
		CurrentLoad:   0,
		State:         reef.ClientConnected,
		LastHeartbeat: time.Now(),
	})
	reg.add(&reef.ClientInfo{
		ID:          "c2",
		Role:        "reviewer",
		Capacity:    1,
		CurrentLoad: 0,
		State:       reef.ClientDisconnected,
	})

	// Add tasks in various states
	task1 := reef.NewTask("t1", "write code", "coder", nil)
	task1.Status = reef.TaskRunning
	task1.StartedAt = timePtr(time.Now())
	sch.add(task1)

	task2 := reef.NewTask("t2", "review code", "reviewer", nil)
	task2.Status = reef.TaskQueued
	sch.add(task2)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp V2StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ClientCount != 1 {
		t.Errorf("connected_clients = %d, want 1", resp.ClientCount)
	}
	if resp.TaskStats.Running != 1 {
		t.Errorf("running = %d, want 1", resp.TaskStats.Running)
	}
	if resp.TaskStats.Queued != 1 {
		t.Errorf("queued = %d, want 1", resp.TaskStats.Queued)
	}
}

func TestV2Tasks_Pagination(t *testing.T) {
	h, _, sch := newTestSetup()
	mux := newTestMux(h)

	// Add 10 tasks
	for i := 0; i < 10; i++ {
		task := reef.NewTask("task-"+string(rune('a'+i)), "instruction", "coder", nil)
		task.Status = reef.TaskQueued
		sch.add(task)
	}

	// Get first page
	req := httptest.NewRequest(http.MethodGet, "/api/v2/tasks?limit=3&offset=0", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp V2TasksResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 10 {
		t.Errorf("total = %d, want 10", resp.Total)
	}
	if resp.Limit != 3 {
		t.Errorf("limit = %d, want 3", resp.Limit)
	}
	if resp.Offset != 0 {
		t.Errorf("offset = %d, want 0", resp.Offset)
	}
	if len(resp.Tasks) != 3 {
		t.Errorf("tasks len = %d, want 3", len(resp.Tasks))
	}

	// Get second page
	req = httptest.NewRequest(http.MethodGet, "/api/v2/tasks?limit=3&offset=3", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp2 V2TasksResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp2.Offset != 3 {
		t.Errorf("offset = %d, want 3", resp2.Offset)
	}
	if len(resp2.Tasks) != 3 {
		t.Errorf("tasks len = %d, want 3", len(resp2.Tasks))
	}
}

func TestV2Tasks_FilterByStatus(t *testing.T) {
	h, _, sch := newTestSetup()
	mux := newTestMux(h)

	task1 := reef.NewTask("t1", "task 1", "coder", nil)
	task1.Status = reef.TaskRunning
	task1.StartedAt = timePtr(time.Now())
	sch.add(task1)

	task2 := reef.NewTask("t2", "task 2", "coder", nil)
	task2.Status = reef.TaskQueued
	sch.add(task2)

	task3 := reef.NewTask("t3", "task 3", "reviewer", nil)
	task3.Status = reef.TaskQueued
	sch.add(task3)

	// Filter by Running
	req := httptest.NewRequest(http.MethodGet, "/api/v2/tasks?status=Running", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp V2TasksResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("total = %d, want 1 for Running filter", resp.Total)
	}
	for _, task := range resp.Tasks {
		if task.Status != "Running" {
			t.Errorf("expected Running status, got %s", task.Status)
		}
	}

	// Filter by role
	req = httptest.NewRequest(http.MethodGet, "/api/v2/tasks?role=reviewer", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("total = %d, want 1 for reviewer filter", resp.Total)
	}
}

func TestV2Tasks_MethodNotAllowed(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/tasks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestV2Clients_Empty(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/clients", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var clients []V2ClientResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &clients); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(clients))
	}
}

func TestV2Clients_WithData(t *testing.T) {
	h, reg, _ := newTestSetup()
	mux := newTestMux(h)

	reg.add(&reef.ClientInfo{
		ID:            "c1",
		Role:          "coder",
		Skills:        []string{"go", "python"},
		Capacity:      3,
		CurrentLoad:   1,
		State:         reef.ClientConnected,
		LastHeartbeat: time.Now(),
	})
	reg.add(&reef.ClientInfo{
		ID:            "c2",
		Role:          "reviewer",
		Skills:        []string{"go"},
		Capacity:      2,
		CurrentLoad:   0,
		State:         reef.ClientStale,
		LastHeartbeat: time.Now().Add(-1 * time.Minute),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/clients", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var clients []V2ClientResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &clients); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(clients))
	}

	// Find c1
	var c1 *V2ClientResponse
	for i := range clients {
		if clients[i].ID == "c1" {
			c1 = &clients[i]
			break
		}
	}
	if c1 == nil {
		t.Fatal("c1 not found")
	}
	if c1.Role != "coder" {
		t.Errorf("role = %s, want coder", c1.Role)
	}
	if c1.State != "connected" {
		t.Errorf("state = %s, want connected", c1.State)
	}
	if c1.CurrentLoad != 1 {
		t.Errorf("load = %d, want 1", c1.CurrentLoad)
	}
	if len(c1.Skills) != 2 {
		t.Errorf("skills len = %d, want 2", len(c1.Skills))
	}
}

func TestV2Events_SSEConnection(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/events", nil)
	rec := httptest.NewRecorder()

	// SSE blocks, so we run in a goroutine with a timeout
	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(rec, req)
	}()

	// Give the handler time to write headers and initial event
	time.Sleep(200 * time.Millisecond)

	// Check headers were set
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("content-type = %s, want text/event-stream", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("cache-control = %s, want no-cache", rec.Header().Get("Cache-Control"))
	}

	// The body should contain at least the initial stats_update event
	body := rec.Body.String()
	if !strings.Contains(body, "event: stats_update") {
		t.Error("expected initial stats_update event in SSE stream")
	}
	if !strings.Contains(body, "server_version") {
		t.Error("expected server_version in stats event data")
	}
}

func TestEventBus_PubSub(t *testing.T) {
	bus := NewEventBus()

	ch1 := bus.Subscribe()
	ch2 := bus.Subscribe()

	// Publish an event
	bus.Publish(Event{Type: "test", Data: "hello"})

	// Both should receive
	select {
	case e := <-ch1:
		if e.Type != "test" {
			t.Errorf("ch1 type = %s, want test", e.Type)
		}
	case <-time.After(time.Second):
		t.Error("ch1 did not receive event")
	}

	select {
	case e := <-ch2:
		if e.Type != "test" {
			t.Errorf("ch2 type = %s, want test", e.Type)
		}
	case <-time.After(time.Second):
		t.Error("ch2 did not receive event")
	}

	// Unsubscribe ch1
	bus.Unsubscribe(ch1)

	// Publish again
	bus.Publish(Event{Type: "test2", Data: "world"})

	// ch1 should not receive (closed)
	_, ok := <-ch1
	if ok {
		t.Error("ch1 should be closed after unsubscribe")
	}

	// ch2 should receive
	select {
	case e := <-ch2:
		if e.Type != "test2" {
			t.Errorf("ch2 type = %s, want test2", e.Type)
		}
	case <-time.After(time.Second):
		t.Error("ch2 did not receive event after unsubscribe of ch1")
	}

	bus.Unsubscribe(ch2)
}

func TestEventBus_NonBlocking(t *testing.T) {
	bus := NewEventBus()

	// Subscribe but never read — buffer should fill and drops should happen gracefully
	ch := bus.Subscribe()

	// Publish many events — should not block
	for i := 0; i < 100; i++ {
		bus.Publish(Event{Type: "flood", Data: i})
	}

	// Drain
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count == 0 {
		t.Error("expected at least some events to be buffered")
	}
	if count >= 100 {
		t.Error("expected some events to be dropped due to slow consumer")
	}

	bus.Unsubscribe(ch)
}

func TestV2Status_MethodNotAllowed(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestV2Clients_MethodNotAllowed(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/clients", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestPublishTaskUpdate(t *testing.T) {
	h, _, _ := newTestSetup()
	bus := h.EventBus()
	ch := bus.Subscribe()

	task := reef.NewTask("t1", "test", "coder", nil)
	task.Status = reef.TaskRunning
	h.PublishTaskUpdate(task)

	select {
	case e := <-ch:
		if e.Type != "task_update" {
			t.Errorf("event type = %s, want task_update", e.Type)
		}
	case <-time.After(time.Second):
		t.Error("did not receive task_update event")
	}

	bus.Unsubscribe(ch)
}

func TestPublishClientUpdate(t *testing.T) {
	h, _, _ := newTestSetup()
	bus := h.EventBus()
	ch := bus.Subscribe()

	client := &reef.ClientInfo{
		ID:            "c1",
		Role:          "coder",
		Skills:        []string{"go"},
		State:         reef.ClientConnected,
		LastHeartbeat: time.Now(),
	}
	h.PublishClientUpdate(client)

	select {
	case e := <-ch:
		if e.Type != "client_update" {
			t.Errorf("event type = %s, want client_update", e.Type)
		}
	case <-time.After(time.Second):
		t.Error("did not receive client_update event")
	}

	bus.Unsubscribe(ch)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func timePtr(t time.Time) *time.Time {
	return &t
}
