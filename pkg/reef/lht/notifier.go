package lht

import (
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
)

// ===================== EventType =====================

// EventType enumerates the key lifecycle events that trigger notifications.
// Extended from the stub: keeps legacy constants and adds new callback-oriented ones.
type EventType string

// Event type constants — both legacy (from stub) and new callback events.
const (
	// Legacy event types (kept for compatibility).
	EventGroundingStarted EventType = "grounding_started"
	EventPlanningDone     EventType = "planning_done"
	EventApprovalNeeded   EventType = "approval_needed"
	EventExecutingStarted EventType = "executing_started"
	EventSubtaskComplete  EventType = "subtask_complete"
	EventEvalComplete     EventType = "eval_complete"
	EventReviewComplete   EventType = "review_complete"
	EventFinalApproval    EventType = "final_approval"
	EventPaused           EventType = "paused"
	EventBudgetWarning    EventType = "budget_warning"
	EventStallWarning     EventType = "stall_warning"

	// New callback-oriented event types (W4B).
	EventTaskCreated EventType = "task_created"
	EventPlanReady   EventType = "plan_ready"
	EventApproved    EventType = "approved"
	EventEscalated   EventType = "escalated"
	EventCompleted   EventType = "completed"
	EventAborted     EventType = "aborted"
	EventSent        EventType = "sent" // internal: raw message sent
)

// ===================== NotifierWrapper (legacy) =====================

// NewNotifier creates a new NotifierWrapper backed by the given implementation.
func NewNotifier(impl Notifier) *NotifierWrapper {
	return &NotifierWrapper{impl: impl}
}

// NotifierWrapper wraps a Notifier with event-type support.
// This is the legacy wrapper retained for backward compatibility.
type NotifierWrapper struct {
	mu   sync.Mutex
	impl Notifier
}

// Send sends a notification via the underlying Notifier.
func (nw *NotifierWrapper) Send(channel, chatID, text string) {
	if nw.impl != nil {
		nw.impl.Send(channel, chatID, text)
	}
}

// Notify sends a typed event notification using the underlying Notifier.
func (nw *NotifierWrapper) Notify(event EventType, goalID, channel, chatID string) {
	nw.mu.Lock()
	defer nw.mu.Unlock()
	text := string(event) + ": " + goalID
	if nw.impl != nil {
		nw.impl.Send(channel, chatID, text)
	}
}

// ===================== CallbackNotifier =====================

// CallbackNotifier wraps the basic Notifier interface with event-driven callbacks.
// Each key lifecycle event can have an optional callback that fires before the
// underlying message is sent via the send function.
//
// All callbacks are optional — nil callbacks are silently skipped.
// If a callback panics, the panic is recovered and the notification continues.
// This type coexists with NotifierWrapper for different use cases:
//   - NotifierWrapper: simple typed events via Notify()
//   - CallbackNotifier: rich callbacks with structured data (goal, report, etc.)
type CallbackNotifier struct {
	send func(channel, chatID, text string)

	// Callbacks for key lifecycle events.
	OnTaskCreated func(goalID, description string)
	OnPlanReady   func(goalID string, taskCount int)
	OnApproved    func(goalID string)
	OnEscalated   func(goalID string, report *HelpReport)
	OnCompleted   func(goalID string)
	OnAborted     func(goalID, reason string)
}

// NewCallbackNotifier creates a CallbackNotifier with the given send function.
// The send function is the underlying message transport (e.g., feishu, telegram, cli).
// Pass nil to disable actual sending (for testing).
func NewCallbackNotifier(send func(channel, chatID, text string)) *CallbackNotifier {
	if send == nil {
		send = func(channel, chatID, text string) {}
	}
	return &CallbackNotifier{send: send}
}

// safeCall executes fn and recovers from any panic, ensuring the notifier
// continues to operate even if a callback misbehaves.
func safeCall(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			// Log the panic for debugging but do not propagate.
			_ = debug.Stack()
		}
	}()
	if fn != nil {
		fn()
	}
}

// ===================== Notify Methods (CallbackNotifier) =====================

// NotifyTaskCreated fires when a new LHT goal is created and enters GROUNDING.
func (n *CallbackNotifier) NotifyTaskCreated(goalID, description, channel, chatID string) {
	if n.OnTaskCreated != nil {
		safeCall(func() { n.OnTaskCreated(goalID, description) })
	}
	text := fmt.Sprintf("[LHT] 新任务 %s 已创建: %s\n状态: GROUNDING — 正在对齐目标...", goalID, description)
	n.send(channel, chatID, text)
}

// NotifyPlanReady fires when a plan has been generated and is awaiting user approval.
func (n *CallbackNotifier) NotifyPlanReady(goalID string, taskCount int, channel, chatID string) {
	if n.OnPlanReady != nil {
		safeCall(func() { n.OnPlanReady(goalID, taskCount) })
	}
	text := fmt.Sprintf("[LHT] 任务 %s 计划已就绪 (%d 个子任务)\n请审批: /lht approve %s 或 /lht reject %s <意见>", goalID, taskCount, goalID, goalID)
	n.send(channel, chatID, text)
}

// NotifyApproved fires when the user approves a plan and execution begins.
func (n *CallbackNotifier) NotifyApproved(goalID, channel, chatID string) {
	if n.OnApproved != nil {
		safeCall(func() { n.OnApproved(goalID) })
	}
	text := fmt.Sprintf("[LHT] 任务 %s 计划已批准，开始执行...", goalID)
	n.send(channel, chatID, text)
}

// NotifyEscalated fires when a task enters ESCALATED state and requires human intervention.
func (n *CallbackNotifier) NotifyEscalated(goalID string, report *HelpReport, channel, chatID string) {
	if n.OnEscalated != nil {
		safeCall(func() { n.OnEscalated(goalID, report) })
	}
	text := fmt.Sprintf("[ESCALATED] 任务 %s 需要人工介入\n", goalID)
	if report != nil {
		text += report.String()
	} else {
		text += "求助报告不可用，请检查任务状态"
	}
	n.send(channel, chatID, text)
}

// NotifyCompleted fires when a task successfully reaches COMPLETED state.
func (n *CallbackNotifier) NotifyCompleted(goalID, channel, chatID string) {
	if n.OnCompleted != nil {
		safeCall(func() { n.OnCompleted(goalID) })
	}
	text := fmt.Sprintf("[LHT] 任务 %s 已完成 ✓", goalID)
	n.send(channel, chatID, text)
}

// NotifyAborted fires when a task is terminated (user stop or system abort).
func (n *CallbackNotifier) NotifyAborted(goalID, reason, channel, chatID string) {
	if n.OnAborted != nil {
		safeCall(func() { n.OnAborted(goalID, reason) })
	}
	text := fmt.Sprintf("[LHT] 任务 %s 已终止", goalID)
	if reason != "" {
		text += ": " + reason
	}
	n.send(channel, chatID, text)
}

// ===================== Convenience =====================

// NotifyEscalatedVia sends an escalation notification using the engine's Notifier interface
// and a pre-built HelpReport. This is a bridge between the callback notifier and the
// engine's existing Notifier.
func NotifyEscalatedVia(notifier Notifier, goalID string, report *HelpReport, channel, chatID string) {
	if notifier == nil {
		return
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[ESCALATED] 任务 %s 需要人工介入\n", goalID))
	if report != nil {
		b.WriteString(report.String())
	} else {
		b.WriteString("求助报告不可用，请检查任务状态")
	}
	notifier.Send(channel, chatID, b.String())
}
