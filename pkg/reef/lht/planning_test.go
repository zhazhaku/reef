package lht

import (
	"strings"
	"testing"
	"time"
)

// --- ClassifyGoal ---

func TestClassifyGoalProject(t *testing.T) {
	projectDescs := []string{
		"搭建一个电商平台",
		"新建项目：微服务架构系统",
		"从零构建应用",
		"系统设计：用户中心",
	}

	for _, desc := range projectDescs {
		cat := ClassifyGoal(desc)
		if cat != CategoryProject {
			t.Errorf("ClassifyGoal(%q) = %s, want %s", desc, cat, CategoryProject)
		}
	}
}

func TestClassifyGoalCode(t *testing.T) {
	codeDescs := []string{
		"实现用户登录功能",
		"修复登录页面的 bug",
		"重构订单模块",
		"添加 API 接口",
		"功能开发：支付模块",
	}

	for _, desc := range codeDescs {
		cat := ClassifyGoal(desc)
		if cat != CategoryCode {
			t.Errorf("ClassifyGoal(%q) = %s, want %s", desc, cat, CategoryCode)
		}
	}
}

func TestClassifyGoalResearch(t *testing.T) {
	researchDescs := []string{
		"调研微服务框架选型",
		"分析系统性能瓶颈",
		"评估技术方案可行性",
		"对比 gRPC vs REST",
	}

	for _, desc := range researchDescs {
		cat := ClassifyGoal(desc)
		if cat != CategoryResearch {
			t.Errorf("ClassifyGoal(%q) = %s, want %s", desc, cat, CategoryResearch)
		}
	}
}

func TestClassifyGoalGeneral(t *testing.T) {
	generalDescs := []string{
		"帮我做点事",
		"处理数据",
		"生成报告",
	}

	for _, desc := range generalDescs {
		cat := ClassifyGoal(desc)
		if cat != CategoryGeneral {
			t.Errorf("ClassifyGoal(%q) = %s, want %s", desc, cat, CategoryGeneral)
		}
	}
}

// --- RouteForCategory ---

func TestRouteForCategoryProject(t *testing.T) {
	route := RouteForCategory(CategoryProject)
	if route.Workflow != "gsd" {
		t.Errorf("workflow: got %s, want gsd", route.Workflow)
	}
	if len(route.Phases) != 6 {
		t.Errorf("gsd should have 6 phases, got %d", len(route.Phases))
	}
	expectedFirst := "initialize"
	if route.Phases[0] != expectedFirst {
		t.Errorf("first phase: got %s, want %s", route.Phases[0], expectedFirst)
	}
}

func TestRouteForCategoryCode(t *testing.T) {
	route := RouteForCategory(CategoryCode)
	if route.Workflow != "openspec" {
		t.Errorf("workflow: got %s, want openspec", route.Workflow)
	}
	if len(route.Phases) != 5 {
		t.Errorf("openspec should have 5 phases, got %d", len(route.Phases))
	}
}

func TestRouteForCategoryResearch(t *testing.T) {
	route := RouteForCategory(CategoryResearch)
	if route.Workflow != "research" {
		t.Errorf("workflow: got %s, want research", route.Workflow)
	}
}

func TestRouteForCategoryGeneral(t *testing.T) {
	route := RouteForCategory(CategoryGeneral)
	if route.Workflow != "plan-and-execute" {
		t.Errorf("workflow: got %s, want plan-and-execute", route.Workflow)
	}
}

// --- GenerateCapabilities ---

func TestGenerateCapabilitiesProject(t *testing.T) {
	caps := GenerateCapabilities(CategoryProject)
	if len(caps) == 0 {
		t.Error("capabilities should not be empty for project")
	}
	// Should include gsd skill
	hasGSD := false
	for _, c := range caps {
		if strings.Contains(c, "gsd") {
			hasGSD = true
		}
	}
	if !hasGSD {
		t.Error("project capabilities should include gsd skill")
	}
}

func TestGenerateCapabilitiesCode(t *testing.T) {
	caps := GenerateCapabilities(CategoryCode)
	hasOpenspec := false
	for _, c := range caps {
		if strings.Contains(c, "openspec") {
			hasOpenspec = true
		}
	}
	if !hasOpenspec {
		t.Error("code capabilities should include openspec skill")
	}
}

func TestGenerateCapabilitiesResearch(t *testing.T) {
	caps := GenerateCapabilities(CategoryResearch)
	// Research should not have eval/rev
	for _, c := range caps {
		if strings.Contains(c, "lht-eval") || strings.Contains(c, "lht-rev") {
			t.Errorf("research capabilities should not include eval/rev: %s", c)
		}
	}
}

// --- BuildPlan ---

func TestBuildPlanDAGStructure(t *testing.T) {
	goal := Goal{
		GoalID:      "goal-001",
		Description: "实现用户登录模块",
		State:       StatePlanning,
		CreatedAt:   time.Now(),
	}
	goalDef := GoalDefinition{
		GoalID:             "goal-001",
		Description:        "实现用户登录模块",
		Granularity:        "feature",
		Scope:              "用户名密码登录 + JWT",
		Constraints:        []string{"Go + PostgreSQL"},
		AcceptanceCriteria: []string{"集成测试通过", "登录耗时 <1s"},
		Priorities:         []string{"P0: 基本登录"},
	}

	plan := BuildPlan(goal, goalDef, CategoryCode, "v1")

	if plan.GoalID != "goal-001" {
		t.Errorf("Plan GoalID: got %q, want %q", plan.GoalID, "goal-001")
	}
	if plan.PlanVersion != "v1" {
		t.Errorf("PlanVersion: got %q, want %q", plan.PlanVersion, "v1")
	}
	if len(plan.Tasks) != 5 {
		t.Errorf("openspec should generate 5 tasks, got %d", len(plan.Tasks))
	}
	if len(plan.Capabilities) == 0 {
		t.Error("plan should have capabilities")
	}

	// First task should have no dependencies
	if len(plan.Tasks[0].Dependencies) != 0 {
		t.Error("first task should have no dependencies")
	}

	// Each subsequent task should depend on the previous
	for i := 1; i < len(plan.Tasks); i++ {
		if len(plan.Tasks[i].Dependencies) == 0 {
			t.Errorf("task %d (%s) should have at least one dependency", i, plan.Tasks[i].TaskID)
		}
	}
}

func TestBuildPlanProjectDAG(t *testing.T) {
	goal := Goal{GoalID: "goal-proj", Description: "build a platform"}
	goalDef := GoalDefinition{GoalID: "goal-proj", Description: "build a platform"}

	plan := BuildPlan(goal, goalDef, CategoryProject, "v1")

	if len(plan.Tasks) != 6 {
		t.Errorf("gsd should generate 6 tasks, got %d", len(plan.Tasks))
	}

	// Verify phases order
	expectedPhases := []string{"initialize", "research", "roadmap", "plan", "execute", "verify"}
	for i, phase := range expectedPhases {
		if plan.Tasks[i].TaskID != "goal-proj/"+phase {
			t.Errorf("task %d: got %s, want %s", i, plan.Tasks[i].TaskID, "goal-proj/"+phase)
		}
	}
}

// --- ValidateDAG ---

func TestValidateDAGValid(t *testing.T) {
	plan := Plan{
		GoalID:      "g1",
		PlanVersion: "v1",
		Tasks: []TaskNode{
			{TaskID: "g1/a", Dependencies: nil},
			{TaskID: "g1/b", Dependencies: []string{"g1/a"}},
			{TaskID: "g1/c", Dependencies: []string{"g1/a"}},
			{TaskID: "g1/d", Dependencies: []string{"g1/b", "g1/c"}},
		},
	}

	if err := ValidateDAG(plan); err != nil {
		t.Errorf("valid DAG should pass: %v", err)
	}
}

func TestValidateDAGDuplicateID(t *testing.T) {
	plan := Plan{
		GoalID: "g1",
		Tasks: []TaskNode{
			{TaskID: "g1/a"},
			{TaskID: "g1/a"},
		},
	}
	if err := ValidateDAG(plan); err == nil {
		t.Error("duplicate task IDs should fail validation")
	}
}

func TestValidateDAGMissingDependency(t *testing.T) {
	plan := Plan{
		GoalID: "g1",
		Tasks: []TaskNode{
			{TaskID: "g1/a", Dependencies: []string{"g1/zzz"}},
		},
	}
	if err := ValidateDAG(plan); err == nil {
		t.Error("missing dependency should fail validation")
	}
}

func TestValidateDAGCycle(t *testing.T) {
	plan := Plan{
		GoalID: "g1",
		Tasks: []TaskNode{
			{TaskID: "g1/a", Dependencies: []string{"g1/b"}},
			{TaskID: "g1/b", Dependencies: []string{"g1/a"}},
		},
	}
	if err := ValidateDAG(plan); err == nil {
		t.Error("cycle should fail validation")
	}
}

func TestValidateDAGSelfLoop(t *testing.T) {
	plan := Plan{
		GoalID: "g1",
		Tasks: []TaskNode{
			{TaskID: "g1/a", Dependencies: []string{"g1/a"}},
		},
	}
	if err := ValidateDAG(plan); err == nil {
		t.Error("self-loop should fail validation")
	}
}

func TestValidateDAGEmpty(t *testing.T) {
	plan := Plan{GoalID: "g1"}
	if err := ValidateDAG(plan); err != nil {
		t.Errorf("empty DAG should be valid: %v", err)
	}
}

// --- MaxDAGDepth ---

func TestMaxDAGDepthLinear(t *testing.T) {
	plan := Plan{
		Tasks: []TaskNode{
			{TaskID: "t1"},
			{TaskID: "t2", Dependencies: []string{"t1"}},
			{TaskID: "t3", Dependencies: []string{"t2"}},
		},
	}
	d := MaxDAGDepth(plan)
	if d != 3 {
		t.Errorf("linear DAG depth: got %d, want 3", d)
	}
}

func TestMaxDAGDepthFork(t *testing.T) {
	plan := Plan{
		Tasks: []TaskNode{
			{TaskID: "t1"},
			{TaskID: "t2", Dependencies: []string{"t1"}},
			{TaskID: "t3", Dependencies: []string{"t1"}},
			{TaskID: "t4", Dependencies: []string{"t2", "t3"}},
		},
	}
	d := MaxDAGDepth(plan)
	if d != 3 {
		t.Errorf("fork DAG depth: got %d, want 3", d)
	}
}

func TestMaxDAGDepthEmpty(t *testing.T) {
	plan := Plan{}
	d := MaxDAGDepth(plan)
	if d != 0 {
		t.Errorf("empty DAG depth: got %d, want 0", d)
	}
}

func TestMaxDAGDepthSingleNode(t *testing.T) {
	plan := Plan{
		Tasks: []TaskNode{
			{TaskID: "t1"},
		},
	}
	d := MaxDAGDepth(plan)
	if d != 1 {
		t.Errorf("single node depth: got %d, want 1", d)
	}
}
