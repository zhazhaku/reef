package agent

import (
	"strings"
	"testing"
	"time"
)

func TestCheckpoint_ShouldSave_FirstTime(t *testing.T) {
	tmp := t.TempDir()
	cm := NewCheckpointManager(DefaultCheckpointConfig("t-1", tmp))

	if !cm.ShouldSave(1) {
		t.Error("ShouldSave should return true on first save")
	}
}

func TestCheckpoint_ShouldSave_RoundTrigger(t *testing.T) {
	tmp := t.TempDir()
	cm := NewCheckpointManager(DefaultCheckpointConfig("t-1", tmp))

	// First save
	cm.Save("checkpoint 1", 1)

	// Round 2-4: no save
	if cm.ShouldSave(2) {
		t.Error("ShouldSave at round 2")
	}

	// Round 5: multiple of MaxRounds=5 → should save
	if !cm.ShouldSave(5) {
		t.Error("ShouldSave at round 5")
	}

	// Round 10: also multiple
	cm.Save("cp", 5)
	if !cm.ShouldSave(10) {
		t.Error("ShouldSave at round 10")
	}
}

func TestCheckpoint_SaveAndRestore(t *testing.T) {
	tmp := t.TempDir()
	cm := NewCheckpointManager(DefaultCheckpointConfig("t-1", tmp))

	if err := cm.Save("Round 5: completed auth module", 5); err != nil {
		t.Fatal(err)
	}

	cp, err := cm.Restore()
	if err != nil {
		t.Fatal(err)
	}
	if cp.RoundNum != 5 {
		t.Errorf("RoundNum = %d, want 5", cp.RoundNum)
	}
	if cp.Summary != "Round 5: completed auth module" {
		t.Errorf("Summary = %s", cp.Summary)
	}
}

func TestCheckpoint_Restore_Nonexistent(t *testing.T) {
	tmp := t.TempDir()
	cm := NewCheckpointManager(DefaultCheckpointConfig("t-1", tmp))

	_, err := cm.Restore()
	if err == nil {
		t.Error("expected error for empty checkpoints")
	}
}

func TestCheckpoint_Restore_MostRecent(t *testing.T) {
	tmp := t.TempDir()
	cm := NewCheckpointManager(DefaultCheckpointConfig("t-1", tmp))

	cm.Save("old", 1)
	time.Sleep(10 * time.Millisecond) // ensure different timestamps
	cm.Save("new", 5)

	cp, _ := cm.Restore()
	if cp.Summary != "new" {
		t.Errorf("restored summary = %s, want 'new'", cp.Summary)
	}
}

func TestCheckpoint_MaxRotation(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultCheckpointConfig("t-1", tmp)
	cfg.MaxCount = 3
	cm := NewCheckpointManager(cfg)

	// Save 5 checkpoints, should keep only 3
	for i := 1; i <= 5; i++ {
		cm.Save("cp", i)
		time.Sleep(5 * time.Millisecond)
	}

	// Restore gets the latest
	cp, _ := cm.Restore()
	if cp.RoundNum != 5 {
		t.Errorf("RoundNum = %d, want 5", cp.RoundNum)
	}
}

func TestCheckpoint_BuildResumeInstruction(t *testing.T) {
	tmp := t.TempDir()
	cm := NewCheckpointManager(DefaultCheckpointConfig("t-1", tmp))

	cp := &Checkpoint{
		TaskID:    "t-1",
		RoundNum:  7,
		Summary:   "Halfway through refactor",
		Timestamp: 1712345678,
	}

	instruction := cm.BuildResumeInstruction(cp)

	if !strings.Contains(instruction, "round 7") {
		t.Error("instruction missing round number")
	}
	if !strings.Contains(instruction, "Halfway through refactor") {
		t.Error("instruction missing summary")
	}
	if !strings.Contains(instruction, "Continue where you left off") {
		t.Error("instruction missing resume prompt")
	}
}

func TestCheckpoint_SummaryOnly(t *testing.T) {
	tmp := t.TempDir()
	cm := NewCheckpointManager(DefaultCheckpointConfig("t-1", tmp))

	// Save with a summary (not full context)
	cm.Save("Compact summary of rounds 1-8", 8)

	cp, _ := cm.Restore()
	// Summary should be concise
	if len(cp.Summary) == 0 {
		t.Error("summary is empty")
	}
}

func TestDefaultCheckpointConfig(t *testing.T) {
	cfg := DefaultCheckpointConfig("t-1", "/tmp/base")

	if cfg.Interval != 5*time.Minute {
		t.Errorf("Interval = %v", cfg.Interval)
	}
	if cfg.MaxRounds != 5 {
		t.Errorf("MaxRounds = %d", cfg.MaxRounds)
	}
	if cfg.MaxCount != 10 {
		t.Errorf("MaxCount = %d", cfg.MaxCount)
	}
}

func TestCheckpointManager_ZeroDefaults(t *testing.T) {
	tmp := t.TempDir()
	cm := NewCheckpointManager(CheckpointConfig{Dir: tmp})

	if cm.cfg.Interval != 5*time.Minute {
		t.Errorf("Interval = %v", cm.cfg.Interval)
	}
}

func TestCheckpoint_LastSaveTime(t *testing.T) {
	tmp := t.TempDir()
	cm := NewCheckpointManager(DefaultCheckpointConfig("t-1", tmp))

	if !cm.LastSaveTime().IsZero() {
		t.Error("initial lastSaveTime should be zero")
	}

	cm.Save("cp", 1)
	if cm.LastSaveTime().IsZero() {
		t.Error("lastSaveTime should not be zero after save")
	}
}
