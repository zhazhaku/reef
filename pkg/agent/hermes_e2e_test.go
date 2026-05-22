// Reef - Distributed multi-agent swarm orchestration system

package agent

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/providers"
)

// setupHermesTestDB creates an in-memory SQLite DB with the Hermes schema.
func setupHermesTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	schema := []string{
		`CREATE TABLE IF NOT EXISTS conversation_mode (
			conversation_id TEXT PRIMARY KEY,
			mode            TEXT NOT NULL DEFAULT 'chat',
			updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS hermes_workflow_sessions (
			conversation_id  TEXT PRIMARY KEY,
			task_id          TEXT NOT NULL,
			phase            TEXT NOT NULL DEFAULT 'idle',
			task_profile     TEXT,
			current_round    INTEGER DEFAULT 0,
			max_rounds       INTEGER DEFAULT 5,
			directions       TEXT,
			converge_streak  INTEGER DEFAULT 0,
			selected_clients TEXT,
			task_boards      TEXT,
			created_at       TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
	}
	for _, ddl := range schema {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	return db
}

func minStr(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func taskBoardKeys(session *WorkflowSession) []string {
	keys := make([]string, 0, len(session.TaskBoards))
	for k := range session.TaskBoards {
		keys = append(keys, k)
	}
	return keys
}

// mockLLMProvider implements providers.LLMProvider with pre-canned responses.
type mockLLMProvider struct {
	responses []string
	callCount int
	t         *testing.T
}

func (m *mockLLMProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, params map[string]any) (*providers.LLMResponse, error) {
	resp := ""
	if m.callCount < len(m.responses) {
		resp = m.responses[m.callCount]
	}
	m.callCount++
	m.t.Logf("mockLLM #%d → %q", m.callCount-1, resp[:minStr(len(resp), 80)])
	return &providers.LLMResponse{Content: resp}, nil
}

func (m *mockLLMProvider) ChatStream(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, params map[string]any) (*providers.LLMResponse, error) {
	return m.Chat(ctx, messages, tools, model, params)
}

func (m *mockLLMProvider) GetDefaultModel() string {
	return "mock-model"
}

// e2eMockResponses returns a full set of canned responses for the 6-phase pipeline.
func e2eMockResponses() []string {
	return []string{
		// Phase 1: Intake classify
		`{"domain":"technology","type":"system-design","complexity":4,"estimated_clients":4,"keywords":["scheduler","distributed"]}`,
		// Phase 2: Brainstorm R1 (4 directions)
		`[{"label":"单体调度器","description":"单进程内嵌调度。","openspec_category":"arch-decision","keywords":["monolith","pq"]},{"label":"分布式队列","description":"Redis/Kafka异步调度。","openspec_category":"arch-decision","keywords":["distributed","kafka"]},{"label":"事件驱动","description":"事件总线解耦。","openspec_category":"arch-decision","keywords":["pubsub"]},{"label":"工作流引擎","description":"Temporal成熟引擎。","openspec_category":"tech-stack","keywords":["temporal","workflow"]}]`,
		// Phase 3: Review (4 dimension reviews for tech-stack)
		`关键发现：Temporal技术成熟，Uber/Netflix生产案例丰富。建议POC验证。`,
		`扩展性：天然支持水平扩展，瓶颈在数据库层。`,
		`复杂度：学习曲线中高，需2-3周培训。长期收益明显。`,
		`替代方案：Celery、Step Functions均不如Temporal适配Go生态。`,
		// Phase 4: Research plan + 3 items
		`[{"topic":"Temporal案例","question":"最佳实践？","method":"literature","priority":"high"},{"topic":"性能基准","question":"1000QPS延迟？","method":"benchmark","priority":"high"},{"topic":"运维成本","question":"部署成本？","method":"analysis","priority":"medium"}]`,
		`Temporal在Uber/Netflix大型调度场景中有丰富案例。建议大载荷存外部存储。`,
		`单Temporal Server可处理500-800task/s，P99延迟200-500ms。可线性扩展。`,
		`自托管最小部署约$500-800/月。Temporal Cloud初期更经济。`,
		// Phase 5: Design
		`## 1. 架构概览
Temporal Server作为核心调度引擎，业务服务通过SDK执行workflow。

## 2. 核心组件
- Temporal Server
- Task Gateway
- Worker Pool
- Payload Store

## 3. 数据流
外部系统 → Task Gateway → Temporal → Workers → 回调

## 4. 接口设计
- POST /api/v1/tasks
- GET /api/v1/tasks/{id}
- Webhook callback

## 5. 部署方案
K8s + Temporal Cloud，灰度发布策略。

## 6. 关键决策
| 决策点 | 选择 | 理由 |
|-------|------|------|
| 引擎 | Temporal | 成熟度 |
| 存储 | Cloud | 降运维 |`,
		// Phase 6: Report
		`## 执行摘要
推荐采用Temporal工作流引擎。成熟度高、扩展性好。预计4-6周实施。

## 1. 项目背景
单体调度系统需升级为分布式工作流引擎。

## 2. 方案选择
评估四种方案后选择Temporal：成熟案例、重试机制、Go SDK。

## 3. 关键发现
- Uber/Netflix有大规模案例
- P99延迟200-500ms
- 自托管$500-800/月
- 可渐进式迁移

## 4. 设计概要
Temporal Server + Gateway + Workers + Payload Store

## 5. 实施建议
三阶段：POC(2周)、非关键上线(2周)、核心迁移(2周)。`,
	}
}

// ─────────────────────────────────────────────────────────────
// E2E: Full 6-phase pipeline
// ─────────────────────────────────────────────────────────────

func TestHermesE2E_FullPipeline(t *testing.T) {
	db := setupHermesTestDB(t)
	ws := NewWorkflowStore(db)
	ms := NewModeStore(db)
	ctx := context.Background()
	convID := "100"

	mock := &mockLLMProvider{t: t, responses: e2eMockResponses()}
	orch := NewHermesOrchestrator(HermesOrchestratorConfig{
		WorkflowStore: ws,
		ModeStore:     ms,
		Provider: func() (providers.LLMProvider, string) { return mock, "mock" },
		Model:     func() string { return "mock" },
	})

	// ── Phase 1: Intake ──
	t.Log("=== Phase 1: Intake ===")
	reply, handled := orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "设计分布式任务调度系统"})
	if !handled {
		t.Fatal("intake not handled")
	}
	sess, _ := ws.GetSession(ctx, convID)
	if sess.Phase != PhaseIntake || sess.TaskProfile.Complexity != 4 {
		t.Fatalf("intake: phase=%s complexity=%d", sess.Phase, sess.TaskProfile.Complexity)
	}
	t.Logf("✓ Intake: domain=%s type=%s complexity=%d", sess.TaskProfile.Domain, sess.TaskProfile.Type, sess.TaskProfile.Complexity)

	// ── Phase 2: Brainstorm Round 1 ──
	t.Log("=== Phase 2: Brainstorm R1 ===")
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "继续"})
	sess, _ = ws.GetSession(ctx, convID)
	if sess.Phase != PhaseBrainstorm || sess.CurrentRound != 1 || len(sess.Directions) != 4 {
		t.Fatalf("brainstorm: phase=%s round=%d dirs=%d", sess.Phase, sess.CurrentRound, len(sess.Directions))
	}
	if !strings.Contains(reply, "头脑风暴") {
		t.Error("missing 头脑风暴 in output")
	}
	t.Logf("✓ Brainstorm: %d directions in round 1", len(sess.Directions))

	// ── Phase 2→3: Select direction #4 ──
	t.Log("=== Phase 3: Select → Review ===")
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "选 4"})
	sess, _ = ws.GetSession(ctx, convID)
	if sess.Phase != PhaseReview {
		t.Fatalf("phase=%s after select, want review", sess.Phase)
	}
	if len(sess.Directions) != 1 || sess.Directions[0].Label != "工作流引擎" {
		t.Fatalf("selected direction: %v", sess.Directions)
	}
	t.Logf("✓ Selected: %s", sess.Directions[0].Label)

	// ── Phase 3: Review — trigger 4-dimension specialist reviews ──
	t.Log("=== Phase 3: Review execution ===")
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "开始评审"})
	sess, _ = ws.GetSession(ctx, convID)
	reviewKey := "review_" + sess.TaskID
	if _, ok := sess.TaskBoards[reviewKey]; !ok {
		t.Fatalf("review not persisted, boards=%v", taskBoardKeys(sess))
	}
	if !strings.Contains(reply, "方案评审") {
		t.Errorf("review output missing title: %s", reply[:minStr(len(reply), 200)])
	}
	t.Logf("✓ Review: persisted under key %s", reviewKey)

	// ── Phase 3→4: Review "继续" → Research ──
	t.Log("=== Phase 4: Research ===")
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "继续"})
	sess, _ = ws.GetSession(ctx, convID)
	if sess.Phase != PhaseResearch {
		t.Fatalf("phase=%s after continue, want research", sess.Phase)
	}
	// Trigger actual research execution
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "开始研究"})
	sess, _ = ws.GetSession(ctx, convID)
	researchKey := "research_" + sess.TaskID
	if _, ok := sess.TaskBoards[researchKey]; !ok {
		t.Fatalf("research not persisted, boards=%v", taskBoardKeys(sess))
	}
	if !strings.Contains(reply, "深度研究") {
		t.Error("research output missing title")
	}
	t.Logf("✓ Research: persisted under key %s", researchKey)

	// ── Phase 4→5: Research "继续" → Design ──
	t.Log("=== Phase 5: Design ===")
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "继续"})
	sess, _ = ws.GetSession(ctx, convID)
	if sess.Phase != PhaseDesign {
		t.Fatalf("phase=%s after continue, want design", sess.Phase)
	}
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "生成设计"})
	sess, _ = ws.GetSession(ctx, convID)
	designKey := "design_" + sess.TaskID
	if _, ok := sess.TaskBoards[designKey]; !ok {
		t.Fatalf("design not persisted, boards=%v", taskBoardKeys(sess))
	}
	if !strings.Contains(reply, "架构概览") {
		t.Error("design missing architecture section")
	}
	t.Logf("✓ Design: persisted under key %s", designKey)

	// ── Phase 5→6: Design "继续" → Report ──
	t.Log("=== Phase 6: Report ===")
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "继续"})
	sess, _ = ws.GetSession(ctx, convID)
	if sess.Phase != PhaseReport {
		t.Fatalf("phase=%s after continue, want report", sess.Phase)
	}
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "生成报告"})
	sess, _ = ws.GetSession(ctx, convID)
	reportKey := "report_" + sess.TaskID
	if _, ok := sess.TaskBoards[reportKey]; !ok {
		t.Fatalf("report not persisted, boards=%v", taskBoardKeys(sess))
	}
	if !strings.Contains(reply, "执行摘要") {
		t.Error("report missing executive summary")
	}
	t.Logf("✓ Report: persisted under key %s", reportKey)

	// ── Phase 6→Complete: "完成" ──
	t.Log("=== Complete ===")
	reply, handled = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "完成"})
	sess, err := ws.GetSession(ctx, convID)
	if err == nil && sess != nil {
		t.Error("session not cleaned")
	}
	mode, _ := ms.GetMode(ctx, convID)
	if mode != ModeChat {
		t.Errorf("mode=%s, want chat", mode)
	}
	if !strings.Contains(reply, "完成") {
		t.Error("completion message missing")
	}
	t.Logf("✓ Complete: session=nil mode=%s", mode)

	t.Log("═══════════════════════════════════════")
	t.Logf("✅ 6-PHASE E2E PIPELINE PASS (%d LLM calls)", mock.callCount)
	t.Log("═══════════════════════════════════════")
}

// ─────────────────────────────────────────────────────────────
// E2E: Abort at each phase
// ─────────────────────────────────────────────────────────────

func TestHermesE2E_AbortAtEachPhase(t *testing.T) {
	db := setupHermesTestDB(t)
	ws := NewWorkflowStore(db)
	ms := NewModeStore(db)
	ctx := context.Background()

	// Need 6 responses: one for each intake call
	mock := &mockLLMProvider{t: t, responses: make([]string, 6)}
	for i := range mock.responses {
		mock.responses[i] = `{"domain":"technology","type":"system-design","complexity":4,"estimated_clients":3,"keywords":["test"]}`
	}

	orch := NewHermesOrchestrator(HermesOrchestratorConfig{
		WorkflowStore: ws,
		ModeStore:     ms,
		Provider: func() (providers.LLMProvider, string) { return mock, "mock" },
		Model:     func() string { return "mock" },
	})

	phases := []struct {
		id    string
		phase WorkflowPhase
		abort string
	}{
		{"201", PhaseIntake, "停"},
		{"202", PhaseBrainstorm, "取消"},
		{"203", PhaseReview, "stop"},
		{"204", PhaseResearch, "停"},
		{"205", PhaseDesign, "取消"},
		{"206", PhaseReport, "stop"},
	}

	for _, tc := range phases {
		t.Logf("Abort at %s", tc.phase)

		// Create session via intake
		orch.ProcessMessage(ctx, tc.id, bus.InboundMessage{Content: "设计系统"})
		sess, _ := ws.GetSession(ctx, tc.id)

		// Force into target phase
		sess.Phase = tc.phase
		sess.Directions = []Direction{{ID: "d1", Label: "Test", OpenspecCategory: "other"}}
		sess.TaskBoards = map[string]string{
			"review_" + sess.TaskID:   `{}`,
			"research_" + sess.TaskID: `{}`,
			"design_" + sess.TaskID:   "x",
			"report_" + sess.TaskID:   "x",
		}
		_ = ws.SaveSession(ctx, sess)

		reply, handled := orch.ProcessMessage(ctx, tc.id, bus.InboundMessage{Content: tc.abort})
		if !handled {
			t.Errorf("abort at %s not handled", tc.phase)
			continue
		}
		if !strings.Contains(reply, "取消") && !strings.Contains(reply, "cancel") {
			t.Errorf("abort msg missing: %.80s", reply)
		}
		sess, _ = ws.GetSession(ctx, tc.id)
		if sess != nil {
			t.Errorf("session not cleaned at %s", tc.phase)
		}
		mode, _ := ms.GetMode(ctx, tc.id)
		if mode != ModeChat {
			t.Errorf("mode=%s after abort at %s", mode, tc.phase)
		}
	}
	t.Log("✓ All 6 phase abort paths verified")
}

// ─────────────────────────────────────────────────────────────
// E2E: Modify backward transitions
// ─────────────────────────────────────────────────────────────

func TestHermesE2E_ModifyBackwardFlow(t *testing.T) {
	db := setupHermesTestDB(t)
	ws := NewWorkflowStore(db)
	ms := NewModeStore(db)
	ctx := context.Background()

	mock := &mockLLMProvider{t: t, responses: []string{
		`{"domain":"technology","type":"system-design","complexity":4,"estimated_clients":3,"keywords":["test"]}`,
	}}

	orch := NewHermesOrchestrator(HermesOrchestratorConfig{
		WorkflowStore: ws,
		ModeStore:     ms,
		Provider: func() (providers.LLMProvider, string) { return mock, "mock" },
		Model:     func() string { return "mock" },
	})

	convID := "300"

	// Create session
	orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "设计系统"})
	sess, _ := ws.GetSession(ctx, convID)
	taskID := sess.TaskID

	// Test Report → Design
	sess.Phase = PhaseReport
	sess.Directions = []Direction{{ID: "d1", Label: "Test", OpenspecCategory: "other"}}
	sess.TaskBoards = map[string]string{
		"review_"+taskID:   `{}`,
		"research_"+taskID: `{}`,
		"design_"+taskID:   "## design",
		"report_"+taskID:   "## report",
	}
	_ = ws.SaveSession(ctx, sess)

_, _ = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "修改"})
	sess, _ = ws.GetSession(ctx, convID)
	if sess.Phase != PhaseDesign {
		t.Errorf("Report→Design: phase=%s", sess.Phase)
	}
	if _, ok := sess.TaskBoards["report_"+taskID]; ok {
		t.Error("report board not cleared")
	}
	t.Logf("✓ Report→Design")

	// Design → Research
_, _ = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "修改"})
	sess, _ = ws.GetSession(ctx, convID)
	if sess.Phase != PhaseResearch {
		t.Errorf("Design→Research: phase=%s", sess.Phase)
	}
	t.Logf("✓ Design→Research")

	// Research → Review
_, _ = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "修改"})
	sess, _ = ws.GetSession(ctx, convID)
	if sess.Phase != PhaseReview {
		t.Errorf("Research→Review: phase=%s", sess.Phase)
	}
	t.Logf("✓ Research→Review")

	// Review → Brainstorm
_, _ = orch.ProcessMessage(ctx, convID, bus.InboundMessage{Content: "修改"})
	sess, _ = ws.GetSession(ctx, convID)
	if sess.Phase != PhaseBrainstorm {
		t.Errorf("Review→Brainstorm: phase=%s", sess.Phase)
	}
	if sess.Directions != nil {
		t.Error("directions not cleared")
	}
	t.Logf("✓ Review→Brainstorm")

	t.Log("✓ All modify backward paths verified")
}
