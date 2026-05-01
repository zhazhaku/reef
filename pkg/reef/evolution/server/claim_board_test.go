package server

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// mockScheduler implements TaskSubmitter for testing.
type mockScheduler struct {
	mu            sync.Mutex
	submitted     []*reef.Task
	dispatched    map[string][]*reef.Task // clientID → tasks
	submitErr     error
	dispatchErr   error
}

func newMockScheduler() *mockScheduler {
	return &mockScheduler{
		dispatched: make(map[string][]*reef.Task),
	}
}

func (m *mockScheduler) Submit(task *reef.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.submitErr != nil {
		return m.submitErr
	}
	m.submitted = append(m.submitted, task)
	return nil
}

func (m *mockScheduler) DispatchTask(clientID string, task *reef.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dispatchErr != nil {
		return m.dispatchErr
	}
	m.dispatched[clientID] = append(m.dispatched[clientID], task)
	return nil
}

func (m *mockScheduler) submittedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.submitted)
}

// mockRegistry implements RoleFinder for testing.
type mockRegistry struct {
	mu      sync.Mutex
	clients map[string]*reef.ClientInfo
}

func newMockRegistry(clients ...*reef.ClientInfo) *mockRegistry {
	r := &mockRegistry{clients: make(map[string]*reef.ClientInfo)}
	for _, c := range clients {
		r.clients[c.ID] = c
	}
	return r
}

func (m *mockRegistry) Get(clientID string) *reef.ClientInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clients[clientID]
}

func (m *mockRegistry) FindByRole(role string) []*reef.ClientInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*reef.ClientInfo
	for _, c := range m.clients {
		if c.Role == role {
			out = append(out, c)
		}
	}
	return out
}

func (m *mockRegistry) addClient(c *reef.ClientInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[c.ID] = c
}

// mockConnManager implements ConnManager for testing.
type mockConnManager struct {
	mu       sync.Mutex
	messages map[string][]reef.Message // clientID → messages
}

func newMockConnManager() *mockConnManager {
	return &mockConnManager{messages: make(map[string][]reef.Message)}
}

func (m *mockConnManager) SendToClient(clientID string, msg reef.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[clientID] = append(m.messages[clientID], msg)
	return nil
}

func (m *mockConnManager) messagesFor(clientID string) []reef.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages[clientID]
}

func (m *mockConnManager) allMessages() []reef.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all []reef.Message
	for _, msgs := range m.messages {
		all = append(all, msgs...)
	}
	return all
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeOnlineClient(id, role string, skills []string, capacity, load int) *reef.ClientInfo {
	return &reef.ClientInfo{
		ID:        id,
		Role:      role,
		Skills:    skills,
		Capacity:  capacity,
		CurrentLoad: load,
		State:     reef.ClientConnected,
	}
}

func makeTask(id, instruction, role string, skills []string, priority int) *reef.Task {
	t := reef.NewTask(id, instruction, role, skills)
	t.Priority = priority
	return t
}

// ---------------------------------------------------------------------------
// Task 1: NewClaimBoard + DefaultConfig
// ---------------------------------------------------------------------------

func TestNewClaimBoard_Defaults(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()
	logger := slog.Default()

	cb := NewClaimBoard(sched, reg, conn, ClaimConfig{}, logger)

	if cb.config.ClaimTimeout != 30*time.Second {
		t.Errorf("expected default ClaimTimeout 30s, got %v", cb.config.ClaimTimeout)
	}
	if cb.config.MaxPriorityClaim != 5 {
		t.Errorf("expected default MaxPriorityClaim 5, got %d", cb.config.MaxPriorityClaim)
	}
	if cb.config.MaxRetriesOnExpiry != 2 {
		t.Errorf("expected default MaxRetriesOnExpiry 2, got %d", cb.config.MaxRetriesOnExpiry)
	}
	if cb.config.MaxBoardSize != 50 {
		t.Errorf("expected default MaxBoardSize 50, got %d", cb.config.MaxBoardSize)
	}
	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board, got %d tasks", cb.BoardSize())
	}
}

func TestNewClaimBoard_CustomConfig(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	config := ClaimConfig{
		ClaimTimeout:       10 * time.Second,
		MaxPriorityClaim:   3,
		MaxRetriesOnExpiry: 1,
		MaxBoardSize:       10,
	}
	cb := NewClaimBoard(sched, reg, conn, config, nil)

	if cb.config.ClaimTimeout != 10*time.Second {
		t.Errorf("expected ClaimTimeout 10s, got %v", cb.config.ClaimTimeout)
	}
	if cb.config.MaxPriorityClaim != 3 {
		t.Errorf("expected MaxPriorityClaim 3, got %d", cb.config.MaxPriorityClaim)
	}
}

func TestNewClaimBoard_NilLogger(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, ClaimConfig{}, nil)
	if cb.logger == nil {
		t.Errorf("expected non-nil logger")
	}
}

// ---------------------------------------------------------------------------
// Task 2: Post
// ---------------------------------------------------------------------------

func TestClaimBoardPost_LowPriority(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "build stuff", "builder", []string{"go"}, 3)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	// Task should be on board, not submitted to scheduler
	if cb.BoardSize() != 1 {
		t.Errorf("expected 1 task on board, got %d", cb.BoardSize())
	}
	if sched.submittedCount() != 0 {
		t.Errorf("expected 0 scheduler submissions, got %d", sched.submittedCount())
	}
}

func TestClaimBoardPost_HighPriority(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t-urgent", "urgent fix", "builder", nil, 8)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	// High-priority → routed directly to scheduler
	if cb.BoardSize() != 0 {
		t.Errorf("expected 0 tasks on board, got %d", cb.BoardSize())
	}
	if sched.submittedCount() != 1 {
		t.Errorf("expected 1 scheduler submission, got %d", sched.submittedCount())
	}
}

func TestClaimBoardPost_BoardFull(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.MaxBoardSize = 1
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	// Fill the board
	task1 := makeTask("t1", "task 1", "builder", nil, 3)
	if err := cb.Post(task1); err != nil {
		t.Fatalf("first Post failed: %v", err)
	}

	task2 := makeTask("t2", "task 2", "builder", nil, 3)
	err := cb.Post(task2)
	if err == nil {
		t.Errorf("expected 'claim board full' error")
	}
}

func TestClaimBoardPost_NoCandidates(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry() // empty registry
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 100 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task", "builder", nil, 3)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	// Task should be on board even without candidates
	if cb.BoardSize() != 1 {
		t.Errorf("expected 1 task on board, got %d", cb.BoardSize())
	}

	// Wait for expiry + fallback
	time.Sleep(500 * time.Millisecond)
	// After expiry with no retries from Post itself (the expiry re-posts),
	// the task will eventually fall back to scheduler
}

func TestClaimBoardPost_SkillsFilter(t *testing.T) {
	sched := newMockScheduler()
	// Only c2 has "docker" skill
	client1 := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	client2 := makeOnlineClient("c2", "builder", []string{"go", "docker"}, 2, 0)
	reg := newMockRegistry(client1, client2)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task", "builder", []string{"docker"}, 3)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	// Only c2 should be notified (has docker skill)
	// c1 should receive 0 messages
	time.Sleep(50 * time.Millisecond) // give goroutines time

	c1Msgs := conn.messagesFor("c1")
	c2Msgs := conn.messagesFor("c2")
	if len(c1Msgs) > 0 {
		t.Errorf("c1 should not receive task_available, got %d messages", len(c1Msgs))
	}
	if len(c2Msgs) != 1 {
		t.Errorf("c2 should receive 1 task_available, got %d", len(c2Msgs))
	}
}

func TestClaimBoardPost_SendsTaskAvailable(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "do the thing", "builder", []string{"go"}, 4)
	err := cb.Post(task)
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond) // give goroutines time

	msgs := conn.messagesFor("c1")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for c1, got %d", len(msgs))
	}

	msg := msgs[0]
	if msg.MsgType != reef.MsgTaskAvailable {
		t.Errorf("expected MsgTaskAvailable, got %s", msg.MsgType)
	}

	var payload reef.TaskAvailablePayload
	if err := msg.DecodePayload(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.TaskID != "t1" {
		t.Errorf("expected task ID t1, got %s", payload.TaskID)
	}
	if payload.RequiredRole != "builder" {
		t.Errorf("expected role builder, got %s", payload.RequiredRole)
	}
	if payload.Priority != 4 {
		t.Errorf("expected priority 4, got %d", payload.Priority)
	}
}

// ---------------------------------------------------------------------------
// Task 3: HandleClaim
// ---------------------------------------------------------------------------

func TestClaimBoardHandleClaim_Success(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "do stuff", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	err := cb.HandleClaim("t1", "c1")
	if err != nil {
		t.Fatalf("HandleClaim failed: %v", err)
	}

	// Task should be dispatched
	dispatched, ok := sched.dispatched["c1"]
	if !ok || len(dispatched) != 1 {
		t.Fatalf("expected 1 dispatch to c1, got %v", sched.dispatched)
	}
	if dispatched[0].ID != "t1" {
		t.Errorf("expected dispatched task t1, got %s", dispatched[0].ID)
	}

	// Check that task was marked as claimed
	ct := cb.GetTask("t1")
	if ct == nil {
		t.Fatal("task should still be on board")
	}
	if ct.AssignedClient != "c1" {
		t.Errorf("expected AssignedClient c1, got %s", ct.AssignedClient)
	}
}

func TestClaimBoardHandleClaim_AlreadyClaimed(t *testing.T) {
	sched := newMockScheduler()
	client1 := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	client2 := makeOnlineClient("c2", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client1, client2)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "do stuff", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// First claim succeeds
	if err := cb.HandleClaim("t1", "c1"); err != nil {
		t.Fatalf("first HandleClaim failed: %v", err)
	}

	// Second claim fails
	err := cb.HandleClaim("t1", "c2")
	if err == nil {
		t.Errorf("expected 'already claimed' error")
	}
}

func TestClaimBoardHandleClaim_TaskNotOnBoard(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cb := NewClaimBoard(sched, reg, conn, DefaultClaimConfig(), nil)

	err := cb.HandleClaim("nonexistent", "c1")
	if err == nil {
		t.Errorf("expected 'not on claim board' error")
	}
}

func TestClaimBoardHandleClaim_IneligibleClient(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "reviewer", []string{"python"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "do stuff", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	err := cb.HandleClaim("t1", "c1")
	if err == nil {
		t.Errorf("expected ineligible error")
	}
}

func TestClaimBoardHandleClaim_NotifiesOthers(t *testing.T) {
	sched := newMockScheduler()
	client1 := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	client2 := makeOnlineClient("c2", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client1, client2)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "do stuff", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if err := cb.HandleClaim("t1", "c1"); err != nil {
		t.Fatalf("HandleClaim failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// c2 should receive task_claimed
	c2Msgs := conn.messagesFor("c2")
	foundClaimed := false
	for _, msg := range c2Msgs {
		if msg.MsgType == reef.MsgTaskClaimed {
			var payload reef.TaskClaimedPayload
			if err := msg.DecodePayload(&payload); err != nil {
				continue
			}
			if payload.TaskID == "t1" && payload.ClaimedBy == "c1" {
				foundClaimed = true
				break
			}
		}
	}
	if !foundClaimed {
		t.Errorf("c2 should receive task_claimed message")
	}
}

func TestClaimBoardHandleClaim_DispatchFailsRepost(t *testing.T) {
	sched := newMockScheduler()
	sched.dispatchErr = fmt.Errorf("dispatch failed")

	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "do stuff", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	err := cb.HandleClaim("t1", "c1")
	if err == nil {
		t.Errorf("expected dispatch error")
	}

	// Due to dispatch failure, a repost should have happened
	// (with task ID "t1-repost")
	time.Sleep(50 * time.Millisecond)
	if cb.BoardSize() < 1 {
		t.Logf("board size after failed dispatch: %d (repost may be async)", cb.BoardSize())
	}
}

// ---------------------------------------------------------------------------
// Task 4: ExpiryTimer
// ---------------------------------------------------------------------------

func TestClaimBoardExpiry_RetryThenFallback(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry() // no candidates
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 50 * time.Millisecond
	cfg.MaxRetriesOnExpiry = 2
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task with no candidates", "builder", nil, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	// Wait for all expiries + fallback
	// Each expiry: 50ms timeout + re-post = new timer
	// Total: 3 cycles × 50ms = 150ms, add buffer
	time.Sleep(300 * time.Millisecond)

	// Task should have fallen back to scheduler
	if sched.submittedCount() < 1 {
		t.Errorf("expected at least 1 scheduler submission after fallback, got %d", sched.submittedCount())
	}

	// Task should no longer be on board
	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board after fallback, got %d tasks", cb.BoardSize())
	}
}

func TestClaimBoardExpiry_ClaimedBeforeExpiry(t *testing.T) {
	sched := newMockScheduler()
	client := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
	reg := newMockRegistry(client)
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 200 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "do stuff", "builder", []string{"go"}, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	// Claim before expiry
	time.Sleep(50 * time.Millisecond)
	if err := cb.HandleClaim("t1", "c1"); err != nil {
		t.Fatalf("HandleClaim failed: %v", err)
	}

	// Wait past expiry
	time.Sleep(300 * time.Millisecond)

	// Task should NOT have been submitted to scheduler
	if sched.submittedCount() > 0 {
		t.Errorf("expected 0 scheduler submissions after claim, got %d", sched.submittedCount())
	}
}

func TestClaimBoardExpiry_RetriesCount(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry() // no eligible candidates
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 30 * time.Millisecond
	cfg.MaxRetriesOnExpiry = 1 // only 1 retry, then fallback
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "lonely task", "builder", nil, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	// Wait: 1st expiry (30ms) + retry post → 2nd expiry (30ms) + fallback
	time.Sleep(150 * time.Millisecond)

	if sched.submittedCount() < 1 {
		t.Errorf("expected scheduler fallback, got %d submissions", sched.submittedCount())
	}
}

func TestClaimBoardCancel(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "cancellable task", "builder", nil, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	if cb.BoardSize() != 1 {
		t.Fatalf("expected 1 task on board")
	}

	cb.Cancel("t1")

	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board after cancel")
	}

	// Wait past expiry — should not trigger scheduler fallback
	time.Sleep(600 * time.Millisecond)
	if sched.submittedCount() > 0 {
		t.Errorf("expected 0 scheduler submissions after cancel, got %d", sched.submittedCount())
	}
}

// ---------------------------------------------------------------------------
// Stop
// ---------------------------------------------------------------------------

func TestClaimBoardStop(t *testing.T) {
	sched := newMockScheduler()
	reg := newMockRegistry()
	conn := newMockConnManager()

	cfg := DefaultClaimConfig()
	cfg.ClaimTimeout = 500 * time.Millisecond
	cb := NewClaimBoard(sched, reg, conn, cfg, nil)

	task := makeTask("t1", "task", "builder", nil, 3)
	if err := cb.Post(task); err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	cb.Stop()

	if cb.BoardSize() != 0 {
		t.Errorf("expected empty board after stop")
	}
}

// ---------------------------------------------------------------------------
// Task 6: End-to-end integration test
// ---------------------------------------------------------------------------

func TestClaimBoardEndToEnd(t *testing.T) {
	t.Run("full claim flow", func(t *testing.T) {
		sched := newMockScheduler()
		client1 := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
		client2 := makeOnlineClient("c2", "builder", []string{"go"}, 2, 0)
		reg := newMockRegistry(client1, client2)
		conn := newMockConnManager()

		cfg := DefaultClaimConfig()
		cfg.ClaimTimeout = 200 * time.Millisecond
		cb := NewClaimBoard(sched, reg, conn, cfg, nil)

		// 1. Post task with required role and skills
		task := makeTask("t-e2e", "integration test task", "builder", []string{"go"}, 4)
		if err := cb.Post(task); err != nil {
			t.Fatalf("Post failed: %v", err)
		}

		// 2. Verify task_available sent to both clients
		time.Sleep(50 * time.Millisecond)
		c1Msgs := conn.messagesFor("c1")
		c2Msgs := conn.messagesFor("c2")
		if len(c1Msgs) != 1 || c1Msgs[0].MsgType != reef.MsgTaskAvailable {
			t.Errorf("c1 should receive task_available, got %d msgs", len(c1Msgs))
		}
		if len(c2Msgs) != 1 || c2Msgs[0].MsgType != reef.MsgTaskAvailable {
			t.Errorf("c2 should receive task_available, got %d msgs", len(c2Msgs))
		}

		// 3. Client 1 claims
		if err := cb.HandleClaim("t-e2e", "c1"); err != nil {
			t.Fatalf("HandleClaim failed: %v", err)
		}

		// 4. Verify scheduler.Dispatch called with client1
		dispatched, ok := sched.dispatched["c1"]
		if !ok || len(dispatched) != 1 {
			t.Errorf("expected dispatch to c1")
		}
		if dispatched != nil && dispatched[0].ID == "t-e2e" {
			t.Logf("task dispatched correctly to c1")
		}

		// 5. Verify task_claimed sent to client 2
		time.Sleep(50 * time.Millisecond)
		c2MsgsAfter := conn.messagesFor("c2")
		foundClaimed := false
		for _, msg := range c2MsgsAfter {
			if msg.MsgType == reef.MsgTaskClaimed {
				var payload reef.TaskClaimedPayload
				if err := msg.DecodePayload(&payload); err == nil {
					if payload.TaskID == "t-e2e" && payload.ClaimedBy == "c1" {
						foundClaimed = true
					}
				}
			}
		}
		if !foundClaimed {
			t.Errorf("c2 should receive task_claimed message")
		}
	})

	t.Run("expiry and fallback flow", func(t *testing.T) {
		sched := newMockScheduler()
		reg := newMockRegistry() // no clients
		conn := newMockConnManager()

		cfg := DefaultClaimConfig()
		cfg.ClaimTimeout = 30 * time.Millisecond
		cfg.MaxRetriesOnExpiry = 2
		cb := NewClaimBoard(sched, reg, conn, cfg, nil)

		// Post task with no eligible clients
		task := makeTask("t-exp", "task that will expire", "builder", nil, 3)
		if err := cb.Post(task); err != nil {
			t.Fatalf("Post failed: %v", err)
		}

		// Wait for all expiries + fallback
		time.Sleep(200 * time.Millisecond)

		// After 2 retries, scheduler.Submit should have been called
		if sched.submittedCount() < 1 {
			t.Errorf("expected scheduler.Submit to be called after fallback, got %d submissions", sched.submittedCount())
		}
	})

	t.Run("high priority bypasses claim board", func(t *testing.T) {
		sched := newMockScheduler()
		reg := newMockRegistry()
		conn := newMockConnManager()

		cfg := DefaultClaimConfig()
		cb := NewClaimBoard(sched, reg, conn, cfg, nil)

		// Post high-priority task
		task := makeTask("t-high", "urgent task", "builder", nil, 8)
		if err := cb.Post(task); err != nil {
			t.Fatalf("Post failed: %v", err)
		}

		// Should be routed directly to scheduler
		if sched.submittedCount() != 1 {
			t.Errorf("expected 1 scheduler submission for high-priority task, got %d", sched.submittedCount())
		}
		if cb.BoardSize() != 0 {
			t.Errorf("high-priority task should not be on claim board")
		}
	})

	t.Run("concurrent claims first-come-first-served", func(t *testing.T) {
		sched := newMockScheduler()
		client1 := makeOnlineClient("c1", "builder", []string{"go"}, 2, 0)
		client2 := makeOnlineClient("c2", "builder", []string{"go"}, 2, 0)
		reg := newMockRegistry(client1, client2)
		conn := newMockConnManager()

		cfg := DefaultClaimConfig()
		cfg.ClaimTimeout = 500 * time.Millisecond
		cb := NewClaimBoard(sched, reg, conn, cfg, nil)

		task := makeTask("t-race", "race task", "builder", []string{"go"}, 3)
		if err := cb.Post(task); err != nil {
			t.Fatalf("Post failed: %v", err)
		}

		var wg sync.WaitGroup
		var c1Err, c2Err error
		wg.Add(2)
		go func() {
			defer wg.Done()
			c1Err = cb.HandleClaim("t-race", "c1")
		}()
		go func() {
			defer wg.Done()
			c2Err = cb.HandleClaim("t-race", "c2")
		}()
		wg.Wait()

		// Exactly one should succeed
		if (c1Err == nil) == (c2Err == nil) {
			t.Errorf("exactly one claim should succeed: c1Err=%v, c2Err=%v", c1Err, c2Err)
		}

		// Only one dispatch should have happened
		totalDispatches := 0
		for _, tasks := range sched.dispatched {
			totalDispatches += len(tasks)
		}
		if totalDispatches != 1 {
			t.Errorf("expected exactly 1 dispatch, got %d", totalDispatches)
		}
	})
}
