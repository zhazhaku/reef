package e2e

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhazhaku/reef/pkg/reef"
)

// MockClient simulates a Reef Client node over real WebSocket.
type MockClient struct {
	conn      *websocket.Conn
	msgCh     chan reef.Message
	clientID  string
	role      string
	skills    []string
	providers []string
	capacity  int
	token     string
	mu        sync.Mutex
	closed    bool
	t         *testing.T
}

// MockClientOptions configures a mock client.
type MockClientOptions struct {
	ClientID  string
	Role      string
	Skills    []string
	Providers []string
	Capacity  int
	Token     string
}

// NewMockClient creates a mock client (does not connect yet).
func NewMockClient(t *testing.T, opts MockClientOptions) *MockClient {
	if opts.ClientID == "" {
		opts.ClientID = "mock-" + opts.Role + "-" + time.Now().Format("150405")
	}
	if opts.Capacity <= 0 {
		opts.Capacity = 3
	}
	return &MockClient{
		msgCh:     make(chan reef.Message, 64),
		clientID:  opts.ClientID,
		role:      opts.Role,
		skills:    opts.Skills,
		providers: opts.Providers,
		capacity:  opts.Capacity,
		token:     opts.Token,
		t:         t,
	}
}

// Connect dials the WebSocket and sends register message.
func (c *MockClient) Connect(wsURL string) error {
	header := http.Header{}
	if c.token != "" {
		header.Set("x-reef-token", c.token)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		return err
	}
	c.conn = conn

	// Send register
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
		conn.Close()
		return err
	}
	if err := c.write(reg); err != nil {
		conn.Close()
		return err
	}

	// Start read loop
	go c.readLoop()
	return nil
}

// Close closes the WebSocket connection.
func (c *MockClient) Close() {
	c.mu.Lock()
	c.closed = true
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// Messages returns the channel of received messages.
func (c *MockClient) Messages() <-chan reef.Message { return c.msgCh }

// Send sends a message to the server.
func (c *MockClient) Send(msg reef.Message) error {
	return c.write(msg)
}

// SendHeartbeat sends a heartbeat message.
func (c *MockClient) SendHeartbeat() {
	msg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{
		Timestamp: time.Now().UnixMilli(),
	})
	_ = c.Send(msg)
}

// SendTaskCompleted reports task completion.
func (c *MockClient) SendTaskCompleted(taskID, result string) {
	msg, _ := reef.NewMessage(reef.MsgTaskCompleted, taskID, reef.TaskCompletedPayload{
		TaskID:          taskID,
		Result:          map[string]any{"text": result},
		ExecutionTimeMs: 100,
		Timestamp:       time.Now().UnixMilli(),
	})
	_ = c.Send(msg)
}

// SendTaskFailed reports task failure.
func (c *MockClient) SendTaskFailed(taskID, errorType, errorMsg string) {
	msg, _ := reef.NewMessage(reef.MsgTaskFailed, taskID, reef.TaskFailedPayload{
		TaskID:         taskID,
		ErrorType:      errorType,
		ErrorMessage:   errorMsg,
		AttemptHistory: []reef.AttemptRecord{},
		Timestamp:      time.Now().UnixMilli(),
	})
	_ = c.Send(msg)
}

// SendTaskProgress reports task progress.
func (c *MockClient) SendTaskProgress(taskID, status string, percent int, message string) {
	msg, _ := reef.NewMessage(reef.MsgTaskProgress, taskID, reef.TaskProgressPayload{
		TaskID:          taskID,
		Status:          status,
		ProgressPercent: percent,
		Message:         message,
		Timestamp:       time.Now().UnixMilli(),
	})
	_ = c.Send(msg)
}

// SendControlAck acknowledges a control message.
func (c *MockClient) SendControlAck(taskID, controlType string) {
	msg, _ := reef.NewMessage(reef.MsgControlAck, taskID, reef.ControlAckPayload{
		ControlType: controlType,
		TaskID:      taskID,
		Timestamp:   time.Now().UnixMilli(),
	})
	_ = c.Send(msg)
}

// WaitForMessage blocks until a message of the given type is received (or timeout).
func (c *MockClient) WaitForMessage(msgType reef.MessageType, timeout time.Duration) (reef.Message, bool) {
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-c.msgCh:
			if msg.MsgType == msgType {
				return msg, true
			}
		case <-deadline:
			return reef.Message{}, false
		}
	}
}

// WaitForTaskDispatch blocks until a task_dispatch is received and returns its payload.
func (c *MockClient) WaitForTaskDispatch(timeout time.Duration) (*reef.TaskDispatchPayload, bool) {
	msg, ok := c.WaitForMessage(reef.MsgTaskDispatch, timeout)
	if !ok {
		return nil, false
	}
	var payload reef.TaskDispatchPayload
	if err := msg.DecodePayload(&payload); err != nil {
		c.t.Logf("decode task_dispatch failed: %v", err)
		return nil, false
	}
	return &payload, true
}

// readLoop continuously reads messages from the WebSocket.
func (c *MockClient) readLoop() {
	for {
		c.mu.Lock()
		closed := c.closed
		conn := c.conn
		c.mu.Unlock()
		if closed || conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			// Connection closed
			return
		}

		var msg reef.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			c.t.Logf("mock client unmarshal error: %v", err)
			continue
		}

		select {
		case c.msgCh <- msg:
		default:
			c.t.Logf("mock client message buffer full, dropping %s", msg.MsgType)
		}
	}
}

func (c *MockClient) write(msg reef.Message) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return nil // silently drop if not connected
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}
