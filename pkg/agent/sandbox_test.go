package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskSandbox_New(t *testing.T) {
	tmp := t.TempDir()
	sb, err := NewTaskSandbox("task-001", tmp)
	if err != nil {
		t.Fatal(err)
	}

	if sb.TaskID != "task-001" {
		t.Errorf("TaskID = %s", sb.TaskID)
	}

	// Check directories created
	for _, sub := range []string{"workspace", "sessions", "checkpoints"} {
		path := filepath.Join(sb.WorkDir, sub)
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			t.Errorf("missing dir: %s (err=%v)", sub, err)
		}
	}
}

func TestTaskSandbox_ContextIsolation(t *testing.T) {
	tmp := t.TempDir()

	sbA, _ := NewTaskSandbox("task-A", tmp)
	sbB, _ := NewTaskSandbox("task-B", tmp)

	sbA.Init("sys A", "role A", nil, nil, "do task A", nil)
	sbB.Init("sys B", "role B", nil, nil, "do task B", nil)

	if sbA.layers.Immutable == sbB.layers.Immutable {
		t.Error("Immutable layers are not isolated")
	}
	if sbA.layers.Task == sbB.layers.Task {
		t.Error("Task layers are not isolated")
	}
}

func TestTaskSandbox_WorkDir_Independent(t *testing.T) {
	tmp := t.TempDir()

	sbA, _ := NewTaskSandbox("task-A", tmp)
	sbB, _ := NewTaskSandbox("task-B", tmp)

	if sbA.WorkDir == sbB.WorkDir {
		t.Error("work dirs are the same")
	}

	// Write a file in sbA, check it's not in sbB
	os.WriteFile(filepath.Join(sbA.WorkDir, "workspace", "a.txt"), []byte("A"), 0644)
	if _, err := os.Stat(filepath.Join(sbB.WorkDir, "workspace", "a.txt")); err == nil {
		t.Error("file A appears in task-B workspace")
	}
}

func TestTaskSandbox_Destroy(t *testing.T) {
	tmp := t.TempDir()
	sb, _ := NewTaskSandbox("task-001", tmp)

	workDir := sb.WorkDir
	if err := sb.Destroy(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Error("work dir not removed after destroy")
	}
}

func TestTaskSandbox_AppendRound(t *testing.T) {
	tmp := t.TempDir()
	sb, _ := NewTaskSandbox("t-1", tmp)

	sb.AppendRound(WorkingRound{Round: 1, Call: "exec", Output: "ok"})
	sb.AppendRound(WorkingRound{Round: 2, Call: "read", Output: "data"})

	if len(sb.layers.Working) != 2 {
		t.Fatalf("Working = %d", len(sb.layers.Working))
	}
}

func TestTaskSandbox_Compact(t *testing.T) {
	tmp := t.TempDir()
	sb, _ := NewTaskSandbox("t-1", tmp)

	sb.Init("short", "coder", nil, nil, "task", nil)
	sb.AppendRound(WorkingRound{Round: 1, Call: "exec", Output: "ok"})

	if err := sb.Compact(); err != nil {
		t.Fatal(err)
	}
	// With tiny content, compaction should be a no-op
}

func TestTaskSandbox_CheckCorruption(t *testing.T) {
	tmp := t.TempDir()
	sb, _ := NewTaskSandbox("t-1", tmp)

	sb.Init("sys", "coder", nil, nil, "task", nil)
	// With clean rounds, no corruption
	sb.AppendRound(WorkingRound{Round: 1, Call: "read_file", Output: "code", Thought: "reading"})
	sb.AppendRound(WorkingRound{Round: 2, Call: "edit_file", Output: "fixed", Thought: "done"})

	if report := sb.CheckCorruption(); report != nil {
		t.Errorf("unexpected corruption: %v", report)
	}
}

func TestTaskSandbox_Guard(t *testing.T) {
	tmp := t.TempDir()
	sb, _ := NewTaskSandbox("t-1", tmp)

	g := sb.Guard()
	if g == nil {
		t.Error("Guard is nil")
	}

	sb.Guard().SetGoal("do thing")
	sb.AppendRound(WorkingRound{Round: 1, Call: "exec", Output: "done"})

	// FeedRound doesn't crash
	report := g.FeedRound("exec", "ok", "ok")
	_ = report
}
