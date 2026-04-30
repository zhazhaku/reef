package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server/store"
)

func setupDAGTest(t *testing.T) (*DAGEngine, *Scheduler, *Registry, func()) {
	t.Helper()

	memStore := store.NewMemoryStore()
	registry := NewRegistry(nil)
	queue := NewPriorityQueue(100, 0)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	scheduler := NewScheduler(registry, queue, SchedulerOptions{
		MaxEscalations: 2,
		Logger:         logger,
	})

	dag := NewDAGEngine(memStore, scheduler, logger)

	cleanup := func() {
		memStore.Close()
	}
	return dag, scheduler, registry, cleanup
}

func TestDAGEngine_CreateSubTasks(t *testing.T) {
	dag, _, _, cleanup := setupDAGTest(t)
	defer cleanup()

	parent := reef.NewTask("parent-1", "build entire app", "coder", nil)
	parent.Priority = 8

	plans := []SubTaskPlan{
		{Instruction: "build backend", Role: "coder", Skills: []string{"go"}},
		{Instruction: "build frontend", Role: "coder", Skills: []string{"react"}},
		{Instruction: "integrate", Role: "coder", Skills: []string{"go", "react"}, DependsOn: []string{"parent-1-sub-1", "parent-1-sub-2"}},
	}

	subs, err := dag.CreateSubTasks(parent, plans)
	if err != nil {
		t.Fatalf("CreateSubTasks: %v", err)
	}

	if len(subs) != 3 {
		t.Fatalf("expected 3 sub-tasks, got %d", len(subs))
	}

	if subs[0].ParentTaskID != "parent-1" {
		t.Errorf("sub[0].ParentTaskID = %s, expected parent-1", subs[0].ParentTaskID)
	}
	if subs[2].Dependencies == nil || len(subs[2].Dependencies) != 2 {
		t.Errorf("sub[2] should have 2 dependencies, got %v", subs[2].Dependencies)
	}

	if subs[0].Status.IsBlocked() {
		t.Error("sub[0] should not be blocked")
	}
	if subs[1].Status.IsBlocked() {
		t.Error("sub[1] should not be blocked")
	}
	if !subs[2].Status.IsBlocked() {
		t.Errorf("sub[2] should be blocked, got %s", subs[2].Status)
	}

	if len(parent.SubTaskIDs) != 3 {
		t.Errorf("parent should have 3 sub-task IDs, got %d", len(parent.SubTaskIDs))
	}
}

func TestDAGEngine_OnSubTaskCompleted_UnblocksDependents(t *testing.T) {
	dag, scheduler, _, cleanup := setupDAGTest(t)
	defer cleanup()

	parent := reef.NewTask("parent-2", "test", "coder", nil)

	plans := []SubTaskPlan{
		{Instruction: "step 1", Role: "coder"},
		{Instruction: "step 2", Role: "coder", DependsOn: []string{"parent-2-sub-1"}},
	}

	subs, err := dag.CreateSubTasks(parent, plans)
	if err != nil {
		t.Fatalf("CreateSubTasks: %v", err)
	}

	// Move sub[0] through dispatch states, then complete
	st0 := scheduler.GetTask(subs[0].ID)
	if st0 == nil {
		t.Fatal("sub[0] not in scheduler")
	}
	_ = st0.Transition(reef.TaskAssigned)
	_ = st0.Transition(reef.TaskRunning)
	st0.Result = &reef.TaskResult{Text: "done"}
	if err := scheduler.HandleTaskCompleted(subs[0].ID, &reef.TaskResult{Text: "done"}); err != nil {
		t.Fatalf("HandleTaskCompleted: %v", err)
	}

	allDone, err := dag.OnSubTaskCompleted(subs[0].ID)
	if err != nil {
		t.Fatalf("OnSubTaskCompleted: %v", err)
	}
	if allDone {
		t.Error("allDone should be false (step 2 not done)")
	}

	// Step 2 should now be unblocked and in the scheduler
	step2 := scheduler.GetTask(subs[1].ID)
	if step2 == nil {
		t.Fatal("step 2 should be registered in scheduler after unblock")
	}
	if step2.Status.IsBlocked() {
		t.Error("step 2 should be unblocked after step 1 completes")
	}
}

func TestDAGEngine_OnSubTaskFailed_FailsDependents(t *testing.T) {
	dag, scheduler, _, cleanup := setupDAGTest(t)
	defer cleanup()

	parent := reef.NewTask("parent-3", "test", "coder", nil)

	plans := []SubTaskPlan{
		{Instruction: "step 1", Role: "coder"},
		{Instruction: "step 2", Role: "coder", DependsOn: []string{"parent-3-sub-1"}},
	}

	subs, err := dag.CreateSubTasks(parent, plans)
	if err != nil {
		t.Fatalf("CreateSubTasks: %v", err)
	}

	// Fail step-1 via scheduler (need to go through dispatch states)
	st0 := scheduler.GetTask(subs[0].ID)
	if st0 != nil {
		_ = st0.Transition(reef.TaskAssigned)
		_ = st0.Transition(reef.TaskRunning)
	}
	scheduler.HandleTaskFailed(subs[0].ID, &reef.TaskError{Type: "test", Message: "failed"}, nil)

	err = dag.OnSubTaskFailed(subs[0].ID)
	if err != nil {
		t.Fatalf("OnSubTaskFailed: %v", err)
	}

	// Step 2 should be failed — check store (it was blocked, never in scheduler)
	step2, err := dag.Store.GetTask(subs[1].ID)
	if err != nil {
		t.Fatalf("get step2 from store: %v", err)
	}
	if step2.Status != reef.TaskFailed {
		t.Errorf("step 2 should be failed, got %s", step2.Status)
	}
	if step2.Error == nil || step2.Error.Type != "dependency_failed" {
		t.Errorf("step 2 error should be dependency_failed, got %v", step2.Error)
	}
}

func TestDAGEngine_AllSubTasksCompleted_Aggregation(t *testing.T) {
	dag, scheduler, _, cleanup := setupDAGTest(t)
	defer cleanup()

	parent := reef.NewTask("parent-4", "test", "coder", nil)
	scheduler.RegisterTask(parent)

	plans := []SubTaskPlan{
		{Instruction: "step 1", Role: "coder"},
		{Instruction: "step 2", Role: "coder"},
	}

	subs, err := dag.CreateSubTasks(parent, plans)
	if err != nil {
		t.Fatalf("CreateSubTasks: %v", err)
	}

	// Complete first sub-task
	st1 := scheduler.GetTask(subs[0].ID)
	if st1 != nil {
		_ = st1.Transition(reef.TaskAssigned)
		_ = st1.Transition(reef.TaskRunning)
	}
	scheduler.HandleTaskCompleted(subs[0].ID, &reef.TaskResult{Text: "done1"})

	allDone, _ := dag.OnSubTaskCompleted(subs[0].ID)
	if allDone {
		t.Error("first completion should not trigger allDone")
	}

	// Complete second
	st2 := scheduler.GetTask(subs[1].ID)
	if st2 != nil {
		_ = st2.Transition(reef.TaskAssigned)
		_ = st2.Transition(reef.TaskRunning)
	}
	scheduler.HandleTaskCompleted(subs[1].ID, &reef.TaskResult{Text: "done2"})

	allDone, err = dag.OnSubTaskCompleted(subs[1].ID)
	if err != nil {
		t.Fatalf("OnSubTaskCompleted: %v", err)
	}
	if !allDone {
		t.Error("second completion should trigger allDone")
	}

	parentTask := scheduler.GetTask("parent-4")
	if parentTask == nil {
		t.Fatal("parent task not found")
	}
	if parentTask.Status != reef.TaskAggregating {
		t.Errorf("parent should be Aggregating, got %s", parentTask.Status)
	}

	msg, err := dag.BuildAggregationMessage("parent-4")
	if err != nil {
		t.Fatalf("BuildAggregationMessage: %v", err)
	}
	if msg == "" || len(msg) < 20 {
		t.Errorf("aggregation message should have content, got: %q", msg)
	}
}
