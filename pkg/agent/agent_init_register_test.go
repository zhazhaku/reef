package agent

import (
	"testing"

	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/config"
)

// TestRegisterSharedTools_SpawnSubagentMatrix verifies that spawn / subagent /
// spawn_status register INDEPENDENTLY based on their own config flag, after
// the fix for the regression introduced by commit 329e68e0 (refactor: Agent
// Looper phase2) which required `subagent` to be enabled for `spawn` to
// register, and nested subagent tool registration inside `if spawnEnabled`.
//
// Hidden bug #5: "spawn=off, subagent=on" used to silently drop subagent.
func TestRegisterSharedTools_SpawnSubagentMatrix(t *testing.T) {
	cases := []struct {
		name          string
		spawn         bool
		subagent      bool
		spawnStatus   bool
		wantSpawn     bool
		wantSubagent  bool
		wantSpawnStat bool
	}{
		{
			name:          "all on (legacy default)",
			spawn:         true, subagent: true, spawnStatus: true,
			wantSpawn: true, wantSubagent: true, wantSpawnStat: true,
		},
		{
			name:          "spawn only (no subagent) — was BROKEN before fix",
			spawn:         true, subagent: false, spawnStatus: false,
			wantSpawn: true, wantSubagent: false, wantSpawnStat: false,
		},
		{
			name:          "subagent only (no spawn) — was BROKEN before fix (hidden #5)",
			spawn:         false, subagent: true, spawnStatus: false,
			wantSpawn: false, wantSubagent: true, wantSpawnStat: false,
		},
		{
			name:          "spawn_status only — must be independent",
			spawn:         false, subagent: false, spawnStatus: true,
			wantSpawn: false, wantSubagent: false, wantSpawnStat: true,
		},
		{
			name:          "spawn + spawn_status, no subagent — was BROKEN before fix",
			spawn:         true, subagent: false, spawnStatus: true,
			wantSpawn: true, wantSubagent: false, wantSpawnStat: true,
		},
		{
			name:          "all off",
			spawn:         false, subagent: false, spawnStatus: false,
			wantSpawn: false, wantSubagent: false, wantSpawnStat: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Agents.Defaults.Workspace = t.TempDir()
			cfg.Tools.Spawn.Enabled = tc.spawn
			cfg.Tools.Subagent.Enabled = tc.subagent
			cfg.Tools.SpawnStatus.Enabled = tc.spawnStatus

			al := NewAgentLoop(cfg, bus.NewMessageBus(), &mockProvider{})
			agent := al.registry.GetDefaultAgent()
			if agent == nil {
				t.Fatal("expected default agent")
			}

			_, hasSpawn := agent.Tools.Get("spawn")
			_, hasSubagent := agent.Tools.Get("subagent")
			_, hasSpawnStatus := agent.Tools.Get("spawn_status")

			if hasSpawn != tc.wantSpawn {
				t.Errorf("spawn registered=%v, want %v", hasSpawn, tc.wantSpawn)
			}
			if hasSubagent != tc.wantSubagent {
				t.Errorf("subagent registered=%v, want %v", hasSubagent, tc.wantSubagent)
			}
			if hasSpawnStatus != tc.wantSpawnStat {
				t.Errorf("spawn_status registered=%v, want %v",
					hasSpawnStatus, tc.wantSpawnStat)
			}
		})
	}
}

// TestRegisterSharedTools_SpawnToolHasSpawnerInjected confirms that the
// registered spawn tool actually has its SubTurnSpawner wired — otherwise
// execute() would return "Subagent manager not configured" at runtime.
// Regression guard for the architectural risk #1 identified in the audit.
func TestRegisterSharedTools_SpawnToolHasSpawnerInjected(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	cfg.Tools.Spawn.Enabled = true
	cfg.Tools.Subagent.Enabled = false // intentionally off — must still inject

	al := NewAgentLoop(cfg, bus.NewMessageBus(), &mockProvider{})
	agent := al.registry.GetDefaultAgent()
	tool, ok := agent.Tools.Get("spawn")
	if !ok {
		t.Fatal("spawn tool not registered")
	}

	// Use reflection-free duck typing: invoke execute via the public Execute
	// path with an empty task; an unconfigured spawner would surface as
	// "Subagent manager not configured" rather than the validation error
	// for the empty task. Conversely, a properly wired tool returns the
	// validation error first.
	res := tool.Execute(t.Context(), map[string]any{"task": ""})
	if res == nil {
		t.Fatal("expected ToolResult, got nil")
	}
	if got := res.ForLLM; got == "" {
		t.Fatal("expected non-empty ForLLM")
	}
	// Must NOT be the "manager not configured" error — that would mean
	// SetSpawner was never called.
	if got := res.ForLLM; got == "Subagent manager not configured" {
		t.Fatalf("spawner not injected: %q", got)
	}
}
