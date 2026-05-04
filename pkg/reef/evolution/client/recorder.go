package client

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// evolutionEventStore is the minimal interface the EvolutionRecorder needs
// from the persistence layer. Defined locally to avoid coupling to concrete store types.
type evolutionEventStore interface {
	InsertEvolutionEvent(event *evolution.EvolutionEvent) error
	GetRecentEvents(clientID string, limit int) ([]*evolution.EvolutionEvent, error)
	CountEventsByType(clientID string, eventType string) (int, error)
}

// RecorderConfig configures the EvolutionRecorder.
type RecorderConfig struct {
	// BatchTriggerCount is the number of pending events that triggers evolution.
	// Defaults to 5.
	BatchTriggerCount int

	// TimeTriggerMinutes is the minimum interval between time-based triggers.
	// Defaults to 30.
	TimeTriggerMinutes int

	// NewFailureTrigger enables immediate trigger on a task's first failure event.
	// Defaults to true.
	NewFailureTrigger bool
}

// EvolutionRecorder persists EvolutionEvents to SQLite and triggers evolution
// via 3 mechanisms: batch count, time interval, and immediate new-failure.
type EvolutionRecorder struct {
	store     evolutionEventStore
	trigger   *RecorderTrigger
	config    RecorderConfig
	logger    *slog.Logger
	mu        sync.Mutex
	onTrigger func() // callback: triggers LocalGeneEvolver.Evolve()
}

// NewRecorder creates a new EvolutionRecorder.
func NewRecorder(store evolutionEventStore, config RecorderConfig, logger *slog.Logger) *EvolutionRecorder {
	if config.BatchTriggerCount <= 0 {
		config.BatchTriggerCount = 5
	}
	if config.TimeTriggerMinutes <= 0 {
		config.TimeTriggerMinutes = 30
	}
	if logger == nil {
		logger = slog.Default()
	}

	r := &EvolutionRecorder{
		store:  store,
		config: config,
		logger: logger,
	}

	r.trigger = &RecorderTrigger{
		batchThreshold:     config.BatchTriggerCount,
		timeThreshold:      time.Duration(config.TimeTriggerMinutes) * time.Minute,
		newFailureTrigger:  config.NewFailureTrigger,
		logger:             logger,
		onTrigger:          func() { r.fireEvolve() },
		store:              store,
		lastTriggerTime:    time.Now().UTC(), // initialize to prevent immediate time trigger
	}

	return r
}

// SetOnTrigger sets the callback that fires when any trigger condition is met.
// This should point to LocalGeneEvolver.Evolve().
func (r *EvolutionRecorder) SetOnTrigger(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onTrigger = fn
}

// Record validates and persists an EvolutionEvent, then checks trigger conditions.
// Concurrent calls are mutex-safe.
func (r *EvolutionRecorder) Record(event *evolution.EvolutionEvent) error {
	if event == nil {
		return fmt.Errorf("recorder: event cannot be nil")
	}
	if event.ID == "" {
		return fmt.Errorf("recorder: event ID cannot be empty")
	}

	// Persist to SQLite
	if err := r.store.InsertEvolutionEvent(event); err != nil {
		r.logger.Error("failed to insert evolution event",
			slog.String("event_id", event.ID),
			slog.String("error", err.Error()))
		return fmt.Errorf("recorder: insert event: %w", err)
	}

	r.logger.Debug("recorded evolution event",
		slog.String("event_id", event.ID),
		slog.String("event_type", string(event.EventType)),
		slog.String("task_id", event.TaskID))

	// Check trigger conditions. The trigger has its own mutex for thread safety.
	pendingCount, err := r.getPendingCount(event.ClientID)
	if err != nil {
		r.logger.Warn("failed to get pending count for trigger check",
			slog.String("client_id", event.ClientID),
			slog.String("error", err.Error()))
		return nil // best-effort: still recorded
	}

	r.trigger.afterRecord(event, pendingCount)
	return nil
}

// getPendingCount returns the number of unprocessed events for a client.
func (r *EvolutionRecorder) getPendingCount(clientID string) (int, error) {
	// Count all unprocessed events regardless of type
	events, err := r.store.GetRecentEvents(clientID, 1000)
	if err != nil {
		return 0, err
	}
	return len(events), nil
}

// fireEvolve is called by the trigger when conditions are met.
func (r *EvolutionRecorder) fireEvolve() {
	r.mu.Lock()
	fn := r.onTrigger
	r.mu.Unlock()

	if fn == nil {
		r.logger.Warn("evolution trigger fired but onTrigger callback is nil")
		return
	}
	go fn()
}

// ---------------------------------------------------------------------------
// RecorderTrigger — 3 trigger mechanisms
// ---------------------------------------------------------------------------

// RecorderTrigger implements batch count, time interval, and immediate new-failure triggers.
type RecorderTrigger struct {
	batchCount        int
	batchThreshold    int
	lastTriggerTime   time.Time
	timeThreshold     time.Duration
	newFailureTrigger bool
	onTrigger         func()
	store             evolutionEventStore
	mu                sync.Mutex
	logger            *slog.Logger
}

// afterRecord is called after a new event is recorded. It checks all trigger
// conditions and fires the appropriate triggers. Multiple triggers may fire
// in a single call (e.g., both batch and time).
func (t *RecorderTrigger) afterRecord(event *evolution.EvolutionEvent, pendingCount int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.batchCount++

	// 1. Immediate new-failure trigger
	if t.newFailureTrigger && event.EventType == evolution.EventFailurePattern {
		if t.isFirstFailureForTask(event) {
			t.fire("immediate_new_failure", pendingCount)
		}
	}

	// 2. Batch count trigger
	if pendingCount >= t.batchThreshold {
		t.fire("batch_threshold", pendingCount)
	}

	// 3. Time interval trigger
	if time.Since(t.lastTriggerTime) >= t.timeThreshold && pendingCount > 0 {
		t.fire("time_interval", pendingCount)
	}
}

// isFirstFailureForTask checks if this is the first failure event for the given task.
func (t *RecorderTrigger) isFirstFailureForTask(event *evolution.EvolutionEvent) bool {
	if t.store == nil {
		return false
	}
	count, err := t.store.CountEventsByType(event.ClientID, string(evolution.EventFailurePattern))
	if err != nil {
		t.logger.Warn("failed to count failure events for trigger check",
			slog.String("error", err.Error()))
		return false
	}
	// The event hasn't been counted yet (CountEventsByType counts from DB, and Record
	// inserts before calling afterRecord), so count == 1 means this is the first.
	return count == 1
}

// fire executes the trigger callback.
func (t *RecorderTrigger) fire(reason string, pendingCount int) {
	t.logger.Info("evolution trigger",
		slog.String("reason", reason),
		slog.Int("pending", pendingCount))
	t.lastTriggerTime = time.Now().UTC()
	if t.onTrigger != nil {
		t.onTrigger()
	}
}

// Reset resets the batch counter and last trigger time (for testing).
func (t *RecorderTrigger) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.batchCount = 0
	t.lastTriggerTime = time.Now().UTC()
}

// setLastTriggerTime sets the last trigger time for testing time-based triggers.
func (t *RecorderTrigger) setLastTriggerTime(tm time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastTriggerTime = tm
}

// setBatchCount sets the batch counter for testing.
func (t *RecorderTrigger) setBatchCount(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.batchCount = n
}
