package server

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

// ---------------------------------------------------------------------------
// Extended mock with SendToClient error capability
// ---------------------------------------------------------------------------

type mockConnManagerErr struct {
	mu        sync.Mutex
	messages  map[string][]reef.Message
	sendErrOn map[string]error // clientID → error to return
}

func newMockConnManagerErr() *mockConnManagerErr {
	return &mockConnManagerErr{
		messages:  make(map[string][]reef.Message),
		sendErrOn: make(map[string]error),
	}
}

func (m *mockConnManagerErr) SendToClient(clientID string, msg reef.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.sendErrOn[clientID]; ok {
		return err
	}
	m.messages[clientID] = append(m.messages[clientID], msg)
	return nil
}

func (m *mockConnManagerErr) messagesFor(clientID string) []reef.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages[clientID]
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeDisconnectedClient(id, role string, skills []string, capacity, load int) *reef.ClientInfo {
	return &reef.ClientInfo{
		ID:          id,
		Role:        role,
		Skills:      skills,
		Capacity:    capacity,
		CurrentLoad: load,
		State:       reef.ClientDisconnected,
	}
}

func makeFullClient(id, role string, skills []string, capacity int) *reef.ClientInfo {
	return &reef.ClientInfo{
		ID:          id,
		Role:        role,
		Skills:      skills,
		Capacity:    capacity,
		CurrentLoad: capacity, // at capacity
		State:       reef.ClientConnected,
	}
}

// ---------------------------------------------------------------------------
// Post: nil scheduler
// ---------------------------------------------------------------------------

func TestPost_NilScheduler(t *testing.T) {
	reg := newMockRegistry()
	conn := newMockConnManager()
	cb := NewClaimBoard(nil, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "test", "builder", nil, 3)
	err := cb.Post(task)
	if err == nil {
		t.Fatal("expected error for nil scheduler")
	}
	if err.Error() != "claim board: scheduler is nil, cannot post task" {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Post: long instruction (>200 chars) gets truncated
// ---------------------------------------------------------------------------

func TestPost_LongInstruction(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	longInstr := strings.Repeat("x", 300)
	task := makeTask("t1", longInstr, "builder", nil, 3)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	msgs := conn.messagesFor("c1")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var payload reef.TaskAvailablePayload
	if err := msgs[0].DecodePayload(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Instruction) != 200 {
		t.Errorf("expected instruction truncated to 200, got %d", len(payload.Instruction))
	}
	if !strings.HasPrefix(longInstr, payload.Instruction) {
		t.Errorf("truncated instruction should be prefix of original")
	}
}

// ---------------------------------------------------------------------------
// Post: high priority → scheduler submit error
// ---------------------------------------------------------------------------

func TestPost_HighPrioritySubmitError(t *testing.T) {
	sched := newMockScheduler()
	sched.submitErr = fmt.Errorf("scheduler overloaded")
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "urgent", "builder", nil, 8)
	err := cb.Post(task)
	if err == nil {
		t.Fatal("expected error from scheduler submit")
	}
	if err.Error() != "scheduler overloaded" {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// findEligibleCandidates: disconnected clients filtered out
// ---------------------------------------------------------------------------

func TestFindEligibleCandidates_Disconnected(t *testing.T) {
	sched := newMockScheduler()
	online := makeOnlineClient("c1", "builder", nil, 2, 0)
	offline := makeDisconnectedClient("c2", "builder", nil, 2, 0)
	reg := newMockRegistry(online, offline)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task", "builder", nil, 3)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Only online client should receive notification
	c2Msgs := conn.messagesFor("c2")
	if len(c2Msgs) > 0 {
		t.Errorf("disconnected client c2 should not receive task_available, got %d", len(c2Msgs))
	}
	c1Msgs := conn.messagesFor("c1")
	if len(c1Msgs) != 1 {
		t.Errorf("online client c1 should receive task_available, got %d", len(c1Msgs))
	}
}

// ---------------------------------------------------------------------------
// findEligibleCandidates: at-capacity clients filtered out
// ---------------------------------------------------------------------------

func TestFindEligibleCandidates_AtCapacity(t *testing.T) {
	sched := newMockScheduler()
	free := makeOnlineClient("c1", "builder", nil, 2, 0)
	full := makeFullClient("c2", "builder", nil, 2)
	reg := newMockRegistry(free, full)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task", "builder", nil, 3)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	c2Msgs := conn.messagesFor("c2")
	if len(c2Msgs) > 0 {
		t.Errorf("full-capacity client c2 should not receive task_available, got %d", len(c2Msgs))
	}
	c1Msgs := conn.messagesFor("c1")
	if len(c1Msgs) != 1 {
		t.Errorf("free client c1 should receive task_available, got %d", len(c1Msgs))
	}
}

// ---------------------------------------------------------------------------
// findEligibleCandidates: no clients with matching role
// ---------------------------------------------------------------------------

func TestFindEligibleCandidates_NoMatchingRole(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "reviewer", nil, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 100 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task", "builder", nil, 3)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// No notifications since no client matches the role
	if conn.messagesFor("c1") != nil && len(conn.messagesFor("c1")) > 0 {
		t.Errorf("client c1 should not receive notification (wrong role)")
	}
}

// ---------------------------------------------------------------------------
// findEligibleCandidates: client not available (IsAvailable false via overload)
// ---------------------------------------------------------------------------

func TestFindEligibleCandidates_NotAvailable(t *testing.T) {
	sched := newMockScheduler()
	// Capacity 2, Load 2 → not available
	overloaded := &reef.ClientInfo{
		ID:          "c1",
		Role:        "builder",
		Capacity:    2,
		CurrentLoad: 2,
		State:       reef.ClientConnected,
	}
	reg := newMockRegistry(overloaded)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task", "builder", nil, 3)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// No notifications since client is not available
	if len(conn.messagesFor("c1")) > 0 {
		t.Errorf("overloaded client should not receive task_available, got %d", len(conn.messagesFor("c1")))
	}
}

// ---------------------------------------------------------------------------
// HandleClaim: client not found in registry
// ---------------------------------------------------------------------------

func TestHandleClaim_ClientNotFound(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry() // empty
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "task", "builder", nil, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	err := cb.HandleClaim("t1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for client not found")
	}
	if !strings.Contains(err.Error(), "client not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HandleClaim: client offline
// ---------------------------------------------------------------------------

func TestHandleClaim_ClientOffline(t *testing.T) {
	sched := newMockScheduler()
	offline := makeDisconnectedClient("c1", "builder", nil, 2, 0)
	reg := newMockRegistry(offline)
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "task", "builder", nil, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	err := cb.HandleClaim("t1", "c1")
	if err == nil {
		t.Fatal("expected error for offline client")
	}
	if !strings.Contains(err.Error(), "not online") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HandleClaim: role mismatch
// ---------------------------------------------------------------------------

func TestHandleClaim_RoleMismatch(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "reviewer", nil, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "task", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	err := cb.HandleClaim("t1", "c1")
	if err == nil {
		t.Fatal("expected error for role mismatch")
	}
	if !strings.Contains(err.Error(), "role mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HandleClaim: skills mismatch
// ---------------------------------------------------------------------------

func TestHandleClaim_SkillsMismatch(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", []string{"python"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "task", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	err := cb.HandleClaim("t1", "c1")
	if err == nil {
		t.Fatal("expected error for skills mismatch")
	}
	if !strings.Contains(err.Error(), "missing required skills") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetTask: task not found
// ---------------------------------------------------------------------------

func TestGetTask_NotFound(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	result := cb.GetTask("nonexistent")
	if result != nil {
		t.Errorf("expected nil for nonexistent task, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// Cancel: task not on board
// ---------------------------------------------------------------------------

func TestCancel_NotOnBoard(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	// Cancel non-existent task should not panic
	cb.Cancel("nonexistent")
	if cb.BoardSize() != 0 {
		t.Errorf("board should be empty")
	}
}

// ---------------------------------------------------------------------------
// Cancel: expiry timer is nil
// ---------------------------------------------------------------------------

func TestCancel_NoExpiryTimer(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "test", "builder", nil, 3)

	// Insert a task directly without timer
	cb.mu.Lock()
	cb.tasks["t1"] = &claimableTask{
		Task:         task,
		PostedAt:     time.Now(),
		ExpiryTimer:  nil, // explicitly nil
		ExpiryRetries: 0,
	}
	cb.mu.Unlock()

	if cb.BoardSize() != 1 {
		t.Fatalf("expected 1 task on board")
	}

	// Cancel should not panic when timer is nil
	cb.Cancel("t1")

	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board after cancel")
	}
}

// ---------------------------------------------------------------------------
// startExpiryTimer: task not on board (direct call)
// ---------------------------------------------------------------------------

func TestStartExpiryTimer_TaskNotFound(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	// Call directly on non-existent task — should not panic
	cb.startExpiryTimer("nonexistent")
	// No assertion needed; test passes if no panic
}

// ---------------------------------------------------------------------------
// expiryTimer: task already claimed (direct call)
// ---------------------------------------------------------------------------

func TestExpiryTimer_AlreadyClaimed(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, ClaimConfig{
		ClaimTimeout:       500 * time.Millisecond,
		MaxRetriesOnExpiry: 2,
	}, nil)

	task := makeTask("t1", "test", "builder", nil, 3)

	// Manually insert a claimed task
	cb.mu.Lock()
	cb.tasks["t1"] = &claimableTask{
		Task:      task,
		PostedAt:  time.Now(),
		ClaimedBy: "c1", // already claimed
	}
	cb.mu.Unlock()

	// expiryTimer should detect the claim and return early
	cb.expiryTimer("t1")

	// No scheduler submission should happen
	if sched.submittedCount() != 0 {
		t.Errorf("expected 0 scheduler submissions (task already claimed), got %d", sched.submittedCount())
	}
}

// ---------------------------------------------------------------------------
// expiryTimer: task not on board (direct call)
// ---------------------------------------------------------------------------

func TestExpiryTimer_TaskNotFound(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	// Direct call on non-existent task — should not panic
	cb.expiryTimer("nonexistent")

	if sched.submittedCount() != 0 {
		t.Errorf("expected 0 submissions, got %d", sched.submittedCount())
	}
}

// ---------------------------------------------------------------------------
// expiryTimer: scheduler submit error on fallback (direct call)
// ---------------------------------------------------------------------------

func TestExpiryTimer_FallbackSubmitError(t *testing.T) {
	sched := newMockScheduler()
	sched.submitErr = fmt.Errorf("scheduler down")
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, ClaimConfig{
		ClaimTimeout:       30 * time.Second,
		MaxRetriesOnExpiry: 1, // 1 retry, so expiryTimer is called first as retry, then as fallback
	}, nil)

	task := makeTask("t1", "test", "builder", nil, 3)

	// Insert task that has already had 1 retry (so next expiry == fallback)
	cb.mu.Lock()
	cb.tasks["t1"] = &claimableTask{
		Task:          task,
		PostedAt:      time.Now(),
		ExpiryRetries: 1, // already at max retries
	}
	cb.mu.Unlock()

	// This should attempt fallback → scheduler.Submit fails
	cb.expiryTimer("t1")

	// Task should be removed from board
	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board after fallback, got %d", cb.BoardSize())
	}
}

// ---------------------------------------------------------------------------
// repostFromExpiry: task was claimed or cancelled between trigger and call
// ---------------------------------------------------------------------------

func TestRepostFromExpiry_TaskGone(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "test", "builder", nil, 3)

	// Create a claimableTask with pending retry count, but don't put it on the board
	ct := &claimableTask{
		Task:          task,
		PostedAt:      time.Now(),
		ExpiryRetries: 1,
	}

	// repostFromExpiry should detect the task is not on board and return
	cb.repostFromExpiry(ct)

	// No new task should appear on board
	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board, got %d", cb.BoardSize())
	}
}

func TestRepostFromExpiry_TaskAlreadyClaimed(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "test", "builder", nil, 3)

	// Put a CLAIMED task on the board
	cb.mu.Lock()
	cb.tasks["t1"] = &claimableTask{
		Task:          task,
		PostedAt:      time.Now(),
		ClaimedBy:     "c1",
		ExpiryRetries: 0,
	}
	cb.mu.Unlock()

	ct := &claimableTask{
		Task:          task,
		ExpiryRetries: 1,
	}

	// repostFromExpiry should see the task is claimed and return
	cb.repostFromExpiry(ct)

	// Board should still have the claimed task
	if cb.BoardSize() != 1 {
		t.Errorf("expected 1 task still on board, got %d", cb.BoardSize())
	}
}

// ---------------------------------------------------------------------------
// repostFromExpiry: with candidates (indirect test via expiry flow)
// ---------------------------------------------------------------------------

func TestRepostFromExpiry_WithCandidates(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", nil, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 30 * time.Millisecond
	cfg.MaxRetriesOnExpiry = 2
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "test retry", "builder", nil, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	// Wait for first expiry → repost
	time.Sleep(80 * time.Millisecond)

	// Client should have received task_available again via repost
	msgs := conn.messagesFor("c1")
	taskAvailableCount := 0
	for _, m := range msgs {
		if m.MsgType == reef.MsgTaskAvailable {
			taskAvailableCount++
		}
	}
	if taskAvailableCount < 2 {
		t.Errorf("expected at least 2 task_available messages (initial + repost), got %d", taskAvailableCount)
	}
}

// ---------------------------------------------------------------------------
// notifyClaimed: task already removed from board (direct call)
// ---------------------------------------------------------------------------

func TestNotifyClaimed_TaskNotOnBoard(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	// Call notifyClaimed on a task that doesn't exist — should not panic
	cb.notifyClaimed("nonexistent", "c1")
}

// ---------------------------------------------------------------------------
// notifyClaimed: SendToClient error path (via goroutine in HandleClaim)
// ---------------------------------------------------------------------------

func TestNotifyClaimed_SendError(t *testing.T) {
	sched := newMockScheduler()
	client1 := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	client2 := makeOnlineClient("c2", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client1, client2)
	conn := newMockConnManagerErr()
	// c2 will fail on SendToClient
	conn.sendErrOn["c2"] = fmt.Errorf("connection lost")

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "task", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// c1 claims → notifyClaimed goroutine tries to send to c2, fails gracefully
	if err := cb.HandleClaim("t1", "c1"); err != nil {
		t.Fatalf("HandleClaim failed: %v", err)
	}

	// Wait for goroutine
	time.Sleep(50 * time.Millisecond)

	// c1 should have been dispatched successfully
	if dispatches, ok := sched.dispatched["c1"]; !ok || len(dispatches) != 1 {
		t.Errorf("c1 should have been dispatched")
	}
}

// ---------------------------------------------------------------------------
// notifyClaimed: valid path with task_claimed message verification
// ---------------------------------------------------------------------------

func TestNotifyClaimed_VerifyPayload(t *testing.T) {
	sched := newMockScheduler()
	client1 := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	client2 := makeOnlineClient("c2", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client1, client2)
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "task", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if err := cb.HandleClaim("t1", "c1"); err != nil {
		t.Fatalf("HandleClaim failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// c2 should have received task_claimed
	c2Msgs := conn.messagesFor("c2")
	found := false
	for _, msg := range c2Msgs {
		if msg.MsgType == reef.MsgTaskClaimed {
			var p reef.TaskClaimedPayload
			if err := msg.DecodePayload(&p); err != nil {
				continue
			}
			if p.TaskID == "t1" && p.ClaimedBy == "c1" && p.ClaimedAt > 0 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("c2 should receive valid task_claimed message")
	}
}

// ---------------------------------------------------------------------------
// Post: SendToClient error path in notify candidates
// ---------------------------------------------------------------------------

func TestPost_SendToClientError(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManagerErr()
	conn.sendErrOn["c1"] = fmt.Errorf("send failed")

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "test", "builder", nil, 3)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Task should still be on board (send error doesn't abort posting)
	if cb.BoardSize() != 1 {
		t.Errorf("expected 1 task on board, got %d", cb.BoardSize())
	}
}

// ---------------------------------------------------------------------------
// Post: instruction exactly at boundary (200 chars) — no truncation
// ---------------------------------------------------------------------------

func TestPost_InstructionAtBoundary(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", nil, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	exact200 := strings.Repeat("y", 200)
	task := makeTask("t1", exact200, "builder", nil, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	msgs := conn.messagesFor("c1")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var payload reef.TaskAvailablePayload
	if err := msgs[0].DecodePayload(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Instruction) != 200 {
		t.Errorf("expected instruction length 200, got %d", len(payload.Instruction))
	}
}

// ---------------------------------------------------------------------------
// expiryTimer: retry path (not fallback) with repost
// ---------------------------------------------------------------------------

func TestExpiryTimer_RetryPath(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, ClaimConfig{
		ClaimTimeout:       30 * time.Second,
		MaxRetriesOnExpiry: 3,
	}, nil)

	task := makeTask("t1", "test", "builder", nil, 3)

	// Insert with 0 retries, maxRetries=3 → will retry, not fallback
	cb.mu.Lock()
	cb.tasks["t1"] = &claimableTask{
		Task:          task,
		PostedAt:      time.Now(),
		ExpiryRetries: 0,
	}
	cb.mu.Unlock()

	cb.expiryTimer("t1")

	// retry count should have been incremented, task still on board
	if sched.submittedCount() > 0 {
		t.Errorf("expected 0 scheduler submissions (should retry, not fallback), got %d", sched.submittedCount())
	}

	cb.mu.Lock()
	ct := cb.tasks["t1"]
	cb.mu.Unlock()
	if ct == nil {
		t.Fatal("task should still be on board after retry")
	}
	if ct.ExpiryRetries != 1 {
		t.Errorf("expected retry count 1, got %d", ct.ExpiryRetries)
	}
}

// ---------------------------------------------------------------------------
// expiryTimer: exhausted retries → fallback submit succeeds
// ---------------------------------------------------------------------------

func TestExpiryTimer_FallbackSuccess(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, ClaimConfig{
		ClaimTimeout:       30 * time.Second,
		MaxRetriesOnExpiry: 2,
	}, nil)

	task := makeTask("t1", "test", "builder", nil, 3)

	// Insert with retries = MaxRetriesOnExpiry (already exhausted)
	cb.mu.Lock()
	cb.tasks["t1"] = &claimableTask{
		Task:          task,
		PostedAt:      time.Now(),
		ExpiryRetries: 2, // at max
	}
	cb.mu.Unlock()

	cb.expiryTimer("t1")

	if sched.submittedCount() != 1 {
		t.Errorf("expected 1 scheduler submission on fallback, got %d", sched.submittedCount())
	}
	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board after fallback submit, got %d", cb.BoardSize())
	}
}

// ---------------------------------------------------------------------------
// Stop: multiple calls
// ---------------------------------------------------------------------------

func TestStop_MultipleCalls(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "test", "builder", nil, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	cb.Stop()
	// Second Stop should not panic (stopCh already closed)
	cb.Stop()

	if cb.BoardSize() != 0 {
		t.Errorf("board should be empty after stop")
	}
}

// ---------------------------------------------------------------------------
// HandleClaim: DispatchTask fails, repost succeeds
// ---------------------------------------------------------------------------

func TestHandleClaim_DispatchFailRepostSucceeds(t *testing.T) {
	sched := newMockScheduler()
	sched.dispatchErr = fmt.Errorf("dispatch failed")
	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	err := cb.HandleClaim("t1", "c1")
	if err == nil {
		t.Fatal("expected dispatch error")
	}
	if sched.dispatchErr == nil || err.Error() != "dispatch failed" {
		t.Errorf("expected dispatch failed error, got: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Repost task "t1-repost" should be on board
	if cb.GetTask("t1-repost") == nil {
		t.Errorf("expected t1-repost on board after dispatch failure")
	}
}

// ---------------------------------------------------------------------------
// Concurrent access: Post + Cancel race
// ---------------------------------------------------------------------------

func TestRace_PostAndCancel(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("t%d", i)
		task := makeTask(id, "task", "builder", nil, 1)
		if err := cb.Post(task); err != nil {
			t.Fatalf("Post failed: %v", err)
		}
	}

	// Concurrently cancel tasks
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("t%d", idx)
			cb.Cancel(id)
		}(i)
	}
	wg.Wait()

	// After all cancels, board should be empty
	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board, got %d", cb.BoardSize())
	}
}

// ---------------------------------------------------------------------------
// Concurrent claims with many clients
// ---------------------------------------------------------------------------

func TestRace_ConcurrentClaimsMany(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	for i := 0; i < 20; i++ {
		reg.addClient(makeOnlineClient(
			fmt.Sprintf("c%d", i), "builder", []string{"go"}, 2, 0,
		))
	}
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t-race", "race task", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	var wg sync.WaitGroup
	successCount := &sync.Mutex{}
	successes := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := cb.HandleClaim("t-race", fmt.Sprintf("c%d", idx))
			if err == nil {
				successCount.Lock()
				successes++
				successCount.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d", successes)
	}
}

// ---------------------------------------------------------------------------
// Board size: Post max then cancel all
// ---------------------------------------------------------------------------

func TestBoardFull_PostAndEmpty(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.MaxBoardSize = 3
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	for i := 0; i < 3; i++ {
		task := makeTask(fmt.Sprintf("t%d", i), "task", "builder", nil, 1)
		if err := cb.Post(task); err != nil {
			t.Fatalf("Post %d failed: %v", i, err)
		}
	}

	if cb.BoardSize() != 3 {
		t.Errorf("expected 3 tasks, got %d", cb.BoardSize())
	}

	// 4th should fail
	task4 := makeTask("t4", "task", "builder", nil, 1)
	err := cb.Post(task4)
	if err == nil {
		t.Error("expected board full error")
	}

	// Cancel all
	for i := 0; i < 3; i++ {
		cb.Cancel(fmt.Sprintf("t%d", i))
	}

	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board, got %d", cb.BoardSize())
	}
}

// ---------------------------------------------------------------------------
// repostFromExpiry with candidates (sends task_available)
// ---------------------------------------------------------------------------

func TestRepostFromExpiry_SendsTaskAvailable(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", nil, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	task := makeTask("t1", "retry task", "builder", nil, 3)

	// Put task on board
	cb.mu.Lock()
	cb.tasks["t1"] = &claimableTask{
		Task:          task,
		PostedAt:      time.Now(),
		ExpiryRetries: 0,
	}
	cb.mu.Unlock()

	ct := &claimableTask{
		Task:          task,
		ExpiryRetries: 1, // will be preserved
	}

	cb.repostFromExpiry(ct)

	time.Sleep(50 * time.Millisecond)

	// Client should receive task_available
	msgs := conn.messagesFor("c1")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].MsgType != reef.MsgTaskAvailable {
		t.Errorf("expected MsgTaskAvailable, got %s", msgs[0].MsgType)
	}

	// Verify retry count was preserved
	cb.mu.Lock()
	existing := cb.tasks["t1"]
	cb.mu.Unlock()
	if existing != nil && existing.ExpiryRetries != 1 {
		t.Errorf("expected retry count 1, got %d", existing.ExpiryRetries)
	}
}

// ---------------------------------------------------------------------------
// repostFromExpiry: long instruction truncated
// ---------------------------------------------------------------------------

func TestRepostFromExpiry_LongInstruction(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", nil, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	longInstr := strings.Repeat("z", 300)
	task := makeTask("t1", longInstr, "builder", nil, 3)

	cb.mu.Lock()
	cb.tasks["t1"] = &claimableTask{
		Task:          task,
		PostedAt:      time.Now(),
		ExpiryRetries: 0,
	}
	cb.mu.Unlock()

	ct := &claimableTask{
		Task:          task,
		ExpiryRetries: 1,
	}

	cb.repostFromExpiry(ct)

	time.Sleep(50 * time.Millisecond)

	msgs := conn.messagesFor("c1")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	var payload reef.TaskAvailablePayload
	if err := msgs[0].DecodePayload(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Instruction) != 200 {
		t.Errorf("expected truncated instruction 200 chars, got %d", len(payload.Instruction))
	}
}

// ---------------------------------------------------------------------------
// Integration: verify board state after post and claim
// ---------------------------------------------------------------------------

func TestIntegration_BoardState(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	// Verify initial state
	if cb.BoardSize() != 0 {
		t.Errorf("initial board should be empty")
	}
	if cb.GetTask("any") != nil {
		t.Errorf("GetTask should return nil for empty board")
	}

	// Post
	task := makeTask("t1", "task", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	if cb.BoardSize() != 1 {
		t.Errorf("board should have 1 task")
	}

	got := cb.GetTask("t1")
	if got == nil {
		t.Fatal("GetTask should return task")
	}
	if got.ID != "t1" {
		t.Errorf("expected t1, got %s", got.ID)
	}
	if got.AssignedClient != "" {
		t.Errorf("unclaimed task should have empty AssignedClient")
	}

	// Claim
	if err := cb.HandleClaim("t1", "c1"); err != nil {
		t.Fatalf("HandleClaim: %v", err)
	}

	gotAfter := cb.GetTask("t1")
	if gotAfter == nil {
		t.Fatal("task should still be on board after claim")
	}
	if gotAfter.AssignedClient != "c1" {
		t.Errorf("expected AssignedClient c1, got %s", gotAfter.AssignedClient)
	}

	// Cancel after claim
	cb.Cancel("t1")
	if cb.GetTask("t1") != nil {
		t.Errorf("task should be gone after cancel")
	}
}

// ---------------------------------------------------------------------------
// HandleClaim: Dispatch fails AND repost fails (board full → postErr path)
// ---------------------------------------------------------------------------

func TestHandleClaim_DispatchFailRepostFails(t *testing.T) {
	sched := newMockScheduler()
	sched.dispatchErr = fmt.Errorf("dispatch failed")
	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.MaxBoardSize = 1
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	// Claim fails (dispatch error), then repost fails because board is at MaxBoardSize=1
	// The original task "t1" is still on the board, so len=1 which is >= MaxBoardSize
	err := cb.HandleClaim("t1", "c1")
	if err == nil {
		t.Fatal("expected dispatch error")
	}

	// Repost should have failed due to full board
	time.Sleep(50 * time.Millisecond)
	if cb.GetTask("t1-repost") != nil {
		t.Errorf("repost task should not be on board (board was full)")
	}
}
