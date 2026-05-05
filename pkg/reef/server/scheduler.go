package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server/notify"
)

// Scheduler matches tasks to available Clients and handles dispatch.
type Scheduler struct {
	registry *Registry
	queue    Queue
	logger   *slog.Logger

	mu    sync.RWMutex
	tasks map[string]*reef.Task

	maxEscalations     int
	webhookURLs        []string
	notifyManager      *notify.Manager
	matchStrategy      MatchStrategy
	onDispatch         func(task *reef.Task, clientID string) error
	onRequeue          func(task *reef.Task)
	onTaskStateChanged func(task *reef.Task)
	resultCallback     func(task *reef.Task, result *reef.TaskResult, taskErr *reef.TaskError)
}

// SchedulerOptions configures the scheduler.
type SchedulerOptions struct {
	MaxEscalations     int
	WebhookURLs        []string
	Logger             *slog.Logger
	NotifyManager      *notify.Manager
	MatchStrategy      MatchStrategy
	OnDispatch         func(task *reef.Task, clientID string) error
	OnRequeue          func(task *reef.Task)
	OnTaskStateChanged func(task *reef.Task)
	ResultCallback     func(task *reef.Task, result *reef.TaskResult, taskErr *reef.TaskError)
}

// NewScheduler creates a scheduler bound to a registry and queue.
func NewScheduler(registry *Registry, queue Queue, opts SchedulerOptions) *Scheduler {
	if opts.MaxEscalations < 0 {
		opts.MaxEscalations = 2
	}
	if opts.Logger == nil {
		opts.Logger = nil // remain nil-safe; slog.Default() panics on some runtimes
	}
	return &Scheduler{
		registry:           registry,
		queue:              queue,
		logger:             opts.Logger,
		tasks:              make(map[string]*reef.Task),
		maxEscalations:     opts.MaxEscalations,
		webhookURLs:        opts.WebhookURLs,
		notifyManager:      opts.NotifyManager,
		matchStrategy:      opts.MatchStrategy,
		onDispatch:         opts.OnDispatch,
		onRequeue:          opts.OnRequeue,
		onTaskStateChanged: opts.OnTaskStateChanged,
		resultCallback:     opts.ResultCallback,
	}
}

// Submit creates a new task and attempts to schedule it immediately.
func (s *Scheduler) Submit(task *reef.Task) error {
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()

	if task.Status != reef.TaskQueued {
		if err := task.Transition(reef.TaskQueued); err != nil {
			return fmt.Errorf("transition to queued: %w", err)
		}
	}

	if err := s.queue.Enqueue(task); err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}

	s.TryDispatch()
	return nil
}

// TryDispatch attempts to match and dispatch the next task from the queue.
// It scans through queued tasks and dispatches all that can be matched,
// skipping tasks with no available client (preventing head-of-line blocking).
func (s *Scheduler) TryDispatch() {
	// Collect tasks that can't be dispatched right now
	var unmatchable []*reef.Task

	for {
		task := s.queue.Peek()
		if task == nil {
			break
		}

		excludeID := ""
		for i := len(task.AttemptHistory) - 1; i >= 0; i-- {
			if task.AttemptHistory[i].Status == "failed" && task.AttemptHistory[i].ClientID != "" {
				excludeID = task.AttemptHistory[i].ClientID
				break
			}
		}

		client := s.matchClient(task, excludeID)
		if client == nil {
			// Can't match this task right now — remove from queue and hold aside
			_ = s.queue.Dequeue()
			unmatchable = append(unmatchable, task)
			continue
		}

		_ = s.queue.Dequeue()

		if err := s.dispatch(task, client); err != nil {
			// Dispatch failed — re-enqueue this task and stop
			_ = s.queue.Enqueue(task)
			break
		}
	}

	// Put unmatchable tasks back into the queue (preserving order)
	for _, t := range unmatchable {
		_ = s.queue.Enqueue(t)
	}
}

// matchClient finds the best-fit client for a task.
func (s *Scheduler) matchClient(task *reef.Task, excludeID string) *reef.ClientInfo {
	candidates := s.registry.List()
	eligible := make([]*reef.ClientInfo, 0, len(candidates))
	for _, c := range candidates {
		if c.ID == excludeID {
			if s.logger != nil {
				s.logger.Debug("matchClient: excluded client",
					slog.String("task_id", task.ID), slog.String("client_id", c.ID))
			}
			continue
		}
		if !c.IsAvailable() {
			if s.logger != nil {
				s.logger.Debug("matchClient: client not available",
					slog.String("task_id", task.ID), slog.String("client_id", c.ID),
					slog.String("state", string(c.State)),
					slog.Int("load", c.CurrentLoad), slog.Int("capacity", c.Capacity))
			}
			continue
		}
		if !c.Matches(task.RequiredRole, task.RequiredSkills) {
			if s.logger != nil {
				s.logger.Warn("matchClient: role/skill mismatch",
					slog.String("task_id", task.ID),
					slog.String("client_id", c.ID),
					slog.String("client_role", c.Role),
					slog.Any("client_skills", c.Skills),
					slog.String("required_role", task.RequiredRole),
					slog.Any("required_skills", task.RequiredSkills))
			}
			continue
		}
		eligible = append(eligible, c)
	}

	if len(eligible) == 0 {
		if s.logger != nil {
			s.logger.Info("matchClient: no eligible clients",
				slog.String("task_id", task.ID),
				slog.Int("total_candidates", len(candidates)),
				slog.String("required_role", task.RequiredRole),
				slog.Any("required_skills", task.RequiredSkills))
		}
		return nil
	}

	strategy := s.matchStrategy
	if strategy == nil {
		strategy = &LeastLoadStrategy{}
	}
	selected := strategy.Select(eligible)
	if s.logger != nil {
		s.logger.Info("matchClient: selected client",
			slog.String("task_id", task.ID),
			slog.String("client_id", selected.ID),
			slog.Int("eligible_count", len(eligible)))
	}
	return selected
}

// dispatch assigns a task to a client and updates state.
func (s *Scheduler) dispatch(task *reef.Task, client *reef.ClientInfo) error {
	if err := task.Transition(reef.TaskAssigned); err != nil {
		return err
	}
	if err := task.Transition(reef.TaskRunning); err != nil {
		return err
	}

	task.AssignedClient = client.ID
	s.registry.IncrementLoad(client.ID)

	if s.onDispatch != nil {
		if err := s.onDispatch(task, client.ID); err != nil {
			s.registry.DecrementLoad(client.ID)
			task.AssignedClient = ""
			_ = task.Transition(reef.TaskQueued)
			return fmt.Errorf("onDispatch hook failed: %w", err)
		}
	}
	return nil
}

// HandleTaskCompleted processes a task completion report from a client.
// If the task was already marked as Failed due to timeout but the client
// actually completed it, the result is accepted and the task is restored
// to Completed status (late completion recovery).
func (s *Scheduler) HandleTaskCompleted(taskID string, result *reef.TaskResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Normal path: task is still Running
	if task.Status == reef.TaskRunning {
		task.Result = result
		if err := task.Transition(reef.TaskCompleted); err != nil {
			return err
		}
		s.registry.DecrementLoad(task.AssignedClient)
		task.AssignedClient = ""

		if s.onTaskStateChanged != nil {
			s.onTaskStateChanged(task)
		}
		if s.resultCallback != nil {
			s.resultCallback(task, result, nil)
		}
		go s.TryDispatch()
		return nil
	}

	// Late completion recovery: task was timed out / paused but client finished
	if task.Status == reef.TaskFailed || task.Status == reef.TaskPaused {
		if s.logger != nil {
			s.logger.Info("late completion: recovering task from terminal state",
				slog.String("task_id", taskID),
				slog.String("was_status", string(task.Status)))
		}

		// Overwrite the timeout/failure result with the actual result
		task.Result = result
		task.Error = nil // clear timeout error — task actually succeeded

		// Force transition: Failed/Paused → Queued → Completed
		// We can't go directly Failed → Completed, so we reset through Queued
		task.Status = reef.TaskQueued
		if err := task.Transition(reef.TaskCompleted); err != nil {
			// If that still fails, force-set the status
			if s.logger != nil {
				s.logger.Warn("late completion: transition failed, force-setting status",
					slog.String("task_id", taskID),
					slog.String("error", err.Error()))
			}
			task.Status = reef.TaskCompleted
			now := time.Now()
			task.CompletedAt = &now
		}

		s.registry.DecrementLoad(task.AssignedClient)
		task.AssignedClient = ""

		if s.onTaskStateChanged != nil {
			s.onTaskStateChanged(task)
		}
		if s.resultCallback != nil {
			s.resultCallback(task, result, nil)
		}
		return nil
	}

	// Already terminal (Completed/Cancelled) — idempotent no-op
	if task.Status.IsTerminal() {
		return nil
	}

	return fmt.Errorf("task %s in unexpected state %s", taskID, task.Status)
}

// HandleTaskFailed processes a task failure report from a client.
func (s *Scheduler) HandleTaskFailed(taskID string, taskErr *reef.TaskError, attemptHistory []reef.AttemptRecord) error {
	s.mu.Lock()

	task, ok := s.tasks[taskID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("task %s not found", taskID)
	}
	if task.Status.IsTerminal() {
		s.mu.Unlock()
		return nil // idempotent
	}
	if task.Status != reef.TaskRunning {
		s.mu.Unlock()
		return fmt.Errorf("task %s not in Running state (was %s)", taskID, task.Status)
	}

	failedClient := task.AssignedClient
	if len(attemptHistory) == 0 {
		attemptHistory = []reef.AttemptRecord{{
			AttemptNumber: len(task.AttemptHistory) + 1,
			StartedAt:     time.Now(),
			EndedAt:       time.Now(),
			Status:        "failed",
			ErrorMessage:  taskErr.Message,
			ClientID:      failedClient,
		}}
	} else {
		for i := range attemptHistory {
			if attemptHistory[i].ClientID == "" {
				attemptHistory[i].ClientID = failedClient
			}
		}
	}
	task.Error = taskErr
	task.AttemptHistory = append(task.AttemptHistory, attemptHistory...)
	s.registry.DecrementLoad(failedClient)

	_ = task.Transition(reef.TaskFailed)

	decision := s.escalate(task)
	needsRequeue := false
	switch decision {
	case EscalationReassign:
		task.EscalationCount++
		task.AssignedClient = ""
		_ = task.Transition(reef.TaskQueued)
		needsRequeue = true
	case EscalationTerminate:
	case EscalationToAdmin:
		_ = task.Transition(reef.TaskEscalated)
		alert := notify.Alert{
			Event:           "task_escalated",
			TaskID:          task.ID,
			Status:          string(task.Status),
			Instruction:     task.Instruction,
			RequiredRole:    task.RequiredRole,
			Error:           task.Error,
			AttemptHistory:  task.AttemptHistory,
			EscalationCount: task.EscalationCount,
			MaxEscalations:  s.maxEscalations,
			Timestamp:       time.Now(),
		}
		if s.notifyManager != nil {
			go s.notifyManager.NotifyAll(context.Background(), alert)
		} else {
			go sendWebhookAlert(s.logger, s.webhookURLs, WebhookPayload{
				Event:           alert.Event,
				TaskID:          alert.TaskID,
				Status:          alert.Status,
				Instruction:     alert.Instruction,
				RequiredRole:    alert.RequiredRole,
				Error:           alert.Error,
				AttemptHistory:  alert.AttemptHistory,
				EscalationCount: alert.EscalationCount,
				MaxEscalations:  alert.MaxEscalations,
				Timestamp:       alert.Timestamp.UnixMilli(),
			})
		}
	}
	s.mu.Unlock()

	if needsRequeue {
		_ = s.queue.Enqueue(task)
		go s.TryDispatch()
	}

	if s.onTaskStateChanged != nil {
		s.onTaskStateChanged(task)
	}
	if s.resultCallback != nil {
		s.resultCallback(task, nil, taskErr)
	}

	return nil
}

// HandleTaskTimedOut marks a running task as failed due to timeout.
// This method is safe to call from any goroutine (e.g., TimeoutScanner).
func (s *Scheduler) HandleTaskTimedOut(taskID string, elapsed time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if task.Status.IsTerminal() {
		return nil // already terminal — no-op
	}
	if task.Status != reef.TaskRunning {
		return fmt.Errorf("task %s not in Running state (was %s)", taskID, task.Status)
	}

	task.Error = &reef.TaskError{
		Type:    "timeout",
		Message: "task exceeded timeout",
		Detail:  "elapsed: " + elapsed.String(),
	}
	_ = task.Transition(reef.TaskFailed)
	s.registry.DecrementLoad(task.AssignedClient)
	task.AssignedClient = ""

	if s.onTaskStateChanged != nil {
		s.onTaskStateChanged(task)
	}
	if s.resultCallback != nil {
		s.resultCallback(task, nil, task.Error)
	}

	go s.TryDispatch()
	return nil
}

// HandleTaskPaused marks a running task as paused (e.g., due to client disconnect).
// This method is safe to call from any goroutine (e.g., heartbeatScanner).
func (s *Scheduler) HandleTaskPaused(taskID string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if task.Status.IsTerminal() {
		return nil
	}
	if task.Status != reef.TaskRunning {
		return fmt.Errorf("task %s not in Running state (was %s)", taskID, task.Status)
	}

	_ = task.Transition(reef.TaskPaused)
	task.PauseReason = reason

	if s.onTaskStateChanged != nil {
		s.onTaskStateChanged(task)
	}
	return nil
}

// HandleClientAvailable triggers re-scheduling when a client becomes available.
func (s *Scheduler) HandleClientAvailable(clientID string) {
	_ = clientID
	go s.TryDispatch()
}

// GetTask returns a task by ID.
func (s *Scheduler) GetTask(taskID string) *reef.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks[taskID]
}

// RegisterTask adds a task to the scheduler's index without enqueuing it.
func (s *Scheduler) RegisterTask(task *reef.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
}

// TasksSnapshot returns a shallow copy of all known tasks.
func (s *Scheduler) TasksSnapshot() []*reef.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*reef.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}

// EscalationDecision enumerates possible escalation outcomes.
type EscalationDecision string

const (
	EscalationReassign  EscalationDecision = "reassign"
	EscalationTerminate EscalationDecision = "terminate"
	EscalationToAdmin   EscalationDecision = "to_admin"
)

func (s *Scheduler) escalate(task *reef.Task) EscalationDecision {
	if task.EscalationCount >= s.maxEscalations {
		return EscalationToAdmin
	}
	if s.matchClient(task, task.AssignedClient) != nil {
		return EscalationReassign
	}
	return EscalationTerminate
}

// HandleTaskProgress records a progress heartbeat from a Client.
func (s *Scheduler) HandleTaskProgress(taskID string, percent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return
	}
	task.ProgressPercent = percent
	if s.onTaskStateChanged != nil {
		s.onTaskStateChanged(task)
	}
}
