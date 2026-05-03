package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// TaskSandbox provides an isolated execution environment for a single task.
// Each sandbox has its own workspace, session, context layers, and memory.
type TaskSandbox struct {
	TaskID  string
	WorkDir string

	layers *ContextLayers
	window *ContextWindow
	guard  *CorruptionGuard

	mu sync.Mutex
}

// NewTaskSandbox creates an isolated sandbox for the given task.
func NewTaskSandbox(taskID string, baseDir string) (*TaskSandbox, error) {
	workDir := filepath.Join(baseDir, "tasks", taskID)
	// Create workdir, workspace, sessions, checkpoints
	for _, dir := range []string{
		workDir,
		filepath.Join(workDir, "workspace"),
		filepath.Join(workDir, "sessions"),
		filepath.Join(workDir, "checkpoints"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("sandbox: mkdir %s: %w", dir, err)
		}
	}

	cfg := DefaultContextConfig()
	sb := &TaskSandbox{
		TaskID:  taskID,
		WorkDir: workDir,
		layers:  NewContextLayers(cfg),
		window:  NewContextWindow(cfg),
		guard:   NewCorruptionGuard(DefaultCorruptionConfig()),
	}

	return sb, nil
}

// Init populates the sandbox context with task parameters.
func (sb *TaskSandbox) Init(systemPrompt, roleConfig string, skills, genes []string, instruction string, metadata map[string]string) {
	sb.layers.SetImmutable(systemPrompt, roleConfig, skills, genes)
	sb.layers.SetTask(instruction, metadata)
	sb.window = NewContextWindow(sb.layers.config)
	sb.window.Build(systemPrompt, roleConfig, skills, genes, instruction, metadata)
}

// AppendRound records an execution round.
func (sb *TaskSandbox) AppendRound(round WorkingRound) {
	sb.layers.AppendRound(round)
}

// Compact triggers context compaction if over budget.
func (sb *TaskSandbox) Compact() error {
	return sb.window.Compact()
}

// CheckCorruption runs corruption detection on current layers.
func (sb *TaskSandbox) CheckCorruption() *CorruptionReport {
	return sb.guard.Check(sb.layers)
}

// Layers returns the current context layers.
func (sb *TaskSandbox) Layers() *ContextLayers {
	return sb.layers
}

// Window returns the context window.
func (sb *TaskSandbox) Window() *ContextWindow {
	return sb.window
}

// Guard returns the corruption guard.
func (sb *TaskSandbox) Guard() *CorruptionGuard {
	return sb.guard
}

// Destroy cleans up the sandbox: extracts working memory and removes the
// work directory (in production this might archive instead of remove).
func (sb *TaskSandbox) Destroy() error {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	// Clear layers
	sb.layers.Clear()
	sb.window = nil
	sb.guard = nil

	// Remove work directory
	return os.RemoveAll(sb.WorkDir)
}
