package agent

import (
	"os"
	"path/filepath"

	client "github.com/zhazhaku/reef/pkg/reef/client"
)

// ReefSandboxFactory creates a client.Sandbox backed by a TaskSandbox.
// This bridges picoclaw's cognitive sandbox into the reef client's Sandbox interface.
func ReefSandboxFactory(taskID, baseDir string) (client.Sandbox, error) {
	workDir := filepath.Join(baseDir, "sandbox-"+taskID)
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, err
	}
	sb, err := NewTaskSandbox(taskID, workDir)
	if err != nil {
		return nil, err
	}
	return &reefSandboxAdapter{sb: sb}, nil
}

// reefSandboxAdapter wraps TaskSandbox to implement client.Sandbox.
type reefSandboxAdapter struct {
	sb *TaskSandbox
}

func (a *reefSandboxAdapter) TaskID() string                           { return a.sb.TaskID }
func (a *reefSandboxAdapter) AppendRound(call, output, thought string) {
	a.sb.AppendRound(WorkingRound{Call: call, Output: output, Thought: thought})
}
func (a *reefSandboxAdapter) RecordProgress(round int, message string) {}
func (a *reefSandboxAdapter) Destroy() error                            { return a.sb.Destroy() }
