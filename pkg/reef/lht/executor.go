// Package lht — sub-task executor for LHT ENGINE.
//
// Implements W4A: submitSubtask (Generator), evalSubtask (Evaluator),
// reviewSubtask (Reviewer), finalApprove (User), and review schema parser
// with ≤2 retries for invalid output.
package lht

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ────────────────────────────────────────────────────────────
// ExecutorClient — abstraction for executing tasks via reef.
// ────────────────────────────────────────────────────────────

// ExecutorClient abstracts the task submission interface.
// In production this wraps reef_submit_task; mock implementations are used in tests.
type ExecutorClient interface {
	SubmitTask(role, taskDescription string) (string, error)
}

// ────────────────────────────────────────────────────────────
// EvalResult — parsed evaluation schema (Evaluator output).
// ────────────────────────────────────────────────────────────

// EvalIssue represents a single issue found during evaluation.
type EvalIssue struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"` // "critical", "major", "minor"
	Description string `json:"description"`
	Criterion   string `json:"criterion"`
}

// EvalResult is the structured output of an Evaluator.
type EvalResult struct {
	Verdict string      `json:"verdict"` // "PASS" or "FAIL"
	Issues  []EvalIssue `json:"issues"`
	Score   int         `json:"score"` // 0-100
}

// ────────────────────────────────────────────────────────────
// ReviewResult — parsed review schema (Reviewer output).
// ────────────────────────────────────────────────────────────

// ReviewResult is the structured output of a Reviewer.
type ReviewResult struct {
	Verdict     string   `json:"verdict"` // "PASS", "FAIL", "UPGRADE"
	Reviewer    string   `json:"reviewer"`
	Summary     string   `json:"summary"`
	ActionItems []string `json:"action_items"`
	Confidence  float64  `json:"confidence"`
}

// ────────────────────────────────────────────────────────────
// Max retry constant for schema parsing.
// ────────────────────────────────────────────────────────────

// maxEvalRetries is the maximum number of retries when parsing
// the Evaluator's output. After N+1 total attempts, treat as FAIL.
const maxEvalRetries = 2

// ────────────────────────────────────────────────────────────
// submitSubtask — submits a subtask to the Generator client.
// ────────────────────────────────────────────────────────────

// submitSubtask sends a TaskNode to the Generator (lht-gen) for execution.
// The task description includes MustHaves and Rubric for context.
func submitSubtask(client ExecutorClient, task *TaskNode) (string, error) {
	taskDesc := buildTaskDescription(task)
	result, err := client.SubmitTask("lht-gen", taskDesc)
	if err != nil {
		return "", fmt.Errorf("submitSubtask %s: %w", task.TaskID, err)
	}
	return result, nil
}

// buildTaskDescription formats a TaskNode into a prompt for the Generator.
func buildTaskDescription(task *TaskNode) string {
	var b strings.Builder
	b.WriteString(task.Description)
	b.WriteString("\n\n验收标准 (MustHaves):\n")
	for _, mh := range task.MustHaves {
		b.WriteString("- " + mh + "\n")
	}
	if task.Rubric != "" {
		b.WriteString("\n评分标准 (Rubric):\n")
		b.WriteString(task.Rubric)
	}
	return b.String()
}

// ────────────────────────────────────────────────────────────
// evalSubtask — evaluates a subtask's output via Evaluator.
// ────────────────────────────────────────────────────────────

// evalSubtask takes the raw output from an Evaluator run and parses it into
// a ReviewRecord.  If the output cannot be parsed, it returns an error.
// Use evalWithRetry for automatic retry on parse failure.
func evalSubtask(evalOutput string, mustHaves []string, rubric string) (*ReviewRecord, error) {
	result, err := ParseEvalSchema(evalOutput)
	if err != nil {
		return nil, fmt.Errorf("evalSubtask parse: %w", err)
	}

	return evalResultToRecord(result), nil
}

// evalResultToRecord converts an EvalResult to a model ReviewRecord.
// EvalIssues are serialized as JSON strings per issue.
func evalResultToRecord(result *EvalResult) *ReviewRecord {
	issues := make([]string, len(result.Issues))
	for i, iss := range result.Issues {
		b, _ := json.Marshal(iss)
		issues[i] = string(b)
	}
	return &ReviewRecord{
		ReviewRound: 0, // filled in by engine
		Reviewer:    "lht-eval",
		Verdict:     result.Verdict,
		Score:       float64(result.Score),
		Issues:      issues,
	}
}

// ────────────────────────────────────────────────────────────
// evalWithRetry — evaluate with ≤2 retries on parse failure.
// ────────────────────────────────────────────────────────────

// evalWithRetry calls the eval function up to maxEvalRetries+1 times.
// If all attempts fail to produce a valid schema, the result is treated as FAIL
// and a synthetic ReviewRecord is returned with score=0.
//
// evalFn is a function that runs the Evaluator and returns its raw output.
// It is called once per attempt.
func evalWithRetry(evalFn func() (string, error), mustHaves []string, rubric string) (*ReviewRecord, error) {
	var lastErr error

	for attempt := 0; attempt <= maxEvalRetries; attempt++ {
		output, err := evalFn()
		if err != nil {
			lastErr = err
			continue
		}

		result, parseErr := ParseEvalSchema(output)
		if parseErr == nil {
			rec := evalResultToRecord(result)
			rec.ReviewRound = attempt + 1
			return rec, nil
		}
		lastErr = parseErr
	}

	// All attempts exhausted — treat as FAIL.
	return &ReviewRecord{
		Reviewer:  "lht-eval",
		Verdict:   VerdictFail,
		Score:     0,
		Issues:    []string{fmt.Sprintf(`{"id":"E-PARSE","severity":"critical","description":"Evaluator output unparseable after %d attempts: %v"}`, maxEvalRetries+1, lastErr)},
		ReviewRound: maxEvalRetries + 1,
	}, nil
}

// ────────────────────────────────────────────────────────────
// ParseEvalSchema — parse Evaluator JSON output.
// ────────────────────────────────────────────────────────────

// ParseEvalSchema parses the Evaluator's structured output.
// It handles:
//   - Plain JSON: {"verdict": "PASS", ...}
//   - JSON wrapped in markdown code fences: ```json\n{...}\n```
//   - Text with leading/trailing whitespace
//
// Returns an error if the verdict is missing or invalid ("PASS"/"FAIL" only).
func ParseEvalSchema(input string) (*EvalResult, error) {
	cleaned := extractJSON(input)

	var result EvalResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parse eval schema: %w (input: %.100s)", err, cleaned)
	}

	if result.Verdict == "" {
		return nil, fmt.Errorf("parse eval schema: missing verdict field")
	}
	if result.Verdict != "PASS" && result.Verdict != "FAIL" {
		return nil, fmt.Errorf("parse eval schema: invalid verdict %q (must be PASS or FAIL)", result.Verdict)
	}

	return &result, nil
}

// ────────────────────────────────────────────────────────────
// reviewSubtask — Reviewer verdict parsing.
// ────────────────────────────────────────────────────────────

// reviewSubtask parses the Reviewer's structured output into a ReviewRecord.
func reviewSubtask(reviewOutput string) (*ReviewRecord, error) {
	result, err := ParseReviewSchema(reviewOutput)
	if err != nil {
		return nil, fmt.Errorf("reviewSubtask parse: %w", err)
	}

	return reviewResultToRecord(result), nil
}

func reviewResultToRecord(result *ReviewResult) *ReviewRecord {
	// Store action items as issues.
	issues := make([]string, len(result.ActionItems))
	copy(issues, result.ActionItems)

	return &ReviewRecord{
		Reviewer: result.Reviewer,
		Verdict:  result.Verdict,
		Score:    result.Confidence * 100, // map 0-1 confidence to 0-100 score
		Issues:   issues,
	}
}

// ────────────────────────────────────────────────────────────
// ParseReviewSchema — parse Reviewer JSON output.
// ────────────────────────────────────────────────────────────

// ParseReviewSchema parses the Reviewer's structured output.
// Accepted verdicts: "PASS", "FAIL", "UPGRADE".
func ParseReviewSchema(input string) (*ReviewResult, error) {
	cleaned := extractJSON(input)

	var result ReviewResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parse review schema: %w (input: %.100s)", err, cleaned)
	}

	if result.Verdict == "" {
		return nil, fmt.Errorf("parse review schema: missing verdict field")
	}
	if result.Verdict != "PASS" && result.Verdict != "FAIL" && result.Verdict != "UPGRADE" {
		return nil, fmt.Errorf("parse review schema: invalid verdict %q (must be PASS, FAIL, or UPGRADE)", result.Verdict)
	}
	if result.Reviewer == "" {
		result.Reviewer = "lht-rev"
	}

	return &result, nil
}

// ────────────────────────────────────────────────────────────
// finalApprove — user final approval record.
// ────────────────────────────────────────────────────────────

// finalApprove creates a ReviewRecord for the user's final approval decision.
// action is "approved" or "rejected"; comment is the user's feedback.
func finalApprove(action, comment string) (*ReviewRecord, error) {
	var verdict string
	if action == "approved" {
		verdict = "APPROVED"
	} else if action == "rejected" {
		verdict = "REJECTED"
	} else {
		return nil, fmt.Errorf("finalApprove: invalid action %q (must be 'approved' or 'rejected')", action)
	}

	return &ReviewRecord{
		Reviewer: "user",
		Verdict:  verdict,
		Issues:   []string{comment},
		Score:    100,
	}, nil
}

// ────────────────────────────────────────────────────────────
// extractJSON — helper to extract JSON from various formats.
// ────────────────────────────────────────────────────────────

// extractJSON attempts to extract a JSON object from text that may include
// markdown code fences, leading/trailing whitespace, or surrounding text.
func extractJSON(input string) string {
	// Pre-allocate a strings.Builder for the result — small N, use simple ops.
	s := strings.TrimSpace(input)

	// Try to extract from ```json ... ``` or ``` ... ``` fences.
	if strings.HasPrefix(s, "```") {
		rest := s[3:]

		// Skip language hint like "json" after opening fence.
		if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
			rest = rest[idx+1:]
		}

		// Find the closing ```.
		if end := strings.LastIndex(rest, "```"); end >= 0 {
			s = rest[:end]
		} else {
			s = rest
		}
	}

	return strings.TrimSpace(s)
}

// Ensure time import is used.
var _ = time.Now
