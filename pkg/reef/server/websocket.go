package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhazhaku/reef/pkg/reef"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// Conn represents a single WebSocket connection to a Client.
type Conn struct {
	id       string
	ws       *websocket.Conn
	sendCh   chan reef.Message
	registry *Registry
	scheduler *Scheduler
	token    string
	mu       sync.Mutex
	closed   bool
}

// WebSocketServer accepts and manages Client connections.
type WebSocketServer struct {
	registry  *Registry
	scheduler *Scheduler
	token     string
	conns     sync.Map // map[string]*Conn
	logger    *slog.Logger

	pendingMu       sync.Mutex
	pendingControls map[string][]reef.Message // buffered control messages for disconnected clients

	// maxPendingControlsPerClient limits buffered control messages per disconnected client.
	// When exceeded, oldest messages are dropped (sliding window).
	maxPendingControlsPerClient int // default 100

	// geneHandler handles gene_submit messages (Phase 6 evolution engine).
	// When nil (default), gene_submit messages are logged and ignored.
	geneHandler GeneSubmitHandler
}

// GeneSubmitHandler is the interface for handling gene_submit messages.
// Implementations route genes through the evolution pipeline (gatekeeper → broadcaster → merger).
type GeneSubmitHandler interface {
	HandleGeneSubmission(clientID string, msg reef.Message) error
}

// SetGeneSubmitHandler sets the handler for gene_submit messages.
func (s *WebSocketServer) SetGeneSubmitHandler(h GeneSubmitHandler) {
	s.geneHandler = h
}

// NewWebSocketServer creates a WebSocket acceptor.
func NewWebSocketServer(registry *Registry, scheduler *Scheduler, token string, logger *slog.Logger) *WebSocketServer {
	return &WebSocketServer{
		registry:                    registry,
		scheduler:                   scheduler,
		token:                       token,
		logger:                      logger,
		pendingControls:             make(map[string][]reef.Message),
		maxPendingControlsPerClient: 100,
	}
}

// ServeHTTP implements http.Handler for WebSocket upgrade.
func (s *WebSocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.token != "" {
		if r.Header.Get("x-reef-token") != s.token {
			s.logger.Warn("websocket upgrade rejected: invalid token",
				slog.String("remote", r.RemoteAddr))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("websocket upgrade failed", slog.String("error", err.Error()))
		return
	}

	conn := &Conn{
		ws:        ws,
		sendCh:    make(chan reef.Message, 64),
		registry:  s.registry,
		scheduler: s.scheduler,
		token:     s.token,
	}

	// Wait for register message before fully accepting
	if err := s.handshake(conn); err != nil {
		s.logger.Warn("websocket handshake failed", slog.String("error", err.Error()))
		ws.Close()
		return
	}

	s.conns.Store(conn.id, conn)
	s.logger.Info("client connected", slog.String("client_id", conn.id))

	go conn.writeLoop()
	go conn.readLoop(s)
}

// handshake reads the first message which MUST be register.
func (s *WebSocketServer) handshake(conn *Conn) error {
	conn.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.ws.SetReadDeadline(time.Time{})

	_, data, err := conn.ws.ReadMessage()
	if err != nil {
		return fmt.Errorf("read register message: %w", err)
	}

	var msg reef.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	if msg.MsgType != reef.MsgRegister {
		return fmt.Errorf("expected register, got %s", msg.MsgType)
	}

	var payload reef.RegisterPayload
	if err := msg.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}

	if err := reef.ValidateProtocolVersion(payload.ProtocolVersion); err != nil {
		_ = conn.sendMessage(reef.MsgRegisterNack, "", reef.RegisterNackPayload{Reason: err.Error()})
		return err
	}

	conn.id = payload.ClientID
	info := &reef.ClientInfo{
		ID:            payload.ClientID,
		Role:          payload.Role,
		Skills:        payload.Skills,
		Providers:     payload.Providers,
		Capacity:      payload.Capacity,
		CurrentLoad:   0,
		LastHeartbeat: time.Now(),
		State:         reef.ClientConnected,
	}
	conn.registry.Register(info)

	// Flush any pending control messages for this client
	s.pendingMu.Lock()
	pending := s.pendingControls[payload.ClientID]
	delete(s.pendingControls, payload.ClientID)
	s.pendingMu.Unlock()
	for _, m := range pending {
		conn.sendCh <- m
	}

	ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
		ClientID:   payload.ClientID,
		ServerTime: time.Now().UnixMilli(),
	})
	conn.sendCh <- ack
	return nil
}

// readLoop continuously reads messages from the WebSocket.
func (c *Conn) readLoop(s *WebSocketServer) {
	defer func() {
		c.close()
		s.conns.Delete(c.id)
		s.registry.MarkDisconnected(c.id)
		s.logger.Info("client disconnected", slog.String("client_id", c.id))
	}()

	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				s.logger.Warn("websocket read error",
					slog.String("client_id", c.id),
					slog.String("error", err.Error()))
			}
			return
		}

		var msg reef.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			s.logger.Warn("unmarshal message failed",
				slog.String("client_id", c.id),
				slog.String("error", err.Error()))
			continue
		}

		if !msg.MsgType.IsValid() {
			s.logger.Warn("unknown message type",
				slog.String("client_id", c.id),
				slog.String("msg_type", string(msg.MsgType)))
			continue
		}

		s.handleMessage(c, msg)
	}
}

// writeLoop sends outbound messages through the WebSocket.
func (c *Conn) writeLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	for {
		select {
		case msg, ok := <-c.sendCh:
			if !ok {
				_ = c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			c.mu.Lock()
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err = c.ws.WriteMessage(websocket.TextMessage, data)
			c.mu.Unlock()
			if err != nil {
				return
			}

		case <-ticker.C:
			c.mu.Lock()
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err := c.ws.WriteMessage(websocket.PingMessage, nil)
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

// handleMessage dispatches incoming messages to the appropriate handler.
func (s *WebSocketServer) handleMessage(c *Conn, msg reef.Message) {
	switch msg.MsgType {
	case reef.MsgHeartbeat:
		var payload reef.HeartbeatPayload
		_ = msg.DecodePayload(&payload)
		c.registry.UpdateHeartbeat(c.id)

	case reef.MsgTaskProgress:
		var payload reef.TaskProgressPayload
		_ = msg.DecodePayload(&payload)
		task := s.scheduler.GetTask(payload.TaskID)
		if task != nil {
			// Update status based on progress type
			switch payload.Status {
			case "started":
				_ = task.Transition(reef.TaskRunning)
			case "paused":
				_ = task.Transition(reef.TaskPaused)
			}
		}

	case reef.MsgTaskCompleted:
		var payload reef.TaskCompletedPayload
		_ = msg.DecodePayload(&payload)
		result := &reef.TaskResult{Metadata: payload.Result}
		_ = s.scheduler.HandleTaskCompleted(msg.TaskID, result)

	case reef.MsgTaskFailed:
		var payload reef.TaskFailedPayload
		_ = msg.DecodePayload(&payload)
		taskErr := &reef.TaskError{
			Type:    payload.ErrorType,
			Message: payload.ErrorMessage,
			Detail:  payload.ErrorDetail,
		}
		_ = s.scheduler.HandleTaskFailed(msg.TaskID, taskErr, payload.AttemptHistory)

	case reef.MsgControlAck:
		// Log control acknowledgments
		var payload reef.ControlAckPayload
		_ = msg.DecodePayload(&payload)
		s.logger.Info("control ack received",
			slog.String("client_id", c.id),
			slog.String("control_type", payload.ControlType),
			slog.String("task_id", payload.TaskID))

	case reef.MsgGeneSubmit:
		if s.geneHandler == nil {
			s.logger.Warn("gene_submit received but no GeneSubmitHandler configured",
				slog.String("client_id", c.id))
			break
		}
		if err := s.geneHandler.HandleGeneSubmission(c.id, msg); err != nil {
			s.logger.Error("gene submission failed",
				slog.String("client_id", c.id),
				slog.String("error", err.Error()))
		}

	default:
		s.logger.Warn("unexpected message type from client",
			slog.String("client_id", c.id),
			slog.String("msg_type", string(msg.MsgType)))
	}
}

// SendMessage sends a message to a specific client by ID.
// If the client is disconnected, control messages are buffered for replay on reconnect.
func (s *WebSocketServer) SendMessage(clientID string, msg reef.Message) error {
	v, ok := s.conns.Load(clientID)
	if !ok {
		// Buffer control messages for disconnected clients
		if isControlMessage(msg.MsgType) {
			s.pendingMu.Lock()
			pending := s.pendingControls[clientID]
			if len(pending) >= s.maxPendingControlsPerClient {
				// Sliding window: drop oldest, keep newest
				pending = pending[1:]
			}
			s.pendingControls[clientID] = append(pending, msg)
			s.pendingMu.Unlock()
			return nil
		}
		return fmt.Errorf("client %s not connected", clientID)
	}
	conn := v.(*Conn)
	select {
	case conn.sendCh <- msg:
		return nil
	default:
		return fmt.Errorf("client %s send buffer full", clientID)
	}
}

func isControlMessage(mt reef.MessageType) bool {
	switch mt {
	case reef.MsgCancel, reef.MsgPause, reef.MsgResume:
		return true
	}
	return false
}

// Broadcast sends a message to all connected clients.
func (s *WebSocketServer) Broadcast(msg reef.Message) {
	s.conns.Range(func(key, value any) bool {
		conn := value.(*Conn)
		select {
		case conn.sendCh <- msg:
		default:
			// drop if buffer full
		}
		return true
	})
}

// CloseConnection forcibly closes a client connection.
func (s *WebSocketServer) CloseConnection(clientID string) {
	if v, ok := s.conns.Load(clientID); ok {
		v.(*Conn).close()
		s.conns.Delete(clientID)
	}
}

func (c *Conn) sendMessage(msgType reef.MessageType, taskID string, payload any) error {
	msg, err := reef.NewMessage(msgType, taskID, payload)
	if err != nil {
		return err
	}
	select {
	case c.sendCh <- msg:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

func (c *Conn) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	close(c.sendCh)
	c.ws.Close()
}
