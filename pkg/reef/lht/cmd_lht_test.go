package lht

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// ────────────────────────────────────────────────────────────
// mockEngine — captures Dispatch / NewGoal / Reply calls.
// ────────────────────────────────────────────────────────────

type dispatchCall struct {
	Cmd   string
	Args  []string
	Reply UserReply
}

type newGoalCall struct {
	Title   string
	Scope   string
	Channel string
	ChatID  string
}

type replyCall struct {
	Gate    string
	GoalID  string
	Action  string
	Content string
}

type mockEngine struct {
	dispatchCalls []dispatchCall
	newGoalCalls  []newGoalCall
	replyCalls    []replyCall

	dispatchErr error
	newGoalErr  error
	replyErr    error
}

func (m *mockEngine) NewGoal(title, scope, channel, chatID string) (*Goal, error) {
	m.newGoalCalls = append(m.newGoalCalls, newGoalCall{
		Title: title, Scope: scope, Channel: channel, ChatID: chatID,
	})
	if m.newGoalErr != nil {
		return nil, m.newGoalErr
	}
	return &Goal{GoalID: "g-001", Description: title, State: StateGrounding}, nil
}

func (m *mockEngine) Dispatch(cmd string, args []string, reply UserReply) error {
	m.dispatchCalls = append(m.dispatchCalls, dispatchCall{
		Cmd: cmd, Args: args, Reply: reply,
	})
	return m.dispatchErr
}

func (m *mockEngine) Reply(gate, goalID, action, content string) error {
	m.replyCalls = append(m.replyCalls, replyCall{
		Gate: gate, GoalID: goalID, Action: action, Content: content,
	})
	return m.replyErr
}

// ────────────────────────────────────────────────────────────
// T3B.1.1 — 子命令路由表（17 full names）
// ────────────────────────────────────────────────────────────

func TestParseLhtCommandFullNames(t *testing.T) {
	tests := []struct {
		line    string
		wantCmd string
		wantN   int // expected arg count
	}{
		{"/lht new build a web app", "new", 4},
		{"/lht list", "list", 0},
		{"/lht list running", "list", 1},
		{"/lht status g-001", "status", 1},
		{"/lht plan g-001", "plan", 1},
		{"/lht approve g-001", "approve", 1},
		{"/lht reject g-001 needs work", "reject", 3},
		{"/lht pause g-001", "pause", 1},
		{"/lht resume g-001", "resume", 1},
		{"/lht stop g-001", "stop", 1},
		{"/lht insert g-001 add logging", "insert", 3},
		{"/lht budget g-001", "budget", 1},
		{"/lht escalate g-001", "escalate", 1},
		{"/lht logs g-001", "logs", 1},
		{"/lht logs g-001 20", "logs", 2},
		{"/lht artifacts g-001", "artifacts", 1},
		{"/lht review g-001", "review", 1},
		{"/lht history", "history", 0},
		{"/lht help", "help", 0},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			cmd, args := ParseLhtCommand(tt.line)
			if cmd != tt.wantCmd {
				t.Errorf("ParseLhtCommand(%q) cmd = %q, want %q", tt.line, cmd, tt.wantCmd)
			}
			if len(args) != tt.wantN {
				t.Errorf("ParseLhtCommand(%q) len(args) = %d, want %d (args=%v)",
					tt.line, len(args), tt.wantN, args)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.2 — 短别名等价性（9 个 spec 别名 + 其余全部覆盖）
// ────────────────────────────────────────────────────────────

func TestParseLhtCommandShortAliases(t *testing.T) {
	tests := []struct {
		line    string
		wantCmd string
	}{
		// 9 short aliases from the spec
		{"/lht ok g-001", "approve"},
		{"/lht no g-001 needs work", "reject"},
		{"/lht pz g-001", "pause"},
		{"/lht go g-001", "resume"},
		{"/lht x g-001", "stop"},
		{"/lht add g-001 add logging", "insert"},
		{"/lht b g-001", "budget"},
		{"/lht esc g-001", "escalate"},
		{"/lht ?", "help"},

		// Other short aliases
		{"/lht n build app", "new"},
		{"/lht ls", "list"},
		{"/lht st g-001", "status"},
		{"/lht p g-001", "plan"},
		{"/lht log g-001", "logs"},
		{"/lht art g-001", "artifacts"},
		{"/lht rev g-001", "review"},
		{"/lht h", "history"},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			cmd, _ := ParseLhtCommand(tt.line)
			if cmd != tt.wantCmd {
				t.Errorf("ParseLhtCommand(%q) cmd = %q, want %q", tt.line, cmd, tt.wantCmd)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.3 — P2-01 回归：短别名仅在 /lht 前缀下生效
// ────────────────────────────────────────────────────────────

func TestShortAliasOnlyUnderLhtPrefix(t *testing.T) {
	bare := []string{
		"ok", "no", "pz", "go", "x", "add", "b", "esc", "?",
		"n", "ls", "st", "p", "h",
	}

	for _, word := range bare {
		t.Run("bare:"+word, func(t *testing.T) {
			cmd, _ := ParseLhtCommand(word)
			if cmd != "" {
				t.Errorf("bare word %q should NOT be recognised as LHT command, got cmd=%q", word, cmd)
			}
			cmd2, _ := ParseLhtCommand(word + " g-001")
			if cmd2 != "" {
				t.Errorf("bare %q with args should NOT be recognised, got cmd=%q", word, cmd2)
			}
		})
	}

	for _, word := range bare {
		if IsLhtCommand(word) {
			t.Errorf("IsLhtCommand(%q) = true, want false", word)
		}
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.4 — 参数解析（list 状态过滤、logs n 分页、status id 必填）
// ────────────────────────────────────────────────────────────

func TestParseLhtCommandArgs(t *testing.T) {
	// list with status filter
	cmd, args := ParseLhtCommand("/lht list running")
	if cmd != "list" || len(args) != 1 || args[0] != "running" {
		t.Fatalf("list running: got cmd=%q args=%v", cmd, args)
	}

	cmd, args = ParseLhtCommand("/lht list paused")
	if cmd != "list" || len(args) != 1 || args[0] != "paused" {
		t.Fatalf("list paused: got cmd=%q args=%v", cmd, args)
	}

	// logs with n pagination
	cmd, args = ParseLhtCommand("/lht logs g-001 20")
	if cmd != "logs" || len(args) != 2 || args[1] != "20" {
		t.Fatalf("logs with n: got cmd=%q args=%v", cmd, args)
	}

	// status requires id
	cmd, args = ParseLhtCommand("/lht status g-xyz-123")
	if cmd != "status" || len(args) != 1 || args[0] != "g-xyz-123" {
		t.Fatalf("status id: got cmd=%q args=%v", cmd, args)
	}

	// reject with multi-word feedback
	cmd, args = ParseLhtCommand("/lht reject g-001 the plan needs more detail in phase 3")
	if cmd != "reject" || len(args) < 3 {
		t.Fatalf("reject multi-word: got cmd=%q args=%v", cmd, args)
	}

	// insert multi-word requirement
	cmd, args = ParseLhtCommand("/lht insert g-001 add an admin dashboard with RBAC")
	if cmd != "insert" || len(args) < 3 {
		t.Fatalf("insert multi-word: got cmd=%q args=%v", cmd, args)
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.5 — /lht 无参数 → help
// ────────────────────────────────────────────────────────────

func TestParseLhtBareSlashLht(t *testing.T) {
	cmd, args := ParseLhtCommand("/lht")
	if cmd != "help" {
		t.Errorf("/lht bare: want help, got %q", cmd)
	}
	if len(args) != 0 {
		t.Errorf("/lht bare: want 0 args, got %v", args)
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.6 — 非 LHT 命令不应识别
// ────────────────────────────────────────────────────────────

func TestParseNonLhtCommands(t *testing.T) {
	nonLht := []string{
		"hello world",
		"/other status g-001",
		"lht status g-001",
		"",
	}

	for _, line := range nonLht {
		t.Run(line, func(t *testing.T) {
			cmd, _ := ParseLhtCommand(line)
			if cmd != "" {
				t.Errorf("non-LHT line %q should return empty cmd, got %q", line, cmd)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.7 — 未知子命令
// ────────────────────────────────────────────────────────────

func TestParseLhtUnknownSubcommand(t *testing.T) {
	cmd, _ := ParseLhtCommand("/lht foobar g-001")
	if cmd != "" {
		t.Errorf("unknown subcommand should return empty cmd, got %q", cmd)
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.8 — IsLhtCommand
// ────────────────────────────────────────────────────────────

func TestIsLhtCommand(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"/lht status g-001", true},
		{"/lht ok g-001", true},
		{"/lht help", true},
		{"/lht", true},
		{"ok g-001", false},
		{"hello", false},
		{"/other cmd", false},
	}
	for _, tt := range tests {
		if got := IsLhtCommand(tt.line); got != tt.want {
			t.Errorf("IsLhtCommand(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.9 — LhtHelp 输出包含全称+短别名对照
// ────────────────────────────────────────────────────────────

func TestLhtHelpContainsAllCommands(t *testing.T) {
	help := LhtHelp()

	for name := range validCommands {
		if !strings.Contains(help, name) {
			t.Errorf("LhtHelp missing full command: %q", name)
		}
	}

	wantAliases := []string{"/ok", "/no", "/pz", "/go", "/x", "/add", "/b", "/esc", "/?"}
	for _, a := range wantAliases {
		if !strings.Contains(help, a) {
			t.Errorf("LhtHelp missing alias hint: %q", a)
		}
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.10 — LhtHandler 调用 engine.Dispatch 正确参数
// ────────────────────────────────────────────────────────────

func TestHandlerDispatchToEngine(t *testing.T) {
	eng := &mockEngine{}
	h := NewLhtHandler(eng)

	// approve
	cmd, err := h.Handle("/lht approve g-001", "telegram", "chat-42")
	if err != nil {
		t.Fatalf("Handle(approve): %v", err)
	}
	if cmd != "approve" {
		t.Fatalf("Handle(approve) returned cmd=%q", cmd)
	}
	if len(eng.dispatchCalls) != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", len(eng.dispatchCalls))
	}
	dc := eng.dispatchCalls[0]
	if dc.Cmd != "approve" || len(dc.Args) != 1 || dc.Args[0] != "g-001" {
		t.Errorf("dispatch args: cmd=%q args=%v", dc.Cmd, dc.Args)
	}
	// Verify UserReply fields
	if dc.Reply.ReplyType != ReplyTypeApprove {
		t.Errorf("UserReply.ReplyType = %q, want %q", dc.Reply.ReplyType, ReplyTypeApprove)
	}
	if dc.Reply.Timestamp.IsZero() {
		t.Error("UserReply.Timestamp should not be zero")
	}

	// pause via alias
	eng.dispatchCalls = nil
	cmd, err = h.Handle("/lht pz g-001", "telegram", "chat-42")
	if err != nil {
		t.Fatalf("Handle(pz): %v", err)
	}
	if cmd != "pause" {
		t.Fatalf("Handle(pz) returned cmd=%q, want pause", cmd)
	}
	if len(eng.dispatchCalls) != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", len(eng.dispatchCalls))
	}
	if eng.dispatchCalls[0].Cmd != "pause" {
		t.Errorf("dispatch after alias: cmd=%q want pause", eng.dispatchCalls[0].Cmd)
	}

	// reject with feedback
	eng.dispatchCalls = nil
	cmd, err = h.Handle("/lht reject g-001 plan too vague", "telegram", "chat-42")
	if err != nil {
		t.Fatalf("Handle(reject): %v", err)
	}
	if len(eng.dispatchCalls) != 1 {
		t.Fatalf("reject: expected 1 dispatch call, got %d", len(eng.dispatchCalls))
	}
	if eng.dispatchCalls[0].Reply.ReplyType != ReplyTypeReject {
		t.Errorf("reject ReplyType = %q, want %q", eng.dispatchCalls[0].Reply.ReplyType, ReplyTypeReject)
	}
	if eng.dispatchCalls[0].Reply.Content != "plan too vague" {
		t.Errorf("reject Content = %q", eng.dispatchCalls[0].Reply.Content)
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.11 — LhtHandler 调用 engine.NewGoal 正确参数
// ────────────────────────────────────────────────────────────

func TestHandlerNewGoal(t *testing.T) {
	eng := &mockEngine{}
	h := NewLhtHandler(eng)

	cmd, err := h.Handle("/lht new build a REST API", "feishu", "chat-99")
	if err != nil {
		t.Fatalf("Handle(new): %v", err)
	}
	if cmd != "new" {
		t.Fatalf("Handle(new) returned cmd=%q", cmd)
	}
	if len(eng.newGoalCalls) != 1 {
		t.Fatalf("expected 1 NewGoal call, got %d", len(eng.newGoalCalls))
	}
	nc := eng.newGoalCalls[0]
	if nc.Title != "build a REST API" {
		t.Errorf("NewGoal title = %q", nc.Title)
	}
	if nc.Scope != "" {
		t.Errorf("NewGoal scope = %q, want empty", nc.Scope)
	}
	if nc.Channel != "feishu" || nc.ChatID != "chat-99" {
		t.Errorf("NewGoal channel/chat: %s/%s", nc.Channel, nc.ChatID)
	}
}

func TestHandlerNewGoalEmptyTitle(t *testing.T) {
	eng := &mockEngine{}
	h := NewLhtHandler(eng)

	_, err := h.Handle("/lht new", "cli", "direct")
	if err == nil {
		t.Fatal("expected error for /lht new with no description")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error should mention required: %v", err)
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.12 — Handler 对非 LHT 命令返回错误
// ────────────────────────────────────────────────────────────

func TestHandlerNonLhtReturnsError(t *testing.T) {
	eng := &mockEngine{}
	h := NewLhtHandler(eng)

	_, err := h.Handle("hello world", "cli", "direct")
	if err == nil {
		t.Fatal("expected error for non-LHT input")
	}
	if !strings.Contains(err.Error(), "not an /lht command") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.13 — reef lht CLI 子命令路由
// ────────────────────────────────────────────────────────────

func TestReefLhtCLISubcommands(t *testing.T) {
	root := NewLhtCommand()

	if root.Use != "lht" {
		t.Errorf("root Use = %q, want lht", root.Use)
	}

	wantSubs := map[string]string{
		"list":   "List long-horizon tasks",
		"status": "Show task status",
		"resume": "Resume task from checkpoint",
		"wake":   "Wake and resume",
		"gc":     "Garbage-collect",
	}

	found := make(map[string]bool)
	for _, sub := range root.Commands() {
		name := sub.Name()
		found[name] = true
		if want, ok := wantSubs[name]; ok {
			if !strings.Contains(sub.Short, want) {
				t.Errorf("subcommand %q: Short %q does not contain %q", name, sub.Short, want)
			}
		}
	}

	for name := range wantSubs {
		if !found[name] {
			t.Errorf("missing subcommand: %q", name)
		}
	}

	if len(found) != len(wantSubs) {
		t.Errorf("got %d subcommands, want %d", len(found), len(wantSubs))
	}
}

func TestReefLhtCLIListExecutes(t *testing.T) {
	root := NewLhtCommand()
	root.SetArgs([]string{"list"})
	err := root.Execute()
	if err != nil {
		t.Fatalf("lht list: %v", err)
	}
}

func TestReefLhtCLIStatusRequiresArg(t *testing.T) {
	root := NewLhtCommand()
	root.SetArgs([]string{"status"})
	err := root.Execute()
	if err == nil {
		t.Fatal("lht status without arg should fail")
	}
}

func TestReefLhtCLIStatusWithArg(t *testing.T) {
	root := NewLhtCommand()
	root.SetArgs([]string{"status", "g-001"})
	err := root.Execute()
	if err != nil {
		t.Fatalf("lht status g-001: %v", err)
	}
}

func TestReefLhtCLIResumeWithArg(t *testing.T) {
	root := NewLhtCommand()
	root.SetArgs([]string{"resume", "g-001"})
	err := root.Execute()
	if err != nil {
		t.Fatalf("lht resume g-001: %v", err)
	}
}

func TestReefLhtCLIWake(t *testing.T) {
	root := NewLhtCommand()
	root.SetArgs([]string{"wake"})
	err := root.Execute()
	if err != nil {
		t.Fatalf("lht wake: %v", err)
	}
}

func TestReefLhtCLIGc(t *testing.T) {
	root := NewLhtCommand()
	root.SetArgs([]string{"gc"})
	err := root.Execute()
	if err != nil {
		t.Fatalf("lht gc: %v", err)
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.14 — 全称短别名 round-trip（短别名→全称→Dispatch 全称）
// ────────────────────────────────────────────────────────────

func TestAllShortAliasesDispatchFullName(t *testing.T) {
	for short, full := range aliasMap {
		eng := &mockEngine{}
		h := NewLhtHandler(eng)

		line := fmt.Sprintf("/lht %s g-001 extra info", short)
		cmd, err := h.Handle(line, "telegram", "c1")
		if err != nil {
			t.Errorf("%s: Handle error: %v", short, err)
			continue
		}
		if cmd != full {
			t.Errorf("%s: Handle returned cmd=%q, want %q", short, cmd, full)
		}

		if full == "new" {
			if len(eng.newGoalCalls) != 1 {
				t.Errorf("%s: NewGoal not called", short)
			}
			continue
		}
		if !stateChanging[full] {
			// Read-only command — no dispatch expected.
			if len(eng.dispatchCalls) != 0 {
				t.Errorf("%s: expected 0 dispatch calls for read-only cmd, got %d", short, len(eng.dispatchCalls))
			}
			continue
		}
		if len(eng.dispatchCalls) != 1 {
			t.Errorf("%s: expected 1 dispatch, got %d", short, len(eng.dispatchCalls))
			continue
		}
		if eng.dispatchCalls[0].Cmd != full {
			t.Errorf("%s: dispatched cmd=%q, want %q", short, eng.dispatchCalls[0].Cmd, full)
		}
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.15 — aliasMap 完整性校验
// ────────────────────────────────────────────────────────────

func TestAliasMapValidity(t *testing.T) {
	for short, full := range aliasMap {
		if _, ok := validCommands[full]; !ok {
			t.Errorf("alias %q → %q: %q is not in validCommands", short, full, full)
		}
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.16 — 多余空格处理
// ────────────────────────────────────────────────────────────

func TestLhtCommandWithExtraWhitespace(t *testing.T) {
	cmd, args := ParseLhtCommand("/lht   status   g-001  ")
	if cmd != "status" || len(args) != 1 || args[0] != "g-001" {
		t.Errorf("extra whitespace: cmd=%q args=%v", cmd, args)
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.17 — 读命令（list/status/plan/budget/logs/artifacts/review/history/insert）不触发 Dispatch
// ────────────────────────────────────────────────────────────

func TestHandlerReadOnlyCommandsNoDispatch(t *testing.T) {
	readOnly := []string{"list", "status", "plan", "budget", "logs", "artifacts", "review", "history", "insert", "help"}
	for _, cmd := range readOnly {
		t.Run(cmd, func(t *testing.T) {
			eng := &mockEngine{}
			h := NewLhtHandler(eng)
			line := fmt.Sprintf("/lht %s g-001", cmd)
			gotCmd, err := h.Handle(line, "telegram", "c1")
			if err != nil {
				t.Errorf("%s: unexpected error: %v", cmd, err)
			}
			if gotCmd != cmd {
				t.Errorf("%s: Handle returned cmd=%q", cmd, gotCmd)
			}
			if len(eng.dispatchCalls) != 0 {
				t.Errorf("%s: expected 0 dispatch, got %d", cmd, len(eng.dispatchCalls))
			}
		})
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.18 — stateChanging 命令覆盖
// ────────────────────────────────────────────────────────────

func TestStateChangingDispatch(t *testing.T) {
	tests := []struct {
		cmd  string
		line string
	}{
		{"approve", "/lht approve g-001"},
		{"reject", "/lht reject g-001 needs work"},
		{"pause", "/lht pause g-001"},
		{"resume", "/lht resume g-001"},
		{"stop", "/lht stop g-001"},
		{"escalate", "/lht escalate g-001"},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			eng := &mockEngine{}
			h := NewLhtHandler(eng)
			cmd, err := h.Handle(tt.line, "telegram", "c1")
			if err != nil {
				t.Fatalf("Handle(%s): %v", tt.cmd, err)
			}
			if cmd != tt.cmd {
				t.Fatalf("Handle returned cmd=%q, want %q", cmd, tt.cmd)
			}
			if len(eng.dispatchCalls) != 1 {
				t.Fatalf("expected 1 dispatch, got %d", len(eng.dispatchCalls))
			}
			if eng.dispatchCalls[0].Cmd != tt.cmd {
				t.Errorf("dispatch cmd=%q, want %q", eng.dispatchCalls[0].Cmd, tt.cmd)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────
// T3B.1.19 — reef lht CLI 别名
// ────────────────────────────────────────────────────────────

func TestReefLhtCLIAliases(t *testing.T) {
	root := NewLhtCommand()
	if len(root.Aliases) == 0 || root.Aliases[0] != "lh" {
		t.Errorf("root alias: got %v, want [lh]", root.Aliases)
	}

	subMap := make(map[string]*cobra.Command)
	for _, sub := range root.Commands() {
		subMap[sub.Use] = sub
	}

	if l, ok := subMap["list"]; ok {
		hasLs := false
		for _, a := range l.Aliases {
			if a == "ls" {
				hasLs = true
				break
			}
		}
		if !hasLs {
			t.Error("list subcommand missing 'ls' alias")
		}
	}

	if s, ok := subMap["status"]; ok {
		hasSt := false
		for _, a := range s.Aliases {
			if a == "st" {
				hasSt = true
				break
			}
		}
		if !hasSt {
			t.Error("status subcommand missing 'st' alias")
		}
	}
}

// Ensure cobra import is used.
var _ = cobra.NoArgs

// Ensure time import is used.
var _ = time.Now
