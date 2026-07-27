// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent tests for ClientPool lifecycle management.
// TDD Phase — written before ClientPool implementation changes.

package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	reefServer "github.com/zhazhaku/reef/pkg/reef/server"
)

// ============================================================================
// Test: ClientPoolOptions defaults
// ============================================================================

func TestDefaultClientPoolOptions(t *testing.T) {
	opts := DefaultClientPoolOptions()
	if opts.ServerURL != "ws://localhost:9999" {
		t.Errorf("ServerURL = %q, want %q", opts.ServerURL, "ws://localhost:9999")
	}
	if opts.ReefBin != "/root/reef_server/reef" {
		t.Errorf("ReefBin = %q, want %q", opts.ReefBin, "/root/reef_server/reef")
	}
	if opts.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want %v", opts.IdleTimeout, 5*time.Minute)
	}
	if opts.MaxClients != 5 {
		t.Errorf("MaxClients = %d, want %d", opts.MaxClients, 5)
	}
	if opts.SpawnTimeout != 30*time.Second {
		t.Errorf("SpawnTimeout = %v, want %v", opts.SpawnTimeout, 30*time.Second)
	}
	if opts.MonitorInterval != 0 {
		// Zero is valid — defaults filled in NewClientPool
		t.Logf("MonitorInterval starts as zero (defaulted in NewClientPool)")
	}
}

// ============================================================================
// Test: NewClientPool defaults fill
// ============================================================================

func TestNewClientPoolDefaults(t *testing.T) {
	pool := NewClientPool(ClientPoolOptions{}, nil)
	if pool.opts.BaseDir == "" {
		t.Error("BaseDir should default to non-empty")
	}
	if pool.opts.SpawnTimeout != 30*time.Second {
		t.Errorf("SpawnTimeout = %v, want %v", pool.opts.SpawnTimeout, 30*time.Second)
	}
	if pool.opts.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want %v", pool.opts.IdleTimeout, 5*time.Minute)
	}
	if pool.opts.MonitorInterval != 10*time.Second {
		t.Errorf("MonitorInterval = %v, want %v", pool.opts.MonitorInterval, 10*time.Second)
	}
	if len(pool.clients) != 0 {
		t.Errorf("clients map should be empty, got %d", len(pool.clients))
	}
}

// ============================================================================
// Test: NewClientPool with custom options
// ============================================================================

func TestNewClientPoolCustomOptions(t *testing.T) {
	opts := ClientPoolOptions{
		ServerURL:        "ws://example.com:9999",
		ReefBin:          "/usr/local/bin/reef",
		IdleTimeout:      10 * time.Minute,
		MaxClients:       10,
		SpawnTimeout:     60 * time.Second,
		MonitorInterval:  15 * time.Second,
	}
	pool := NewClientPool(opts, nil)
	if pool.opts.ServerURL != "ws://example.com:9999" {
		t.Errorf("ServerURL = %q", pool.opts.ServerURL)
	}
	if pool.opts.MaxClients != 10 {
		t.Errorf("MaxClients = %d", pool.opts.MaxClients)
	}
	if pool.opts.MonitorInterval != 15*time.Second {
		t.Errorf("MonitorInterval = %v", pool.opts.MonitorInterval)
	}
}

// ============================================================================
// Test: EnsureClient with no available clients — spawn limit
// ============================================================================

func TestEnsureClientPoolFullError(t *testing.T) {
	opts := ClientPoolOptions{
		MaxClients:  1,
		SpawnTimeout: 100 * time.Millisecond, // fast timeout for testing
	}
	// Don't pass a real registry — SpawnClient will fail fast
	pool := NewClientPool(opts, nil)

	// First call should attempt spawn but fail (no reef binary available in test)
	_, err := pool.EnsureClient(context.Background(), "tester", []string{"exec"})
	if err == nil {
		t.Fatal("expected error from EnsureClient with no registry, got nil")
	}
	t.Logf("EnsureClient error (expected): %v", err)
}

// ============================================================================
// Test: ManagedClient state management
// ============================================================================

func TestManagedClientStates(t *testing.T) {
	if string(ManagedStarting) != "starting" {
		t.Errorf("ManagedStarting = %q", ManagedStarting)
	}
	if string(ManagedRunning) != "running" {
		t.Errorf("ManagedRunning = %q", ManagedRunning)
	}
	if string(ManagedDraining) != "draining" {
		t.Errorf("ManagedDraining = %q", ManagedDraining)
	}
	if string(ManagedStopped) != "stopped" {
		t.Errorf("ManagedStopped = %q", ManagedStopped)
	}
}

// ============================================================================
// Test: ManagedClient IsIdle
// ============================================================================

func TestManagedClientIsIdle(t *testing.T) {
	now := time.Now()
	mc := &ManagedClient{
		ClientID:   "test-1",
		State:      ManagedRunning,
		LastActive: now.Add(-10 * time.Minute),
	}
	if !mc.IsIdle(5 * time.Minute) {
		t.Error("client should be idle (10min inactive > 5min timeout)")
	}
	if mc.IsIdle(15 * time.Minute) {
		t.Error("client should NOT be idle (10min inactive < 15min timeout)")
	}
	if mc.State = ManagedDraining; mc.IsIdle(1*time.Minute) {
		t.Error("draining client should never be idle")
	}
}

// ============================================================================
// Test: ManagedClient MarkActive
// ============================================================================

func TestManagedClientMarkActive(t *testing.T) {
	mc := &ManagedClient{
		ClientID:   "test-2",
		State:      ManagedRunning,
		LastActive: time.Time{}, // zero value
	}
	mc.MarkActive()
	if mc.LastActive.IsZero() {
		t.Error("LastActive should be updated after MarkActive")
	}
}

// ============================================================================
// Test: ListIdle / GetActiveClients / GetActiveCount / GetByClientID
// ============================================================================

func TestClientPoolQueryMethods(t *testing.T) {
	pool := NewClientPool(DefaultClientPoolOptions(), nil)

	// Add some clients manually (without spawning)
	pool.mu.Lock()
	pool.clients["running-1"] = &ManagedClient{ClientID: "running-1", State: ManagedRunning, LastActive: time.Now()}
	pool.clients["running-2"] = &ManagedClient{ClientID: "running-2", State: ManagedRunning, LastActive: time.Now().Add(-10 * time.Minute)}
	pool.clients["idle-1"] = &ManagedClient{ClientID: "idle-1", State: ManagedRunning, LastActive: time.Now().Add(-20 * time.Minute)}
	pool.clients["draining-1"] = &ManagedClient{ClientID: "draining-1", State: ManagedDraining, LastActive: time.Now()}
	pool.mu.Unlock()

	// ListIdle
	idle := pool.ListIdle(5 * time.Minute)
	if len(idle) != 2 {
		t.Errorf("ListIdle(5min) = %d clients, want 2 (running-2 and idle-1)", len(idle))
	}
	for _, c := range idle {
		if c.State != ManagedRunning || c.IsIdle(5*time.Minute) == false {
			t.Errorf("unexpected idle client: %s", c.ClientID)
		}
	}

	// GetActiveClients
	active := pool.GetActiveClients()
	if len(active) != 3 {
		t.Errorf("GetActiveClients() = %d, want 3", len(active))
	}

	// GetActiveCount
	if count := pool.GetActiveCount(); count != 3 {
		t.Errorf("GetActiveCount() = %d, want 3", count)
	}

	// GetByClientID
	if c := pool.GetByClientID("running-1"); c == nil {
		t.Error("GetByClientID('running-1') = nil, want client")
	}
	if c := pool.GetByClientID("nonexistent"); c != nil {
		t.Errorf("GetByClientID('nonexistent') = %v, want nil", c)
	}
}

// ============================================================================
// Test: KillClient with nonexistent client returns error
// ============================================================================

func TestKillClientNotFound(t *testing.T) {
	pool := NewClientPool(DefaultClientPoolOptions(), nil)
	err := pool.KillClient("nonexistent")
	if !errors.Is(err, ErrClientNotFound) {
		t.Errorf("KillClient error = %v, want ErrClientNotFound", err)
	}
}

// ============================================================================
// Test: Registry integration — getClientFromRegistry
// ============================================================================

func TestGetClientFromRegistry(t *testing.T) {
	reg := reefServer.NewRegistry(func(clientID string) {})
	reg.Register(&reef.ClientInfo{
		ID:           "registry-client-1",
		Role:         "worker",
		Skills:       []string{"exec", "go"},
		Capacity:     2,
		CurrentLoad:  0,
		State:        reef.ClientConnected,
	})
	reg.Register(&reef.ClientInfo{
		ID:           "registry-client-2",
		Role:         "worker",
		Skills:       []string{"exec"},
		Capacity:     1,
		CurrentLoad:  1, // fully loaded
		State:        reef.ClientConnected,
	})
	reg.Register(&reef.ClientInfo{
		ID:           "registry-client-3",
		Role:         "analyst",
		Skills:       []string{"summarize"},
		Capacity:     2,
		CurrentLoad:  0,
		State:        reef.ClientDisconnected, // not connected
	})

	pool := NewClientPool(DefaultClientPoolOptions(), reg)

	// Should find client-1 (available, matching role+skills)
	info := pool.getClientFromRegistry("worker", []string{"exec"})
	if info == nil {
		t.Fatal("getClientFromRegistry returned nil, expected client-1")
	}
	if info.ID != "registry-client-1" {
		t.Errorf("getClientFromRegistry = %s, want registry-client-1", info.ID)
	}

	// Should NOT find client-2 (load=1, full capacity)
	info2 := pool.getClientFromRegistry("worker", []string{"exec"})
	if info2 == nil {
		t.Fatal("getClientFromRegistry returned nil on second call, expected client-1 still")
	}
	if info2.ID != "registry-client-1" {
		t.Errorf("expected client-1 still, got %s", info2.ID)
	}

	// Should NOT find client-3 (disconnected)
	info3 := pool.getClientFromRegistry("analyst", []string{"summarize"})
	if info3 != nil {
		t.Errorf("getClientFromRegistry for disconnected client = %v, want nil", info3)
	}

	// No matching role
	info4 := pool.getClientFromRegistry("nonexistent", nil)
	if info4 != nil {
		t.Errorf("getClientFromRegistry for nonexistent role = %v, want nil", info4)
	}
}

// ============================================================================
// Test: ClientPool monitor context cancellation
// ============================================================================

func TestMonitorContextCancellation(t *testing.T) {
	pool := NewClientPool(DefaultClientPoolOptions(), nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		pool.Monitor(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// Monitor exited cleanly on cancel
	case <-time.After(5 * time.Second):
		t.Fatal("Monitor did not exit within 5s after context cancel")
	}
}

// ============================================================================
// Test: ClientPoolInterface compile-time check
// ============================================================================

func TestClientPoolInterfaceCompileTime(t *testing.T) {
	var iface ClientPoolInterface = &ClientPool{}
	_ = iface
}
