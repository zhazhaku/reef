package agent

import (
	"context"
	"sync/atomic"
)

// dangerousToolNames lists tools that should require human-in-the-loop (HITL)
// approval before execution. These tools can cause irreversible data loss,
// schedule persistent background tasks, or modify remote state.
//
// OWASP LLM07: Insecure Plugin Design | OWASP LLM08: Excessive Agency
//
// Narrow scope: only tools that cause data loss, persistent scheduling,
// or remote state mutation. Read-only tools and essential agent operations
// (search, message, subagent, spawn) are NOT included — those have existing
// guardrails (shell.go guardCommand, HermesGuard mode restrictions).
var dangerousToolNames = map[string]bool{
	"file_delete":    true,
	"file_remove":    true,
	"cron_add":       true,
	"cron_remove":    true,
	"git_push":       true,
	"git_force_push": true,
}

// IsDangerousTool returns true if the given tool name requires HITL approval.
func IsDangerousTool(name string) bool {
	return dangerousToolNames[name]
}

// DangerousToolApprover implements ToolApprover to require human-in-the-loop
// (HITL) approval for dangerous tool operations. When the guard is active, tools
// listed in dangerousToolNames are denied unless the user explicitly approves them.
//
// OWASP LLM07: Insecure Plugin Design | OWASP LLM08: Excessive Agency
//
// Usage in agent initialization:
//
//	approver := NewDangerousToolApprover()
//	hookManager.Mount(HookRegistration{
//	    Name:     "dangerous-tool-approver",
//	    Hook:     approver,
//	    Priority: 50,
//	    Source:   HookSourceBuiltin,
//	})
//
// To disable (e.g., in a trusted environment):
//
//	approver.SetEnabled(false)
type DangerousToolApprover struct {
	enabled atomic.Bool
}

// NewDangerousToolApprover creates a new DangerousToolApprover with HITL enabled.
func NewDangerousToolApprover() *DangerousToolApprover {
	a := &DangerousToolApprover{}
	a.enabled.Store(true)
	return a
}

// SetEnabled enables or disables the dangerous tool approval guard.
func (a *DangerousToolApprover) SetEnabled(enabled bool) {
	a.enabled.Store(enabled)
}

// IsEnabled returns whether the guard is currently active.
func (a *DangerousToolApprover) IsEnabled() bool {
	return a.enabled.Load()
}

// ApproveTool checks whether the requested tool requires human approval.
func (a *DangerousToolApprover) ApproveTool(ctx context.Context, req *ToolApprovalRequest) (ApprovalDecision, error) {
	if !a.enabled.Load() {
		return ApprovalDecision{Approved: true}, nil
	}
	if req == nil {
		return ApprovalDecision{Approved: true}, nil
	}
	if IsDangerousTool(req.Tool) {
		return ApprovalDecision{
			Approved: false,
			Reason:   "Tool \"" + req.Tool + "\" requires human approval (dangerous operation). Ask the user for confirmation before proceeding.",
		}, nil
	}
	return ApprovalDecision{Approved: true}, nil
}
