package client

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// Gene generation prompt
// ---------------------------------------------------------------------------

// geneGenerationPromptTemplate is the system prompt instructing the LLM to produce a compact Gene.
const geneGenerationPromptTemplate = `You are a Gene evolution engine based on the Evolver framework (arXiv:2604.15097).
Your task is to generate a compact, reusable execution strategy (Gene) from observed task execution signals.

A Gene is a self-contained improvement rule with:
- strategy_name: A concise, human-readable name for this strategy (required)
- role: The agent role this gene applies to (e.g., "coder", "analyst", "tester")
- skills: Relevant skill tags (e.g., ["debugging", "code-review"])
- match_condition: A natural language description of WHEN this gene should activate
- control_signal: An ACTIONABLE set of commands, patterns, or heuristics an agent can follow. Must be ≤200 lines.
- failure_warnings: Up to 20 known failure patterns and how to avoid them

Rules:
1. ControlSignal must be actionable — specific commands, patterns, or heuristics an agent can follow.
2. Keep ControlSignal under 200 lines.
3. Include at most 20 FailureWarnings.
4. MatchCondition describes WHEN this gene should activate.
5. Output ONLY valid JSON with keys: strategy_name, role, skills, match_condition, control_signal, failure_warnings.

Example output format:
{
  "strategy_name": "retry_with_backoff",
  "role": "coder",
  "skills": ["debugging"],
  "match_condition": "Task fails with transient network error",
  "control_signal": "1. Wait 5 seconds\n2. Retry the last tool call\n3. If still failing, escalate to human",
  "failure_warnings": ["Do not retry more than 3 times consecutively"]
}`

// BuildGeneGenerationPrompt creates the system prompt for generating a new Gene from observed events.
func BuildGeneGenerationPrompt(events []*evolution.EvolutionEvent) string {
	return geneGenerationPromptTemplate
}

// ---------------------------------------------------------------------------
// Gene mutation prompt
// ---------------------------------------------------------------------------

// geneMutationPromptTemplate is the system prompt instructing the LLM to improve an existing Gene.
const geneMutationPromptTemplate = `You are refining an existing Gene based on new evidence.

Existing Gene:
%s

New signals:
%s

Improve the control_signal to better handle the observed patterns.
Preserve the strategy_name and role from the existing gene.
Output updated JSON with all keys: strategy_name, role, skills, match_condition, control_signal, failure_warnings.`

// maxExistingControlSignalChars is the maximum chars of existing ControlSignal to include
// in the mutation prompt to avoid token overflow.
const maxExistingControlSignalChars = 3000

// BuildGeneMutationPrompt creates the system prompt for mutating an existing Gene with new events.
// The existing gene's ControlSignal is truncated if necessary to avoid token overflow.
func BuildGeneMutationPrompt(existing *evolution.Gene, events []*evolution.EvolutionEvent) string {
	// Prepare existing gene JSON with truncated ControlSignal
	geneCopy := *existing
	if len(existing.ControlSignal) > maxExistingControlSignalChars {
		geneCopy.ControlSignal = existing.ControlSignal[:maxExistingControlSignalChars] + "\n[...truncated...]"
	}

	existingJSON, _ := json.MarshalIndent(geneCopy, "", "  ")

	// Build signals summary
	var signalSB strings.Builder
	for i, evt := range events {
		signalSB.WriteString(fmt.Sprintf("%d. [%s] %s", i+1, evt.EventType, evt.Signal))
		if evt.RootCause != "" {
			signalSB.WriteString(fmt.Sprintf(" (root cause: %s)", evt.RootCause))
		}
		signalSB.WriteString("\n")
	}

	return fmt.Sprintf(geneMutationPromptTemplate, string(existingJSON), signalSB.String())
}

// ---------------------------------------------------------------------------
// Root cause analysis prompt
// ---------------------------------------------------------------------------

// rootCausePromptTemplate is the prompt for the Observer LLM to perform root cause analysis on failures.
const rootCausePromptTemplate = `Analyze the following task execution failure and identify the root cause. Be concise (≤500 chars).

Task: %s

Error: %s

Last tool calls: %s`

// BuildRootCausePrompt creates a compact prompt for LLM-based root cause analysis.
// It is used by the Observer when EnableRootCause is set.
func BuildRootCausePrompt(taskInstruction, errMessage, toolSummary string) string {
	return fmt.Sprintf(rootCausePromptTemplate, taskInstruction, errMessage, toolSummary)
}
