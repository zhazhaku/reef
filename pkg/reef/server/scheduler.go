package server

import (
	"fmt"
	"sync"

	"github.com/sipeed/reef/pkg/reef"
)

// Scheduler matches tasks to available Clients and handles dispatch.
type Scheduler struct {
	registry *Registry
	queue    *TaskQueue

	mu    sync.RWMutex
	tasks map[string]*reef.Task // global task index by ID

	maxEscalations int
	onDispatch     func(taskID, clientID string) error
	onRequeue      func(task *reef.Task)
}

// SchedulerOptions configures the scheduler.
type SchedulerOptions struct {
	MaxEscalations int
	OnDispatch     func(taskID, clientID string) error
	OnRequeue      func(task *reef.Task)
}

// NewScheduler creates a scheduler bound to a registry and queue.
func NewScheduler(registry *Registry, queue *TaskQueue, opts SchedulerOptions) *Scheduler {
	if opts.MaxEscalations < 0 {
		opts.MaxEscalations = 2
	}
	return &Scheduler{
		registry:       registry,
		queue:          queue,
		tasks:          make(map[string]*reef.Task),
		maxEscalations: opts.MaxEscalations,
		onDispatch:     opts.OnDispatch,
		onRequeue:      opts.OnRequeue,
	}
}

// Submit creates a new task and attempts to schedule it immediately.
// If no matching client is available, the task is queued.
func (s *Scheduler) Submit(task *reef.Task) error {
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()

	if err := task.Transition(reef.TaskQueued); err != nil {
		return fmt.Errorf("transition to queued: %w", err)
	}

	if err := s.queue.Enqueue(task); err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}

	// Try immediate dispatch
	s.TryDispatch()
	return nil
}

// TryDispatch attempts to match and dispatch the next task from the queue.
// It processes tasks in FIFO order until the queue is empty or no match is found.
func (s *Scheduler) TryDispatch() {
	for {
		task := s.queue.Dequeue()
		if task == nil {
			return
		}

		client := s.matchClient(task)
		if client == nil {
			// No match available — put it back at the head and stop.
			_ = s.queue.Enqueue(task) // should never fail since we just dequeued
			if s.onRequeue != nil {
				s.onRequeue(task)
			}
			return
		}

		// Attempt dispatch
		if err := s.dispatch(task, client); err != nil {
			// Dispatch failed — put back and try next
			_ = s.queue.Enqueue(task)
			return
		}
	}
}

// matchClient finds the best-fit client for a task.
// Algorithm: role match → skill coverage → capacity → lowest current load.
func (s *Scheduler) matchClient(task *reef.Task) *reef.ClientInfo {
	candidates := s.registry.List()
	var best *reef.ClientInfo
	for _, c := range candidates {
		if !c.IsAvailable() {
			continue
		}
		if !c.Matches(task.RequiredRole, task.RequiredSkills) {
			continue
		}
		if best == nil || c.CurrentLoad < best.CurrentLoad {
			best = c
		}
	}
	return best
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
		if err := s.onDispatch(task.ID, client.ID); err != nil {
			// Rollback
			s.registry.DecrementLoad(client.ID)
			task.AssignedClient = ""
			_ = task.Transition(reef.TaskQueued)
			return fmt.Errorf("onDispatch hook failed: %w", err)
		}
	}
	return nil
}

// HandleTaskCompleted processes a task completion report from a client.
func (s *Scheduler) HandleTaskCompleted(taskID string, result *reef.TaskResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if task.Status != reef.TaskRunning {
		return fmt.Errorf("task %s not in Running state (was %s)", taskID, task.Status)
	}

	task.Result = result
	if err := task.Transition(reef.TaskCompleted); err != nil {
		return err
	}
	s.registry.DecrementLoad(task.AssignedClient)
	task.AssignedClient = ""

	// Trigger re-schedule in case queued tasks can now be dispatched
	go s.TryDispatch()
	return nil
}

// HandleTaskFailed processes a task failure report from a client.
func (s *Scheduler) HandleTaskFailed(taskID string, taskErr *reef.TaskError, attemptHistory []reef.AttemptRecord) error {
	s.mu.Lock()

	task, ok := s.tasks[taskID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("task %s not found", taskID)
	}
	if task.Status != reef.TaskRunning {
		s.mu.Unlock()
		return fmt.Errorf("task %s not in Running state (was %s)", taskID, task.Status)
	}

	task.Error = taskErr
	task.AttemptHistory = append(task.AttemptHistory, attemptHistory...)
	s.registry.DecrementLoad(task.AssignedClient)

	decision := s.escalate(task)
	needsRequeue := false
	switch decision {
	case EscalationReassign:
		task.EscalationCount++
		task.AssignedClient = ""
		_ = task.Transition(reef.TaskQueued)
		needsRequeue = true
	case EscalationTerminate:
		_ = task.Transition(reef.TaskFailed)
	case EscalationToAdmin:
		_ = task.Transition(reef.TaskFailed)
		// TODO: emit admin alert
	}
	s.mu.Unlock()

	if needsRequeue {
		_ = s.queue.Enqueue(task)
		go s.TryDispatch()
	}
	return nil
}

// HandleClientAvailable triggers re-scheduling when a client becomes available.
func (s *Scheduler) HandleClientAvailable(clientID string) {
	_ = clientID // may be used for logging or metrics
	go s.TryDispatch()
}

// GetTask returns a task by ID.
func (s *Scheduler) GetTask(taskID string) *reef.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks[taskID]
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

// ---------------------------------------------------------------------------
// Escalation
// ---------------------------------------------------------------------------

// EscalationDecision enumerates possible escalation outcomes.
type EscalationDecision string

const (
	EscalationReassign EscalationDecision = "reassign"
	EscalationTerminate EscalationDecision = "terminate"
	EscalationToAdmin   EscalationDecision = "to_admin"
)

// escalate decides what to do with a failed task.
func (s *Scheduler) escalate(task *reef.Task) EscalationDecision {
	if task.EscalationCount >= s.maxEscalations {
		return EscalationToAdmin
	}
	// Check if another client is available
	if s.matchClient(task) != nil {
		return EscalationReassign
	}
	// No other client available — try once more later or terminate
	return EscalationTerminate
}
