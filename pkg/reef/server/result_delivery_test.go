package server

import (
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/zhazhaku/reef/pkg/reef"
)

// mockChannelManager records sent messages for test assertions.
type mockChannelManager struct {
	mu    sync.Mutex
	calls []mockSend
}
type mockSend struct {
	channel, chatID, content string
}

func (m *mockChannelManager) Send(channel, chatID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockSend{channel, chatID, content})
	return nil
}

func TestResultDelivery_CompletedTask(t *testing.T) {
	cm := &mockChannelManager{}
	rd := NewResultDelivery(slog.New(slog.NewTextHandler(io.Discard, nil)))
	rd.SetChannelManager(cm)

	task := &reef.Task{
		ID:          "t1",
		Status:      reef.TaskCompleted,
		Instruction: "summarise meeting notes",
		ReplyTo:     &reef.ReplyToContext{Channel: "feishu", ChatID: "oc_test", UserID: "ou_123"},
	}
	result := &reef.TaskResult{Text: "Meeting summary: discussed Q2 goals."}

	rd.OnTaskResult(task, result, nil)

	if len(cm.calls) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(cm.calls))
	}
	c := cm.calls[0]
	if c.channel != "feishu" || c.chatID != "oc_test" {
		t.Errorf("sent to %s/%s, expected feishu/oc_test", c.channel, c.chatID)
	}
	if c.content == "" {
		t.Error("content should not be empty")
	}
}

func TestResultDelivery_FailedTask(t *testing.T) {
	cm := &mockChannelManager{}
	rd := NewResultDelivery(slog.New(slog.NewTextHandler(io.Discard, nil)))
	rd.SetChannelManager(cm)

	task := &reef.Task{
		ID:          "t2",
		Status:      reef.TaskFailed,
		Instruction: "process data",
		ReplyTo:     &reef.ReplyToContext{Channel: "telegram", ChatID: "chat_456"},
	}
	taskErr := &reef.TaskError{Type: "timeout", Message: "timed out after 30s"}

	rd.OnTaskResult(task, nil, taskErr)

	if len(cm.calls) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(cm.calls))
	}
	c := cm.calls[0]
	if c.channel != "telegram" || c.chatID != "chat_456" {
		t.Errorf("sent to %s/%s", c.channel, c.chatID)
	}
	if c.content == "" || len(c.content) < 10 {
		t.Errorf("content too short: %q", c.content)
	}
}

func TestResultDelivery_NoReplyTo(t *testing.T) {
	cm := &mockChannelManager{}
	rd := NewResultDelivery(slog.New(slog.NewTextHandler(io.Discard, nil)))
	rd.SetChannelManager(cm)

	task := &reef.Task{ID: "t3", Status: reef.TaskCompleted, Instruction: "test"}
	result := &reef.TaskResult{Text: "done"}

	rd.OnTaskResult(task, result, nil)

	if len(cm.calls) != 0 {
		t.Errorf("expected no messages when no ReplyTo, got %d", len(cm.calls))
	}
}

func TestResultDelivery_NoChannelManager(t *testing.T) {
	rd := NewResultDelivery(slog.New(slog.NewTextHandler(io.Discard, nil)))
	// not setting channel manager — should not panic

	task := &reef.Task{
		ID:      "t4",
		Status:  reef.TaskCompleted,
		ReplyTo: &reef.ReplyToContext{Channel: "feishu", ChatID: "oc_test"},
	}
	result := &reef.TaskResult{Text: "ok"}

	// Should not panic
	rd.OnTaskResult(task, result, nil)
}
