// Reef - Distributed multi-agent swarm orchestration system
// Based on PicoClaw (github.com/sipeed/picoclaw)
//
// ServerBridge implements reef.ReefBridge by delegating to the
// in-process Scheduler and Registry. Used when the AgentLoop
// runs in the same process as the Reef Server (picoclaw server mode).

package server

import (
	"fmt"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

// ServerBridge implements reef.ReefBridge for in-process access.
type ServerBridge struct {
	scheduler *Scheduler
	registry  *Registry
}

// NewServerBridge creates a bridge backed by the given scheduler and registry.
func NewServerBridge(scheduler *Scheduler, registry *Registry) *ServerBridge {
	return &ServerBridge{
		scheduler: scheduler,
		registry:  registry,
	}
}

// SubmitTask submits a task to the in-process scheduler.
func (b *ServerBridge) SubmitTask(instruction string, requiredRole string, requiredSkills []string, opts reef.TaskOptions) (string, error) {
	if instruction == "" {
		return "", fmt.Errorf("instruction is required")
	}
	if requiredRole == "" {
		return "", fmt.Errorf("required_role is required")
	}

	task := reef.NewTask(
		generateTaskID(),
		instruction,
		requiredRole,
		requiredSkills,
	)
	if opts.MaxRetries > 0 {
		task.MaxRetries = opts.MaxRetries
	}
	if opts.TimeoutMs > 0 {
		task.TimeoutMs = opts.TimeoutMs
	}
	if opts.ModelHint != "" {
		task.ModelHint = opts.ModelHint
	}
	if opts.ReplyTo != nil && !opts.ReplyTo.IsZero() {
		task.ReplyTo = opts.ReplyTo
	}

	if err := b.scheduler.Submit(task); err != nil {
		return "", fmt.Errorf("submit task: %w", err)
	}
	return task.ID, nil
}

// QueryTask returns a snapshot of the task's current state.
func (b *ServerBridge) QueryTask(taskID string) (*reef.TaskSnapshot, error) {
	task := b.scheduler.GetTask(taskID)
	if task == nil {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	return &reef.TaskSnapshot{
		TaskID:          task.ID,
		Status:          string(task.Status),
		Instruction:     task.Instruction,
		AssignedClient:  task.AssignedClient,
		Result:          task.Result,
		Error:           task.Error,
		AttemptHistory:  task.AttemptHistory,
		EscalationCount: task.EscalationCount,
		CreatedAt:       task.CreatedAt,
		UpdatedAt:       time.Now(),
	}, nil
}

// Status returns the overall system status.
func (b *ServerBridge) Status() (*reef.SystemStatus, error) {
	clients := b.registry.List()
	status := &reef.SystemStatus{
		Clients: make([]reef.ClientSnapshot, 0, len(clients)),
	}
	for _, c := range clients {
		switch c.State {
		case reef.ClientConnected:
			status.ConnectedClients++
		case reef.ClientDisconnected:
			status.DisconnectedCount++
		}
		status.Clients = append(status.Clients, reef.ClientSnapshot{
			ClientID:    c.ID,
			Role:        c.Role,
			Skills:      c.Skills,
			State:       string(c.State),
			CurrentLoad: c.CurrentLoad,
			Capacity:    c.Capacity,
		})
	}

	tasks := b.scheduler.TasksSnapshot()
	for _, t := range tasks {
		switch t.Status {
		case reef.TaskQueued:
			status.QueuedTasks++
		case reef.TaskRunning:
			status.RunningTasks++
		case reef.TaskCompleted:
			status.CompletedTasks++
		case reef.TaskFailed, reef.TaskEscalated:
			status.FailedTasks++
		}
	}

	_ = time.Now() // ensure time import
	return status, nil
}
