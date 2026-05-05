package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhazhaku/reef/pkg/reef"
	evolutionsrv "github.com/zhazhaku/reef/pkg/reef/evolution/server"
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

	// evolutionHub is the server-side evolution engine hub.
	// When nil (default), gene_submit messages are silently accepted and logged.
	evolutionHub *evolutionsrv.EvolutionHub
}

// NewWebSocketServer creates a WebSocket acceptor.
func NewWebSocketServer(registry *Registry, scheduler *Scheduler, token string, logger *slog.Logger) *WebSocketServer {
	return &WebSocketServer{
		registry:        registry,
		scheduler:       scheduler,
		token:           token,
		logger:          logger,
		pendingControls: make(map[string][]reef.Message),
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
	if conn.id == "" {
		conn.id = "client-" + strconv.Itoa(int(time.Now().UnixMilli()%100000)) + "-" + strconv.FormatInt(time.Now().UnixNano()%1000000, 36)
		s.logger.Info("auto-assigned client ID", slog.String("client_id", conn.id))
	}
	info := &reef.ClientInfo{
		ID:            conn.id,
		Role:          payload.Role,
		Skills:        payload.Skills,
		Providers:     payload.Providers,
		Capacity:      payload.Capacity,
		CurrentLoad:   0,
		LastHeartbeat: time.Now(),
		State:         reef.ClientConnected,
	}
	conn.registry.Register(info)

	// Notify scheduler that a new client is available for task dispatch
	s.scheduler.HandleClientAvailable(conn.id)

	// Flush any pending control messages for this client
	s.pendingMu.Lock()
	pending := s.pendingControls[conn.id]
	delete(s.pendingControls, conn.id)
	s.pendingMu.Unlock()
	for _, m := range pending {
		conn.sendCh <- m
	}

	ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
		ClientID:   conn.id,
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

		if err := msg.ValidateVersion(); err != nil {
			s.logger.Warn("protocol version mismatch",
				slog.String("client_id", c.id),
				slog.String("version", msg.Version),
				slog.String("error", err.Error()))
			c.sendMessage(reef.MsgError, msg.TaskID, reef.ErrorPayload{
				Code:    "ERR_VERSION",
				Message: err.Error(),
			})
			continue
		}

		if !msg.MsgType.IsValid() {
			s.logger.Warn("unknown message type",
				slog.String("client_id", c.id),
				slog.String("msg_type", string(msg.MsgType)))
			// Send an error response so the Client knows the message was rejected
			c.sendMessage(reef.MsgError, msg.TaskID, reef.ErrorPayload{
				Code:         "ERR_UNKNOWN_TYPE",
				Message:      "unknown message type: " + string(msg.MsgType),
				OriginalType: string(msg.MsgType),
			})
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
		if err := msg.DecodePayload(&payload); err != nil {
			s.logger.Warn("decode task_completed payload failed",
				slog.String("client_id", c.id),
				slog.String("task_id", msg.TaskID),
				slog.String("error", err.Error()))
			break
		}
		result := &reef.TaskResult{Metadata: payload.Result}
		// Extract text from metadata if present (client sends {"text": result})
		if text, ok := payload.Result["text"].(string); ok && text != "" {
			result.Text = text
		}
		if err := s.scheduler.HandleTaskCompleted(msg.TaskID, result); err != nil {
			s.logger.Warn("handle task completed failed",
				slog.String("client_id", c.id),
				slog.String("task_id", msg.TaskID),
				slog.String("error", err.Error()))
		}

	case reef.MsgTaskFailed:
		var payload reef.TaskFailedPayload
		if err := msg.DecodePayload(&payload); err != nil {
			s.logger.Warn("decode task_failed payload failed",
				slog.String("client_id", c.id),
				slog.String("task_id", msg.TaskID),
				slog.String("error", err.Error()))
			break
		}
		taskErr := &reef.TaskError{
			Type:    payload.ErrorType,
			Message: payload.ErrorMessage,
			Detail:  payload.ErrorDetail,
		}
		if err := s.scheduler.HandleTaskFailed(msg.TaskID, taskErr, payload.AttemptHistory); err != nil {
			s.logger.Warn("handle task failed failed",
				slog.String("client_id", c.id),
				slog.String("task_id", msg.TaskID),
				slog.String("error", err.Error()))
		}

	case reef.MsgControlAck:
		// Log control acknowledgments
		var payload reef.ControlAckPayload
		_ = msg.DecodePayload(&payload)
		s.logger.Info("control ack received",
			slog.String("client_id", c.id),
			slog.String("control_type", payload.ControlType),
			slog.String("task_id", payload.TaskID))

	case reef.MsgGeneSubmit:
		if s.evolutionHub == nil {
			s.logger.Warn("gene_submit received but EvolutionHub not initialized",
				slog.String("client_id", c.id))
			break
		}
		if err := s.evolutionHub.HandleGeneSubmission(context.Background(), msg, c.id); err != nil {
			s.logger.Error("handle gene submission",
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
			s.pendingControls[clientID] = append(s.pendingControls[clientID], msg)
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

// SetEvolutionHub sets the evolution hub for gene submission handling.
func (s *WebSocketServer) SetEvolutionHub(hub *evolutionsrv.EvolutionHub) {
	s.evolutionHub = hub
}

// GetEvolutionHub returns the current evolution hub, or nil if not set.
func (s *WebSocketServer) GetEvolutionHub() *evolutionsrv.EvolutionHub {
	return s.evolutionHub
}
