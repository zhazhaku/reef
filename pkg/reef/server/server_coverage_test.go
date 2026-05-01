package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

// =========================================================================
// Admin: handleSubmitTask error paths (500)
// =========================================================================

// TestAdminServer_HandleSubmitTask_QueueFull triggers the Submit→500 path
// by filling the queue to capacity so Enqueue returns ErrQueueFull.
func TestAdminServer_HandleSubmitTask_QueueFull(t *testing.T) {
	reg := NewRegistry(nil)
	// Queue with maxLen=0 defaults to 1000; use maxLen=1 and fill it.
	queue := NewTaskQueue(1, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	admin := NewAdminServer(reg, sched, logger)

	// Register a client so TryDispatch doesn't return after first iteration
	// (it dequeues+dispatches, emptying the queue). We need to fill the queue
	// so that after Submit's TryDispatch runs, the queue is full.
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Skills: []string{"go"}, Capacity: 1, CurrentLoad: 0,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})

	// First submit should succeed and dispatch (emptying queue)
	body1 := SubmitTaskRequest{
		Instruction:  "first task",
		RequiredRole: "coder",
	}
	bodyBytes1, _ := json.Marshal(body1)
	req1 := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(bodyBytes1))
	w1 := httptest.NewRecorder()
	admin.handleSubmitTask(w1, req1)
	if w1.Code != http.StatusAccepted {
		t.Fatalf("first submit should succeed, got %d: %s", w1.Code, w1.Body.String())
	}

	// Fill queue: enqueue a task directly without going through Submit
	// This fills the single-slot queue.
	filler := reef.NewTask("filler", "fill", "coder", nil)
	_ = filler.Transition(reef.TaskQueued)
	if err := queue.Enqueue(filler); err != nil {
		t.Fatalf("failed to fill queue: %v", err)
	}

	// Second submit: Submit → Enqueue fails because queue is full → 500
	body2 := SubmitTaskRequest{
		Instruction:  "second task",
		RequiredRole: "coder",
	}
	bodyBytes2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(bodyBytes2))
	w2 := httptest.NewRecorder()
	admin.handleSubmitTask(w2, req2)

	if w2.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for queue full, got %d: %s", w2.Code, w2.Body.String())
	}

	// Cleanup
	taskIDMu.Lock()
	taskIDCounter = 0
	taskIDMu.Unlock()
}

// =========================================================================
// Scheduler: Submit error paths
// =========================================================================

// TestScheduler_Submit_TransitionError covers the Transition→Queued failure path.
// A task in a terminal state (Completed) cannot transition to Queued.
func TestScheduler_Submit_TransitionError(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	// Create a task and manually put it in Completed state
	task := reef.NewTask("t1", "test", "coder", nil)
	task.Status = reef.TaskCompleted // terminal — can't transition to Queued

	err := sched.Submit(task)
	if err == nil {
		t.Error("expected error when submitting task in terminal state")
	}
}

// TestScheduler_Submit_EnqueueError covers the Enqueue failure path in Submit.
func TestScheduler_Submit_EnqueueError(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(1, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	// Fill the queue
	filler := reef.NewTask("filler", "fill", "coder", nil)
	_ = filler.Transition(reef.TaskQueued)
	_ = queue.Enqueue(filler)

	// Submit a new task — Enqueue should fail
	task := reef.NewTask("t1", "test", "coder", nil)
	err := sched.Submit(task)
	if err == nil {
		t.Error("expected error when queue is full")
	}
}

// =========================================================================
// Scheduler: dispatch error paths
// =========================================================================

// TestScheduler_Dispatch_TransitionRunningError covers the Transition→Running
// failure path in dispatch(). A task in Completed state cannot go to Running.
func TestScheduler_Dispatch_TransitionRunningError(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	// Create task in Queued state then sneakily change to Completed after dequeue
	task := reef.NewTask("t1", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	_ = queue.Enqueue(task)

	// Dequeue the task ourselves, then set it to a terminal state
	// and call dispatch directly
	task = queue.Dequeue()
	if task == nil {
		t.Fatal("expected task in queue")
	}
	task.Status = reef.TaskCompleted // terminal — Transition to Assigned will fail

	client := reg.Get("c1")
	err := sched.dispatch(task, client)
	if err == nil {
		t.Error("expected dispatch error for task in terminal state")
	}
}

// TestScheduler_Dispatch_TransitionAssignedError covers the Transition→Assigned
// failure path. Task in Completed can't go to Assigned.
func TestScheduler_Dispatch_TransitionAssignedError(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	// Task in Cancelled state
	task := reef.NewTask("t1", "test", "coder", nil)
	task.Status = reef.TaskCancelled

	client := reg.Get("c1")
	err := sched.dispatch(task, client)
	if err == nil {
		t.Error("expected dispatch error: cancelled → assigned is invalid")
	}
}

// =========================================================================
// Scheduler: HandleTaskCompleted error paths
// =========================================================================

// TestScheduler_HandleTaskCompleted_AlreadyTerminal covers the path where
// the task is already in a terminal state when HandleTaskCompleted is called.
// Note: the Running check (line ~173) will catch this first and return an error.
func TestScheduler_HandleTaskCompleted_AlreadyCompleted(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	// Task already completed
	task := reef.NewTask("t1", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	_ = task.Transition(reef.TaskAssigned)
	_ = task.Transition(reef.TaskRunning)
	_ = task.Transition(reef.TaskCompleted)
	sched.mu.Lock()
	sched.tasks["t1"] = task
	sched.mu.Unlock()

	// HandleTaskCompleted checks task.Status != Running → returns error
	err := sched.HandleTaskCompleted("t1", &reef.TaskResult{Text: "done"})
	if err == nil {
		t.Error("expected error completing an already-completed task")
	}
}

// =========================================================================
// Scheduler: HandleTaskFailed error paths (already covered, verify)
// =========================================================================

// TestScheduler_HandleTaskFailed_WithAttemptHistoryPreserveClientID verifies
// that attempt history entries without ClientID get the failed client's ID.
func TestScheduler_HandleTaskFailed_AttachClientID(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 1,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{MaxEscalations: 1})

	task := reef.NewTask("t1", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	_ = task.Transition(reef.TaskAssigned)
	_ = task.Transition(reef.TaskRunning)
	task.AssignedClient = "c1"
	sched.mu.Lock()
	sched.tasks["t1"] = task
	sched.mu.Unlock()

	err := sched.HandleTaskFailed("t1", &reef.TaskError{
		Type: "test", Message: "msg",
	}, []reef.AttemptRecord{
		{AttemptNumber: 1, Status: "failed", ErrorMessage: "fail"},
		// No ClientID — should be filled in
	})
	if err != nil {
		t.Fatalf("HandleTaskFailed: %v", err)
	}
	if len(task.AttemptHistory) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(task.AttemptHistory))
	}
	if task.AttemptHistory[0].ClientID != "c1" {
		t.Errorf("expected ClientID c1, got %s", task.AttemptHistory[0].ClientID)
	}
}

// =========================================================================
// Scheduler: escalate edge cases
// =========================================================================

// TestScheduler_Escalate_ReassignToOtherClient tests escalation to reassign
// when another matching client is available.
func TestScheduler_Escalate_ReassignWhenClientAvailable(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 1,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	reg.Register(&reef.ClientInfo{
		ID: "c2", Role: "coder", Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{MaxEscalations: 2})

	task := reef.NewTask("t1", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	_ = task.Transition(reef.TaskAssigned)
	_ = task.Transition(reef.TaskRunning)
	task.AssignedClient = "c1"
	sched.mu.Lock()
	sched.tasks["t1"] = task
	sched.mu.Unlock()

	err := sched.HandleTaskFailed("t1", &reef.TaskError{Type: "test", Message: "fail"}, nil)
	if err != nil {
		t.Fatalf("HandleTaskFailed: %v", err)
	}

	// Should be reassigned (Queued for c2)
	if task.Status != reef.TaskQueued {
		t.Errorf("expected Queued for reassignment, got %s", task.Status)
	}
	if task.EscalationCount != 1 {
		t.Errorf("expected escalation=1, got %d", task.EscalationCount)
	}
}

// TestScheduler_Escalate_TerminateNoOtherClient tests escalation to terminate
// when no other client is available.
func TestScheduler_Escalate_TerminateNoOtherClient(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 1,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	// No other coder clients
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{MaxEscalations: 2})

	task := reef.NewTask("t1", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	_ = task.Transition(reef.TaskAssigned)
	_ = task.Transition(reef.TaskRunning)
	task.AssignedClient = "c1"
	sched.mu.Lock()
	sched.tasks["t1"] = task
	sched.mu.Unlock()

	err := sched.HandleTaskFailed("t1", &reef.TaskError{Type: "test", Message: "fail"}, nil)
	if err != nil {
		t.Fatalf("HandleTaskFailed: %v", err)
	}

	// No other client → terminate
	if task.Status != reef.TaskFailed {
		t.Errorf("expected Failed (terminate), got %s", task.Status)
	}
}

// =========================================================================
// Server: Start error paths
// =========================================================================

// TestServer_Start_WebSocketListenError covers net.Listen failure for WS.
func TestServer_Start_WebSocketListenError(t *testing.T) {
	cfg := DefaultConfig()
	// Invalid port range
	cfg.WebSocketAddr = ":99999"
	cfg.AdminAddr = ":0"

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := NewServer(cfg, logger)

	err := s.Start()
	if err == nil {
		t.Error("expected error for invalid WebSocket address")
		_ = s.Stop()
	}
}

// TestServer_Start_AdminListenError covers net.Listen failure for Admin.
func TestServer_Start_AdminListenError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WebSocketAddr = ":0"
	cfg.AdminAddr = ":99999" // invalid

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := NewServer(cfg, logger)

	err := s.Start()
	if err == nil {
		t.Error("expected error for invalid Admin address")
		_ = s.Stop()
	}
}

// TestServer_Start_ServeError covers the Serve goroutine error path
// when the listener returns an unexpected error on Accept.
func TestServer_Start_ServeError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeartbeatScan = 100 * time.Millisecond

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := NewServer(cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.cancelCtx = cancel

	wsMux := http.NewServeMux()
	wsMux.Handle("/ws", s.wsServer)
	s.wsHTTPServer = &http.Server{Addr: ":0", Handler: wsMux}

	adminMux := http.NewServeMux()
	s.admin.RegisterRoutes(adminMux)
	s.httpServer = &http.Server{Addr: ":0", Handler: adminMux}

	// Listeners that immediately return an error on every Accept
	errWs := &errorListener{acceptErr: fmt.Errorf("simulated ws accept error")}
	errAdmin := &errorListener{acceptErr: fmt.Errorf("simulated admin accept error")}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = s.wsHTTPServer.Serve(errWs)
	}()
	go func() {
		defer wg.Done()
		_ = s.httpServer.Serve(errAdmin)
	}()

	go s.heartbeatScanner(ctx)

	// Wait for goroutines to process the error and exit
	wg.Wait()
	time.Sleep(20 * time.Millisecond)
	_ = s.Stop()
}

// errorListener is a net.Listener that returns a fixed error on Accept.
type errorListener struct {
	acceptErr error
}

func (l *errorListener) Accept() (net.Conn, error) { return nil, l.acceptErr }
func (l *errorListener) Close() error              { return nil }
func (l *errorListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0} }

// =========================================================================
// Server: full lifecycle edge cases
// =========================================================================

// TestServer_StopDouble tests stopping twice without panic.
func TestServer_StopDouble(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WebSocketAddr = ":0"
	cfg.AdminAddr = ":0"
	cfg.HeartbeatScan = 100 * time.Millisecond

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := NewServer(cfg, logger)

	_ = s.Start()
	time.Sleep(50 * time.Millisecond)
	_ = s.Stop()
	_ = s.Stop() // double stop — should not panic
}

// TestServer_HeartbeatScanner_ContextCancel tests scanner exit via context cancel.
func TestServer_HeartbeatScanner_ContextCancel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HeartbeatScan = 10 * time.Millisecond
	cfg.HeartbeatTimeout = 100 * time.Millisecond

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := NewServer(cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelCtx = cancel

	done := make(chan struct{})
	go func() {
		s.heartbeatScanner(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Scanner exited cleanly
	case <-time.After(500 * time.Millisecond):
		t.Error("heartbeatScanner did not exit after cancel")
	}
}

// =========================================================================
// Server: heartbeatScanner with stale+queue expiry combo
// =========================================================================

// TestServer_HeartbeatScanner_FullPass exercises all branches:
// stale clients with running tasks that get paused, and expired queue tasks.
func TestServer_HeartbeatScanner_FullPass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WebSocketAddr = ":0"
	cfg.AdminAddr = ":0"
	cfg.HeartbeatScan = 50 * time.Millisecond
	cfg.HeartbeatTimeout = 50 * time.Millisecond
	cfg.QueueMaxAge = 50 * time.Millisecond

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	s := NewServer(cfg, logger)

	// Register a stale client with a running task
	s.registry.Register(&reef.ClientInfo{
		ID:            "stale-runner",
		Role:          "coder",
		Capacity:      2,
		CurrentLoad:   1,
		LastHeartbeat: time.Now().Add(-2 * time.Second),
		State:         reef.ClientConnected,
	})

	task := reef.NewTask("running-task", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	_ = task.Transition(reef.TaskAssigned)
	_ = task.Transition(reef.TaskRunning)
	task.AssignedClient = "stale-runner"
	s.scheduler.mu.Lock()
	s.scheduler.tasks["running-task"] = task
	s.scheduler.mu.Unlock()

	// Queue an old task that should expire
	oldTask := reef.NewTask("old-task", "test", "coder", nil)
	oldTask.CreatedAt = time.Now().Add(-2 * time.Second)
	_ = s.queue.Enqueue(oldTask)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelCtx = cancel

	done := make(chan struct{})
	go func() {
		s.heartbeatScanner(ctx)
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("heartbeatScanner did not exit")
	}

	// Verify task was paused
	if task.Status != reef.TaskPaused {
		t.Errorf("expected task paused, got %s", task.Status)
	}
	if task.PauseReason != "disconnect" {
		t.Errorf("expected pause reason 'disconnect', got '%s'", task.PauseReason)
	}

	// Verify old task expired
	if oldTask.Status != reef.TaskFailed {
		t.Errorf("expected old task Failed after expiry, got %s", oldTask.Status)
	}
}

// =========================================================================
// Scheduler: TryDispatch error flow (dispatch hook fails)
// =========================================================================

// TestScheduler_TryDispatch_DispatchFailsThenRequeue tests the path where
// dispatch returns an error (e.g., onDispatch hook fails), and the task is
// re-enqueued.
func TestScheduler_TryDispatch_DispatchFailsThenRequeue(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	queue := NewTaskQueue(10, time.Hour)

	dispatchCalled := false
	sched := NewScheduler(reg, queue, SchedulerOptions{
		OnDispatch: func(taskID, clientID string) error {
			dispatchCalled = true
			return &testError{"dispatch hook failed"}
		},
	})

	task := reef.NewTask("t1", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	_ = queue.Enqueue(task)

	sched.TryDispatch()

	if !dispatchCalled {
		t.Error("OnDispatch should have been called")
	}

	// Task should be back in queue (rollback happened)
	if task.Status != reef.TaskQueued {
		t.Errorf("expected Queued after rollback, got %s", task.Status)
	}
	if queue.Len() != 1 {
		t.Errorf("expected task re-enqueued, len=%d", queue.Len())
	}
}

// =========================================================================
// Scheduler: matchClient edge cases
// =========================================================================

// TestScheduler_MatchClient_RoleMismatch tests filtering by role.
func TestScheduler_MatchClient_RoleMismatch(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "analyst", Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	task := reef.NewTask("t1", "test", "coder", nil)
	matched := sched.matchClient(task, "")
	if matched != nil {
		t.Errorf("expected no match (role mismatch), got %s", matched.ID)
	}
}

// TestScheduler_MatchClient_NotAvailable tests filtering by availability state.
func TestScheduler_MatchClient_NotAvailable(t *testing.T) {
	reg := NewRegistry(nil)
	// Disconnected client
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now(), State: reef.ClientDisconnected,
	})
	// Stale client
	reg.Register(&reef.ClientInfo{
		ID: "c2", Role: "coder", Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now().Add(-time.Hour), State: reef.ClientStale,
	})
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	task := reef.NewTask("t1", "test", "coder", nil)
	matched := sched.matchClient(task, "")
	if matched != nil {
		t.Errorf("expected no match (no available clients), got %s", matched.ID)
	}
}

// TestScheduler_MatchClient_WithStaleClient tests that stale clients are excluded.
func TestScheduler_MatchClient_StaleExcluded(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now().Add(-time.Hour), State: reef.ClientStale,
	})
	reg.Register(&reef.ClientInfo{
		ID: "c2", Role: "coder", Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	task := reef.NewTask("t1", "test", "coder", nil)
	matched := sched.matchClient(task, "")
	if matched == nil || matched.ID != "c2" {
		t.Errorf("expected c2 (stale excluded), got %v", matched)
	}
}

// =========================================================================
// Scheduler: HandleTaskFailed with attempt history having ClientID=""
// =========================================================================

// TestScheduler_HandleTaskFailed_NilAttemptHistory tests the nil case
// which triggers the default attempt record creation.
func TestScheduler_HandleTaskFailed_NilAttemptHistory(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 1,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{MaxEscalations: 1})

	task := reef.NewTask("t1", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	_ = task.Transition(reef.TaskAssigned)
	_ = task.Transition(reef.TaskRunning)
	task.AssignedClient = "c1"
	sched.mu.Lock()
	sched.tasks["t1"] = task
	sched.mu.Unlock()

	// nil attemptHistory → creates default
	err := sched.HandleTaskFailed("t1", &reef.TaskError{Type: "test", Message: "msg"}, nil)
	if err != nil {
		t.Fatalf("HandleTaskFailed: %v", err)
	}

	if len(task.AttemptHistory) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(task.AttemptHistory))
	}
	if task.AttemptHistory[0].ClientID != "c1" {
		t.Errorf("expected ClientID c1, got %s", task.AttemptHistory[0].ClientID)
	}
	if task.AttemptHistory[0].Status != "failed" {
		t.Errorf("expected status 'failed', got %s", task.AttemptHistory[0].Status)
	}
}

// =========================================================================
// Scheduler: GetTask for missing task
// =========================================================================

func TestScheduler_GetTask_NotFound(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	task := sched.GetTask("nonexistent")
	if task != nil {
		t.Error("expected nil for unknown task")
	}
}

// =========================================================================
// Scheduler: empty TasksSnapshot
// =========================================================================

func TestScheduler_TasksSnapshot_Empty(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})

	snap := sched.TasksSnapshot()
	if len(snap) != 0 {
		t.Errorf("expected 0 tasks in empty snapshot, got %d", len(snap))
	}
}

// =========================================================================
// Registry: coverage for List with disconnected/stale mixed
// =========================================================================

func TestRegistry_List_MixedStates(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&reef.ClientInfo{ID: "c1", State: reef.ClientConnected, LastHeartbeat: time.Now()})
	r.Register(&reef.ClientInfo{ID: "c2", State: reef.ClientDisconnected, LastHeartbeat: time.Now()})
	r.Register(&reef.ClientInfo{ID: "c3", State: reef.ClientStale, LastHeartbeat: time.Now().Add(-time.Hour)})

	list := r.List()
	if len(list) != 3 {
		t.Errorf("expected 3 clients, got %d", len(list))
	}
}

// TestRegistry_ScanStale_ConnectedButRecent tests ScanStale when all are fresh.
func TestRegistry_ScanStale_AllFresh(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&reef.ClientInfo{
		ID: "fresh", LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})

	stale := r.ScanStale(1 * time.Minute)
	if len(stale) != 0 {
		t.Errorf("expected 0 stale, got %v", stale)
	}
}

// TestRegistry_ScanStale_Empty tests ScanStale on empty registry.
func TestRegistry_ScanStale_Empty(t *testing.T) {
	r := NewRegistry(nil)
	stale := r.ScanStale(1 * time.Minute)
	if len(stale) != 0 {
		t.Errorf("expected 0 stale from empty registry, got %v", stale)
	}
}

// TestRegistry_OnStaleCallback tests the onStale callback is invoked.
func TestRegistry_OnStaleCallback(t *testing.T) {
	calledID := ""
	r := NewRegistry(func(clientID string) {
		calledID = clientID
	})
	r.Register(&reef.ClientInfo{
		ID: "c1", State: reef.ClientConnected, LastHeartbeat: time.Now(),
	})
	r.MarkStale("c1")
	if calledID != "c1" {
		t.Errorf("expected onStale callback with 'c1', got '%s'", calledID)
	}
}

// =========================================================================
// WebSocket: SendMessage edge cases
// =========================================================================

// TestWebSocketServer_SendMessage_Disconnected_AllControlTypes tests
// buffering for all control message types.
func TestWebSocketServer_SendMessage_AllControlTypes(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	ws := NewWebSocketServer(reg, sched, "", nil)

	tests := []reef.MessageType{reef.MsgCancel, reef.MsgPause, reef.MsgResume}
	for _, mt := range tests {
		msg, _ := reef.NewMessage(mt, "t1", reef.ControlPayload{
			ControlType: string(mt),
			TaskID:      "t1",
		})
		err := ws.SendMessage("ctrl-client", msg)
		if err != nil {
			t.Errorf("SendMessage(%s) should buffer without error, got: %v", mt, err)
		}
	}

	ws.pendingMu.Lock()
	count := len(ws.pendingControls["ctrl-client"])
	ws.pendingMu.Unlock()
	if count != 3 {
		t.Errorf("expected 3 pending controls, got %d", count)
	}
}

// =========================================================================
// Admin: handleTasks edge cases (Assigned state, filter edge cases)
// =========================================================================

func TestAdminServer_HandleTasks_AssignedState(t *testing.T) {
	admin, _, sched := setupAdminTest(t)

	assigned := reef.NewTask("t-assigned", "test", "coder", nil)
	_ = assigned.Transition(reef.TaskQueued)
	_ = assigned.Transition(reef.TaskAssigned)
	sched.mu.Lock()
	sched.tasks["t-assigned"] = assigned
	sched.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/admin/tasks", nil)
	w := httptest.NewRecorder()
	admin.handleTasks(w, req)

	var resp TasksResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if len(resp.InflightTasks) != 1 {
		t.Errorf("expected 1 inflight (assigned), got %d", len(resp.InflightTasks))
	}
}

func TestAdminServer_HandleTasks_FilterNoMatch(t *testing.T) {
	admin, _, sched := setupAdminTest(t)

	task := reef.NewTask("t-coder", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	sched.mu.Lock()
	sched.tasks["t-coder"] = task
	sched.mu.Unlock()

	// Filter for a role that doesn't exist
	req := httptest.NewRequest(http.MethodGet, "/admin/tasks?role=designer", nil)
	w := httptest.NewRecorder()
	admin.handleTasks(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp TasksResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if len(resp.QueuedTasks) != 0 {
		t.Errorf("expected 0 tasks matching designer role, got %d", len(resp.QueuedTasks))
	}
}

func TestAdminServer_HandleTasks_FilterStatusNoMatch(t *testing.T) {
	admin, _, sched := setupAdminTest(t)

	task := reef.NewTask("t-queued", "test", "coder", nil)
	_ = task.Transition(reef.TaskQueued)
	sched.mu.Lock()
	sched.tasks["t-queued"] = task
	sched.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/admin/tasks?status=Running", nil)
	w := httptest.NewRecorder()
	admin.handleTasks(w, req)

	var resp TasksResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	// Total still counts all tasks
	if resp.Stats.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Stats.Total)
	}
	if len(resp.QueuedTasks) != 0 {
		t.Errorf("expected 0 Running tasks, got %d in queued", len(resp.QueuedTasks))
	}
}

// TestAdminServer_HandleTasks_BothFilters tests combining status and role filters.
func TestAdminServer_HandleTasks_BothFilters(t *testing.T) {
	admin, _, sched := setupAdminTest(t)

	coderQueued := reef.NewTask("t-cq", "test", "coder", nil)
	_ = coderQueued.Transition(reef.TaskQueued)
	sched.mu.Lock()
	sched.tasks["t-cq"] = coderQueued
	sched.mu.Unlock()

	coderRunning := reef.NewTask("t-cr", "test", "coder", nil)
	_ = coderRunning.Transition(reef.TaskQueued)
	_ = coderRunning.Transition(reef.TaskAssigned)
	_ = coderRunning.Transition(reef.TaskRunning)
	sched.mu.Lock()
	sched.tasks["t-cr"] = coderRunning
	sched.mu.Unlock()

	analystQueued := reef.NewTask("t-aq", "test", "analyst", nil)
	_ = analystQueued.Transition(reef.TaskQueued)
	sched.mu.Lock()
	sched.tasks["t-aq"] = analystQueued
	sched.mu.Unlock()

	// Filter by both role=coder AND status=Queued
	req := httptest.NewRequest(http.MethodGet, "/admin/tasks?role=coder&status=Queued", nil)
	w := httptest.NewRecorder()
	admin.handleTasks(w, req)

	var resp TasksResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if len(resp.QueuedTasks) != 1 {
		t.Errorf("expected 1 queued coder task, got %d", len(resp.QueuedTasks))
	}
}

// =========================================================================
// Admin: handleSubmitTask with MaxRetries=0 and TimeoutMs=0 (edge cases)
// =========================================================================

func TestAdminServer_HandleSubmitTask_NoRetriesNoTimeout(t *testing.T) {
	admin, reg, sched := setupAdminTest(t)

	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Skills: []string{"go"}, Capacity: 2, CurrentLoad: 0,
		LastHeartbeat: time.Now(), State: reef.ClientConnected,
	})

	// MaxRetries and TimeoutMs omitted (default to 0)
	body := SubmitTaskRequest{
		Instruction:    "no options",
		RequiredRole:   "coder",
		RequiredSkills: nil,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()
	admin.handleSubmitTask(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)

	task := sched.GetTask(resp["task_id"])
	if task == nil {
		t.Fatal("task not found")
	}
	if task.MaxRetries != 3 { // default
		t.Errorf("MaxRetries = %d, want 3 (default)", task.MaxRetries)
	}
	if task.TimeoutMs != 300000 { // default (5 min)
		t.Errorf("TimeoutMs = %d, want 300000 (default)", task.TimeoutMs)
	}

	taskIDMu.Lock()
	taskIDCounter = 0
	taskIDMu.Unlock()
}

// =========================================================================
// Server: test with custom Config
// =========================================================================

func TestDefaultConfig_AllFields(t *testing.T) {
	cfg := DefaultConfig()
	// Verify all fields from Config struct
	if cfg.Token != "" {
		t.Errorf("Token should default to empty, got %q", cfg.Token)
	}
	if cfg.QueueMaxLen != 1000 {
		t.Errorf("QueueMaxLen = %d, want 1000", cfg.QueueMaxLen)
	}
	if cfg.QueueMaxAge != 10*time.Minute {
		t.Errorf("QueueMaxAge = %v, want 10m", cfg.QueueMaxAge)
	}
	if cfg.MaxEscalations != 2 {
		t.Errorf("MaxEscalations = %d, want 2", cfg.MaxEscalations)
	}
}
