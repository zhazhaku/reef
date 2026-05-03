package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Checkpoint stores a snapshot of task state for recovery.
type Checkpoint struct {
	TaskID    string `json:"task_id"`
	RoundNum  int    `json:"round_num"`
	Summary   string `json:"summary"`
	Timestamp int64  `json:"timestamp"`
}

// CheckpointConfig defines checkpoint autosave policy.
type CheckpointConfig struct {
	Dir        string        `json:"-"`            // checkpoint storage directory (set from task context, not config)
	Interval   time.Duration `json:"interval_ms"`  // time-based save interval in ms (default 5 min)
	MaxRounds  int           `json:"max_rounds"`   // round-based save interval (default 5)
	MaxCount   int           `json:"max_count"`    // max checkpoints to retain, oldest rotated (default 10)
}

// DefaultCheckpointConfig returns sensible defaults.
func DefaultCheckpointConfig(taskID, baseDir string) CheckpointConfig {
	return CheckpointConfig{
		Dir:       filepath.Join(baseDir, "tasks", taskID, "checkpoints"),
		Interval:  5 * time.Minute,
		MaxRounds: 5,
		MaxCount:  10,
	}
}

// CheckpointManager manages automatic checkpoint saving and restoration.
type CheckpointManager struct {
	cfg        CheckpointConfig
	taskID     string
	lastSaveAt time.Time
	mu         sync.Mutex
}

// NewCheckpointManager creates a manager with the given config.
func NewCheckpointManager(cfg CheckpointConfig) *CheckpointManager {
	os.MkdirAll(cfg.Dir, 0o755)
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.MaxRounds == 0 {
		cfg.MaxRounds = 5
	}
	if cfg.MaxCount == 0 {
		cfg.MaxCount = 10
	}
	return &CheckpointManager{cfg: cfg}
}

// ShouldSave returns true if it's time to save a checkpoint based on
// round count or elapsed time (whichever triggers first).
func (cm *CheckpointManager) ShouldSave(roundNum int) bool {
	if cm.lastSaveAt.IsZero() {
		return true // first save
	}
	if roundNum%cm.cfg.MaxRounds == 0 {
		return true
	}
	if time.Since(cm.lastSaveAt) >= cm.cfg.Interval {
		return true
	}
	return false
}

// Save writes a checkpoint file with the given summary.
func (cm *CheckpointManager) Save(summary string, roundNum int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cp := Checkpoint{
		TaskID:    cm.cfg.Dir, // store task ID context
		RoundNum:  roundNum,
		Summary:   summary,
		Timestamp: time.Now().Unix(),
	}

	// Write to file: {dir}/checkpoint_{timestamp}.json
	filename := filepath.Join(cm.cfg.Dir, fmt.Sprintf("checkpoint_%d.json", cp.Timestamp))
	data, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return err
	}

	cm.lastSaveAt = time.Now()

	// Enforce max count: remove oldest if over limit
	cm.rotate()

	return nil
}

// Restore loads the most recent checkpoint.
func (cm *CheckpointManager) Restore() (*Checkpoint, error) {
	entries, err := cm.listCheckpoints()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no checkpoint found")
	}

	// Most recent is the last one (sorted by timestamp)
	latest := entries[len(entries)-1]
	data, err := os.ReadFile(latest)
	if err != nil {
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// BuildResumeInstruction creates a resume instruction for the LLM.
func (cm *CheckpointManager) BuildResumeInstruction(cp *Checkpoint) string {
	return fmt.Sprintf(
		"[Resume from checkpoint: round %d, saved at %s]\nPrevious summary: %s\nContinue where you left off.",
		cp.RoundNum,
		time.Unix(cp.Timestamp, 0).Format(time.RFC3339),
		cp.Summary,
	)
}

// rotate removes the oldest checkpoint if maxCount is exceeded.
func (cm *CheckpointManager) rotate() {
	entries, err := cm.listCheckpoints()
	if err != nil || len(entries) <= cm.cfg.MaxCount {
		return
	}
	// Remove oldest checkpoints
	excess := len(entries) - cm.cfg.MaxCount
	for _, path := range entries[:excess] {
		os.Remove(path)
	}
}

// listCheckpoints returns sorted checkpoint file paths (oldest first).
func (cm *CheckpointManager) listCheckpoints() ([]string, error) {
	entries, err := os.ReadDir(cm.cfg.Dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			paths = append(paths, filepath.Join(cm.cfg.Dir, e.Name()))
		}
	}
	sort.Strings(paths) // filename includes timestamp, so this sorts by time
	return paths, nil
}

// LastSaveTime returns the last save time.
func (cm *CheckpointManager) LastSaveTime() time.Time {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.lastSaveAt
}
