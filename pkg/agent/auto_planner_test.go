// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent tests for TaskPlanner instruction parsing and DAG management.
// TDD Phase — written before TaskPlanner implementation changes.

package agent

import (
	"context"
	"testing"
)

// ============================================================================
// Test: NewTaskPlanner creates valid planner
// ============================================================================

func TestNewTaskPlanner(t *testing.T) {
	tp := NewTaskPlanner()
	if tp == nil {
		t.Fatal("NewTaskPlanner returned nil")
	}
}

// ============================================================================
// Test: Parse empty instruction
// ============================================================================

func TestParseEmptyInstruction(t *testing.T) {
	tp := NewTaskPlanner()
	_, err := tp.Parse(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty instruction")
	}
	if err.Error() != "empty instruction" {
		t.Errorf("error = %q, want 'empty instruction'", err.Error())
	}
}

// ============================================================================
// Test: Single instruction → single task
// ============================================================================

func TestParseSingleInstruction(t *testing.T) {
	tp := NewTaskPlanner()
	plan, err := tp.Parse(context.Background(), "编译项目")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if plan == nil {
		t.Fatal("plan is nil")
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("Tasks = %d, want 1", len(plan.Tasks))
	}
	task := plan.Tasks[0]
	if task.Instruction != "编译项目" {
		t.Errorf("Instruction = %q, want %q", task.Instruction, "编译项目")
	}
	if task.Status != NodeReady {
		t.Errorf("Status = %v, want NodeReady", task.Status)
	}
	if task.ID == "" {
		t.Error("task.ID should not be empty")
	}
}

// ============================================================================
// Test: Single instruction — unique task IDs across calls
// ============================================================================

func TestParseUniqueTaskIDs(t *testing.T) {
	tp := NewTaskPlanner()
	plan1, _ := tp.Parse(context.Background(), "编译项目")
	plan2, _ := tp.Parse(context.Background(), "编译项目")
	if plan1.Tasks[0].ID == plan2.Tasks[0].ID {
		t.Error("task IDs should be unique across Parse() calls")
	}
}

// ============================================================================
// Test: Parallel split — "并"
// ============================================================================

func TestParseParallelSplit(t *testing.T) {
	tp := NewTaskPlanner()
	plan, err := tp.Parse(context.Background(), "编译项目并部署测试环境")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("Tasks = %d, want 2 (parallel)", len(plan.Tasks))
	}
	// Both tasks should be NodeReady (no dependencies)
	for i, task := range plan.Tasks {
		if task.Status != NodeReady {
			t.Errorf("task[%d] Status = %v, want NodeReady", i, task.Status)
		}
		if len(task.DependsOn) != 0 {
			t.Errorf("task[%d] DependsOn = %v, want empty", i, task.DependsOn)
		}
	}
}

// ============================================================================
// Test: Parallel split — 3+ segments with "和"
// ============================================================================

func TestParseParallelSplitThreeSegments(t *testing.T) {
	tp := NewTaskPlanner()
	plan, err := tp.Parse(context.Background(), "编译项目和部署和环境检查")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(plan.Tasks) < 2 {
		t.Fatalf("Tasks = %d, want >= 2 (parallel multi-segment)", len(plan.Tasks))
	}
	// All tasks should be NodeReady
	for i, task := range plan.Tasks {
		if task.Status != NodeReady {
			t.Errorf("task[%d] Status = %v, want NodeReady", i, task.Status)
		}
	}
}

// ============================================================================
// Test: Parallel split — "同时"
// ============================================================================

func TestParseParallelSplitTongshi(t *testing.T) {
	tp := NewTaskPlanner()
	plan, err := tp.Parse(context.Background(), "同时监控CPU和内存使用")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("Tasks = %d, want 2", len(plan.Tasks))
	}
}

// ============================================================================
// Test: Serial split — "然后"
// ============================================================================

func TestParseSerialSplit(t *testing.T) {
	tp := NewTaskPlanner()
	plan, err := tp.Parse(context.Background(), "编译项目然后部署测试环境")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("Tasks = %d, want 2 (serial)", len(plan.Tasks))
	}

	// First task: ready, no dependencies
	first := plan.Tasks[0]
	if first.Status != NodeReady {
		t.Errorf("first task Status = %v, want NodeReady", first.Status)
	}
	if len(first.DependsOn) != 0 {
		t.Errorf("first task DependsOn = %v, want empty", first.DependsOn)
	}

	// Second task: pending, depends on first
	second := plan.Tasks[1]
	if second.Status != NodePending {
		t.Errorf("second task Status = %v, want NodePending", second.Status)
	}
	if len(second.DependsOn) != 1 {
		t.Fatalf("second task DependsOn = %v, want [firstID]", second.DependsOn)
	}
}

// ============================================================================
// Test: Serial split "先...然后...再" — 3 steps
// ============================================================================

func TestParseSerialSplitThreeSteps(t *testing.T) {
	tp := NewTaskPlanner()
	plan, err := tp.Parse(context.Background(), "先编译项目然后运行测试再部署生产")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(plan.Tasks) < 3 {
		t.Fatalf("Tasks = %d, want >= 3 (serial 3-step)", len(plan.Tasks))
	}

	// Verify DAG chain: task0 → task1 → task2 → ...
	for i := 0; i < len(plan.Tasks); i++ {
		if i == 0 {
			// First task: ready, no dependencies
			if plan.Tasks[i].Status != NodeReady {
				t.Errorf("task[%d] Status = %v, want NodeReady", i, plan.Tasks[i].Status)
			}
			if len(plan.Tasks[i].DependsOn) != 0 {
				t.Errorf("task[%d] DependsOn = %v, want empty", i, plan.Tasks[i].DependsOn)
			}
		} else {
			// Subsequent tasks: pending, depends on previous
			if plan.Tasks[i].Status != NodePending {
				t.Errorf("task[%d] Status = %v, want NodePending", i, plan.Tasks[i].Status)
			}
			if len(plan.Tasks[i].DependsOn) != 1 {
				t.Errorf("task[%d] DependsOn = %v, want 1 dep", i, plan.Tasks[i].DependsOn)
			}
			if plan.Tasks[i].DependsOn[0] != plan.Tasks[i-1].ID {
				t.Errorf("task[%d] depends on %s, want %s", i, plan.Tasks[i].DependsOn[0], plan.Tasks[i-1].ID)
			}
		}
	}
}

// ============================================================================
// Test: Serial split — "之后"
// ============================================================================

func TestParseSerialSplitZhihou(t *testing.T) {
	tp := NewTaskPlanner()
	plan, err := tp.Parse(context.Background(), "分析数据之后输出报告")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("Tasks = %d, want 2", len(plan.Tasks))
	}
	if plan.Tasks[1].Status != NodePending {
		t.Errorf("second task Status = %v, want NodePending", plan.Tasks[1].Status)
	}
}

// ============================================================================
// Test: InferRoleSkills
// ============================================================================

func TestInferRoleSkills(t *testing.T) {
	tp := NewTaskPlanner()

	tests := []struct {
		instruction string
		wantRole    string
		wantSkills  []string
	}{
		{"编译项目", "coder", []string{"go", "docker"}},
		{"构建服务镜像", "coder", []string{"go", "docker"}},
		{"部署到k8s集群", "ops", []string{"docker", "k8s"}},
		{"发布新版本", "ops", []string{"docker", "k8s"}},
		{"测试登录功能", "executor", nil},
		{"检查数据库连接", "executor", nil},
		{"随便聊聊", "executor", nil},
	}

	for _, tt := range tests {
		role, skills := tp.InferRoleSkills(tt.instruction)
		if role != tt.wantRole {
			t.Errorf("InferRoleSkills(%q) role = %q, want %q", tt.instruction, role, tt.wantRole)
		}
		if len(skills) != len(tt.wantSkills) {
			t.Errorf("InferRoleSkills(%q) skills = %v, want %v", tt.instruction, skills, tt.wantSkills)
		} else {
			for i, s := range skills {
				if s != tt.wantSkills[i] {
					t.Errorf("InferRoleSkills(%q) skills[%d] = %q, want %q", tt.instruction, i, s, tt.wantSkills[i])
				}
			}
		}
	}
}

// ============================================================================
// Test: DAG — cycle detection (no cycle)
// ============================================================================

func TestDetectCyclesNoCycle(t *testing.T) {
	tp := NewTaskPlanner()
	tasks := []*PlannedTask{
		{ID: "a", DependsOn: nil},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}
	err := tp.DetectCycles(tasks)
	if err != nil {
		t.Errorf("DetectCycles returned error for acyclic graph: %v", err)
	}
}

// ============================================================================
// Test: DAG — cycle detection (has cycle)
// ============================================================================

func TestDetectCyclesHasCycle(t *testing.T) {
	tp := NewTaskPlanner()
	tasks := []*PlannedTask{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"c"}},
		{ID: "c", DependsOn: []string{"a"}},
	}
	err := tp.DetectCycles(tasks)
	if err == nil {
		t.Fatal("DetectCycles should detect a-b-c-a cycle")
	}
	t.Logf("Cycle correctly detected: %v", err)
}

// ============================================================================
// Test: DAG — self-loop
// ============================================================================

func TestDetectCyclesSelfLoop(t *testing.T) {
	tp := NewTaskPlanner()
	tasks := []*PlannedTask{
		{ID: "a", DependsOn: []string{"a"}},
	}
	err := tp.DetectCycles(tasks)
	if err == nil {
		t.Fatal("DetectCycles should detect self-loop")
	}
	t.Logf("Self-loop correctly detected: %v", err)
}

// ============================================================================
// Test: TopologicalSort
// ============================================================================

func TestTopologicalSort(t *testing.T) {
	tp := NewTaskPlanner()
	tasks := []*PlannedTask{
		{ID: "a", DependsOn: nil},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"a", "b"}},
	}
	sorted := tp.TopologicalSort(tasks)
	if len(sorted) != 3 {
		t.Fatalf("sorted length = %d, want 3", len(sorted))
	}
	// a must come before b, b must come before c
	positions := make(map[string]int)
	for i, t := range sorted {
		positions[t.ID] = i
	}
	if positions["a"] > positions["b"] {
		t.Errorf("a should come before b in topological sort")
	}
	if positions["b"] > positions["c"] {
		t.Errorf("b should come before c in topological sort")
	}
}

// ============================================================================
// Test: PlanDAG builds correct dependencies
// ============================================================================

func TestPlanDAG(t *testing.T) {
	tp := NewTaskPlanner()
	tasks := []*PlannedTask{
		{ID: "first", Instruction: "编译", DependsOn: nil},
		{ID: "second", Instruction: "测试", DependsOn: []string{"first"}},
		{ID: "third", Instruction: "部署", DependsOn: []string{"second"}},
	}
	plan, err := tp.PlanDAG(tasks)
	if err != nil {
		t.Fatalf("PlanDAG error = %v", err)
	}
	if plan.Status != PlanActive {
		t.Errorf("Plan status = %v, want PlanActive", plan.Status)
	}

	// Check dependency resolution
	// first: ready (0 deps)
	if plan.Tasks[0].UnresolvedDeps != 0 {
		t.Errorf("first.UnresolvedDeps = %d, want 0", plan.Tasks[0].UnresolvedDeps)
	}
	if plan.Tasks[0].Status != NodeReady {
		t.Errorf("first.Status = %v, want NodeReady", plan.Tasks[0].Status)
	}
	// second: 1 unresolved dep
	if plan.Tasks[1].UnresolvedDeps != 1 {
		t.Errorf("second.UnresolvedDeps = %d, want 1", plan.Tasks[1].UnresolvedDeps)
	}
	if plan.Tasks[1].Status != NodePending {
		t.Errorf("second.Status = %v, want NodePending", plan.Tasks[1].Status)
	}
	// Check dependents reverse-index
	if len(plan.Tasks[0].Dependents) != 1 || plan.Tasks[0].Dependents[0] != "second" {
		t.Errorf("first.Dependents = %v, want [second]", plan.Tasks[0].Dependents)
	}
}

// ============================================================================
// Test: PlanDAG with cycle (should error)
// ============================================================================

func TestPlanDAGWithCycle(t *testing.T) {
	tp := NewTaskPlanner()
	tasks := []*PlannedTask{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}
	_, err := tp.PlanDAG(tasks)
	if err == nil {
		t.Fatal("PlanDAG should error on cycle")
	}
}

// ============================================================================
// Test: PlanDAG empty tasks
// ============================================================================

func TestPlanDAGEmpty(t *testing.T) {
	tp := NewTaskPlanner()
	_, err := tp.PlanDAG(nil)
	if err == nil {
		t.Fatal("PlanDAG should error on nil tasks")
	}
	_, err = tp.PlanDAG([]*PlannedTask{})
	if err == nil {
		t.Fatal("PlanDAG should error on empty tasks")
	}
}
