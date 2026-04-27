package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackNotifier sends alerts via Slack Incoming Webhook.
type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewSlackNotifier creates a new SlackNotifier.
func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *SlackNotifier) Name() string { return "slack" }

// slackMessage is the Slack Block Kit payload.
type slackMessage struct {
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string       `json:"type"`
	Text     *slackText   `json:"text,omitempty"`
	Fields   []slackText  `json:"fields,omitempty"`
	Elements []slackText  `json:"elements,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (n *SlackNotifier) Notify(ctx context.Context, alert Alert) error {
	color := "#ff9900" // orange for warning
	if alert.Event == "task_escalated" {
		color = "#ff0000" // red for escalated
	}

	msg := slackMessage{
		Blocks: []slackBlock{
			{
				Type: "header",
				Text: &slackText{
					Type: "plain_text",
					Text: fmt.Sprintf("🚨 Reef Alert: %s", alert.Event),
				},
			},
			{
				Type: "section",
				Fields: []slackText{
					{Type: "mrkdwn", Text: fmt.Sprintf("*Task ID:*\n%s", alert.TaskID)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Status:*\n%s", alert.Status)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Role:*\n%s", alert.RequiredRole)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Escalations:*\n%d/%d", alert.EscalationCount, alert.MaxEscalations)},
				},
			},
			{
				Type: "section",
				Text: &slackText{
					Type: "mrkdwn",
					Text: fmt.Sprintf("*Instruction:*\n%s", alert.Instruction),
				},
			},
			{
				Type: "context",
				Elements: []slackText{
					{Type: "mrkdwn", Text: fmt.Sprintf("Color: %s | Time: %s", color, alert.Timestamp.Format(time.RFC3339))},
				},
			},
		},
	}

	if alert.Error != nil {
		msg.Blocks = append(msg.Blocks, slackBlock{
			Type: "section",
			Text: &slackText{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*Error:*\n```%s: %s```", alert.Error.Type, alert.Error.Message),
			},
		})
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send slack message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}
	return nil
}
