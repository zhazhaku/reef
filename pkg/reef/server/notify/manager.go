package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	evolutionsrv "github.com/zhazhaku/reef/pkg/reef/evolution/server"
)

// Manager fans out alerts to multiple Notifiers.
type Manager struct {
	notifiers []Notifier
	logger    *slog.Logger
}

// NewManager creates a new NotificationManager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{logger: logger}
}

// Add registers a notifier.
func (m *Manager) Add(n Notifier) {
	m.notifiers = append(m.notifiers, n)
}

// NotifyAll sends an alert to all registered notifiers concurrently.
// Failures in one notifier do not affect others.
func (m *Manager) NotifyAll(ctx context.Context, alert Alert) {
	if len(m.notifiers) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, n := range m.notifiers {
		wg.Add(1)
		go func(notifier Notifier) {
			defer wg.Done()
			if err := notifier.Notify(ctx, alert); err != nil {
				m.logger.Warn("notification failed",
					slog.String("notifier", notifier.Name()),
					slog.String("task_id", alert.TaskID),
					slog.String("error", err.Error()))
			}
		}(n)
	}
	wg.Wait()
}

// Count returns the number of registered notifiers.
func (m *Manager) Count() int {
	return len(m.notifiers)
}

// NotifyEvolutionDraft sends an evolution skill draft notification
// to all registered notifiers. It routes the notification through
// the existing notification channels with appropriate formatting.
//
// If no notification channels are configured, this is a no-op
// (logs at INFO level).
//
// If all notification channels fail, all errors are logged and
// the first error is returned.
func (m *Manager) NotifyEvolutionDraft(n evolutionsrv.Notification) error {
	if len(m.notifiers) == 0 {
		m.logger.Info("no notification channels configured, skipping evolution draft notification",
			slog.String("type", n.Type),
			slog.String("draft_id", n.DraftID),
			slog.String("role", n.Role))
		return nil
	}

	// Convert to Alert format for existing notifiers.
	alert := Alert{
		Event:           fmt.Sprintf("evolution.%s", n.Type),
		TaskID:          n.DraftID,
		Status:          n.Type,
		Instruction:     "",
		RequiredRole:    n.Role,
		Error:           nil,
		AttemptHistory:  nil,
		EscalationCount: 0,
		MaxEscalations:  0,
		Timestamp:       n.Timestamp,
	}

	// Since Alert doesn't have a title/body field, we use the
	// Instruction field to carry the formatted message for
	// evolution notifications.
	subject := fmt.Sprintf("[Reef] SKILL.md Draft Ready: %s", n.Title)
	alert.Instruction = fmt.Sprintf("%s\n%s\nDraft ID: %s\nRole: %s", subject, n.Body, n.DraftID, n.Role)

	var firstErr error
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, notifier := range m.notifiers {
		wg.Add(1)
		go func(notif Notifier) {
			defer wg.Done()
			if err := notif.Notify(context.Background(), alert); err != nil {
				m.logger.Warn("evolution notification failed",
					slog.String("notifier", notif.Name()),
					slog.String("type", alert.Event),
					slog.String("draft_id", n.DraftID),
					slog.String("error", err.Error()))
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(notifier)
	}
	wg.Wait()

	if firstErr != nil {
		// Check if all failed.
		mu.Lock()
		allFailed := firstErr != nil
		mu.Unlock()
		if allFailed {
			return fmt.Errorf("all notification channels failed: %w", firstErr)
		}
	}

	return nil
}

// =========================================================================
// EvolutionNotifierAdapter — adapts *Manager to evolutionsrv.Notifier
// =========================================================================

// Ensure managerAdapter implements evolutionsrv.Notifier.
var _ evolutionsrv.Notifier = (*managerAdapter)(nil)

// managerAdapter wraps a *Manager to implement evolutionsrv.Notifier.
type managerAdapter struct {
	mgr *Manager
}

// NewEvolutionNotifier creates an evolutionsrv.Notifier from a *Manager.
// This adapter allows the SkillMerger to send evolution-specific
// notifications through the existing notification infrastructure.
func NewEvolutionNotifier(mgr *Manager) evolutionsrv.Notifier {
	return &managerAdapter{mgr: mgr}
}

// NotifyAdmin implements evolutionsrv.Notifier by delegating to
// Manager.NotifyEvolutionDraft.
func (a *managerAdapter) NotifyAdmin(n evolutionsrv.Notification) error {
	return a.mgr.NotifyEvolutionDraft(n)
}
