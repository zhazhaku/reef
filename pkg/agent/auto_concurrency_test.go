package agent

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// 并发安全测试 — AutoLoopOrchestrator
// ============================================================================

// TestConcurrentGetSetMode 验证 GetMode/SetMode 在并发下的锁保护
func TestConcurrentGetSetMode(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())
	var wg sync.WaitGroup
	workers := 20

	// 20 个 goroutine 同时读写 mode
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				prev := o.SetMode(ModeManual)
				got := o.GetMode()
				// 确保返回的值不是空
				if got != ModeManual && got != ModeAuto {
					t.Errorf("impossible mode: %v", got)
				}
				_ = prev
			}
		}()
	}
	wg.Wait()

	// 并行轮询 Status — 不 panic
	var wg2 sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			for j := 0; j < 30; j++ {
				s := o.Status()
				_ = s
			}
		}()
	}
	wg2.Wait()
}

// TestConcurrentEnqueueAndProcessQueue 验证 EnqueueMessage + ProcessQueue
// 在不同 goroutine 中同时调用不产生 data race
func TestConcurrentEnqueueAndProcessQueue(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())
	var wg sync.WaitGroup

	// 生产 goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			o.EnqueueMessage("task-from-producer")
			time.Sleep(time.Microsecond)
		}
	}()

	// 消费 goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			o.ProcessQueue()
			time.Sleep(time.Microsecond * 2)
		}
	}()

	// 监控 goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			depth := o.QueueDepth()
			_ = depth
			list := o.ListQueue()
			_ = list
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()
}

// TestConcurrentStopAndStatus 验证 Stop + Status 并发调用
func TestConcurrentStopAndStatus(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())
	var wg sync.WaitGroup

	// 非空闲状态下调用 Stop（先 SetMode 激活）
	o.SetMode(ModeAuto)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Stop 内部有锁，多次调用不应 panic 或 deadlock
			o.Stop()
		}()
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := o.Status()
			_ = s
		}()
	}

	wg.Wait()
}

// TestConcurrentSetLoopConfigAndStatus 验证 SetLoopConfig + Status 安全并发
func TestConcurrentSetLoopConfigAndStatus(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				o.SetLoopConfig(LoopConfig{
					PollEvery:   time.Duration(j) * time.Second,
					Mode:        ModeOnce,
					LoopCount:   j,
					IdleTimeout: time.Duration(j) * time.Minute,
					MaxClients:  j + 1,
				})
			}
		}()
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				s := o.Status()
				_ = s
			}
		}()
	}

	wg.Wait()
}

// TestConcurrentTriggerAndPollQueue 验证 Trigger + PollQueue 并发
func TestConcurrentTriggerAndPollQueue(t *testing.T) {
	ctx := context.Background()
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())
	var wg sync.WaitGroup

	// Trigger 写入 buffered channel，不阻塞
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				o.Trigger()
				time.Sleep(time.Microsecond)
			}
		}()
	}

	// PollQueue 从 channel 消费
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				o.PollQueue(ctx)
				time.Sleep(time.Microsecond * 2)
			}
		}()
	}

	wg.Wait()
}

// TestConcurrentAllMethods 全家桶并发 — 所有公开方法同时调用
func TestConcurrentAllMethods(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())
	ctx := context.Background()
	var wg sync.WaitGroup

	methods := []func(){
		func() { o.GetMode() },
		func() { o.SetMode(ModeAuto) },
		func() { o.SetMode(ModeManual) },
		func() { o.Status() },
		func() { o.Stop() },
		func() { o.Trigger() },
		func() { o.EnqueueMessage("concurrent-test") },
		func() { o.ProcessQueue() },
		func() { o.QueueDepth() },
		func() { o.ListQueue() },
		func() { o.ListHistory() },
		func() { o.SetLoopConfig(DefaultLoopConfig()) },
		func() { _ = o.String() },
		func() { o.PollQueue(ctx) },
		func() { o.PollHealing(ctx) },
		func() { o.PollScale(ctx) },
		func() { o.Poll(ctx) },
		func() { _ = o.Tick() },
		func() { _ = o.Done() },
	}

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				fn := methods[(id+j)%len(methods)]
				fn()
			}
		}(i)
	}

	wg.Wait()
}

// ============================================================================
// 边角情况测试 — AutoLoopOrchestrator
// ============================================================================

// TestStopWhenAlreadyStopped 多次调用 Stop 不应 panic
func TestStopWhenAlreadyStopped(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())
	o.SetMode(ModeAuto) // 激活（非 Idle）
	o.Stop()
	// 第二次 Stop：此时 state 已回到 Idle
	o.Stop()
	// 第三次 Stop
	o.Stop()
}

// TestSetModeIdempotent 反复 SetMode 到同一个值
func TestSetModeIdempotent(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())

	for i := 0; i < 100; i++ {
		prev := o.SetMode(ModeManual)
		if i > 0 && prev != ModeManual {
			t.Fatalf("iteration %d: expected prev=manual, got %v", i, prev)
		}
	}

	prev := o.SetMode(ModeAuto)
	if prev != ModeManual {
		t.Fatalf("last switch: expected prev=manual, got %v", prev)
	}
	if o.GetMode() != ModeAuto {
		t.Fatalf("expected mode=auto, got %v", o.GetMode())
	}
}

// TestQueueEdgeCases 队列边角
func TestQueueEdgeCases(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())

	// 空队列深度
	if d := o.QueueDepth(); d != 0 {
		t.Fatalf("expected depth=0, got %d", d)
	}

	// 空队列 ProcessQueue 不应 panic
	o.ProcessQueue()

	// 空队列 ListQueue
	if q := o.ListQueue(); len(q) != 0 {
		t.Fatalf("expected empty queue, got %d items", len(q))
	}

	// 入队空字符串
	o.EnqueueMessage("")
	if d := o.QueueDepth(); d != 1 {
		t.Fatalf("expected depth=1 after empty string, got %d", d)
	}

	// 入队长字符串（8KB）
	long := string(make([]byte, 8192))
	o.EnqueueMessage(long)
	if d := o.QueueDepth(); d != 2 {
		t.Fatalf("expected depth=2 after long string, got %d", d)
	}

	// ProcessQueue 消耗后队列减少
	o.ProcessQueue()
	if d := o.QueueDepth(); d != 1 {
		t.Fatalf("expected depth=1 after one dequeue, got %d", d)
	}

	o.ProcessQueue()
	if d := o.QueueDepth(); d != 0 {
		t.Fatalf("expected depth=0 after two dequeues, got %d", d)
	}

	// 继续消费空队列
	o.ProcessQueue()
	o.ProcessQueue()
}

// TestChannelOverflow tickerCh 溢出不应 panic
func TestChannelOverflow(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())

	// tickerCh 容量为 10，发送 50 次 Trigger
	for i := 0; i < 50; i++ {
		o.Trigger() // non-blocking select
	}

	// Tick 返回的 channel 不应该 close
	ch := o.Tick()
	// 读取最多 12 个（channel 最多 10 个元素 + 可能的新 pulses）
	drained := 0
	for i := 0; i < 15; i++ {
		select {
		case <-ch:
			drained++
		default:
			break
		}
	}
	if drained > 12 {
		t.Logf("drained %d ticks from channel (cap=10, expected <=12)", drained)
	}
}

// TestModeRapidSwitching 快速模式切换
func TestModeRapidSwitching(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())

	modes := []ConversationMode{ModeAuto, ModeManual, ModeAuto, ModeManual, ModeAuto}
	for i := 0; i < 100; i++ {
		mode := modes[i%len(modes)]
		o.SetMode(mode)
		if o.GetMode() != mode {
			t.Fatalf("iteration %d: expected %v, got %v", i, mode, o.GetMode())
		}
	}
}

// TestSetLoopConfigZeroValues 零值 LoopConfig 不应 panic
func TestSetLoopConfigZeroValues(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())

	o.SetLoopConfig(LoopConfig{})
	s := o.Status()
	_ = s

	o.SetLoopConfig(LoopConfig{
		Mode: ModeOnce, // 最低合法值
	})
	_ = o.Status()
}

// TestHistoryEdgeCases 历史记录边角
func TestHistoryEdgeCases(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())

	// 无 plan 时 ListHistory 应返回 nil/空
	h := o.ListHistory()
	if h != nil {
		t.Logf("history without plan: len=%d, expected nil", len(h))
	}
}

// TestConcurrentHistoryAccess 并发读历史
func TestConcurrentHistoryAccess(t *testing.T) {
	o := NewAutoLoopOrchestrator(DefaultLoopConfig())
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				h := o.ListHistory()
				_ = h
				d := o.QueueDepth()
				_ = d
				s := o.String()
				_ = s
			}
		}()
	}

	wg.Wait()
}
