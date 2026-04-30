package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

// WebhookPayload is sent to configured webhook URLs when a task is escalated.
type WebhookPayload struct {
	Event           string            `json:"event"`
	TaskID          string            `json:"task_id"`
	Status          string            `json:"status"`
	Instruction     string            `json:"instruction"`
	RequiredRole    string            `json:"required_role"`
	Error           *reef.TaskError   `json:"error,omitempty"`
	AttemptHistory  []reef.AttemptRecord `json:"attempt_history,omitempty"`
	EscalationCount int               `json:"escalation_count"`
	MaxEscalations  int               `json:"max_escalations"`
	Timestamp       int64             `json:"timestamp"`
}

// sendWebhookAlert sends a POST request to all configured webhook URLs.
// Each URL is called concurrently. Failures are logged but do not affect
// task state or block the scheduler.
func sendWebhookAlert(logger *slog.Logger, urls []string, payload WebhookPayload) {
	if len(urls) == 0 {
		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error("webhook: failed to marshal payload", slog.String("error", err.Error()))
		return
	}

	for _, u := range urls {
		go func(url string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if err != nil {
				logger.Warn("webhook: failed to create request",
					slog.String("url", url), slog.String("error", err.Error()))
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				logger.Warn("webhook: request failed",
					slog.String("url", url), slog.String("error", err.Error()))
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 400 {
				logger.Warn("webhook: server returned error",
					slog.String("url", url), slog.Int("status", resp.StatusCode))
			} else {
				logger.Info("webhook: delivered",
					slog.String("url", url), slog.Int("status", resp.StatusCode))
			}
		}(u)
	}
}
