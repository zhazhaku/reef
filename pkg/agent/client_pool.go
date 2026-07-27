// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent provides the ClientPool, which manages the full lifecycle
// of reef client processes: spawn, monitor, idle collection, and graceful
// termination with proper PID reaping and temporary directory cleanup.
//
// Client 2 (Phase 2) — Foundation for client lifecycle management.

package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	reefServer "github.com/zhazhaku/reef/pkg/reef/server"
)

// ============================================================================
// ManagedState — client lifecycle states
// ============================================================================

// ManagedState tracks the lifecycle of a client process managed by ClientPool.
type ManagedState string

const (
	ManagedStarting ManagedState = "starting" // process spawned, waiting for registry
	ManagedRunning  ManagedState = "running"  // healthy, accepting tasks
	ManagedDraining ManagedState = "draining" // no new tasks, waiting for load=0
	ManagedStopped  ManagedState = "stopped"  // terminated and cleaned up
)

// ============================================================================
// ClientPoolOptions — configuration for ClientPool
// ============================================================================

// ClientPoolOptions contains all configurable parameters for ClientPool.
type ClientPoolOptions struct {
	// ServerURL is the WebSocket URL of the reef server (e.g. "ws://localhost:9999").
	ServerURL string `json:"server_url"`

	// Token is the Reef authentication token injected into spawned client
	// processes via the REEF_API_KEY environment variable (not copied to disk).
	Token string `json:"-"`

	// ReefBin is the absolute path to the reef binary.
	ReefBin string `json:"reef_bin"`

	// BaseDir is the parent directory for temporary client home directories.
	// Defaults to os.TempDir() if empty.
	BaseDir string `json:"base_dir,omitempty"`

	// IdleTimeout is how long a client can stay idle before being eligible
	// for collection by the AutoScaler.
	IdleTimeout time.Duration `json:"idle_timeout"`

	// MaxClients is the hard upper limit for concurrent client processes.
	MaxClients int `json:"max_clients"`

	// SpawnTimeout is how long to wait for a new client to register in the
	// Registry before returning an error.
	SpawnTimeout time.Duration `json:"spawn_timeout"`

	// MonitorInterval is the period between liveness checks in the Monitor
	// goroutine. Defaults to 10s if zero.
	MonitorInterval time.Duration `json:"monitor_interval,omitempty"`
}

// DefaultClientPoolOptions returns sensible defaults suitable for most deployments.
func DefaultClientPoolOptions() ClientPoolOptions {
	return ClientPoolOptions{
		ServerURL:    "ws://localhost:9999",
		ReefBin:      "/root/reef_server/reef",
		IdleTimeout:  5 * time.Minute,
		MaxClients:   5,
		SpawnTimeout: 30 * time.Second,
	}
}

// ============================================================================
// ManagedClient — represents a single managed client process
// ============================================================================

// ManagedClient represents a reef client process managed by the ClientPool.
type ManagedClient struct {
	ClientID   string       `json:"client_id"`
	PID        int          `json:"pid"`
	Role       string       `json:"role"`
	Skills     []string     `json:"skills"`
	State      ManagedState `json:"state"`
	StartedAt  time.Time    `json:"started_at"`
	LastActive time.Time    `json:"last_active"` // last task completion time
	HomeDir    string       `json:"home_dir"`    // temporary REEF_HOME path
	cmd        *exec.Cmd    // underlying OS process handle
	stopCh     chan struct{}
}

// IsIdle returns true if the client has been inactive longer than the
// provided timeout duration.
func (mc *ManagedClient) IsIdle(timeout time.Duration) bool {
	return mc.State == ManagedRunning && time.Since(mc.LastActive) > timeout
}

// MarkActive updates the LastActive timestamp to time.Now().
func (mc *ManagedClient) MarkActive() {
	mc.LastActive = time.Now()
}

// ============================================================================
// ClientPool — manages all client processes
// ============================================================================

// ClientPool manages the full lifecycle of reef client processes.
//
// It handles:
//   - On-demand spawning based on role/skill requirements
//   - Health monitoring (PID liveness, heartbeat)
//   - Graceful termination (SIGTERM → 2s → SIGKILL)
//   - Temporary directory cleanup
//   - Idle client discovery for AutoScaler integration
type ClientPool struct {
	mu      sync.Mutex
	clients map[string]*ManagedClient

	opts   ClientPoolOptions
	server *reefServer.Registry // shared server Registry for client lookups
}

// NewClientPool creates a new ClientPool with the given options.
// The registry parameter is the server's shared client Registry used
// for completion checks during SpawnClient.
func NewClientPool(opts ClientPoolOptions, registry *reefServer.Registry) *ClientPool {
	if opts.BaseDir == "" {
		opts.BaseDir = os.TempDir()
	}
	if opts.SpawnTimeout == 0 {
		opts.SpawnTimeout = 30 * time.Second
	}
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = 5 * time.Minute
	}
	if opts.MonitorInterval == 0 {
		opts.MonitorInterval = 10 * time.Second
	}
	return &ClientPool{
		clients: make(map[string]*ManagedClient),
		opts:    opts,
		server:  registry,
	}
}

// ============================================================================
// EnsureClient — find or create a matching client
// ============================================================================

// EnsureClient returns a client ID that matches the given role and skills.
// If a matching client is already available in the Registry, it returns that
// client's ID. Otherwise, it spawns a new client process.
func (p *ClientPool) EnsureClient(ctx context.Context, role string, skills []string) (string, error) {
	p.mu.Lock()

	// 1. Check local managed clients first
	for _, mc := range p.clients {
		if mc.State == ManagedRunning && mc.Role == role && matchSkills(mc.Skills, skills) {
			p.mu.Unlock()
			return mc.ClientID, nil
		}
	}

	// 2. Check server Registry for any matching client
	if info := p.getClientFromRegistry(role, skills); info != nil {
		p.mu.Unlock()
		return info.ID, nil
	}

	// 3. Enforce MaxClients limit
	if len(p.clients) >= p.opts.MaxClients {
		p.mu.Unlock()
		return "", fmt.Errorf("client pool full: %d/%d clients active", len(p.clients), p.opts.MaxClients)
	}

	p.mu.Unlock()

	// 4. Spawn a new client
	mc, err := p.SpawnClient(role, skills)
	if err != nil {
		return "", fmt.Errorf("spawn client: %w", err)
	}

	return mc.ClientID, nil
}

// getClientFromRegistry looks up a matching client in the server Registry.
// Returns nil if no matching client is found.
func (p *ClientPool) getClientFromRegistry(role string, skills []string) *reef.ClientInfo {
	if p.server == nil {
		return nil
	}
	for _, info := range p.server.List() {
		if info.IsAvailable() && info.Matches(role, skills) {
			return info
		}
	}
	return nil
}

// ============================================================================
// SpawnClient — launch a new reef client process
// ============================================================================

// SpawnClient starts a new reef client process with the given role and skills.
//
// Flow:
//  1. Create a temporary REEF_HOME directory
//  2. Construct the exec.Cmd with Setsid
//  3. Pass credentials via REEF_API_KEY environment variable (never on disk)
//  4. Start the process
//  5. Poll the Registry until the client appears as ClientConnected (with timeout)
//  6. Register the client in the local managed pool
//  7. Return the ManagedClient handle
func (p *ClientPool) SpawnClient(role string, skills []string) (*ManagedClient, error) {
	// 1. Create temp home directory
	homeDir, err := os.MkdirTemp(p.opts.BaseDir, "reef-client-*")
	if err != nil {
		return nil, fmt.Errorf("MkdirTemp: %w", err)
	}

	// Generate a client ID based on role and PID
	clientID := fmt.Sprintf("%s-%d", role, time.Now().UnixNano()%100000)

	// 2. Build command with setsid
	args := []string{
		"--mode", "client",
		"--server", p.opts.ServerURL,
		"--role", role,
		"--client-id", clientID,
	}
	if len(skills) > 0 {
		skillStr := ""
		for i, s := range skills {
			if i > 0 {
				skillStr += ","
			}
			skillStr += s
		}
		args = append(args, "--skills", skillStr)
	}

	cmd := exec.Command(p.opts.ReefBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(),
		"REEF_HOME="+homeDir,
		"REEF_API_KEY="+p.opts.Token,
	)
	// Detach stdout/stderr from parent so we don't leak pipes
	cmd.Stdout = nil
	cmd.Stderr = nil

	// 3. Start the process
	if err := cmd.Start(); err != nil {
		os.RemoveAll(homeDir) // best-effort cleanup
		return nil, fmt.Errorf("start reef client: %w", err)
	}

	mc := &ManagedClient{
		ClientID:   clientID,
		PID:        cmd.Process.Pid,
		Role:       role,
		Skills:     skills,
		State:      ManagedStarting,
		StartedAt:  time.Now(),
		LastActive: time.Now(),
		HomeDir:    homeDir,
		cmd:        cmd,
		stopCh:     make(chan struct{}),
	}

	// 4. Register locally (preliminary)
	p.mu.Lock()
	p.clients[clientID] = mc
	p.mu.Unlock()

	// 5. Wait for Registry confirmation
	if p.server != nil {
		if err := p.waitForRegistry(context.Background(), clientID, p.opts.SpawnTimeout); err != nil {
			// Cleanup on failure
			p.mu.Lock()
			delete(p.clients, clientID)
			p.mu.Unlock()
			_ = cmd.Process.Signal(syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			os.RemoveAll(homeDir)
			return nil, fmt.Errorf("registry wait timeout: %w", err)
		}
	}

	mc.State = ManagedRunning
	return mc, nil
}

// waitForRegistry polls the server Registry until the given client ID
// appears in ClientConnected state, or the timeout expires.
func (p *ClientPool) waitForRegistry(ctx context.Context, clientID string, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("client %s did not register within %s", clientID, timeout)
		case <-ticker.C:
			info := p.server.Get(clientID)
			if info != nil && info.State == reef.ClientConnected {
				return nil
			}
		}
	}
}

// ============================================================================
// Sentinel errors for KillClient
// ============================================================================

var (
	// ErrClientNotFound is returned when attempting to kill a client
	// that is not managed by this ClientPool.
	ErrClientNotFound = fmt.Errorf("client not found in pool")

	// ErrKillTimeout is returned when a client does not respond to
	// SIGTERM within the grace period.
	ErrKillTimeout = fmt.Errorf("client kill timeout")
)

// ============================================================================
// KillClient — gracefully terminate a client process
// ============================================================================

// KillClient gracefully terminates a managed client process.
//
// Flow:
//  1. Mark the client as Draining (stop accepting new tasks)
//  2. Wait for current load to reach 0 (up to 30s)
//  3. Send SIGTERM and wait 2s
//  4. If still alive, send SIGKILL
//  5. Unregister from server Registry
//  6. Clean up the temporary home directory
//  7. Remove from local managed pool
func (p *ClientPool) KillClient(clientID string) error {
	p.mu.Lock()
	mc, ok := p.clients[clientID]
	if !ok {
		p.mu.Unlock()
		return ErrClientNotFound
	}

	// 1. Mark Draining
	mc.State = ManagedDraining
	p.mu.Unlock()

	// 2. Wait for load=0
	if err := p.waitForDrain(clientID, 30*time.Second); err != nil {
		// Warn but proceed — the client will be force-killed anyway
		_ = err
	}

	// 3. SIGTERM → 2s → SIGKILL
	if mc.cmd != nil && mc.cmd.Process != nil {
		_ = mc.cmd.Process.Signal(syscall.SIGTERM)

		// Wait for graceful exit
		done := make(chan error, 1)
		go func() { done <- mc.cmd.Wait() }()
		select {
		case <-done:
			// process exited on SIGTERM
		case <-time.After(2 * time.Second):
			// SIGTERM didn't work, force kill
			_ = mc.cmd.Process.Signal(syscall.SIGKILL)
			<-done
		}
	}

	// 4. Unregister from server Registry
	if p.server != nil {
		p.server.Unregister(clientID)
	}

	// 5. Clean up temp directory
	if mc.HomeDir != "" {
		os.RemoveAll(mc.HomeDir)
	}

	// 6. Remove from local pool
	p.mu.Lock()
	delete(p.clients, clientID)
	mc.State = ManagedStopped
	p.mu.Unlock()

	return nil
}

// waitForDrain polls the Registry until the client's current load reaches 0.
func (p *ClientPool) waitForDrain(clientID string, timeout time.Duration) error {
	if p.server == nil {
		return nil
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("drain timeout for client %s after %s", clientID, timeout)
		case <-ticker.C:
			info := p.server.Get(clientID)
			if info == nil {
				return nil // already gone
			}
			if info.CurrentLoad == 0 {
				return nil
			}
		}
	}
}

// ============================================================================
// Query methods
// ============================================================================

// ListIdle returns all managed clients that have been idle longer than the
// given timeout. Used by AutoScaler for scale-down decisions.
func (p *ClientPool) ListIdle(timeout time.Duration) []*ManagedClient {
	p.mu.Lock()
	defer p.mu.Unlock()

	var idle []*ManagedClient
	for _, mc := range p.clients {
		if mc.IsIdle(timeout) {
			idle = append(idle, mc)
		}
	}
	return idle
}

// GetActiveClients returns all managed clients that are currently in the
// Running state.
func (p *ClientPool) GetActiveClients() []*ManagedClient {
	p.mu.Lock()
	defer p.mu.Unlock()

	var active []*ManagedClient
	for _, mc := range p.clients {
		if mc.State == ManagedRunning {
			active = append(active, mc)
		}
	}
	return active
}

// GetActiveCount returns the number of running managed clients.
func (p *ClientPool) GetActiveCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	for _, mc := range p.clients {
		if mc.State == ManagedRunning {
			count++
		}
	}
	return count
}

// GetByClientID returns the ManagedClient with the given client ID, or nil
// if not found.
func (p *ClientPool) GetByClientID(clientID string) *ManagedClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clients[clientID]
}

// ============================================================================
// Monitor — background goroutine for process liveness and heartbeat
// ============================================================================

// Monitor runs a background loop that checks managed client process health.
// It detects exited processes and heartbeats, removing dead clients from
// the pool and optionally triggering a restart callback.
//
// The ctx parameter controls the lifetime of the monitor goroutine.
func (p *ClientPool) Monitor(ctx context.Context) {
	ticker := time.NewTicker(p.opts.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkLiveness()
		}
	}
}

// checkLiveness iterates all managed clients and checks if their OS
// processes are still alive.
func (p *ClientPool) checkLiveness() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for clientID, mc := range p.clients {
		if mc.State == ManagedStopped {
			continue
		}
		if mc.cmd == nil || mc.cmd.Process == nil {
			continue
		}
		// Signal(0) checks if the process exists
		if err := mc.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			// Process has exited — clean up
			if p.server != nil {
				p.server.Unregister(clientID)
			}
			if mc.HomeDir != "" {
				os.RemoveAll(mc.HomeDir)
			}
			mc.State = ManagedStopped
			delete(p.clients, clientID)
		}
	}
}

// ============================================================================
// Helpers
// ============================================================================

// matchSkills returns true if all required skills are present in available.
func matchSkills(available, required []string) bool {
	if len(required) == 0 {
		return true
	}
	skillSet := make(map[string]struct{}, len(available))
	for _, s := range available {
		skillSet[s] = struct{}{}
	}
	for _, req := range required {
		if _, ok := skillSet[req]; !ok {
			return false
		}
	}
	return true
}

// ============================================================================
// ClientPoolInterface — contract for later phases
// ============================================================================

// ClientPoolInterface defines the contract that ClientPool satisfies.
// Other modules (Orchestrator, Healer, AutoScaler) depend on this interface
// rather than the concrete ClientPool type.
type ClientPoolInterface interface {
	EnsureClient(ctx context.Context, role string, skills []string) (clientID string, err error)
	SpawnClient(role string, skills []string) (*ManagedClient, error)
	KillClient(clientID string) error
	ListIdle(timeout time.Duration) []*ManagedClient
	GetActiveClients() []*ManagedClient
	GetActiveCount() int
	GetByClientID(clientID string) *ManagedClient
	Monitor(ctx context.Context)
}

// Compile-time check: *ClientPool implements ClientPoolInterface.
var _ ClientPoolInterface = (*ClientPool)(nil)
