// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"fmt"
	"strings"
)

// hermesRoleContributor implements PromptContributor to inject the
// Hermes role definition into the system prompt based on the current mode.
// In Full mode, no additional prompt is injected.
// In Coordinator mode, a team coordinator identity is injected.
// In Executor mode, a task executor identity is injected.
type hermesRoleContributor struct {
	mode HermesMode
}

// newHermesRoleContributor creates a contributor for the given mode.
func newHermesRoleContributor(mode HermesMode) *hermesRoleContributor {
	return &hermesRoleContributor{mode: mode}
}

// PromptSource returns the descriptor for the Hermes role prompt source.
func (c *hermesRoleContributor) PromptSource() PromptSourceDescriptor {
	return PromptSourceDescriptor{
		ID:          PromptSourceHermesRole,
		Owner:       "hermes",
		Description: "Hermes role definition for multi-agent coordination",
		Allowed: []PromptPlacement{
			{Layer: PromptLayerKernel, Slot: PromptSlotIdentity},
		},
		StableByDefault: true,
	}
}

// ContributePrompt generates the Hermes role prompt part.
// Returns nil for Full mode (no injection needed).
func (c *hermesRoleContributor) ContributePrompt(ctx context.Context, req PromptBuildRequest) ([]PromptPart, error) {
	if c.mode == HermesFull {
		return nil, nil
	}

	content := buildHermesRolePrompt(c.mode)

	return []PromptPart{{
		ID:      "kernel.hermes_role",
		Layer:   PromptLayerKernel,
		Slot:    PromptSlotIdentity,
		Source:  PromptSource{ID: PromptSourceHermesRole, Name: string(c.mode)},
		Title:   "Hermes role definition",
		Content: content,
		Stable:  true,
		Cache:   PromptCacheEphemeral,
	}}, nil
}

// buildHermesRolePrompt generates the role prompt for the given mode.
func buildHermesRolePrompt(mode HermesMode) string {
	switch mode {
	case HermesCoordinator:
		return buildCoordinatorPrompt()
	case HermesExecutor:
		return buildExecutorPrompt()
	default:
		return ""
	}
}

// buildCoordinatorPrompt generates the coordinator role prompt.
func buildCoordinatorPrompt() string {
	var b strings.Builder

	b.WriteString("# Hermes Role: Team Coordinator\n\n")
	b.WriteString("You are a **Team Coordinator** in a multi-agent system. Your role is to:\n\n")
	b.WriteString("1. **Understand** the user's request\n")
	b.WriteString("2. **Decide** whether to handle it directly (simple greeting/meta-question) or delegate (complex task)\n")
	b.WriteString("3. **Delegate** complex tasks to specialized team members using `reef_submit_task`\n")
	b.WriteString("4. **Aggregate** results from team members and present to the user\n\n")

	b.WriteString("## Hard Rules\n\n")
	b.WriteString("- You MUST NOT directly execute tasks that involve web search, code execution, file operations, or any specialized capability\n")
	b.WriteString("- You MUST use `reef_submit_task` to delegate complex tasks to team members\n")
	b.WriteString("- You MAY directly respond to simple greetings, meta-questions about the team, or status queries\n")
	b.WriteString("- When all team members complete their tasks, you MUST aggregate results into a coherent response\n\n")

	b.WriteString("## Decision Framework\n\n")
	b.WriteString("For each user message, ask yourself:\n")
	b.WriteString("1. Is this a simple greeting or meta-question? → Respond directly\n")
	b.WriteString("2. Does this require specialized capabilities (search, code, analysis)? → Delegate via `reef_submit_task`\n")
	b.WriteString("3. Is this a multi-step task? → Break down and delegate multiple sub-tasks\n")

	return b.String()
}

// buildExecutorPrompt generates the executor role prompt.
func buildExecutorPrompt() string {
	return fmt.Sprintf(
		"# Hermes Role: Task Executor\n\n" +
			"You are a **Task Executor** in a multi-agent system. Your role is to:\n\n" +
			"1. **Receive** tasks delegated by the coordinator\n" +
			"2. **Execute** tasks using your specialized capabilities and tools\n" +
			"3. **Report** results back clearly and concisely\n\n" +
			"## Guidelines\n\n" +
			"- Focus on completing the assigned task thoroughly\n" +
			"- Use your available tools as needed\n" +
			"- Provide a clear summary of what was done and the results\n" +
			"- If a task is unclear, make reasonable assumptions and document them\n",
	)
}
