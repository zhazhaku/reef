package lht

import (
	"strings"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────────
// mockExecutorClient — simulates the client that executes tasks.
// ────────────────────────────────────────────────────────────

type mockExecutorClient struct {
	submitCalls []string
	submitOut   string
	submitErr   error
}

func (m *mockExecutorClient) SubmitTask(role, task string) (string, error) {
	m.submitCalls = append(m.submitCalls, task)
	if m.submitErr != nil {
		return "", m.submitErr
	}
	return m.submitOut, nil
}

// ────────────────────────────────────────────────────────────
// T4A.2.1 — submitSubtask 提交子任务执行
// ────────────────────────────────────────────────────────────

func TestSubmitSubtask(t *testing.T) {
	client := &mockExecutorClient{
		submitOut: "task completed: artifact written to artifacts/result.go",
	}

	task := TaskNode{
		TaskID:      "t-001",
		Description: "implement the user authentication module",
		State:       StateExecuting,
		MustHaves:   []string{"JWT token generation", "password hashing"},
		Rubric:      "all tests pass, no security vulnerabilities",
	}

	result, err := submitSubtask(client, &task)
	if err != nil {
		t.Fatalf("submitSubtask: %v", err)
	}
	if result == "" {
		t.Error("submitSubtask returned empty result")
	}
	if !strings.Contains(result, "artifact") {
		t.Errorf("result does not contain 'artifact': %q", result)
	}
	if len(client.submitCalls) != 1 {
		t.Fatalf("expected 1 submit call, got %d", len(client.submitCalls))
	}
	if !strings.Contains(client.submitCalls[0], task.Description) {
		t.Errorf("submit task description mismatch: %q", client.submitCalls[0])
	}
}

// T4A.2.2 — submitSubtask 错误传递
func TestSubmitSubtaskError(t *testing.T) {
	client := &mockExecutorClient{
		submitErr: &testError{"connection refused"},
	}
	task := TaskNode{TaskID: "t-002", Description: "broken task"}

	_, err := submitSubtask(client, &task)
	if err == nil {
		t.Fatal("expected error from submitSubtask")
	}
}

// ────────────────────────────────────────────────────────────
// T4A.2.3 — evalSubtask 评估通过 (PASS)
// ────────────────────────────────────────────────────────────

func TestEvalSubtaskPass(t *testing.T) {
	evalOutput := `{
  "verdict": "PASS",
  "issues": [],
  "score": 95
}`

	mustHaves := []string{"JWT token generation", "password hashing"}
	rubric := "all tests pass, no security vulnerabilities"

	record, err := evalSubtask(evalOutput, mustHaves, rubric)
	if err != nil {
		t.Fatalf("evalSubtask: %v", err)
	}
	if record.Verdict != "PASS" {
		t.Errorf("verdict = %q, want PASS", record.Verdict)
	}
	if record.Reviewer != "lht-eval" {
		t.Errorf("reviewer = %q, want lht-eval", record.Reviewer)
	}
	if record.Score != 95 {
		t.Errorf("score = %v, want 95", record.Score)
	}
	if len(record.Issues) != 0 {
		t.Errorf("expected 0 issues, got %d: %v", len(record.Issues), record.Issues)
	}
}

// T4A.2.4 — evalSubtask 评估失败 (FAIL)
func TestEvalSubtaskFail(t *testing.T) {
	evalOutput := `{
  "verdict": "FAIL",
  "issues": [
    {"id": "E001", "severity": "critical", "description": "no password hashing", "criterion": "password hashing"},
    {"id": "E002", "severity": "major", "description": "missing unit tests", "criterion": "all tests pass"}
  ],
  "score": 35
}`

	mustHaves := []string{"JWT token generation", "password hashing"}
	rubric := "all tests pass, no security vulnerabilities"

	record, err := evalSubtask(evalOutput, mustHaves, rubric)
	if err != nil {
		t.Fatalf("evalSubtask: %v", err)
	}
	if record.Verdict != "FAIL" {
		t.Errorf("verdict = %q, want FAIL", record.Verdict)
	}
	if record.Score != 35 {
		t.Errorf("score = %v, want 35", record.Score)
	}
	if len(record.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(record.Issues))
	}
}

// ────────────────────────────────────────────────────────────
// T4A.2.5 — 评审 Schema 解析器：基础解析
// ────────────────────────────────────────────────────────────

func TestParseEvalSchemaValid(t *testing.T) {
	input := `{"verdict": "PASS", "issues": [{"id": "E001", "severity": "minor", "description": "minor typo", "criterion": "readability"}], "score": 88}`

	result, err := ParseEvalSchema(input)
	if err != nil {
		t.Fatalf("ParseEvalSchema: %v", err)
	}
	if result.Verdict != "PASS" {
		t.Errorf("verdict = %q, want PASS", result.Verdict)
	}
	if result.Score != 88 {
		t.Errorf("score = %d, want 88", result.Score)
	}
	if len(result.Issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(result.Issues))
	}
}

// T4A.2.6 — 评审 Schema 解析器：解析失败
func TestParseEvalSchemaRetry(t *testing.T) {
	invalidInput := `not valid json at all {{{`

	_, err := ParseEvalSchema(invalidInput)
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
	if !strings.Contains(err.Error(), "parse") && !strings.Contains(err.Error(), "invalid") {
		t.Logf("error (might not mention retries in message): %v", err)
	}
}

// T4A.2.7 — 评审 Schema 解析器：缺少 verdict
func TestParseEvalSchemaMissingVerdict(t *testing.T) {
	input := `{"issues": [], "score": 90}`

	_, err := ParseEvalSchema(input)
	if err == nil {
		t.Fatal("expected error for missing verdict")
	}
	if !strings.Contains(err.Error(), "verdict") {
		t.Errorf("error should mention verdict: %v", err)
	}
}

// T4A.2.8 — 评审 Schema 解析器：verdict 非法值
func TestParseEvalSchemaInvalidVerdict(t *testing.T) {
	input := `{"verdict": "MAYBE", "issues": [], "score": 50}`

	_, err := ParseEvalSchema(input)
	if err == nil {
		t.Fatal("expected error for invalid verdict")
	}
}

// T4A.2.9 — EvalWithRetry：首次成功不重试
func TestEvalWithRetryFirstSuccess(t *testing.T) {
	callCount := 0
	mockEval := func() (string, error) {
		callCount++
		return `{"verdict": "PASS", "issues": [], "score": 100}`, nil
	}

	record, err := evalWithRetry(mockEval, []string{}, "")
	if err != nil {
		t.Fatalf("evalWithRetry: %v", err)
	}
	if record.Verdict != "PASS" {
		t.Errorf("verdict = %q", record.Verdict)
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retry needed)", callCount)
	}
}

// T4A.2.10 — EvalWithRetry：首次解析失败→重试 1 次成功
func TestEvalWithRetrySecondTrySucceeds(t *testing.T) {
	callCount := 0
	mockEval := func() (string, error) {
		callCount++
		if callCount == 1 {
			return "garbage not json", nil
		}
		return `{"verdict": "PASS", "issues": [], "score": 90}`, nil
	}

	record, err := evalWithRetry(mockEval, []string{}, "")
	if err != nil {
		t.Fatalf("evalWithRetry: %v", err)
	}
	if record.Verdict != "PASS" {
		t.Errorf("verdict = %q", record.Verdict)
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2 (1 retry)", callCount)
	}
}

// T4A.2.11 — EvalWithRetry：重试 2 次后仍失败→视为 FAIL
func TestEvalWithRetryExhausted(t *testing.T) {
	callCount := 0
	mockEval := func() (string, error) {
		callCount++
		return "always broken {{{", nil
	}

	record, err := evalWithRetry(mockEval, []string{}, "")
	if err != nil {
		t.Fatalf("evalWithRetry should not error on exhausted: %v", err)
	}
	if record.Verdict != "FAIL" {
		t.Errorf("verdict = %q, want FAIL after retries exhausted", record.Verdict)
	}
	if callCount != maxEvalRetries+1 {
		t.Errorf("callCount = %d, want %d (N+1 attempts)", callCount, maxEvalRetries+1)
	}
}

// ────────────────────────────────────────────────────────────
// T4A.2.12 — reviewSubtask: PASS
// ────────────────────────────────────────────────────────────

func TestReviewSubtaskPass(t *testing.T) {
	reviewOutput := `{
  "verdict": "PASS",
  "reviewer": "lht-rev",
  "summary": "产物质量良好，评估准确",
  "action_items": [],
  "confidence": 0.92
}`

	record, err := reviewSubtask(reviewOutput)
	if err != nil {
		t.Fatalf("reviewSubtask: %v", err)
	}
	if record.Verdict != "PASS" {
		t.Errorf("verdict = %q, want PASS", record.Verdict)
	}
	if record.Reviewer != "lht-rev" {
		t.Errorf("reviewer = %q, want lht-rev", record.Reviewer)
	}
	if record.Score < 90 {
		t.Errorf("score = %v, want >= 90 (confidence 0.92)", record.Score)
	}
}

// T4A.2.13 — reviewSubtask: FAIL 打回
func TestReviewSubtaskFail(t *testing.T) {
	reviewOutput := `{
  "verdict": "FAIL",
  "reviewer": "lht-rev",
  "summary": "存在质量问题",
  "action_items": ["补全单元测试", "修复空指针"],
  "confidence": 0.7
}`

	record, err := reviewSubtask(reviewOutput)
	if err != nil {
		t.Fatalf("reviewSubtask: %v", err)
	}
	if record.Verdict != "FAIL" {
		t.Errorf("verdict = %q, want FAIL", record.Verdict)
	}
	if len(record.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d: %v", len(record.Issues), record.Issues)
	}
}

// T4A.2.14 — reviewSubtask: UPGRADE 升级
func TestReviewSubtaskUpgrade(t *testing.T) {
	reviewOutput := `{
  "verdict": "UPGRADE",
  "reviewer": "lht-rev",
  "summary": "目标不可行，三方无法达成共识",
  "action_items": ["需要人工介入重新定义目标"],
  "confidence": 0.2
}`

	record, err := reviewSubtask(reviewOutput)
	if err != nil {
		t.Fatalf("reviewSubtask: %v", err)
	}
	if record.Verdict != "UPGRADE" {
		t.Errorf("verdict = %q, want UPGRADE", record.Verdict)
	}
}

// T4A.2.15 — 评审解析器 review schema 处理
func TestParseReviewSchema(t *testing.T) {
	input := `{
  "verdict": "PASS",
  "reviewer": "lht-rev",
  "summary": "good work",
  "action_items": [],
  "confidence": 0.95
}`

	result, err := ParseReviewSchema(input)
	if err != nil {
		t.Fatalf("ParseReviewSchema: %v", err)
	}
	if result.Verdict != "PASS" {
		t.Errorf("verdict = %q", result.Verdict)
	}
	if result.Reviewer != "lht-rev" {
		t.Errorf("reviewer = %q", result.Reviewer)
	}
}

// T4A.2.16 — finalApprove 终审
func TestFinalApprove(t *testing.T) {
	record, err := finalApprove("approved", "looks great")
	if err != nil {
		t.Fatalf("finalApprove: %v", err)
	}
	if record.Verdict != "APPROVED" {
		t.Errorf("verdict = %q, want APPROVED", record.Verdict)
	}
	if record.Reviewer != "user" {
		t.Errorf("reviewer = %q, want user", record.Reviewer)
	}
}

func TestFinalApproveReject(t *testing.T) {
	record, err := finalApprove("rejected", "needs more work on auth")
	if err != nil {
		t.Fatalf("finalApprove: %v", err)
	}
	if record.Verdict != "REJECTED" {
		t.Errorf("verdict = %q, want REJECTED", record.Verdict)
	}
}

// T4A.2.17 — 评审记录持久化（store.SaveReviewRecord）
func TestSaveReviewRecord(t *testing.T) {
	store := tempStore(t)
	goalID := "g-test-review"

	if err := store.EnsureTaskDirs(goalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}

	record := &ReviewRecord{
		ReviewRound: 1,
		Reviewer:    "lht-eval",
		Verdict:     "PASS",
		Score:       95,
		Issues:      nil,
	}

	if err := store.SaveReviewRecord(goalID, record); err != nil {
		t.Fatalf("SaveReviewRecord: %v", err)
	}

	loaded, err := store.LoadReviewRecords(goalID)
	if err != nil {
		t.Fatalf("LoadReviewRecords: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 review record, got %d", len(loaded))
	}
	if loaded[0].Verdict != "PASS" {
		t.Errorf("loaded verdict = %q, want PASS", loaded[0].Verdict)
	}
	if loaded[0].Score != 95 {
		t.Errorf("loaded score = %v, want 95", loaded[0].Score)
	}
	if loaded[0].Reviewer != "lht-eval" {
		t.Errorf("loaded reviewer = %q", loaded[0].Reviewer)
	}
}

// T4A.2.18 — 保存多次评审记录
func TestSaveMultipleReviewRecords(t *testing.T) {
	store := tempStore(t)
	goalID := "g-test-multi"

	if err := store.EnsureTaskDirs(goalID); err != nil {
		t.Fatalf("EnsureTaskDirs: %v", err)
	}

	for i := 1; i <= 3; i++ {
		record := &ReviewRecord{
			ReviewRound: i,
			Reviewer:    "lht-eval",
			Verdict:     "PASS",
			Score:       float64(90 + i),
		}
		if err := store.SaveReviewRecord(goalID, record); err != nil {
			t.Fatalf("SaveReviewRecord round %d: %v", i, err)
		}
	}

	loaded, err := store.LoadReviewRecords(goalID)
	if err != nil {
		t.Fatalf("LoadReviewRecords: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 records, got %d", len(loaded))
	}
	for _, rec := range loaded {
		if rec.Score != float64(90+rec.ReviewRound) {
			t.Errorf("round %d: score = %v, want %v", rec.ReviewRound, rec.Score, float64(90+rec.ReviewRound))
		}
	}
}

// ────────────────────────────────────────────────────────────
// T4A.2.19 — submitSubtask 含 must_haves 和 rubric
// ────────────────────────────────────────────────────────────

func TestSubmitSubtaskContext(t *testing.T) {
	client := &mockExecutorClient{
		submitOut: "done",
	}

	task := TaskNode{
		TaskID:      "t-ctx",
		Description: "build login page",
		MustHaves:   []string{"responsive design", "dark mode support"},
		Rubric:      "passes WCAG 2.1 AA accessibility",
	}

	_, err := submitSubtask(client, &task)
	if err != nil {
		t.Fatalf("submitSubtask: %v", err)
	}

	call := client.submitCalls[0]
	for _, mh := range task.MustHaves {
		if !strings.Contains(call, mh) {
			t.Errorf("submit call missing must_have %q in: %s", mh, call)
		}
	}
	if !strings.Contains(call, task.Rubric) {
		t.Errorf("submit call missing rubric %q in: %s", task.Rubric, call)
	}
}

// ────────────────────────────────────────────────────────────
// T4A.2.20 — evalSubtask 解析 Markdown 包装的 JSON
// ────────────────────────────────────────────────────────────

func TestParseEvalSchemaJSONInMarkdown(t *testing.T) {
	input := "```json\n{\"verdict\": \"PASS\", \"issues\": [], \"score\": 100}\n```"

	result, err := ParseEvalSchema(input)
	if err != nil {
		t.Fatalf("ParseEvalSchema with markdown fences: %v", err)
	}
	if result.Verdict != "PASS" {
		t.Errorf("verdict = %q", result.Verdict)
	}
}

// ────────────────────────────────────────────────────────────
// T4A.2.21 — extractJSON 更多变体
// ────────────────────────────────────────────────────────────

func TestExtractJSONVariants(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain", `{"a":1}`, `{"a":1}`},
		{"whitespace", `  {"a":1}  `, `{"a":1}`},
		{"fences", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"json_fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.expected {
				t.Errorf("extractJSON = %q, want %q", got, tt.expected)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────
// testError helper
// ────────────────────────────────────────────────────────────

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// Ensure time import is used.
var _ = time.Now
