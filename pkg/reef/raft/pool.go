// Package raft provides ClientConnPool — multi-server WebSocket connection pool
// with automatic leader discovery and routing.
package raft

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sipeed/reef/pkg/reef"
)

// =====================================================================
// serverConn — per-server WebSocket connection state
// =====================================================================

type serverConn struct {
	Addr     string
	Conn     *websocket.Conn
	IsLeader bool
	mu       sync.Mutex
	LastPing time.Time
}

// =====================================================================
// ClientConnPool — full implementation
// =====================================================================

type ClientConnPool struct {
	mu          sync.RWMutex
	servers     []*serverConn
	leaderIndex int // index into servers, -1 if unknown
	config      PoolConfig
	logger      *slog.Logger

	// Message channels
	sendCh chan poolMessage  // Outgoing messages queued for sending
	recvCh chan ReceivedMessage // Incoming messages from all connections

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Callbacks
	onLeaderChange func(newLeaderAddr string)
}

// poolMessage is an internal message queued for sending.
type poolMessage struct {
	msg      []byte  // Serialized JSON message
	target   int     // Server index, or -1 for leader, -2 for broadcast
	response chan error
}

// ReceivedMessage carries an inbound message from a specific server address.
type ReceivedMessage struct {
	Addr string
	Data []byte
}

// PoolConfig holds configuration for ClientConnPool.
type PoolConfig struct {
	ServerAddrs      []string      `json:"server_addrs"`
	ReconnectBackoff time.Duration `json:"reconnect_backoff"`
	MaxReconnect     time.Duration `json:"max_reconnect"`
	PingInterval     time.Duration `json:"ping_interval"`
}

// DefaultPoolConfig returns sensible defaults.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}
}

// Validate checks the pool configuration.
func (c PoolConfig) Validate() error {
	if len(c.ServerAddrs) == 0 {
		return fmt.Errorf("ServerAddrs must not be empty")
	}
	if c.ReconnectBackoff <= 0 {
		return fmt.Errorf("ReconnectBackoff must be positive")
	}
	if c.MaxReconnect <= 0 {
		return fmt.Errorf("MaxReconnect must be positive")
	}
	if c.PingInterval <= 0 {
		return fmt.Errorf("PingInterval must be positive")
	}
	if c.MaxReconnect < c.ReconnectBackoff {
		return fmt.Errorf("MaxReconnect (%v) must be >= ReconnectBackoff (%v)", c.MaxReconnect, c.ReconnectBackoff)
	}
	return nil
}

// =====================================================================
// Constructor
// =====================================================================

// NewClientConnPool creates a new ClientConnPool. Does NOT connect yet — call Start().
func NewClientConnPool(config PoolConfig, logger *slog.Logger) (*ClientConnPool, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Deduplicate addresses
	seen := make(map[string]bool)
	deduped := make([]string, 0, len(config.ServerAddrs))
	for _, addr := range config.ServerAddrs {
		if !seen[addr] {
			seen[addr] = true
			deduped = append(deduped, addr)
		} else {
			logger.Warn("duplicate server address, skipping", "addr", addr)
		}
	}
	config.ServerAddrs = deduped

	servers := make([]*serverConn, len(config.ServerAddrs))
	for i, addr := range config.ServerAddrs {
		servers[i] = &serverConn{Addr: addr}
	}

	return &ClientConnPool{
		servers:     servers,
		leaderIndex: -1,
		config:      config,
		logger:      logger,
		sendCh:      make(chan poolMessage, 256),
		recvCh:      make(chan ReceivedMessage, 256),
	}, nil
}

// =====================================================================
// Lifecycle
// =====================================================================

// Start launches connection goroutines, heartbeat, and dispatch loops.
func (p *ClientConnPool) Start() {
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Connection loops — one per server
	for i, addr := range p.config.ServerAddrs {
		p.wg.Add(1)
		go p.connectLoop(i, addr)
	}

	// Heartbeat loop
	p.wg.Add(1)
	go p.heartbeatLoop()

	// Dispatch (send) loop
	p.wg.Add(1)
	go p.sendLoop()
}

// Stop shuts down the pool gracefully.
func (p *ClientConnPool) Stop() {
	if p.cancel != nil {
		p.cancel()
	}

	// Close all connections to unblock read loops
	p.mu.Lock()
	for _, s := range p.servers {
		if s != nil && s.Conn != nil {
			s.Conn.Close()
		}
	}
	p.mu.Unlock()

	p.wg.Wait()
}

// Receive returns the channel of inbound messages from all servers.
func (p *ClientConnPool) Receive() <-chan ReceivedMessage {
	return p.recvCh
}

// =====================================================================
// Connection management
// =====================================================================

func (p *ClientConnPool) connectLoop(index int, addr string) {
	defer p.wg.Done()

	backoff := p.config.ReconnectBackoff

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		dialCtx, dialCancel := context.WithTimeout(p.ctx, 10*time.Second)
		conn, _, err := websocket.DefaultDialer.DialContext(dialCtx, addr, nil)
		dialCancel()
		if err != nil {
			p.logger.Error("connect failed", "addr", addr, "error", err)

			select {
			case <-p.ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff *= 2
			if backoff > p.config.MaxReconnect {
				backoff = p.config.MaxReconnect
			}
			continue
		}

		// Reset backoff on successful connection
		backoff = p.config.ReconnectBackoff

		p.mu.Lock()
		if p.servers[index] != nil {
			p.servers[index].Conn = conn
			p.servers[index].LastPing = time.Now()
		}
		p.mu.Unlock()

		p.logger.Info("connected to server", "addr", addr, "index", index)

		// Read loop for this connection (blocks until disconnect)
		// Pass the server entry pointer; readLoop will read from its Conn
		p.readLoop(index)

		// Connection lost — clean up
		p.mu.Lock()
		if p.servers[index] != nil {
			if p.servers[index].Conn != nil {
				p.servers[index].Conn.Close()
			}
			p.servers[index].Conn = nil
			p.servers[index].IsLeader = false
		}
		if p.leaderIndex == index {
			p.leaderIndex = -1
		}
		p.mu.Unlock()

		p.logger.Warn("disconnected from server", "addr", addr)
	}
}

func (p *ClientConnPool) readLoop(index int) {
	defer func() {
		// Ensure connection is cleaned up on exit
		p.mu.Lock()
		if p.servers[index] != nil {
			p.servers[index].Conn = nil
			p.servers[index].IsLeader = false
			if p.leaderIndex == index {
				p.leaderIndex = -1
			}
		}
		p.mu.Unlock()
	}()

	// Monitor ctx cancellation and close connection to unblock ReadMessage
	go func() {
		<-p.ctx.Done()
		p.mu.RLock()
		if p.servers[index] != nil && p.servers[index].Conn != nil {
			p.servers[index].Conn.Close()
		}
		p.mu.RUnlock()
	}()

	for {
		p.mu.RLock()
		var conn *websocket.Conn
		var addr string
		if p.servers[index] != nil {
			conn = p.servers[index].Conn
			addr = p.servers[index].Addr
		}
		p.mu.RUnlock()

		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			return // connection closed or error — exit loop, reconnect will handle it
		}

		// Detect raft_leader_change messages
		if lc := p.detectLeaderChange(data); lc != nil {
			p.handleLeaderChange(*lc)
		}

		// Forward to receive channel
		select {
		case p.recvCh <- ReceivedMessage{Addr: addr, Data: data}:
		default:
			p.logger.Warn("recv channel full, dropped message", "addr", addr)
		}
	}
}

// =====================================================================
// Leader detection
// =====================================================================

func (p *ClientConnPool) detectLeaderChange(data []byte) *reef.RaftLeaderChangePayload {
	if !bytes.Contains(data, []byte("raft_leader_change")) {
		return nil
	}
	var msg struct {
		MsgType string                       `json:"msg_type"`
		Payload reef.RaftLeaderChangePayload `json:"payload"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil
	}
	if msg.MsgType != "raft_leader_change" {
		return nil
	}
	return &msg.Payload
}

func (p *ClientConnPool) handleLeaderChange(payload reef.RaftLeaderChangePayload) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Update the new leader
	found := false
	for i, s := range p.servers {
		if s != nil && s.Addr == payload.NewLeaderAddr {
			s.IsLeader = true
			p.leaderIndex = i
			found = true
		}
	}

	if !found {
		p.logger.Warn("leader change to unknown address", "new_leader", payload.NewLeaderAddr)
	}

	// Clear the old leader
	for _, s := range p.servers {
		if s != nil && s.Addr == payload.OldLeaderAddr {
			s.IsLeader = false
		}
	}

	p.logger.Info("leader changed",
		"new_leader", payload.NewLeaderAddr,
		"old_leader", payload.OldLeaderAddr,
		"term", payload.Term,
	)

	// Fire callback asynchronously
	if p.onLeaderChange != nil {
		go p.onLeaderChange(payload.NewLeaderAddr)
	}
}

// LeaderAddr returns the current leader's address, or "" if unknown.
func (p *ClientConnPool) LeaderAddr() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.leaderIndex >= 0 && p.leaderIndex < len(p.servers) && p.servers[p.leaderIndex] != nil {
		return p.servers[p.leaderIndex].Addr
	}
	return ""
}

// LeaderIndex returns the current leader's server index, or -1 if unknown.
func (p *ClientConnPool) LeaderIndex() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.leaderIndex
}

// OnLeaderChange updates the pool's leader based on a raft_leader_change payload.
// This is the public API; it is also called automatically from the read loop.
func (p *ClientConnPool) OnLeaderChange(payload reef.RaftLeaderChangePayload) {
	p.handleLeaderChange(payload)
}

// SetOnLeaderChange registers a callback for leader transitions.
func (p *ClientConnPool) SetOnLeaderChange(fn func(newLeaderAddr string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onLeaderChange = fn
}

// =====================================================================
// Sending
// =====================================================================

// SendToLeader sends a raw JSON message to the current leader with
// exponential-backoff retry (max 3 attempts). Returns error if no
// leader is available after all attempts.
func (p *ClientConnPool) SendToLeader(msg []byte) error {
	maxAttempts := 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		p.mu.RLock()
		leaderIdx := p.leaderIndex
		hasLeader := leaderIdx >= 0 && leaderIdx < len(p.servers) && p.servers[leaderIdx] != nil
		var sc *serverConn
		if hasLeader {
			sc = p.servers[leaderIdx]
		}
		p.mu.RUnlock()

		if sc != nil && sc.Conn != nil {
			sc.mu.Lock()
			err := sc.Conn.WriteMessage(websocket.TextMessage, msg)
			sc.mu.Unlock()
			if err == nil {
				return nil
			}
			p.logger.Warn("send to leader failed", "addr", sc.Addr, "error", err)

			// Mark disconnected
			p.mu.Lock()
			if p.servers[leaderIdx] == sc {
				p.servers[leaderIdx].Conn = nil
				p.servers[leaderIdx].IsLeader = false
				p.leaderIndex = -1
			}
			p.mu.Unlock()
		}

		if attempt == maxAttempts-1 {
			break
		}

		// Wait for a new leader to emerge
		waitTime := time.Duration(attempt+1) * p.config.ReconnectBackoff
		p.logger.Info("waiting for new leader", "wait_ms", waitTime.Milliseconds())

		// If ctx is nil (Start not called), return immediately
		if p.ctx == nil {
			return fmt.Errorf("no leader available (pool not started)")
		}

		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-time.After(waitTime):
		}
	}
	return fmt.Errorf("no leader available after %d attempts", maxAttempts)
}

// SendToAll broadcasts a raw JSON message to all connected servers.
// Returns immediately without waiting for confirmation.
func (p *ClientConnPool) SendToAll(msg []byte) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, sc := range p.servers {
		if sc != nil && sc.Conn != nil {
			sc := sc // capture
			go func() {
				sc.mu.Lock()
				defer sc.mu.Unlock()
				if sc.Conn == nil {
					return // connection was closed between check and lock
				}
				if err := sc.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					p.logger.Warn("broadcast write failed", "addr", sc.Addr, "error", err)
				}
			}()
		}
	}
}

// SendToLeaderJSON is a convenience wrapper that marshals a value to JSON
// and sends it to the leader.
func (p *ClientConnPool) SendToLeaderJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return p.SendToLeader(data)
}

// SendToAllJSON is a convenience wrapper that marshals a value to JSON
// and broadcasts it to all servers.
func (p *ClientConnPool) SendToAllJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		p.logger.Warn("marshal for broadcast failed", "error", err)
		return
	}
	p.SendToAll(data)
}

// =====================================================================
// Heartbeat
// =====================================================================

func (p *ClientConnPool) heartbeatLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.mu.RLock()
			servers := make([]*serverConn, len(p.servers))
			copy(servers, p.servers)
			p.mu.RUnlock()

			for i, sc := range servers {
				if sc == nil || sc.Conn == nil {
					continue
				}
				sc.mu.Lock()
				err := sc.Conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
				if err == nil {
					sc.LastPing = time.Now()
				}
				sc.mu.Unlock()

				if err != nil {
					p.logger.Warn("heartbeat ping failed", "addr", sc.Addr, "error", err)
					// Mark as disconnected
					p.mu.Lock()
					if p.servers[i] == sc && p.servers[i].Conn != nil {
						p.servers[i].Conn.Close()
						p.servers[i].Conn = nil
						p.servers[i].IsLeader = false
						if p.leaderIndex == i {
							p.leaderIndex = -1
						}
					}
					p.mu.Unlock()
				}
			}
		}
	}
}

// =====================================================================
// Send dispatch loop
// =====================================================================

func (p *ClientConnPool) sendLoop() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case pm, ok := <-p.sendCh:
			if !ok {
				return
			}
			p.dispatchMessage(pm)
		}
	}
}

func (p *ClientConnPool) dispatchMessage(pm poolMessage) {
	switch {
	case pm.target == -1:
		// Send to leader
		err := p.SendToLeader(pm.msg)
		if pm.response != nil {
			pm.response <- err
		}
	case pm.target == -2:
		// Broadcast
		p.SendToAll(pm.msg)
		if pm.response != nil {
			pm.response <- nil
		}
	default:
		// Send to specific server
		p.mu.RLock()
		var sc *serverConn
		if pm.target >= 0 && pm.target < len(p.servers) {
			sc = p.servers[pm.target]
		}
		p.mu.RUnlock()

		if sc == nil || sc.Conn == nil {
			if pm.response != nil {
				pm.response <- fmt.Errorf("server %d not connected", pm.target)
			}
			return
		}

		sc.mu.Lock()
		err := sc.Conn.WriteMessage(websocket.TextMessage, pm.msg)
		sc.mu.Unlock()

		if pm.response != nil {
			pm.response <- err
		}
	}
}

// =====================================================================
// Server count / introspection
// =====================================================================

// ServerCount returns the number of configured servers.
func (p *ClientConnPool) ServerCount() int {
	return len(p.servers)
}

// ConnectedCount returns the number of currently connected servers.
func (p *ClientConnPool) ConnectedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, s := range p.servers {
		if s != nil && s.Conn != nil {
			count++
		}
	}
	return count
}
