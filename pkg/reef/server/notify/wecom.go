package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WeComNotifier sends alerts via WeCom (企业微信) webhook.
type WeComNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewWeComNotifier creates a new WeComNotifier.
func NewWeComNotifier(webhookURL string) *WeComNotifier {
	return &WeComNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *WeComNotifier) Name() string { return "wecom" }

// wecomMessage is the WeCom webhook message format.
type wecomMessage struct {
	MsgType  string       `json:"msgtype"`
	Markdown wecomMarkdown `json:"markdown"`
}

type wecomMarkdown struct {
	Content string `json:"content"`
}

func (n *WeComNotifier) Notify(ctx context.Context, alert Alert) error {
	var content string
	content += fmt.Sprintf("## 🚨 Reef Alert: %s\n", alert.Event)
	content += fmt.Sprintf("> **Task ID:** %s\n", alert.TaskID)
	content += fmt.Sprintf("> **Status:** <font color=\"warning\">%s</font>\n", alert.Status)
	content += fmt.Sprintf("> **Role:** %s\n", alert.RequiredRole)
	content += fmt.Sprintf("> **Instruction:** %s\n", alert.Instruction)
	content += fmt.Sprintf("> **Escalations:** %d/%d\n", alert.EscalationCount, alert.MaxEscalations)

	if alert.Error != nil {
		content += fmt.Sprintf("> **Error:** %s: %s\n", alert.Error.Type, alert.Error.Message)
	}

	content += fmt.Sprintf("> **Time:** %s\n", alert.Timestamp.Format("2006-01-02 15:04:05"))

	msg := wecomMessage{
		MsgType:  "markdown",
		Markdown: wecomMarkdown{Content: content},
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal wecom message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send wecom message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wecom returned status %d", resp.StatusCode)
	}
	return nil
}
