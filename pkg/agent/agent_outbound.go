// Reef - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/logger"
	"github.com/zhazhaku/reef/pkg/providers"
	"github.com/zhazhaku/reef/pkg/tools"
	"github.com/zhazhaku/reef/pkg/utils"
)

func (al *AgentLoop) maybePublishError(ctx context.Context, channel, chatID, sessionKey string, err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	al.PublishResponseIfNeeded(ctx, channel, chatID, sessionKey, fmt.Sprintf("Error processing message: %v", err), "")
	return true
}

func (al *AgentLoop) publishResponseOrError(
	ctx context.Context,
	channel, chatID, sessionKey string,
	response string,
	thought string,
	err error,
) {
	if err != nil {
		if !al.maybePublishError(ctx, channel, chatID, sessionKey, err) {
			return
		}
		response = ""
		thought = ""
	}
	al.PublishResponseIfNeeded(ctx, channel, chatID, sessionKey, response, thought)
}

func (al *AgentLoop) PublishResponseIfNeeded(ctx context.Context, channel, chatID, sessionKey, response, thought string) {
	if response == "" {
		return
	}

	alreadySentToSameChat := false
	defaultAgent := al.GetRegistry().GetDefaultAgent()
	if defaultAgent != nil {
		if tool, ok := defaultAgent.Tools.Get("message"); ok {
			if mt, ok := tool.(*tools.MessageTool); ok {
				alreadySentToSameChat = mt.HasSentTo(sessionKey, channel, chatID)
			}
		}
	}

	if alreadySentToSameChat {
		logger.DebugCF(
			"agent",
			"Skipped outbound (message tool already sent to same chat)",
			map[string]any{"channel": channel, "chat_id": chatID},
		)
		return
	}

	msg := bus.OutboundMessage{
		Context: bus.NewOutboundContext(channel, chatID, ""),
		Content: response,
		Thought: thought,
	}
	if sessionKey != "" {
		msg.ContextUsage = computeContextUsage(al.agentForSession(sessionKey), sessionKey)
	}
	al.bus.PublishOutbound(ctx, msg)
	logger.InfoCF("agent", "Published outbound response",
		map[string]any{
			"channel":     channel,
			"chat_id":     chatID,
			"content_len": len(response),
		})
}

func (al *AgentLoop) targetReasoningChannelID(channelName string) (chatID string) {
	if al.channelManager == nil {
		return ""
	}
	if ch, ok := al.channelManager.GetChannel(channelName); ok {
		return ch.ReasoningChannelID()
	}
	return ""
}

func (al *AgentLoop) publishPicoReasoning(ctx context.Context, reasoningContent, chatID string) {
	if reasoningContent == "" || chatID == "" {
		return
	}

	if ctx.Err() != nil {
		return
	}

	pubCtx, pubCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pubCancel()

	if err := al.bus.PublishOutbound(pubCtx, bus.OutboundMessage{
		Context: bus.InboundContext{
			Channel: "pico",
			ChatID:  chatID,
			Raw: map[string]string{
				metadataKeyMessageKind: messageKindThought,
			},
		},
		Content: reasoningContent,
	}); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
			errors.Is(err, bus.ErrBusClosed) {
			logger.DebugCF("agent", "Pico reasoning publish skipped (timeout/cancel)", map[string]any{
				"channel": "pico",
				"error":   err.Error(),
			})
		} else {
			logger.WarnCF("agent", "Failed to publish pico reasoning (best-effort)", map[string]any{
				"channel": "pico",
				"error":   err.Error(),
			})
		}
	}
}

func (al *AgentLoop) publishPicoToolCallInterim(
	ctx context.Context,
	ts *turnState,
	reasoningContent string,
	content string,
	toolCalls []providers.ToolCall,
) {
	if ts == nil || ts.chatID == "" || al == nil || al.bus == nil {
		return
	}

	if strings.TrimSpace(reasoningContent) != "" {
		pubCtx, pubCancel := context.WithTimeout(ctx, 3*time.Second)
		err := al.bus.PublishOutbound(
			pubCtx,
			outboundMessageForTurnWithKind(ts, reasoningContent, messageKindThought),
		)
		pubCancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, bus.ErrBusClosed) {
			logger.WarnCF("agent", "Failed to publish pico reasoning", map[string]any{
				"channel": ts.channel,
				"chat_id": ts.chatID,
				"error":   err.Error(),
			})
		}
	}

	if !ts.opts.AllowInterimPicoPublish {
		return
	}

	visibleToolCalls := utils.BuildVisibleToolCalls(
		toolCalls,
		al.cfg.Agents.Defaults.GetToolFeedbackMaxArgsLength(),
	)
	duplicateToolCallContent := len(visibleToolCalls) > 0 &&
		utils.ToolCallExplanationDuplicatesContent(content, toolCalls)

	if strings.TrimSpace(content) != "" && !duplicateToolCallContent {
		pubCtx, pubCancel := context.WithTimeout(ctx, 3*time.Second)
		err := al.bus.PublishOutbound(pubCtx, outboundMessageForTurn(ts, content))
		pubCancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, bus.ErrBusClosed) {
			logger.WarnCF("agent", "Failed to publish pico interim assistant content", map[string]any{
				"channel": ts.channel,
				"chat_id": ts.chatID,
				"error":   err.Error(),
			})
		}
	}

	if len(visibleToolCalls) == 0 {
		return
	}

	rawToolCalls, err := json.Marshal(visibleToolCalls)
	if err != nil {
		logger.WarnCF("agent", "Failed to serialize pico tool calls", map[string]any{
			"channel": ts.channel,
			"chat_id": ts.chatID,
			"error":   err.Error(),
		})
		return
	}

	msg := outboundMessageForTurnWithKind(ts, "", messageKindToolCalls)
	if msg.Context.Raw == nil {
		msg.Context.Raw = map[string]string{}
	}
	msg.Context.Raw[metadataKeyToolCalls] = string(rawToolCalls)

	pubCtx, pubCancel := context.WithTimeout(ctx, 3*time.Second)
	err = al.bus.PublishOutbound(pubCtx, msg)
	pubCancel()
	if err != nil && !errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, bus.ErrBusClosed) {
		logger.WarnCF("agent", "Failed to publish pico tool calls", map[string]any{
			"channel": ts.channel,
			"chat_id": ts.chatID,
			"error":   err.Error(),
		})
	}
}

func (al *AgentLoop) handleReasoning(
	ctx context.Context,
	reasoningContent, channelName, channelID string,
) {
	if reasoningContent == "" || channelName == "" || channelID == "" {
		return
	}

	// For feishu channel: publish as thinking_card so the channel can
	// progressively update a dedicated thinking card (PatchMessage).
	if channelName == "feishu" {
		al.publishThinkingCardUpdate(ctx, reasoningContent, channelID)
		return
	}

	// Check context cancellation before attempting to publish.
	if ctx.Err() != nil {
		return
	}

	pubCtx, pubCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pubCancel()

	if err := al.bus.PublishOutbound(pubCtx, bus.OutboundMessage{
		Context: bus.NewOutboundContext(channelName, channelID, ""),
		Content: reasoningContent,
	}); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
			errors.Is(err, bus.ErrBusClosed) {
			logger.DebugCF("agent", "Reasoning publish skipped (timeout/cancel)", map[string]any{
				"channel": channelName,
				"error":   err.Error(),
			})
		} else {
			logger.WarnCF("agent", "Failed to publish reasoning (best-effort)", map[string]any{
				"channel": channelName,
				"error":   err.Error(),
			})
		}
	}
}

// publishThinkingCardUpdate sends a thinking-card update message for the
// feishu channel.  The channel maintains a per-chat thinking card that is
// created on first message and patched on subsequent messages.
func (al *AgentLoop) publishThinkingCardUpdate(ctx context.Context, reasoningContent, chatID string) {
	if ctx.Err() != nil {
		return
	}

	pubCtx, pubCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pubCancel()

	if err := al.bus.PublishOutbound(pubCtx, bus.OutboundMessage{
		Context: bus.InboundContext{
			Channel: "feishu",
			ChatID:  chatID,
			Raw: map[string]string{
				metadataKeyMessageKind: messageKindThinkingCard,
			},
		},
		Content: reasoningContent,
	}); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) &&
			!errors.Is(err, bus.ErrBusClosed) {
			logger.WarnCF("agent", "Failed to publish thinking card update", map[string]any{
				"error": err.Error(),
			})
		}
	}
}

// publishThinkingFinal sends a thinking_final message to the feishu channel
// so it can replace the progressive thinking card with the final answer.
func (al *AgentLoop) publishThinkingFinal(ctx context.Context, chatID, reasoningSummary string) {
	pubCtx, pubCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pubCancel()

	if err := al.bus.PublishOutbound(pubCtx, bus.OutboundMessage{
		Context: bus.InboundContext{
			Channel: "feishu",
			ChatID:  chatID,
			Raw: map[string]string{
				metadataKeyMessageKind: messageKindThinkingFinal,
			},
		},
		Content: reasoningSummary,
	}); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) &&
			!errors.Is(err, bus.ErrBusClosed) {
			logger.WarnCF("agent", "Failed to publish thinking final", map[string]any{
				"error": err.Error(),
			})
		}
	}
}
