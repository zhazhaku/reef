// Package lht — /lht command family parser + reef lht CLI.
//
// Implements W3B: message-channel command routing with short-alias support,
// and the server-side reef lht Cobra subcommand factory.
package lht

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ────────────────────────────────────────────────────────────
// LhtEngine — engine interface consumed by the command handler.
// This is the W3B↔W3A contract; implemented by Engine in engine.go.
// ────────────────────────────────────────────────────────────

// LhtEngine is the interface that cmd_lht requires from the LHT engine.
// Frozen contract between W3B (commands) and W3A (engine.go).
type LhtEngine interface {
	NewGoal(title, scope string, channel, chatID string) (*Goal, error)
	Dispatch(cmd string, args []string, reply UserReply) error
	Reply(gate string, goalID string, action string, content string) error
}

// ────────────────────────────────────────────────────────────
// Command routing table — full names + short aliases.
// ────────────────────────────────────────────────────────────

// aliasMap maps short aliases to full command names.
// Only applied when the input line starts with "/lht ".
var aliasMap = map[string]string{
	"n":   "new",
	"ls":  "list",
	"st":  "status",
	"p":   "plan",
	"ok":  "approve",
	"no":  "reject",
	"pz":  "pause",
	"go":  "resume",
	"x":   "stop",
	"add": "insert",
	"b":   "budget",
	"esc": "escalate",
	"log": "logs",
	"art": "artifacts",
	"rev": "review",
	"h":   "history",
	"?":   "help",
}

// validCommands is the set of all recognised full command names (after alias
// resolution).  Used for help output and validation.
var validCommands = map[string]string{
	"new":        "Create a new long-horizon task",
	"list":       "List tasks (optional state filter)",
	"status":     "Show task detail",
	"plan":       "Show plan DAG and budget proposal",
	"approve":    "Approve plan and budget",
	"reject":     "Reject plan with feedback",
	"pause":      "Pause a running task",
	"resume":     "Resume a paused task",
	"stop":       "Abort a task",
	"insert":     "Insert a new requirement",
	"budget":     "Show budget usage",
	"escalate":   "Show escalation / help-request report",
	"logs":       "Show execution logs",
	"artifacts":  "List task artifacts",
	"review":     "Show review records",
	"history":    "Show historical tasks",
	"help":       "Show this help",
}

// stateChanging is the set of commands that go through engine.Dispatch.
var stateChanging = map[string]bool{
	"approve":  true,
	"reject":   true,
	"pause":    true,
	"resume":   true,
	"stop":     true,
	"escalate": true,
}

// aliasReverse maps full command → short alias (for help display).
var aliasReverse = map[string]string{}

func init() {
	for short, full := range aliasMap {
		aliasReverse[full] = short
	}
}

// ────────────────────────────────────────────────────────────
// ParseLhtCommand — main entry point for message-channel parsing.
// ────────────────────────────────────────────────────────────

// ParseLhtCommand parses a raw message line and returns:
//
//	cmd  — full command name (e.g. "approve"), "" if not an /lht command
//	args — whitespace-split arguments after the subcommand
//
// Short aliases (ok→approve, no→reject, etc.) are resolved.
// Aliases are ONLY recognised when the line starts with "/lht " (P2-01).
func ParseLhtCommand(line string) (cmd string, args []string) {
	const prefix = "/lht "

	if !strings.HasPrefix(line, prefix) {
		// Also match exactly "/lht" (no trailing space) — treat as help.
		if line == "/lht" {
			return "help", nil
		}
		return "", nil
	}

	rest := strings.TrimSpace(line[len(prefix):])
	if rest == "" {
		return "help", nil
	}

	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return "", nil
	}

	raw := parts[0]
	args = parts[1:]

	// Resolve short alias → full name.
	if full, ok := aliasMap[raw]; ok {
		return full, args
	}

	// Recognise full command names directly.
	if _, ok := validCommands[raw]; ok {
		return raw, args
	}

	return "", nil
}

// IsLhtCommand reports whether line is recognised as an /lht command.
func IsLhtCommand(line string) bool {
	cmd, _ := ParseLhtCommand(line)
	return cmd != ""
}

// ────────────────────────────────────────────────────────────
// LhtHandler — dispatches parsed /lht commands to the engine.
// ────────────────────────────────────────────────────────────

// LhtHandler binds an LhtEngine and provides Handle() for message channels.
type LhtHandler struct {
	engine LhtEngine
}

// NewLhtHandler creates a handler backed by the given engine.
func NewLhtHandler(engine LhtEngine) *LhtHandler {
	return &LhtHandler{engine: engine}
}

// Handle parses line and, if it is an /lht command, dispatches to the engine.
// channel/chatID are passed through when creating new goals; for all other
// commands the engine already tracks context per-goal internally.
//
// Returns the resolved command name and any error.
func (h *LhtHandler) Handle(line string, channel, chatID string) (string, error) {
	cmd, args := ParseLhtCommand(line)
	if cmd == "" {
		return "", fmt.Errorf("not an /lht command: %q", line)
	}

	switch {
	case cmd == "new":
		title := strings.Join(args, " ")
		if strings.TrimSpace(title) == "" {
			return cmd, fmt.Errorf("/lht new: goal description required")
		}
		_, err := h.engine.NewGoal(title, "", channel, chatID)
		return cmd, err

	case stateChanging[cmd]:
		// Build a UserReply from the original input.
		reply := UserReply{
			ReplyType: replyTypeForStateChange(cmd),
			Content:   strings.Join(args[1:], " "),
			Timestamp: time.Now(),
		}
		return cmd, h.engine.Dispatch(cmd, args, reply)

	default:
		// Read-only / informational commands (list, status, plan, budget,
		// logs, artifacts, review, history, insert, help).
		// These are handled at the message-channel layer; the engine may
		// later expose query methods (W4+).
		return cmd, nil
	}
}

// replyTypeForStateChange maps a state-changing command to the UserReply.ReplyType
// that engine.Dispatch expects for routing.
func replyTypeForStateChange(cmd string) string {
	switch cmd {
	case "approve":
		return ReplyTypeApprove
	case "reject":
		return ReplyTypeReject
	default:
		// pause/resume/stop/escalate — Dispatch doesn't read ReplyType,
		// only approve/reject do.
		return ""
	}
}

// ────────────────────────────────────────────────────────────
// Help text
// ────────────────────────────────────────────────────────────

// LhtHelp returns formatted help text for /lht commands.
func LhtHelp() string {
	var b strings.Builder
	b.WriteString("/lht — Long-Horizon Task commands\n\n")
	b.WriteString("Usage: /lht <command> [args...]\n\n")
	b.WriteString("Commands:\n")

	// Ordered list matching spec.
	order := []string{
		"new", "list", "status", "plan",
		"approve", "reject", "pause", "resume", "stop",
		"insert", "budget", "escalate",
		"logs", "artifacts", "review", "history",
		"help",
	}
	for _, name := range order {
		desc := validCommands[name]
		alias := aliasReverse[name]
		if alias != "" {
			fmt.Fprintf(&b, "  %-10s /%-4s  %s\n", name, alias, desc)
		} else {
			fmt.Fprintf(&b, "  %-10s        %s\n", name, desc)
		}
	}
	return b.String()
}

// ────────────────────────────────────────────────────────────
// reef lht CLI (Cobra commands, server-side)
// ────────────────────────────────────────────────────────────

// NewLhtCommand returns the "reef lht" Cobra command tree.
// It is designed to be registered in cmd/reef/main.go via:
//
//	cmd.AddCommand(lht.NewLhtCommand())
//
// Subcommands: list, status, resume, wake, gc.
func NewLhtCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "lht",
		Short:   "Manage long-horizon tasks (server-side)",
		Aliases: []string{"lh"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newLhtListCmd(),
		newLhtStatusCmd(),
		newLhtResumeCmd(),
		newLhtWakeCmd(),
		newLhtGcCmd(),
	)

	return cmd
}

func newLhtListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List long-horizon tasks (all states)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Stub — engine not yet wired at CLI level (W3A).
			fmt.Println("lht list: engine not yet wired (W3A pending)")
			return nil
		},
	}
}

func newLhtStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status <goal-id>",
		Aliases: []string{"st"},
		Short:   "Show task status (server perspective)",
		Long:    "Show detailed status of a long-horizon task. Requires the goal ID.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("lht status %s: engine not yet wired (W3A pending)\n", args[0])
			return nil
		},
	}
}

func newLhtResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <goal-id>",
		Short: "Resume task from checkpoint after process restart",
		Long:  "Read the checkpoint and resume a long-horizon task from its last saved state.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("lht resume %s: engine not yet wired (W3A pending)\n", args[0])
			return nil
		},
	}
}

func newLhtWakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wake",
		Short: "Wake and resume all incomplete tasks (cron trigger)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("lht wake: engine not yet wired (W3A pending)")
			return nil
		},
	}
}

func newLhtGcCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Garbage-collect completed/aborted task directories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("lht gc: engine not yet wired (W3A pending)")
			return nil
		},
	}
}
