// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent tests for AutoLoopOrchestrator core types.
// TDD Phase — written before orchestrator implementation.

package agent

import (
	"testing"
	"time"
)

// ============================================================================
// Test: OrchestratorState string values
// ============================================================================

func TestOrchestratorStateValues(t *testing.T) {
	states := []struct {
		state OrchestratorState
		want  string
	}{
		{StateIdle, "idle"},
		{StatePlanning, "planning"},
		{StateDispatching, "dispatching"},
		{StateMonitoring, "monitoring"},
		{StateHealing, "healing"},
		{StateScaling, "scaling"},
	}
	for _, s := range states {
		if got := string(s.state); got != s.want {
			t.Errorf("OrchestratorState(%s) = %q, want %q", s.want, got, s.want)
		}
	}
}

// ============================================================================
// Test: AutoMode string values
// ============================================================================

func TestAutoModeValues(t *testing.T) {
	modes := []struct {
		mode AutoMode
		want string
	}{
		{ModeOnce, "once"},
		{ModeLoopN, "loop_n"},
		{ModeInfinite, "infinite"},
		{ModeUntilCond, "until_cond"},
	}
	for _, m := range modes {
		if got := string(m.mode); got != m.want {
			t.Errorf("AutoMode(%s) = %q, want %q", m.want, got, m.want)
		}
	}
}

// ============================================================================
// Test: PlanStatus values
// ============================================================================

func TestPlanStatusValues(t *testing.T) {
	statuses := []struct {
		status PlanStatus
		want   string
	}{
		{PlanActive, "active"},
		{PlanCompleted, "completed"},
		{PlanFailed, "failed"},
		{PlanCancelled, "cancelled"},
	}
	for _, s := range statuses {
		if got := string(s.status); got != s.want {
			t.Errorf("PlanStatus(%s) = %q, want %q", s.want, got, s.want)
		}
	}
}

// ============================================================================
// Test: TaskNodeStatus values
// ============================================================================

func TestTaskNodeStatusValues(t *testing.T) {
	statuses := []struct {
		status TaskNodeStatus
		want   string
	}{
		{NodePending, "pending"},
		{NodeReady, "ready"},
		{NodeRunning, "running"},
		{NodeDone, "done"},
		{NodeFailed, "failed"},
		{NodeBlocked, "blocked"},
	}
	for _, s := range statuses {
		if got := string(s.status); got != s.want {
			t.Errorf("TaskNodeStatus(%s) = %q, want %q", s.want, got, s.want)
		}
	}
}

// ============================================================================
// Test: DefaultLoopConfig
// ============================================================================

func TestDefaultLoopConfig(t *testing.T) {
	cfg := DefaultLoopConfig()
	if cfg.Mode != ModeInfinite {
		t.Errorf("Mode = %v, want ModeInfinite", cfg.Mode)
	}
	if cfg.PollEvery != 1*time.Second {
		t.Errorf("PollEvery = %v, want 1s", cfg.PollEvery)
	}
	if cfg.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want 5m", cfg.IdleTimeout)
	}
	if cfg.MaxClients != 5 {
		t.Errorf("MaxClients = %d, want 5", cfg.MaxClients)
	}
}

// ============================================================================
// Test: DefaultRetryPolicy
// ============================================================================

func TestDefaultRetryPolicy(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", p.MaxRetries)
	}
	if p.BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor = %f, want 2.0", p.BackoffFactor)
	}
	if p.RetryDelay <= 0 {
		t.Errorf("RetryDelay = %v, want > 0", p.RetryDelay)
	}
}

// ============================================================================
// Test: NewAutoLoopOrchestrator defaults
// ============================================================================

func TestNewAutoLoopOrchestrator(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	if orch == nil {
		t.Fatal("NewAutoLoopOrchestrator returned nil")
	}
	if orch.GetMode() != ModeAuto {
		t.Errorf("GetMode() = %v, want ModeAuto", orch.GetMode())
	}
	status := orch.Status()
	if status.State != StateIdle {
		t.Errorf("Status().State = %v, want StateIdle", status.State)
	}
	if status.TasksTotal != 0 {
		t.Errorf("Status().TasksTotal = %d, want 0", status.TasksTotal)
	}
	if status.ClientsMax != 5 {
		t.Errorf("Status().ClientsMax = %d, want 5", status.ClientsMax)
	}
}

// ============================================================================
// Test: GetMode / SetMode
// ============================================================================

func TestGetSetMode(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	if orch.GetMode() != ModeAuto {
		t.Errorf("initial mode = %v, want ModeAuto", orch.GetMode())
	}
	prev := orch.SetMode(ModeManual)
	if prev != ModeAuto {
		t.Errorf("previous mode = %v, want ModeAuto", prev)
	}
	if orch.GetMode() != ModeManual {
		t.Errorf("mode after SetMode = %v, want ModeManual", orch.GetMode())
	}
}

// ============================================================================
// Test: Status returns correct info
// ============================================================================

func TestStatus(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	status := orch.Status()
	if status.State != StateIdle {
		t.Errorf("Status().State = %v, want StateIdle", status.State)
	}
	if status.TasksTotal != 0 {
		t.Errorf("Status().TasksTotal = %d, want 0", status.TasksTotal)
	}
	if status.TasksPending != 0 {
		t.Errorf("Status().TasksPending = %d, want 0", status.TasksPending)
	}
	if status.TasksRunning != 0 {
		t.Errorf("Status().TasksRunning = %d, want 0", status.TasksRunning)
	}
}

// ============================================================================
// Test: Stop returns cleanly when already idle
// ============================================================================

func TestStop(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	// Stop on idle orchestrator should return without error or panic
	orch.Stop()
	status := orch.Status()
	if status.State != StateIdle {
		t.Errorf("Status().State after Stop = %v, want StateIdle", status.State)
	}
	// Done channel exists (not nil)
	if orch.Done() == nil {
		t.Error("Done() channel should not be nil")
	}
}

// ============================================================================
// Test: Tick channel is not nil and has buffer
// ============================================================================

func TestTickChannel(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	tick := orch.Tick()
	if tick == nil {
		t.Fatal("Tick() returned nil channel")
	}
	// The ticker channel is buffered (size 10) and will be driven by
	// the AgentLoop in the actual implementation. At orchestrator level,
	// we verify the channel is ready for use.
	if cap(tick) != 10 {
		t.Errorf("Tick channel capacity = %d, want 10", cap(tick))
	}
}

// ============================================================================
// Test: ExecutionPlan with tasks
// ============================================================================

func TestExecutionPlan(t *testing.T) {
	plan := &ExecutionPlan{
		ID:     "plan-test-1",
		Status: PlanActive,
		Tasks: []*PlannedTask{
			{ID: "t1", Instruction: "task one", Status: NodeReady},
			{ID: "t2", Instruction: "task two", Status: NodePending, DependsOn: []string{"t1"}},
		},
	}
	if len(plan.Tasks) != 2 {
		t.Errorf("Tasks = %d, want 2", len(plan.Tasks))
	}
	if plan.Status != PlanActive {
		t.Errorf("Status = %v, want PlanActive", plan.Status)
	}
	if plan.Tasks[0].Status != NodeReady {
		t.Errorf("task[0] Status = %v, want NodeReady", plan.Tasks[0].Status)
	}
}

// ============================================================================
// Test: OrchestratorStatus composition
// ============================================================================

func TestOrchestratorStatus(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	s := orch.Status()
	if s.State == "" {
		t.Error("Status().State is empty")
	}
	if len(s.Mode) == 0 {
		t.Error("Status().Mode is empty")
	}
}

// ============================================================================
// Wave 3 — Core Scheduling Tests
// ============================================================================

// TestTrigger verifies that Trigger sends a time.Time value on tickerCh
// within a reasonable timeout.
func TestTrigger(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())

	// Drain any existing ticks first
	drained := false
	for drained {
		select {
		case <-orch.Tick():
		default:
			drained = true
		}
	}

	orch.Trigger()

	select {
	case <-orch.Tick():
		// OK — tick received
	case <-time.After(100 * time.Millisecond):
		t.Error("Trigger did not send tick on tickerCh within 100ms")
	}
}

// TestEnqueueDequeue verifies EnqueueMessage and QueueDepth.
func TestEnqueueDequeue(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())

	if d := orch.QueueDepth(); d != 0 {
		t.Errorf("initial QueueDepth = %d, want 0", d)
	}

	orch.EnqueueMessage("hello")
	orch.EnqueueMessage("world")

	if d := orch.QueueDepth(); d != 2 {
		t.Errorf("QueueDepth after 2 enqueues = %d, want 2", d)
	}
}

// TestPollQueueIdle verifies that PollQueue does not panic when the
// orchestrator has no active plan.
func TestPollQueueIdle(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	// No plan set; PollQueue should return immediately without panicking.
	orch.PollQueue(nil) //nolint:staticcheck // nil context is safe for test
}

// TestPollQueueNoPlan verifies PollQueue does nothing when plan is nil.
func TestPollQueueNoPlan(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	orch.mu.Lock()
	orch.plan = nil
	orch.mu.Unlock()

	// Should not panic
	orch.PollQueue(nil) //nolint:staticcheck
}

// TestFindPlanTask verifies findPlanTask returns the correct task.
func TestFindPlanTask(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	orch.plan = &ExecutionPlan{
		ID:     "plan-1",
		Status: PlanActive,
		Tasks: []*PlannedTask{
			{ID: "t1", Instruction: "first", Status: NodeReady},
			{ID: "t2", Instruction: "second", Status: NodePending, DependsOn: []string{"t1"}},
		},
	}

	t1 := orch.findPlanTask("t1")
	if t1 == nil {
		t.Fatal("findPlanTask(t1) returned nil")
	}
	if t1.ID != "t1" {
		t.Errorf("findPlanTask(t1).ID = %q, want t1", t1.ID)
	}

	t2 := orch.findPlanTask("t2")
	if t2 == nil {
		t.Fatal("findPlanTask(t2) returned nil")
	}
	if t2.ID != "t2" {
		t.Errorf("findPlanTask(t2).ID = %q, want t2", t2.ID)
	}

	missing := orch.findPlanTask("t3")
	if missing != nil {
		t.Errorf("findPlanTask(t3) = %v, want nil", missing)
	}
}

// TestCheckDAG verifies that when a dependency completes, the dependent
// task transitions from NodePending → NodeReady.
func TestCheckDAG(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())
	orch.plan = &ExecutionPlan{
		ID:     "plan-dag",
		Status: PlanActive,
		Tasks: []*PlannedTask{
			{
				ID:          "task-a",
				Instruction: "compile",
				Status:      NodeDone,
			},
			{
				ID:             "task-b",
				Instruction:    "test",
				Status:         NodePending,
				DependsOn:      []string{"task-a"},
				UnresolvedDeps: 1,
			},
		},
	}

	// Before PollQueue, task-b should be pending
	tb := orch.findPlanTask("task-b")
	if tb.Status != NodePending {
		t.Fatalf("pre-PollQueue task-b status = %v, want NodePending", tb.Status)
	}

	// Running PollQueue should unlock task-b, then submitPlanTask marks it running
	orch.PollQueue(nil) //nolint:staticcheck

	tb = orch.findPlanTask("task-b")
	if tb.Status != NodeRunning {
		t.Errorf("post-PollQueue task-b status = %v, want NodeRunning (unlocked then dispatched)", tb.Status)
	}
	if tb.UnresolvedDeps != 0 {
		t.Errorf("post-PollQueue task-b UnresolvedDeps = %d, want 0", tb.UnresolvedDeps)
	}
}

// TestPollQueueTick verifies that Poll() runs PollQueue without panicking.
func TestPollQueueTick(t *testing.T) {
	orch := NewAutoLoopOrchestrator(DefaultLoopConfig())

	// Set up a simple plan
	orch.plan = &ExecutionPlan{
		ID:     "plan-tick",
		Status: PlanActive,
		Tasks: []*PlannedTask{
			{ID: "t1", Instruction: "run test", Status: NodeReady},
		},
	}

	// Poll should not panic
	orch.Poll(nil) //nolint:staticcheck

	// After Poll, t1 should be NodeRunning (simulated submit)
	t1 := orch.findPlanTask("t1")
	if t1.Status != NodeRunning {
		t.Errorf("post-Poll task status = %v, want NodeRunning", t1.Status)
	}
}
