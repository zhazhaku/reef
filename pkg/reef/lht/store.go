// Package lht — storage layer for Long-Horizon Task engine.
//
// Implements W2: persistence via atomic writes, multi-file ordering,
// crash recovery, and checkpoint snapshots per the lht-persistence spec.
package lht

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Sentinel errors for persistence operations.
var (
	ErrInvalidGoalID       = errors.New("invalid goal_id: path traversal denied")
	ErrPlanVersionMismatch = errors.New("plan_version mismatch between state.json and plan.json")
)

// Store provides disk persistence for LHT tasks under BasePath/{goal_id}/.
//
// Directory layout (per the persistence spec):
//
//	BasePath/{goal_id}/
//	  goal.json       — Goal definition
//	  plan.json       — execution Plan (DAG)
//	  state.json      — authoritative recovery snapshot (overwritten each cycle)
//	  budget.json     — Budget tracking
//	  artifacts/      — task outputs
//	  logs/           — execution logs
//	  review/         — review records
//	  checkpoints/    — historical state snapshots (audit / rollback only)
type Store struct {
	BasePath string // root for longhorizon storage, e.g. "workspace/longhorizon"
}

// NewStore creates a Store rooted at basePath.
func NewStore(basePath string) *Store {
	return &Store{BasePath: basePath}
}

// validateGoalID rejects goal IDs that could cause path traversal.
// Allowed: alphanumeric, hyphen, underscore; rejected: "..", "/", "\\", null byte, empty.
func validateGoalID(goalID string) error {
	if goalID == "" {
		return fmt.Errorf("%w: empty goal_id", ErrInvalidGoalID)
	}
	if strings.Contains(goalID, "..") {
		return fmt.Errorf("%w: %q contains '..'", ErrInvalidGoalID, goalID)
	}
	if strings.Contains(goalID, "/") || strings.Contains(goalID, "\\") {
		return fmt.Errorf("%w: %q contains path separator", ErrInvalidGoalID, goalID)
	}
	if strings.ContainsRune(goalID, 0) {
		return fmt.Errorf("%w: %q contains null byte", ErrInvalidGoalID, goalID)
	}
	return nil
}

// taskDir returns the per-goal directory path (without validation).
func (s *Store) taskDir(goalID string) string {
	return filepath.Join(s.BasePath, goalID)
}

// ----- directory helpers (2.3) -----

// EnsureTaskDirs creates the full directory tree for a task.
func (s *Store) EnsureTaskDirs(goalID string) error {
	if err := validateGoalID(goalID); err != nil {
		return err
	}
	dirs := []string{
		s.taskDir(goalID),
		s.ArtifactsDir(goalID),
		s.LogsDir(goalID),
		s.ReviewDir(goalID),
		s.CheckpointsDir(goalID),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

// ArtifactsDir returns the artifacts/ path for a goal.
func (s *Store) ArtifactsDir(goalID string) string {
	return filepath.Join(s.BasePath, goalID, "artifacts")
}

// LogsDir returns the logs/ path for a goal.
func (s *Store) LogsDir(goalID string) string {
	return filepath.Join(s.BasePath, goalID, "logs")
}

// ReviewDir returns the review/ path for a goal.
func (s *Store) ReviewDir(goalID string) string {
	return filepath.Join(s.BasePath, goalID, "review")
}

// CheckpointsDir returns the checkpoints/ path for a goal.
func (s *Store) CheckpointsDir(goalID string) string {
	return filepath.Join(s.BasePath, goalID, "checkpoints")
}

// ----- atomic write primitive (2.1) -----

// atomicWriteFile writes data to path using the temp-file + rename pattern.
// On crash the caller sees either the complete old file or the complete new file,
// never a half-written file. The temp file is cleaned up on any error.
func atomicWriteFile(path string, data []byte) error {
	tmpPath := path + ".tmp"

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("atomic write mkdir: %w", err)
	}

	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("atomic write create tmp: %w", err)
	}

	// Write all bytes.
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("atomic write: %w", err)
	}

	// fsync to disk so the content is durable before rename.
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("atomic write sync: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic write close: %w", err)
	}

	// Atomic rename — the actual commit point.
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic write rename: %w", err)
	}

	return nil
}

// ----- goal.json (2.2) -----

// SaveGoal persists a Goal to goal.json.
func (s *Store) SaveGoal(goal *Goal) error {
	if goal == nil {
		return fmt.Errorf("cannot save nil goal")
	}
	if err := validateGoalID(goal.GoalID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(goal, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal goal: %w", err)
	}
	return atomicWriteFile(filepath.Join(s.taskDir(goal.GoalID), "goal.json"), data)
}

// LoadGoal reads a Goal from goal.json.
func (s *Store) LoadGoal(goalID string) (*Goal, error) {
	if err := validateGoalID(goalID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.taskDir(goalID), "goal.json"))
	if err != nil {
		return nil, fmt.Errorf("read goal.json: %w", err)
	}
	var goal Goal
	if err := json.Unmarshal(data, &goal); err != nil {
		return nil, fmt.Errorf("unmarshal goal: %w", err)
	}
	return &goal, nil
}

// ----- plan.json (2.2) -----

// SavePlan persists a Plan to plan.json atomically.
func (s *Store) SavePlan(goalID string, plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("cannot save nil plan")
	}
	if err := validateGoalID(goalID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	return atomicWriteFile(filepath.Join(s.taskDir(goalID), "plan.json"), data)
}

// LoadPlan reads a Plan from plan.json.
func (s *Store) LoadPlan(goalID string) (*Plan, error) {
	if err := validateGoalID(goalID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.taskDir(goalID), "plan.json"))
	if err != nil {
		return nil, fmt.Errorf("read plan.json: %w", err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}
	return &plan, nil
}

// ----- state.json (2.2 + 2.5) -----

// SaveState persists a Goal as state.json — the authoritative recovery source.
// This is overwritten every cycle; the caller is responsible for setting
// PlanVersion to the current Plan's version before calling.
func (s *Store) SaveState(goalID string, goal *Goal) error {
	if goal == nil {
		return fmt.Errorf("cannot save nil state")
	}
	if err := validateGoalID(goalID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(goal, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return atomicWriteFile(filepath.Join(s.taskDir(goalID), "state.json"), data)
}

// LoadState reads the authoritative state snapshot from state.json.
func (s *Store) LoadState(goalID string) (*Goal, error) {
	if err := validateGoalID(goalID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.taskDir(goalID), "state.json"))
	if err != nil {
		return nil, fmt.Errorf("read state.json: %w", err)
	}
	var goal Goal
	if err := json.Unmarshal(data, &goal); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &goal, nil
}

// ----- budget.json (2.2) -----

// SaveBudget persists a Budget to budget.json atomically.
func (s *Store) SaveBudget(goalID string, budget *Budget) error {
	if budget == nil {
		return fmt.Errorf("cannot save nil budget")
	}
	if err := validateGoalID(goalID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(budget, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal budget: %w", err)
	}
	return atomicWriteFile(filepath.Join(s.taskDir(goalID), "budget.json"), data)
}

// LoadBudget reads a Budget from budget.json.
func (s *Store) LoadBudget(goalID string) (*Budget, error) {
	if err := validateGoalID(goalID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.taskDir(goalID), "budget.json"))
	if err != nil {
		return nil, fmt.Errorf("read budget.json: %w", err)
	}
	var budget Budget
	if err := json.Unmarshal(data, &budget); err != nil {
		return nil, fmt.Errorf("unmarshal budget: %w", err)
	}
	return &budget, nil
}

// ----- multi-file ordered write (2.1 consistency ordering) -----

// SaveAll writes plan.json, budget.json, then state.json in the required order.
// plan.json and budget.json are written first; state.json is written last as
// the consistency flag. The goal's PlanVersion is synced from plan before writing.
//
// All three writes are atomic individually. If a crash occurs mid-way:
//   - After plan.json only: state.json is stale → LoadTask detects version mismatch.
//   - After budget.json only: same as above.
//   - After state.json: all three files are consistent.
func (s *Store) SaveAll(goalID string, goal *Goal, plan *Plan, budget *Budget) error {
	if err := validateGoalID(goalID); err != nil {
		return err
	}
	if err := s.EnsureTaskDirs(goalID); err != nil {
		return err
	}

	// Sync PlanVersion for cross-file consistency.
	if plan != nil {
		goal.PlanVersion = plan.PlanVersion
	}

	// Step 1: plan.json
	if plan != nil {
		if err := s.SavePlan(goalID, plan); err != nil {
			return fmt.Errorf("SaveAll plan: %w", err)
		}
	}

	// Step 2: budget.json
	if budget != nil {
		if err := s.SaveBudget(goalID, budget); err != nil {
			return fmt.Errorf("SaveAll budget: %w", err)
		}
	}

	// Step 3: state.json — consistency flag written last.
	if goal != nil {
		if err := s.SaveState(goalID, goal); err != nil {
			return fmt.Errorf("SaveAll state: %w", err)
		}
	}

	return nil
}

// ----- checkpoint snapshots (2.5) -----

// SaveCheckpointSnapshot writes a timestamped copy of the goal state to
// checkpoints/{state_<timestamp>.json}. This is for audit/rollback only;
// state.json remains the authoritative recovery source.
func (s *Store) SaveCheckpointSnapshot(goalID string, goal *Goal) error {
	if err := validateGoalID(goalID); err != nil {
		return err
	}
	if goal == nil {
		return fmt.Errorf("cannot checkpoint nil goal")
	}
	if err := os.MkdirAll(s.CheckpointsDir(goalID), 0755); err != nil {
		return fmt.Errorf("checkpoint mkdir: %w", err)
	}

	ts := time.Now().UTC().Format("20060102T150405Z")
	filename := fmt.Sprintf("state_%s.json", ts)
	path := filepath.Join(s.CheckpointsDir(goalID), filename)

	data, err := json.MarshalIndent(goal, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	return atomicWriteFile(path, data)
}

// ----- crash recovery (2.4) -----

// LoadTask performs crash recovery from disk.
//
// Recovery order:
//  1. Load state.json (authoritative source).
//  2. Load plan.json.
//  3. Validate plan_version: state.PlanVersion must match plan.PlanVersion
//     (unless state.PlanVersion is empty — task hasn't been planned yet).
//  4. Load budget.json.
//
// Returns ErrPlanVersionMismatch if the version invariant is violated.
func (s *Store) LoadTask(goalID string) (*Goal, *Plan, *Budget, error) {
	if err := validateGoalID(goalID); err != nil {
		return nil, nil, nil, err
	}

	// Step 1: state.json is the authoritative recovery source.
	goal, err := s.LoadState(goalID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("LoadTask state: %w", err)
	}

	// Step 2: plan.json
	plan, err := s.LoadPlan(goalID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("LoadTask plan: %w", err)
	}

	// Step 3: validate plan_version consistency.
	// If state has a non-empty PlanVersion it MUST match plan.PlanVersion.
	if goal.PlanVersion != "" && plan.PlanVersion != goal.PlanVersion {
		return nil, nil, nil, fmt.Errorf("%w: state=%q plan=%q",
			ErrPlanVersionMismatch, goal.PlanVersion, plan.PlanVersion)
	}

	// Step 4: budget.json
	budget, err := s.LoadBudget(goalID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("LoadTask budget: %w", err)
	}

	return goal, plan, budget, nil
}

// ----- review records (2.3) -----

// SaveReviewRecord persists a review record to review/review_NNN.json.
func (s *Store) SaveReviewRecord(goalID string, record *ReviewRecord) error {
	if err := validateGoalID(goalID); err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("cannot save nil review record")
	}
	if err := os.MkdirAll(s.ReviewDir(goalID), 0755); err != nil {
		return fmt.Errorf("review mkdir: %w", err)
	}

	filename := fmt.Sprintf("review_%03d.json", record.ReviewRound)
	path := filepath.Join(s.ReviewDir(goalID), filename)

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal review: %w", err)
	}
	return atomicWriteFile(path, data)
}

// LoadReviewRecords loads all review records from the review/ directory.
// Returns nil, nil if the directory does not exist.
func (s *Store) LoadReviewRecords(goalID string) ([]*ReviewRecord, error) {
	if err := validateGoalID(goalID); err != nil {
		return nil, err
	}

	dir := s.ReviewDir(goalID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read review dir: %w", err)
	}

	var records []*ReviewRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec ReviewRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		records = append(records, &rec)
	}
	return records, nil
}

// ----- artifact & log helpers (2.3) -----

// WriteArtifact writes raw data to a file in artifacts/.
func (s *Store) WriteArtifact(goalID, filename string, data []byte) error {
	if err := validateGoalID(goalID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.ArtifactsDir(goalID), 0755); err != nil {
		return fmt.Errorf("artifact mkdir: %w", err)
	}
	path := filepath.Join(s.ArtifactsDir(goalID), filename)
	return atomicWriteFile(path, data)
}

// ReadArtifact reads a file from artifacts/.
func (s *Store) ReadArtifact(goalID, filename string) ([]byte, error) {
	if err := validateGoalID(goalID); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(s.ArtifactsDir(goalID), filename))
}

// WriteLogEntry writes a log entry to logs/{filename}. Uses atomic write.
func (s *Store) WriteLogEntry(goalID, filename string, data []byte) error {
	if err := validateGoalID(goalID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.LogsDir(goalID), 0755); err != nil {
		return fmt.Errorf("log mkdir: %w", err)
	}
	path := filepath.Join(s.LogsDir(goalID), filename)
	return atomicWriteFile(path, data)
}

// ReadLogEntry reads a log file from logs/.
func (s *Store) ReadLogEntry(goalID, filename string) ([]byte, error) {
	if err := validateGoalID(goalID); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(s.LogsDir(goalID), filename))
}
