// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"context"
	"strings"
	"testing"
)

func TestHermesRoleContributor_FullMode(t *testing.T) {
	c := newHermesRoleContributor(HermesFull)

	// Full mode should not contribute any prompt
	parts, err := c.ContributePrompt(context.Background(), PromptBuildRequest{})
	if err != nil {
		t.Fatalf("ContributePrompt error: %v", err)
	}
	if len(parts) != 0 {
		t.Errorf("Full mode should contribute 0 parts, got %d", len(parts))
	}
}

func TestHermesRoleContributor_CoordinatorMode(t *testing.T) {
	c := newHermesRoleContributor(HermesCoordinator)

	parts, err := c.ContributePrompt(context.Background(), PromptBuildRequest{})
	if err != nil {
		t.Fatalf("ContributePrompt error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("Coordinator mode should contribute 1 part, got %d", len(parts))
	}

	part := parts[0]

	// Check part properties
	if part.Layer != PromptLayerKernel {
		t.Errorf("Layer = %q, want %q", part.Layer, PromptLayerKernel)
	}
	if part.Slot != PromptSlotIdentity {
		t.Errorf("Slot = %q, want %q", part.Slot, PromptSlotIdentity)
	}
	if part.Source.ID != PromptSourceHermesRole {
		t.Errorf("Source.ID = %q, want %q", part.Source.ID, PromptSourceHermesRole)
	}
	if !part.Stable {
		t.Error("Part should be stable")
	}

	// Check content contains key phrases
	content := part.Content
	if !strings.Contains(content, "Team Coordinator") {
		t.Error("Coordinator prompt should contain 'Team Coordinator'")
	}
	if !strings.Contains(content, "reef_submit_task") {
		t.Error("Coordinator prompt should mention 'reef_submit_task'")
	}
	if !strings.Contains(content, "MUST NOT directly execute") {
		t.Error("Coordinator prompt should contain hard rule about not executing")
	}
}

func TestHermesRoleContributor_ExecutorMode(t *testing.T) {
	c := newHermesRoleContributor(HermesExecutor)

	parts, err := c.ContributePrompt(context.Background(), PromptBuildRequest{})
	if err != nil {
		t.Fatalf("ContributePrompt error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("Executor mode should contribute 1 part, got %d", len(parts))
	}

	content := parts[0].Content
	if !strings.Contains(content, "Task Executor") {
		t.Error("Executor prompt should contain 'Task Executor'")
	}
}

func TestHermesRoleContributor_PromptSource(t *testing.T) {
	c := newHermesRoleContributor(HermesCoordinator)
	desc := c.PromptSource()

	if desc.ID != PromptSourceHermesRole {
		t.Errorf("PromptSource ID = %q, want %q", desc.ID, PromptSourceHermesRole)
	}
	if desc.Owner != "hermes" {
		t.Errorf("PromptSource Owner = %q, want 'hermes'", desc.Owner)
	}
	if !desc.StableByDefault {
		t.Error("PromptSource should be stable by default")
	}
	if len(desc.Allowed) != 1 {
		t.Fatalf("Allowed placements = %d, want 1", len(desc.Allowed))
	}
	if desc.Allowed[0].Layer != PromptLayerKernel {
		t.Errorf("Allowed Layer = %q, want %q", desc.Allowed[0].Layer, PromptLayerKernel)
	}
}
