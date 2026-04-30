package swarm

import (
	"encoding/json"
	"testing"

	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/config"
)

func TestNewSwarmChannel(t *testing.T) {
	cfg := &config.SwarmSettings{
		ServerURL: "ws://localhost:8080",
		Role:      "coder",
		Skills:    []string{"github", "write_file"},
	}
	bc := &config.Channel{Enabled: true, Type: config.ChannelSwarm}
	msgBus := bus.NewMessageBus()
	defer msgBus.Close()

	ch, err := NewSwarmChannel(bc, cfg, msgBus)
	if err != nil {
		t.Fatalf("NewSwarmChannel: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
	if ch.Name() != "swarm" {
		t.Fatalf("expected name swarm, got %s", ch.Name())
	}
}

func TestSwarmSettingsDecode(t *testing.T) {
	raw := []byte(`{"enabled":true,"server_url":"ws://test:8080","role":"analyst","skills":["web_fetch"],"capacity":5}`)
	var cfg config.SwarmSettings
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.ServerURL != "ws://test:8080" {
		t.Fatalf("unexpected server_url: %s", cfg.ServerURL)
	}
	if cfg.Role != "analyst" {
		t.Fatalf("unexpected role: %s", cfg.Role)
	}
	if len(cfg.Skills) != 1 || cfg.Skills[0] != "web_fetch" {
		t.Fatalf("unexpected skills: %v", cfg.Skills)
	}
	if cfg.Capacity != 5 {
		t.Fatalf("unexpected capacity: %d", cfg.Capacity)
	}
}
