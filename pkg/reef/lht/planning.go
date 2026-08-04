package lht

import (
	"fmt"
	"strings"
)

// GoalCategory classifies a goal for workflow routing.
type GoalCategory string

const (
	CategoryProject  GoalCategory = "project"  // → gsd
	CategoryCode     GoalCategory = "code"     // → openspec
	CategoryResearch GoalCategory = "research" // → research
	CategoryGeneral  GoalCategory = "general"  // → plan-and-execute
)

// PlanRoute describes the target workflow for a classified goal.
type PlanRoute struct {
	Category     GoalCategory `json:"category"`
	Workflow     string       `json:"workflow"`     // gsd / openspec / research / plan-and-execute
	Phases       []string     `json:"phases"`       // ordered workflow phases
	Description  string       `json:"description"`  // human-readable routing rationale
}

// ClassifyGoal determines the workflow category from the goal description.
// This uses keyword-based heuristics (v1); production should use an LLM classifier.
func ClassifyGoal(description string) GoalCategory {
	lower := strings.ToLower(description)

	// Project keywords: new project/system/platform/app from scratch
	projectKeywords := []string{
		"搭建", "新建项目", "新建工程", "从零", "脚手架", "初始化项目",
		"系统设计", "架构设计", "平台", "完整系统", "构建应用", "create project",
	}
	for _, kw := range projectKeywords {
		if strings.Contains(lower, kw) {
			return CategoryProject
		}
	}

	// Code keywords: code changes, features, fixes
	codeKeywords := []string{
		"实现", "添加功能", "新增", "修复", "bug", "fix", "重构", "优化",
		"接口", "api", "模块", "组件", "feature", "功能开发", "代码",
	}
	for _, kw := range codeKeywords {
		if strings.Contains(lower, kw) {
			return CategoryCode
		}
	}

	// Research keywords: analysis, investigation, study
	researchKeywords := []string{
		"调研", "分析", "研究", "评估", "对比", "选型", "可行性", "report",
		"analysis", "research", "调查", "review", "benchmark",
	}
	for _, kw := range researchKeywords {
		if strings.Contains(lower, kw) {
			return CategoryResearch
		}
	}

	return CategoryGeneral
}

// RouteForCategory returns the PlanRoute for a given category.
func RouteForCategory(cat GoalCategory) PlanRoute {
	switch cat {
	case CategoryProject:
		return PlanRoute{
			Category:    CategoryProject,
			Workflow:    "gsd",
			Phases:      []string{"initialize", "research", "roadmap", "plan", "execute", "verify"},
			Description: "完整项目/新工程 → gsd 流程",
		}
	case CategoryCode:
		return PlanRoute{
			Category:    CategoryCode,
			Workflow:    "openspec",
			Phases:      []string{"proposal", "specs", "design", "tasks", "implementation"},
			Description: "代码变更/功能开发 → openspec 流程",
		}
	case CategoryResearch:
		return PlanRoute{
			Category:    CategoryResearch,
			Workflow:    "research",
			Phases:      []string{"define_scope", "collect_data", "analyze", "report"},
			Description: "研究/分析 → 研究流程",
		}
	default:
		return PlanRoute{
			Category:    CategoryGeneral,
			Workflow:    "plan-and-execute",
			Phases:      []string{"plan", "execute", "verify"},
			Description: "通用任务 → plan-and-execute 流程",
		}
	}
}

// GenerateCapabilities produces the capability list (skills/tools/clients)
// for a given category, to be included in the plan for user approval.
func GenerateCapabilities(cat GoalCategory) []string {
	base := []string{
		"store.go (持久化)",
		"cmd_lht.go (命令处理)",
	}

	switch cat {
	case CategoryProject:
		return append(base,
			"skill:gsd (项目初始化/路线图/执行)",
			"client:lht-gen (生成角色)",
			"client:lht-eval (评估角色)",
			"client:lht-rev (评审角色)",
		)
	case CategoryCode:
		return append(base,
			"skill:openspec (proposal/specs/design/tasks/implementation)",
			"client:lht-gen (生成角色)",
			"client:lht-eval (评估角色)",
			"client:lht-rev (评审角色)",
		)
	case CategoryResearch:
		return append(base,
			"client:lht-gen (研究分析)",
			"web_search (信息检索)",
			"web_fetch (数据采集)",
		)
	default:
		return append(base,
			"client:lht-gen (通用执行)",
			"client:lht-eval (评估角色)",
		)
	}
}

// BuildPlan generates a Plan from a Goal, GoalDefinition, and GoalCategory.
// It creates a task DAG with dependencies and capability declarations.
func BuildPlan(goal Goal, goalDef GoalDefinition, category GoalCategory, version string) Plan {
	route := RouteForCategory(category)
	caps := GenerateCapabilities(category)

	// Generate tasks based on the workflow phases
	tasks := generateTaskDAG(goal.GoalID, route, goalDef)

	return Plan{
		GoalID:       goal.GoalID,
		PlanVersion:  version,
		Tasks:        tasks,
		Capabilities: caps,
	}
}

// generateTaskDAG creates a task DAG from the workflow phases.
// Each phase becomes a task, with linear dependencies between phases.
// Acceptance criteria are derived from the goal definition.
func generateTaskDAG(goalID string, route PlanRoute, goalDef GoalDefinition) []TaskNode {
	tasks := make([]TaskNode, 0, len(route.Phases))

	for i, phase := range route.Phases {
		deps := make([]string, 0)
		if i > 0 {
			// Each phase depends on the previous one
			deps = append(deps, taskID(goalID, route.Phases[i-1]))
		}

		desc := fmt.Sprintf("[%s] %s: %s", route.Workflow, phase, goalDef.Description)
		mustHaves := derivePhaseMustHaves(phase, goalDef)
		rubric := derivePhaseRubric(phase, goalDef)

		tasks = append(tasks, TaskNode{
			TaskID:       taskID(goalID, phase),
			Description:  desc,
			State:        StateGrounding, // tasks start in GROUNDING; engine transitions to EXECUTING
			Dependencies: deps,
			MustHaves:    mustHaves,
			Rubric:       rubric,
		})
	}

	return tasks
}

func taskID(goalID, phase string) string {
	return fmt.Sprintf("%s/%s", goalID, phase)
}

func derivePhaseMustHaves(phase string, def GoalDefinition) []string {
	switch phase {
	case "specs", "design":
		return append([]string{"必须符合架构规范"}, def.Constraints...)
	case "implementation", "execute":
		return def.AcceptanceCriteria
	case "verify", "evaluate":
		return []string{"所有 MUST 验收标准通过", "测试覆盖率达标"}
	default:
		return []string{"阶段输出物完成"}
	}
}

func derivePhaseRubric(phase string, def GoalDefinition) string {
	switch phase {
	case "specs":
		return "规格文档完整、无歧义、验收标准可量化"
	case "design":
		return "设计文档覆盖所有需求，架构合理"
	case "implementation":
		return strings.Join(def.AcceptanceCriteria, "; ")
	case "verify":
		return "所有测试通过，覆盖率 ≥ 80%"
	default:
		return "输出物通过审阅"
	}
}

// ValidateDAG checks the plan's task DAG for structural integrity:
// - No duplicate task IDs
// - All dependencies reference existing tasks
// - No cycles (simple DFS)
// Returns nil if valid, or a descriptive error.
func ValidateDAG(plan Plan) error {
	idSet := make(map[string]int) // taskID → index in plan.Tasks
	for i, t := range plan.Tasks {
		if _, exists := idSet[t.TaskID]; exists {
			return fmt.Errorf("duplicate task ID: %s", t.TaskID)
		}
		idSet[t.TaskID] = i
	}

	// Check dependency references exist
	for _, t := range plan.Tasks {
		for _, dep := range t.Dependencies {
			if _, exists := idSet[dep]; !exists {
				return fmt.Errorf("task %s depends on unknown task %s", t.TaskID, dep)
			}
		}
	}

	// Cycle detection via DFS coloring: 0=unvisited, 1=visiting, 2=visited
	color := make(map[string]int)
	for _, t := range plan.Tasks {
		color[t.TaskID] = 0
	}

	var dfs func(taskID string) error
	dfs = func(taskID string) error {
		c, ok := color[taskID]
		if !ok {
			return fmt.Errorf("unknown task %s in DFS", taskID)
		}
		if c == 1 {
			return fmt.Errorf("cycle detected involving task %s", taskID)
		}
		if c == 2 {
			return nil
		}
		color[taskID] = 1
		idx := idSet[taskID]
		for _, dep := range plan.Tasks[idx].Dependencies {
			if err := dfs(dep); err != nil {
				return err
			}
		}
		color[taskID] = 2
		return nil
	}

	for _, t := range plan.Tasks {
		if err := dfs(t.TaskID); err != nil {
			return err
		}
	}

	return nil
}

// MaxDAGDepth computes the maximum dependency depth in the plan.
func MaxDAGDepth(plan Plan) int {
	if len(plan.Tasks) == 0 {
		return 0
	}

	depth := make(map[string]int)

	// Compute depth for each task: depth = 1 + max(depDepths)
	var compute func(taskID string) int
	compute = func(taskID string) int {
		if d, ok := depth[taskID]; ok {
			return d
		}

		// Find the task
		var deps []string
		for _, t := range plan.Tasks {
			if t.TaskID == taskID {
				deps = t.Dependencies
				break
			}
		}

		if len(deps) == 0 {
			depth[taskID] = 1
			return 1
		}

		maxDep := 0
		for _, d := range deps {
			dd := compute(d)
			if dd > maxDep {
				maxDep = dd
			}
		}
		depth[taskID] = maxDep + 1
		return depth[taskID]
	}

	maxDepth := 0
	for _, t := range plan.Tasks {
		d := compute(t.TaskID)
		if d > maxDepth {
			maxDepth = d
		}
	}

	return maxDepth
}
