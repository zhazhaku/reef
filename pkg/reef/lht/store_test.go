package lht

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tempBase creates a temporary directory for testing and returns its path.
func tempBase(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lht-store-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// ----- 2.1: goal_id validation -----

func TestValidateGoalID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid simple", "task-001", false},
		{"valid uuid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"valid alphanumeric", "goal123_alpha", false},
		{"empty", "", true},
		{"dot traversal", "../etc/passwd", true},
		{"dot traversal mid", "task/../secret", true},
		{"slash", "task/001", true},
		{"backslash", "task\\001", true},
		{"null byte", "task\x00001", true},
		{"only dots", "..", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGoalID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGoalID(%q) error=%v, wantErr=%v", tt.id, err, tt.wantErr)
			}
		})
	}
}

// ----- 2.1: atomic writes -----

func TestAtomicWriteFile_Success(t *testing.T) {
	base := tempBase(t)
	path := filepath.Join(base, "test.json")
	data := []byte(`{"key":"value"}`)

	if err := atomicWriteFile(path, data); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	// Verify content.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content mismatch: got %q want %q", got, data)
	}

	// Verify no tmp file left behind.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("tmp file %s should not exist after successful write", tmpPath)
	}
}

func TestAtomicWriteFile_Overwrite(t *testing.T) {
	base := tempBase(t)
	path := filepath.Join(base, "test.json")

	v1 := []byte(`{"version":1}`)
	v2 := []byte(`{"version":2}`)

	if err := atomicWriteFile(path, v1); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	if err := atomicWriteFile(path, v2); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != string(v2) {
		t.Errorf("overwrite failed: got %q want %q", got, v2)
	}

	// No tmp file after overwrite.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file should not exist after overwrite")
	}
}

// TestAtomicWriteFile_NoPartialOnCrash is a best-effort test:
// we verify that after a successful atomic write, no .tmp file lingers.
// A true crash simulation would require process-level injection, but the
// tmp-file+rename pattern inherently prevents half-written files.
func TestAtomicWriteFile_NoPartialOnCrash(t *testing.T) {
	base := tempBase(t)
	path := filepath.Join(base, "test.json")
	data := []byte(`{"crash":"resistant"}`)

	if err := atomicWriteFile(path, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	// After write: no tmp, content intact.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("tmp %s exists — indicates incomplete write", tmpPath)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content mismatch: %q vs %q", got, data)
	}
}

// ----- 2.2: goal.json -----

func TestSaveLoadGoal(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goal := &Goal{
		GoalID:      "g-001",
		Description: "Build a CLI tool",
		State:       StateGrounding,
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}

	if err := s.EnsureTaskDirs(goal.GoalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}
	if err := s.SaveGoal(goal); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}

	loaded, err := s.LoadGoal(goal.GoalID)
	if err != nil {
		t.Fatalf("LoadGoal: %v", err)
	}
	if loaded.GoalID != goal.GoalID {
		t.Errorf("GoalID: got %q want %q", loaded.GoalID, goal.GoalID)
	}
	if loaded.Description != goal.Description {
		t.Errorf("Description: got %q want %q", loaded.Description, goal.Description)
	}
	if loaded.State != goal.State {
		t.Errorf("State: got %q want %q", loaded.State, goal.State)
	}
}

func TestSaveGoal_NilRejected(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	if err := s.SaveGoal(nil); err == nil {
		t.Error("expected error for nil goal")
	}
}

func TestSaveGoal_InvalidIDRejected(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goal := &Goal{GoalID: "../escape"}
	if err := s.SaveGoal(goal); err == nil {
		t.Error("expected error for path traversal goal ID")
	}
}

// ----- 2.2: plan.json -----

func TestSaveLoadPlan(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	plan := &Plan{
		GoalID:      "g-001",
		PlanVersion: "20260803-a1b2c3",
		Tasks: []TaskNode{
			{TaskID: "t1", Description: "Setup project", State: StateGrounding},
			{TaskID: "t2", Description: "Implement core", State: StateGrounding, Dependencies: []string{"t1"}},
		},
		Capabilities: []string{"go", "docker"},
	}

	if err := s.EnsureTaskDirs(plan.GoalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}
	if err := s.SavePlan(plan.GoalID, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	loaded, err := s.LoadPlan(plan.GoalID)
	if err != nil {
		t.Fatalf("LoadPlan: %v", err)
	}
	if loaded.GoalID != plan.GoalID {
		t.Errorf("GoalID: got %q want %q", loaded.GoalID, plan.GoalID)
	}
	if loaded.PlanVersion != plan.PlanVersion {
		t.Errorf("PlanVersion: got %q want %q", loaded.PlanVersion, plan.PlanVersion)
	}
	if len(loaded.Tasks) != 2 {
		t.Errorf("task count: got %d want 2", len(loaded.Tasks))
	}
	if loaded.Tasks[1].Dependencies[0] != "t1" {
		t.Errorf("dependency: got %q want t1", loaded.Tasks[1].Dependencies[0])
	}
}

func TestSavePlan_OverwriteAtomic(t *testing.T) {
	// Simulates /lht insert triggered re-plan: overwrites plan.json atomically.
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-overwrite"

	if err := s.EnsureTaskDirs(goalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}

	planV1 := &Plan{GoalID: goalID, PlanVersion: "v1", Tasks: []TaskNode{
		{TaskID: "t1", Description: "Original task"},
	}}
	planV2 := &Plan{GoalID: goalID, PlanVersion: "v2", Tasks: []TaskNode{
		{TaskID: "t1", Description: "Updated task"},
		{TaskID: "t2", Description: "New inserted task"},
	}}

	if err := s.SavePlan(goalID, planV1); err != nil {
		t.Fatalf("SavePlan v1: %v", err)
	}
	if err := s.SavePlan(goalID, planV2); err != nil {
		t.Fatalf("SavePlan v2: %v", err)
	}

	// Verify v2 is persisted.
	loaded, err := s.LoadPlan(goalID)
	if err != nil {
		t.Fatalf("LoadPlan v2: %v", err)
	}
	if loaded.PlanVersion != "v2" {
		t.Errorf("expected PlanVersion v2, got %q", loaded.PlanVersion)
	}
	if len(loaded.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(loaded.Tasks))
	}
	if loaded.Tasks[1].Description != "New inserted task" {
		t.Errorf("unexpected task: %s", loaded.Tasks[1].Description)
	}

	// No tmp file after overwrite.
	if _, err := os.Stat(filepath.Join(s.taskDir(goalID), "plan.json.tmp")); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after overwrite — atomicity violated")
	}
}

// ----- 2.2: budget.json -----

func TestSaveLoadBudget(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	budget := &Budget{
		GoalID:           "g-001",
		MaxRounds:        10,
		MaxTokens:        100000,
		MaxDurationHours: 2.0,
		UsedRounds:       3,
		UsedTokens:       25000,
		UsedHours:        0.5,
	}

	if err := s.EnsureTaskDirs(budget.GoalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}
	if err := s.SaveBudget(budget.GoalID, budget); err != nil {
		t.Fatalf("SaveBudget: %v", err)
	}

	loaded, err := s.LoadBudget(budget.GoalID)
	if err != nil {
		t.Fatalf("LoadBudget: %v", err)
	}
	if loaded.MaxRounds != 10 {
		t.Errorf("MaxRounds: got %d want 10", loaded.MaxRounds)
	}
	if loaded.UsedTokens != 25000 {
		t.Errorf("UsedTokens: got %d want 25000", loaded.UsedTokens)
	}
	if loaded.UsedHours != 0.5 {
		t.Errorf("UsedHours: got %f want 0.5", loaded.UsedHours)
	}
}

func TestSaveBudget_OverwriteAtomic(t *testing.T) {
	// Budget is overwritten every cycle as resources are consumed.
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-budget-ow"

	if err := s.EnsureTaskDirs(goalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}

	b1 := &Budget{GoalID: goalID, MaxRounds: 10, UsedRounds: 0}
	b2 := &Budget{GoalID: goalID, MaxRounds: 10, UsedRounds: 3}

	if err := s.SaveBudget(goalID, b1); err != nil {
		t.Fatalf("save b1: %v", err)
	}
	if err := s.SaveBudget(goalID, b2); err != nil {
		t.Fatalf("save b2: %v", err)
	}

	loaded, _ := s.LoadBudget(goalID)
	if loaded.UsedRounds != 3 {
		t.Errorf("UsedRounds should be 3, got %d", loaded.UsedRounds)
	}

	// No tmp file.
	if _, err := os.Stat(filepath.Join(s.taskDir(goalID), "budget.json.tmp")); !os.IsNotExist(err) {
		t.Error("tmp file should not exist")
	}
}

// ----- 2.2: state.json -----

func TestSaveLoadState(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goal := &Goal{
		GoalID:      "g-state",
		State:       StateExecuting,
		PlanVersion: "20260803-v1",
		PreviousState: StatePlanning,
	}

	if err := s.EnsureTaskDirs(goal.GoalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}
	if err := s.SaveState(goal.GoalID, goal); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := s.LoadState(goal.GoalID)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.State != StateExecuting {
		t.Errorf("State: got %q want %q", loaded.State, StateExecuting)
	}
	if loaded.PlanVersion != "20260803-v1" {
		t.Errorf("PlanVersion: got %q", loaded.PlanVersion)
	}
	if loaded.PreviousState != StatePlanning {
		t.Errorf("PreviousState: got %q want %q", loaded.PreviousState, StatePlanning)
	}
}

// ----- 2.1: SaveAll multi-file ordered write with plan_version sync -----

func TestSaveAll_OrderAndConsistency(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-saveall"

	goal := &Goal{
		GoalID:    goalID,
		State:     StateExecuting,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
	plan := &Plan{
		GoalID:      goalID,
		PlanVersion: "v3-consistent",
		Tasks: []TaskNode{
			{TaskID: "t1", State: StateExecuting},
		},
	}
	budget := &Budget{
		GoalID:    goalID,
		MaxRounds: 20,
		UsedRounds: 5,
	}

	if err := s.SaveAll(goalID, goal, plan, budget); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Verify all three files exist.
	for _, f := range []string{"state.json", "plan.json", "budget.json"} {
		p := filepath.Join(s.taskDir(goalID), f)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("file %s should exist but does not", f)
		}
	}

	// Verify plan_version sync: goal.PlanVersion must be set from plan.
	loadedGoal, err := s.LoadState(goalID)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loadedGoal.PlanVersion != "v3-consistent" {
		t.Errorf("state PlanVersion: got %q want %q", loadedGoal.PlanVersion, "v3-consistent")
	}

	// Verify plan and budget content.
	loadedPlan, _ := s.LoadPlan(goalID)
	if loadedPlan.PlanVersion != "v3-consistent" {
		t.Errorf("plan version: %q", loadedPlan.PlanVersion)
	}

	loadedBudget, _ := s.LoadBudget(goalID)
	if loadedBudget.MaxRounds != 20 {
		t.Errorf("budget max rounds: %d", loadedBudget.MaxRounds)
	}
}

// ----- 2.5: checkpoint snapshots -----

func TestSaveCheckpointSnapshot(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-ckpt"

	goal := &Goal{
		GoalID:    goalID,
		State:     StateExecuting,
		PlanVersion: "v1",
	}

	if err := s.SaveCheckpointSnapshot(goalID, goal); err != nil {
		t.Fatalf("SaveCheckpointSnapshot: %v", err)
	}

	// Verify a file exists in checkpoints/ with proper naming.
	entries, err := os.ReadDir(s.CheckpointsDir(goalID))
	if err != nil {
		t.Fatalf("read checkpoints dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 checkpoint file")
	}

	// Read back and verify content.
	data, err := os.ReadFile(filepath.Join(s.CheckpointsDir(goalID), entries[0].Name()))
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if string(data) == "" {
		t.Error("checkpoint file is empty")
	}
}

func TestCheckpoint_NotRecoverySource(t *testing.T) {
	// Verify that checkpoints/ is distinct from state.json:
	// Saving a checkpoint does NOT overwrite state.json.
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-ckpt2"

	if err := s.EnsureTaskDirs(goalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}

	// Save state.json separately.
	stateGoal := &Goal{GoalID: goalID, State: StateExecuting, PlanVersion: "v-state"}
	if err := s.SaveState(goalID, stateGoal); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Save checkpoint with different data.
	ckptGoal := &Goal{GoalID: goalID, State: StatePaused, PlanVersion: "v-ckpt"}
	if err := s.SaveCheckpointSnapshot(goalID, ckptGoal); err != nil {
		t.Fatalf("SaveCheckpointSnapshot: %v", err)
	}

	// state.json must still be the authoritative source.
	loaded, _ := s.LoadState(goalID)
	if loaded.State != StateExecuting {
		t.Errorf("state.json should be EXECUTING, got %q — checkpoint overwrote it", loaded.State)
	}
}

// ----- 2.4: crash recovery (LoadTask) -----

func TestLoadTask_HappyPath(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-recover"

	goal := &Goal{
		GoalID:      goalID,
		State:       StateExecuting,
		PlanVersion: "v42",
	}
	plan := &Plan{
		GoalID:      goalID,
		PlanVersion: "v42",
		Tasks:       []TaskNode{{TaskID: "t1", State: StateCompleted}},
	}
	budget := &Budget{
		GoalID:    goalID,
		MaxRounds: 10,
		UsedRounds: 3,
	}

	if err := s.SaveAll(goalID, goal, plan, budget); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	g, p, b, err := s.LoadTask(goalID)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if g.State != StateExecuting {
		t.Errorf("goal state: %q", g.State)
	}
	if p.PlanVersion != "v42" {
		t.Errorf("plan version: %q", p.PlanVersion)
	}
	if b.UsedRounds != 3 {
		t.Errorf("used rounds: %d", b.UsedRounds)
	}
}

func TestLoadTask_PlanVersionMismatch(t *testing.T) {
	// Simulate crash after plan.json was updated but state.json was not.
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-mismatch"

	if err := s.EnsureTaskDirs(goalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}

	// Write state claiming v2.
	stateGoal := &Goal{GoalID: goalID, State: StateExecuting, PlanVersion: "v2"}
	if err := s.SaveState(goalID, stateGoal); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Write plan that is still v1 (simulating incomplete multi-file write).
	plan := &Plan{GoalID: goalID, PlanVersion: "v1"}
	if err := s.SavePlan(goalID, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	// Write a budget so LoadTask doesn't fail on missing budget.
	budget := &Budget{GoalID: goalID}
	if err := s.SaveBudget(goalID, budget); err != nil {
		t.Fatalf("SaveBudget: %v", err)
	}

	_, _, _, err := s.LoadTask(goalID)
	if err == nil {
		t.Fatal("expected PlanVersionMismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "plan_version mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadTask_MissingState(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-nostate"

	if err := s.EnsureTaskDirs(goalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}
	// Don't write state.json.

	_, _, _, err := s.LoadTask(goalID)
	if err == nil {
		t.Fatal("expected error for missing state.json")
	}
}

func TestLoadTask_PlanVersionEmpty(t *testing.T) {
	// When state.PlanVersion is empty (pre-planning stage), no mismatch error.
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-empty-pv"

	if err := s.EnsureTaskDirs(goalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}

	stateGoal := &Goal{GoalID: goalID, State: StateGrounding, PlanVersion: ""}
	if err := s.SaveState(goalID, stateGoal); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	plan := &Plan{GoalID: goalID, PlanVersion: "any-version"}
	if err := s.SavePlan(goalID, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	budget := &Budget{GoalID: goalID}
	if err := s.SaveBudget(goalID, budget); err != nil {
		t.Fatalf("SaveBudget: %v", err)
	}

	g, p, b, err := s.LoadTask(goalID)
	if err != nil {
		t.Fatalf("LoadTask should succeed with empty PlanVersion: %v", err)
	}
	if g.State != StateGrounding {
		t.Errorf("state: %q", g.State)
	}
	_ = p
	_ = b
}

// ----- 2.3: review records -----

func TestSaveLoadReviewRecords(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-review"

	r1 := &ReviewRecord{ReviewRound: 1, Reviewer: "lht-eval", Verdict: "PASS", Score: 0.85}
	r2 := &ReviewRecord{ReviewRound: 2, Reviewer: "lht-rev", Verdict: "UPGRADE", Score: 0.92}

	if err := s.SaveReviewRecord(goalID, r1); err != nil {
		t.Fatalf("SaveReview r1: %v", err)
	}
	if err := s.SaveReviewRecord(goalID, r2); err != nil {
		t.Fatalf("SaveReview r2: %v", err)
	}

	records, err := s.LoadReviewRecords(goalID)
	if err != nil {
		t.Fatalf("LoadReviewRecords: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Reviewer != "lht-eval" {
		t.Errorf("r1 reviewer: %s", records[0].Reviewer)
	}
	if records[1].Verdict != "UPGRADE" {
		t.Errorf("r2 verdict: %s", records[1].Verdict)
	}
}

// ----- 2.3: artifact helpers -----

func TestWriteReadArtifact(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-art"

	data := []byte("binary artifact content")
	if err := s.WriteArtifact(goalID, "output.bin", data); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}

	got, err := s.ReadArtifact(goalID, "output.bin")
	if err != nil {
		t.Fatalf("ReadArtifact: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("artifact mismatch")
	}
}

// ----- 2.3: log helpers -----

func TestWriteReadLogEntry(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)
	goalID := "g-log"

	entry := []byte("2026-08-03T10:00:00Z [INFO] executing subtask t1\n")
	if err := s.WriteLogEntry(goalID, "execution.log", entry); err != nil {
		t.Fatalf("WriteLogEntry: %v", err)
	}

	got, err := s.ReadLogEntry(goalID, "execution.log")
	if err != nil {
		t.Fatalf("ReadLogEntry: %v", err)
	}
	if string(got) != string(entry) {
		t.Errorf("log entry mismatch")
	}
}

// ----- 2.6: EnsureTaskDirs creates full tree -----

func TestEnsureTaskDirs_CreatesFullTree(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)

	if err := s.EnsureTaskDirs("g-dirs"); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}

	dirs := []string{
		s.taskDir("g-dirs"),
		s.ArtifactsDir("g-dirs"),
		s.LogsDir("g-dirs"),
		s.ReviewDir("g-dirs"),
		s.CheckpointsDir("g-dirs"),
	}
	for _, d := range dirs {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("dir %s should exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", d)
		}
	}
}

func TestEnsureTaskDirs_RejectsInvalidID(t *testing.T) {
	base := tempBase(t)
	s := NewStore(base)

	if err := s.EnsureTaskDirs("../escape"); err == nil {
		t.Error("expected error for path traversal goal_id")
	}
}
