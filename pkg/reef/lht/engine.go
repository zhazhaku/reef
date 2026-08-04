package lht

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// ===================== Config =====================

// Config holds engine-wide configuration, including anti-stall hard limits.
type Config struct {
	MaxRepairRounds        int // N: same-strategy repair limit (default 3)
	MaxStrategySwitchCount int // M: strategy switch limit (default 2)
	MaxStallRounds         int // K: consecutive stall rounds (default 3)
	MaxGroundingRounds     int // max grounding Q&A rounds (default 10)
	MaxReplanRounds        int // max plan reject/replan rounds (default 5)
	MaxFinalRejectRounds   int // max final approval reject rounds (default 3)
}

// DefaultConfig returns a Config populated with standard defaults.
func DefaultConfig() Config {
	return Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
}

// ===================== Notifier =====================

// Notifier is the interface for sending notifications to users.
type Notifier interface {
	Send(channel, chatID, text string)
}

// ===================== gate reply / command types =====================

// gateReply is a user reply directed at a specific interaction gate.
type gateReply struct {
	action  string // "answer", "approve", "reject"
	content string
}

// ===================== Engine =====================

// Engine is the LHT lifecycle state machine driver.
// Each long-horizon task runs in its own goroutine via a Task.
type Engine struct {
	tasks     sync.Map     // goalID → *Task
	cfg       Config
	store     *Store
	notifier  Notifier
	EventHook func(eventType string, goalID string, data any) // SSE event callback (W5A)
}

// NewEngine creates a new LHT Engine.
func NewEngine(cfg Config, store *Store, notifier Notifier) *Engine {
	return &Engine{
		cfg:      cfg,
		store:    store,
		notifier: notifier,
	}
}

// ===================== Task =====================

// Task is the per-goal runtime state, driven by a single goroutine.
type Task struct {
	engine *Engine
	goal   *Goal
	plan   *Plan
	budget *Budget

	// Gate reply channels (P1-06: buffered=3)
	groundCh chan gateReply
	planCh   chan gateReply
	finalCh  chan gateReply

	// Command channel for pause/resume/stop/escalate
	cmdCh chan string

	// Channel for user-provided channel/chatID (set on creation)
	channel string
	chatID  string

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

// ===================== NewGoal =====================

// newGoalID generates a short unique goal ID.
func newGoalID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "g-" + hex.EncodeToString(b)
}

// NewGoal creates a new LHT goal, initializes its task, and starts the lifecycle goroutine.
// The task enters GROUNDING state immediately and begins alignment questions.
func (e *Engine) NewGoal(title, scope string, channel, chatID string) (*Goal, error) {
	goalID := newGoalID()
	now := time.Now()

	goal := &Goal{
		GoalID:      goalID,
		Description: title,
		State:       StateGrounding,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Initialize empty plan and budget.
	plan := &Plan{
		GoalID:      goalID,
		PlanVersion: "1",
		Tasks:       make([]TaskNode, 0),
		Capabilities: []string{},
	}

	budget := &Budget{
		GoalID:           goalID,
		MaxRounds:        3,
		MaxTokens:        100000,
		MaxDurationHours: 2.0,
	}

	// Persist initial state.
	if err := e.store.EnsureTaskDirs(goalID); err != nil {
		return nil, fmt.Errorf("NewGoal ensure dirs: %w", err)
	}
	if err := e.store.SaveGoal(goal); err != nil {
		return nil, fmt.Errorf("NewGoal save goal: %w", err)
	}
	if err := e.store.SavePlan(goalID, plan); err != nil {
		return nil, fmt.Errorf("NewGoal save plan: %w", err)
	}
	if err := e.store.SaveBudget(goalID, budget); err != nil {
		return nil, fmt.Errorf("NewGoal save budget: %w", err)
	}
	if err := e.store.SaveState(goalID, goal); err != nil {
		return nil, fmt.Errorf("NewGoal save state: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	task := &Task{
		engine:   e,
		goal:     goal,
		plan:     plan,
		budget:   budget,
		groundCh: make(chan gateReply, 3),
		planCh:   make(chan gateReply, 3),
		finalCh:  make(chan gateReply, 3),
		cmdCh:    make(chan string, 3),
		channel:  channel,
		chatID:   chatID,
		ctx:      ctx,
		cancel:   cancel,
	}

	e.tasks.Store(goalID, task)

	go task.run()

	return goal, nil
}

// ===================== Dispatch =====================

// Dispatch routes a user command to the appropriate task.
// Supported commands: pause, resume, stop, escalate, approve, reject.
// args[0] must be the goalID.
func (e *Engine) Dispatch(cmd string, args []string, reply UserReply) error {
	if len(args) == 0 {
		return fmt.Errorf("dispatch %s: missing goal_id argument", cmd)
	}
	goalID := args[0]

	val, ok := e.tasks.Load(goalID)
	if !ok {
		return fmt.Errorf("dispatch %s: goal %q not found", cmd, goalID)
	}
	task := val.(*Task)

	switch cmd {
	case "pause":
		return task.sendCommand("pause")
	case "resume":
		return task.sendCommand("resume")
	case "stop":
		return task.sendCommand("stop")
	case "escalate":
		return task.sendCommand("escalate")
	case "approve":
		// Route to plan or final gate based on current state.
		return task.routeApproval(reply)
	case "reject":
		return task.routeRejection(reply)
	default:
		return fmt.Errorf("dispatch: unknown command %q", cmd)
	}
}

func (t *Task) sendCommand(cmd string) error {
	select {
	case t.cmdCh <- cmd:
		return nil
	case <-t.ctx.Done():
		return fmt.Errorf("task %s is done", t.goal.GoalID)
	default:
		return fmt.Errorf("task %s command channel full", t.goal.GoalID)
	}
}

func (t *Task) routeApproval(reply UserReply) error {
	t.mu.Lock()
	state := t.goal.State
	t.mu.Unlock()

	switch state {
	case StateWaitApproval:
		select {
		case t.planCh <- gateReply{action: "approve", content: reply.Content}:
			return nil
		default:
			return fmt.Errorf("plan gate channel full")
		}
	case StateFinalApproval:
		select {
		case t.finalCh <- gateReply{action: "approve", content: reply.Content}:
			return nil
		default:
			return fmt.Errorf("final gate channel full")
		}
	default:
		return fmt.Errorf("approve not applicable in state %s", state)
	}
}

func (t *Task) routeRejection(reply UserReply) error {
	t.mu.Lock()
	state := t.goal.State
	t.mu.Unlock()

	switch state {
	case StateWaitApproval:
		select {
		case t.planCh <- gateReply{action: "reject", content: reply.Content}:
			return nil
		default:
			return fmt.Errorf("plan gate channel full")
		}
	case StateFinalApproval:
		select {
		case t.finalCh <- gateReply{action: "reject", content: reply.Content}:
			return nil
		default:
			return fmt.Errorf("final gate channel full")
		}
	default:
		return fmt.Errorf("reject not applicable in state %s", state)
	}
}

// ===================== Reply =====================

// Reply delivers a user reply to the specified interaction gate.
// Gates: "ground" (GROUNDING Q&A), "plan" (WAIT_APPROVAL), "final" (FINAL_APPROVAL).
func (e *Engine) Reply(gate string, goalID string, action string, content string) error {
	val, ok := e.tasks.Load(goalID)
	if !ok {
		return fmt.Errorf("reply: goal %q not found", goalID)
	}
	task := val.(*Task)

	gr := gateReply{action: action, content: content}

	switch gate {
	case "ground":
		select {
		case task.groundCh <- gr:
			return nil
		default:
			return fmt.Errorf("ground gate channel full for %s", goalID)
		}
	case "plan":
		select {
		case task.planCh <- gr:
			return nil
		default:
			return fmt.Errorf("plan gate channel full for %s", goalID)
		}
	case "final":
		select {
		case task.finalCh <- gr:
			return nil
		default:
			return fmt.Errorf("final gate channel full for %s", goalID)
		}
	default:
		return fmt.Errorf("reply: illegal gate %q (expected ground, plan, or final)", gate)
	}
}

// Shutdown cancels all running task goroutines and cleans up.
// It is idempotent — multiple calls are safe and will not panic.
// After shutdown, tasks will not accept new commands/replies;
// already-completed tasks are unaffected.
func (e *Engine) Shutdown() {
	e.tasks.Range(func(key, value any) bool {
		task := value.(*Task)
		task.cancel()
		return true
	})
}

// ===================== Task.run — State Machine Loop =====================

func (t *Task) run() {
	defer t.cancel()

	// Track replan and final reject counts for anti-stall.
	replanCount := 0       // WAIT_APPROVAL replan counter (limit: MaxReplanRounds)
	finalRejectCount := 0   // FINAL_APPROVAL reject counter (limit: MaxFinalRejectRounds)

	for {
		// Check for commands before processing state.
		select {
		case cmd := <-t.cmdCh:
			if t.handleCommand(cmd, &replanCount, &finalRejectCount) {
				return // task terminated
			}
			continue
		case <-t.ctx.Done():
			return // shutdown requested
		default:
		}

		t.mu.Lock()
		state := t.goal.State
		t.mu.Unlock()

		switch state {
		case StateGrounding:
			t.doGrounding()
			// Only transition to PLANNING if still in GROUNDING (may have escalated).
			t.mu.Lock()
			if t.goal.State == StateGrounding {
				t.goal.State = StatePlanning
				t.mu.Unlock()
				t.persist()
			} else {
				t.mu.Unlock()
			}

		case StatePlanning:
			t.doPlanning()
			t.transitionTo(StateWaitApproval)

		case StateWaitApproval:
			replanCount = t.doWaitApproval(replanCount)

		case StateExecuting:
			t.doExecuting()
			t.transitionTo(StateEvaluating)

		case StateEvaluating:
			t.doEvaluating()
			t.transitionTo(StateReviewing)

		case StateReviewing:
			t.doReviewing()
			t.transitionTo(StateFinalApproval)

		case StateFinalApproval:
			finalRejectCount = t.doFinalApproval(finalRejectCount)

		case StateCompleted:
			// Terminal — goroutine exits.
			return

		case StatePaused:
			if t.doPaused() {
				return // → ABORTED
			}

		case StateEscalated:
			if t.doEscalated() {
				return // → ABORTED
			}

		case StateAborted:
			// Terminal — goroutine exits.
			return
		}
	}
}

// handleCommand processes a command while the task is in any state.
// Returns true if the task should terminate.
func (t *Task) handleCommand(cmd string, replanCount, finalRejectCount *int) bool {
	t.mu.Lock()
	state := t.goal.State
	t.mu.Unlock()

	switch cmd {
	case "pause":
		// Any non-terminal state can pause.
		if IsTerminal(state) {
			return false
		}
		// Save current state as PreviousState.
		t.mu.Lock()
		t.goal.PreviousState = state
		t.goal.State = StatePaused
		t.goal.UpdatedAt = time.Now()
		t.mu.Unlock()
		t.persist()
		return false

	case "resume":
		if state != StatePaused && state != StateEscalated {
			return false
		}
		t.mu.Lock()
		prev := t.goal.PreviousState
		escFrom := t.goal.EscalatedFrom

		if state == StatePaused {
			// Restore to previous state, default to GROUNDING.
			if prev == "" {
				prev = StateGrounding
			}
			t.goal.State = prev
			t.goal.PreviousState = ""
		} else {
			// ESCALATED resume: use EscalatedFrom, default to GROUNDING.
			if escFrom == "" {
				escFrom = StateGrounding
			}
			t.goal.State = escFrom
			t.goal.EscalatedFrom = ""
		}
		t.goal.UpdatedAt = time.Now()
		t.mu.Unlock()
		t.persist()
		return false

	case "stop":
		if IsTerminal(state) {
			return false
		}
		// Direct transition to ABORTED from any non-terminal state.
		t.mu.Lock()
		t.goal.State = StateAborted
		t.goal.UpdatedAt = time.Now()
		t.mu.Unlock()
		t.persist()
		return true // terminate goroutine

	case "escalate":
		if IsTerminal(state) {
			return false
		}
		t.mu.Lock()
		t.goal.EscalatedFrom = state
		t.goal.State = StateEscalated
		t.goal.UpdatedAt = time.Now()
		t.mu.Unlock()
		t.persist()
		return false

	default:
		return false
	}
}

// ===================== State Handlers =====================

func (t *Task) doGrounding() {
	// NOTE: We intentionally do NOT reuse grounding.GroundingSession here.
	// GroundingSession uses a hard-coded MaxGroundingRounds const (10),
	// while the engine supports per-instance Config.MaxGroundingRounds.
	// Additionally, GroundingSession manages per-question state with
	// ProcessAnswer/NextQuestion/HasUnresolvedQuestions patterns that
	// are incompatible with the engine's channel-based gate design.
	// The inline groundRounds counter + cfg.MaxGroundingRounds achieves
	// the same escalation behaviour without a larger refactor.
	questions := GenerateQuestions(t.goal.Description)
	groundRounds := 0
	maxRounds := t.engine.cfg.MaxGroundingRounds
	if maxRounds <= 0 {
		maxRounds = 10
	}

	// Loop for re-grounding rounds (future: re-ask when answers insufficient).
	for {
		// Ask all questions in this round.
		for _, q := range questions {
			// Notify user with the question.
			if t.engine.notifier != nil {
				t.engine.notifier.Send(t.channel, t.chatID,
					fmt.Sprintf("[GROUNDING] %s\n(%s) 请回答:", q.Question, q.Category))
			}

			// Wait for user answer or command.
			for {
				select {
				case reply := <-t.groundCh:
					_ = reply
					goto nextQuestion

				case <-t.ctx.Done():
					return

				case cmd := <-t.cmdCh:
					replanCount := 0
					finalRejectCount := 0
					if t.handleCommand(cmd, &replanCount, &finalRejectCount) {
						return // terminated
					}
					t.mu.Lock()
					s := t.goal.State
					t.mu.Unlock()
					if s == StatePaused || s == StateEscalated || s == StateAborted {
						return
					}
				}
			}
		nextQuestion:
		}

		// Full round completed — count it.
		groundRounds++
		if groundRounds >= maxRounds {
			if t.engine.notifier != nil {
				t.engine.notifier.Send(t.channel, t.chatID,
					fmt.Sprintf("[GROUNDING] 问答轮次已达上限 (%d)，升级求助。", maxRounds))
			}
			t.transitionToEscalated()
			return
		}

		// Answers accepted — proceed to planning.
		break
	}

	// Normal path: questions answered, proceed to planning.
	if t.engine.notifier != nil {
		t.engine.notifier.Send(t.channel, t.chatID,
			"[GROUNDING] 对齐完成，进入规划阶段...")
	}
}

func (t *Task) doPlanning() {
	// Simulate planning work.
	time.Sleep(100 * time.Millisecond)

	// Use existing PlanVersion if re-planning (insert/reject), otherwise start at "1".
	planVersion := t.goal.PlanVersion
	if planVersion == "" || planVersion == "0" {
		planVersion = "1"
	}

	// Classify and create a plan.
	category := ClassifyGoal(t.goal.Description)
	route := RouteForCategory(category)
	caps := GenerateCapabilities(category)

	plan := &Plan{
		GoalID:       t.goal.GoalID,
		PlanVersion:  planVersion,
		Tasks: []TaskNode{
			{
				TaskID:      "task-1",
				Description: "Execute " + string(category) + " workflow: " + route.Workflow,
				State:       StateGrounding,
			},
		},
		Capabilities: caps,
	}

	t.mu.Lock()
	t.plan = plan
	t.goal.PlanRef = t.goal.GoalID
	t.goal.PlanVersion = plan.PlanVersion
	t.mu.Unlock()

	// Persist plan.
	t.engine.store.SavePlan(t.goal.GoalID, plan)

	if t.engine.notifier != nil {
		t.engine.notifier.Send(t.channel, t.chatID,
			fmt.Sprintf("[PLANNING] 计划已生成: workflow=%s, 任务数=%d, 能力=%v\n请批准或打回。",
				route.Workflow, len(plan.Tasks), caps))
	}
}

func (t *Task) doWaitApproval(replanCount int) int {
	// Notify user to approve or reject.
	if t.engine.notifier != nil && replanCount == 0 {
		t.engine.notifier.Send(t.channel, t.chatID,
			"[WAIT_APPROVAL] 等待计划批准。回复 approve 批准，reject 打回重规划。")
	}

	for {
		select {
		case <-t.ctx.Done():
			return replanCount
		case reply := <-t.planCh:
			switch reply.action {
			case "approve":
				if t.engine.notifier != nil {
					t.engine.notifier.Send(t.channel, t.chatID,
						"[WAIT_APPROVAL] 计划已批准，进入执行阶段。")
				}
				t.transitionTo(StateExecuting)
				return 0 // reset replan count on successful approval

			case "reject":
				replanCount++
				if replanCount >= t.engine.cfg.MaxReplanRounds {
					if t.engine.notifier != nil {
						t.engine.notifier.Send(t.channel, t.chatID,
							"[WAIT_APPROVAL] 打回次数已达上限，升级求助。")
					}
					t.transitionToEscalated()
					return replanCount
				}
				if t.engine.notifier != nil {
					t.engine.notifier.Send(t.channel, t.chatID,
						fmt.Sprintf("[WAIT_APPROVAL] 计划被打回（%d/%d），重新规划...",
							replanCount, t.engine.cfg.MaxReplanRounds))
				}
				t.transitionTo(StatePlanning)
				return replanCount

			case "insert":
				// Full replan: insert forces a complete re-planning phase.
				// Clear existing plan, bump PlanVersion, go back to PLANNING
				// so doPlanning() regenerates with the new requirement included.
				t.mu.Lock()
				ver, err := strconv.Atoi(t.goal.PlanVersion)
				if err != nil || ver < 1 {
					ver = 1
				}
				ver++
				t.goal.PlanVersion = fmt.Sprintf("%d", ver)
				t.plan = nil
				t.mu.Unlock()
				if t.engine.notifier != nil {
					t.engine.notifier.Send(t.channel, t.chatID,
						fmt.Sprintf("[WAIT_APPROVAL] 已插入新需求「%s」，开始重新规划（plan v%s）...",
							reply.content, t.goal.PlanVersion))
				}
				t.transitionTo(StatePlanning)
				return replanCount
			}

		case cmd := <-t.cmdCh:
			if t.handleCommand(cmd, &replanCount, &replanCount) {
				return replanCount
			}
			t.mu.Lock()
			s := t.goal.State
			t.mu.Unlock()
			if s != StateWaitApproval {
				return replanCount
			}
		}
	}
}

func (t *Task) doExecuting() {
	// Simulate task execution.
	time.Sleep(100 * time.Millisecond)

	if t.engine.notifier != nil {
		t.engine.notifier.Send(t.channel, t.chatID,
			"[EXECUTING] 子任务执行完成，进入评估阶段。")
	}
}

func (t *Task) doEvaluating() {
	// Simulate evaluation — always PASS in v1.
	time.Sleep(100 * time.Millisecond)

	if t.engine.notifier != nil {
		t.engine.notifier.Send(t.channel, t.chatID,
			"[EVALUATING] 评估 PASS，进入评审阶段。")
	}
}

func (t *Task) doReviewing() {
	// Simulate review — always pass in v1.
	time.Sleep(100 * time.Millisecond)

	if t.engine.notifier != nil {
		t.engine.notifier.Send(t.channel, t.chatID,
			"[REVIEWING] 评审通过，进入终审阶段。")
	}
}

func (t *Task) doFinalApproval(rejectCount int) int {
	if t.engine.notifier != nil && rejectCount == 0 {
		t.engine.notifier.Send(t.channel, t.chatID,
			"[FINAL_APPROVAL] 等待终审。回复 approve 批准完成，reject 打回重新执行。")
	}

	for {
		select {
		case <-t.ctx.Done():
			return rejectCount
		case reply := <-t.finalCh:
			switch reply.action {
			case "approve":
				if t.engine.notifier != nil {
					t.engine.notifier.Send(t.channel, t.chatID,
						"[FINAL_APPROVAL] 终审通过，任务完成！")
				}
				t.transitionTo(StateCompleted)
				return 0

			case "reject":
				rejectCount++
				if rejectCount >= t.engine.cfg.MaxFinalRejectRounds {
					if t.engine.notifier != nil {
						t.engine.notifier.Send(t.channel, t.chatID,
							"[FINAL_APPROVAL] 终审打回已达上限，升级求助。")
					}
					t.transitionToEscalated()
					return rejectCount
				}
				if t.engine.notifier != nil {
					t.engine.notifier.Send(t.channel, t.chatID,
						fmt.Sprintf("[FINAL_APPROVAL] 终审打回（%d/%d），返回执行。",
							rejectCount, t.engine.cfg.MaxFinalRejectRounds))
				}
				t.transitionTo(StateExecuting)
				return rejectCount
			}

		case cmd := <-t.cmdCh:
			if t.handleCommand(cmd, &rejectCount, &rejectCount) {
				return rejectCount
			}
			t.mu.Lock()
			s := t.goal.State
			t.mu.Unlock()
			if s != StateFinalApproval {
				return rejectCount
			}
		}
	}
}

func (t *Task) doPaused() bool {
	// Notify.
	if t.engine.notifier != nil {
		t.engine.notifier.Send(t.channel, t.chatID,
			"[PAUSED] 任务已暂停。resume 恢复 / stop 终止。")
	}

	for {
		select {
		case <-t.ctx.Done():
			return false
		case cmd := <-t.cmdCh:
			switch cmd {
			case "resume":
				t.mu.Lock()
				prev := t.goal.PreviousState
				if prev == "" {
					prev = StateGrounding
				}
				t.goal.State = prev
				t.goal.PreviousState = ""
				t.goal.UpdatedAt = time.Now()
				t.mu.Unlock()
				t.persist()
				return false

			case "stop":
				t.mu.Lock()
				t.goal.State = StateAborted
				t.goal.UpdatedAt = time.Now()
				t.mu.Unlock()
				t.persist()
				return true // terminate

			default:
				// ignore other commands while paused
			}
		}
	}
}

func (t *Task) doEscalated() bool {
	if t.engine.notifier != nil {
		t.engine.notifier.Send(t.channel, t.chatID,
			"[ESCALATED] 任务已升级求助，等待人工介入。resume 恢复 / stop 终止。")
	}

	for {
		select {
		case <-t.ctx.Done():
			return false
		case cmd := <-t.cmdCh:
			switch cmd {
			case "resume":
				t.mu.Lock()
				escFrom := t.goal.EscalatedFrom
				if escFrom == "" {
					escFrom = StateGrounding
				}
				t.goal.State = escFrom
				t.goal.EscalatedFrom = ""
				t.goal.UpdatedAt = time.Now()
				t.mu.Unlock()
				t.persist()
				return false

			case "stop":
				t.mu.Lock()
				t.goal.State = StateAborted
				t.goal.UpdatedAt = time.Now()
				t.mu.Unlock()
				t.persist()
				return true

			default:
				// ignore other commands
			}
		}
	}
}

// ===================== State Transition Helpers =====================

// transitionTo transitions the task to a new state and persists.
func (t *Task) transitionTo(newState State) {
	t.mu.Lock()
	oldState := t.goal.State
	t.goal.State = newState
	t.goal.UpdatedAt = time.Now()

	// Set recovery anchors when transitioning to PAUSED or ESCALATED.
	switch newState {
	case StatePaused:
		if t.goal.PreviousState == "" {
			t.goal.PreviousState = oldState
		}
	case StateEscalated:
		if t.goal.EscalatedFrom == "" {
			t.goal.EscalatedFrom = oldState
		}
	}
	t.mu.Unlock()
	t.persist()

	// Fire SSE event via EventHook (W5A).
	t.fireEvent("lht_state", map[string]interface{}{
		"goal_id":    t.goal.GoalID,
		"old_state":  string(oldState),
		"new_state":  string(newState),
		"updated_at": t.goal.UpdatedAt,
	})
}

// transitionToEscalated transitions to ESCALATED, capturing the current state as EscalatedFrom.
func (t *Task) transitionToEscalated() {
	t.mu.Lock()
	oldState := t.goal.State
	t.goal.State = StateEscalated
	t.goal.EscalatedFrom = oldState
	t.goal.UpdatedAt = time.Now()
	t.mu.Unlock()
	t.persist()

	// Fire SSE event via EventHook (W5A).
	t.fireEvent("lht_state", map[string]interface{}{
		"goal_id":    t.goal.GoalID,
		"old_state":  string(oldState),
		"new_state":  string(StateEscalated),
		"updated_at": t.goal.UpdatedAt,
	})
}

// fireEvent invokes the engine's EventHook if set (W5A).
func (t *Task) fireEvent(eventType string, data any) {
	if t.engine.EventHook != nil {
		t.engine.EventHook(eventType, t.goal.GoalID, data)
	}
}

// persist saves the current goal state to state.json and goal.json.
func (t *Task) persist() {
	t.mu.Lock()
	goalCopy := *t.goal
	t.mu.Unlock()

	_ = t.engine.store.SaveState(goalCopy.GoalID, &goalCopy)
	_ = t.engine.store.SaveGoal(&goalCopy)
}
