package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/raft"
)

// Connector manages the WebSocket connection to a Reef Server.
type Connector struct {
	serverURL   string
	token       string
	clientID    string
	role        string
	skills      []string
	providers   []string
	capacity    int

	conn      *websocket.Conn
	mu        sync.Mutex
	sendCh    chan reef.Message
	msgInCh   chan reef.Message // messages received from server
	closed    bool

	backoff          *Backoff
	heartbeatInterval time.Duration
	logger           *slog.Logger

	// Phase 6 — Claim Board callbacks
	onTaskAvailable func(reef.TaskAvailablePayload)
	onTaskClaimed   func(reef.TaskClaimedPayload)

	// Phase 7 — Federation: multi-server pool
	pool       *raft.ClientConnPool // nil in single-address mode
	poolActive bool
}

// ConnectorOptions configures a new Connector.
type ConnectorOptions struct {
	ServerURL         string
	Token             string
	ClientID          string
	Role              string
	Skills            []string
	Providers         []string
	Capacity          int
	HeartbeatInterval time.Duration
	Logger            *slog.Logger
}

// NewConnector creates a new WebSocket connector (single-server mode).
func NewConnector(opts ConnectorOptions) *Connector {
	if opts.Capacity <= 0 {
		opts.Capacity = 1
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 10 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Connector{
		serverURL:         opts.ServerURL,
		token:             opts.Token,
		clientID:          opts.ClientID,
		role:              opts.Role,
		skills:            opts.Skills,
		providers:         opts.Providers,
		capacity:          opts.Capacity,
		sendCh:            make(chan reef.Message, 64),
		msgInCh:           make(chan reef.Message, 16),
		backoff:           NewBackoff(1*time.Second, 60*time.Second),
		heartbeatInterval: opts.HeartbeatInterval,
		logger:            opts.Logger,
	}
}

// NewPoolConnector creates a Connector backed by a multi-server ClientConnPool.
// The pool manages connections to all configured servers, auto-detects the
// leader via raft_leader_change messages, and routes Send() to the leader.
// Pass the same ConnectorOptions as NewConnector; the ServerURL field is ignored
// in favor of the pool config.
func NewPoolConnector(opts ConnectorOptions, poolCfg raft.PoolConfig) (*Connector, error) {
	if opts.Capacity <= 0 {
		opts.Capacity = 1
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 10 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	pool, err := raft.NewClientConnPool(poolCfg, opts.Logger)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	return &Connector{
		token:             opts.Token,
		clientID:          opts.ClientID,
		role:              opts.Role,
		skills:            opts.Skills,
		providers:         opts.Providers,
		capacity:          opts.Capacity,
		sendCh:            make(chan reef.Message, 64),
		msgInCh:           make(chan reef.Message, 16),
		backoff:           NewBackoff(1*time.Second, 60*time.Second),
		heartbeatInterval: opts.HeartbeatInterval,
		logger:            opts.Logger,
		pool:              pool,
	}, nil
}

// Connect establishes the WebSocket connection and starts background loops.
// In pool mode, starts all pool connections and a pool-receive relay goroutine.
// In single-address mode, dials and registers with the server.
func (c *Connector) Connect(ctx context.Context) error {
	if c.pool != nil {
		return c.connectPool(ctx)
	}
	if err := c.dialAndRegister(ctx); err != nil {
		return err
	}
	go c.readLoop(ctx)
	go c.writeLoop(ctx)
	go c.heartbeatLoop(ctx)
	go c.reconnectLoop(ctx)
	return nil
}

func (c *Connector) connectPool(ctx context.Context) error {
	c.pool.Start()
	c.poolActive = true

	// Relay goroutine: pool.Receive() → msgInCh
	go c.poolRelayLoop(ctx)

	return nil
}

// poolRelayLoop forwards messages from the pool's Receive channel into msgInCh.
func (c *Connector) poolRelayLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case rm, ok := <-c.pool.Receive():
			if !ok {
				return
			}
			var msg reef.Message
			if err := json.Unmarshal(rm.Data, &msg); err != nil {
				c.logger.Warn("pool unmarshal", "addr", rm.Addr, "error", err)
				continue
			}
			if !msg.MsgType.IsValid() {
				continue
			}

			// Handle claim board callbacks
			switch msg.MsgType {
			case reef.MsgTaskAvailable:
				var payload reef.TaskAvailablePayload
				if err := msg.DecodePayload(&payload); err == nil {
					c.mu.Lock()
					cb := c.onTaskAvailable
					c.mu.Unlock()
					if cb != nil {
						go cb(payload)
					}
				}
			case reef.MsgTaskClaimed:
				var payload reef.TaskClaimedPayload
				if err := msg.DecodePayload(&payload); err == nil {
					c.mu.Lock()
					cb := c.onTaskClaimed
					c.mu.Unlock()
					if cb != nil {
						go cb(payload)
					}
				}
			}

			select {
			case c.msgInCh <- msg:
			default:
				c.logger.Warn("incoming message dropped, buffer full")
			}
		}
	}
}

// Messages returns the channel of incoming server messages.
func (c *Connector) Messages() <-chan reef.Message { return c.msgInCh }

// Send queues a message for transmission to the server.
// In pool mode, sends to the current leader.
func (c *Connector) Send(msg reef.Message) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("connector closed")
	}
	c.mu.Unlock()

	if c.poolActive {
		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		return c.pool.SendToLeader(data)
	}

	select {
	case c.sendCh <- msg:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// Close shuts down the connector.
func (c *Connector) Close() error {
	c.mu.Lock()
	c.closed = true
	conn := c.conn
	poolActive := c.poolActive
	c.mu.Unlock()

	if poolActive {
		c.pool.Stop()
		c.poolActive = false
	}
	if conn != nil {
		conn.Close()
	}
	close(c.sendCh)
	close(c.msgInCh)
	return nil
}

// SendToAll broadcasts a message to all connected servers.
// In single-server mode, falls back to Send.
func (c *Connector) SendToAll(msg reef.Message) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("connector closed")
	}
	c.mu.Unlock()

	if c.poolActive {
		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		c.pool.SendToAll(data)
		return nil
	}
	return c.Send(msg) // fallback: send to single server
}

// LeaderAddr returns the current leader's address.
// In pool mode, returns the detected leader address; in single-server mode,
// returns the configured server URL.
func (c *Connector) LeaderAddr() string {
	if c.poolActive {
		return c.pool.LeaderAddr()
	}
	return c.serverURL
}

// Pool returns the underlying ClientConnPool, or nil if not in pool mode.
func (c *Connector) Pool() *raft.ClientConnPool {
	return c.pool
}

// SetOnTaskAvailable sets the callback invoked when a task_available message is received.
// The callback is called from a goroutine to avoid blocking the message loop.
func (c *Connector) SetOnTaskAvailable(fn func(reef.TaskAvailablePayload)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onTaskAvailable = fn
}

// SetOnTaskClaimed sets the callback invoked when a task_claimed message is received.
// The callback is called from a goroutine to avoid blocking the message loop.
func (c *Connector) SetOnTaskClaimed(fn func(reef.TaskClaimedPayload)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onTaskClaimed = fn
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (c *Connector) dialAndRegister(ctx context.Context) error {
	header := http.Header{}
	if c.token != "" {
		header.Set("x-reef-token", c.token)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	ws, _, err := dialer.DialContext(ctx, c.serverURL, header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	c.mu.Lock()
	c.conn = ws
	c.mu.Unlock()

	// Send register message
	reg, err := reef.NewMessage(reef.MsgRegister, "", reef.RegisterPayload{
		ProtocolVersion: reef.ProtocolVersion,
		ClientID:        c.clientID,
		Role:            c.role,
		Skills:          c.skills,
		Providers:       c.providers,
		Capacity:        c.capacity,
		Timestamp:       time.Now().UnixMilli(),
	})
	if err != nil {
		ws.Close()
		return fmt.Errorf("build register: %w", err)
	}
	if err := c.writeMessage(ws, reg); err != nil {
		ws.Close()
		return fmt.Errorf("send register: %w", err)
	}

	c.backoff.Reset()
	c.logger.Info("registered with server", slog.String("client_id", c.clientID))
	return nil
}

func (c *Connector) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		ws := c.conn
		c.mu.Unlock()
		if ws == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		_, data, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.logger.Warn("websocket read error", slog.String("error", err.Error()))
			}
			c.triggerReconnect()
			continue
		}

		var msg reef.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			c.logger.Warn("unmarshal message", slog.String("error", err.Error()))
			continue
		}
		if !msg.MsgType.IsValid() {
			c.logger.Warn("unknown message type", slog.String("msg_type", string(msg.MsgType)))
			continue
		}

		// Phase 6 — Claim Board messages go to callbacks (non-blocking goroutines)
		switch msg.MsgType {
		case reef.MsgTaskAvailable:
			var payload reef.TaskAvailablePayload
			if err := msg.DecodePayload(&payload); err == nil {
				c.mu.Lock()
				cb := c.onTaskAvailable
				c.mu.Unlock()
				if cb != nil {
					go cb(payload)
				}
			}
		case reef.MsgTaskClaimed:
			var payload reef.TaskClaimedPayload
			if err := msg.DecodePayload(&payload); err == nil {
				c.mu.Lock()
				cb := c.onTaskClaimed
				c.mu.Unlock()
				if cb != nil {
					go cb(payload)
				}
			}
		}

		select {
		case c.msgInCh <- msg:
		default:
			c.logger.Warn("incoming message dropped, buffer full")
		}
	}
}

func (c *Connector) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.sendCh:
			if !ok {
				return
			}
			c.mu.Lock()
			ws := c.conn
			c.mu.Unlock()
			if ws == nil {
				c.logger.Warn("dropped message, no connection", slog.String("msg_type", string(msg.MsgType)))
				continue
			}
			if err := c.writeMessage(ws, msg); err != nil {
				c.logger.Warn("write failed", slog.String("error", err.Error()))
				c.triggerReconnect()
			}
		}
	}
}

func (c *Connector) writeMessage(ws *websocket.Conn, msg reef.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return ws.WriteMessage(websocket.TextMessage, data)
}

func (c *Connector) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{
				Timestamp: time.Now().UnixMilli(),
			})
			_ = c.Send(msg)
		}
	}
}

func (c *Connector) reconnectLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.backoff.Wait():
			c.mu.Lock()
			needReconnect := c.conn == nil && !c.closed
			c.mu.Unlock()
			if needReconnect {
				c.logger.Info("attempting reconnect", slog.Int("attempt", c.backoff.Attempt()))
				if err := c.dialAndRegister(ctx); err != nil {
					c.logger.Warn("reconnect failed", slog.String("error", err.Error()))
					c.triggerReconnect()
				} else {
					c.logger.Info("reconnected successfully")
				}
			}
		}
	}
}

func (c *Connector) triggerReconnect() {
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()
	c.backoff.Next()
}

// ---------------------------------------------------------------------------
// Backoff
// ---------------------------------------------------------------------------

// Backoff implements exponential backoff with jitter.
type Backoff struct {
	min    time.Duration
	max    time.Duration
	current time.Duration
	attempt int
	trigger chan struct{}
}

// NewBackoff creates a backoff helper.
func NewBackoff(min, max time.Duration) *Backoff {
	return &Backoff{
		min:     min,
		max:     max,
		current: min,
		trigger: make(chan struct{}, 1),
	}
}

// Next increases the backoff duration.
func (b *Backoff) Next() {
	b.attempt++
	b.current *= 2
	if b.current > b.max {
		b.current = b.max
	}
	select {
	case b.trigger <- struct{}{}:
	default:
	}
}

// Reset clears the backoff.
func (b *Backoff) Reset() {
	b.attempt = 0
	b.current = b.min
}

// Attempt returns the current attempt count.
func (b *Backoff) Attempt() int { return b.attempt }

// Wait returns a channel that fires after the current backoff duration.
func (b *Backoff) Wait() <-chan time.Time {
	return time.After(b.current)
}
