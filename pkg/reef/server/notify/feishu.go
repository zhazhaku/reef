package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FeishuNotifier sends alerts via Feishu (Lark) webhook.
type FeishuNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewFeishuNotifier creates a new FeishuNotifier.
func NewFeishuNotifier(webhookURL string) *FeishuNotifier {
	return &FeishuNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *FeishuNotifier) Name() string { return "feishu" }

// feishuCard is the Feishu interactive card message format.
type feishuCard struct {
	MsgType string      `json:"msg_type"`
	Card    feishuBody  `json:"card"`
}

type feishuBody struct {
	Header   feishuHeader   `json:"header"`
	Elements []feishuElement `json:"elements"`
}

type feishuHeader struct {
	Title    feishuText `json:"title"`
	Template string     `json:"template"`
}

type feishuText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type feishuElement struct {
	Tag  string     `json:"tag"`
	Text *feishuText `json:"text,omitempty"`
}

func (n *FeishuNotifier) Notify(ctx context.Context, alert Alert) error {
	color := "orange"
	if alert.Event == "task_escalated" {
		color = "red"
	}

	msg := feishuCard{
		MsgType: "interactive",
		Card: feishuBody{
			Header: feishuHeader{
				Title:    feishuText{Tag: "plain_text", Content: fmt.Sprintf("🚨 Reef Alert: %s", alert.Event)},
				Template: color,
			},
			Elements: []feishuElement{
				{Tag: "div", Text: &feishuText{Tag: "lark_md", Content: fmt.Sprintf("**Task ID:** %s", alert.TaskID)}},
				{Tag: "div", Text: &feishuText{Tag: "lark_md", Content: fmt.Sprintf("**Status:** %s", alert.Status)}},
				{Tag: "div", Text: &feishuText{Tag: "lark_md", Content: fmt.Sprintf("**Role:** %s", alert.RequiredRole)}},
				{Tag: "div", Text: &feishuText{Tag: "lark_md", Content: fmt.Sprintf("**Instruction:** %s", alert.Instruction)}},
				{Tag: "div", Text: &feishuText{Tag: "lark_md", Content: fmt.Sprintf("**Escalations:** %d/%d", alert.EscalationCount, alert.MaxEscalations)}},
			},
		},
	}

	if alert.Error != nil {
		msg.Card.Elements = append(msg.Card.Elements, feishuElement{
			Tag:  "div",
			Text: &feishuText{Tag: "lark_md", Content: fmt.Sprintf("**Error:** %s: %s", alert.Error.Type, alert.Error.Message)},
		})
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal feishu message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send feishu message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("feishu returned status %d", resp.StatusCode)
	}
	return nil
}
