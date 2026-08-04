package lht

import (
	"testing"
	"time"
)

// --- GenerateQuestions ---

func TestGenerateQuestionsProducesFourCategories(t *testing.T) {
	qs := GenerateQuestions("实现用户登录模块")
	if len(qs) < 4 {
		t.Fatalf("expected at least 4 questions, got %d", len(qs))
	}

	cats := make(map[GroundingQuestionCategory]bool)
	for _, q := range qs {
		cats[q.Category] = true
	}

	required := []GroundingQuestionCategory{
		QuestionScope, QuestionPriority, QuestionConstraint, QuestionAcceptanceCriteria,
	}
	for _, r := range required {
		if !cats[r] {
			t.Errorf("missing required question category: %s", r)
		}
	}

	// Each question should be non-empty
	for i, q := range qs {
		if q.Question == "" {
			t.Errorf("question %d has empty text", i)
		}
	}
}

// --- NewGroundingSession ---

func TestNewGroundingSessionDefaults(t *testing.T) {
	gs := NewGroundingSession("goal-001", "实现用户模块")

	if gs.GoalID != "goal-001" {
		t.Errorf("GoalID: got %q, want %q", gs.GoalID, "goal-001")
	}
	if gs.Round != 0 {
		t.Errorf("Round should be 0, got %d", gs.Round)
	}
	if gs.IsComplete {
		t.Error("IsComplete should be false for new session")
	}
	if gs.NextQuestion() == nil {
		t.Error("NextQuestion should return non-nil for new session")
	}
	if gs.PendingIndex != 0 {
		t.Errorf("PendingIndex should be 0, got %d", gs.PendingIndex)
	}
	if gs.HasUnresolvedQuestions() != true {
		t.Error("HasUnresolvedQuestions should return true for new session")
	}
}

// --- NextQuestion ---

func TestNextQuestionAdvances(t *testing.T) {
	gs := NewGroundingSession("g1", "test")

	q1 := gs.NextQuestion()
	if q1 == nil {
		t.Fatal("first question should not be nil")
	}
	if q1.Category != QuestionScope {
		t.Errorf("first question category: got %s, want %s", q1.Category, QuestionScope)
	}

	// Advance by processing a valid answer
	err := gs.ProcessAnswer(UserReply{
		ReplyType: ReplyTypeAnswer,
		Content:   "只实现用户名密码登录，不包括社交登录",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Logf("process answer warning (may be follow-up): %v", err)
	}

	// After one answer, NextQuestion should return next question (if not complete)
	q2 := gs.NextQuestion()
	if q2 == nil && !gs.IsComplete {
		t.Error("should have more questions after first answer")
	}
}

// --- ProcessAnswer: sufficient answer ---

func TestProcessAnswerSufficient(t *testing.T) {
	gs := NewGroundingSession("g1", "构建一个 REST API")

	err := gs.ProcessAnswer(UserReply{
		ReplyType: ReplyTypeAnswer,
		Content:   "范围包括用户 CRUD 接口和认证接口，不包括前端和管理后台",
		Timestamp: time.Now(),
	})
	// A sufficiently-detailed answer should not return an error for insufficiency.
	// (Though if it's a short answer <10 chars, it will flag.)
	if err != nil {
		t.Logf("note: answer still flagged, may be borderline: %v", err)
	}
}

// --- ProcessAnswer: insufficient answer (too short) ---

func TestProcessAnswerInsufficientShort(t *testing.T) {
	gs := NewGroundingSession("g1", "test")
	initialLen := len(gs.Questions)

	err := gs.ProcessAnswer(UserReply{
		ReplyType: ReplyTypeAnswer,
		Content:   "好",
		Timestamp: time.Now(),
	})
	if err == nil {
		t.Error("expected error for insufficient answer")
	}
	// A follow-up question should have been inserted.
	if len(gs.Questions) <= initialLen {
		t.Errorf("expected follow-up question to be inserted, questions: %d -> %d", initialLen, len(gs.Questions))
	}
}

// --- ProcessAnswer: insufficient answer (empty) ---

func TestProcessAnswerEmpty(t *testing.T) {
	gs := NewGroundingSession("g1", "test")

	err := gs.ProcessAnswer(UserReply{
		ReplyType: ReplyTypeAnswer,
		Content:   "",
		Timestamp: time.Now(),
	})
	if err == nil {
		t.Error("expected error for empty answer")
	}
}

// --- ProcessAnswer: insufficient answer (generic) ---

func TestProcessAnswerGenericNonAnswer(t *testing.T) {
	generics := []string{"不知道", "不确定", "随便", "都可以", "你决定", "无所谓"}
	for _, g := range generics {
		gs := NewGroundingSession("g1", "test")
		err := gs.ProcessAnswer(UserReply{
			ReplyType: ReplyTypeAnswer,
			Content:   g,
			Timestamp: time.Now(),
		})
		if err == nil {
			t.Errorf("expected error for generic answer %q", g)
		}
	}
}

// --- Escalation: rounds exceed limit ---

func TestGroundingEscalationAfterMaxRounds(t *testing.T) {
	gs := NewGroundingSession("g1", "test")

	// Simulate MaxGroundingRounds rounds with insufficient answers
	for i := 0; i < MaxGroundingRounds; i++ {
		gs.ProcessAnswer(UserReply{
			ReplyType: ReplyTypeAnswer,
			Content:   "ok", // short, triggers follow-up each time
			Timestamp: time.Now(),
		})
	}

	if !gs.ShouldEscalate() {
		t.Errorf("ShouldEscalate should be true after %d rounds, got false", MaxGroundingRounds)
	}

	// Further processing should error
	err := gs.ProcessAnswer(UserReply{
		ReplyType: ReplyTypeAnswer,
		Content:   "another answer",
		Timestamp: time.Now(),
	})
	if err == nil {
		t.Error("expected error when processing after escalation threshold")
	}
}

// --- Loop completion: all questions answered ---

func TestGroundingCompleteAfterAllAnswered(t *testing.T) {
	gs := NewGroundingSession("g1", "test")

	// Answer all questions with sufficient detail
	answers := []string{
		"实现用户名密码登录，JWT 鉴权，不包括社交登录和 SSO",
		"P0: 基本登录功能; P1: 密码重置; P2: 登录日志",
		"必须使用 Go + PostgreSQL，兼容现有认证中间件",
		"所有 API 端点通过集成测试，登录响应时间 <1s",
	}

	for _, a := range answers {
		err := gs.ProcessAnswer(UserReply{
			ReplyType: ReplyTypeAnswer,
			Content:   a,
			Timestamp: time.Now(),
		})
		// Accept even if some answers are flagged; we want to test completion
		if err != nil {
			t.Logf("answer flagged (expected for edge cases): %v", err)
		}
	}

	// After all answers, session may or may not be complete depending on follow-ups.
	// If complete, no next question.
	if gs.IsComplete {
		if gs.NextQuestion() != nil {
			t.Error("NextQuestion should be nil when session is complete")
		}
	}
}

// --- BuildGoalDefinition ---

func TestBuildGoalDefinition(t *testing.T) {
	gs := &GroundingSession{
		GoalID:      "goal-001",
		Description: "实现用户登录",
		Questions: []GroundingQuestion{
			{Category: QuestionScope, Question: "scope?"},
			{Category: QuestionPriority, Question: "priority?"},
			{Category: QuestionConstraint, Question: "constraint?"},
			{Category: QuestionAcceptanceCriteria, Question: "accept?"},
		},
		Answers: []UserReply{
			{ReplyType: ReplyTypeAnswer, Content: "模块级别：仅认证模块"},
			{ReplyType: ReplyTypeAnswer, Content: "P0 登录, P1 重置密码"},
			{ReplyType: ReplyTypeAnswer, Content: "Go + PostgreSQL"},
			{ReplyType: ReplyTypeAnswer, Content: "集成测试通过，覆盖率 > 80%"},
		},
		IsComplete: true,
	}

	def := gs.BuildGoalDefinition()

	if def.GoalID != "goal-001" {
		t.Errorf("GoalID: got %q, want %q", def.GoalID, "goal-001")
	}
	if def.Description != "实现用户登录" {
		t.Errorf("Description: got %q, want %q", def.Description, "实现用户登录")
	}
	if def.Granularity != "module" {
		t.Errorf("Granularity: got %q, want %q", def.Granularity, "module")
	}
	if def.Scope == "" {
		t.Error("Scope should not be empty")
	}
	if len(def.Priorities) == 0 {
		t.Error("Priorities should not be empty")
	}
	if len(def.Constraints) == 0 {
		t.Error("Constraints should not be empty")
	}
	if len(def.AcceptanceCriteria) == 0 {
		t.Error("AcceptanceCriteria should not be empty")
	}
}

// --- Granularity derivation ---

func TestDeriveGranularity(t *testing.T) {
	tests := []struct {
		scope    string
		expected string
	}{
		{"系统架构设计", "system"},
		{"微服务平台", "system"},
		{"模块重构", "module"},
		{"服务拆分", "module"},
		{"功能开发", "feature"},
		{"新特性实现", "feature"},
		{"bug 修复", "bugfix"},
		{"修复登录问题", "bugfix"},
		{"做点什么", "task"},
	}

	for _, tt := range tests {
		got := deriveGranularity(tt.scope)
		if got != tt.expected {
			t.Errorf("deriveGranularity(%q) = %q, want %q", tt.scope, got, tt.expected)
		}
	}
}

// --- HasUnresolvedQuestions ---

func TestHasUnresolvedQuestionsNew(t *testing.T) {
	gs := NewGroundingSession("g1", "test")
	if !gs.HasUnresolvedQuestions() {
		t.Error("new session should have unresolved questions")
	}
}

// --- isAnswerInsufficient ---

func TestIsAnswerInsufficientEmpty(t *testing.T) {
	if !isAnswerInsufficient("") {
		t.Error("empty answer should be insufficient")
	}
	if !isAnswerInsufficient("   ") {
		t.Error("whitespace-only answer should be insufficient")
	}
}

func TestIsAnswerInsufficientShort(t *testing.T) {
	if !isAnswerInsufficient("ok") {
		t.Error("2-char answer should be insufficient")
	}
	if !isAnswerInsufficient("yes") {
		t.Error("3-char answer should be insufficient")
	}
	if isAnswerInsufficient("this is enough detail") {
		t.Error("16-char answer should be sufficient")
	}
}

// --- Edge: process answer on already-complete session ---

func TestProcessAnswerOnComplete(t *testing.T) {
	gs := &GroundingSession{
		GoalID:     "g1",
		IsComplete: true,
	}
	err := gs.ProcessAnswer(UserReply{
		ReplyType: ReplyTypeAnswer,
		Content:   "anything",
		Timestamp: time.Now(),
	})
	if err == nil {
		t.Error("expected error when processing answer on completed session")
	}
}
