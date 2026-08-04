// Package lht — Capability classification and approval system.
//
// Implements W5B: L1/L2 capability tiering for the LHT engine.
// L1 capabilities are whitelisted and directly usable.
// L2 capabilities (new skills, plugins, non-LHT clients) require user approval
// during plan confirmation.
package lht

import "strings"

// CapLevel represents the approval tier of a capability.
type CapLevel int

const (
	CapL1 CapLevel = 1 // Whitelisted, directly usable without confirmation
	CapL2 CapLevel = 2 // Needs user approval during plan confirmation
)

// String returns a human-readable representation of the capability level.
func (c CapLevel) String() string {
	switch c {
	case CapL1:
		return "L1"
	case CapL2:
		return "L2"
	default:
		return "unknown"
	}
}

// ────────────────────────────────────────────────────────────
// L1 Whitelist — always-available capabilities
// ────────────────────────────────────────────────────────────

// l1Skills is the set of whitelisted skills that are always available.
// These are built-in or well-known skills that the Reef platform ships with.
var l1Skills = map[string]bool{
	"gsd":      true, // project initialization & roadmap
	"openspec": true, // proposal/specs/design/tasks/implementation
	"research": true, // research/analysis workflow
	"bash":     true, // shell scripting
	"go":       true, // Go development
	"github":   true, // GitHub integration
	"deck":     true, // presentation generation
	"python":   true, // Python development
	"git":      true, // Git VCS
	"docker":   true, // Docker container management
	"read":     true, // file reading
	"write":    true, // file writing
	"web":      true, // web search/fetch
	"notify":   true, // notifications
}

// l1Clients is the set of built-in LHT role clients that don't need confirmation.
var l1Clients = map[string]bool{
	"lht-gen": true, // Generator role
	"lht-eval": true, // Evaluator role
	"lht-rev":  true, // Reviewer role
}

// ────────────────────────────────────────────────────────────
// LHT Built-in Roles
// ────────────────────────────────────────────────────────────

// Three LHT roles that are engine-managed and exempt from per-creation confirmation.
var lhtBuiltinRoles = map[string]bool{
	"lht-gen":  true,
	"lht-eval": true,
	"lht-rev":  true,
}

// IsLHTBuiltinRole returns true if the role name is one of the three
// engine-managed LHT roles (lht-gen, lht-eval, lht-rev).
func IsLHTBuiltinRole(name string) bool {
	return lhtBuiltinRoles[name]
}

// LHTRoleDefaultSkills returns the default skill set for LHT three roles.
// These are all L1-whitelisted skills that the roles inherit automatically.
func LHTRoleDefaultSkills() []string {
	return []string{
		"skill:go",
		"skill:bash",
		"skill:github",
		"skill:deck",
		"skill:git",
		"skill:docker",
		"skill:read",
		"skill:write",
	}
}

// ────────────────────────────────────────────────────────────
// Capability Classification
// ────────────────────────────────────────────────────────────

// ClassifyCapability determines whether a capability string is L1 (whitelisted)
// or L2 (needs approval). The capability format is "type:name".
//
// Types:
//   - "skill:*" → checked against l1Skills
//   - "client:*" → checked against l1Clients
//   - "plugin:*" → always L2 (plugin whitelist is empty in v1)
//   - anything else → L2 (conservative default)
func ClassifyCapability(cap string) CapLevel {
	if cap == "" {
		return CapL2
	}

	parts := strings.SplitN(cap, ":", 2)
	if len(parts) != 2 {
		return CapL2
	}

	typ, name := parts[0], parts[1]

	switch typ {
	case "skill":
		if l1Skills[name] {
			return CapL1
		}
		return CapL2

	case "client":
		if l1Clients[name] {
			return CapL1
		}
		return CapL2

	case "plugin":
		// No plugin whitelist in v1 — all plugins need approval.
		return CapL2

	default:
		// Unknown capability type → L2 (conservative).
		return CapL2
	}
}

// IsCapabilityAllowed returns true if the capability is L1 and can be used
// without user confirmation.
func IsCapabilityAllowed(cap string) bool {
	return ClassifyCapability(cap) == CapL1
}

// ────────────────────────────────────────────────────────────
// Capability Audit — Plan-level analysis
// ────────────────────────────────────────────────────────────

// AuditCapabilities splits a list of capability strings into L1 (auto-approved)
// and L2 (needs confirmation) groups.
func AuditCapabilities(caps []string) (l1 []string, l2 []string) {
	l1 = make([]string, 0, len(caps))
	l2 = make([]string, 0)

	for _, c := range caps {
		switch ClassifyCapability(c) {
		case CapL1:
			l1 = append(l1, c)
		case CapL2:
			l2 = append(l2, c)
		}
	}
	return
}

// NeedsCapabilityApproval returns true if the capability list contains any
// L2 capabilities that require user confirmation.
func NeedsCapabilityApproval(caps []string) bool {
	for _, c := range caps {
		if ClassifyCapability(c) == CapL2 {
			return true
		}
	}
	return false
}
