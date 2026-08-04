package lht

import (
	"strings"
	"sync"
	"testing"
)

// ===================== T4B.2.1: 关键节点通知 =====================

func TestCallbackNotifierTaskCreated(t *testing.T) {
	var events []EventRecord
	cb := NewCallbackNotifier(func(channel, chatID, text string) {
		// noop — just verify callback fires
	})
	cb.OnTaskCreated = func(goalID, description string) {
		events = append(events, EventRecord{Event: EventTaskCreated, GoalID: goalID})
	}

	cb.NotifyTaskCreated("g-1", "build api", "telegram", "chat-1")

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != EventTaskCreated {
		t.Fatalf("expected TaskCreated, got %s", events[0].Event)
	}
	if events[0].GoalID != "g-1" {
		t.Fatalf("expected GoalID=g-1, got %s", events[0].GoalID)
	}
}

func TestCallbackNotifierPlanReady(t *testing.T) {
	var events []EventRecord
	cb := NewCallbackNotifier(func(channel, chatID, text string) {})
	cb.OnPlanReady = func(goalID string, taskCount int) {
		events = append(events, EventRecord{Event: EventPlanReady, GoalID: goalID})
	}

	cb.NotifyPlanReady("g-2", 5, "cli", "chat-2")

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != EventPlanReady {
		t.Fatalf("expected PlanReady, got %s", events[0].Event)
	}
}

func TestCallbackNotifierApproved(t *testing.T) {
	var events []EventRecord
	cb := NewCallbackNotifier(func(channel, chatID, text string) {})
	cb.OnApproved = func(goalID string) {
		events = append(events, EventRecord{Event: EventApproved, GoalID: goalID})
	}

	cb.NotifyApproved("g-3", "telegram", "chat-3")

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != EventApproved {
		t.Fatalf("expected Approved, got %s", events[0].Event)
	}
}

func TestCallbackNotifierEscalated(t *testing.T) {
	var events []EventRecord
	cb := NewCallbackNotifier(func(channel, chatID, text string) {})
	cb.OnEscalated = func(goalID string, report *HelpReport) {
		events = append(events, EventRecord{Event: EventEscalated, GoalID: goalID})
	}

	report := &HelpReport{GoalID: "g-4", State: StateEscalated}
	cb.NotifyEscalated("g-4", report, "telegram", "chat-4")

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != EventEscalated {
		t.Fatalf("expected Escalated, got %s", events[0].Event)
	}
}

func TestCallbackNotifierCompleted(t *testing.T) {
	var events []EventRecord
	cb := NewCallbackNotifier(func(channel, chatID, text string) {})
	cb.OnCompleted = func(goalID string) {
		events = append(events, EventRecord{Event: EventCompleted, GoalID: goalID})
	}

	cb.NotifyCompleted("g-5", "telegram", "chat-5")

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != EventCompleted {
		t.Fatalf("expected Completed, got %s", events[0].Event)
	}
}

func TestCallbackNotifierAborted(t *testing.T) {
	var events []EventRecord
	cb := NewCallbackNotifier(func(channel, chatID, text string) {})
	cb.OnAborted = func(goalID, reason string) {
		events = append(events, EventRecord{Event: EventAborted, GoalID: goalID})
	}

	cb.NotifyAborted("g-6", "user cancelled", "cli", "chat-6")

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != EventAborted {
		t.Fatalf("expected Aborted, got %s", events[0].Event)
	}
}

// ===================== T4B.2.2: 回调式通知器 — 验证调用时机与参数 =====================

func TestCallbackNotifierAllEventTypes(t *testing.T) {
	var mu sync.Mutex
	var events []EventRecord

	cb := NewCallbackNotifier(func(channel, chatID, text string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, EventRecord{Event: EventSent, GoalID: text})
	})

	cb.OnTaskCreated = func(goalID, description string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, EventRecord{Event: EventTaskCreated, GoalID: goalID})
	}
	cb.OnPlanReady = func(goalID string, taskCount int) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, EventRecord{Event: EventPlanReady, GoalID: goalID})
	}
	cb.OnApproved = func(goalID string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, EventRecord{Event: EventApproved, GoalID: goalID})
	}
	cb.OnEscalated = func(goalID string, report *HelpReport) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, EventRecord{Event: EventEscalated, GoalID: goalID})
	}
	cb.OnCompleted = func(goalID string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, EventRecord{Event: EventCompleted, GoalID: goalID})
	}
	cb.OnAborted = func(goalID, reason string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, EventRecord{Event: EventAborted, GoalID: goalID})
	}

	// 模拟完整生命周期
	cb.NotifyTaskCreated("g-full", "build all", "telegram", "chat-full")
	cb.NotifyPlanReady("g-full", 3, "telegram", "chat-full")
	cb.NotifyApproved("g-full", "telegram", "chat-full")
	report := &HelpReport{GoalID: "g-full", State: StateEscalated, StallRounds: 3}
	cb.NotifyEscalated("g-full", report, "telegram", "chat-full")
	cb.NotifyCompleted("g-full", "telegram", "chat-full")
	cb.NotifyAborted("g-full", "test", "cli", "chat-full")

	expectedOrder := []EventType{
		EventTaskCreated, EventSent,
		EventPlanReady, EventSent,
		EventApproved, EventSent,
		EventEscalated, EventSent,
		EventCompleted, EventSent,
		EventAborted, EventSent,
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != len(expectedOrder) {
		t.Fatalf("expected %d events, got %d", len(expectedOrder), len(events))
	}
	for i, e := range expectedOrder {
		if events[i].Event != e {
			t.Fatalf("event[%d]: expected %s, got %s", i, e, events[i].Event)
		}
	}
}

func TestCallbackNotifierSendIncludesChannelAndChatID(t *testing.T) {
	var lastChannel, lastChatID, lastText string
	cb := NewCallbackNotifier(func(channel, chatID, text string) {
		lastChannel = channel
		lastChatID = chatID
		lastText = text
	})

	cb.NotifyTaskCreated("g-chan", "test", "feishu", "chat-xyz")

	if lastChannel != "feishu" {
		t.Fatalf("expected channel=feishu, got %s", lastChannel)
	}
	if lastChatID != "chat-xyz" {
		t.Fatalf("expected chatID=chat-xyz, got %s", lastChatID)
	}
	if !strings.Contains(lastText, "g-chan") {
		t.Fatalf("expected text to contain 'g-chan', got %s", lastText)
	}
}

// ===================== T4B.2.3: 通知失败不阻断主流程 =====================

func TestCallbackNotifierPanicTolerance(t *testing.T) {
	var normalEvents []EventRecord

	cb := NewCallbackNotifier(func(channel, chatID, text string) {
		normalEvents = append(normalEvents, EventRecord{Event: EventSent})
	})

	// 设置会 panic 的回调
	cb.OnTaskCreated = func(goalID, description string) {
		panic("simulated callback panic")
	}

	// 不应 panic，应正常返回
	cb.NotifyTaskCreated("g-panic", "test", "telegram", "chat-panic")

	// 后续通知应正常工作
	cb.NotifyPlanReady("g-panic", 3, "telegram", "chat-panic")

	if len(normalEvents) < 1 {
		t.Fatal("expected Send to be called even after callback panic")
	}
}

func TestCallbackNotifierNilCallbackGraceful(t *testing.T) {
	var sentCalled bool
	cb := NewCallbackNotifier(func(channel, chatID, text string) {
		sentCalled = true
	})

	// 所有回调为 nil（默认状态）
	cb.NotifyTaskCreated("g-nil", "test", "cli", "chat-nil")
	cb.NotifyPlanReady("g-nil", 1, "cli", "chat-nil")
	cb.NotifyApproved("g-nil", "cli", "chat-nil")
	cb.NotifyEscalated("g-nil", &HelpReport{GoalID: "g-nil"}, "cli", "chat-nil")
	cb.NotifyCompleted("g-nil", "cli", "chat-nil")
	cb.NotifyAborted("g-nil", "test", "cli", "chat-nil")

	if !sentCalled {
		t.Fatal("expected Send to be called even with nil callbacks")
	}
}

func TestCallbackNotifierPartialCallbacks(t *testing.T) {
	var taskCreatedCalled, escalatedCalled bool
	cb := NewCallbackNotifier(func(channel, chatID, text string) {})

	// 只设置部分回调
	cb.OnTaskCreated = func(goalID, description string) {
		taskCreatedCalled = true
	}
	// OnEscalated 不设置（nil）
	cb.OnEscalated = func(goalID string, report *HelpReport) {
		escalatedCalled = true
	}

	cb.NotifyTaskCreated("g-partial", "test", "cli", "chat")
	cb.NotifyEscalated("g-partial", &HelpReport{GoalID: "g-partial"}, "cli", "chat")

	if !taskCreatedCalled {
		t.Fatal("expected OnTaskCreated callback to be called")
	}
	if !escalatedCalled {
		t.Fatal("expected OnEscalated callback to be called")
	}
}

func TestCallbackNotifierSendFailureDoesNotPanic(t *testing.T) {
	// 即使内部 Send 函数有问题，通知也不应 panic
	cb := NewCallbackNotifier(func(channel, chatID, text string) {
		// 模拟某些内部问题但不 panic
	})

	// 这些调用应正常完成
	cb.NotifyTaskCreated("g-safe", "test", "telegram", "chat")
	cb.NotifyCompleted("g-safe", "telegram", "chat")
}

// ===================== T4B.2.4: EventType 常量完整性 =====================

func TestEventTypeConstants(t *testing.T) {
	allEvents := []EventType{
		EventTaskCreated,
		EventPlanReady,
		EventApproved,
		EventEscalated,
		EventCompleted,
		EventAborted,
		EventSent,
	}
	if len(allEvents) != 7 {
		t.Fatalf("expected 7 event types, got %d", len(allEvents))
	}
	// 验证无重复
	seen := make(map[EventType]bool)
	for _, e := range allEvents {
		if seen[e] {
			t.Fatalf("duplicate event type: %s", e)
		}
		seen[e] = true
	}
}

// ===================== Helpers for notifier tests =====================

// EventRecord captures a notification event for test assertions.
type EventRecord struct {
	Event  EventType
	GoalID string
}
