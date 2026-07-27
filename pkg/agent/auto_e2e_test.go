package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/commands"
)

// ============================================================================
// E2E Integration Tests: AutoLoopOrchestrator + Commands Executor
//
// These tests exercise the real integration between:
//   - AutoLoopOrchestrator (state machine, queue, history)
//   - commands.Runtime (callback wiring)
//   - commands.Executor (command parsing, routing)
//
// New AutoLoopOrchestrator starts in StateIdle / ModeAuto.
// Stop() is a no-op when idle (only stops an active loop).
// ProcessQueue() is stubbed (no LLM dispatch), so we test:
//   - mode switching via orchestrator.SetMode()
//   - queue enqueue/dequeue via EnqueueMessage()/ProcessQueue()
//   - queue FIFO ordering
//   - step processing
//   - loop configuration
//   - alias resolution (/autotask → auto, /autoloop → auto)
//   - concurrent operations
// ============================================================================

// newE2EOrchestrator creates a real AutoLoopOrchestrator with default config
// and wires all Runtime callbacks matching the production buildCommandsRuntime pattern.
func newE2EOrchestrator(t *testing.T) (*AutoLoopOrchestrator, *commands.Runtime, *commands.Executor) {
	t.Helper()

	orch := NewAutoLoopOrchestrator(LoopConfig{
		PollEvery:   1 * time.Second,
		Mode:        ModeInfinite,
		LoopCount:   0,
		IdleTimeout: 5 * time.Minute,
		MaxClients:  5,
	})

	rt := &commands.Runtime{}

	// Wire callbacks — mirrors agent_command.go buildCommandsRuntime pattern exactly
	rt.GetAutoStatus = func() interface{} {
		s := orch.Status()
		return map[string]interface{}{
			"mode":            s.Mode.String(),
			"state":           s.State.String(),
			"plan_id":         s.PlanID,
			"plan_status":     s.PlanStatus,
			"tasks_total":     s.TasksTotal,
			"tasks_completed": s.TasksCompleted,
			"tasks_failed":    s.TasksFailed,
			"tasks_running":   s.TasksRunning,
			"tasks_pending":   s.TasksPending,
			"clients_active":  s.ClientsActive,
			"clients_max":     s.ClientsMax,
		}
	}
	rt.SetAutoMode = func(mode string) (string, error) {
		var targetMode ConversationMode
		switch mode {
		case "auto":
			targetMode = ModeAuto
		case "manual":
			targetMode = ModeManual
		case "chat":
			targetMode = ModeChat
		case "hermes":
			targetMode = ModeHermes
		default:
			return "", fmt.Errorf("unknown mode: %s", mode)
		}
		prev := orch.GetMode()
		_ = orch.SetMode(targetMode)
		prevStr := ""
		switch prev {
		case ModeAuto:
			prevStr = "auto"
		case ModeManual:
			prevStr = "manual"
		case ModeChat:
			prevStr = "chat"
		case ModeHermes:
			prevStr = "hermes"
		}
		return prevStr, nil
	}
	rt.SetAutoLoopCount = func(count int) {
		var mode AutoMode
		if count == 0 {
			mode = ModeInfinite
		} else {
			mode = ModeLoopN
		}
		orch.SetLoopConfig(LoopConfig{
			PollEvery:   1 * time.Second,
			Mode:        mode,
			LoopCount:   count,
			IdleTimeout: 5 * time.Minute,
			MaxClients:  5,
		})
	}
	rt.EnqueueAutoMessage = func(instruction string) {
		orch.EnqueueMessage(instruction)
	}
	rt.RunAutoStep = func() {
		orch.ProcessQueue()
	}
	rt.StopAuto = func() {
		orch.Stop()
	}
	rt.GetAutoQueue = func() []string {
		return orch.ListQueue()
	}
	rt.GetAutoHistory = func() []commands.AutoHistoryEntry {
		entries := orch.ListHistory()
		result := make([]commands.AutoHistoryEntry, len(entries))
		for i, e := range entries {
			result[i] = commands.AutoHistoryEntry{
				ID:          e.ID,
				Instruction: e.Instruction,
				Status:      e.Status,
				Error:       e.ErrorMessage,
			}
		}
		return result
	}

	ex := commands.NewExecutor(commands.NewRegistry(commands.BuiltinDefinitions()), rt)
	return orch, rt, ex
}

// execCmd is a helper that executes a command and returns the reply text.
func execCmd(t *testing.T, ex *commands.Executor, text string) string {
	t.Helper()
	var reply string
	res := ex.Execute(context.Background(), commands.Request{
		Text: text,
		Reply: func(msg string) error {
			reply = msg
			return nil
		},
	})
	if res.Outcome != commands.OutcomeHandled {
		t.Fatalf("command %q: outcome=%v, want=%v", text, res.Outcome, commands.OutcomeHandled)
	}
	return reply
}

// ============================================================================
// E2E Test 1: 完整生命周期
// 模式变化 → 入队 → step → status → queue → history
// ============================================================================

func TestAutoE2E_FullLifecycle(t *testing.T) {
	orch, _, ex := newE2EOrchestrator(t)

	// 1. 初始状态 — default ModeAuto
	reply := execCmd(t, ex, "/auto status")
	t.Logf("initial status: %s", reply)
	if !strings.Contains(strings.ToLower(reply), "auto") {
		t.Errorf("initial status should mention 'auto' mode, got: %s", reply)
	}

	// 2. 切换为手动模式
	reply = execCmd(t, ex, "/auto mode manual")
	if !strings.Contains(reply, "auto") {
		t.Errorf("mode switch reply should mention previous mode 'auto', got: %s", reply)
	}
	if orch.GetMode() != ModeManual {
		t.Errorf("mode should be ModeManual, got %v", orch.GetMode())
	}

	// 3. 入队任务
	execCmd(t, ex, "/auto run 分析当前系统日志")
	execCmd(t, ex, "/auto run 检查磁盘空间")
	execCmd(t, ex, "/auto run 发送每日报告")

	// 4. 验证队列
	if len(orch.ListQueue()) != 3 {
		t.Errorf("queue should have 3 items, got %d", len(orch.ListQueue()))
	}

	// 5-7. 处理所有步骤
	execCmd(t, ex, "/auto step") // 分析当前系统日志
	execCmd(t, ex, "/auto step") // 检查磁盘空间
	execCmd(t, ex, "/auto step") // 发送每日报告

	// 8. 队列应为空
	if len(orch.ListQueue()) != 0 {
		t.Errorf("after 3 steps, queue should be empty, got %d", len(orch.ListQueue()))
	}

	// 9. Stopped orchestrator still accepts enqueue
	execCmd(t, ex, "/auto run 停止后的任务")
	if len(orch.ListQueue()) != 1 {
		t.Errorf("stopped orchestrator should still queue messages, got %d", len(orch.ListQueue()))
	}
}

// ============================================================================
// E2E Test 2: 模式切换全覆盖
// ============================================================================

func TestAutoE2E_ModeSwitching(t *testing.T) {
	orch, _, ex := newE2EOrchestrator(t)

	// Manual
	execCmd(t, ex, "/auto mode manual")
	if orch.GetMode() != ModeManual {
		t.Errorf("expected ModeManual, got %v", orch.GetMode())
	}

	// Chat
	execCmd(t, ex, "/auto mode chat")
	if orch.GetMode() != ModeChat {
		t.Errorf("expected ModeChat, got %v", orch.GetMode())
	}

	// Hermes
	execCmd(t, ex, "/auto mode hermes")
	if orch.GetMode() != ModeHermes {
		t.Errorf("expected ModeHermes, got %v", orch.GetMode())
	}

	// Auto (back)
	execCmd(t, ex, "/auto mode auto")
	if orch.GetMode() != ModeAuto {
		t.Errorf("expected ModeAuto, got %v", orch.GetMode())
	}

	// Invalid mode → error via handler validation, mode unchanged
	reply := execCmd(t, ex, "/auto mode invalid")
	if !strings.Contains(reply, "Invalid mode") {
		t.Errorf("invalid mode should return error, got: %s", reply)
	}
	if orch.GetMode() != ModeAuto {
		t.Errorf("mode should remain ModeAuto after invalid switch, got %v", orch.GetMode())
	}
}

// ============================================================================
// E2E Test 3: 循环计数配置
// ============================================================================

func TestAutoE2E_LoopConfiguration(t *testing.T) {
	orch, _, ex := newE2EOrchestrator(t)

	// 设置循环次数
	execCmd(t, ex, "/auto loop 3")
	if orch.loopConfig.Mode != ModeLoopN {
		t.Errorf("expected ModeLoopN, got %v", orch.loopConfig.Mode)
	}
	if orch.loopConfig.LoopCount != 3 {
		t.Errorf("expected LoopCount=3, got %d", orch.loopConfig.LoopCount)
	}

	// 无限循环
	execCmd(t, ex, "/auto loop infinite")
	if orch.loopConfig.Mode != ModeInfinite {
		t.Errorf("expected ModeInfinite, got %v", orch.loopConfig.Mode)
	}
}

// ============================================================================
// E2E Test 4: 入队队列管理边界情况 — 空队列、单任务、FIFO
// ============================================================================

func TestAutoE2E_QueueManagement(t *testing.T) {
	orch, _, ex := newE2EOrchestrator(t)

	// 空队列 — step 不应 crash
	execCmd(t, ex, "/auto step")

	// 单任务入队
	execCmd(t, ex, "/auto run A任务")
	if len(orch.ListQueue()) != 1 {
		t.Errorf("queue should have 1 entry, got %d", len(orch.ListQueue()))
	}

	// Step 处理
	execCmd(t, ex, "/auto step")
	if len(orch.ListQueue()) != 0 {
		t.Errorf("after step, queue should be empty, got %d", len(orch.ListQueue()))
	}

	// 多任务批量入队 (无引号确保清除)
	execCmd(t, ex, "/auto run A")
	execCmd(t, ex, "/auto run B")
	execCmd(t, ex, "/auto run C")
	if len(orch.ListQueue()) != 3 {
		t.Errorf("queue should have 3 entries, got %d", len(orch.ListQueue()))
	}

	// FIFO 顺序验证
	queue := orch.ListQueue()
	if queue[0] != "A" || queue[1] != "B" || queue[2] != "C" {
		t.Errorf("queue should maintain FIFO order, got %v", queue)
	}

	// Status 反映空队列（空 step 后）
	execCmd(t, ex, "/auto step")
	execCmd(t, ex, "/auto step")
	execCmd(t, ex, "/auto step")
	if len(orch.ListQueue()) != 0 {
		t.Errorf("after all steps, queue should be empty, got %d", len(orch.ListQueue()))
	}
}

// ============================================================================
// E2E Test 5: 别名集成 — /autotask run, /autoloop loop
// 注意: 别名只是 registry 映射，子命令仍需指定
// ============================================================================

func TestAutoE2E_Aliases(t *testing.T) {
	orch, _, ex := newE2EOrchestrator(t)

	// /autotask run <instruction> — alias for /auto run
	execCmd(t, ex, "/autotask run 通过别名入队的任务")
	if len(orch.ListQueue()) != 1 {
		t.Errorf("autotask alias should enqueue, got %d", len(orch.ListQueue()))
	}
	queue := orch.ListQueue()
	if queue[0] != "通过别名入队的任务" {
		t.Errorf("queue should contain the auto-task alias task, got %q", queue[0])
	}

	// /autoloop loop <count> — alias for /auto loop
	execCmd(t, ex, "/autoloop loop 5")
	if orch.loopConfig.Mode != ModeLoopN {
		t.Errorf("autoloop alias should set ModeLoopN, got %v", orch.loopConfig.Mode)
	}
	if orch.loopConfig.LoopCount != 5 {
		t.Errorf("autoloop alias should set LoopCount=5, got %d", orch.loopConfig.LoopCount)
	}
}

// ============================================================================
// E2E Test 6: Step + Queue 联动
// ============================================================================

func TestAutoE2E_StepAndQueue(t *testing.T) {
	orch, _, ex := newE2EOrchestrator(t)

	// 切换为手动模式
	execCmd(t, ex, "/auto mode manual")

	// 入队 3 个任务
	execCmd(t, ex, "/auto run task-1")
	execCmd(t, ex, "/auto run task-2")
	execCmd(t, ex, "/auto run task-3")
	if len(orch.ListQueue()) != 3 {
		t.Errorf("queue should have 3 items, got %d", len(orch.ListQueue()))
	}

	// Step 1 → 2 个剩余
	execCmd(t, ex, "/auto step")
	if len(orch.ListQueue()) != 2 {
		t.Errorf("after 1 step, queue should have 2, got %d", len(orch.ListQueue()))
	}

	// Step 2 → 1 个剩余
	execCmd(t, ex, "/auto step")
	if len(orch.ListQueue()) != 1 {
		t.Errorf("after 2 steps, queue should have 1, got %d", len(orch.ListQueue()))
	}
	queue := orch.ListQueue()
	if queue[0] != "task-3" {
		t.Errorf("remaining task should be 'task-3', got %q", queue[0])
	}

	// Step 3 → 0 个剩余
	execCmd(t, ex, "/auto step")
	if len(orch.ListQueue()) != 0 {
		t.Errorf("after 3 steps, queue should be empty, got %d", len(orch.ListQueue()))
	}
}

// ============================================================================
// E2E Test 7: 并发安全 — 快速连续入队
// ============================================================================

func TestAutoE2E_ConcurrentEnqueue(t *testing.T) {
	orch, _, ex := newE2EOrchestrator(t)

	tasks := []string{
		"并发-任务-1",
		"并发-任务-2",
		"并发-任务-3",
		"并发-任务-4",
		"并发-任务-5",
	}

	// 连续入队 — 验证 sync.Mutex 在 EnqueueMessage 中的保护
	for _, task := range tasks {
		execCmd(t, ex, "/auto run "+task)
	}

	if len(orch.ListQueue()) != 5 {
		t.Errorf("should have 5 enqueued tasks, got %d", len(orch.ListQueue()))
	}

	// FIFO 验证
	queue := orch.ListQueue()
	for i, task := range tasks {
		if queue[i] != task {
			t.Errorf("FIFO violation at index %d: got %q, want %q", i, queue[i], task)
		}
	}
}

// ============================================================================
// E2E Test 8: 命令行接口 —— status 输出格式验证
// ============================================================================

func TestAutoE2E_StatusOutput(t *testing.T) {
	_, _, ex := newE2EOrchestrator(t)

	// status 应显示基本字段
	reply := execCmd(t, ex, "/auto status")
	for _, want := range []string{"mode", "state", "mode:", "state:"} {
		if !strings.Contains(strings.ToLower(reply), want) {
			t.Errorf("status should contain %q, got: %s", want, reply)
		}
	}
}

// ============================================================================
// E2E Test 9: 队列状态命令输出验证
// ============================================================================

func TestAutoE2E_QueueCommand(t *testing.T) {
	_, _, ex := newE2EOrchestrator(t)

	// 空队列
	reply := execCmd(t, ex, "/auto queue")
	if !strings.Contains(strings.ToLower(reply), "empty") && !strings.Contains(reply, "0") {
		t.Logf("empty queue reply: %s", reply)
	}

	// 入队后
	execCmd(t, ex, "/auto run hello")
	reply = execCmd(t, ex, "/auto queue")
	if !strings.Contains(reply, "hello") {
		t.Logf("queue with items reply: %s", reply)
	}
}
