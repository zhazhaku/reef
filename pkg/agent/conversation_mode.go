// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent provides per-conversation mode switching (Chat ↔ Hermes)
// with isolated seahorse session keys.

package agent

import "strings"

// ConversationMode defines the per-conversation operational mode.
// Unlike HermesMode (process-level role), ConversationMode is scoped
// to a single seahorse conversation and can be switched at runtime via
// chat commands ("切换聊天模式", "切换Hermes模式").
type ConversationMode string

const (
	// ModeChat is standard agent chat — all tools available, LLM
	// decides freely. This is the default for every conversation.
	ModeChat ConversationMode = "chat"

	// ModeHermes is the Hermes 6-phase collaborative workflow — the
	// Server acts as a workflow coordinator, HermesGuard filters tool
	// calls, and user messages are routed through the phase state machine.
	ModeHermes ConversationMode = "hermes"
)

// String returns the human-readable mode name.
func (m ConversationMode) String() string {
	switch m {
	case ModeChat:
		return "聊天模式 (Chat)"
	case ModeHermes:
		return "Hermes 模式"
	default:
		return string(m)
	}
}

// IsHermes returns true if this is the Hermes workflow mode.
func (m ConversationMode) IsHermes() bool {
	return m == ModeHermes
}

// SessionKey returns the seahorse session key for this conversation+mode
// combination. Different modes use different session keys so their
// contexts are completely isolated — a message sent in Chat mode is
// invisible when the conversation switches to Hermes mode.
//
// Format: "conv:{convID}:{mode}"  e.g. "conv:42:chat" / "conv:42:hermes"
func SessionKey(convID string, mode ConversationMode) string {
	switch mode {
	case ModeChat:
		return "conv:" + convID + ":chat"
	case ModeHermes:
		return "conv:" + convID + ":hermes"
	default:
		return "conv:" + convID + ":chat"
	}
}

// ModeCommand represents a parsed mode-switch instruction.
type ModeCommand int

const (
	// ModeCmdNone is returned when the message is not a mode command.
	ModeCmdNone ModeCommand = iota
	// ModeCmdSwitchToChat switches the conversation to Chat mode.
	ModeCmdSwitchToChat
	// ModeCmdSwitchToHermes switches the conversation to Hermes mode.
	ModeCmdSwitchToHermes
	// ModeCmdShowMode queries the current mode.
	ModeCmdShowMode
)

// detectModeCommand checks if a user message is a mode-switch or mode-query
// command. Returns ModeCmdNone if the message is a normal chat message.
func detectModeCommand(text string) ModeCommand {
	lower := strings.TrimSpace(strings.ToLower(text))

	// Exact matches first
	switch lower {
	case "chat mode", "switch to chat",
		"切换聊天模式", "切换到聊天模式",
		"聊天模式", "进入聊天模式":
		return ModeCmdSwitchToChat

	case "hermes mode", "switch to hermes",
		"切换hermes模式", "切换到hermes模式",
		"hermes模式", "进入hermes模式",
		"切换hermes", "进入hermes":
		return ModeCmdSwitchToHermes

	case "status", "mode", "查看模式", "当前模式", "模式":
		return ModeCmdShowMode
	}

	// Prefix match for commands embedded in longer messages
	if strings.HasPrefix(lower, "chat ") || strings.HasPrefix(lower, "/chat") {
		return ModeCmdSwitchToChat
	}
	if strings.HasPrefix(lower, "hermes ") || strings.HasPrefix(lower, "/hermes") {
		return ModeCmdSwitchToHermes
	}

	return ModeCmdNone
}

// HandleModeSwitch processes a mode-switch command and returns the
// response to send to the user. Returns false if the message was not
// a mode command (should be processed normally).
func HandleModeSwitch(cmd ModeCommand, currentMode ConversationMode) (string, bool) {
	switch cmd {
	case ModeCmdSwitchToChat:
		if currentMode == ModeChat {
			return "当前已在聊天模式。", true
		}
		return "已切换到聊天模式。所有工具可用，自由对话。", true

	case ModeCmdSwitchToHermes:
		if currentMode == ModeHermes {
			return "当前已在 Hermes 模式。", true
		}
		return "已切换到 Hermes 模式。当前无进行中的工作流，请发送需求开始协作。", true

	case ModeCmdShowMode:
		return "当前模式: " + currentMode.String(), true

	default:
		return "", false
	}
}
