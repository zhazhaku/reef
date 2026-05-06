package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

// ---------------------------------------------------------------------------
// Mock implementations for extended APIs
// ---------------------------------------------------------------------------

// mockClientController implements ClientController interface.
type mockClientController struct {
	mu  sync.RWMutex
	err error
}

func (m *mockClientController) Pause(id string) error   { return m.err }
func (m *mockClientController) Resume(id string) error  { return m.err }
func (m *mockClientController) Restart(id string) error { return m.err }

// mockBoardMover implements TaskBoardMover interface.
type mockBoardMover struct {
	mu      sync.RWMutex
	moved   map[string]string // taskID → newStatus
	moveErr error
}

func (m *mockBoardMover) MoveTask(taskID string, newStatus reef.TaskStatus) error {
	if m.moveErr != nil {
		return m.moveErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.moved == nil {
		m.moved = make(map[string]string)
	}
	m.moved[taskID] = string(newStatus)
	return nil
}

// mockEvolutionHub implements EvolutionHub interface.
type mockEvolutionHub struct {
	genes      []GeneResponse
	strategy   EvolutionStrategyResponse
	capsules   []CapsuleResponse
	approved   []string
	rejected   []string
	approveErr error
	rejectErr  error
}

func (m *mockEvolutionHub) Genes() []GeneResponse           { return m.genes }
func (m *mockEvolutionHub) ApproveGene(id string) error    { m.approved = append(m.approved, id); return m.approveErr }
func (m *mockEvolutionHub) RejectGene(id string, _ string) error { m.rejected = append(m.rejected, id); return m.rejectErr }
func (m *mockEvolutionHub) Strategy() EvolutionStrategyResponse      { return m.strategy }
func (m *mockEvolutionHub) SetStrategy(s string) error {
	m.strategy.Strategy = s
	return nil
}
func (m *mockEvolutionHub) Capsules() []CapsuleResponse { return m.capsules }

func newMockEvolutionHub() *mockEvolutionHub {
	return &mockEvolutionHub{
		genes: []GeneResponse{
			{ID: "gene-001", Role: "coder", ControlSignal: 0.85, Status: "approved"},
			{ID: "gene-002", Role: "tester", ControlSignal: 0.72, Status: "submitted"},
		},
		strategy: EvolutionStrategyResponse{Strategy: "balanced", Innovate: 50, Optimize: 30, Repair: 20},
		capsules: []CapsuleResponse{
			{ID: "caps-001", Name: "deploy-automation", Role: "coder", SkillCount: 5, Rating: 4.8},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests: Client Detail API (Wave 1.1)
// ---------------------------------------------------------------------------

func TestV2ClientDetail_Exists(t *testing.T) {
	h, reg, _ := newTestSetup()
	mux := newTestMux(h)

	now := time.Now()
	reg.add(&reef.ClientInfo{
		ID:            "c1",
		Role:          "coder",
		Skills:        []string{"go", "python"},
		Providers:     []string{"openai"},
		Capacity:      5,
		CurrentLoad:   2,
		State:         reef.ClientConnected,
		LastHeartbeat: now,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/client/c1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp V2ClientDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ID != "c1" {
		t.Errorf("id = %s, want c1", resp.ID)
	}
	if resp.Role != "coder" {
		t.Errorf("role = %s, want coder", resp.Role)
	}
	if len(resp.Skills) != 2 || resp.Skills[0] != "go" {
		t.Errorf("skills = %v, want [go, python]", resp.Skills)
	}
	if resp.Capacity != 5 {
		t.Errorf("capacity = %d, want 5", resp.Capacity)
	}
	if resp.CurrentLoad != 2 {
		t.Errorf("current_load = %d, want 2", resp.CurrentLoad)
	}
	if resp.State != "connected" {
		t.Errorf("state = %s, want connected", resp.State)
	}
}

func TestV2ClientDetail_NotFound(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/client/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestV2ClientDetail_WithCurrentTask(t *testing.T) {
	h, reg, sch := newTestSetup()
	mux := newTestMux(h)

	now := time.Now()
	reg.add(&reef.ClientInfo{ID: "c1", Role: "coder", Capacity: 3, CurrentLoad: 1, State: reef.ClientConnected, LastHeartbeat: now})

	task := reef.NewTask("t-42", "fix bug", "coder", nil)
	task.Status = reef.TaskRunning
	task.AssignedClient = "c1"
	task.StartedAt = &now
	sch.add(task)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/client/c1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp V2ClientDetailResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.CurrentTaskID != "t-42" {
		t.Errorf("current_task_id = %s, want t-42", resp.CurrentTaskID)
	}
}

// ---------------------------------------------------------------------------
// Tests: Board API (Wave 1.2)
// ---------------------------------------------------------------------------

func TestV2Board_Empty(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/board", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp V2BoardResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if len(resp.Backlog) != 0 || len(resp.InProgress) != 0 || len(resp.Review) != 0 || len(resp.Done) != 0 {
		t.Errorf("expected empty board, got %+v", resp)
	}
}

func TestV2Board_WithTasks(t *testing.T) {
	h, _, sch := newTestSetup()
	mux := newTestMux(h)

	t1 := reef.NewTask("t-queued", "in queue", "coder", nil)
	t1.Status = reef.TaskQueued
	sch.add(t1)

	t2 := reef.NewTask("t-running", "in progress", "coder", nil)
	t2.Status = reef.TaskRunning
	t2.AssignedClient = "c1"
	sch.add(t2)

	t3 := reef.NewTask("t-done", "completed", "coder", nil)
	t3.Status = reef.TaskCompleted
	sch.add(t3)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/board", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp V2BoardResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if len(resp.Backlog) != 1 {
		t.Errorf("backlog len = %d, want 1", len(resp.Backlog))
	}
	if len(resp.InProgress) != 1 {
		t.Errorf("in_progress len = %d, want 1", len(resp.InProgress))
	}
	if len(resp.Done) != 1 {
		t.Errorf("done len = %d, want 1", len(resp.Done))
	}
}

func TestV2BoardMove_Fallback(t *testing.T) {
	h, _, sch := newTestSetup()
	mux := newTestMux(h)

	task := reef.NewTask("t1", "task", "coder", nil)
	task.Status = reef.TaskQueued
	sch.add(task)

	body := `{"task_id":"t1","new_status":"running"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/board/move", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests: Chatroom API (Wave 1.3)
// ---------------------------------------------------------------------------

func TestV2ChatroomMessages_Empty(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/chatroom/t-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)

	msgs := resp["messages"].([]any)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestV2ChatroomSend(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	body := `{"sender":"test-user","content":"hello world"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/chatroom/t-1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var msg ChatMessage
	json.Unmarshal(rec.Body.Bytes(), &msg)

	if msg.TaskID != "t-1" {
		t.Errorf("task_id = %s, want t-1", msg.TaskID)
	}
	if msg.Sender != "test-user" {
		t.Errorf("sender = %s, want test-user", msg.Sender)
	}
	if msg.Content != "hello world" {
		t.Errorf("content = %s, want hello world", msg.Content)
	}
	if msg.SenderType != "user" {
		t.Errorf("sender_type = %s, want user", msg.SenderType)
	}
	if msg.ContentType != "text" {
		t.Errorf("content_type = %s, want text", msg.ContentType)
	}

	// Verify message stored in chatstore
	msgs := h.chatStore.Messages("t-1")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in store, got %d", len(msgs))
	}
	if msgs[0].Content != "hello world" {
		t.Errorf("stored content = %s, want hello world", msgs[0].Content)
	}
}

func TestV2ChatroomMessages_WithData(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	for i := 0; i < 3; i++ {
		body := fmt.Sprintf(`{"content":"msg-%d"}`, i)
		req := httptest.NewRequest(http.MethodPost, "/api/v2/chatroom/t-1/send", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("send failed at i=%d: %d", i, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v2/chatroom/t-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)

	msgs := resp["messages"].([]any)
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
}

func TestV2Chatroom_MissingContent(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	body := `{"sender":"test","content":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/chatroom/t-1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty content, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests: Task Decomposition API (Wave 1.4)
// ---------------------------------------------------------------------------

func TestV2TaskDecompose_NotFound(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/tasks/nonexist/decompose", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestV2TaskDecompose_Empty(t *testing.T) {
	h, _, sch := newTestSetup()
	mux := newTestMux(h)

	task := reef.NewTask("t-1", "task", "coder", nil)
	sch.add(task)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/tasks/t-1/decompose", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp V2DecomposeResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.TaskID != "t-1" {
		t.Errorf("task_id = %s, want t-1", resp.TaskID)
	}
	if len(resp.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(resp.Nodes))
	}
}

func TestV2TaskDecompose_Create(t *testing.T) {
	h, _, sch := newTestSetup()
	mux := newTestMux(h)

	task := reef.NewTask("t-1", "task", "coder", nil)
	sch.add(task)

	body := `{"nodes":[{"id":"st-1","instruction":"sub task 1","status":"queued"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v2/tasks/t-1/decompose", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp V2DecomposeResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if len(resp.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(resp.Nodes))
	}
	if resp.Nodes[0].Instruction != "sub task 1" {
		t.Errorf("node instruction = %s, want 'sub task 1'", resp.Nodes[0].Instruction)
	}
}

// ---------------------------------------------------------------------------
// Tests: Evolution API (Wave 1.5)
// ---------------------------------------------------------------------------

func TestV2EvolutionHubNotSet(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	t.Run("Genes list returns empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/evolution/genes", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var genes []GeneResponse
		json.Unmarshal(rec.Body.Bytes(), &genes)
		if len(genes) != 0 {
			t.Errorf("expected 0 genes without hub, got %d", len(genes))
		}
	})

	t.Run("Gene action returns 503", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/evolution/genes/g-1/approve", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rec.Code)
		}
	})

	t.Run("Strategy returns 503", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/evolution/strategy", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rec.Code)
		}
	})
}

func TestV2EvolutionAPIs(t *testing.T) {
	h, _, _ := newTestSetup()
	hub := newMockEvolutionHub()
	h.SetEvolutionHub(hub)
	mux := newTestMux(h)

	t.Run("List genes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/evolution/genes", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var genes []GeneResponse
		json.Unmarshal(rec.Body.Bytes(), &genes)
		if len(genes) != 2 {
			t.Errorf("expected 2 genes, got %d", len(genes))
		}
	})

	t.Run("Approve gene", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/evolution/genes/g-002/approve", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if len(hub.approved) != 1 || hub.approved[0] != "g-002" {
			t.Errorf("expected gene g-002 approved, got %v", hub.approved)
		}
	})

	t.Run("Reject gene", func(t *testing.T) {
		body := `{"reason":"insufficient signal"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v2/evolution/genes/g-003/reject", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if len(hub.rejected) != 1 || hub.rejected[0] != "g-003" {
			t.Errorf("expected gene g-003 rejected, got %v", hub.rejected)
		}
	})

	t.Run("Get strategy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/evolution/strategy", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var strat EvolutionStrategyResponse
		json.Unmarshal(rec.Body.Bytes(), &strat)
		if strat.Strategy != "balanced" {
			t.Errorf("strategy = %s, want balanced", strat.Strategy)
		}
	})

	t.Run("Update strategy", func(t *testing.T) {
		body := `{"strategy":"innovate"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v2/evolution/strategy", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if hub.strategy.Strategy != "innovate" {
			t.Errorf("strategy was not updated, got %s", hub.strategy.Strategy)
		}
	})

	t.Run("List capsules", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/evolution/capsules", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var caps []CapsuleResponse
		json.Unmarshal(rec.Body.Bytes(), &caps)
		if len(caps) != 1 {
			t.Errorf("expected 1 capsule, got %d", len(caps))
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: Config API (Wave 1.6)
// ---------------------------------------------------------------------------

func TestV2Config_GetEmpty(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var cfg map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cfg)
	if len(cfg) != 0 {
		t.Errorf("expected empty config, got %v", cfg)
	}
}

func TestV2Config_PutAndGet(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	body := `{"store_type":"sqlite","store_path":"/data/reef.db"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("put expected 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/config", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var cfg map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cfg)
	if cfg["store_type"] != "sqlite" {
		t.Errorf("store_type = %v, want sqlite", cfg["store_type"])
	}
	if cfg["store_path"] != "/data/reef.db" {
		t.Errorf("store_path = %v, want /data/reef.db", cfg["store_path"])
	}
}

// ---------------------------------------------------------------------------
// Tests: Hermes API (Wave 1.6)
// ---------------------------------------------------------------------------

func TestV2Hermes_GetDefault(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/hermes", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var cfg map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cfg)
	if cfg["mode"] != "standard" {
		t.Errorf("mode = %v, want standard", cfg["mode"])
	}
}

func TestV2Hermes_PutAndGet(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	body := `{"mode":"coordinator","fallback_enabled":true,"fallback_timeout":30000}`
	req := httptest.NewRequest(http.MethodPut, "/api/v2/hermes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("put expected 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/hermes", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var cfg map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cfg)
	if cfg["mode"] != "coordinator" {
		t.Errorf("mode = %v, want coordinator", cfg["mode"])
	}
	if cfg["fallback_enabled"] != true {
		t.Errorf("fallback_enabled = %v, want true", cfg["fallback_enabled"])
	}
}

// ---------------------------------------------------------------------------
// Tests: Activity API (Wave 1.6)
// ---------------------------------------------------------------------------

func TestV2Activity_Empty(t *testing.T) {
	h, _, _ := newTestSetup()
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/activity", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	events := resp["events"].([]any)
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestV2Activity_WithEvents(t *testing.T) {
	h, _, _ := newTestSetup()
	ensureStores()

	store := GetActivityStore()
	store.Add(ActivityEvent{ID: "e1", Type: "agent", Actor: "c1", Description: "came online", Timestamp: time.Now().UnixMilli()})
	store.Add(ActivityEvent{ID: "e2", Type: "task", Actor: "c1", Description: "task started", Timestamp: time.Now().UnixMilli()})
	store.Add(ActivityEvent{ID: "e3", Type: "evolution", Actor: "system", Description: "gene activated", Timestamp: time.Now().UnixMilli()})

	mux := newTestMux(h)

	t.Run("List all", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/activity", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		var resp map[string]any
		json.Unmarshal(rec.Body.Bytes(), &resp)
		events := resp["events"].([]any)
		if len(events) < 3 {
			t.Errorf("expected at least 3 events, got %d", len(events))
		}
		if resp["total"].(float64) < 3 {
			t.Errorf("total < 3, got %v", resp["total"])
		}
	})

	t.Run("Filter by type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/activity?type=task", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		var resp map[string]any
		json.Unmarshal(rec.Body.Bytes(), &resp)
		events := resp["events"].([]any)
		if len(events) < 1 {
			t.Errorf("expected at least 1 task event, got %d", len(events))
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: ActivityStore unit
// ---------------------------------------------------------------------------

func TestActivityStore_CRUD(t *testing.T) {
	store := NewActivityStore(5)

	for i := 0; i < 3; i++ {
		store.Add(ActivityEvent{ID: fmt.Sprintf("e%d", i), Type: "test", Description: fmt.Sprintf("event %d", i), Timestamp: time.Now().UnixMilli()})
	}

	events := store.Query("", 10)
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}

	events = store.Query("nonexistent", 10)
	if len(events) != 0 {
		t.Errorf("expected 0 events for nonexistent type, got %d", len(events))
	}

	events = store.Query("", 2)
	if len(events) != 2 {
		t.Errorf("expected 2 events with limit=2, got %d", len(events))
	}
}

func TestActivityStore_RingBuffer(t *testing.T) {
	store := NewActivityStore(3)

	for i := 0; i < 5; i++ {
		store.Add(ActivityEvent{ID: fmt.Sprintf("e%d", i), Type: "test", Timestamp: time.Now().UnixMilli()})
	}

	events := store.Query("", 10)
	if len(events) != 3 {
		t.Errorf("expected 3 events (cap=3), got %d", len(events))
	}
	if events[0].ID != "e4" {
		t.Errorf("newest event should be e4, got %s", events[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Tests: MemoryChatStore unit
// ---------------------------------------------------------------------------

func TestMemoryChatStore_SendAndGet(t *testing.T) {
	store := NewMemoryChatStore()

	msgs := store.Messages("t-1")
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for empty store, got %d", len(msgs))
	}

	store.Send("t-1", ChatMessage{ID: "m1", TaskID: "t-1", Content: "hello", SenderType: "user"})
	store.Send("t-1", ChatMessage{ID: "m2", TaskID: "t-1", Content: "world", SenderType: "user"})

	msgs = store.Messages("t-1")
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != "m1" || msgs[1].ID != "m2" {
		t.Errorf("messages out of order: %v", msgs)
	}

	msgs = store.Messages("other-task")
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for other task, got %d", len(msgs))
	}
}

func TestMemoryChatStore_CopySemantics(t *testing.T) {
	store := NewMemoryChatStore()
	store.Send("t-1", ChatMessage{ID: "m1", TaskID: "t-1", Content: "original", SenderType: "user"})

	msgs := store.Messages("t-1")
	msgs[0].Content = "modified"

	msgs2 := store.Messages("t-1")
	if msgs2[0].Content != "original" {
		t.Errorf("copy semantics broken: got %s, want original", msgs2[0].Content)
	}
}

// ---------------------------------------------------------------------------
// Integration test: multi-step workflow
// ---------------------------------------------------------------------------

func TestIntegration_Workflow(t *testing.T) {
	h, reg, sch := newTestSetup()
	mux := newTestMux(h)

	now := time.Now()

	reg.add(&reef.ClientInfo{ID: "c1", Role: "coder", Skills: []string{"go"},
		Capacity: 5, CurrentLoad: 1, State: reef.ClientConnected, LastHeartbeat: now})

	task := reef.NewTask("t-1", "implement feature", "coder", []string{"go"})
	task.Status = reef.TaskQueued
	sch.add(task)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var statusResp V2StatusResponse
	json.Unmarshal(rec.Body.Bytes(), &statusResp)
	if statusResp.ClientCount != 1 {
		t.Errorf("expected 1 client, got %d", statusResp.ClientCount)
	}
	if statusResp.TaskStats.Queued != 1 {
		t.Errorf("expected 1 queued, got %d", statusResp.TaskStats.Queued)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/client/c1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var clientResp V2ClientDetailResponse
	json.Unmarshal(rec.Body.Bytes(), &clientResp)
	if clientResp.ID != "c1" || clientResp.Role != "coder" {
		t.Errorf("unexpected client detail: %+v", clientResp)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/board", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var boardResp V2BoardResponse
	json.Unmarshal(rec.Body.Bytes(), &boardResp)
	if len(boardResp.Backlog) != 1 {
		t.Errorf("expected 1 backlog task, got %d", len(boardResp.Backlog))
	}

	body := `{"content":"starting work"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v2/chatroom/t-1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("chat send expected 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/chatroom/t-1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var chatResp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &chatResp)
	msgs := chatResp["messages"].([]any)
	if len(msgs) < 1 {
		t.Errorf("expected at least 1 chat message, got %d", len(msgs))
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/tasks/t-1/decompose", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("decompose expected 200, got %d", rec.Code)
	}
}
