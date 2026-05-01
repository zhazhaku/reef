package client

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// mockEventStore implements evolutionEventStore for testing.
type mockEventStore struct {
	mu     sync.Mutex
	events []*evolution.EvolutionEvent
	// Error injection
	insertErr error
}

func newMockEventStore() *mockEventStore {
	return &mockEventStore{}
}

func (m *mockEventStore) InsertEvolutionEvent(event *evolution.EvolutionEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insertErr != nil {
		return m.insertErr
	}
	m.events = append(m.events, event)
	return nil
}

func (m *mockEventStore) GetRecentEvents(clientID string, limit int) ([]*evolution.EvolutionEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*evolution.EvolutionEvent
	for _, e := range m.events {
		if e.ClientID == clientID {
			result = append(result, e)
			if len(result) >= limit {
				break
			}
		}
	}
	if result == nil {
		result = []*evolution.EvolutionEvent{}
	}
	return result, nil
}

func (m *mockEventStore) CountEventsByType(clientID string, eventType string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, e := range m.events {
		if e.ClientID == clientID && string(e.EventType) == eventType {
			count++
		}
	}
	return count, nil
}

func (m *mockEventStore) eventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

// ---------------------------------------------------------------------------
// Recorder Record tests
// ---------------------------------------------------------------------------

func TestRecorderRecord_OneEvent(t *testing.T) {
	store := newMockEventStore()
	rec := NewRecorder(store, RecorderConfig{BatchTriggerCount: 5, TimeTriggerMinutes: 30}, nil)

	event := &evolution.EvolutionEvent{
		ID:         "evt-1",
		TaskID:     "task-1",
		ClientID:   "client-1",
		EventType:  evolution.EventSuccessPattern,
		Signal:     "success",
		Importance: 0.7,
		CreatedAt:  time.Now().UTC(),
	}

	err := rec.Record(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.eventCount() != 1 {
		t.Errorf("expected 1 event in store, got %d", store.eventCount())
	}
}

func TestRecorderRecord_BatchTrigger(t *testing.T) {
	store := newMockEventStore()
	triggerFired := false
	var mu sync.Mutex

	rec := NewRecorder(store, RecorderConfig{BatchTriggerCount: 3, TimeTriggerMinutes: 30}, nil)
	rec.SetOnTrigger(func() {
		mu.Lock()
		triggerFired = true
		mu.Unlock()
	})
	// Manually set batch to 2 so the 3rd event triggers
	rec.trigger.setBatchCount(2)

	for i := 0; i < 5; i++ {
		event := &evolution.EvolutionEvent{
			ID:         fmt.Sprintf("evt-%d", i),
			TaskID:     fmt.Sprintf("task-%d", i),
			ClientID:   "client-1",
			EventType:  evolution.EventSuccessPattern,
			Signal:     "test",
			Importance: 0.5,
			CreatedAt:  time.Now().UTC(),
		}
		err := rec.Record(event)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	time.Sleep(50 * time.Millisecond) // wait for goroutine

	mu.Lock()
	fired := triggerFired
	mu.Unlock()

	if !fired {
		t.Error("expected trigger to fire after batch threshold")
	}
	if store.eventCount() != 5 {
		t.Errorf("expected 5 events, got %d", store.eventCount())
	}
}

func TestRecorderRecord_NilEventReturnsError(t *testing.T) {
	store := newMockEventStore()
	rec := NewRecorder(store, RecorderConfig{}, nil)

	err := rec.Record(nil)
	if err == nil {
		t.Fatal("expected error for nil event")
	}
}

func TestRecorderRecord_EmptyIDReturnsError(t *testing.T) {
	store := newMockEventStore()
	rec := NewRecorder(store, RecorderConfig{}, nil)

	event := &evolution.EvolutionEvent{
		ID:        "",
		TaskID:    "task-1",
		ClientID:  "client-1",
		CreatedAt: time.Now().UTC(),
	}

	err := rec.Record(event)
	if err == nil {
		t.Fatal("expected error for empty event ID")
	}
}

func TestRecorderRecord_InsertErrorReturnsError(t *testing.T) {
	store := newMockEventStore()
	store.insertErr = fmt.Errorf("db error")
	rec := NewRecorder(store, RecorderConfig{}, nil)

	event := &evolution.EvolutionEvent{
		ID:         "evt-1",
		TaskID:     "task-1",
		ClientID:   "client-1",
		EventType:  evolution.EventSuccessPattern,
		Signal:     "test",
		Importance: 0.5,
		CreatedAt:  time.Now().UTC(),
	}

	err := rec.Record(event)
	if err == nil {
		t.Fatal("expected error from insert failure")
	}
	if store.eventCount() != 0 {
		t.Errorf("expected 0 events after insert error, got %d", store.eventCount())
	}
}

// ---------------------------------------------------------------------------
// RecorderTrigger tests
// ---------------------------------------------------------------------------

func TestRecorderTrigger_BatchFiresOnThreshold(t *testing.T) {
	store := newMockEventStore()
	fired := false
	var mu sync.Mutex

	tr := &RecorderTrigger{
		batchThreshold:   5,
		timeThreshold:    30 * time.Minute,
		newFailureTrigger: false,
		lastTriggerTime:  time.Now().UTC(),
		logger:           slog.Default(),
		store:            store,
		onTrigger: func() {
			mu.Lock()
			fired = true
			mu.Unlock()
		},
	}
	tr.setBatchCount(4)

	event := &evolution.EvolutionEvent{
		ID:         "evt-1",
		TaskID:     "task-1",
		ClientID:   "client-1",
		EventType:  evolution.EventSuccessPattern,
		CreatedAt:  time.Now().UTC(),
	}

	tr.afterRecord(event, 5)

	mu.Lock()
	result := fired
	mu.Unlock()

	if !result {
		t.Error("expected trigger to fire when pending >= batch threshold")
	}
}

func TestRecorderTrigger_ImmediateNewFailure(t *testing.T) {
	store := newMockEventStore()
	// Pre-insert the failure event so CountEventsByType returns 1 (this is the first)
	store.InsertEvolutionEvent(&evolution.EvolutionEvent{
		ID:         "evt-existing",
		TaskID:     "task-fail",
		ClientID:   "client-1",
		EventType:  evolution.EventFailurePattern,
		CreatedAt:  time.Now().UTC(),
	})

	fired := false
	var mu sync.Mutex

	tr := &RecorderTrigger{
		batchThreshold:    5,
		timeThreshold:     30 * time.Minute,
		newFailureTrigger: true,
		lastTriggerTime:   time.Now().UTC(),
		logger:            slog.Default(),
		store:             store,
		onTrigger: func() {
			mu.Lock()
			fired = true
			mu.Unlock()
		},
	}

	event := &evolution.EvolutionEvent{
		ID:         "evt-new-fail",
		TaskID:     "task-fail",
		ClientID:   "client-1",
		EventType:  evolution.EventFailurePattern,
		CreatedAt:  time.Now().UTC(),
	}

	tr.afterRecord(event, 1)

	mu.Lock()
	result := fired
	mu.Unlock()

	if !result {
		t.Error("expected immediate trigger on new failure")
	}
}

func TestRecorderTrigger_TimeInterval(t *testing.T) {
	store := newMockEventStore()
	fired := false
	var mu sync.Mutex

	tr := &RecorderTrigger{
		batchThreshold:    5,
		timeThreshold:     30 * time.Minute,
		newFailureTrigger: false,
		lastTriggerTime:   time.Now().Add(-31 * time.Minute), // simulate 31 min since last trigger
		logger:            slog.Default(),
		store:             store,
		onTrigger: func() {
			mu.Lock()
			fired = true
			mu.Unlock()
		},
	}

	event := &evolution.EvolutionEvent{
		ID:         "evt-time",
		TaskID:     "task-t",
		ClientID:   "client-1",
		EventType:  evolution.EventSuccessPattern,
		CreatedAt:  time.Now().UTC(),
	}

	tr.afterRecord(event, 1)

	mu.Lock()
	result := fired
	mu.Unlock()

	if !result {
		t.Error("expected time-based trigger after 31 min")
	}
}

func TestRecorderTrigger_TimeNotExpiredDoesNotFire(t *testing.T) {
	store := newMockEventStore()
	fired := false
	var mu sync.Mutex

	tr := &RecorderTrigger{
		batchThreshold:    5,
		timeThreshold:     30 * time.Minute,
		newFailureTrigger: false,
		lastTriggerTime:   time.Now().UTC(), // just now
		logger:            slog.Default(),
		store:             store,
		onTrigger: func() {
			mu.Lock()
			fired = true
			mu.Unlock()
		},
	}

	event := &evolution.EvolutionEvent{
		ID:         "evt-time",
		TaskID:     "task-t",
		ClientID:   "client-1",
		EventType:  evolution.EventSuccessPattern,
		CreatedAt:  time.Now().UTC(),
	}

	tr.afterRecord(event, 1)

	mu.Lock()
	result := fired
	mu.Unlock()

	if result {
		t.Error("expected no time trigger when interval not expired")
	}
}

func TestRecorderTrigger_NoPendingNoTimeTrigger(t *testing.T) {
	store := newMockEventStore()
	fired := false
	var mu sync.Mutex

	tr := &RecorderTrigger{
		batchThreshold:    5,
		timeThreshold:     30 * time.Minute,
		newFailureTrigger: false,
		lastTriggerTime:   time.Now().Add(-31 * time.Minute),
		logger:            slog.Default(),
		store:             store,
		onTrigger: func() {
			mu.Lock()
			fired = true
			mu.Unlock()
		},
	}

	event := &evolution.EvolutionEvent{
		ID:         "evt-zero",
		TaskID:     "task-z",
		ClientID:   "client-1",
		EventType:  evolution.EventSuccessPattern,
		CreatedAt:  time.Now().UTC(),
	}

	tr.afterRecord(event, 0) // pending=0

	mu.Lock()
	result := fired
	mu.Unlock()

	if result {
		t.Error("expected no time trigger when pending count is 0")
	}
}

func TestRecorderTrigger_MultipleTriggersOnSameCall(t *testing.T) {
	store := newMockEventStore()
	fireCount := 0
	var mu sync.Mutex

	tr := &RecorderTrigger{
		batchThreshold:    2,
		timeThreshold:     30 * time.Minute,
		newFailureTrigger: true,
		lastTriggerTime:   time.Now().Add(-31 * time.Minute),
		logger:            slog.Default(),
		store:             store,
		onTrigger: func() {
			mu.Lock()
			fireCount++
			mu.Unlock()
		},
	}

	// Pre-insert one failure event so CountEventsByType returns 1
	store.InsertEvolutionEvent(&evolution.EvolutionEvent{
		ID:         "evt-existing-fail",
		TaskID:     "task-multi",
		ClientID:   "client-1",
		EventType:  evolution.EventFailurePattern,
		CreatedAt:  time.Now().UTC(),
	})

	event := &evolution.EvolutionEvent{
		ID:         "evt-multi",
		TaskID:     "task-multi",
		ClientID:   "client-1",
		EventType:  evolution.EventFailurePattern,
		CreatedAt:  time.Now().UTC(),
	}

	// This should trigger: immediate_new_failure + batch_threshold(=2) + time_interval
	tr.afterRecord(event, 3)

	mu.Lock()
	count := fireCount
	mu.Unlock()

	if count < 2 {
		t.Errorf("expected at least 2 triggers (failure + batch), got %d", count)
	}
}

func TestRecorderTrigger_NilOnTriggerNoPanic(t *testing.T) {
	store := newMockEventStore()
	tr := &RecorderTrigger{
		batchThreshold:    5,
		timeThreshold:     30 * time.Minute,
		newFailureTrigger: true,
		lastTriggerTime:   time.Now().UTC(),
		logger:            slog.Default(),
		store:             store,
		onTrigger:         nil,
	}

	event := &evolution.EvolutionEvent{
		ID:         "evt-nilcb",
		TaskID:     "task-nilcb",
		ClientID:   "client-1",
		EventType:  evolution.EventFailurePattern,
		CreatedAt:  time.Now().UTC(),
	}

	// Should not panic
	tr.afterRecord(event, 10)
}

func TestRecorderTrigger_Reset(t *testing.T) {
	tr := &RecorderTrigger{
		batchThreshold: 5,
		lastTriggerTime: time.Now().Add(-1 * time.Hour),
		logger: slog.Default(),
	}
	tr.setBatchCount(10)
	tr.Reset()

	tr.mu.Lock()
	if tr.batchCount != 0 {
		t.Errorf("expected batchCount=0 after reset, got %d", tr.batchCount)
	}
	if time.Since(tr.lastTriggerTime) > time.Second {
		t.Error("expected lastTriggerTime to be recent after reset")
	}
	tr.mu.Unlock()
}
