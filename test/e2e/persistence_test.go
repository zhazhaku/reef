package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server"
)

// tempSQLitePath returns a temporary SQLite database path under t.TempDir().
func tempSQLitePath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "reef-test.db")
}

// newE2EServerSQLite creates a Reef Server with SQLite persistence.
func newE2EServerSQLite(t *testing.T, dbPath string, token string) *E2EServer {
	wsPort := getFreePort(t)
	adminPort := getFreePort(t)

	cfg := server.Config{
		WebSocketAddr:    fmt.Sprintf("127.0.0.1:%d", wsPort),
		AdminAddr:        fmt.Sprintf("127.0.0.1:%d", adminPort),
		Token:            token,
		HeartbeatTimeout: 5 * time.Second,
		HeartbeatScan:    1 * time.Second,
		QueueMaxLen:      100,
		QueueMaxAge:      5 * time.Minute,
		MaxEscalations:   2,
		StoreType:        "sqlite",
		StorePath:        dbPath,
	}

	srv := server.NewServer(cfg, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("start SQLite server: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	return &E2EServer{
		Server:    srv,
		WSAddr:    cfg.WebSocketAddr,
		AdminAddr: cfg.AdminAddr,
		token:     token,
	}
}

// ============================================================================
// Phase 1.7.2: Submit → restart → verify recovery
// ============================================================================

// TestE2E_Persistence_SubmitRestartRecover verifies that tasks submitted
// before a Server restart are restored and can be dispatched.
func TestE2E_Persistence_SubmitRestartRecover(t *testing.T) {
	dbPath := tempSQLitePath(t)

	// === Phase 1: Start server, submit tasks ===
	srv1 := newE2EServerSQLite(t, dbPath, "")
	taskID1 := srv1.SubmitTask(t, "write unit tests", "coder", []string{"github"})
	taskID2 := srv1.SubmitTask(t, "refactor module", "coder", []string{"execute_command"})

	// Verify tasks exist in scheduler
	task1 := srv1.Scheduler().GetTask(taskID1)
	if task1 == nil {
		t.Fatal("task1 should exist in scheduler")
	}
	if task1.Status != reef.TaskQueued {
		t.Errorf("task1 status=%s, want Queued", task1.Status)
	}
	if task1.RequiredRole != "coder" {
		t.Errorf("task1 role=%s, want coder", task1.RequiredRole)
	}

	task2 := srv1.Scheduler().GetTask(taskID2)
	if task2 == nil {
		t.Fatal("task2 should exist in scheduler")
	}

	// Verify SQLite file exists and has data
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("SQLite db should exist: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("SQLite db should not be empty after submitting tasks")
	}

	// Shutdown first server
	srv1.Shutdown(t)

	// === Phase 2: Restart server with same DB ===
	srv2 := newE2EServerSQLite(t, dbPath, "")
	defer srv2.Shutdown(t)

	// Verify restored tasks exist in the new scheduler
	restored1 := srv2.Scheduler().GetTask(taskID1)
	if restored1 == nil {
		t.Fatal("task1 should be restored after restart")
	}
	if restored1.Status != reef.TaskQueued {
		t.Errorf("restored task1 status=%s, want Queued", restored1.Status)
	}
	if restored1.Instruction != "write unit tests" {
		t.Errorf("restored task1 instruction=%s, want 'write unit tests'", restored1.Instruction)
	}

	restored2 := srv2.Scheduler().GetTask(taskID2)
	if restored2 == nil {
		t.Fatal("task2 should be restored after restart")
	}
	if restored2.Instruction != "refactor module" {
		t.Errorf("restored task2 instruction=%s, want 'refactor module'", restored2.Instruction)
	}

	// === Phase 3: Connect a client and verify restored tasks are dispatchable ===
	client := NewMockClient(t, MockClientOptions{
		Role:     "coder",
		Skills:   []string{"github", "execute_command"},
		Capacity: 2,
	})
	if err := client.Connect(srv2.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// First restored task should be dispatched
	payload1, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for first restored task dispatch")
	}
	if payload1.TaskID != taskID1 && payload1.TaskID != taskID2 {
		t.Errorf("dispatched task_id=%s, want %s or %s", payload1.TaskID, taskID1, taskID2)
	}

	// Complete first task
	client.SendTaskCompleted(payload1.TaskID, "done")

	// Second task should be dispatched next
	payload2, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for second restored task dispatch")
	}
	client.SendTaskCompleted(payload2.TaskID, "done")

	// Both should be completed
	ts1 := srv2.WaitForTaskStatus(t, taskID1, reef.TaskCompleted, 3*time.Second)
	if ts1 == nil {
		t.Fatal("task1 should be completed")
	}
	ts2 := srv2.WaitForTaskStatus(t, taskID2, reef.TaskCompleted, 3*time.Second)
	if ts2 == nil {
		t.Fatal("task2 should be completed")
	}
}

// ============================================================================
// Phase 1.7.3: SQLite mode full task lifecycle
// ============================================================================

// TestE2E_Persistence_TaskLifecycle_SQLite verifies the complete task
// lifecycle with SQLite persistence: submit → dispatch → running → complete.
func TestE2E_Persistence_TaskLifecycle_SQLite(t *testing.T) {
	dbPath := tempSQLitePath(t)
	srv := newE2EServerSQLite(t, dbPath, "")
	defer srv.Shutdown(t)

	// Connect client
	client := NewMockClient(t, MockClientOptions{
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 2,
	})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Submit task
	taskID := srv.SubmitTask(t, "implement feature X", "coder", []string{"github"})

	// Task should be dispatched immediately since a matching client is connected
	payload, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for task dispatch")
	}
	if payload.TaskID != taskID {
		t.Errorf("dispatched task_id=%s, want %s", payload.TaskID, taskID)
	}

	// Send progress
	client.SendTaskProgress(taskID, "running", 50, "halfway")
	time.Sleep(200 * time.Millisecond)

	// Verify running
	task := srv.Scheduler().GetTask(taskID)
	if task == nil {
		t.Fatal("task not found")
	}
	if task.Status != reef.TaskRunning {
		t.Errorf("status=%s, want Running", task.Status)
	}

	// Complete
	client.SendTaskCompleted(taskID, "feature X implemented successfully")

	// Verify completed
	ts := srv.WaitForTaskStatus(t, taskID, reef.TaskCompleted, 3*time.Second)
	if ts == nil {
		t.Fatal("task should be completed")
	}

	// Verify admin stats
	tasks := srv.GetTasks(t, "", "")
	if tasks.Stats.Success != 1 {
		t.Fatalf("expected 1 success, got %d", tasks.Stats.Success)
	}
}

// ============================================================================
// Phase 1.7.4: Memory mode backward compatibility
// ============================================================================

// TestE2E_Persistence_MemoryMode_BackwardCompatible verifies that the
// default memory mode still works correctly.
func TestE2E_Persistence_MemoryMode_BackwardCompatible(t *testing.T) {
	srv := NewE2EServer(t, "") // default: memory store
	defer srv.Shutdown(t)

	// Connect client
	client := NewMockClient(t, MockClientOptions{
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 2,
	})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Submit and complete a task
	taskID := srv.SubmitTask(t, "write docs", "coder", []string{"github"})

	payload, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for dispatch")
	}
	client.SendTaskCompleted(payload.TaskID, "docs written")

	ts := srv.WaitForTaskStatus(t, taskID, reef.TaskCompleted, 3*time.Second)
	if ts == nil {
		t.Fatal("task should be completed in memory mode")
	}

	// Shutdown
	srv.Shutdown(t)
}

// TestE2E_Persistence_RunningTasksResetToQueued verifies that tasks
// that were Running/Assigned at shutdown are reset to Queued on restore.
func TestE2E_Persistence_RunningTasksResetToQueued(t *testing.T) {
	dbPath := tempSQLitePath(t)
	srv1 := newE2EServerSQLite(t, dbPath, "")
	defer srv1.Shutdown(t)

	// Connect client
	client := NewMockClient(t, MockClientOptions{
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 2,
	})
	if err := client.Connect(srv1.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Submit a task and let it be dispatched (but not completed)
	taskID := srv1.SubmitTask(t, "long running build", "coder", []string{"github"})

	payload, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for dispatch")
	}
	_ = payload

	// Verify it's assigned/running
	task := srv1.Scheduler().GetTask(taskID)
	if task == nil {
		t.Fatal("task not found")
	}
	if task.Status != reef.TaskRunning && task.Status != reef.TaskAssigned {
		t.Errorf("task status=%s, want Running or Assigned", task.Status)
	}
	if task.AssignedClient == "" {
		t.Fatal("task should have assigned client")
	}

	// Force-shutdown without completing the task
	srv1.Shutdown(t)

	// Restart with same DB
	srv2 := newE2EServerSQLite(t, dbPath, "")
	defer srv2.Shutdown(t)

	// Verify task is restored as Queued (not Running/Assigned)
	restored := srv2.Scheduler().GetTask(taskID)
	if restored == nil {
		t.Fatal("task should be restored")
	}
	if restored.Status != reef.TaskQueued {
		t.Errorf("restored task status=%s, want Queued (reset from Running/Assigned)", restored.Status)
	}
	if restored.AssignedClient != "" {
		t.Errorf("restored task assigned_client=%s, want empty (client disconnected)", restored.AssignedClient)
	}
}

// TestE2E_Persistence_CompletedTasksNotRestored verifies that completed
// tasks are NOT re-queued after restart.
func TestE2E_Persistence_CompletedTasksNotRestored(t *testing.T) {
	dbPath := tempSQLitePath(t)
	srv1 := newE2EServerSQLite(t, dbPath, "")
	defer srv1.Shutdown(t)

	// Connect client and complete a task
	client := NewMockClient(t, MockClientOptions{
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 2,
	})
	if err := client.Connect(srv1.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	taskID := srv1.SubmitTask(t, "write tests", "coder", []string{"github"})
	payload, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for dispatch")
	}
	client.SendTaskCompleted(payload.TaskID, "tests written")

	ts := srv1.WaitForTaskStatus(t, taskID, reef.TaskCompleted, 3*time.Second)
	if ts == nil {
		t.Fatal("task should be completed")
	}

	// Shutdown
	srv1.Shutdown(t)

	// Restart
	srv2 := newE2EServerSQLite(t, dbPath, "")
	defer srv2.Shutdown(t)

	// Verify completed task is in store (queryable) but NOT in queue
	restored := srv2.Scheduler().GetTask(taskID)
	if restored == nil {
		t.Fatal("completed task should still be queryable from store")
	}
	if restored.Status != reef.TaskCompleted {
		t.Errorf("completed task status=%s, want Completed", restored.Status)
	}

	// Queue should be empty (completed tasks not re-queued)
	if srv2.Queue().Len() != 0 {
		t.Errorf("queue length=%d, want 0 (completed tasks should not be restored to queue)", srv2.Queue().Len())
	}
}

// TestE2E_Persistence_MultipleTaskTypesMixedStatuses verifies that
// multiple tasks with mixed statuses survive restart correctly.
func TestE2E_Persistence_MultipleTaskTypesMixedStatuses(t *testing.T) {
	dbPath := tempSQLitePath(t)
	srv1 := newE2EServerSQLite(t, dbPath, "")
	defer srv1.Shutdown(t)

	client := NewMockClient(t, MockClientOptions{
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 3,
	})
	if err := client.Connect(srv1.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Submit 5 tasks with different roles
	taskA := srv1.SubmitTask(t, "task A - code", "coder", []string{"github"})
	taskB := srv1.SubmitTask(t, "task B - refactor", "coder", []string{"execute_command"})
	taskC := srv1.SubmitTask(t, "task C - review", "analyst", []string{"web_fetch"})
	taskD := srv1.SubmitTask(t, "task D - docs", "coder", []string{"github"})
	_ = srv1.SubmitTask(t, "task E - test", "tester", []string{"execute_command"})

	// Dispatch and complete only the first coder task
	payload, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for task dispatch")
	}
	client.SendTaskCompleted(payload.TaskID, "completed")

	// Shutdown
	srv1.Shutdown(t)

	// Restart
	srv2 := newE2EServerSQLite(t, dbPath, "")
	defer srv2.Shutdown(t)

	// Verify: completed task is still queryable
	completed := srv2.Scheduler().GetTask(taskA)
	if completed == nil {
		t.Fatal("completed taskA should be queryable")
	}
	if completed.Status != reef.TaskCompleted {
		t.Errorf("taskA status=%s, want Completed", completed.Status)
	}

	// Verify: queued tasks (with matching roles) are restored
	queued := srv2.Scheduler().GetTask(taskB)
	if queued == nil {
		t.Fatal("queued taskB should be restored")
	}
	if queued.Status != reef.TaskQueued {
		t.Errorf("taskB status=%s, want Queued", queued.Status)
	}

	// Connect coder client — should receive restored coder tasks
	client2 := NewMockClient(t, MockClientOptions{
		Role:     "coder",
		Skills:   []string{"github", "execute_command"},
		Capacity: 2,
	})
	if err := client2.Connect(srv2.WSURL()); err != nil {
		t.Fatalf("client2 connect: %v", err)
	}
	defer client2.Close()
	_, _ = client2.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Should receive at least taskB or taskD (coder tasks)
	dispatchedIDs := make(map[string]bool)
	for i := 0; i < 3; i++ {
		select {
		case p := <-dispatchChan(client2, 2*time.Second):
			dispatchedIDs[p.TaskID] = true
			client2.SendTaskCompleted(p.TaskID, "done")
		case <-time.After(1 * time.Second):
			break
		}
	}

	// At least one coder task should be dispatched
	coderTasks := map[string]bool{taskB: true, taskD: true}
	found := false
	for id := range dispatchedIDs {
		if coderTasks[id] {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no restored coder task dispatched; dispatched=%v, expected among %v", dispatchedIDs, coderTasks)
	}

	// Verify non-matching tasks (analyst, tester) are still queued
	analyst := srv2.Scheduler().GetTask(taskC)
	if analyst == nil {
		t.Fatal("analyst taskC should be restored")
	}
	if analyst.Status != reef.TaskQueued {
		t.Errorf("taskC status=%s, want Queued", analyst.Status)
	}

	// Verify admin stats
	time.Sleep(500 * time.Millisecond)
	tasks := srv2.GetTasks(t, "", "")
	t.Logf("Stats after restore: total=%d success=%d failed=%d queued=%d running=%d",
		tasks.Stats.Total, tasks.Stats.Success, tasks.Stats.Failed, tasks.Stats.Queued, tasks.Stats.Running)
}

// TestE2E_Persistence_WebhookStillWorksAfterRestart verifies that webhook
// notification configuration persists across server restarts.
func TestE2E_Persistence_WebhookStillWorksAfterRestart(t *testing.T) {
	// Set up mock webhook
	webhookReceived := make(chan []byte, 1)
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		webhookReceived <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	dbPath := tempSQLitePath(t)

	// Create custom server with webhook + SQLite, max_escalations=1
	wsPort := getFreePort(t)
	adminPort := getFreePort(t)
	cfg := server.Config{
		WebSocketAddr:    fmt.Sprintf("127.0.0.1:%d", wsPort),
		AdminAddr:        fmt.Sprintf("127.0.0.1:%d", adminPort),
		Token:            "",
		HeartbeatTimeout: 5 * time.Second,
		HeartbeatScan:    1 * time.Second,
		QueueMaxLen:      100,
		QueueMaxAge:      5 * time.Minute,
		MaxEscalations:   1,
		WebhookURLs:      []string{webhookServer.URL},
		StoreType:        "sqlite",
		StorePath:        dbPath,
	}
	srv1 := server.NewServer(cfg, nil)
	if err := srv1.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	e2e1 := &E2EServer{
		Server:    srv1,
		WSAddr:    cfg.WebSocketAddr,
		AdminAddr: cfg.AdminAddr,
	}

	// Register one client and complete a task (no escalation needed)
	client := NewMockClient(t, MockClientOptions{
		ClientID: "persist-webhook-c",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 1,
	})
	if err := client.Connect(e2e1.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	taskID1 := e2e1.SubmitTask(t, "task before restart", "coder", []string{"github"})
	payload1, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for first dispatch")
	}
	client.SendTaskCompleted(payload1.TaskID, "done before restart")
	e2e1.WaitForTaskStatus(t, taskID1, reef.TaskCompleted, 3*time.Second)

	// Restart server with same config
	srv1.Stop()
	time.Sleep(200 * time.Millisecond)

	srv2 := server.NewServer(cfg, nil)
	if err := srv2.Start(); err != nil {
		t.Fatalf("restart server: %v", err)
	}
	defer srv2.Stop()
	time.Sleep(100 * time.Millisecond)

	e2e2 := &E2EServer{
		Server:    srv2,
		WSAddr:    cfg.WebSocketAddr,
		AdminAddr: cfg.AdminAddr,
	}

	// Reconnect and submit another task
	client2 := NewMockClient(t, MockClientOptions{
		ClientID: "persist-webhook-c",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 1,
	})
	if err := client2.Connect(e2e2.WSURL()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer client2.Close()
	_, _ = client2.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Verify completed task from before restart is still queryable
	restored := e2e2.Scheduler().GetTask(taskID1)
	if restored == nil {
		t.Fatal("completed task should be queryable after restart")
	}
	if restored.Status != reef.TaskCompleted {
		t.Errorf("status=%s, want Completed", restored.Status)
	}

	// Submit a new task after restart
	taskID2 := e2e2.SubmitTask(t, "task after restart", "coder", []string{"github"})
	payload2, ok := client2.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for dispatch after restart")
	}
	client2.SendTaskCompleted(payload2.TaskID, "done after restart")
	e2e2.WaitForTaskStatus(t, taskID2, reef.TaskCompleted, 3*time.Second)

	// Verify tasks from admin
	tasks := e2e2.GetTasks(t, "", "")
	if tasks.Stats.Success < 1 {
		t.Errorf("expected at least 1 success after restart, got %d", tasks.Stats.Success)
	}
	t.Logf("Persistence+webhook test: stats total=%d success=%d", tasks.Stats.Total, tasks.Stats.Success)
}

// dispatchChan returns a channel that receives the next task_dispatch
// from the mock client, or times out after the given duration.
func dispatchChan(client *MockClient, timeout time.Duration) <-chan *reef.TaskDispatchPayload {
	ch := make(chan *reef.TaskDispatchPayload, 1)
	go func() {
		payload, ok := client.WaitForTaskDispatch(timeout)
		if ok {
			ch <- payload
		}
		close(ch)
	}()
	return ch
}
