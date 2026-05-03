package agent

import (
	"testing"
)

func TestReefSandboxFactory(t *testing.T) {
	tmp := t.TempDir()
	sb, err := ReefSandboxFactory("task-1", tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Destroy()

	if sb.TaskID() != "task-1" {
		t.Errorf("TaskID = %s", sb.TaskID())
	}

	sb.AppendRound("exec", "ok", "")
	sb.RecordProgress(1, "half done")

	if err := sb.Destroy(); err != nil {
		t.Fatal(err)
	}
}

func TestReefSandbox_Isolation(t *testing.T) {
	tmp := t.TempDir()

	sb1, _ := ReefSandboxFactory("task-A", tmp)
	sb2, _ := ReefSandboxFactory("task-B", tmp)

	if sb1.TaskID() == sb2.TaskID() {
		t.Error("same task ID")
	}

	sb1.Destroy()
	sb2.Destroy()
}

func TestReefSandbox_Rounds(t *testing.T) {
	tmp := t.TempDir()
	sb, _ := ReefSandboxFactory("task-1", tmp)
	defer sb.Destroy()

	for i := 0; i < 5; i++ {
		sb.AppendRound("exec", "ok", "")
	}

	// Adapter should have stored all rounds on the underlying sandbox
	adapter := sb.(*reefSandboxAdapter)
	if adapter.sb.Rounds() != 5 {
		t.Errorf("rounds = %d, want 5", adapter.sb.Rounds())
	}
}
