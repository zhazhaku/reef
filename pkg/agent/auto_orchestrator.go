// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent provides the AutoLoopOrchestrator — the core execution
// engine for Auto/Manual modes. It manages task planning, client pool
// lifecycle, DAG dependency resolution, self-healing retries, and
// autoscaling decisions.
//
// This file contains the foundational type definitions and skeleton
// interface. Full implementations of ClientPool, TaskPlanner, Healer,
// and AutoScaler live in separate files (Phase 2-6).

package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// State Definitions
// ============================================================================

// OrchestratorState indicates what the Orchestrator is currently doing.
type OrchestratorState string

const (
	StateIdle        OrchestratorState = "idle"
	StatePlanning    OrchestratorState = "planning"
	StateDispatching OrchestratorState = "dispatching"
	StateMonitoring  OrchestratorState = "monitoring"
	StateHealing     OrchestratorState = "healing"
	StateScaling     OrchestratorState = "scaling"
)

// String returns the human-readable state name.
func (s OrchestratorState) String() string { return string(s) }

// IsActive returns true if the Orchestrator is actively processing
// (not idle).
func (s OrchestratorState) IsActive() bool {
	return s != StateIdle
}

// AutoMode defines the loop execution strategy.
type AutoMode string

const (
	// ModeOnce executes the plan exactly once and stops.
	ModeOnce AutoMode = "once"

	// ModeLoopN executes the plan N times (controlled by LoopCount).
	ModeLoopN AutoMode = "loop_n"

	// ModeInfinite loops until explicitly stopped via /auto stop.
	ModeInfinite AutoMode = "infinite"

	// ModeUntilCond loops until a termination condition is met
	// (e.g. queue empty, error threshold exceeded).
	ModeUntilCond AutoMode = "until_cond"
)

// ============================================================================
// Configuration Types
// ============================================================================

// LoopConfig controls the Orchestrator's run loop behaviour.
type LoopConfig struct {
	// PollEvery is the interval between scheduler poll cycles.
	PollEvery time.Duration `json:"poll_every"`

	// Mode determines how many times the loop executes.
	Mode AutoMode `json:"mode"`

	// LoopCount is the number of iterations for ModeLoopN.
	// Ignored for other modes.
	LoopCount int `json:"loop_count,omitempty"`

	// IdleTimeout is how long the Orchestrator waits with an empty
	// queue before triggering ScaleDown.
	IdleTimeout time.Duration `json:"idle_timeout"`

	// MaxClients is the hard upper limit for the client pool.
	MaxClients int `json:"max_clients"`

	// Token is the Reef authentication token injected into spawned
	// client processes.
	Token string `json:"-"`

	// ReefBin is the path to the reef binary used for spawning clients.
	ReefBin string `json:"reef_bin"`
}

// DefaultLoopConfig returns a reasonable default configuration.
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{
		PollEvery:   1 * time.Second,
		Mode:        ModeInfinite,
		IdleTimeout: 5 * time.Minute,
		MaxClients:  5,
	}
}

// RetryPolicy defines how the Healer retries failed tasks.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int `json:"max_retries"`

	// RetryDelay is the base delay between retries.
	RetryDelay time.Duration `json:"retry_delay"`

	// BackoffFactor multiplies the delay on each successive retry
	// (e.g. 2.0 = exponential backoff: 1s → 2s → 4s).
	BackoffFactor float64 `json:"backoff_factor"`

	// ExcludeFailedClient when true prevents re-dispatching to the
	// same client that failed the previous attempt.
	ExcludeFailedClient bool `json:"exclude_failed_client"`

	// AutoSpawnOnRetry when true automatically spawns a new client
	// for retry if the failed client is unavailable.
	AutoSpawnOnRetry bool `json:"auto_spawn_on_retry"`
}

// DefaultRetryPolicy returns a sensible default retry configuration.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:          3,
		RetryDelay:          5 * time.Second,
		BackoffFactor:       2.0,
		ExcludeFailedClient: true,
		AutoSpawnOnRetry:    true,
	}
}

// ============================================================================
// Execution Plan Types
// ============================================================================

// PlanStatus tracks the lifecycle of an ExecutionPlan.
type PlanStatus string

const (
	PlanActive    PlanStatus = "active"
	PlanCompleted PlanStatus = "completed"
	PlanFailed    PlanStatus = "failed"
	PlanCancelled PlanStatus = "cancelled"
)

// TaskNodeStatus tracks the lifecycle of an individual PlannedTask.
type TaskNodeStatus string

const (
	NodePending TaskNodeStatus = "pending" // waiting for dependencies
	NodeReady   TaskNodeStatus = "ready"   // dependencies satisfied, schedulable
	NodeRunning TaskNodeStatus = "running" // dispatched to a client
	NodeDone    TaskNodeStatus = "done"    // completed successfully
	NodeFailed  TaskNodeStatus = "failed"  // terminal failure
	NodeBlocked TaskNodeStatus = "blocked" // blocked by a failed dependency
)

// ExecutionPlan represents one orchestrated execution cycle.
// It contains a DAG of PlannedTasks to be executed in dependency order.
type ExecutionPlan struct {
	ID          string         `json:"id"`
	Instruction string         `json:"instruction"`
	Tasks       []*PlannedTask `json:"tasks"`
	CreatedAt   time.Time      `json:"created_at"`
	Status      PlanStatus     `json:"status"`
}

// PlannedTask is a single task node in an ExecutionPlan DAG.
type PlannedTask struct {
	ID              string         `json:"id"`
	Instruction     string         `json:"instruction"`
	RequiredRole    string         `json:"required_role"`
	RequiredSkills  []string       `json:"required_skills,omitempty"`
	DependsOn       []string       `json:"depends_on,omitempty"`   // task IDs this depends on
	Dependents      []string       `json:"dependents,omitempty"`   // task IDs that depend on this
	UnresolvedDeps  int            `json:"unresolved_deps"`        // count of unsatisfied dependencies
	Status          TaskNodeStatus `json:"status"`
	AssignedClient  string         `json:"assigned_client,omitempty"`
	AttemptCount    int            `json:"attempt_count"`
	MaxRetries      int            `json:"max_retries"`
	SubmitTime      time.Time      `json:"submit_time,omitempty"`
	CompleteTime    time.Time      `json:"complete_time,omitempty"`
	ErrorMessage    string         `json:"error_message,omitempty"`
}

// IsReady returns true if all dependencies are satisfied and the
// task has not already been dispatched.
func (t *PlannedTask) IsReady() bool {
	return t.UnresolvedDeps == 0 && t.Status == NodeReady
}

// MarkBlocked transitions the task to blocked due to a failed dependency.
func (t *PlannedTask) MarkBlocked(reason string) {
	t.Status = NodeBlocked
	t.ErrorMessage = reason
}

// ============================================================================
// Orchestrator Status Report
// ============================================================================

// OrchestratorStatus is a snapshot of the Orchestrator's current state,
// returned by the Status() method.
type OrchestratorStatus struct {
	Mode           ConversationMode  `json:"mode"`
	State          OrchestratorState `json:"state"`
	PlanID         string            `json:"plan_id,omitempty"`
	PlanStatus     PlanStatus        `json:"plan_status,omitempty"`
	TasksTotal     int               `json:"tasks_total"`
	TasksCompleted int               `json:"tasks_completed"`
	TasksFailed    int               `json:"tasks_failed"`
	TasksRunning   int               `json:"tasks_running"`
	TasksPending   int               `json:"tasks_pending"`
	ClientsActive  int               `json:"clients_active"`
	ClientsMax     int               `json:"clients_max"`
}

// ============================================================================
// AutoLoopOrchestrator — Core Engine
// ============================================================================

// AutoLoopOrchestrator is the central execution engine for Auto and
// Manual modes. It coordinates task planning, client pool lifecycle,
// dependency resolution, self-healing, and autoscaling.
//
// Phase 1 provides the skeleton types and mode management methods.
// Full lifecycle (Poll, Tick, SubmitTask, Heal, Scale) is implemented
// in Phase 2-9.
type AutoLoopOrchestrator struct {
	mu   sync.Mutex
	mode ConversationMode  // active conversation mode (auto/manual)
	state OrchestratorState // current engine state

	loopConfig LoopConfig     // loop execution configuration
	plan       *ExecutionPlan // active execution plan (nil if none)

	// Statistics
	completedCount int
	failedCount    int

	// Task scheduling
	taskQueue   []string                     // non-command message queue
	activeTasks map[string]context.CancelFunc // running watchTask goroutines
	planLock    sync.Mutex                   // plan-dedicated lock (avoids mixing with mu)

	// Wave 4A — Healer + AutoScaler
	healer *Healer
	scaler *AutoScaler

	// Control channels
	tickerCh chan time.Time
	doneCh   chan struct{}
	stopCh   chan struct{}
}

// NewAutoLoopOrchestrator creates a new AutoLoopOrchestrator with the
// given loop configuration. Full dependency injection (planner, clientPool,
// healer, scaler, scheduler, registry, eventBus) is deferred to Phase 2+
// when those components are implemented.
func NewAutoLoopOrchestrator(cfg LoopConfig) *AutoLoopOrchestrator {
	return &AutoLoopOrchestrator{
		mode:        ModeAuto, // default to auto mode
		state:       StateIdle,
		loopConfig:  cfg,
		taskQueue:   make([]string, 0),
		activeTasks: make(map[string]context.CancelFunc),
		healer:      NewHealer(DefaultRetryPolicy()),
		scaler:      NewAutoScaler(1, 5, 5*time.Minute, 30*time.Second),
		tickerCh:    make(chan time.Time, 10),
		doneCh:      make(chan struct{}),
		stopCh:      make(chan struct{}),
	}
}

// GetMode returns the active conversation mode for this orchestrator.
func (o *AutoLoopOrchestrator) GetMode() ConversationMode {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.mode
}

// SetMode changes the conversation mode and returns the previous mode.
// Switching to ModeAuto or ModeManual triggers the orchestrator lifecycle;
// switching to ModeChat or ModeHermes triggers graceful shutdown.
func (o *AutoLoopOrchestrator) SetMode(mode ConversationMode) ConversationMode {
	o.mu.Lock()
	defer o.mu.Unlock()
	prev := o.mode
	o.mode = mode
	return prev
}

// Status returns a snapshot of the orchestrator's current runtime state.
// This is used by the CLI /auto status command and the monitoring dashboard.
func (o *AutoLoopOrchestrator) Status() OrchestratorStatus {
	o.mu.Lock()
	defer o.mu.Unlock()

	status := OrchestratorStatus{
		Mode:  o.mode,
		State: o.state,
	}

	if o.plan != nil {
		status.PlanID = o.plan.ID
		status.PlanStatus = o.plan.Status
		status.TasksTotal = len(o.plan.Tasks)
		for _, t := range o.plan.Tasks {
			switch t.Status {
			case NodeDone:
				status.TasksCompleted++
			case NodeFailed:
				status.TasksFailed++
			case NodeRunning:
				status.TasksRunning++
			case NodePending, NodeReady:
				status.TasksPending++
			}
		}
	}

	status.ClientsActive = 0 // populated in Phase 2 (ClientPool)
	status.ClientsMax = o.loopConfig.MaxClients

	return status
}

// Stop gracefully stops the orchestrator. It signals the main loop
// to stop accepting new tasks, drains in-flight tasks, and cleans
// up resources.
func (o *AutoLoopOrchestrator) Stop() {
	o.mu.Lock()
	if o.state == StateIdle {
		o.mu.Unlock()
		return
	}
	o.state = StateIdle
	o.mu.Unlock()

	// Signal the main loop to exit
	close(o.stopCh)

	// Signal completion (non-blocking)
	select {
	case o.doneCh <- struct{}{}:
	default:
	}
}

// Tick returns the ticker channel for the orchestrator's main event
// loop. The channel receives a time value every PollEvery interval.
// Consumed by AgentLoop.Run() select block.
func (o *AutoLoopOrchestrator) Tick() <-chan time.Time {
	return o.tickerCh
}

// Done returns a channel that closes when the orchestrator completes
// its current execution plan (or is stopped).
func (o *AutoLoopOrchestrator) Done() <-chan struct{} {
	return o.doneCh
}

// String returns a human-readable summary.
func (o *AutoLoopOrchestrator) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return fmt.Sprintf("AutoLoopOrchestrator{mode=%s, state=%s}", o.mode, o.state)
}

// ============================================================================
// Core Scheduling — Phase 3 (Wave 3)
// ============================================================================

// Trigger fires a single poll cycle immediately by sending a time.Now()
// pulse on the tickerCh channel. Non-blocking; drops the pulse silently
// if the channel is already full.
func (o *AutoLoopOrchestrator) Trigger() {
	select {
	case o.tickerCh <- time.Now():
	default:
	}
}

// Poll runs one complete poll iteration over all subsystems.
func (o *AutoLoopOrchestrator) Poll(ctx context.Context) {
	o.PollQueue(ctx)
	o.PollHealing(ctx)
	o.PollScale(ctx)
}

// PollQueue is the main dispatch loop. It drains the message queue,
// resolves DAG dependencies, finds ready tasks, and submits them.
// In Phase 3 all submits are simulated (log-only); real scheduler
// dispatch arrives in Wave 4.
func (o *AutoLoopOrchestrator) PollQueue(ctx context.Context) {
	o.mu.Lock()
	if o.plan == nil || o.plan.Status == PlanCompleted || o.plan.Status == PlanFailed || o.plan.Status == PlanCancelled {
		o.mu.Unlock()
		return
	}
	o.mu.Unlock()

	// 1. Drain message queue — Phase 3 stub (future: convert messages → tasks)
	o.mu.Lock()
	_ = len(o.taskQueue) // consumed in Wave 4+
	o.taskQueue = o.taskQueue[:0]
	o.mu.Unlock()

	// 2. checkDAG — unlock dependencies that have been satisfied
	o.planLock.Lock()
	defer o.planLock.Unlock()

	for _, task := range o.plan.Tasks {
		if task.Status != NodePending {
			continue
		}
		allSatisfied := true
		for _, depID := range task.DependsOn {
			dep := o.findPlanTask(depID)
			if dep == nil || dep.Status != NodeDone {
				allSatisfied = false
				break
			}
		}
		if allSatisfied {
			task.Status = NodeReady
			task.UnresolvedDeps = 0
		}
	}

	// 3. findReadyTasks — collect all ready-to-run tasks
	readyTasks := o.findReadyTasks()

	// 4. applyParallelismLimit — cap to configured max parallelism
	readyTasks = o.applyParallelismLimit(readyTasks)

	// 5. submitPlanTask — simulate dispatch (log-only in Phase 3)
	for _, task := range readyTasks {
		o.submitPlanTask(task)
	}
}

// EnqueueMessage appends a non-command user message to the task queue.
// The message will be processed in the next PollQueue cycle.
func (o *AutoLoopOrchestrator) EnqueueMessage(content string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.taskQueue = append(o.taskQueue, content)
}

// QueueDepth returns the number of pending messages in the task queue.
func (o *AutoLoopOrchestrator) QueueDepth() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.taskQueue)
}

// findPlanTask locates a PlannedTask by ID within the active plan.
// Returns nil if no plan is active or the task ID is not found.
func (o *AutoLoopOrchestrator) findPlanTask(id string) *PlannedTask {
	if o.plan == nil {
		return nil
	}
	for i := range o.plan.Tasks {
		if o.plan.Tasks[i].ID == id {
			return o.plan.Tasks[i]
		}
	}
	return nil
}

// findReadyTasks returns all tasks that have NodeReady status and are
// not already tracked in activeTasks. Requires planLock to be held.
func (o *AutoLoopOrchestrator) findReadyTasks() []*PlannedTask {
	if o.plan == nil {
		return nil
	}
	var ready []*PlannedTask
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, task := range o.plan.Tasks {
		if task.Status == NodeReady {
			if _, active := o.activeTasks[task.ID]; !active {
				ready = append(ready, task)
			}
		}
	}
	return ready
}

// applyParallelismLimit caps the number of concurrently scheduled tasks
// to loopConfig.MaxClients (used as a parallelism limiter). Tasks beyond
// the limit remain ready and are picked up in the next poll.
func (o *AutoLoopOrchestrator) applyParallelismLimit(tasks []*PlannedTask) []*PlannedTask {
	limit := o.loopConfig.MaxClients
	if limit <= 0 {
		limit = 5 // sensible fallback
	}
	o.mu.Lock()
	activeCount := len(o.activeTasks)
	o.mu.Unlock()

	available := limit - activeCount
	if available <= 0 {
		return nil
	}
	if len(tasks) > available {
		return tasks[:available]
	}
	return tasks
}

// submitPlanTask simulates dispatching a task to the scheduler.
// Phase 3: marks NodeRunning and logs only. Real dispatch via
// clientPool arrives in Wave 4.
func (o *AutoLoopOrchestrator) submitPlanTask(task *PlannedTask) {
	task.Status = NodeRunning
	task.SubmitTime = time.Now()
	o.mu.Lock()
	o.state = StateDispatching
	o.mu.Unlock()
	// TODO(Wave 4): real dispatch via o.clientPool.EnsureClient(...)
}

// ============================================================================
// Healer + AutoScaler Integration — Phase 4A (Wave 4A)
// ============================================================================

// PollHealing processes pending heal operations from the Healer queue
// and retries eligible failed tasks. In Phase 4A this is a stub that
// drains the queue and logs decisions; real re-dispatch via clientPool
// arrives in Wave 4B.
func (o *AutoLoopOrchestrator) PollHealing(ctx context.Context) {
	if o.healer == nil {
		return
	}

	ops := o.healer.ProcessPending(ctx)
	if len(ops) == 0 {
		return
	}

	o.mu.Lock()
	o.state = StateHealing
	o.mu.Unlock()

	for _, op := range ops {
		// Re-dispatch the task: reset status and increment attempt
		task := o.findPlanTask(op.PlanTaskID)
		if task != nil {
			o.planLock.Lock()
			task.Status = NodeReady
			task.AttemptCount = op.RetryCount
			task.ErrorMessage = ""
			o.planLock.Unlock()
		}
	}

	// Trigger immediate re-poll to pick up the healed tasks
	o.Trigger()
}

// PollScale evaluates the current queue depth and idle client count
// and issues scaling decisions. In Phase 4A this is a stub that
// evaluates but does not spawn/kill clients; real scaling via
// clientPool arrives in Wave 4B.
func (o *AutoLoopOrchestrator) PollScale(ctx context.Context) {
	if o.scaler == nil {
		return
	}

	queueDepth := o.QueueDepth()
	// idleClients = 0 until clientPool is integrated (Wave 4B)
	decision := o.scaler.Evaluate(queueDepth, 0)

	if decision.Action == "none" {
		return
	}

	o.mu.Lock()
	o.state = StateScaling
	o.mu.Unlock()

	o.scaler.Execute(decision)
	// TODO(Wave 4B): real clientPool.SpawnClient() / .KillClient()
}

// ListQueue returns a copy of the current message queue.
func (o *AutoLoopOrchestrator) ListQueue() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	result := make([]string, len(o.taskQueue))
	copy(result, o.taskQueue)
	return result
}

// ListHistory returns a list of history entries for completed/failed plans.
// This is a lightweight implementation; in Phase 5 a persistent store
// will replace this.
func (o *AutoLoopOrchestrator) ListHistory() []HistoryEntry {
	o.mu.Lock()
	defer o.mu.Unlock()
	// For now, derive history from the current plan if one exists
	if o.plan == nil {
		return nil
	}
	entries := make([]HistoryEntry, 0, len(o.plan.Tasks))
	for _, t := range o.plan.Tasks {
		if t.Status == NodeDone || t.Status == NodeFailed || t.Status == NodeBlocked {
			entries = append(entries, HistoryEntry{
				ID:          t.ID,
				Instruction: t.Instruction,
				Status:      string(t.Status),
				ErrorMessage: t.ErrorMessage,
			})
		}
	}
	return entries
}

// ProcessQueue processes one message from the queue in manual mode.
// It dequeues the message and triggers a plan execution cycle.
func (o *AutoLoopOrchestrator) ProcessQueue() {
	o.mu.Lock()
	if len(o.taskQueue) == 0 {
		o.mu.Unlock()
		return
	}
	// Dequeue the first message
	msg := o.taskQueue[0]
	o.taskQueue = o.taskQueue[1:]
	o.mu.Unlock()

	// For manual step, just log the processing
	// Full implementation in Phase 4+ handles actual task dispatch
	_ = msg
}

// HistoryEntry is a lightweight record of a completed/failed task.
type HistoryEntry struct {
	ID           string
	Instruction  string
	Status       string
	ErrorMessage string
}

// SetLoopConfig safely updates the orchestrator's loop configuration.
func (o *AutoLoopOrchestrator) SetLoopConfig(cfg LoopConfig) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.loopConfig = cfg
}

// ============================================================================
// Compile-time interface checks
// ============================================================================

// Ensure AutoLoopOrchestrator satisfies context cancellation patterns
// by implementing a basic Shutdown method compatible with the agent
// lifecycle. Full lifecycle integration is in Phase 8.
var _ interface {
	GetMode() ConversationMode
	SetMode(ConversationMode) ConversationMode
	Status() OrchestratorStatus
	Stop()
	Tick() <-chan time.Time
	Done() <-chan struct{}
	fmt.Stringer
} = (*AutoLoopOrchestrator)(nil)
