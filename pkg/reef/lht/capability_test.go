package lht

import (
	"testing"
)

// ===================== T5B.1: Capability Tests =====================

func TestCapabilityL1Whitelist(t *testing.T) {
	// L1 whitelist capabilities should be classified as L1 and directly usable.
	l1Caps := []string{
		"skill:gsd",
		"skill:openspec",
		"skill:bash",
		"skill:go",
		"skill:github",
		"skill:deck",
		"skill:python",
		"skill:git",
		"skill:docker",
		"skill:research",
		"client:lht-gen",
		"client:lht-eval",
		"client:lht-rev",
	}

	for _, cap := range l1Caps {
		level := ClassifyCapability(cap)
		if level != CapL1 {
			t.Errorf("%q classified as %v, want L1", cap, level)
		}
		if !IsCapabilityAllowed(cap) {
			t.Errorf("%q should be allowed without confirmation", cap)
		}
	}
}

func TestCapabilityL2NewSkills(t *testing.T) {
	// New/unknown skills should be L2 - require confirmation.
	l2Skills := []string{
		"skill:terraform",
		"skill:k8s",
		"skill:ansible",
		"skill:unknown-tool",
		"skill:custom-plugin",
	}

	for _, cap := range l2Skills {
		level := ClassifyCapability(cap)
		if level != CapL2 {
			t.Errorf("%q classified as %v, want L2", cap, level)
		}
		if IsCapabilityAllowed(cap) {
			t.Errorf("%q should require confirmation (L2)", cap)
		}
	}
}

func TestCapabilityL2NewPlugins(t *testing.T) {
	// All plugins are L2 by default (no plugin whitelist).
	l2Plugins := []string{
		"plugin:datadog",
		"plugin:prometheus",
		"plugin:custom-monitoring",
	}

	for _, cap := range l2Plugins {
		level := ClassifyCapability(cap)
		if level != CapL2 {
			t.Errorf("%q classified as %v, want L2", level, cap)
		}
	}
}

func TestCapabilityL2NewClients(t *testing.T) {
	// Non-LHT clients need confirmation.
	l2Clients := []string{
		"client:custom-agent",
		"client:external-api",
		"client:deploy-bot",
	}

	for _, cap := range l2Clients {
		level := ClassifyCapability(cap)
		if level != CapL2 {
			t.Errorf("%q classified as %v, want L2", level, cap)
		}
	}
}

func TestCapabilityThreeRoleExemption(t *testing.T) {
	// The three LHT roles (lht-gen, lht-eval, lht-rev) are built-in.
	// Creating them should NOT trigger L2 confirmation.
	roles := []string{"lht-gen", "lht-eval", "lht-rev"}

	for _, name := range roles {
		if !IsLHTBuiltinRole(name) {
			t.Errorf("%q should be recognized as built-in LHT role", name)
		}
	}
}

func TestCapabilityNonLHTRoleNotExempt(t *testing.T) {
	// Non-LHT roles are not exempt.
	nonRoles := []string{"coder", "analyst", "tester", "deployer"}

	for _, name := range nonRoles {
		if IsLHTBuiltinRole(name) {
			t.Errorf("%q should NOT be recognized as built-in LHT role", name)
		}
	}
}

func TestCapabilityThreeRoleDefaultSkillsAreL1(t *testing.T) {
	// The default skills for LHT three roles should all be L1.
	defaultSkills := LHTRoleDefaultSkills()

	for _, skill := range defaultSkills {
		if ClassifyCapability(skill) != CapL1 {
			t.Errorf("default role skill %q should be L1, got %s", skill, ClassifyCapability(skill))
		}
	}
}

func TestCapabilitySkillExpansionNeedsConfirmation(t *testing.T) {
	// Adding new skills to an LHT role's skill set requires confirmation.
	expandedSkills := []string{
		"skill:terraform",  // not in L1 whitelist
		"skill:ansible",    // not in L1 whitelist
		"plugin:monitoring", // plugin, always L2
	}

	for _, cap := range expandedSkills {
		if IsCapabilityAllowed(cap) {
			t.Errorf("%q should need confirmation even for LHT roles", cap)
		}
	}
}

func TestCapabilityEmptyOrUnknown(t *testing.T) {
	// Empty or unknown capability format
	tests := []string{"", "unknown", "invalid:format:too:many"}

	for _, cap := range tests {
		level := ClassifyCapability(cap)
		if level != CapL2 {
			t.Errorf("%q should be L2 (conservative default), got %s", cap, level)
		}
	}
}

func TestCapabilityLevelString(t *testing.T) {
	if CapL1.String() != "L1" {
		t.Errorf("CapL1.String() = %q, want %q", CapL1.String(), "L1")
	}
	if CapL2.String() != "L2" {
		t.Errorf("CapL2.String() = %q, want %q", CapL2.String(), "L2")
	}
}

func TestCapabilityPlanCapabilityAudit(t *testing.T) {
	// Audit a plan's declared capabilities: separate into L1 (auto) and L2 (needs approval).
	caps := []string{
		"skill:go",
		"skill:github",
		"skill:terraform", // new skill
		"plugin:custom",   // new plugin
		"client:lht-gen",  // built-in role
		"client:custom",   // new client
	}

	l1, l2 := AuditCapabilities(caps)

	// Check L1 set
	expectedL1 := map[string]bool{"skill:go": true, "skill:github": true, "client:lht-gen": true}
	for _, c := range l1 {
		if !expectedL1[c] {
			t.Errorf("unexpected L1: %q", c)
		}
		delete(expectedL1, c)
	}
	if len(expectedL1) > 0 {
		t.Errorf("missing L1 entries: %v", expectedL1)
	}

	// Check L2 set
	expectedL2 := map[string]bool{"skill:terraform": true, "plugin:custom": true, "client:custom": true}
	for _, c := range l2 {
		if !expectedL2[c] {
			t.Errorf("unexpected L2: %q", c)
		}
		delete(expectedL2, c)
	}
	if len(expectedL2) > 0 {
		t.Errorf("missing L2 entries: %v", expectedL2)
	}
}

func TestCapabilityNeedsApprovalEmpty(t *testing.T) {
	// Empty plan needs no approval.
	if NeedsCapabilityApproval(nil) {
		t.Error("nil capabilities should not need approval")
	}
	if NeedsCapabilityApproval([]string{}) {
		t.Error("empty capabilities should not need approval")
	}
}

func TestCapabilityNeedsApprovalOnlyL1(t *testing.T) {
	// Plan with only L1 capabilities does not need approval.
	caps := []string{"skill:go", "skill:bash", "client:lht-gen"}
	if NeedsCapabilityApproval(caps) {
		t.Error("L1-only plan should not need capability approval")
	}
}

func TestCapabilityNeedsApprovalHasL2(t *testing.T) {
	// Plan with any L2 capability needs approval.
	caps := []string{"skill:go", "skill:terraform"}
	if !NeedsCapabilityApproval(caps) {
		t.Error("plan with L2 capability should need approval")
	}
}
