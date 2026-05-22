// Reef - Distributed multi-agent swarm orchestration system

package agent

import "testing"

func TestDetectModeCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected ModeCommand
	}{
		// Switch to chat
		{"切换聊天模式", ModeCmdSwitchToChat},
		{"切换到聊天模式", ModeCmdSwitchToChat},
		{"聊天模式", ModeCmdSwitchToChat},
		{"chat mode", ModeCmdSwitchToChat},
		{"switch to chat", ModeCmdSwitchToChat},

		// Switch to Hermes
		{"切换Hermes模式", ModeCmdSwitchToHermes},
		{"切换到hermes模式", ModeCmdSwitchToHermes},
		{"切换hermes", ModeCmdSwitchToHermes},
		{"hermes mode", ModeCmdSwitchToHermes},
		{"switch to hermes", ModeCmdSwitchToHermes},

		// Show mode
		{"查看模式", ModeCmdShowMode},
		{"status", ModeCmdShowMode},
		{"mode", ModeCmdShowMode},

		// Not a mode command
		{"你好", ModeCmdNone},
		{"帮我查天气", ModeCmdNone},
		{"设计一个系统", ModeCmdNone},
		{"", ModeCmdNone},
		{"切换", ModeCmdNone},           // ambiguous
		{"hermes设计", ModeCmdNone},      // starts with "hermes " but has extra content
	}

	for _, tt := range tests {
		got := detectModeCommand(tt.input)
		if got != tt.expected {
			t.Errorf("detectModeCommand(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestSessionKey(t *testing.T) {
	tests := []struct {
		convID string
		mode   ConversationMode
		want   string
	}{
		{"42", ModeChat, "conv:42:chat"},
		{"42", ModeHermes, "conv:42:hermes"},
		{"oc_abc123", ModeChat, "conv:oc_abc123:chat"},
		{"", ModeChat, "conv::chat"},
	}

	for _, tt := range tests {
		got := SessionKey(tt.convID, tt.mode)
		if got != tt.want {
			t.Errorf("SessionKey(%q, %v) = %q, want %q", tt.convID, tt.mode, got, tt.want)
		}
	}
}

func TestConversationModeString(t *testing.T) {
	if ModeChat.String() != "聊天模式 (Chat)" {
		t.Errorf("ModeChat.String() = %q", ModeChat.String())
	}
	if ModeHermes.String() != "Hermes 模式" {
		t.Errorf("ModeHermes.String() = %q", ModeHermes.String())
	}
}

func TestConversationModeIsHermes(t *testing.T) {
	if ModeChat.IsHermes() {
		t.Error("ModeChat.IsHermes() should be false")
	}
	if !ModeHermes.IsHermes() {
		t.Error("ModeHermes.IsHermes() should be true")
	}
}
