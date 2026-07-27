// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent provides the TaskPlanner, which parses user instructions
// into ExecutionPlans with DAG dependency resolution.
//
// Client 3 (Phase 3) — Instruction parsing + DAG orchestration engine.

package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ============================================================================
// TaskPlanner — instruction parsing and DAG management
// ============================================================================

// TaskPlanner parses user instructions and splits them into DAG-structured
// ExecutionPlans.
//
// Parsing strategy (rule-based, with optional LLM assistance in Phase 3+):
//   1. Single instruction → single-task plan
//   2. Parallel keywords ("并"/"和"/"同时") → parallel subtasks
//   3. Sequential keywords ("先...再..."/"然后"/"之后") → DAG dependency chain
//   4. LLM-assisted parsing (optional, for complex instructions)
type TaskPlanner struct {
	// llmProvider is an optional LLM provider for intelligent instruction
	// splitting (Phase 3+). When nil, only rule-based parsing is used.
	// llmProvider providers.Provider
}

// NewTaskPlanner creates a new TaskPlanner. LLM provider injection is
// deferred to a later phase.
func NewTaskPlanner() *TaskPlanner {
	return &TaskPlanner{}
}

// nextTaskID returns a unique task identifier based on the current
// nanosecond timestamp and an optional sequential index. This avoids
// collisions across multiple Parse() calls.
func nextTaskID(seq int) string {
	ts := time.Now().UnixNano()
	return fmt.Sprintf("task-%d-%d", ts, seq)
}

// ============================================================================
// Parse — main entry point
// ============================================================================

// Parse analyzes an instruction string and produces an ExecutionPlan.
// The plan may contain a single task or a DAG of dependent tasks,
// depending on the instruction complexity.
func (tp *TaskPlanner) Parse(ctx context.Context, instruction string) (*ExecutionPlan, error) {
	if strings.TrimSpace(instruction) == "" {
		return nil, fmt.Errorf("empty instruction")
	}

	// Rule-based parsing
	tasks, err := tp.parseInstruction(instruction)
	if err != nil {
		return nil, fmt.Errorf("parse instruction: %w", err)
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks parsed from instruction")
	}

	// Build DAG
	plan, err := tp.PlanDAG(tasks)
	if err != nil {
		return nil, fmt.Errorf("plan DAG: %w", err)
	}

	return plan, nil
}

// ============================================================================
// Rule-based instruction splitting
// ============================================================================

// parseInstruction splits an instruction into PlannedTasks using keyword
// heuristics.
func (tp *TaskPlanner) parseInstruction(instruction string) ([]*PlannedTask, error) {
	// Strategy 1: Check for sequential keywords → serial DAG
	if serial := tp.trySerialSplit(instruction); len(serial) > 1 {
		return serial, nil
	}

	// Strategy 2: Check for parallel keywords → concurrent tasks
	if parallel := tp.tryParallelSplit(instruction); len(parallel) > 1 {
		return parallel, nil
	}

	// Strategy 3: Single task
	role, skills := tp.InferRoleSkills(instruction)
	task := &PlannedTask{
		ID:             nextTaskID(0),
		Instruction:    strings.TrimSpace(instruction),
		RequiredRole:   role,
		RequiredSkills: skills,
		Status:         NodeReady, // single task is immediately ready
		MaxRetries:     3,
	}
	return []*PlannedTask{task}, nil
}

// trySerialSplit detects sequential keywords and splits the instruction
// into ordered tasks with DAG dependencies. Supports 3+ steps.
//
// Keywords: "先...再...", "首先...然后...", "然后", "之后", "接着", "再"
// Example: "先编译，然后测试，之后部署" → 3 tasks: compile → test → deploy
func (tp *TaskPlanner) trySerialSplit(instruction string) []*PlannedTask {
	serialMarkers := []string{"然后", "之后", "接着", "再"}
	lower := instruction

	// Collect all split positions and their corresponding markers
	type splitPos struct {
		idx    int
		marker string
	}
	var splits []splitPos

	for _, marker := range serialMarkers {
		searchFrom := 0
		for {
			idx := strings.Index(lower[searchFrom:], marker)
			if idx < 0 {
				break
			}
			absIdx := searchFrom + idx
			// Avoid matching at the very start or very end
			if absIdx > 0 && absIdx < len(lower)-len(marker) {
				splits = append(splits, splitPos{absIdx, marker})
			}
			searchFrom = absIdx + len(marker)
		}
	}

	// Sort splits by position (ascending)
	for i := 0; i < len(splits); i++ {
		for j := i + 1; j < len(splits); j++ {
			if splits[j].idx < splits[i].idx {
				splits[i], splits[j] = splits[j], splits[i]
			}
		}
	}

	if len(splits) < 1 {
		return nil
	}

	// Split into segments
	segments := make([]string, 0, len(splits)+1)
	prevEnd := 0
	for _, sp := range splits {
		seg := strings.TrimSpace(instruction[prevEnd:sp.idx])
		seg = strings.TrimLeft(seg, "先首先，, ")
		seg = strings.TrimSpace(seg)
		if seg != "" {
			segments = append(segments, seg)
		}
		prevEnd = sp.idx + len(sp.marker)
	}
	// Last segment after the final split
	lastSeg := strings.TrimSpace(instruction[prevEnd:])
	lastSeg = strings.TrimLeft(lastSeg, "，, ")
	lastSeg = strings.TrimSpace(lastSeg)
	if lastSeg != "" {
		segments = append(segments, lastSeg)
	}

	if len(segments) < 2 {
		return nil
	}

	// Build DAG: each task depends on the previous one
	tasks := make([]*PlannedTask, 0, len(segments))
	for i, seg := range segments {
		role, skills := tp.InferRoleSkills(seg)
		task := &PlannedTask{
			ID:             nextTaskID(i),
			Instruction:    seg,
			RequiredRole:   role,
			RequiredSkills: skills,
			Status:         NodeReady,
			MaxRetries:     3,
		}
		if i > 0 {
			task.DependsOn = []string{tasks[i-1].ID}
			task.UnresolvedDeps = 1
			task.Status = NodePending
		}
		if i < len(segments)-1 {
			task.Dependents = []string{nextTaskID(i + 1)}
		}
		tasks = append(tasks, task)
	}
	return tasks
}

// tryParallelSplit detects parallel keywords and splits the instruction
// into independent concurrent tasks.
//
// Keywords: "并", "和", "同时", "以及"
func (tp *TaskPlanner) tryParallelSplit(instruction string) []*PlannedTask {
	parallelMarkers := []string{"同时", "以及", "并", "和"}
	lower := instruction

	for _, marker := range parallelMarkers {
		// Split on ALL occurrences of this marker for 3+ segments
		segments := strings.Split(lower, marker)
		if len(segments) < 2 {
			continue
		}

		tasks := make([]*PlannedTask, 0, len(segments))
		for i, seg := range segments {
			seg = strings.TrimSpace(seg)
			seg = strings.TrimLeft(seg, "，, ")
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			role, skills := tp.InferRoleSkills(seg)
			task := &PlannedTask{
				ID:             nextTaskID(i),
				Instruction:    seg,
				RequiredRole:   role,
				RequiredSkills: skills,
				Status:         NodeReady,
				MaxRetries:     3,
			}
			tasks = append(tasks, task)
		}
		if len(tasks) >= 2 {
			return tasks
		}
	}
	return nil
}

// ============================================================================
// DAG Management
// ============================================================================

// PlanDAG takes a list of PlannedTasks with optional DependsOn fields
// and builds a complete ExecutionPlan.
//
// It performs:
//  1. Cycle detection via Kahn's algorithm
//  2. Topological ordering for correct dependency resolution
//  3. Dependents reverse-index population
//  4. Status initialization (ready vs pending)
func (tp *TaskPlanner) PlanDAG(tasks []*PlannedTask) (*ExecutionPlan, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks provided")
	}

	// 1. Cycle detection
	if err := tp.DetectCycles(tasks); err != nil {
		return nil, fmt.Errorf("cyclic dependency detected: %w", err)
	}

	// 2. Build reverse dependency index (dependents)
	tp.buildDependents(tasks)

	// 3. Initialize statuses based on unresolved deps
	for _, t := range tasks {
		t.UnresolvedDeps = len(t.DependsOn)
		if t.UnresolvedDeps == 0 {
			t.Status = NodeReady
		} else {
			t.Status = NodePending
		}
	}

	plan := &ExecutionPlan{
		ID:        fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		Tasks:     tasks,
		CreatedAt: time.Now(),
		Status:    PlanActive,
	}
	return plan, nil
}

// buildDependents populates the Dependents field for each task by
// reversing the DependsOn edges.
func (tp *TaskPlanner) buildDependents(tasks []*PlannedTask) {
	// Build a reverse lookup: taskID → PlannedTask
	byID := make(map[string]*PlannedTask, len(tasks))
	for _, t := range tasks {
		byID[t.ID] = t
	}

	// For each task, for each dependency, add this task as a dependent
	for _, t := range tasks {
		for _, depID := range t.DependsOn {
			if dep, ok := byID[depID]; ok {
				dep.Dependents = append(dep.Dependents, t.ID)
			}
		}
	}
}

// ============================================================================
// Kahn's Algorithm — cycle detection and topological sort
// ============================================================================

// DetectCycles uses Kahn's algorithm (BFS indegree) to detect cycles in
// the task DAG.
//
// Algorithm:
//  1. indegree[i] = len(tasks[i].DependsOn) — number of prerequisites
//  2. Build reverse lookup: taskID -> indices of tasks that depend on it
//  3. Enqueue all nodes with indegree 0.
//  4. Process queue: for each dequeued node, decrement indegree of all
//     its dependents. When indegree reaches 0, enqueue.
//  5. If processed nodes != total nodes, a cycle exists.
//
// Returns nil if the DAG is acyclic, or an error listing the nodes in cycles.
func (tp *TaskPlanner) DetectCycles(tasks []*PlannedTask) error {
	if len(tasks) == 0 {
		return nil
	}

	// Map task IDs to indices
	idToIdx := make(map[string]int, len(tasks))
	// Reverse lookup: depID -> indices of tasks that depend on it
	reverseDeps := make(map[string][]int)

	// indegree[i] = number of prerequisites for tasks[i]
	indegree := make([]int, len(tasks))
	for i, t := range tasks {
		idToIdx[t.ID] = i
		indegree[i] = len(t.DependsOn)
		for _, depID := range t.DependsOn {
			reverseDeps[depID] = append(reverseDeps[depID], i)
		}
	}

	// BFS queue: nodes with indegree 0 (no prerequisites)
	var queue []int
	for i, deg := range indegree {
		if deg == 0 {
			queue = append(queue, i)
		}
	}

	processed := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		processed++

		// Decrement indegree of tasks that depend on this node
		for _, depIdx := range reverseDeps[tasks[node].ID] {
			indegree[depIdx]--
			if indegree[depIdx] == 0 {
				queue = append(queue, depIdx)
			}
		}
	}

	if processed != len(tasks) {
		var remaining []string
		for i, deg := range indegree {
			if deg > 0 {
				remaining = append(remaining, tasks[i].ID)
			}
		}
		return fmt.Errorf("cycle involving tasks: %v", remaining)
	}

	return nil
}

// TopologicalSort returns tasks in topological order using Kahn's
// algorithm (BFS indegree).
//
// Tasks with indegree 0 (no prerequisites) come first.
// The order respects all dependency constraints.
func (tp *TaskPlanner) TopologicalSort(tasks []*PlannedTask) []*PlannedTask {
	if len(tasks) <= 1 {
		return tasks
	}

	// Same as DetectCycles: compute indegree + reverse lookup
	idToIdx := make(map[string]int, len(tasks))
	reverseDeps := make(map[string][]int)

	indegree := make([]int, len(tasks))
	for i, t := range tasks {
		idToIdx[t.ID] = i
		indegree[i] = len(t.DependsOn)
		for _, depID := range t.DependsOn {
			reverseDeps[depID] = append(reverseDeps[depID], i)
		}
	}

	// BFS queue: nodes with indegree 0
	var queue []int
	for i, deg := range indegree {
		if deg == 0 {
			queue = append(queue, i)
		}
	}

	result := make([]*PlannedTask, 0, len(tasks))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, tasks[node])

		// Decrement indegree of dependents
		for _, depIdx := range reverseDeps[tasks[node].ID] {
			indegree[depIdx]--
			if indegree[depIdx] == 0 {
				queue = append(queue, depIdx)
			}
		}
	}

	return result
}

// ============================================================================
// DAG Dependency Unlocking (checkDAG)
// ============================================================================

// CheckDAG scans all Pending tasks and updates their status based on
// the completion state of their dependencies.
//
// Rules:
//   - All dependencies Done → Status becomes Ready
//   - Any dependency Failed → Status becomes Blocked
//   - Otherwise → Status remains Pending
//
// Returns the number of tasks that became Ready and the number that
// became Blocked.
func CheckDAG(tasks []*PlannedTask) (readyCount int, blockedCount int) {
	for _, t := range tasks {
		if t.Status != NodePending {
			continue
		}

		// Count done vs failed dependencies
		doneCount := 0
		failedCount := 0

		for _, depID := range t.DependsOn {
			dep := findTaskByID(tasks, depID)
			if dep == nil {
				continue
			}
			switch dep.Status {
			case NodeDone:
				doneCount++
			case NodeFailed:
				failedCount++
			}
		}

		if failedCount > 0 {
			t.MarkBlocked(fmt.Sprintf("dependency failed: %d/%d failed", failedCount, len(t.DependsOn)))
			blockedCount++
		} else if doneCount == len(t.DependsOn) {
			// All dependencies satisfied
			t.UnresolvedDeps = 0
			t.Status = NodeReady
			readyCount++
		}
	}
	return
}

// findTaskByID looks up a PlannedTask by its ID.
func findTaskByID(tasks []*PlannedTask, id string) *PlannedTask {
	for _, t := range tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// ============================================================================
// Role / Skill Inference
// ============================================================================

// InferRoleSkills infers the required role and skills for an instruction
// using simple keyword matching rules.
//
// Rules:
//   - "编译"/"构建"/"build"/"编译并"   → role=coder, skills=["go","docker"]
//   - "部署"/"发布"/"deploy"/"上线"   → role=ops,   skills=["docker","k8s"]
//   - "测试"/"检查"/"查询"/"验证"/"test" → role=executor
//   - Default                           → role=executor, skills=nil
func (tp *TaskPlanner) InferRoleSkills(instruction string) (role string, skills []string) {
	lower := strings.ToLower(instruction)

	// Build / Compile
	if containsAny(lower, "编译", "构建", "build", "编译并") {
		return "coder", []string{"go", "docker"}
	}

	// Deploy / Release
	if containsAny(lower, "部署", "发布", "deploy", "上线") {
		return "ops", []string{"docker", "k8s"}
	}

	// Test / Check / Query
	if containsAny(lower, "测试", "检查", "查询", "验证", "test") {
		return "executor", nil
	}

	// Default
	return "executor", nil
}

// ============================================================================
// TaskPlannerInterface — contract for later phases
// ============================================================================

// TaskPlannerInterface defines the contract that TaskPlanner satisfies.
type TaskPlannerInterface interface {
	Parse(ctx context.Context, instruction string) (*ExecutionPlan, error)
	InferRoleSkills(instruction string) (role string, skills []string)
}

// Compile-time check: *TaskPlanner implements TaskPlannerInterface.
var _ TaskPlannerInterface = (*TaskPlanner)(nil)

// ============================================================================
// Helpers
// ============================================================================

// containsAny returns true if s contains any of the given substrings.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
