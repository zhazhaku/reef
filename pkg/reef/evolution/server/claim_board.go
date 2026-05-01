// Package server implements the server-side evolution engine components.
package server

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

// ---------------------------------------------------------------------------
// Interfaces (reused from existing server package)
// ---------------------------------------------------------------------------

// TaskSubmitter is the interface ClaimBoard uses to dispatch tasks
// (either directly to a client or as a fallback to the scheduler).
type TaskSubmitter interface {
	Submit(task *reef.Task) error
	DispatchTask(clientID string, task *reef.Task) error
}

// ConnManager is the interface for sending messages to connected clients.
type ConnManager interface {
	SendToClient(clientID string, msg reef.Message) error
}

// RoleFinder is the interface for looking up clients by role.
type RoleFinder interface {
	Get(clientID string) *reef.ClientInfo
	FindByRole(role string) []*reef.ClientInfo
}

// ---------------------------------------------------------------------------
// ClaimConfig
// ---------------------------------------------------------------------------

// ClaimConfig holds configuration parameters for the ClaimBoard.
type ClaimConfig struct {
	// ClaimTimeout is how long a task remains on the board before expiring.
	// Default: 30 * time.Second.
	ClaimTimeout time.Duration

	// MaxPriorityClaim is the maximum priority for tasks routed to the claim board.
	// Tasks with priority > MaxPriorityClaim go directly to the scheduler.
	// Default: 5.
	MaxPriorityClaim int

	// MaxRetriesOnExpiry is how many times an expired task is re-posted
	// to the board before falling back to the scheduler.
	// Default: 2.
	MaxRetriesOnExpiry int

	// MaxBoardSize is the maximum number of tasks allowed on the board at once.
	// Default: 50.
	MaxBoardSize int
}

// DefaultClaimConfig returns a ClaimConfig with sensible defaults.
func DefaultClaimConfig() ClaimConfig {
	return ClaimConfig{
		ClaimTimeout:       30 * time.Second,
		MaxPriorityClaim:   5,
		MaxRetriesOnExpiry: 2,
		MaxBoardSize:       50,
	}
}

// ---------------------------------------------------------------------------
// claimableTask (unexported)
// ---------------------------------------------------------------------------

// claimableTask wraps a reef.Task with claim-board metadata.
type claimableTask struct {
	Task          *reef.Task
	PostedAt      time.Time
	ClaimedBy     string // clientID, empty if unclaimed
	ExpiryRetries int
	ExpiryTimer   *time.Timer
}

// ---------------------------------------------------------------------------
// ClaimBoard
// ---------------------------------------------------------------------------

// ClaimBoard implements a Multica-style autonomous task claiming board.
// Low-priority tasks (priority ≤ MaxPriorityClaim) are posted to the board,
// eligible online clients are notified, and claims are handled first-come-first-served.
type ClaimBoard struct {
	mu          sync.Mutex
	tasks       map[string]*claimableTask // taskID → claimableTask
	scheduler   TaskSubmitter
	registry    RoleFinder
	connManager ConnManager
	config      ClaimConfig
	logger      *slog.Logger
	stopCh      chan struct{}
}

// NewClaimBoard creates a new ClaimBoard with the given dependencies.
func NewClaimBoard(
	scheduler TaskSubmitter,
	registry RoleFinder,
	connManager ConnManager,
	config ClaimConfig,
	logger *slog.Logger,
) *ClaimBoard {
	if config.ClaimTimeout <= 0 {
		config.ClaimTimeout = DefaultClaimConfig().ClaimTimeout
	}
	if config.MaxPriorityClaim <= 0 {
		config.MaxPriorityClaim = DefaultClaimConfig().MaxPriorityClaim
	}
	if config.MaxRetriesOnExpiry <= 0 {
		config.MaxRetriesOnExpiry = DefaultClaimConfig().MaxRetriesOnExpiry
	}
	if config.MaxBoardSize <= 0 {
		config.MaxBoardSize = DefaultClaimConfig().MaxBoardSize
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ClaimBoard{
		tasks:       make(map[string]*claimableTask),
		scheduler:   scheduler,
		registry:    registry,
		connManager: connManager,
		config:      config,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

// ---------------------------------------------------------------------------
// Post — publish task to board
// ---------------------------------------------------------------------------

// Post publishes a task. Low-priority tasks (priority ≤ MaxPriorityClaim) go to the
// claim board; high-priority tasks are routed directly to the scheduler.
func (cb *ClaimBoard) Post(task *reef.Task) error {
	if cb.scheduler == nil {
		return fmt.Errorf("claim board: scheduler is nil, cannot post task")
	}

	// Priority check: high-priority → direct to scheduler
	if task.Priority > cb.config.MaxPriorityClaim {
		return cb.scheduler.Submit(task)
	}

	// Board size check
	cb.mu.Lock()
	if len(cb.tasks) >= cb.config.MaxBoardSize {
		cb.mu.Unlock()
		return fmt.Errorf("claim board full (max %d)", cb.config.MaxBoardSize)
	}

	// Create claimableTask
	ct := &claimableTask{
		Task:          task,
		PostedAt:      time.Now(),
		ExpiryRetries: 0,
	}
	cb.tasks[task.ID] = ct
	cb.mu.Unlock()

	// Find eligible candidates
	candidates := cb.findEligibleCandidates(task)

	// Build task_available payload
	instruction := task.Instruction
	if len(instruction) > 200 {
		instruction = instruction[:200]
	}
	availablePayload := reef.TaskAvailablePayload{
		TaskID:         task.ID,
		RequiredRole:   task.RequiredRole,
		RequiredSkills: task.RequiredSkills,
		Priority:       task.Priority,
		Instruction:    instruction,
		ExpiresAt:      time.Now().Add(cb.config.ClaimTimeout).UnixMilli(),
	}
	availableMsg, err := reef.NewMessage(reef.MsgTaskAvailable, task.ID, availablePayload)
	if err != nil {
		cb.logger.Error("failed to build task_available message", slog.String("error", err.Error()))
	}

	// Notify each candidate (non-blocking goroutines)
	for _, c := range candidates {
		clientID := c.ID
		go func() {
			if err := cb.connManager.SendToClient(clientID, availableMsg); err != nil {
				cb.logger.Warn("failed to send task_available",
					slog.String("client_id", clientID),
					slog.String("task_id", task.ID),
					slog.String("error", err.Error()),
				)
			}
		}()
	}

	// Start expiry timer
	cb.startExpiryTimer(task.ID)

	cb.logger.Info("task posted to claim board",
		slog.String("task_id", task.ID),
		slog.Int("candidates_count", len(candidates)),
	)

	return nil
}

// findEligibleCandidates filters clients by role, skills, online status, and capacity.
func (cb *ClaimBoard) findEligibleCandidates(task *reef.Task) []*reef.ClientInfo {
	clients := cb.registry.FindByRole(task.RequiredRole)
	var eligible []*reef.ClientInfo
	for _, c := range clients {
		if c.State != reef.ClientConnected || !c.IsAvailable() {
			continue
		}
		if c.CurrentLoad >= c.Capacity {
			continue
		}
		if len(task.RequiredSkills) > 0 && !c.Matches(task.RequiredRole, task.RequiredSkills) {
			continue
		}
		eligible = append(eligible, c)
	}
	return eligible
}

// ---------------------------------------------------------------------------
// HandleClaim — first-come-first-served
// ---------------------------------------------------------------------------

// HandleClaim processes a claim request from a client for a task on the board.
// Returns an error if the claim fails (task not on board, already claimed, or ineligible).
func (cb *ClaimBoard) HandleClaim(taskID, clientID string) error {
	cb.mu.Lock()

	ct, ok := cb.tasks[taskID]
	if !ok {
		cb.mu.Unlock()
		return fmt.Errorf("task %s not on claim board", taskID)
	}

	// First-come-first-served: check if already claimed
	if ct.ClaimedBy != "" {
		cb.mu.Unlock()
		return fmt.Errorf("task %s already claimed by %s", taskID, ct.ClaimedBy)
	}

	// Verify eligibility
	client := cb.registry.Get(clientID)
	if client == nil {
		cb.mu.Unlock()
		return fmt.Errorf("client %s not eligible for task %s: client not found", clientID, taskID)
	}
	if client.State != reef.ClientConnected {
		cb.mu.Unlock()
		return fmt.Errorf("client %s not eligible for task %s: client not online", clientID, taskID)
	}
	if client.Role != ct.Task.RequiredRole {
		cb.mu.Unlock()
		return fmt.Errorf("client %s not eligible for task %s: role mismatch (need %s, have %s)",
			clientID, taskID, ct.Task.RequiredRole, client.Role)
	}
	if len(ct.Task.RequiredSkills) > 0 && !client.Matches(ct.Task.RequiredRole, ct.Task.RequiredSkills) {
		cb.mu.Unlock()
		return fmt.Errorf("client %s not eligible for task %s: missing required skills",
			clientID, taskID)
	}

	// Claim it
	ct.ClaimedBy = clientID
	ct.Task.AssignedClient = clientID

	// Stop expiry timer
	if ct.ExpiryTimer != nil {
		ct.ExpiryTimer.Stop()
	}

	cb.mu.Unlock()

	// Notify other candidates asynchronously
	go cb.notifyClaimed(taskID, clientID)

	// Dispatch to claiming client
	if err := cb.scheduler.DispatchTask(clientID, ct.Task); err != nil {
		cb.logger.Error("dispatch failed after claim",
			slog.String("task_id", taskID),
			slog.String("client_id", clientID),
			slog.String("error", err.Error()),
		)
		// Re-post to board with a new ID (copy of the task)
		repostTask := *ct.Task
		repostTask.ID = taskID + "-repost"
		repostTask.AssignedClient = ""
		if postErr := cb.Post(&repostTask); postErr != nil {
			cb.logger.Error("failed to re-post after dispatch failure",
				slog.String("original_task_id", taskID),
				slog.String("repost_task_id", repostTask.ID),
				slog.String("error", postErr.Error()),
			)
		}
		return err
	}

	cb.logger.Info("task claimed",
		slog.String("task_id", taskID),
		slog.String("client_id", clientID),
	)

	return nil
}

// notifyClaimed sends task_claimed message to all eligible candidates except the claimer.
func (cb *ClaimBoard) notifyClaimed(taskID, claimerID string) {
	cb.mu.Lock()
	ct, ok := cb.tasks[taskID]
	if !ok {
		cb.mu.Unlock()
		return
	}
	// Snapshot the task info under lock
	task := ct.Task
	cb.mu.Unlock()

	candidates := cb.registry.FindByRole(task.RequiredRole)
	now := time.Now().UnixMilli()
	claimedPayload := reef.TaskClaimedPayload{
		TaskID:    taskID,
		ClaimedBy: claimerID,
		ClaimedAt: now,
	}
	claimedMsg, err := reef.NewMessage(reef.MsgTaskClaimed, taskID, claimedPayload)
	if err != nil {
		cb.logger.Error("failed to build task_claimed message", slog.String("error", err.Error()))
		return
	}

	for _, c := range candidates {
		if c.ID == claimerID {
			continue
		}
		clientID := c.ID
		go func() {
			if err := cb.connManager.SendToClient(clientID, claimedMsg); err != nil {
				cb.logger.Warn("failed to send task_claimed",
					slog.String("client_id", clientID),
					slog.String("task_id", taskID),
					slog.String("error", err.Error()),
				)
			}
		}()
	}
}

// ---------------------------------------------------------------------------
// Cancel — remove task from board
// ---------------------------------------------------------------------------

// Cancel removes a task from the claim board and stops its expiry timer.
func (cb *ClaimBoard) Cancel(taskID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	ct, ok := cb.tasks[taskID]
	if !ok {
		return
	}
	if ct.ExpiryTimer != nil {
		ct.ExpiryTimer.Stop()
	}
	delete(cb.tasks, taskID)
	cb.logger.Info("task cancelled on claim board", slog.String("task_id", taskID))
}

// ---------------------------------------------------------------------------
// expiryTimer — retry then fallback to scheduler
// ---------------------------------------------------------------------------

// startExpiryTimer kicks off a goroutine that waits for the claim timeout.
func (cb *ClaimBoard) startExpiryTimer(taskID string) {
	cb.mu.Lock()
	ct, ok := cb.tasks[taskID]
	if !ok {
		cb.mu.Unlock()
		return
	}
	timeout := cb.config.ClaimTimeout
	cb.mu.Unlock()

	// Use time.AfterFunc for testable timer injection
	ct.ExpiryTimer = time.AfterFunc(timeout, func() {
		cb.expiryTimer(taskID)
	})
}

// expiryTimer handles task expiry: retry or fallback to scheduler.
func (cb *ClaimBoard) expiryTimer(taskID string) {
	cb.mu.Lock()
	ct, ok := cb.tasks[taskID]
	if !ok {
		cb.mu.Unlock()
		return
	}

	// Already claimed in time — nothing to do
	if ct.ClaimedBy != "" {
		cb.mu.Unlock()
		return
	}

	ct.ExpiryRetries++
	retries := ct.ExpiryRetries
	maxRetries := cb.config.MaxRetriesOnExpiry

	if retries < maxRetries {
		// Re-post to board preserving the retry counter
		cb.mu.Unlock()

		cb.logger.Info("claim expired, retrying",
			slog.String("task_id", taskID),
			slog.Int("retries", retries),
		)
		cb.repostFromExpiry(ct)
		return
	}

	// Exhausted retries → fallback to scheduler
	delete(cb.tasks, taskID)
	task := ct.Task
	cb.mu.Unlock()

	cb.logger.Info("claim exhausted, falling back to scheduler",
		slog.String("task_id", taskID),
	)

	if err := cb.scheduler.Submit(task); err != nil {
		cb.logger.Error("scheduler fallback submit failed",
			slog.String("task_id", taskID),
			slog.String("error", err.Error()),
		)
	}
}

// repostFromExpiry re-posts a task to the board from the expiry timer,
// preserving the existing retry count.
func (cb *ClaimBoard) repostFromExpiry(ct *claimableTask) {
	cb.mu.Lock()
	// The task may have been claimed or cancelled between expiry trigger and this call
	existing, ok := cb.tasks[ct.Task.ID]
	if !ok || existing.ClaimedBy != "" {
		cb.mu.Unlock()
		return
	}
	// Preserve retry count
	existing.ExpiryRetries = ct.ExpiryRetries
	existing.PostedAt = time.Now()
	task := existing.Task
	cb.mu.Unlock()

	// Re-notify candidates
	candidates := cb.findEligibleCandidates(task)
	instruction := task.Instruction
	if len(instruction) > 200 {
		instruction = instruction[:200]
	}
	availablePayload := reef.TaskAvailablePayload{
		TaskID:         task.ID,
		RequiredRole:   task.RequiredRole,
		RequiredSkills: task.RequiredSkills,
		Priority:       task.Priority,
		Instruction:    instruction,
		ExpiresAt:      time.Now().Add(cb.config.ClaimTimeout).UnixMilli(),
	}
	availableMsg, err := reef.NewMessage(reef.MsgTaskAvailable, task.ID, availablePayload)
	if err != nil {
		cb.logger.Error("failed to build task_available message", slog.String("error", err.Error()))
	}

	for _, c := range candidates {
		clientID := c.ID
		go func() {
			if err := cb.connManager.SendToClient(clientID, availableMsg); err != nil {
				cb.logger.Warn("failed to send task_available (retry)",
					slog.String("client_id", clientID),
					slog.String("task_id", task.ID),
					slog.String("error", err.Error()),
				)
			}
		}()
	}

	// Restart expiry timer
	cb.startExpiryTimer(task.ID)

	cb.logger.Info("task reposted to claim board (retry)",
		slog.String("task_id", task.ID),
		slog.Int("retries", existing.ExpiryRetries),
		slog.Int("candidates_count", len(candidates)),
	)
}

// ---------------------------------------------------------------------------
// Status / introspection
// ---------------------------------------------------------------------------

// BoardSize returns the current number of tasks on the claim board.
func (cb *ClaimBoard) BoardSize() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return len(cb.tasks)
}

// GetTask returns a task from the board by ID, or nil if not found.
func (cb *ClaimBoard) GetTask(taskID string) *reef.Task {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	ct, ok := cb.tasks[taskID]
	if !ok {
		return nil
	}
	return ct.Task
}

// Stop shuts down the ClaimBoard, stopping all expiry timers.
func (cb *ClaimBoard) Stop() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	for id, ct := range cb.tasks {
		if ct.ExpiryTimer != nil {
			ct.ExpiryTimer.Stop()
		}
		delete(cb.tasks, id)
	}
	select {
	case <-cb.stopCh:
	default:
		close(cb.stopCh)
	}
}
