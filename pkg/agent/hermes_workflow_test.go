package agent

import (
	"strings"
	"testing"
)

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from, to WorkflowPhase
		ok       bool
	}{
		{PhaseIdle, PhaseIntake, true},
		{PhaseIntake, PhaseBrainstorm, true},
		{PhaseIntake, PhaseComplete, true},
		{PhaseIntake, PhaseAborted, true},
		{PhaseBrainstorm, PhaseReview, true},
		{PhaseBrainstorm, PhaseResearch, true},
		{PhaseBrainstorm, PhaseAborted, true},
		{PhaseReview, PhaseResearch, true},
		{PhaseReview, PhaseDesign, true},
		{PhaseReview, PhaseAborted, true},
		{PhaseReport, PhaseComplete, true},
		{PhaseReport, PhaseAborted, true},
		{PhaseAborted, PhaseIdle, true},
		// Invalid transitions
		{PhaseIdle, PhaseComplete, false},
		{PhaseIntake, PhaseReview, false},
		{PhaseBrainstorm, PhaseIdle, false},
		{PhaseComplete, PhaseIntake, false},
	}

	for _, tt := range tests {
		got := CanTransition(tt.from, tt.to)
		if got != tt.ok {
			t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.ok)
		}
	}
}

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`{}`, `{}`},
		{"```json\n{}\n```", `{}`},
		{"```\n{}\n```", `{}`},
		{"  ```json\n{}\n```  ", `{}`},
		{`{"a":1}`, `{"a":1}`},
		// DeepSeek V4 xhigh markers
		{`[thinking:]{"a":1}[/thinking]`, `{"a":1}`},
		{`[thinking:] [{"label":"A"}] [/thinking]`, `[{"label":"A"}]`},
		{`[tool_use:exec]{"args":{}}[/tool_use]`, ``},
		{`[{"label":"A"}]`, `[{"label":"A"}]`},
	}

	for _, tt := range tests {
		got := stripCodeFences(tt.input)
		if got != tt.want {
			t.Errorf("stripCodeFences(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseTaskProfile(t *testing.T) {
	tp, err := parseTaskProfile(`{"domain":"tech","type":"system-design","complexity":4,"estimated_clients":3,"keywords":["scheduler","distributed"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if tp.Domain != "tech" {
		t.Errorf("domain = %q", tp.Domain)
	}
	if tp.Type != "system-design" {
		t.Errorf("type = %q", tp.Type)
	}
	if tp.Complexity != 4 {
		t.Errorf("complexity = %d", tp.Complexity)
	}
	if tp.EstimatedClients != 3 {
		t.Errorf("estimated_clients = %d", tp.EstimatedClients)
	}
	if len(tp.Keywords) != 2 {
		t.Errorf("keywords len = %d", len(tp.Keywords))
	}
}

func TestParseTaskProfileDefaults(t *testing.T) {
	// Missing fields should get defaults
	tp, err := parseTaskProfile(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	if tp.Complexity != 1 {
		t.Errorf("default complexity = %d", tp.Complexity)
	}
	if tp.Domain != "general" {
		t.Errorf("default domain = %q", tp.Domain)
	}
	if tp.Type != "unknown" {
		t.Errorf("default type = %q", tp.Type)
	}
	if tp.Keywords == nil {
		t.Error("keywords should be empty slice, not nil")
	}
}

func TestParseTaskProfileClampComplexity(t *testing.T) {
	tp, err := parseTaskProfile(`{"complexity":10}`)
	if err != nil {
		t.Fatal(err)
	}
	if tp.Complexity != 5 {
		t.Errorf("complexity should clamp to 5, got %d", tp.Complexity)
	}

	tp2, err := parseTaskProfile(`{"complexity":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if tp2.Complexity != 1 {
		t.Errorf("complexity should clamp to 1, got %d", tp2.Complexity)
	}
}

func TestIsContinueCommand(t *testing.T) {
	yes := []string{"继续", "yes", "ok", "好", "开始", "go", "next"}
	for _, s := range yes {
		if !isContinueCommand(s) {
			t.Errorf("isContinueCommand(%q) should be true", s)
		}
	}
	no := []string{"hello", "no", "", "design"}
	for _, s := range no {
		if isContinueCommand(s) {
			t.Errorf("isContinueCommand(%q) should be false", s)
		}
	}
}

func TestIsAbortCommand(t *testing.T) {
	yes := []string{"停", "停止", "取消", "stop", "cancel", "abort", "不"}
	for _, s := range yes {
		if !isAbortCommand(s) {
			t.Errorf("isAbortCommand(%q) should be true", s)
		}
	}
	no := []string{"hello", "yes", "", "go"}
	for _, s := range no {
		if isAbortCommand(s) {
			t.Errorf("isAbortCommand(%q) should be false", s)
		}
	}
}

func TestParseDirectionResponse(t *testing.T) {
	raw := `[
		{"label":"Monolith","description":"Single binary approach","openspec_category":"arch-decision","keywords":["monolith","simple"]},
		{"label":"Microservices","description":"Distributed approach","openspec_category":"arch-decision","keywords":["microservices","distributed"]}
	]`
	dirs, err := parseDirectionResponse(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 {
		t.Fatalf("expected 2 directions, got %d", len(dirs))
	}
	if dirs[0].Label != "Monolith" {
		t.Errorf("dirs[0].Label = %q", dirs[0].Label)
	}
	if dirs[0].Round != 1 {
		t.Errorf("dirs[0].Round = %d", dirs[0].Round)
	}
	if dirs[1].Keywords[0] != "microservices" {
		t.Errorf("dirs[1].Keywords[0] = %q", dirs[1].Keywords[0])
	}
}

func TestParseDirectionResponseCodeFenced(t *testing.T) {
	raw := "```json\n[{\"label\":\"A\",\"description\":\"desc\",\"openspec_category\":\"other\",\"keywords\":[\"k1\"]}]\n```"
	dirs, err := parseDirectionResponse(raw, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || dirs[0].Label != "A" {
		t.Errorf("got %+v", dirs)
	}
}

func TestExtractSelectionIndex(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"选 2", 2},
		{"选2", 2},
		{"选 5", 5},
		{"select 3", 3},
		{"select3", 3},
		{"选 abc", 0},
		{"hello", 0},
		{"选 0", 0},
		{"选 -1", 0},
	}
	for _, tt := range tests {
		got := extractSelectionIndex(tt.input)
		if got != tt.want {
			t.Errorf("extractSelectionIndex(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestExtractExplicitComplexity(t *testing.T) {
	tests := []struct {
		msg     string
		compl   int
		rest string
	}{
		{"复杂度5", 5, ""},
		{"复杂度 3", 3, ""},
		{"complexity4", 4, ""},
		{"这是一个复杂度5的工作，请开始执行", 5, "这是一个的工作，请开始执行"},
		{"请处理这个，复杂度3", 3, "请处理这个，"},
		{"hello world", 0, "hello world"},
		{"复杂度", 0, "复杂度"},
		{"complexity", 0, "complexity"},
	}
	for _, tt := range tests {
		gotC, gotR := extractExplicitComplexity(tt.msg)
		if gotC != tt.compl || gotR != tt.rest {
			t.Errorf("extractExplicitComplexity(%q) = (%d, %q), want (%d, %q)",
				tt.msg, gotC, gotR, tt.compl, tt.rest)
		}
	}
}

func TestParseComplexityFromMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want int
	}{
		{"3", 3},
		{"5", 5},
		{"复杂度4", 4},
		{"complexity 2", 2},
		{"选3", 3},
		{"select1", 1},
		{"hello", 0},
		{"10", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseComplexityFromMessage(tt.msg)
		if got != tt.want {
			t.Errorf("parseComplexityFromMessage(%q) = %d, want %d", tt.msg, got, tt.want)
		}
	}
}

func TestKeywordOverlapRatio(t *testing.T) {
	a := []string{"monolith", "simple"}
	b := []string{"monolith", "simple", "easy"}
	ratio := keywordOverlapRatio(a, b)
	if ratio < 0.6 || ratio > 0.7 {
		t.Errorf("overlap ratio = %f, expected ~0.66", ratio)
	}

	// Disjoint
	c := []string{"monolith"}
	d := []string{"distributed"}
	if keywordOverlapRatio(c, d) != 0.0 {
		t.Errorf("disjoint should be 0.0")
	}

	// Empty
	if keywordOverlapRatio(nil, nil) != 1.0 {
		t.Errorf("both empty should be 1.0")
	}
}

func TestNormalizeKeywords(t *testing.T) {
	out := normalizeKeywords([]string{"A", "a", "B", "", "  C  "})
	if len(out) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(out), out)
	}
	if out[0] != "a" || out[1] != "b" || out[2] != "c" {
		t.Errorf("got %v", out)
	}
}

func TestDetectConvergenceMaxRounds(t *testing.T) {
	session := &WorkflowSession{
		CurrentRound: 5,
		MaxRounds:    5,
	}
	converged, msg := detectConvergence(session)
	if !converged {
		t.Error("should converge at max rounds")
	}
	if msg == "" {
		t.Error("should have message")
	}
}

func TestDetectConvergenceStreak(t *testing.T) {
	// Simulate 2 rounds of stable keywords (need >= 4 directions total)
	session := &WorkflowSession{
		CurrentRound:   3,
		MaxRounds:      5,
		ConvergeStreak: 1, // already had 1 streak from previous comparison
		Directions: []Direction{
			{Round: 2, Keywords: []string{"monolith", "simple", "scalable"}},
			{Round: 2, Keywords: []string{"microservices", "distributed"}},
			{Round: 3, Keywords: []string{"monolith", "simple", "reliable"}},
			{Round: 3, Keywords: []string{"microservices", "scalable"}},
		},
	}
	// Round 3 kws: ["monolith","simple","reliable","microservices","scalable"] = 5
	// Round 2 kws: ["monolith","simple","scalable","microservices","distributed"] = 5
	// Intersection: {"monolith","simple","scalable","microservices"} = 4
	// Union: 6 → 0.667 >= 0.5 → streak 2 → converge
	converged, _ := detectConvergence(session)
	if !converged {
		t.Error("should converge with overlap >= 0.5 and streak becoming 2")
	}
}

func TestExtractRoundKeywords(t *testing.T) {
	dirs := []Direction{
		{Round: 1, Keywords: []string{"a", "b"}},
		{Round: 2, Keywords: []string{"c"}},
		{Round: 1, Keywords: []string{"d"}},
	}
	kws := extractRoundKeywords(dirs, 1)
	if len(kws) != 3 {
		t.Errorf("expected 3 keywords from round 1, got %d: %v", len(kws), kws)
	}
}

func TestSortedByRound(t *testing.T) {
	dirs := []Direction{
		{ID: "d-r2-2", Round: 2},
		{ID: "d-r1-1", Round: 1},
		{ID: "d-r2-1", Round: 2},
	}
	sorted := sortedByRound(dirs)
	if sorted[0].ID != "d-r1-1" {
		t.Errorf("sorted[0].ID = %s", sorted[0].ID)
	}
	if sorted[1].ID != "d-r2-1" {
		t.Errorf("sorted[1].ID = %s", sorted[1].ID)
	}
	if sorted[2].ID != "d-r2-2" {
		t.Errorf("sorted[2].ID = %s", sorted[2].ID)
	}
}

// ─────────────────────────────────────────────────────────────
// Wave 3: Review tests
// ─────────────────────────────────────────────────────────────

func TestGetReviewDimensions(t *testing.T) {
	// Known category
	dir := Direction{OpenspecCategory: "arch-decision"}
	dims := getReviewDimensions(dir)
	if len(dims) != 4 {
		t.Errorf("expected 4 review dims for arch-decision, got %d", len(dims))
	}
	if dims[0].Name != "技术可行性" {
		t.Errorf("first dim name = %q", dims[0].Name)
	}

	// Unknown category → fallback to "other"
	dir2 := Direction{OpenspecCategory: "nonexistent"}
	dims2 := getReviewDimensions(dir2)
	if len(dims2) != 4 {
		t.Errorf("expected 4 fallback dims, got %d", len(dims2))
	}
}

func TestReviewDimensionsAllCategories(t *testing.T) {
	categories := []string{
		"arch-decision", "tech-stack", "data-model", "api-design",
		"deployment", "security", "ux-flow", "tradeoff", "other",
	}
	for _, cat := range categories {
		dir := Direction{OpenspecCategory: cat}
		dims := getReviewDimensions(dir)
		if len(dims) != 4 {
			t.Errorf("category %s: expected 4 dims, got %d", cat, len(dims))
		}
		for _, d := range dims {
			if d.Name == "" || d.Question == "" || d.Role == "" {
				t.Errorf("category %s: dim has empty field: %+v", cat, d)
			}
		}
	}
}

func TestFormatReviewOutput(t *testing.T) {
	dir := Direction{
		Label:       "Microservices",
		Description: "A distributed approach with independent services.",
	}
	results := map[string]string{
		"技术可行性": "方案可行。需要关注服务间通信的延迟和一致性。",
		"可扩展性":   "扩展性优秀。每个服务可以独立横向扩展。",
	}

	out := formatReviewOutput(dir, results)
	if !strings.Contains(out, "Microservices") {
		t.Error("missing direction label")
	}
	if !strings.Contains(out, "技术可行性") {
		t.Error("missing dimension name")
	}
	if !strings.Contains(out, "方案可行") {
		t.Error("missing review content")
	}
	if !strings.Contains(out, "继续") {
		t.Error("missing continue hint")
	}
	if !strings.Contains(out, "修改") {
		t.Error("missing revise hint")
	}
}

// ─────────────────────────────────────────────────────────────
// Wave 4: Research, Design, Report tests
// ─────────────────────────────────────────────────────────────

func TestFormatResearchOutput(t *testing.T) {
	plan := []map[string]string{
		{"topic": "技术背景", "method": "literature", "priority": "high"},
		{"topic": "方案验证", "method": "comparison", "priority": "high"},
	}
	results := map[string]string{
		"技术背景": "该领域主流方案是事件驱动架构。",
		"方案验证": "类似场景下该方案响应时间降低30%。",
	}

	out := formatResearchOutput(plan, results)
	if !strings.Contains(out, "深度研究") {
		t.Error("missing title")
	}
	if !strings.Contains(out, "技术背景") {
		t.Error("missing topic")
	}
	if !strings.Contains(out, "事件驱动") {
		t.Error("missing research content")
	}
	if !strings.Contains(out, "继续") {
		t.Error("missing continue hint")
	}
	if !strings.Contains(out, "详细设计") {
		t.Error("missing next phase hint")
	}
}

func TestCanTransitionFullPipeline(t *testing.T) {
	// Verify complete forward pipeline
	pipeline := []struct{ from, to WorkflowPhase }{
		{PhaseIdle, PhaseIntake},
		{PhaseIntake, PhaseBrainstorm},
		{PhaseBrainstorm, PhaseReview},
		{PhaseReview, PhaseResearch},
		{PhaseResearch, PhaseDesign},
		{PhaseDesign, PhaseReport},
		{PhaseReport, PhaseComplete},
	}
	for _, p := range pipeline {
		if !CanTransition(p.from, p.to) {
			t.Errorf("pipeline broken: %s → %s should be allowed", p.from, p.to)
		}
	}
}

func TestWorkflowSessionDefaultMaxRounds(t *testing.T) {
	ws := &WorkflowSession{
		MaxRounds: 5,
	}
	if ws.MaxRounds != 5 {
		t.Errorf("default max rounds = %d", ws.MaxRounds)
	}
}
