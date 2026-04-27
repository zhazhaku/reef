package notify

import (
	"context"
	"log/slog"
	"sync"
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
