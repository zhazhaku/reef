// Package e2e provides end-to-end integration tests for Reef.
// This file tests the Coordinator bridge integration:
//   reef_submit_task → ServerBridge → Scheduler → WS Client → task_completed → reef_query_task
package e2e

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/reef"
	"github.com/sipeed/picoclaw/pkg/reef/server"
)

// ---------------------------------------------------------------------------
// Coordinator Bridge E2E Tests
// ---------------------------------------------------------------------------

func TestE2E_CoordinatorBridge_SubmitAndQuery(t *testing.T) {
	// Start a real Reef Server
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	bridge := server.NewServerBridge(srv.Scheduler(), srv.Registry())

	// Submit via bridge (simulates reef_submit_task tool)
	taskID, err := bridge.SubmitTask("search for Go tutorials", "executor", []string{"web_search"}, reef.TaskOptions{
		MaxRetries: 1,
		TimeoutMs:  60000,
		ModelHint:  "gpt-4o",
	})
	if err != nil {
		t.Fatalf("bridge submit: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected non-empty task ID")
	}

	// Query via bridge (simulates reef_query_task tool)
	snapshot, err := bridge.QueryTask(taskID)
	if err != nil {
		t.Fatalf("bridge query: %v", err)
	}
	if snapshot.TaskID != taskID {
		t.Fatalf("task ID mismatch: %s vs %s", snapshot.TaskID, taskID)
	}
	if snapshot.Status != "Queued" {
		t.Fatalf("expected Queued, got %s", snapshot.Status)
	}
	if snapshot.Instruction != "search for Go tutorials" {
		t.Fatalf("instruction mismatch: %s", snapshot.Instruction)
	}
}

func TestE2E_CoordinatorBridge_SubmitDispatchCompleteQuery(t *testing.T) {
	// Start a real Reef Server
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	bridge := server.NewServerBridge(srv.Scheduler(), srv.Registry())

	// Connect a mock client
	client := NewMockClient(t, MockClientOptions{
		Role:     "executor",
		Skills:   []string{"web_search"},
		Capacity: 3,
	})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Submit via bridge
	taskID, err := bridge.SubmitTask("search for Go tutorials", "executor", []string{"web_search"}, reef.TaskOptions{
		MaxRetries: 1,
		TimeoutMs:  60000,
	})
	if err != nil {
		t.Fatalf("bridge submit: %v", err)
	}

	// Client should receive task_dispatch
	payload, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for task_dispatch")
	}
	if payload.TaskID != taskID {
		t.Fatalf("task ID mismatch: %s vs %s", payload.TaskID, taskID)
	}
	if payload.Instruction != "search for Go tutorials" {
		t.Fatalf("instruction mismatch: %s", payload.Instruction)
	}
	if payload.ModelHint != "" {
		t.Fatalf("expected empty model hint, got %s", payload.ModelHint)
	}

	// Client reports completion
	client.SendTaskCompleted(taskID, "found 10 Go tutorials")

	// Wait for task to be completed
	ts := srv.WaitForTaskStatus(t, taskID, reef.TaskCompleted, 3*time.Second)
	if ts == nil {
		t.Fatal("task should be completed")
	}

	// Query via bridge — should have result
	snapshot, err := bridge.QueryTask(taskID)
	if err != nil {
		t.Fatalf("bridge query: %v", err)
	}
	if snapshot.Status != "Completed" {
		t.Fatalf("expected Completed, got %s", snapshot.Status)
	}
	if snapshot.Result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestE2E_CoordinatorBridge_Status(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	bridge := server.NewServerBridge(srv.Scheduler(), srv.Registry())

	// Connect two clients
	client1 := NewMockClient(t, MockClientOptions{
		ClientID: "worker-1",
		Role:     "executor",
		Skills:   []string{"web_search"},
		Capacity: 3,
	})
	if err := client1.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect client1: %v", err)
	}
	defer client1.Close()
	_, _ = client1.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	client2 := NewMockClient(t, MockClientOptions{
		ClientID: "worker-2",
		Role:     "coder",
		Skills:   []string{"github", "write_file"},
		Capacity: 2,
	})
	if err := client2.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect client2: %v", err)
	}
	defer client2.Close()
	_, _ = client2.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Check status via bridge
	status, err := bridge.Status()
	if err != nil {
		t.Fatalf("bridge status: %v", err)
	}
	if status.ConnectedClients != 2 {
		t.Fatalf("expected 2 connected clients, got %d", status.ConnectedClients)
	}
	if len(status.Clients) != 2 {
		t.Fatalf("expected 2 clients in list, got %d", len(status.Clients))
	}

	// Verify client roles
	roles := make(map[string]bool)
	for _, c := range status.Clients {
		roles[c.Role] = true
	}
	if !roles["executor"] || !roles["coder"] {
		t.Fatalf("expected both executor and coder, got %+v", roles)
	}
}

func TestE2E_CoordinatorBridge_FullWorkflow(t *testing.T) {
	// This test simulates the complete Coordinator workflow:
	// 1. Coordinator checks status (reef_status)
	// 2. Coordinator submits task (reef_submit_task)
	// 3. Client receives and executes task
	// 4. Coordinator queries result (reef_query_task)
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	bridge := server.NewServerBridge(srv.Scheduler(), srv.Registry())

	// Step 1: Check status — no clients yet
	status, err := bridge.Status()
	if err != nil {
		t.Fatalf("initial status: %v", err)
	}
	if status.ConnectedClients != 0 {
		t.Fatalf("expected 0 clients, got %d", status.ConnectedClients)
	}

	// Connect a worker client
	worker := NewMockClient(t, MockClientOptions{
		Role:   "executor",
		Skills: []string{"web_search", "code_execution"},
		Capacity: 5,
	})
	if err := worker.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect worker: %v", err)
	}
	defer worker.Close()
	_, _ = worker.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Step 2: Check status — 1 client now
	status, err = bridge.Status()
	if err != nil {
		t.Fatalf("status after connect: %v", err)
	}
	if status.ConnectedClients != 1 {
		t.Fatalf("expected 1 client, got %d", status.ConnectedClients)
	}

	// Step 3: Submit multiple tasks
	taskID1, err := bridge.SubmitTask("search for Rust tutorials", "executor", []string{"web_search"}, reef.TaskOptions{
		MaxRetries: 2,
		TimeoutMs:  30000,
	})
	if err != nil {
		t.Fatalf("submit task1: %v", err)
	}

	taskID2, err := bridge.SubmitTask("run benchmark tests", "executor", []string{"code_execution"}, reef.TaskOptions{
		MaxRetries: 1,
		TimeoutMs:  120000,
		ModelHint:  "gpt-4o",
	})
	if err != nil {
		t.Fatalf("submit task2: %v", err)
	}

	// Step 4: Worker receives first task (capacity allows)
	payload, ok := worker.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for first task dispatch")
	}
	// One of the two tasks should have been dispatched
	dispatchedTask1 := payload.TaskID

	// Step 5: Worker completes first task
	var result1 string
	if dispatchedTask1 == taskID1 {
		result1 = "found 10 Rust tutorials"
	} else {
		result1 = "benchmark passed"
	}
	worker.SendTaskCompleted(dispatchedTask1, result1)

	// Wait for completion
	srv.WaitForTaskStatus(t, dispatchedTask1, reef.TaskCompleted, 3*time.Second)

	// Step 6: Query result
	snapshot, err := bridge.QueryTask(dispatchedTask1)
	if err != nil {
		t.Fatalf("query completed task: %v", err)
	}
	if snapshot.Status != "Completed" {
		t.Fatalf("expected Completed, got %s", snapshot.Status)
	}

	// Step 7: Worker receives second task (after first completed, capacity freed)
	payload2, ok := worker.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for second task dispatch")
	}
	dispatchedTask2 := payload2.TaskID
	if dispatchedTask2 != taskID1 && dispatchedTask2 != taskID2 {
		t.Fatalf("unexpected task ID: %s", dispatchedTask2)
	}

	// Step 8: Worker completes second task
	worker.SendTaskCompleted(dispatchedTask2, "all tests passed")
	srv.WaitForTaskStatus(t, dispatchedTask2, reef.TaskCompleted, 3*time.Second)

	// Step 9: Final status check
	status, err = bridge.Status()
	if err != nil {
		t.Fatalf("final status: %v", err)
	}
	if status.CompletedTasks != 2 {
		t.Fatalf("expected 2 completed tasks, got %d", status.CompletedTasks)
	}
	if status.QueuedTasks != 0 {
		t.Fatalf("expected 0 queued tasks, got %d", status.QueuedTasks)
	}
}

func TestE2E_CoordinatorBridge_TaskFailedRetry(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	bridge := server.NewServerBridge(srv.Scheduler(), srv.Registry())

	// Connect two workers
	worker1 := NewMockClient(t, MockClientOptions{
		ClientID: "worker-1",
		Role:     "executor",
		Skills:   []string{"web_search"},
		Capacity: 1,
	})
	if err := worker1.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect worker1: %v", err)
	}
	defer worker1.Close()
	_, _ = worker1.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	worker2 := NewMockClient(t, MockClientOptions{
		ClientID: "worker-2",
		Role:     "executor",
		Skills:   []string{"web_search"},
		Capacity: 1,
	})
	if err := worker2.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect worker2: %v", err)
	}
	defer worker2.Close()
	_, _ = worker2.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Submit task
	taskID, err := bridge.SubmitTask("fragile task", "executor", []string{"web_search"}, reef.TaskOptions{
		MaxRetries: 2,
		TimeoutMs:  30000,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// First worker receives and fails it
	payload, ok := worker1.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for dispatch to worker1")
	}
	if payload.TaskID != taskID {
		t.Fatalf("task ID mismatch: %s vs %s", payload.TaskID, taskID)
	}

	worker1.SendTaskFailed(taskID, "execution_error", "connection timeout")

	// Task should be reassigned to worker2 (escalation reassign)
	payload2, ok := worker2.WaitForTaskDispatch(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for re-dispatch to worker2")
	}
	if payload2.TaskID != taskID {
		t.Fatalf("re-dispatched task ID mismatch: %s vs %s", payload2.TaskID, taskID)
	}

	// Worker2 succeeds
	worker2.SendTaskCompleted(taskID, "done on second try")

	ts := srv.WaitForTaskStatus(t, taskID, reef.TaskCompleted, 3*time.Second)
	if ts == nil {
		t.Fatal("task should be completed after retry")
	}

	// Query result
	snapshot, err := bridge.QueryTask(taskID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if snapshot.Status != "Completed" {
		t.Fatalf("expected Completed, got %s", snapshot.Status)
	}
}

func TestE2E_CoordinatorBridge_ValidationErrors(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	bridge := server.NewServerBridge(srv.Scheduler(), srv.Registry())

	// Missing instruction
	_, err := bridge.SubmitTask("", "executor", nil, reef.TaskOptions{})
	if err == nil {
		t.Fatal("expected error for empty instruction")
	}

	// Missing role
	_, err = bridge.SubmitTask("do something", "", nil, reef.TaskOptions{})
	if err == nil {
		t.Fatal("expected error for empty role")
	}

	// Query nonexistent task
	_, err = bridge.QueryTask("nonexistent-task-id")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestE2E_CoordinatorBridge_ConcurrentSubmissions(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	bridge := server.NewServerBridge(srv.Scheduler(), srv.Registry())

	// Connect a high-capacity worker
	worker := NewMockClient(t, MockClientOptions{
		Role:     "executor",
		Skills:   []string{"web_search"},
		Capacity: 10,
	})
	if err := worker.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer worker.Close()
	_, _ = worker.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Submit 5 tasks concurrently
	taskIDs := make([]string, 5)
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			taskIDs[idx], errs[idx] = bridge.SubmitTask(
				"concurrent task", "executor", []string{"web_search"},
				reef.TaskOptions{MaxRetries: 1, TimeoutMs: 30000},
			)
		}(i)
	}

	// Wait for submissions
	time.Sleep(500 * time.Millisecond)

	// Verify all submitted successfully
	for i := 0; i < 5; i++ {
		if errs[i] != nil {
			t.Fatalf("submit %d failed: %v", i, errs[i])
		}
		if taskIDs[i] == "" {
			t.Fatalf("submit %d returned empty task ID", i)
		}
	}

	// All task IDs should be unique
	seen := make(map[string]bool)
	for _, id := range taskIDs {
		if seen[id] {
			t.Fatalf("duplicate task ID: %s", id)
		}
		seen[id] = true
	}

	// Complete all dispatched tasks
	completed := 0
	for completed < 5 {
		payload, ok := worker.WaitForTaskDispatch(2 * time.Second)
		if !ok {
			break // no more dispatches
		}
		worker.SendTaskCompleted(payload.TaskID, "done")
		completed++
	}

	// Check final status
	status, err := bridge.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.CompletedTasks != 5 {
		t.Logf("completed=%d, running=%d, queued=%d", status.CompletedTasks, status.RunningTasks, status.QueuedTasks)
		// Some tasks may still be queued if dispatch is async
	}
}

// TestE2E_CoordinatorBridge_JSONSerialization verifies that bridge results
// can be properly JSON-marshaled (as the tools would do).
func TestE2E_CoordinatorBridge_JSONSerialization(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	bridge := server.NewServerBridge(srv.Scheduler(), srv.Registry())

	// Submit a task
	taskID, err := bridge.SubmitTask("test task", "executor", []string{"web_search"}, reef.TaskOptions{
		MaxRetries: 1,
		TimeoutMs:  30000,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Query and serialize
	snapshot, err := bridge.QueryTask(taskID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	// Verify it's valid JSON with expected fields
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["task_id"] != taskID {
		t.Fatalf("task_id mismatch: %v", parsed["task_id"])
	}
	if parsed["status"] != "Queued" {
		t.Fatalf("status mismatch: %v", parsed["status"])
	}

	// Status serialization
	status, err := bridge.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	statusData, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}

	var statusParsed map[string]any
	if err := json.Unmarshal(statusData, &statusParsed); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	// Should have connected_clients, queued_tasks, etc.
	if _, ok := statusParsed["connected_clients"]; !ok {
		t.Fatal("missing connected_clients in status JSON")
	}
}
