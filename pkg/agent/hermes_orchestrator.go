// Reef - Distributed multi-agent swarm orchestration system

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/logger"
	"github.com/zhazhaku/reef/pkg/providers"
)

// ─────────────────────────────────────────────────────────────
// Hermes Orchestrator
// ─────────────────────────────────────────────────────────────

// HermesOrchestrator drives the 6-phase Hermes collaborative workflow.
type HermesOrchestrator struct {
	workflowStore *WorkflowStore
	modeStore     *ModeStore
	getProvider   func() (providers.LLMProvider, string)
	getModel      func() string
	llmTimeout    time.Duration

	mu sync.Mutex
}

// HermesOrchestratorConfig wires the orchestrator into AgentLoop.
type HermesOrchestratorConfig struct {
	WorkflowStore *WorkflowStore
	ModeStore     *ModeStore
	Provider      func() (providers.LLMProvider, string)
	Model         func() string
	LLMTimeout    time.Duration
}

// NewHermesOrchestrator creates a HermesOrchestrator.
func NewHermesOrchestrator(cfg HermesOrchestratorConfig) *HermesOrchestrator {
	timeout := cfg.LLMTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &HermesOrchestrator{
		workflowStore: cfg.WorkflowStore,
		modeStore:     cfg.ModeStore,
		getProvider:   cfg.Provider,
		getModel:      cfg.Model,
		llmTimeout:    timeout,
	}
}

// ─────────────────────────────────────────────────────────────
// Phase dispatcher
// ─────────────────────────────────────────────────────────────

// ProcessMessage routes a Hermes-mode message through the workflow.
func (o *HermesOrchestrator) ProcessMessage(ctx context.Context, convID string, msg bus.InboundMessage) (reply string, handled bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	session, err := o.workflowStore.GetSession(ctx, convID)
	if err != nil {
		o.logf("get session error: %v", err)
		return "内部错误：无法获取工作流状态", true
	}

	if session == nil {
		return o.handleIntake(ctx, convID, msg.Content)
	}

	switch session.Phase {
	case PhaseIntake:
		return o.handlePhaseIntake(ctx, convID, session, msg.Content)
	case PhaseBrainstorm:
		return o.handlePhaseBrainstorm(ctx, convID, session, msg.Content)
	case PhaseReview:
		return o.handlePhaseReview(ctx, convID, session, msg.Content)
	case PhaseResearch:
		return o.handlePhaseResearch(ctx, convID, session, msg.Content)
	case PhaseDesign:
		return o.handlePhaseDesign(ctx, convID, session, msg.Content)
	case PhaseReport:
		return o.handlePhaseReport(ctx, convID, session, msg.Content)
	default:
		return "工作流状态异常，已重置。请重新开始。", true
	}
}

// ─────────────────────────────────────────────────────────────
// Phase 1: Intake
// ─────────────────────────────────────────────────────────────

const classifyTaskPrompt = `Analyze the following user request and return ONLY a JSON object. No explanation.

{
  "domain": "<technology|business|science|general|other>",
  "type": "<system-design|market-research|code-review|tech-selection|debugging|simple-qa|creative|unknown>",
  "complexity": <1-5>,
  "estimated_clients": <number>,
  "keywords": ["key1", "key2"]
}

Complexity scale:
1 = trivial greeting / single fact question
2 = requires 1 tool call or simple research
3 = multi-step, needs 2-3 specialists
4 = complex with trade-offs, needs 4+ specialists
5 = system-level architecture / strategic

User request:`

func (o *HermesOrchestrator) ClassifyTask(ctx context.Context, userMessage string) (*TaskProfile, error) {
	provider, model := o.getProvider()
	if model == "" {
		model = o.getModel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), o.llmTimeout)
	defer cancel()

	resp, err := provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: classifyTaskPrompt + "\n\n" + userMessage},
	}, nil, model, map[string]any{
		"temperature": 0.0,
		"thinking_level": "xhigh",
	})
	if err != nil {
		return nil, fmt.Errorf("classify: %w", err)
	}

	o.logf("classify response: content_len=%d reasoning_len=%d finish=%s",
		len(resp.Content), len(resp.ReasoningContent), resp.FinishReason)

	content := resp.Content
	if content == "" && resp.ReasoningContent != "" {
		content = resp.ReasoningContent
	}

	tp, err := parseTaskProfile(content)
	if err != nil {
		o.logf("classify parse failed: %v, raw=%s", err, resp.Content)
		return &TaskProfile{Type: "unknown", Complexity: 1, Keywords: []string{}}, nil
	}
	return tp, nil
}

func (o *HermesOrchestrator) handleIntake(ctx context.Context, convID, userMessage string) (string, bool) {
	// ── Step 0: Check if user explicitly specified complexity ──
	explicitComplexity, _ := extractExplicitComplexity(userMessage)
	if explicitComplexity > 0 {
		o.logf("intake: user specified complexity=%d", explicitComplexity)
	}

	// Classify with full original message — LLM needs complete context
	// for type/keywords/domain, even when user explicitly specified complexity.
	tp, err := o.ClassifyTask(ctx, userMessage)
	if err != nil {
		o.logf("classify error: %v", err)
		tp = &TaskProfile{Type: "unknown", Complexity: 3, Keywords: []string{}}
	}

	// Override with user-specified complexity if provided
	if explicitComplexity > 0 {
		tp.Complexity = explicitComplexity
	} else if tp.Type == "unknown" {
		// LLM couldn't classify → ask user for complexity
		tp.Complexity = 0 // sentinel: needs clarification
	}

	o.logf("intake: domain=%s type=%s complexity=%d", tp.Domain, tp.Type, tp.Complexity)

	// ── Step 1: LLM uncertain → return to user for complexity clarification ──
	if tp.Complexity == 0 {
		taskID := generateTaskID()
		ws := &WorkflowSession{
			ConversationID: convID,
			TaskID:         taskID,
			Phase:          PhaseIntake,
			TaskProfile:    tp,
			CurrentRound:   0,
			MaxRounds:      5,
			TaskBoards:     map[string]string{"intake_pending": userMessage},
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		_ = o.workflowStore.SaveSession(ctx, ws)
		return `[Hermes] 无法确定任务复杂度。请回复数字 1-5 指定复杂度：

1 = 简单问候 / 单个事实查询
2 = 需要 1 个工具调用或简单搜索
3 = 多步骤，需要 2-3 个专家
4 = 复杂有取舍，需要 4+ 专家
5 = 系统级架构 / 战略决策

你也可以输入「复杂度N」整体重新指定，例如「复杂度4」。`, true
	}

	// ── Step 2: Low complexity → reject ──
	if tp.Complexity <= 2 {
		return fmt.Sprintf(
			"[Hermes] 这个任务比较简单 (类型: %s, 复杂度: %d/5)，建议直接处理。\n\n"+
				"如需复杂协作（复杂度 3-5），可以输入「复杂度N」重新指定，或输入「切换聊天模式」回到普通对话。",
			tp.Type, tp.Complexity,
		), true
	}

	// ── Step 3: Create session and proceed ──
	return o.createIntakeSession(ctx, convID, tp)
}

func (o *HermesOrchestrator) createIntakeSession(ctx context.Context, convID string, tp *TaskProfile) (string, bool) {
	taskID := generateTaskID()
	ws := &WorkflowSession{
		ConversationID: convID,
		TaskID:         taskID,
		Phase:          PhaseIntake,
		TaskProfile:    tp,
		CurrentRound:   0,
		MaxRounds:      5,
		TaskBoards:     make(map[string]string),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := o.workflowStore.SaveSession(ctx, ws); err != nil {
		o.logf("save session error: %v", err)
		return "内部错误：创建工作流失败", true
	}

	return formatTaskProfileReply(tp, taskID), true
}

// extractExplicitComplexity detects if the user specified complexity in their message.
// Returns (complexity, remainder), where complexity is 0 if none found.
// Supports: "复杂度5", "复杂度 5", "complexity 4", "这是一个复杂度3的工作", etc.
func extractExplicitComplexity(msg string) (int, string) {
	lower := toLower(msg)

	// Pattern: "复杂度N" or "复杂度 N" or "complexity N"
	patterns := []string{"复杂度", "complexity"}
	for _, p := range patterns {
		idx := findIndex(lower, p)
		if idx == -1 {
			continue
		}
		rest := msg[idx+len(p):]
		rest = trimLeft(rest)
		if len(rest) > 0 && rest[0] >= '1' && rest[0] <= '5' {
			c := int(rest[0] - '0')
			// Remove the matched pattern from msg
			prefix := msg[:idx]
			suffix := rest[1:]
			remainder := trimSpace(prefix + suffix)
			return c, remainder
		}
	}
	return 0, msg
}

// findIndex returns the index of substring in s, or -1 if not found.
func findIndex(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimLeft(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[i:]
}

func trimSpace(s string) string {
	s = trimLeft(s)
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// ─────────────────────────────────────────────────────────────
// Phase 1b: handlePhaseIntake (user already has session)
// ─────────────────────────────────────────────────────────────

func (o *HermesOrchestrator) handlePhaseIntake(ctx context.Context, convID string, session *WorkflowSession, msg string) (string, bool) {
	lower := toLower(msg)

	// ── User specifies complexity during intake clarification ──
	// Check if session is awaiting complexity (complexity==0 in TaskProfile)
	if session.TaskProfile != nil && session.TaskProfile.Complexity == 0 {
		complexity := parseComplexityFromMessage(msg)
		if complexity > 0 {
			session.TaskProfile.Complexity = complexity
			_ = o.workflowStore.SaveSession(ctx, session)

			if complexity <= 2 {
				// Low complexity → reject, clean up
				_ = o.workflowStore.DeleteSession(ctx, convID)
				return fmt.Sprintf(
					"[Hermes] 复杂度 %d/5 的任务比较简单，建议直接处理。\n\n"+
						"如需复杂协作（复杂度 3-5），请重新发送需求并指定更高复杂度。",
					complexity,
				), true
			}

			// Proceed to workflow
			return formatTaskProfileReply(session.TaskProfile, session.TaskID), true
		}
		// Couldn't parse → re-prompt
		return "请回复数字 1-5 指定复杂度。例如：\n3 = 多步骤，需要 2-3 个专家\n4 = 复杂有取舍，需要 4+ 专家\n5 = 系统级架构 / 战略决策", true
	}

	if isContinueCommand(lower) {
		session.Phase = PhaseBrainstorm
		_ = o.workflowStore.SaveSession(ctx, session)
		return o.brainstormNextRound(ctx, convID, session)
	}
	if isAbortCommand(lower) {
		_ = o.workflowStore.DeleteSession(ctx, convID)
		_ = o.modeStore.SetMode(ctx, convID, ModeChat)
		return "工作流已取消，已切换回聊天模式。", true
	}
	return "请回复「继续」以进入头脑风暴，或「停」以取消。", true
}

// parseComplexityFromMessage extracts a complexity number (1-5) from a message.
// Handles: "3", "复杂度3", "complexity 4", "选3"
func parseComplexityFromMessage(msg string) int {
	lower := toLower(msg)
	msgTrimmed := trimSpace(msg)

	// Pure number: "3"
	if len(msgTrimmed) == 1 && msgTrimmed[0] >= '1' && msgTrimmed[0] <= '5' {
		return int(msgTrimmed[0] - '0')
	}

	// Prefix + number: "复杂度3", "complexity4", "选3"
	prefixes := []string{"复杂度", "complexity", "选", "select", "complexity "}
	for _, p := range prefixes {
		idx := findIndex(lower, p)
		if idx == -1 {
			continue
		}
		rest := msg[idx+len(p):]
		rest = trimLeft(rest)
		if len(rest) > 0 && rest[0] >= '1' && rest[0] <= '5' {
			return int(rest[0] - '0')
		}
	}
	return 0
}

// ─────────────────────────────────────────────────────────────
// Phase 2: Brainstorm
// ─────────────────────────────────────────────────────────────

const brainstormPrompt = `You are a creative solution architect. Given the task profile below, generate 4 solution directions.

Return ONLY a JSON array. No explanation, no markdown.

[
  {
    "label": "Short catchy name (5-10 chars)",
    "description": "1-2 sentence summary of this approach",
    "openspec_category": "<arch-decision|tech-stack|data-model|api-design|deployment|security|ux-flow|tradeoff|other>",
    "keywords": ["keyword1", "keyword2"]
  },
  ...
]

Rules:
- Each direction should be substantially different from others
- Labels should be distinctive and memorable
- Favor concrete approaches over vague ones
- openspec_category should be the most relevant single category

Task profile:`

const brainstormDiversityPrompt = `You are a creative solution architect. This is round %d of %d for the same task.

PREVIOUSLY PROPOSED directions (do NOT repeat these):
%s

Generate 3 NEW solution directions that are SUBSTANTIALLY DIFFERENT from all previous ones.
Explore angles not yet considered. Return ONLY a JSON array:

[
  {
    "label": "Short catchy name",
    "description": "1-2 sentence summary",
    "openspec_category": "<arch-decision|tech-stack|data-model|api-design|deployment|security|ux-flow|tradeoff|other>",
    "keywords": ["keyword1", "keyword2"]
  }
]

Task profile:`

func (o *HermesOrchestrator) handlePhaseBrainstorm(ctx context.Context, convID string, session *WorkflowSession, msg string) (string, bool) {
	lower := toLower(msg)

	if isAbortCommand(lower) {
		_ = o.workflowStore.DeleteSession(ctx, convID)
		_ = o.modeStore.SetMode(ctx, convID, ModeChat)
		return "工作流已取消，已切换回聊天模式。", true
	}

	// Select specific direction: "选 2" or "选2"
	if strings.HasPrefix(lower, "选") || strings.HasPrefix(lower, "select") {
		idx := extractSelectionIndex(lower)
		if idx > 0 && idx <= len(session.Directions) {
			sorted := sortedByRound(session.Directions)
			if idx-1 < len(sorted) {
				selected := sorted[idx-1]
				session.Phase = PhaseReview
				session.Directions = []Direction{selected}
				session.SelectedClients = nil
				_ = o.workflowStore.SaveSession(ctx, session)
				return fmt.Sprintf(
					"已选择方案「%s」。进入 **评审** 阶段。\n\n> %s\n\n此阶段将在 Wave 3 实现。",
					selected.Label, selected.Description,
				), true
			}
		}
		return "请使用「选 数字」选择一个有效方案。", true
	}

	// Next round
	if isContinueCommand(lower) {
		return o.brainstormNextRound(ctx, convID, session)
	}

	// Default help
	var b strings.Builder
	b.WriteString("头脑风暴阶段指令：\n")
	b.WriteString("- 「继续」— 生成新的方案方向\n")
	b.WriteString("- 「选 N」— 选择第 N 个方案进入评审\n")
	b.WriteString("- 「停」— 取消工作流\n")
	if session.CurrentRound > 0 {
		fmt.Fprintf(&b, "\n当前：第 %d/%d 轮，已有 %d 个方案",
			session.CurrentRound, session.MaxRounds, len(session.Directions))
	}
	return b.String(), true
}

// brainstormNextRound generates a new round of directions and checks convergence.
func (o *HermesOrchestrator) brainstormNextRound(ctx context.Context, convID string, session *WorkflowSession) (string, bool) {
	isFirst := session.CurrentRound == 0
	session.CurrentRound++

	dirs, err := o.generateDirections(ctx, session, isFirst)
	if err != nil {
		o.logf("brainstorm generate error: %v", err)
		return fmt.Sprintf("头脑风暴生成失败: %v。请重试。", err), true
	}

	if len(dirs) == 0 {
		return "未能生成有效方案，请重新尝试。", true
	}

	session.Directions = append(session.Directions, dirs...)

	converged, hint := detectConvergence(session)
	if converged {
		session.Phase = PhaseReview
		_ = o.workflowStore.SaveSession(ctx, session)
		return formatBrainstormOutput(session.Directions, session.CurrentRound, session.MaxRounds,
			"✅ "+hint+" 自动进入评审阶段。"), true
	}

	_ = o.workflowStore.SaveSession(ctx, session)

	streakHint := ""
	if session.ConvergeStreak > 0 {
		streakHint = fmt.Sprintf("💡 关键词已连续 %d 轮趋同，再一轮即可收敛。", session.ConvergeStreak)
	}

	return formatBrainstormOutput(session.Directions, session.CurrentRound, session.MaxRounds, streakHint), true
}

// generateDirections calls the LLM to brainstorm solution directions.
func (o *HermesOrchestrator) generateDirections(ctx context.Context, session *WorkflowSession, isFirstRound bool) ([]Direction, error) {
	provider, model := o.getProvider()
	if model == "" {
		model = o.getModel()
	}

	tpJSON, _ := json.Marshal(session.TaskProfile)
	tpStr := string(tpJSON)

	var prompt string
	if isFirstRound {
		prompt = brainstormPrompt + "\n" + tpStr
	} else {
		prevSummary := formatPreviousDirections(session.Directions)
		prompt = fmt.Sprintf(brainstormDiversityPrompt,
			session.CurrentRound, session.MaxRounds,
			prevSummary,
		) + "\n" + tpStr
	}

	// Use context.Background() to avoid inheriting parent deadline —
	// the LLM call has its own 30s timeout.
	ctx, cancel := context.WithTimeout(context.Background(), o.llmTimeout)
	defer cancel()

	resp, err := provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: prompt},
	}, nil, model, map[string]any{
		"temperature": 0.7,
		"thinking_level": "xhigh",
	})
	if err != nil {
		return nil, fmt.Errorf("brainstorm LLM: %w", err)
	}

	o.logf("brainstorm response: content_len=%d reasoning_len=%d tool_calls=%d finish=%s",
		len(resp.Content), len(resp.ReasoningContent), len(resp.ToolCalls), resp.FinishReason)

	// DeepSeek V4 xhigh may return empty content and put results in
	// reasoning_content or tool_calls. Fall back to reasoning_content.
	content := resp.Content
	if content == "" && resp.ReasoningContent != "" {
		content = resp.ReasoningContent
		o.logf("brainstorm: content empty, using reasoning_content (len=%d)", len(content))
	}

	dirs, err := parseDirectionResponse(content, session.CurrentRound)
	if err != nil {
		o.logf("brainstorm parse failed: %v, raw=%s", err, resp.Content)
		return nil, err
	}

	return dirs, nil
}

func parseDirectionResponse(raw string, round int) ([]Direction, error) {
	raw = stripCodeFences(raw)

	var items []struct {
		Label            string   `json:"label"`
		Description      string   `json:"description"`
		OpenspecCategory string   `json:"openspec_category"`
		Keywords         []string `json:"keywords"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("parse directions: %w", err)
	}

	dirs := make([]Direction, 0, len(items))
	for i, item := range items {
		if item.Label == "" {
			continue
		}
		dirs = append(dirs, Direction{
			ID:               fmt.Sprintf("d-r%d-%d", round, i+1),
			Label:            item.Label,
			Description:      item.Description,
			Proposer:         "hermes",
			Round:            round,
			OpenspecCategory: item.OpenspecCategory,
			Keywords:         item.Keywords,
		})
	}
	return dirs, nil
}

func formatPreviousDirections(dirs []Direction) string {
	if len(dirs) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, d := range dirs {
		fmt.Fprintf(&b, "- [%s] %s\n", d.Label, d.Description)
	}
	return b.String()
}

func formatBrainstormOutput(dirs []Direction, round, maxRounds int, convergeHint string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## 头脑风暴 — 第 %d/%d 轮\n\n", round, maxRounds)

	b.WriteString("| # | 方案 | 类别 |\n")
	b.WriteString("|---|------|------|\n")
	for i, d := range dirs {
		fmt.Fprintf(&b, "| %d | **%s** | %s |\n", i+1, d.Label, d.OpenspecCategory)
	}
	b.WriteString("\n")

	for i, d := range dirs {
		fmt.Fprintf(&b, "### %d. %s\n", i+1, d.Label)
		fmt.Fprintf(&b, "%s\n\n", d.Description)
		fmt.Fprintf(&b, "> 类别: `%s` | ID: `%s`\n\n", d.OpenspecCategory, d.ID)
	}

	if convergeHint != "" {
		b.WriteString(convergeHint)
		b.WriteString("\n\n")
	}

	b.WriteString("回复「继续」探索更多方向，「选 N」选择一个方案，「停」取消。")
	return b.String()
}

// ─────────────────────────────────────────────────────────────
// Converge detection
// ─────────────────────────────────────────────────────────────

func detectConvergence(session *WorkflowSession) (bool, string) {
	if session.CurrentRound >= session.MaxRounds {
		return true, fmt.Sprintf("已达最大轮数 (%d)，强制收敛。", session.MaxRounds)
	}

	if session.CurrentRound < 2 || len(session.Directions) < 4 {
		return false, ""
	}

	currentRoundKW := extractRoundKeywords(session.Directions, session.CurrentRound)
	prevRoundKW := extractRoundKeywords(session.Directions, session.CurrentRound-1)

	overlap := keywordOverlapRatio(currentRoundKW, prevRoundKW)

	if overlap >= 0.5 {
		session.ConvergeStreak++
		if session.ConvergeStreak >= 2 {
			return true, fmt.Sprintf("连续 %d 轮关键词收敛 (相似度 %.0f%%), 方向已稳定。",
				session.ConvergeStreak, overlap*100)
		}
	} else {
		session.ConvergeStreak = 0
	}

	return false, ""
}

func extractRoundKeywords(dirs []Direction, round int) []string {
	var kws []string
	for _, d := range dirs {
		if d.Round == round {
			for _, kw := range d.Keywords {
				kws = append(kws, kw)
			}
		}
	}
	return kws
}

func keywordOverlapRatio(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	aNorm := normalizeKeywords(a)
	bNorm := normalizeKeywords(b)

	union := make(map[string]bool)
	intersection := 0
	for _, k := range aNorm {
		union[k] = true
	}
	for _, k := range bNorm {
		if union[k] {
			intersection++
		}
		union[k] = true
	}
	if len(union) == 0 {
		return 1.0
	}
	return float64(intersection) / float64(len(union))
}

func normalizeKeywords(kws []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, k := range kws {
		lower := strings.ToLower(strings.TrimSpace(k))
		if lower != "" && !seen[lower] {
			seen[lower] = true
			out = append(out, lower)
		}
	}
	return out
}

func sortedByRound(dirs []Direction) []Direction {
	sorted := make([]Direction, len(dirs))
	copy(sorted, dirs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Round != sorted[j].Round {
			return sorted[i].Round < sorted[j].Round
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}

func extractSelectionIndex(lower string) int {
	s := lower
	s = strings.TrimPrefix(s, "选")
	s = strings.TrimPrefix(s, "select")
	s = strings.TrimSpace(s)

	var idx int
	if _, err := fmt.Sscanf(s, "%d", &idx); err == nil && idx > 0 {
		return idx
	}
	return 0
}

// ─────────────────────────────────────────────────────────────
// Phase 3: Review
// ─────────────────────────────────────────────────────────────

// reviewDimension defines one specialist review angle.
type reviewDimension struct {
	Name     string // display name
	Question string // the question to ask the specialist
	Role     string // specialist persona
}

// reviewDimensionsByCategory maps openspec_category to review dimensions.
var reviewDimensionsByCategory = map[string][]reviewDimension{
	"arch-decision": {
		{"技术可行性", "这个方案在给定的技术约束下是否可行？有哪些技术障碍？", "资深架构师"},
		{"可扩展性", "方案在数据量和用户量增长10倍时的表现如何？瓶颈在哪里？", "分布式系统专家"},
		{"复杂度评估", "实现这个方案需要多少工程投入？是否有过度设计的风险？", "Tech Lead"},
		{"替代方案", "是否有更简单但同样有效的替代方案？方案与其他已知方案的对比如何？", "技术顾问"},
	},
	"tech-stack": {
		{"技术成熟度", "所选技术栈的稳定性、社区支持和长期维护前景如何？", "技术评估师"},
		{"性能表现", "在目标场景下的性能基准和资源消耗预估如何？", "性能工程师"},
		{"学习曲线", "团队掌握该技术栈需要的培训成本和迁移风险？", "工程效能专家"},
		{"生态兼容", "与现有系统的集成难度？生态系统中是否有足够的工具和库？", "集成架构师"},
	},
	"data-model": {
		{"数据一致性", "方案如何保证数据一致性？CAP 定理中的取舍是什么？", "数据架构师"},
		{"查询效率", "核心查询路径的性能特征？是否需要 CQRS 或物化视图？", "数据库专家"},
		{"扩展策略", "分片策略？读写分离？数据增长到 TB 级时的表现？", "数据平台工程师"},
		{"迁移方案", "如果有现有数据，迁移策略和回滚计划是什么？", "数据工程师"},
	},
	"api-design": {
		{"接口设计", "API 是否符合 RESTful/GraphQL 最佳实践？接口的稳定性和可演化性？", "API 设计师"},
		{"版本策略", "如何处理 API 版本变更？向后兼容性如何保证？", "平台工程师"},
		{"安全考量", "认证、授权、限流、CORS 策略？是否有潜在的注入风险？", "安全工程师"},
		{"性能上限", "在高并发场景下的 QPS 预估？是否需要异步处理或任务队列？", "后端架构师"},
	},
	"deployment": {
		{"运维复杂度", "部署、监控、日志、告警的整套方案是否完整？", "SRE 工程师"},
		{"成本估算", "基础设施月度成本预估？是否有成本优化空间？", "FinOps 分析师"},
		{"可靠性", "目标 SLA 是多少？HA 策略、灾难恢复方案是否充分？", "可靠性工程师"},
		{"灰度策略", "如何逐步上线？金丝雀发布还是蓝绿部署？回滚时间？", "发布工程师"},
	},
	"security": {
		{"威胁建模", "方案面临的主要攻击面是什么？STRIDE 模型分析结果？", "安全架构师"},
		{"合规要求", "是否需要满足 PCI-DSS/GDPR/SOC2 等合规标准？差距在哪里？", "合规分析师"},
		{"实现成本", "安全方案的实施成本和性能损耗是否在可接受范围？", "安全工程师"},
		{"纵深防御", "网络层、应用层、数据层的防护是否层层递进？", "安全顾问"},
	},
	"ux-flow": {
		{"用户认知", "用户是否能凭直觉理解这个流程？认知负荷是否过高？", "UX 研究员"},
		{"交互效率", "完成核心任务需要的最少步骤？是否有不必要的摩擦点？", "交互设计师"},
		{"无障碍", "是否符合 WCAG 2.1 标准？键盘操作和屏幕阅读器支持？", "无障碍专家"},
		{"多端一致性", "Web/Mobile/Desktop 的体验是否一致？响应式策略如何？", "全栈设计师"},
	},
	"tradeoff": {
		{"收益分析", "方案带来的核心收益是什么？量化指标能否给出？", "产品策略师"},
		{"风险评估", "最大的风险是什么？概率和影响如何？缓解措施充分吗？", "风险管理师"},
		{"时机判断", "现在是最佳实施时机吗？推迟或提前的影响是什么？", "战略顾问"},
		{"依赖关系", "方案依赖哪些前置条件？如果依赖未就绪怎么办？", "项目经理"},
	},
	"other": {
		{"技术可行性", "方案最核心的技术挑战及应对策略？", "技术专家"},
		{"业务价值", "方案如何对齐业务目标？ROI 预估如何？", "业务分析师"},
		{"风险管控", "关键风险点和缓解策略？有降级或回滚方案吗？", "风险分析师"},
		{"实施路径", "最小可行实施路径？分几个迭代？里程碑是什么？", "交付经理"},
	},
}

// getReviewDimensions returns review dimensions for a direction.
// Falls back to "other" if the category is unknown.
func getReviewDimensions(dir Direction) []reviewDimension {
	if dims, ok := reviewDimensionsByCategory[dir.OpenspecCategory]; ok {
		return dims
	}
	return reviewDimensionsByCategory["other"]
}

const reviewSpecialistPrompt = `You are a %s reviewing a proposed technical solution.

## Task Context
%s

## Proposed Direction
**%s**: %s

## Your Review Focus
%s

Provide a concise specialist review (3-5 sentences). Structure your response as:
1. Key finding
2. Specific concern or strength
3. Recommendation

Reply in Chinese. Keep it concise and actionable.`

func (o *HermesOrchestrator) runReview(ctx context.Context, dim reviewDimension, dir Direction, tp *TaskProfile) (string, error) {
	provider, model := o.getProvider()
	if model == "" {
		model = o.getModel()
	}

	tpStr, _ := json.Marshal(tp)
	prompt := fmt.Sprintf(reviewSpecialistPrompt,
		dim.Role,
		string(tpStr),
		dir.Label,
		dir.Description,
		dim.Question,
	)

	ctx, cancel := context.WithTimeout(context.Background(), o.llmTimeout)
	defer cancel()

	resp, err := provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: prompt},
	}, nil, model, map[string]any{
		"temperature": 0.3,
		"thinking_level": "xhigh",
	})
	if err != nil {
		return "", fmt.Errorf("review %s: %w", dim.Name, err)
	}

	o.logf("review response (%s): content_len=%d reasoning_len=%d finish=%s",
		dim.Name, len(resp.Content), len(resp.ReasoningContent), resp.FinishReason)

	content := resp.Content
	if content == "" && resp.ReasoningContent != "" {
		content = resp.ReasoningContent
	}

	return content, nil
}

func (o *HermesOrchestrator) runAllReviews(ctx context.Context, dir Direction, tp *TaskProfile) (map[string]string, error) {
	dims := getReviewDimensions(dir)
	results := make(map[string]string, len(dims))

	for _, dim := range dims {
		o.logf("review: dimension=%s role=%s", dim.Name, dim.Role)
		result, err := o.runReview(ctx, dim, dir, tp)
		if err != nil {
			o.logf("review dimension %s failed: %v", dim.Name, err)
			results[dim.Name] = fmt.Sprintf("⚠️ 评审失败: %v", err)
			continue
		}
		results[dim.Name] = result
	}

	return results, nil
}

func formatReviewOutput(dir Direction, results map[string]string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## 方案评审 — %s\n\n", dir.Label)
	fmt.Fprintf(&b, "> %s\n\n", dir.Description)

	// Summary table
	b.WriteString("| 评审维度 | 结果摘要 |\n")
	b.WriteString("|----------|----------|\n")
	for name, result := range results {
		snippet := result
		if len(snippet) > 60 {
			snippet = snippet[:60] + "..."
		}
		// Remove newlines for table
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(&b, "| %s | %s |\n", name, snippet)
	}
	b.WriteString("\n")

	// Detailed results
	i := 1
	for name, result := range results {
		fmt.Fprintf(&b, "### %d. %s\n", i, name)
		fmt.Fprintf(&b, "%s\n\n", result)
		i++
	}

	b.WriteString("---\n")
	b.WriteString("回复「继续」进入**深度研究**阶段，「修改」回退到头脑风暴，「停」取消。")
	return b.String()
}

func (o *HermesOrchestrator) handlePhaseReview(ctx context.Context, convID string, session *WorkflowSession, msg string) (string, bool) {
	lower := toLower(msg)

	if isAbortCommand(lower) {
		_ = o.workflowStore.DeleteSession(ctx, convID)
		_ = o.modeStore.SetMode(ctx, convID, ModeChat)
		return "工作流已取消，已切换回聊天模式。", true
	}

	// First entry into review: run all specialist reviews
	if session.TaskBoards == nil {
		session.TaskBoards = make(map[string]string)
	}

	reviewKey := "review_" + session.TaskID
	if _, done := session.TaskBoards[reviewKey]; !done {
		// Need at least one direction
		if len(session.Directions) == 0 {
			return "没有可评审的方案，请回到头脑风暴阶段。", true
		}

		dir := session.Directions[0] // primary direction

		results, err := o.runAllReviews(ctx, dir, session.TaskProfile)
		if err != nil {
			o.logf("review error: %v", err)
			return "评审过程出错，请重试。", true
		}

		// Store results in task boards as JSON
		resultJSON, _ := json.Marshal(results)
		session.TaskBoards[reviewKey] = string(resultJSON)
		_ = o.workflowStore.SaveSession(ctx, session)

		return formatReviewOutput(dir, results), true
	}

	// Review is done, user can proceed or modify
	if isContinueCommand(lower) {
		session.Phase = PhaseResearch
		_ = o.workflowStore.SaveSession(ctx, session)
		return "评审完成。进入 **深度研究** 阶段。\n\n正在制定研究计划...", true
	}

	if lower == "修改" || lower == "回到" || lower == "回退" || lower == "revise" {
		session.Phase = PhaseBrainstorm
		delete(session.TaskBoards, reviewKey)
		session.Directions = nil
		session.CurrentRound = 0
		session.ConvergeStreak = 0
		_ = o.workflowStore.SaveSession(ctx, session)
		return "已回到**头脑风暴**阶段。请发送「继续」重新生成方案。", true
	}

	// Show review results again
	var results map[string]string
	if storedJSON, ok := session.TaskBoards[reviewKey]; ok {
		_ = json.Unmarshal([]byte(storedJSON), &results)
	}
	if results == nil {
		return "评审结果丢失，请回到头脑风暴重新开始。", true
	}

	dir := session.Directions[0]
	return formatReviewOutput(dir, results), true
}

// ─────────────────────────────────────────────────────────────
// Phase 4: Research
// ─────────────────────────────────────────────────────────────

const researchPlanPrompt = `You are a research lead. Given the task profile, selected direction, and specialist reviews below, generate a research plan.

## Task Profile
%s

## Selected Direction
**%s**: %s

## Specialist Reviews
%s

Generate a focused research plan with exactly 3 research items. Each item should investigate a critical unknown or validate a key assumption. Return ONLY a JSON array:

[
  {
    "topic": "Short topic name",
    "question": "Specific research question",
    "method": "literature|comparison|analysis|benchmark|best-practice",
    "priority": "high|medium"
  }
]

Focus on actionable, concrete questions. Avoid vague explorations.`

const researchExecutePrompt = `You are a %s researcher conducting deep investigation.

## Task Context
%s

## Direction: %s
%s

## Research Question
%s

Provide a thorough research answer (4-6 sentences) with specific findings, data points, or best practices. Reply in Chinese. Be concrete and actionable.`

func (o *HermesOrchestrator) generateResearchPlan(ctx context.Context, session *WorkflowSession) ([]map[string]string, error) {
	provider, model := o.getProvider()
	if model == "" {
		model = o.getModel()
	}

	tpStr, _ := json.Marshal(session.TaskProfile)
	dir := session.Directions[0]

	// Build reviews summary
	var reviewSum strings.Builder
	reviewKey := "review_" + session.TaskID
	if reviewJSON, ok := session.TaskBoards[reviewKey]; ok {
		var reviews map[string]string
		_ = json.Unmarshal([]byte(reviewJSON), &reviews)
		for name, result := range reviews {
			fmt.Fprintf(&reviewSum, "**%s**: %s\\n", name, result)
		}
	}

	prompt := fmt.Sprintf(researchPlanPrompt,
		string(tpStr),
		dir.Label, dir.Description,
		reviewSum.String(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), o.llmTimeout)
	defer cancel()

	resp, err := provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: prompt},
	}, nil, model, map[string]any{
		"temperature": 0.2,
		"thinking_level": "xhigh",
	})
	if err != nil {
		return nil, fmt.Errorf("research plan: %w", err)
	}

	o.logf("research plan response: content_len=%d reasoning_len=%d finish=%s",
		len(resp.Content), len(resp.ReasoningContent), resp.FinishReason)

	content := resp.Content
	if content == "" && resp.ReasoningContent != "" {
		content = resp.ReasoningContent
	}

	raw := stripCodeFences(content)
	var items []map[string]string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		// Fallback: default research items
		items = []map[string]string{
			{"topic": "技术背景调研", "question": "当前技术领域的最佳实践和常见陷阱是什么？", "method": "literature", "priority": "high"},
			{"topic": "方案验证", "question": "所选方案在类似场景中的实际表现如何？", "method": "comparison", "priority": "high"},
			{"topic": "风险评估", "question": "实施过程中的最大风险和不确定性是什么？", "method": "analysis", "priority": "medium"},
		}
	}
	return items, nil
}

func (o *HermesOrchestrator) executeResearchItem(ctx context.Context, item map[string]string, session *WorkflowSession) (string, error) {
	provider, model := o.getProvider()
	if model == "" {
		model = o.getModel()
	}

	tpStr, _ := json.Marshal(session.TaskProfile)
	dir := session.Directions[0]

	methodRoles := map[string]string{
		"literature":  "文献调研",
		"comparison":  "对比分析",
		"analysis":    "深度分析",
		"benchmark":   "基准测试",
		"best-practice": "最佳实践",
	}
	role := methodRoles[item["method"]]
	if role == "" {
		role = "技术研究"
	}

	prompt := fmt.Sprintf(researchExecutePrompt,
		role,
		string(tpStr),
		dir.Label, dir.Description,
		item["question"],
	)

	ctx, cancel := context.WithTimeout(context.Background(), o.llmTimeout)
	defer cancel()

	resp, err := provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: prompt},
	}, nil, model, map[string]any{
		"temperature": 0.3,
		"thinking_level": "xhigh",
	})
	if err != nil {
		return "", fmt.Errorf("research %s: %w", item["topic"], err)
	}

	o.logf("research execute (%s): content_len=%d reasoning_len=%d finish=%s",
		item["topic"], len(resp.Content), len(resp.ReasoningContent), resp.FinishReason)

	content := resp.Content
	if content == "" && resp.ReasoningContent != "" {
		content = resp.ReasoningContent
	}

	return content, nil
}

func formatResearchOutput(plan []map[string]string, results map[string]string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## 深度研究\n\n")

	b.WriteString("| # | 研究课题 | 方法 | 优先级 | 结果 |\n")
	b.WriteString("|---|----------|------|--------|------|\n")
	for i, item := range plan {
		snippet := results[item["topic"]]
		if len(snippet) > 40 {
			snippet = snippet[:40] + "..."
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |\n",
			i+1, item["topic"], item["method"], item["priority"], snippet)
	}
	b.WriteString("\n")

	for i, item := range plan {
		fmt.Fprintf(&b, "### %d. %s\n", i+1, item["topic"])
		fmt.Fprintf(&b, "> 方法: %s | 优先级: %s\n\n", item["method"], item["priority"])
		if result, ok := results[item["topic"]]; ok {
			fmt.Fprintf(&b, "%s\n\n", result)
		}
	}

	b.WriteString("---\n")
	b.WriteString("回复「继续」进入**详细设计**阶段，「修改」回退到评审，「停」取消。")
	return b.String()
}

func (o *HermesOrchestrator) handlePhaseResearch(ctx context.Context, convID string, session *WorkflowSession, msg string) (string, bool) {
	lower := toLower(msg)

	if isAbortCommand(lower) {
		_ = o.workflowStore.DeleteSession(ctx, convID)
		_ = o.modeStore.SetMode(ctx, convID, ModeChat)
		return "工作流已取消，已切换回聊天模式。", true
	}

	if session.TaskBoards == nil {
		session.TaskBoards = make(map[string]string)
	}

	if lower == "修改" || lower == "回到" || lower == "回退" || lower == "revise" {
		session.Phase = PhaseReview
		delete(session.TaskBoards, "research_"+session.TaskID)
		_ = o.workflowStore.SaveSession(ctx, session)
		return "已回到**评审**阶段。", true
	}

	researchKey := "research_" + session.TaskID
	if _, done := session.TaskBoards[researchKey]; !done {
		// Generate plan
		plan, err := o.generateResearchPlan(ctx, session)
		if err != nil {
			o.logf("research plan error: %v", err)
			return "研究计划生成失败，请重试。", true
		}

		// Execute all research items
		results := make(map[string]string)
		for _, item := range plan {
			o.logf("research: topic=%s method=%s", item["topic"], item["method"])
			result, err := o.executeResearchItem(ctx, item, session)
			if err != nil {
				o.logf("research item %s failed: %v", item["topic"], err)
				results[item["topic"]] = fmt.Sprintf("⚠️ 研究失败: %v", err)
				continue
			}
			results[item["topic"]] = result
		}

		// Store plan + results
		stored := map[string]interface{}{"plan": plan, "results": results}
		storedJSON, _ := json.Marshal(stored)
		session.TaskBoards[researchKey] = string(storedJSON)
		_ = o.workflowStore.SaveSession(ctx, session)

		return formatResearchOutput(plan, results), true
	}

	if isContinueCommand(lower) {
		session.Phase = PhaseDesign
		_ = o.workflowStore.SaveSession(ctx, session)
		return "研究完成。进入 **详细设计** 阶段。\n\n正在生成设计方案...", true
	}

	// Re-display stored results
	var stored struct {
		Plan    []map[string]string `json:"plan"`
		Results map[string]string   `json:"results"`
	}
	if storedJSON, ok := session.TaskBoards[researchKey]; ok {
		_ = json.Unmarshal([]byte(storedJSON), &stored)
	}
	if stored.Plan == nil {
		return "研究数据丢失，请回到评审阶段重新开始。", true
	}
	return formatResearchOutput(stored.Plan, stored.Results), true
}

// ─────────────────────────────────────────────────────────────
// Phase 5: Design
// ─────────────────────────────────────────────────────────────

const designPrompt = `You are a senior solution architect creating a detailed design specification.

## Task Profile
%s

## Selected Direction
**%s**: %s

## Review Summary
%s

## Research Findings
%s

Create a detailed design document in Chinese. Structure it as:

## 1. 架构概览
(2-3 sentences on overall architecture)

## 2. 核心组件
(3-5 bullet points, each: component name + 1 sentence responsibility)

## 3. 数据流
(Describe the main data flow in 2-3 sentences)

## 4. 接口设计
(Key API/interface definitions, 3-5 bullet points)

## 5. 部署方案
(2-3 sentences on deployment strategy)

## 6. 关键决策
(Table: | 决策点 | 选择 | 理由 |, 3-4 rows)

Keep it concrete and actionable. Avoid boilerplate.`

func (o *HermesOrchestrator) generateDesign(ctx context.Context, session *WorkflowSession) (string, error) {
	provider, model := o.getProvider()
	if model == "" {
		model = o.getModel()
	}

	tpStr, _ := json.Marshal(session.TaskProfile)
	dir := session.Directions[0]

	// Build review summary
	var reviewSum strings.Builder
	reviewKey := "review_" + session.TaskID
	if reviewJSON, ok := session.TaskBoards[reviewKey]; ok {
		var reviews map[string]string
		_ = json.Unmarshal([]byte(reviewJSON), &reviews)
		for name, result := range reviews {
			fmt.Fprintf(&reviewSum, "- **%s**: %s\\n", name, result)
		}
	}

	// Build research summary
	var researchSum strings.Builder
	researchKey := "research_" + session.TaskID
	if researchJSON, ok := session.TaskBoards[researchKey]; ok {
		var stored struct {
			Results map[string]string `json:"results"`
		}
		_ = json.Unmarshal([]byte(researchJSON), &stored)
		for topic, result := range stored.Results {
			fmt.Fprintf(&researchSum, "- **%s**: %s\\n", topic, result)
		}
	}

	prompt := fmt.Sprintf(designPrompt,
		string(tpStr),
		dir.Label, dir.Description,
		reviewSum.String(),
		researchSum.String(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), o.llmTimeout)
	defer cancel()

	resp, err := func() (*providers.LLMResponse, error) {
		if sp, ok := provider.(providers.StreamingProvider); ok {
			return sp.ChatStream(ctx, []providers.Message{
				{Role: "user", Content: prompt},
			}, nil, model, map[string]any{
				"temperature":    0.3,
				"thinking_level": "xhigh",
			}, nil)
		}
		return provider.Chat(ctx, []providers.Message{
			{Role: "user", Content: prompt},
		}, nil, model, map[string]any{
			"temperature":    0.3,
			"thinking_level": "xhigh",
		})
	}()
	if err != nil {
		return "", fmt.Errorf("design: %w", err)
	}

	o.logf("design response: content_len=%d reasoning_len=%d finish=%s",
		len(resp.Content), len(resp.ReasoningContent), resp.FinishReason)

	content := resp.Content
	if content == "" && resp.ReasoningContent != "" {
		content = resp.ReasoningContent
	}

	return content, nil
}

func (o *HermesOrchestrator) handlePhaseDesign(ctx context.Context, convID string, session *WorkflowSession, msg string) (string, bool) {
	lower := toLower(msg)

	if isAbortCommand(lower) {
		_ = o.workflowStore.DeleteSession(ctx, convID)
		_ = o.modeStore.SetMode(ctx, convID, ModeChat)
		return "工作流已取消，已切换回聊天模式。", true
	}

	if session.TaskBoards == nil {
		session.TaskBoards = make(map[string]string)
	}

	if lower == "修改" || lower == "回到" || lower == "回退" || lower == "revise" {
		session.Phase = PhaseResearch
		delete(session.TaskBoards, "design_"+session.TaskID)
		_ = o.workflowStore.SaveSession(ctx, session)
		return "已回到**深度研究**阶段。", true
	}

	designKey := "design_" + session.TaskID
	if _, done := session.TaskBoards[designKey]; !done {
		o.logf("design: generating design doc")
		design, err := o.generateDesign(ctx, session)
		if err != nil {
			o.logf("design error: %v", err)
			return "设计生成失败，请重试。", true
		}

		session.TaskBoards[designKey] = design
		_ = o.workflowStore.SaveSession(ctx, session)

		var b strings.Builder
		fmt.Fprintf(&b, "## 详细设计 — %s\n\n", session.Directions[0].Label)
		b.WriteString(design)
		b.WriteString("\n\n---\n")
		b.WriteString("回复「继续」生成**最终报告**，「修改」回退到研究，「停」取消。")
		return b.String(), true
	}

	if isContinueCommand(lower) {
		session.Phase = PhaseReport
		_ = o.workflowStore.SaveSession(ctx, session)
		return "设计完成。正在生成 **最终报告**...", true
	}

	// Re-display
	design, _ := session.TaskBoards[designKey]
	if design == "" {
		return "设计数据丢失，请回到研究阶段重新开始。", true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## 详细设计 — %s\n\n", session.Directions[0].Label)
	b.WriteString(design)
	b.WriteString("\n\n---\n")
	b.WriteString("回复「继续」生成**最终报告**，「修改」回退到研究，「停」取消。")
	return b.String(), true
}

// ─────────────────────────────────────────────────────────────
// Phase 6: Report
// ─────────────────────────────────────────────────────────────

const reportPrompt = `You are an executive technical writer creating a final project report.

## Task Profile
%s

## Selected Direction
**%s**: %s

## Review Results
%s

## Research Findings
%s

## Design Specification
%s

Write an executive summary (3-4 sentences) followed by a final report in Chinese structured as:

## 执行摘要
(3-4 sentences summarizing the recommendation)

## 1. 项目背景
(1 sentence)

## 2. 方案选择
(Brief rationale for the chosen direction)

## 3. 关键发现
(3-5 bullet points from research and review)

## 4. 设计概要
(Key design decisions, 3-5 bullet points)

## 5. 实施建议
(Next steps and timeline suggestion, 2-3 sentences)

Be thorough but concise. This is the final deliverable.`

func (o *HermesOrchestrator) generateReport(ctx context.Context, session *WorkflowSession) (string, error) {
	provider, model := o.getProvider()
	if model == "" {
		model = o.getModel()
	}

	tpStr, _ := json.Marshal(session.TaskProfile)
	dir := session.Directions[0]

	// Reviews
	var reviewSum strings.Builder
	reviewKey := "review_" + session.TaskID
	if reviewJSON, ok := session.TaskBoards[reviewKey]; ok {
		var reviews map[string]string
		_ = json.Unmarshal([]byte(reviewJSON), &reviews)
		for name, result := range reviews {
			snippet := result
			if len(snippet) > 80 {
				snippet = snippet[:80] + "..."
			}
			fmt.Fprintf(&reviewSum, "- **%s**: %s\\n", name, snippet)
		}
	}

	// Research
	var researchSum strings.Builder
	researchKey := "research_" + session.TaskID
	if researchJSON, ok := session.TaskBoards[researchKey]; ok {
		var stored struct {
			Results map[string]string `json:"results"`
		}
		_ = json.Unmarshal([]byte(researchJSON), &stored)
		for topic, result := range stored.Results {
			snippet := result
			if len(snippet) > 80 {
				snippet = snippet[:80] + "..."
			}
			fmt.Fprintf(&researchSum, "- **%s**: %s\\n", topic, snippet)
		}
	}

	// Design
	designKey := "design_" + session.TaskID
	design, _ := session.TaskBoards[designKey]

	prompt := fmt.Sprintf(reportPrompt,
		string(tpStr),
		dir.Label, dir.Description,
		reviewSum.String(),
		researchSum.String(),
		design,
	)

	ctx, cancel := context.WithTimeout(context.Background(), o.llmTimeout)
	defer cancel()

	resp, err := func() (*providers.LLMResponse, error) {
		if sp, ok := provider.(providers.StreamingProvider); ok {
			return sp.ChatStream(ctx, []providers.Message{
				{Role: "user", Content: prompt},
			}, nil, model, map[string]any{
				"temperature":    0.2,
				"thinking_level": "xhigh",
			}, nil)
		}
		return provider.Chat(ctx, []providers.Message{
			{Role: "user", Content: prompt},
		}, nil, model, map[string]any{
			"temperature":    0.2,
			"thinking_level": "xhigh",
		})
	}()
	if err != nil {
		return "", fmt.Errorf("report: %w", err)
	}

	o.logf("report response: content_len=%d reasoning_len=%d finish=%s",
		len(resp.Content), len(resp.ReasoningContent), resp.FinishReason)

	content := resp.Content
	if content == "" && resp.ReasoningContent != "" {
		content = resp.ReasoningContent
	}

	return content, nil
}

func (o *HermesOrchestrator) handlePhaseReport(ctx context.Context, convID string, session *WorkflowSession, msg string) (string, bool) {
	lower := toLower(msg)

	if isAbortCommand(lower) {
		_ = o.workflowStore.DeleteSession(ctx, convID)
		_ = o.modeStore.SetMode(ctx, convID, ModeChat)
		return "工作流已取消，已切换回聊天模式。", true
	}

	if lower == "修改" || lower == "回到" || lower == "回退" || lower == "revise" {
		session.Phase = PhaseDesign
		delete(session.TaskBoards, "report_"+session.TaskID)
		_ = o.workflowStore.SaveSession(ctx, session)
		return "已回到**详细设计**阶段。", true
	}

	reportKey := "report_" + session.TaskID
	if _, done := session.TaskBoards[reportKey]; !done {
		o.logf("report: generating final report")
		report, err := o.generateReport(ctx, session)
		if err != nil {
			o.logf("report error: %v", err)
			return "报告生成失败，请重试。", true
		}

		session.TaskBoards[reportKey] = report
		_ = o.workflowStore.SaveSession(ctx, session)

		dir := session.Directions[0]
		var b strings.Builder
		fmt.Fprintf(&b, "# 最终报告 — %s\n\n", dir.Label)
		b.WriteString(report)
		b.WriteString("\n\n---\n")
		b.WriteString("✅ 工作流完成。回复「修改」回退到设计，「完成」结束并切换回聊天模式。")
		return b.String(), true
	}

	if lower == "完成" || lower == "finish" || lower == "done" || lower == "结束" || isContinueCommand(lower) {
		_ = o.workflowStore.DeleteSession(ctx, convID)
		_ = o.modeStore.SetMode(ctx, convID, ModeChat)
		return "✅ Hermes 工作流已完成。已切换回聊天模式。感谢使用！", true
	}

	// Re-display
	report, _ := session.TaskBoards[reportKey]
	if report == "" {
		return "报告数据丢失，请回到设计阶段重新开始。", true
	}
	dir := session.Directions[0]
	var b strings.Builder
	fmt.Fprintf(&b, "# 最终报告 — %s\n\n", dir.Label)
	b.WriteString(report)
	b.WriteString("\n\n---\n")
	b.WriteString("回复「完成」结束工作流，「修改」回退到设计，「停」取消。")
	return b.String(), true
}

// ─────────────────────────────────────────────────────────────
// Command detection
// ─────────────────────────────────────────────────────────────

func isContinueCommand(lower string) bool {
	switch lower {
	case "继续", "yes", "ok", "好", "开始", "go", "next", "proceed", "确定", "是", "可以":
		return true
	}
	return false
}

func isAbortCommand(lower string) bool {
	switch lower {
	case "停", "停止", "取消", "stop", "cancel", "abort", "quit", "不", "否", "退出":
		return true
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

// ─────────────────────────────────────────────────────────────
// Logging
// ─────────────────────────────────────────────────────────────

func (o *HermesOrchestrator) logf(format string, args ...any) {
	logger.DebugCF("hermes", fmt.Sprintf(format, args...), nil)
}
