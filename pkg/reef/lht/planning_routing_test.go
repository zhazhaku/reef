package lht

import (
	"strings"
	"testing"
)

// ===================== T5B.2: Planning Routing Tests =====================

func TestPlanningRoutingProjectToGSD(t *testing.T) {
	// Complete/new project should route to gsd workflow.
	descriptions := []string{
		"搭建一个电商平台",
		"从零开始构建应用",
		"新建项目 博客系统",
		"系统设计 微服务架构",
		"初始化项目",
	}

	for _, desc := range descriptions {
		cat := ClassifyGoal(desc)
		if cat != CategoryProject {
			t.Errorf("%q classified as %s, want project→gsd", desc, cat)
		}

		route := RouteForCategory(cat)
		if route.Workflow != "gsd" {
			t.Errorf("route.Workflow for project = %s, want gsd", route.Workflow)
		}

		// gsd phases: initialize, research, roadmap, plan, execute, verify
		expected := []string{"initialize", "research", "roadmap", "plan", "execute", "verify"}
		if len(route.Phases) != len(expected) {
			t.Errorf("gsd phases count = %d, want %d", len(route.Phases), len(expected))
		}
		for i, p := range expected {
			if i < len(route.Phases) && route.Phases[i] != p {
				t.Errorf("gsd phase[%d] = %s, want %s", i, route.Phases[i], p)
			}
		}
	}
}

func TestPlanningRoutingCodeToOpenSpec(t *testing.T) {
	// Code changes/features should route to openspec workflow.
	descriptions := []string{
		"实现用户登录功能",
		"修复登录 bug",
		"重构支付模块",
		"添加新接口 API",
		"优化数据库查询",
		"新增用户管理组件",
	}

	for _, desc := range descriptions {
		cat := ClassifyGoal(desc)
		if cat != CategoryCode {
			t.Errorf("%q classified as %s, want code→openspec", desc, cat)
		}

		route := RouteForCategory(cat)
		if route.Workflow != "openspec" {
			t.Errorf("route.Workflow for code = %s, want openspec", route.Workflow)
		}

		// openspec phases: proposal, specs, design, tasks, implementation
		expected := []string{"proposal", "specs", "design", "tasks", "implementation"}
		if len(route.Phases) != len(expected) {
			t.Errorf("openspec phases count = %d, want %d", len(route.Phases), len(expected))
		}
		for i, p := range expected {
			if i < len(route.Phases) && route.Phases[i] != p {
				t.Errorf("openspec phase[%d] = %s, want %s", i, route.Phases[i], p)
			}
		}
	}
}

func TestPlanningRoutingResearch(t *testing.T) {
	// Research/analysis should route to research workflow.
	descriptions := []string{
		"调研微服务框架选型",
		"分析竞品功能对比",
		"评估技术方案的可行性",
		"行业趋势研究分析",
	}

	for _, desc := range descriptions {
		cat := ClassifyGoal(desc)
		if cat != CategoryResearch {
			t.Errorf("%q classified as %s, want research", desc, cat)
		}

		route := RouteForCategory(cat)
		if route.Workflow != "research" {
			t.Errorf("route.Workflow for research = %s, want research", route.Workflow)
		}
	}
}

func TestPlanningRoutingGeneral(t *testing.T) {
	// Anything not matching specific keywords → general plan-and-execute.
	descriptions := []string{
		"帮我整理一下文件",
		"写一个周报",
		"deploy the latest build",
	}

	for _, desc := range descriptions {
		cat := ClassifyGoal(desc)
		if cat != CategoryGeneral {
			t.Errorf("%q classified as %s, want general", desc, cat)
		}

		route := RouteForCategory(cat)
		if route.Workflow != "plan-and-execute" {
			t.Errorf("route.Workflow for general = %s, want plan-and-execute", route.Workflow)
		}

		expected := []string{"plan", "execute", "verify"}
		if len(route.Phases) != len(expected) {
			t.Errorf("general phases count = %d, want %d", len(route.Phases), len(expected))
		}
	}
}

func TestPlanningRoutingGSDMustHaves(t *testing.T) {
	// GSD project phases should generate must_haves for Evaluator acceptance criteria.
	goalDef := GoalDefinition{
		GoalID:              "g-test-1",
		Description:         "搭建电商平台",
		Granularity:         "完整系统",
		Scope:               "全栈Web应用",
		Constraints:         []string{"Go后端", "React前端"},
		AcceptanceCriteria:  []string{"用户可注册登录", "可浏览商品", "可下单"},
	}

	mustHaves := derivePhaseMustHaves("execute", goalDef)
	if len(mustHaves) == 0 {
		t.Error("gsd execute phase should have must_haves")
	}

	// must_haves should reference the acceptance criteria
	found := false
	for _, mh := range mustHaves {
		if strings.Contains(mh, "用户可注册登录") || strings.Contains(mh, "可浏览商品") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("must_haves should include acceptance criteria: %v", mustHaves)
	}
}

func TestPlanningRoutingOpenSpecMustHaves(t *testing.T) {
	// OpenSpec code phases should generate must_haves for Review.
	goalDef := GoalDefinition{
		GoalID:              "g-test-2",
		Description:         "实现用户登录功能",
		Granularity:         "功能模块",
		Scope:               "auth模块",
		Constraints:         []string{"JWT认证", "bcrypt加密"},
		AcceptanceCriteria:  []string{"登录成功返回token", "密码错误返回401", "token过期需刷新"},
	}

	mustHaves := derivePhaseMustHaves("implementation", goalDef)
	if len(mustHaves) == 0 {
		t.Error("openspec implementation phase should have must_haves")
	}

	for _, ac := range goalDef.AcceptanceCriteria {
		found := false
		for _, mh := range mustHaves {
			if strings.Contains(mh, ac) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("acceptance criteria %q not found in must_haves: %v", ac, mustHaves)
		}
	}
}

func TestPlanningRoutingResearchPhases(t *testing.T) {
	route := RouteForCategory(CategoryResearch)
	expected := []string{"define_scope", "collect_data", "analyze", "report"}

	if len(route.Phases) != len(expected) {
		t.Fatalf("research phases: got %v, want %v", route.Phases, expected)
	}

	for i, p := range expected {
		if route.Phases[i] != p {
			t.Errorf("research phase[%d] = %s, want %s", i, route.Phases[i], p)
		}
	}
}

func TestPlanningRoutingBuildPlanProjectDAG(t *testing.T) {
	// BuildPlan for project should produce a DAG covering gsd phases.
	goal := Goal{
		GoalID:      "g-prj-1",
		Description: "搭建电商平台",
		State:       StatePlanning,
	}

	goalDef := GoalDefinition{
		GoalID:              "g-prj-1",
		Description:         "搭建电商平台",
		Granularity:         "完整系统",
		AcceptanceCriteria:  []string{"可注册登录", "可浏览商品"},
	}

	plan := BuildPlan(goal, goalDef, CategoryProject, "1")

	if len(plan.Tasks) == 0 {
		t.Error("project plan should have tasks")
	}

	// Each gsd phase should map to a task node.
	gsdPhases := []string{"initialize", "research", "roadmap", "plan", "execute", "verify"}
	phaseTasks := make(map[string]bool)
	for _, t := range plan.Tasks {
		for _, p := range gsdPhases {
			if strings.Contains(t.Description, p) {
				phaseTasks[p] = true
			}
		}
	}

	for _, p := range gsdPhases {
		if !phaseTasks[p] {
			t.Errorf("gsd phase %q not represented in plan tasks", p)
		}
	}

	// Plan should have plan_version.
	if plan.PlanVersion != "1" {
		t.Errorf("PlanVersion = %q, want %q", plan.PlanVersion, "1")
	}

	// Plan should include capabilities.
	if len(plan.Capabilities) == 0 {
		t.Error("project plan should include capabilities")
	}

	// Validate DAG.
	if err := ValidateDAG(plan); err != nil {
		t.Errorf("project DAG validation failed: %v", err)
	}
}

func TestPlanningRoutingBuildPlanCodeDAG(t *testing.T) {
	// BuildPlan for code should produce OpenSpec DAG.
	goal := Goal{
		GoalID:      "g-code-1",
		Description: "实现用户登录功能",
		State:       StatePlanning,
	}

	goalDef := GoalDefinition{
		GoalID:              "g-code-1",
		Description:         "实现用户登录功能",
		Granularity:         "功能模块",
		AcceptanceCriteria:  []string{"JWT登录", "密码验证"},
	}

	plan := BuildPlan(goal, goalDef, CategoryCode, "1")

	if len(plan.Tasks) == 0 {
		t.Error("code plan should have tasks")
	}

	// OpenSpec phases should be in tasks.
	osPhases := []string{"proposal", "specs", "design", "tasks", "implementation"}
	phaseTasks := make(map[string]bool)
	for _, t := range plan.Tasks {
		for _, p := range osPhases {
			if strings.Contains(t.Description, p) {
				phaseTasks[p] = true
			}
		}
	}
	for _, p := range osPhases {
		if !phaseTasks[p] {
			t.Errorf("openspec phase %q not represented in plan tasks", p)
		}
	}

	if err := ValidateDAG(plan); err != nil {
		t.Errorf("code DAG validation failed: %v", err)
	}
}

func TestPlanningRoutingCapabilityListInPlan(t *testing.T) {
	// Plans should include a capability list relevant to the category.
	caps := GenerateCapabilities(CategoryProject)
	if len(caps) < 2 {
		t.Error("project capabilities should include at least base + gsd + roles")
	}

	// Should include gsd skill and role clients.
	hasGSD := false
	hasGen := false
	for _, c := range caps {
		if strings.Contains(c, "gsd") {
			hasGSD = true
		}
		if strings.Contains(c, "lht-gen") {
			hasGen = true
		}
	}
	if !hasGSD {
		t.Error("project capabilities should include gsd skill")
	}
	if !hasGen {
		t.Error("project capabilities should include lht-gen role")
	}
}

func TestPlanningRoutingDeriveRubric(t *testing.T) {
	goalDef := GoalDefinition{
		Description:        "实现用户登录",
		AcceptanceCriteria: []string{"JWT返回", "密码验证"},
		Constraints:        []string{"Go语言", "PostgreSQL"},
	}

	rubric := derivePhaseRubric("implementation", goalDef)
	if rubric == "" {
		t.Error("rubric should not be empty")
	}
	if !strings.Contains(rubric, "JWT") {
		t.Error("rubric should reference acceptance criteria")
	}
}
