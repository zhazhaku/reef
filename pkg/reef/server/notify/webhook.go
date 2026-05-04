package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookNotifier sends alerts via HTTP POST to webhook URLs.
type WebhookNotifier struct {
	urls     []string
	client   *http.Client
}

// NewWebhookNotifier creates a new WebhookNotifier.
func NewWebhookNotifier(urls []string) *WebhookNotifier {
	return &WebhookNotifier{
		urls: urls,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *WebhookNotifier) Name() string { return "webhook" }

func (n *WebhookNotifier) Notify(ctx context.Context, alert Alert) error {
	payload, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("marshal alert: %w", err)
	}

	var lastErr error
	for _, url := range n.urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := n.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("webhook %s returned %d", url, resp.StatusCode)
		}
	}
	return lastErr
}
