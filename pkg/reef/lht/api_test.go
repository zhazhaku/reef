// +build ignore

package lht

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────────
// test helpers
// ────────────────────────────────────────────────────────────

type testNotifier struct {
	mu      sync.Mutex
	lastMsg string
	msgs    []string
}

func (n *testNotifier) Send(channel, chatID, text string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastMsg = text
	n.msgs = append(n.msgs, text)
}

func setupTestAPI(t *testing.T) (*APIHandler, *Store, *Engine, func()) {
	t.Helper()

	basePath, err := os.MkdirTemp("", "lht-api-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	cleanup := func() { os.RemoveAll(basePath) }

	store := NewStore(basePath)
	cfg := DefaultConfig()
	notifier := &testNotifier{}
	engine := NewEngine(cfg, store, notifier)

	api := NewAPIHandler(engine, store)
	return api, store, engine, cleanup
}

// createStoreOnlyGoal creates a goal in the store without starting an engine task.
// Used for list/detail tests that don't need engine Dispatch.
func createStoreOnlyGoal(t *testing.T, store *Store, goalID, title string) {
	t.Helper()
	g := Goal{
		GoalID:      goalID,
		Description: title,
		State:       StateGrounding,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.EnsureTaskDirs(goalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}
	if err := store.SaveGoal(&g); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}
	if err := store.SaveState(goalID, &g); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	plan := &Plan{
		GoalID:      goalID,
		PlanVersion: "1.0",
		Tasks: []TaskNode{
			{TaskID: "t-001", Description: "subtask 1", State: StateGrounding},
			{TaskID: "t-002", Description: "subtask 2", State: StateGrounding},
		},
	}
	_ = store.SavePlan(goalID, plan)
	_ = store.WriteLogEntry(goalID, "exec.log", []byte("[INFO] started\n[INFO] processing\n"))
}

// createEngineGoal creates a running engine task via NewGoal and returns its goalID.
func createEngineGoal(t *testing.T, engine *Engine, title string) string {
	t.Helper()
	goal, err := engine.NewGoal(title, "code", "test-channel", "test-chat")
	if err != nil {
		t.Fatalf("NewGoal: %v", err)
	}
	// Give the goroutine a moment to start.
	time.Sleep(50 * time.Millisecond)
	return goal.GoalID
}

func makeRequest(method, path string, body interface{}) (*httptest.ResponseRecorder, *http.Request) {
	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			panic(fmt.Sprintf("marshal body: %v", err))
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	return w, req
}

// ────────────────────────────────────────────────────────────
// T5A.1.1 — GET /api/v2/lht/list 返回任务列表
// ────────────────────────────────────────────────────────────

func TestAPIListTasks(t *testing.T) {
	api, store, _, cleanup := setupTestAPI(t)
	defer cleanup()

	createStoreOnlyGoal(t, store, "g-001", "build web app")
	createStoreOnlyGoal(t, store, "g-002", "research ML")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("GET", "/api/v2/lht/list", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	tasks, ok := response["tasks"].([]interface{})
	if !ok {
		t.Fatalf("response.tasks is not an array: %v", response)
	}
	if len(tasks) < 2 {
		t.Errorf("expected at least 2 tasks, got %d", len(tasks))
	}
}

// T5A.1.2 — GET /api/v2/lht/<id> 返回任务详情
func TestAPIHandlers(t *testing.T) {
	api, store, _, cleanup := setupTestAPI(t)
	defer cleanup()

	createStoreOnlyGoal(t, store, "g-001", "build web app")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("GET", "/api/v2/lht/g-001", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var detail map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}

	goalMap, ok := detail["goal"].(map[string]interface{})
	if !ok {
		t.Fatalf("detail.goal missing: %v", detail)
	}
	if goalMap["goal_id"] != "g-001" {
		t.Errorf("goal_id = %v, want g-001", goalMap["goal_id"])
	}
	if _, ok := detail["plan"]; !ok {
		t.Error("detail.plan missing")
	}
}

// T5A.1.3 — GET /api/v2/lht/<id> 非法 ID 返回 404
func TestAPIDetailNotFound(t *testing.T) {
	api, _, _, cleanup := setupTestAPI(t)
	defer cleanup()

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("GET", "/api/v2/lht/nonexistent", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// T5A.1.4 — GET /api/v2/lht/<id>/logs 分页日志
func TestAPILogsPagination(t *testing.T) {
	api, store, _, cleanup := setupTestAPI(t)
	defer cleanup()

	createStoreOnlyGoal(t, store, "g-001", "build web app")
	for i := 1; i <= 5; i++ {
		_ = store.WriteLogEntry("g-001", fmt.Sprintf("step%d.log", i),
			[]byte(fmt.Sprintf("log entry %d", i)))
	}

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	t.Run("page1_size2", func(t *testing.T) {
		w, req := makeRequest("GET", "/api/v2/lht/g-001/logs?page=1&size=2", nil)
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		entries, _ := resp["entries"].([]interface{})
		if len(entries) != 2 {
			t.Errorf("page 1: expected 2 entries, got %d", len(entries))
		}
	})

	t.Run("page2_size2", func(t *testing.T) {
		w, req := makeRequest("GET", "/api/v2/lht/g-001/logs?page=2&size=2", nil)
		mux.ServeHTTP(w, req)
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		entries, _ := resp["entries"].([]interface{})
		if len(entries) != 2 {
			t.Errorf("page 2: expected 2 entries, got %d", len(entries))
		}
	})

	t.Run("page_default", func(t *testing.T) {
		w, req := makeRequest("GET", "/api/v2/lht/g-001/logs", nil)
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		if _, ok := resp["entries"]; !ok {
			t.Error("response missing entries field")
		}
		currentPage, _ := resp["page"].(float64)
		if currentPage != 1 {
			t.Errorf("default page = %v, want 1", currentPage)
		}
	})
}

// T5A.1.5 — POST /api/v2/lht/<id>/pause 暂停任务
func TestAPIPause(t *testing.T) {
	api, _, engine, cleanup := setupTestAPI(t)
	defer cleanup()

	goalID := createEngineGoal(t, engine, "test pause")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("POST", "/api/v2/lht/"+goalID+"/pause", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		t.Logf("body: %s", w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
}

// T5A.1.6 — POST 非法 goalID 返回 404
func TestAPIActionNotFound(t *testing.T) {
	api, _, _, cleanup := setupTestAPI(t)
	defer cleanup()

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("POST", "/api/v2/lht/nonexistent/pause", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// T5A.1.7 — POST 非法操作返回 400
func TestAPIBadAction(t *testing.T) {
	api, _, engine, cleanup := setupTestAPI(t)
	defer cleanup()

	goalID := createEngineGoal(t, engine, "test")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("POST", "/api/v2/lht/"+goalID+"/invalidop", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// T5A.1.8 — POST /api/v2/lht/<id>/insert 插入新要求
func TestAPIInsert(t *testing.T) {
	api, _, engine, cleanup := setupTestAPI(t)
	defer cleanup()

	goalID := createEngineGoal(t, engine, "test insert")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	body := map[string]string{"requirement": "add dark mode support"}
	w, req := makeRequest("POST", "/api/v2/lht/"+goalID+"/insert", body)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// T5A.1.9 — POST /api/v2/lht/<id>/reply 回答引擎提问
func TestAPIReply(t *testing.T) {
	api, _, engine, cleanup := setupTestAPI(t)
	defer cleanup()

	goalID := createEngineGoal(t, engine, "test reply")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	body := map[string]string{
		"gate":    "ground",
		"action":  "answer",
		"content": "the target platform is web and mobile",
	}
	w, req := makeRequest("POST", "/api/v2/lht/"+goalID+"/reply", body)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ────────────────────────────────────────────────────────────
// T5A.1.10 — SSE 事件发布：lht_state
// ────────────────────────────────────────────────────────────

func TestAPIEventHookStateChange(t *testing.T) {
	api, _, engine, cleanup := setupTestAPI(t)
	defer cleanup()

	events := make(chan SSEEvent, 10)
	api.SubscribeSSE(events)

	engine.EventHook = func(eventType, goalID string, data any) {
		api.PublishSSE(SSEEvent{Type: eventType, GoalID: goalID, Data: data})
	}

	goal, err := engine.NewGoal("test-event", "code", "test-channel", "chat-1")
	if err != nil {
		t.Fatalf("NewGoal: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	select {
	case ev := <-events:
		if ev.Type != "lht_state" && ev.Type != "lht_milestone" && ev.Type != "lht_log" {
			t.Logf("unexpected event type: %s", ev.Type)
		}
		if ev.GoalID != goal.GoalID {
			t.Errorf("event GoalID = %q, want %q", ev.GoalID, goal.GoalID)
		}
	default:
		t.Log("no SSE event received (may need more time)")
	}
}

// T5A.1.11 — SSE 订阅/取消订阅
func TestAPISSESubscribeUnsubscribe(t *testing.T) {
	api, _, _, cleanup := setupTestAPI(t)
	defer cleanup()

	ch := make(chan SSEEvent, 5)
	api.SubscribeSSE(ch)

	if api.SSEClientCount() != 1 {
		t.Errorf("expected 1 subscriber, got %d", api.SSEClientCount())
	}

	api.UnsubscribeSSE(ch)

	if api.SSEClientCount() != 0 {
		t.Errorf("expected 0 subscribers after unsubscribe, got %d", api.SSEClientCount())
	}
}

// T5A.1.12 — 空列表返回 200 + 空数组
func TestAPIListEmpty(t *testing.T) {
	api, _, _, cleanup := setupTestAPI(t)
	defer cleanup()

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("GET", "/api/v2/lht/list", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	tasks, _ := resp["tasks"].([]interface{})
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

// T5A.1.13 — GET / 根路径
func TestAPIRoot(t *testing.T) {
	api, _, _, cleanup := setupTestAPI(t)
	defer cleanup()

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("GET", "/api/v2/lht/", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// T5A.1.14 — 非 GET/POST 方法返回 405
func TestAPIMethodNotAllowed(t *testing.T) {
	api, _, engine, cleanup := setupTestAPI(t)
	defer cleanup()

	goalID := createEngineGoal(t, engine, "test")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("PUT", "/api/v2/lht/"+goalID+"/pause", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// T5A.1.15 — Logs 分页越界钳制
func TestAPILogsClampPagination(t *testing.T) {
	api, store, _, cleanup := setupTestAPI(t)
	defer cleanup()

	createStoreOnlyGoal(t, store, "g-001", "test clamp")
	_ = store.WriteLogEntry("g-001", "a.log", []byte("a"))

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("GET", "/api/v2/lht/g-001/logs?page=999&size=10", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	entries, _ := resp["entries"].([]interface{})
	if len(entries) != 0 {
		t.Errorf("page 999: expected 0 entries, got %d", len(entries))
	}
}

// T5A.1.16 — 冗余斜杠路由不混淆
func TestAPITrailingSlashNormalization(t *testing.T) {
	api, _, engine, cleanup := setupTestAPI(t)
	defer cleanup()

	goalID := createEngineGoal(t, engine, "test slash")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("GET", "/api/v2/lht/"+goalID+"/", nil)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for trailing slash, got %d", w.Code)
	}

	w2, req2 := makeRequest("POST", "/api/v2/lht/"+goalID+"/pause/", nil)
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for pause with trailing slash, got %d: %s", w2.Code, w2.Body.String())
	}
}

// T5A.1.17 — Store-based list: verify goal fields
func TestAPIListFields(t *testing.T) {
	api, store, _, cleanup := setupTestAPI(t)
	defer cleanup()

	createStoreOnlyGoal(t, store, "g-list", "list fields test")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("GET", "/api/v2/lht/list", nil)
	mux.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	tasks, _ := resp["tasks"].([]interface{})
	if len(tasks) == 0 {
		t.Fatal("no tasks in list")
	}

	first, _ := tasks[0].(map[string]interface{})
	if first["goal_id"] != "g-list" {
		t.Errorf("goal_id = %v, want g-list", first["goal_id"])
	}
	if _, ok := first["state"]; !ok {
		t.Error("missing state field")
	}
	if _, ok := first["description"]; !ok {
		t.Error("missing description field")
	}
}

// T5A.1.18 — 详情页包含子任务信息
func TestAPIDetailIncludesSubtasks(t *testing.T) {
	api, store, _, cleanup := setupTestAPI(t)
	defer cleanup()

	createStoreOnlyGoal(t, store, "g-sub", "subtask test")

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	w, req := makeRequest("GET", "/api/v2/lht/g-sub", nil)
	mux.ServeHTTP(w, req)

	var detail map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &detail)

	plan, ok := detail["plan"].(map[string]interface{})
	if !ok {
		t.Fatal("detail.plan missing or not object")
	}
	subtasks, ok := plan["tasks"].([]interface{})
	if !ok || len(subtasks) < 2 {
		t.Errorf("expected at least 2 subtasks, got %v", subtasks)
	}
}
