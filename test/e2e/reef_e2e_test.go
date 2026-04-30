package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server"
	"github.com/zhazhaku/reef/pkg/reef/server/notify"
)

// ---------------------------------------------------------------------------
// 1. Server Startup & Client Registration
// ---------------------------------------------------------------------------

func TestE2E_SingleClientRegisters(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	client := NewMockClient(t, MockClientOptions{
		Role:     "coder",
		Skills:   []string{"github", "write_file"},
		Capacity: 2,
	})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	// Wait for register_ack
	msg, ok := client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)
	if !ok {
		t.Fatal("timeout waiting for register_ack")
	}
	var ack reef.RegisterAckPayload
	if err := msg.DecodePayload(&ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}

	// Verify admin status
	status := srv.GetStatus(t)
	if len(status.ConnectedClients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(status.ConnectedClients))
	}
	if status.ConnectedClients[0].Role != "coder" {
		t.Errorf("role=%s, want coder", status.ConnectedClients[0].Role)
	}
	if status.ServerVersion != reef.ProtocolVersion {
		t.Errorf("version=%s, want %s", status.ServerVersion, reef.ProtocolVersion)
	}
}

func TestE2E_MultipleClientsDifferentRoles(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	coder := NewMockClient(t, MockClientOptions{Role: "coder", Skills: []string{"github"}, Capacity: 2})
	analyst := NewMockClient(t, MockClientOptions{Role: "analyst", Skills: []string{"web_fetch"}, Capacity: 2})

	if err := coder.Connect(srv.WSURL()); err != nil {
		t.Fatalf("coder connect: %v", err)
	}
	if err := analyst.Connect(srv.WSURL()); err != nil {
		t.Fatalf("analyst connect: %v", err)
	}
	defer coder.Close()
	defer analyst.Close()

	waitFor(t, 2*time.Second, func() bool {
		status := srv.GetStatus(t)
		return len(status.ConnectedClients) == 2
	})

	status := srv.GetStatus(t)
	roles := make(map[string]bool)
	for _, c := range status.ConnectedClients {
		roles[c.Role] = true
	}
	if !roles["coder"] || !roles["analyst"] {
		t.Fatalf("expected both coder and analyst, got %+v", roles)
	}
}

func TestE2E_ClientHeartbeatKeepsAlive(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	client := NewMockClient(t, MockClientOptions{Role: "coder", Skills: []string{"github"}})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	// Wait for initial registration
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Send heartbeats every 500ms for 3 seconds
	for i := 0; i < 6; i++ {
		client.SendHeartbeat()
		time.Sleep(500 * time.Millisecond)
	}

	// After a short delay, scan for stale should not mark this client
	time.Sleep(2 * time.Second)
	status := srv.GetStatus(t)
	if len(status.ConnectedClients) != 1 {
		t.Fatalf("expected 1 connected client after heartbeats, got %d", len(status.ConnectedClients))
	}
	if status.StaleCount != 0 {
		t.Fatalf("expected 0 stale clients, got %d", status.StaleCount)
	}
}

func TestE2E_InvalidTokenRejected(t *testing.T) {
	srv := NewE2EServer(t, "secret-token")
	defer srv.Shutdown(t)

	// Client without token
	client := NewMockClient(t, MockClientOptions{Role: "coder"})
	if err := client.Connect(srv.WSURL()); err == nil {
		t.Fatal("expected connection to be rejected without token")
	}
}

// ---------------------------------------------------------------------------
// 2. Task Dispatch & Execution
// ---------------------------------------------------------------------------

func TestE2E_TaskDispatchedToMatchingRole(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

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

	taskID := srv.SubmitTask(t, "write a unit test", "coder", []string{"github"})

	// Client should receive task_dispatch
	payload, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for task_dispatch")
	}
	if payload.TaskID != taskID {
		t.Errorf("task_id=%s, want %s", payload.TaskID, taskID)
	}
}

func TestE2E_TaskQueuedWhenNoMatchingClient(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	// Only analyst connected
	client := NewMockClient(t, MockClientOptions{Role: "analyst", Skills: []string{"web_fetch"}})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	taskID := srv.SubmitTask(t, "write code", "coder", []string{"github"})

	// Task should be queued
	ts := srv.WaitForTaskStatus(t, taskID, reef.TaskQueued, 2*time.Second)
	if ts == nil {
		t.Fatal("task should be queued")
	}

	// Analyst should NOT receive any task_dispatch
	select {
	case msg := <-client.Messages():
		if msg.MsgType == reef.MsgTaskDispatch {
			t.Fatal("analyst should not receive coder task")
		}
	case <-time.After(500 * time.Millisecond):
		// expected - no message
	}
}

func TestE2E_TaskDispatchedWhenClientLaterConnects(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	// Submit task before any client connects
	taskID := srv.SubmitTask(t, "write code", "coder", []string{"github"})

	// Verify task is queued
	ts := srv.WaitForTaskStatus(t, taskID, reef.TaskQueued, 2*time.Second)
	if ts == nil {
		t.Fatal("task should be queued")
	}

	// Now connect a coder client
	client := NewMockClient(t, MockClientOptions{Role: "coder", Skills: []string{"github"}})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	// Should receive the queued task
	payload, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for queued task dispatch")
	}
	if payload.TaskID != taskID {
		t.Errorf("task_id=%s, want %s", payload.TaskID, taskID)
	}
}

func TestE2E_TaskRoutedToLowestLoadClient(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	// Client A with load=0
	clientA := NewMockClient(t, MockClientOptions{
		ClientID: "coder-a",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 3,
	})
	if err := clientA.Connect(srv.WSURL()); err != nil {
		t.Fatalf("clientA connect: %v", err)
	}
	defer clientA.Close()
	_, _ = clientA.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Client B with load=0 initially
	clientB := NewMockClient(t, MockClientOptions{
		ClientID: "coder-b",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 3,
	})
	if err := clientB.Connect(srv.WSURL()); err != nil {
		t.Fatalf("clientB connect: %v", err)
	}
	defer clientB.Close()
	_, _ = clientB.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Simulate clientA having load=1 by manually incrementing
	srv.Registry().IncrementLoad("coder-a")

	// Submit task - should go to clientB (load=0 < load=1)
	taskID := srv.SubmitTask(t, "write code", "coder", []string{"github"})

	// Only clientB should receive it
	payload, ok := clientB.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for task dispatch to clientB")
	}
	if payload.TaskID != taskID {
		t.Errorf("task_id=%s, want %s", payload.TaskID, taskID)
	}

	// clientA should not receive anything
	select {
	case msg := <-clientA.Messages():
		if msg.MsgType == reef.MsgTaskDispatch {
			t.Fatal("clientA (higher load) should not receive task")
		}
	case <-time.After(500 * time.Millisecond):
		// expected
	}
}

// ---------------------------------------------------------------------------
// 3. Task Completion & Result Reporting
// ---------------------------------------------------------------------------

func TestE2E_TaskCompletedReported(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	client := NewMockClient(t, MockClientOptions{Role: "coder", Skills: []string{"github"}})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	taskID := srv.SubmitTask(t, "write code", "coder", []string{"github"})

	// Receive dispatch
	_, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for task dispatch")
	}

	// Report completion
	client.SendTaskCompleted(taskID, "code written successfully")

	// Verify task is completed
	ts := srv.WaitForTaskStatus(t, taskID, reef.TaskCompleted, 3*time.Second)
	if ts == nil {
		t.Fatal("task should be completed")
	}

	// Verify admin tasks
	tasks := srv.GetTasks(t, "", "")
	if tasks.Stats.Success != 1 {
		t.Fatalf("expected 1 success, got %d", tasks.Stats.Success)
	}
}

func TestE2E_TaskProgressReported(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	client := NewMockClient(t, MockClientOptions{Role: "coder", Skills: []string{"github"}})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	taskID := srv.SubmitTask(t, "write code", "coder", []string{"github"})

	_, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for task dispatch")
	}

	// Report progress
	client.SendTaskProgress(taskID, "running", 50, "halfway done")

	// Server should still show it as running
	time.Sleep(200 * time.Millisecond)
	task := srv.Scheduler().GetTask(taskID)
	if task == nil {
		t.Fatal("task not found")
	}
	if task.Status != reef.TaskRunning {
		t.Errorf("status=%s, want Running", task.Status)
	}
}

// ---------------------------------------------------------------------------
// 4. Task Failure & Escalation
// ---------------------------------------------------------------------------

func TestE2E_FailedTaskReassignedToAnotherClient(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	clientA := NewMockClient(t, MockClientOptions{
		ClientID: "coder-a",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 2,
	})
	clientB := NewMockClient(t, MockClientOptions{
		ClientID: "coder-b",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 2,
	})

	if err := clientA.Connect(srv.WSURL()); err != nil {
		t.Fatalf("clientA connect: %v", err)
	}
	if err := clientB.Connect(srv.WSURL()); err != nil {
		t.Fatalf("clientB connect: %v", err)
	}
	defer clientA.Close()
	defer clientB.Close()

	_, _ = clientA.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)
	_, _ = clientB.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	taskID := srv.SubmitTask(t, "write code", "coder", []string{"github"})

	// First dispatch goes to one of the two clients
	var firstClient, secondClient *MockClient
	select {
	case msg := <-clientA.Messages():
		if msg.MsgType == reef.MsgTaskDispatch {
			firstClient = clientA
			secondClient = clientB
		}
	case msg := <-clientB.Messages():
		if msg.MsgType == reef.MsgTaskDispatch {
			firstClient = clientB
			secondClient = clientA
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for first dispatch")
	}

	// First client reports failure
	firstClient.SendTaskFailed(taskID, "execution_error", "compilation failed")

	// Task should be reassigned to the other client
	payload, ok := secondClient.WaitForTaskDispatch(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for reassigned task dispatch")
	}
	if payload.TaskID != taskID {
		t.Errorf("task_id=%s, want %s", payload.TaskID, taskID)
	}
}

func TestE2E_TaskEscalatedAfterMaxRetries(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	// Two clients, max_escalations=1 (default)
	clientA := NewMockClient(t, MockClientOptions{
		ClientID: "coder-a",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 2,
	})
	clientB := NewMockClient(t, MockClientOptions{
		ClientID: "coder-b",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 2,
	})
	if err := clientA.Connect(srv.WSURL()); err != nil {
		t.Fatalf("clientA connect: %v", err)
	}
	if err := clientB.Connect(srv.WSURL()); err != nil {
		t.Fatalf("clientB connect: %v", err)
	}
	defer clientA.Close()
	defer clientB.Close()
	_, _ = clientA.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)
	_, _ = clientB.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	taskID := srv.SubmitTask(t, "write code", "coder", []string{"github"})

	// First dispatch goes to clientA or clientB
	var firstClient, secondClient *MockClient
	select {
	case msg := <-clientA.Messages():
		if msg.MsgType == reef.MsgTaskDispatch {
			firstClient = clientA
			secondClient = clientB
		}
	case msg := <-clientB.Messages():
		if msg.MsgType == reef.MsgTaskDispatch {
			firstClient = clientB
			secondClient = clientA
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for first dispatch")
	}

	// First client fails -> reassigned to second client (escalation count = 1)
	firstClient.SendTaskFailed(taskID, "execution_error", "first failure")

	payload, ok := secondClient.WaitForTaskDispatch(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for reassigned dispatch to second client")
	}
	if payload.TaskID != taskID {
		t.Fatalf("expected task %s, got %s", taskID, payload.TaskID)
	}

	// Second client also fails -> escalation count already = 1, max = 1 -> escalate to admin
	secondClient.SendTaskFailed(taskID, "execution_error", "second failure")

	// Should be escalated now
	ts := srv.WaitForTaskStatus(t, taskID, reef.TaskEscalated, 3*time.Second)
	if ts == nil {
		t.Fatal("task should be escalated after max retries")
	}
}

// ---------------------------------------------------------------------------
// 5. Task Lifecycle Control
// ---------------------------------------------------------------------------

func TestE2E_ServerCancelsRunningTask(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	client := NewMockClient(t, MockClientOptions{Role: "coder", Skills: []string{"github"}})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	taskID := srv.SubmitTask(t, "write code", "coder", []string{"github"})

	_, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for task dispatch")
	}

	// Server sends cancel
	cancelMsg, _ := reef.NewMessage(reef.MsgCancel, taskID, reef.ControlPayload{
		ControlType: "cancel",
		TaskID:      taskID,
	})
	_ = srv.WSServer().SendMessage(client.clientID, cancelMsg)

	// Client should receive cancel (buffered by server for disconnected, but client is connected)
	msg, ok := client.WaitForMessage(reef.MsgCancel, 2*time.Second)
	if !ok {
		t.Fatal("timeout waiting for cancel message")
	}
	if msg.TaskID != taskID {
		t.Errorf("cancel task_id=%s, want %s", msg.TaskID, taskID)
	}
}

func TestE2E_ServerPausesAndResumesTask(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	client := NewMockClient(t, MockClientOptions{Role: "coder", Skills: []string{"github"}})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	taskID := srv.SubmitTask(t, "write code", "coder", []string{"github"})

	_, ok := client.WaitForTaskDispatch(3 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for task dispatch")
	}

	// Send pause
	pauseMsg, _ := reef.NewMessage(reef.MsgPause, taskID, reef.ControlPayload{
		ControlType: "pause",
		TaskID:      taskID,
	})
	_ = srv.WSServer().SendMessage(client.clientID, pauseMsg)

	msg, ok := client.WaitForMessage(reef.MsgPause, 2*time.Second)
	if !ok {
		t.Fatal("timeout waiting for pause message")
	}
	if msg.TaskID != taskID {
		t.Errorf("pause task_id=%s, want %s", msg.TaskID, taskID)
	}

	// Send resume
	resumeMsg, _ := reef.NewMessage(reef.MsgResume, taskID, reef.ControlPayload{
		ControlType: "resume",
		TaskID:      taskID,
	})
	_ = srv.WSServer().SendMessage(client.clientID, resumeMsg)

	msg, ok = client.WaitForMessage(reef.MsgResume, 2*time.Second)
	if !ok {
		t.Fatal("timeout waiting for resume message")
	}
	if msg.TaskID != taskID {
		t.Errorf("resume task_id=%s, want %s", msg.TaskID, taskID)
	}
}

// ---------------------------------------------------------------------------
// 6. Connection Resilience
// ---------------------------------------------------------------------------

func TestE2E_ClientReconnectsAfterDisconnection(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	clientID := "coder-reconnect"
	client := NewMockClient(t, MockClientOptions{
		ClientID: clientID,
		Role:     "coder",
		Skills:   []string{"github"},
	})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Disconnect
	client.Close()

	// Wait for server to mark disconnected
	waitFor(t, 3*time.Second, func() bool {
		status := srv.GetStatus(t)
		return status.DisconnectedCount >= 1 || status.StaleCount >= 1
	})

	// Reconnect with same client ID
	client2 := NewMockClient(t, MockClientOptions{
		ClientID: clientID,
		Role:     "coder",
		Skills:   []string{"github"},
	})
	if err := client2.Connect(srv.WSURL()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer client2.Close()

	msg, ok := client2.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)
	if !ok {
		t.Fatal("timeout waiting for register_ack after reconnect")
	}
	var ack reef.RegisterAckPayload
	_ = msg.DecodePayload(&ack)
	if ack.ClientID != clientID {
		t.Errorf("client_id=%s, want %s", ack.ClientID, clientID)
	}

	// Server should show 1 connected again
	waitFor(t, 2*time.Second, func() bool {
		status := srv.GetStatus(t)
		return len(status.ConnectedClients) == 1
	})
}

// ---------------------------------------------------------------------------
// 7. Admin API
// ---------------------------------------------------------------------------

func TestE2E_AdminStatusReflectsSystemState(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	// Empty server
	status := srv.GetStatus(t)
	if len(status.ConnectedClients) != 0 {
		t.Fatalf("expected 0 clients, got %d", len(status.ConnectedClients))
	}
	if status.UptimeMs <= 0 {
		t.Fatal("uptime should be > 0")
	}

	// Add clients and a task
	client := NewMockClient(t, MockClientOptions{Role: "coder", Skills: []string{"github"}})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	_ = srv.SubmitTask(t, "write code", "coder", []string{"github"})

	waitFor(t, 2*time.Second, func() bool {
		status = srv.GetStatus(t)
		return len(status.ConnectedClients) == 1
	})

	status = srv.GetStatus(t)
	if len(status.ConnectedClients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(status.ConnectedClients))
	}
}

func TestE2E_AdminTasksFilteredByRole(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	// Submit tasks for different roles
	taskCoder := srv.SubmitTask(t, "write code", "coder", []string{"github"})
	taskAnalyst := srv.SubmitTask(t, "analyze data", "analyst", []string{"web_fetch"})

	// Filter by coder
	tasks := srv.GetTasks(t, "coder", "")
	foundCoder := false
	for _, ts := range tasks.QueuedTasks {
		if ts.TaskID == taskCoder {
			foundCoder = true
		}
		if ts.TaskID == taskAnalyst {
			t.Fatal("analyst task should not appear in coder filter")
		}
	}
	if !foundCoder {
		t.Fatal("coder task should appear in coder filter")
	}
}

func TestE2E_AdminSubmitTask(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	taskID := srv.SubmitTask(t, "write tests", "coder", []string{"github"})
	if taskID == "" {
		t.Fatal("expected non-empty task_id")
	}

	// Verify task exists
	task := srv.Scheduler().GetTask(taskID)
	if task == nil {
		t.Fatal("task should exist in scheduler")
	}
	if task.RequiredRole != "coder" {
		t.Errorf("role=%s, want coder", task.RequiredRole)
	}
}

// ============================================================================
// Reef v1.1 E2E Tests
// ============================================================================

func TestE2E_AdminAPI_Auth_ValidToken(t *testing.T) {
	srv := NewE2EServer(t, "secret123")
	defer srv.Shutdown(t)

	// Valid token should succeed
	status := srv.GetStatus(t)
	if status.ServerVersion == "" {
		t.Fatal("expected non-empty server_version")
	}
}

func TestE2E_AdminAPI_Auth_InvalidToken(t *testing.T) {
	srv := NewE2EServer(t, "secret123")
	defer srv.Shutdown(t)

	resp := srv.GetStatusRaw(t, "Bearer wrong-token")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestE2E_AdminAPI_Auth_MissingToken(t *testing.T) {
	srv := NewE2EServer(t, "secret123")
	defer srv.Shutdown(t)

	resp := srv.GetStatusRaw(t, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestE2E_AdminAPI_Auth_NoTokenConfigured(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	// No token configured — auth is skipped
	resp := srv.GetStatusRaw(t, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestE2E_AdminAPI_Auth_AllEndpoints(t *testing.T) {
	srv := NewE2EServer(t, "secret123")
	defer srv.Shutdown(t)

	endpoints := []string{"/admin/status", "/admin/tasks", "/tasks"}
	for _, ep := range endpoints {
		method := http.MethodGet
		if ep == "/tasks" {
			method = http.MethodPost
		}
		req, _ := http.NewRequest(method, srv.AdminURL()+ep, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", ep, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", ep, resp.StatusCode)
		}
	}
}

func TestE2E_Webhook_TaskEscalation(t *testing.T) {
	// Set up a mock webhook server
	webhookReceived := make(chan []byte, 1)
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		webhookReceived <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	// Create server with webhook URL and max_escalations=1
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
	}
	srv := server.NewServer(cfg, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer srv.Stop()
	time.Sleep(100 * time.Millisecond)

	e2e := &E2EServer{
		Server:    srv,
		WSAddr:    cfg.WebSocketAddr,
		AdminAddr: cfg.AdminAddr,
	}

	// Register two clients so escalation can reassign
	clientA := NewMockClient(t, MockClientOptions{
		ClientID: "failer-a",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 1,
	})
	clientB := NewMockClient(t, MockClientOptions{
		ClientID: "failer-b",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 1,
	})
	if err := clientA.Connect(e2e.WSURL()); err != nil {
		t.Fatalf("connect A: %v", err)
	}
	if err := clientB.Connect(e2e.WSURL()); err != nil {
		t.Fatalf("connect B: %v", err)
	}
	defer clientA.Close()
	defer clientB.Close()
	_, _ = clientA.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)
	_, _ = clientB.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Submit task
	taskID := e2e.SubmitTask(t, "this will fail", "coder", []string{"github"})

	// First dispatch goes to one client
	var firstClient, secondClient *MockClient
	select {
	case msg := <-clientA.Messages():
		if msg.MsgType == reef.MsgTaskDispatch {
			firstClient = clientA
			secondClient = clientB
		}
	case msg := <-clientB.Messages():
		if msg.MsgType == reef.MsgTaskDispatch {
			firstClient = clientB
			secondClient = clientA
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for first dispatch")
	}

	// First client fails → reassigned to second
	firstClient.SendTaskFailed(taskID, "execution_error", "first failure")

	// Second client gets the reassignment
	_, ok := secondClient.WaitForTaskDispatch(3*time.Second)
	if !ok {
		t.Fatal("timeout waiting for reassignment to second client")
	}

	// Second client also fails → escalation count = 1, max = 1 → escalate
	secondClient.SendTaskFailed(taskID, "execution_error", "second failure")

	// Wait for escalation
	e2e.WaitForTaskStatus(t, taskID, reef.TaskEscalated, 5*time.Second)

	// Verify webhook was called
	select {
	case body := <-webhookReceived:
		var payload notify.Alert
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode webhook payload: %v", err)
		}
		if payload.Event != "task_escalated" {
			t.Errorf("event=%s, want task_escalated", payload.Event)
		}
		if payload.TaskID != taskID {
			t.Errorf("task_id=%s, want %s", payload.TaskID, taskID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook not received within timeout")
	}
}

func TestE2E_TaskWithModelHint(t *testing.T) {
	srv := NewE2EServer(t, "")
	defer srv.Shutdown(t)

	// Register a client
	client := NewMockClient(t, MockClientOptions{
		ClientID: "coder-1",
		Role:     "coder",
		Skills:   []string{"github"},
		Capacity: 3,
	})
	if err := client.Connect(srv.WSURL()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()
	_, _ = client.WaitForMessage(reef.MsgRegisterAck, 2*time.Second)

	// Submit task with model_hint
	taskID := srv.SubmitTaskWithModelHint(t, "write code", "coder", []string{"github"}, "gpt-4o")

	// Verify task has model_hint in scheduler
	task := srv.Scheduler().GetTask(taskID)
	if task == nil {
		t.Fatal("task should exist")
	}
	if task.ModelHint != "gpt-4o" {
		t.Errorf("model_hint=%s, want gpt-4o", task.ModelHint)
	}

	// Wait for dispatch and verify payload includes model_hint
	payload, ok := client.WaitForTaskDispatch(3*time.Second)
	if !ok {
		t.Fatal("timeout waiting for dispatch")
	}
	if payload.ModelHint != "gpt-4o" {
		t.Errorf("dispatch model_hint=%s, want gpt-4o", payload.ModelHint)
	}
}
