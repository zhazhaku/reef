// Package server provides result delivery from Reef tasks back to chat channels.
// When a task with ReplyTo context completes/fails, the result is sent
// through the ChannelManager back to the originating chat.

package server

import (
	"fmt"
	"log/slog"

	"github.com/zhazhaku/reef/pkg/reef"
)

// ResultDelivery handles routing task results back to the originating channel.
type ResultDelivery struct {
	logger *slog.Logger
	// channelManager allows sending outbound messages.
	// This is set after construction via SetChannelManager.
	channelManager interface {
		Send(channel, chatID string, content string) error
	}
}

// NewResultDelivery creates a result delivery handler.
func NewResultDelivery(logger *slog.Logger) *ResultDelivery {
	if logger == nil {
		logger = slog.Default()
	}
	return &ResultDelivery{logger: logger}
}

// SetChannelManager sets the channel manager for outbound message delivery.
func (rd *ResultDelivery) SetChannelManager(cm interface {
	Send(channel, chatID string, content string) error
}) {
	rd.channelManager = cm
}

// OnTaskResult is the scheduler callback invoked on task completion/failure.
// It checks ReplyTo context and delivers results back to the source channel.
func (rd *ResultDelivery) OnTaskResult(task *reef.Task, result *reef.TaskResult, taskErr *reef.TaskError) {
	if task.ReplyTo == nil || task.ReplyTo.IsZero() {
		return
	}
	if rd.channelManager == nil {
		rd.logger.Warn("result delivery: no channel manager set",
			slog.String("task_id", task.ID))
		return
	}

	rt := task.ReplyTo
	msg := rd.buildResultMessage(task, result, taskErr)
	if msg == "" {
		return
	}

	if err := rd.channelManager.Send(rt.Channel, rt.ChatID, msg); err != nil {
		rd.logger.Error("result delivery: send failed",
			slog.String("task_id", task.ID),
			slog.String("channel", rt.Channel),
			slog.String("chat_id", rt.ChatID),
			slog.String("error", err.Error()))
	}
}

// buildResultMessage constructs a user-friendly result message.
func (rd *ResultDelivery) buildResultMessage(task *reef.Task, result *reef.TaskResult, taskErr *reef.TaskError) string {
	switch task.Status {
	case reef.TaskCompleted:
		if result == nil {
			return fmt.Sprintf("✅ Task completed: %s", task.Instruction)
		}
		if result.Text != "" {
			return fmt.Sprintf("✅ Result for \"%s\":\n\n%s", task.Instruction, result.Text)
		}
		return fmt.Sprintf("✅ Task completed: %s", task.Instruction)

	case reef.TaskFailed:
		if taskErr != nil {
			return fmt.Sprintf("❌ Task failed: %s\nError: %s", task.Instruction, taskErr.Message)
		}
		return fmt.Sprintf("❌ Task failed: %s", task.Instruction)

	case reef.TaskCancelled:
		return fmt.Sprintf("⏹️ Task cancelled: %s", task.Instruction)

	default:
		return ""
	}
}
