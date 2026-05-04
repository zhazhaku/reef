package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhazhaku/reef/pkg/reef"
)

// ---------------------------------------------------------------------------
// WebSocket test server helpers
// ---------------------------------------------------------------------------

// wsTestServer creates an httptest server that upgrades to WebSocket and calls
// handler for each accepted connection (in a new goroutine).
func wsTestServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	var upgrader = websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("ws upgrade error: %v", err)
			return
		}
		handler(ws)
	}))
}

// wsURL converts an httptest server URL from http:// to ws://.
func wsURL(srv *httptest.Server) string {
	return "ws" + srv.URL[4:]
}

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

func TestConnector_Messages_ReturnsChannel(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://localhost:0"})
	ch := c.Messages()
	if ch == nil {
		t.Fatal("Messages() returned nil channel")
	}
}

// ---------------------------------------------------------------------------
// Connect — success path
// ---------------------------------------------------------------------------

func TestConnector_Connect_Success(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		// Read register message
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var msg reef.Message
		json.Unmarshal(data, &msg)

		// Send register ack
		ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
			ClientID:  "test-client",
			ServerTime: time.Now().UnixMilli(),
		})
		ackBytes, _ := json.Marshal(ack)
		ws.WriteMessage(websocket.TextMessage, ackBytes)

		// Keep connection alive briefly
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
		Capacity:  2,
		Skills:    []string{"go"},
		Providers: []string{"openai"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	// Verify Messages() returns a working channel
	msgCh := c.Messages()
	if msgCh == nil {
		t.Fatal("Messages() returned nil after Connect")
	}

	// Clean up
	c.Close()
}

// ---------------------------------------------------------------------------
// Connect — dial fails
// ---------------------------------------------------------------------------

func TestConnector_Connect_DialFailed(t *testing.T) {
	c := NewConnector(ConnectorOptions{
		ServerURL: "ws://127.0.0.1:1", // invalid port, should fail
		ClientID:  "test-client",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := c.Connect(ctx)
	if err == nil {
		t.Error("Connect() expected error for invalid URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// Connect — server sends NACK (invalid register)
// ---------------------------------------------------------------------------

func TestConnector_dialAndRegister_ServerSendsNothing(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		// Just read and close — don't send register ack
		_, _, _ = ws.ReadMessage()
		ws.Close()
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// dialAndRegister should succeed (it just sends register, doesn't wait for response)
	err := c.dialAndRegister(ctx)
	if err != nil {
		t.Fatalf("dialAndRegister() failed: %v", err)
	}

	if c.conn == nil {
		t.Error("conn should not be nil after dialAndRegister")
	}

	c.Close()
}

// ---------------------------------------------------------------------------
// dialAndRegister — with token header
// ---------------------------------------------------------------------------

func TestConnector_dialAndRegister_WithToken(t *testing.T) {
	var receivedToken string
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		// Read register
		_, data, _ := ws.ReadMessage()
		var msg reef.Message
		json.Unmarshal(data, &msg)
	})
	defer srv.Close()

	_ = receivedToken // used for future assertions

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
		Token:     "secret-token",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.dialAndRegister(ctx)
	if err != nil {
		t.Fatalf("dialAndRegister() with token failed: %v", err)
	}

	c.Close()
}

// ---------------------------------------------------------------------------
// dialAndRegister — without token
// ---------------------------------------------------------------------------

func TestConnector_dialAndRegister_WithoutToken(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		_, _, _ = ws.ReadMessage()
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
		// Token: "" — empty
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.dialAndRegister(ctx)
	if err != nil {
		t.Fatalf("dialAndRegister() without token failed: %v", err)
	}

	c.Close()
}

// ---------------------------------------------------------------------------
// readLoop — receives messages from server
// ---------------------------------------------------------------------------

func TestConnector_readLoop_ReceivesMessage(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		// Read register
		_, data, _ := ws.ReadMessage()
		var msg reef.Message
		json.Unmarshal(data, &msg)

		// Send register ack
		ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
			ClientID:  "test-client",
			ServerTime: time.Now().UnixMilli(),
		})
		ackBytes, _ := json.Marshal(ack)
		ws.WriteMessage(websocket.TextMessage, ackBytes)

		// Send a heartbeat message
		hb, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{
			Timestamp: time.Now().UnixMilli(),
		})
		hbBytes, _ := json.Marshal(hb)
		ws.WriteMessage(websocket.TextMessage, hbBytes)

		// Keep connection alive
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer c.Close()

	// First message is register_ack
	select {
	case msg := <-c.Messages():
		if msg.MsgType != reef.MsgRegisterAck {
			t.Logf("first message type: %s (expected register_ack)", msg.MsgType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first message from readLoop")
	}

	// Second message is the heartbeat
	select {
	case msg := <-c.Messages():
		if msg.MsgType != reef.MsgHeartbeat {
			t.Errorf("second message type: %s, expected heartbeat", msg.MsgType)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for heartbeat message")
	}
}

// ---------------------------------------------------------------------------
// readLoop — task_available callback
// ---------------------------------------------------------------------------

func TestConnector_readLoop_TaskAvailableCallback(t *testing.T) {
	var mu sync.Mutex
	var received reef.TaskAvailablePayload
	done := make(chan struct{})

	srv := wsTestServer(t, func(ws *websocket.Conn) {
		// Read register
		_, _, _ = ws.ReadMessage()

		// Send register ack
		ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
			ClientID:  "test-client",
			ServerTime: time.Now().UnixMilli(),
		})
		ackBytes, _ := json.Marshal(ack)
		ws.WriteMessage(websocket.TextMessage, ackBytes)

		// Send task_available
		ta, _ := reef.NewMessage(reef.MsgTaskAvailable, "task-1", reef.TaskAvailablePayload{
			TaskID:       "task-1",
			RequiredRole: "builder",
			Priority:     3,
			Instruction:  "build it",
			ExpiresAt:    time.Now().Add(30 * time.Second).UnixMilli(),
		})
		taBytes, _ := json.Marshal(ta)
		ws.WriteMessage(websocket.TextMessage, taBytes)

		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})
	c.SetOnTaskAvailable(func(p reef.TaskAvailablePayload) {
		mu.Lock()
		received = p
		mu.Unlock()
		close(done)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer c.Close()

	select {
	case <-done:
		mu.Lock()
		if received.TaskID != "task-1" {
			t.Errorf("TaskID = %s, want task-1", received.TaskID)
		}
		if received.Instruction != "build it" {
			t.Errorf("Instruction = %s, want 'build it'", received.Instruction)
		}
		mu.Unlock()
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for task_available callback")
	}
}

// ---------------------------------------------------------------------------
// readLoop — task_claimed callback
// ---------------------------------------------------------------------------

func TestConnector_readLoop_TaskClaimedCallback(t *testing.T) {
	var mu sync.Mutex
	var received reef.TaskClaimedPayload
	done := make(chan struct{})

	srv := wsTestServer(t, func(ws *websocket.Conn) {
		_, _, _ = ws.ReadMessage()

		// register ack
		ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
			ClientID:  "test-client",
			ServerTime: time.Now().UnixMilli(),
		})
		ackBytes, _ := json.Marshal(ack)
		ws.WriteMessage(websocket.TextMessage, ackBytes)

		// Send task_claimed
		tc, _ := reef.NewMessage(reef.MsgTaskClaimed, "task-2", reef.TaskClaimedPayload{
			TaskID:    "task-2",
			ClaimedBy: "other-client",
			ClaimedAt: time.Now().UnixMilli(),
		})
		tcBytes, _ := json.Marshal(tc)
		ws.WriteMessage(websocket.TextMessage, tcBytes)

		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})
	c.SetOnTaskClaimed(func(p reef.TaskClaimedPayload) {
		mu.Lock()
		received = p
		mu.Unlock()
		close(done)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer c.Close()

	select {
	case <-done:
		mu.Lock()
		if received.TaskID != "task-2" {
			t.Errorf("TaskID = %s, want task-2", received.TaskID)
		}
		if received.ClaimedBy != "other-client" {
			t.Errorf("ClaimedBy = %s, want other-client", received.ClaimedBy)
		}
		mu.Unlock()
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for task_claimed callback")
	}
}

// ---------------------------------------------------------------------------
// readLoop — handles close from server (triggers reconnect)
// ---------------------------------------------------------------------------

func TestConnector_readLoop_ServerClosesTriggersReconnect(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		_, _, _ = ws.ReadMessage()
		// Send ack
		ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
			ClientID:  "test-client",
			ServerTime: time.Now().UnixMilli(),
		})
		ackBytes, _ := json.Marshal(ack)
		ws.WriteMessage(websocket.TextMessage, ackBytes)

		// Close immediately — triggers unexpected close in readLoop
		ws.Close()
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	// Wait for readLoop to detect close and trigger reconnect
	time.Sleep(300 * time.Millisecond)

	// Conn should be nil after triggerReconnect
	c.mu.Lock()
	isNil := c.conn == nil
	c.mu.Unlock()
	if !isNil {
		t.Error("expected conn to be nil after server close triggers reconnect")
	}

	c.Close()
}

// ---------------------------------------------------------------------------
// readLoop — invalid JSON is skipped
// ---------------------------------------------------------------------------

func TestConnector_readLoop_InvalidJSON(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		_, _, _ = ws.ReadMessage()

		// Send ack
		ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
			ClientID:  "test-client",
			ServerTime: time.Now().UnixMilli(),
		})
		ackBytes, _ := json.Marshal(ack)
		ws.WriteMessage(websocket.TextMessage, ackBytes)

		// Send invalid JSON
		ws.WriteMessage(websocket.TextMessage, []byte("not-json"))

		// Then send a valid message
		hb, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{
			Timestamp: time.Now().UnixMilli(),
		})
		hbBytes, _ := json.Marshal(hb)
		ws.WriteMessage(websocket.TextMessage, hbBytes)

		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer c.Close()

	// Should eventually receive the heartbeat (invalid JSON is skipped)
	foundHb := false
	timeout := time.After(2 * time.Second)
	for !foundHb {
		select {
		case msg := <-c.Messages():
			if msg.MsgType == reef.MsgHeartbeat {
				foundHb = true
			}
		case <-timeout:
			if !foundHb {
				t.Error("did not receive heartbeat after invalid JSON — readLoop may have stalled")
			}
			return
		}
	}
}

// ---------------------------------------------------------------------------
// readLoop — unknown message type is skipped
// ---------------------------------------------------------------------------

func TestConnector_readLoop_UnknownMessageType(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		_, _, _ = ws.ReadMessage()

		ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
			ClientID:  "test-client",
			ServerTime: time.Now().UnixMilli(),
		})
		ackBytes, _ := json.Marshal(ack)
		ws.WriteMessage(websocket.TextMessage, ackBytes)

		// Send unknown type
		unknown := reef.Message{
			MsgType:   "bogus_type_xyz",
			Timestamp: time.Now().UnixMilli(),
			Payload:   json.RawMessage(`{}`),
		}
		unknownBytes, _ := json.Marshal(unknown)
		ws.WriteMessage(websocket.TextMessage, unknownBytes)

		// Then valid
		hb, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{
			Timestamp: time.Now().UnixMilli(),
		})
		hbBytes, _ := json.Marshal(hb)
		ws.WriteMessage(websocket.TextMessage, hbBytes)

		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer c.Close()

	// Drain register_ack first
	select {
	case <-c.Messages():
		// consumed register_ack
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for register_ack")
	}

	// The unknown type should be skipped, then we get the heartbeat
	select {
	case msg := <-c.Messages():
		if msg.MsgType != reef.MsgHeartbeat {
			t.Errorf("expected heartbeat, got %s", msg.MsgType)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for heartbeat — unknown type may have broken readLoop")
	}
}

// ---------------------------------------------------------------------------
// writeLoop — sends message to server
// ---------------------------------------------------------------------------

func TestConnector_writeLoop_SendsMessage(t *testing.T) {
	received := make(chan reef.Message, 1)

	srv := wsTestServer(t, func(ws *websocket.Conn) {
		// Read register
		_, data, _ := ws.ReadMessage()
		var regMsg reef.Message
		json.Unmarshal(data, &regMsg)

		// Send ack
		ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
			ClientID:  "test-client",
			ServerTime: time.Now().UnixMilli(),
		})
		ackBytes, _ := json.Marshal(ack)
		ws.WriteMessage(websocket.TextMessage, ackBytes)

		// Wait for the test message
		_, data, err := ws.ReadMessage()
		if err == nil {
			var msg reef.Message
			json.Unmarshal(data, &msg)
			received <- msg
		}

		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer c.Close()

	// Send a message
	msg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{
		Timestamp: time.Now().UnixMilli(),
	})
	if err := c.Send(msg); err != nil {
		t.Fatalf("Send() failed: %v", err)
	}

	select {
	case recv := <-received:
		if recv.MsgType != reef.MsgHeartbeat {
			t.Errorf("received MsgType = %s, want %s", recv.MsgType, reef.MsgHeartbeat)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for server to receive message")
	}
}

// ---------------------------------------------------------------------------
// writeLoop — drops message when no connection
// ---------------------------------------------------------------------------

func TestConnector_writeLoop_DropsMessageWhenNoConn(t *testing.T) {
	c := NewConnector(ConnectorOptions{
		ServerURL: "ws://localhost:0",
		ClientID:  "test-client",
	})

	// Start writeLoop without a real connection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.writeLoop(ctx)

	// Send should succeed (goes to sendCh)
	msg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{})
	if err := c.Send(msg); err != nil {
		t.Fatalf("Send() failed: %v", err)
	}

	// Give writeLoop time to pick up message and drop it (no conn)
	time.Sleep(100 * time.Millisecond)

	// Should not panic — message just gets dropped with a warning
	c.Close()
}

// ---------------------------------------------------------------------------
// writeMessage — direct call
// ---------------------------------------------------------------------------

func TestConnector_writeMessage_Success(t *testing.T) {
	readCh := make(chan []byte, 1)

	srv := wsTestServer(t, func(ws *websocket.Conn) {
		_, data, err := ws.ReadMessage()
		if err == nil {
			readCh <- data
		}
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	c := NewConnector(ConnectorOptions{ServerURL: wsURL(srv)})
	msg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{
		Timestamp: time.Now().UnixMilli(),
	})

	err = c.writeMessage(ws, msg)
	if err != nil {
		t.Fatalf("writeMessage() failed: %v", err)
	}

	select {
	case data := <-readCh:
		var recv reef.Message
		if err := json.Unmarshal(data, &recv); err != nil {
			t.Fatalf("unmarshal received message: %v", err)
		}
		if recv.MsgType != reef.MsgHeartbeat {
			t.Errorf("MsgType = %s, want %s", recv.MsgType, reef.MsgHeartbeat)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for message on server")
	}
}

// ---------------------------------------------------------------------------
// heartbeatLoop — sends heartbeat at interval
// ---------------------------------------------------------------------------

func TestConnector_heartbeatLoop_SendsHeartbeat(t *testing.T) {
	var mu sync.Mutex
	var heartbeatCount int
	done := make(chan struct{})

	srv := wsTestServer(t, func(ws *websocket.Conn) {
		// Read register
		_, _, _ = ws.ReadMessage()

		// Read heartbeats
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var msg reef.Message
			json.Unmarshal(data, &msg)
			if msg.MsgType == reef.MsgHeartbeat {
				mu.Lock()
				heartbeatCount++
				count := heartbeatCount
				mu.Unlock()
				if count >= 2 {
					close(done)
					return
				}
			}
		}
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL:         wsURL(srv),
		ClientID:          "test-client",
		Role:              "builder",
		HeartbeatInterval: 100 * time.Millisecond, // fast heartbeat for testing
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer c.Close()

	select {
	case <-done:
		mu.Lock()
		if heartbeatCount < 2 {
			t.Errorf("expected at least 2 heartbeats, got %d", heartbeatCount)
		}
		mu.Unlock()
	case <-time.After(3 * time.Second):
		mu.Lock()
		t.Errorf("timed out waiting for heartbeats, got %d", heartbeatCount)
		mu.Unlock()
	}
}

// ---------------------------------------------------------------------------
// reconnectLoop — triggers reconnection after disconnect
// ---------------------------------------------------------------------------

func TestConnector_reconnectLoop_ReconnectsAfterDisconnect(t *testing.T) {
	// Track connections
	var mu sync.Mutex
	var connectCount int
	serverReady := make(chan struct{}, 1)

	// Server that counts connections
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		mu.Lock()
		connectCount++
		count := connectCount
		mu.Unlock()

		// Read register
		_, _, _ = ws.ReadMessage()

		// Send ack
		ack, _ := reef.NewMessage(reef.MsgRegisterAck, "", reef.RegisterAckPayload{
			ClientID:  "test-client",
			ServerTime: time.Now().UnixMilli(),
		})
		ackBytes, _ := json.Marshal(ack)
		ws.WriteMessage(websocket.TextMessage, ackBytes)

		if count == 2 {
			// Second connection succeeded
			close(serverReady)
		}

		// Keep alive
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL:         wsURL(srv),
		ClientID:          "test-client",
		Role:              "builder",
		HeartbeatInterval: 5 * time.Second, // slow heartbeat so it doesn't interfere
	})
	// Fast reconnect backoff
	c.backoff = NewBackoff(50*time.Millisecond, 200*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	// Wait for initial connection
	time.Sleep(200 * time.Millisecond)

	// Simulate disconnect: set conn to nil and trigger
	c.triggerReconnect()

	// Wait for reconnect
	select {
	case <-serverReady:
		mu.Lock()
		if connectCount < 2 {
			t.Errorf("expected at least 2 connections, got %d", connectCount)
		}
		mu.Unlock()
	case <-time.After(5 * time.Second):
		mu.Lock()
		t.Errorf("timed out waiting for reconnect, got %d connections", connectCount)
		mu.Unlock()
	}

	c.Close()
}

// ---------------------------------------------------------------------------
// reconnectLoop — stops when context cancelled
// ---------------------------------------------------------------------------

func TestConnector_reconnectLoop_StopsOnCancel(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		_, _, _ = ws.ReadMessage()
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})

	ctx, cancel := context.WithCancel(context.Background())

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	// Cancel context — all loops should stop
	cancel()

	// Give goroutines time to exit
	time.Sleep(300 * time.Millisecond)

	// Should not panic; loops should have exited cleanly
	c.Close()
}

// ---------------------------------------------------------------------------
// reconnectLoop — doesn't reconnect when closed
// ---------------------------------------------------------------------------

func TestConnector_reconnectLoop_DoesNotReconnectWhenClosed(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		_, _, _ = ws.ReadMessage()
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL: wsURL(srv),
		ClientID:  "test-client",
		Role:      "builder",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Close the connector — reconnect should not fire
	c.Close()

	// Trigger reconnect — should be no-op since closed=true
	c.triggerReconnect()

	// Give time for potential reconnect attempt
	time.Sleep(300 * time.Millisecond)

	// Conn should still be nil (no reconnect)
	c.mu.Lock()
	if c.conn != nil {
		t.Error("conn should be nil after Close + triggerReconnect")
	}
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// triggerReconnect — sets conn to nil
// ---------------------------------------------------------------------------

func TestConnector_triggerReconnect_SetsConnToNil(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		time.Sleep(500 * time.Millisecond)
	})
	defer srv.Close()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	c := NewConnector(ConnectorOptions{ServerURL: wsURL(srv)})
	c.conn = ws

	c.triggerReconnect()

	c.mu.Lock()
	if c.conn != nil {
		t.Error("conn should be nil after triggerReconnect")
	}
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// triggerReconnect — nil conn is no-op for Close
// ---------------------------------------------------------------------------

func TestConnector_triggerReconnect_NilConn(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://localhost:0"})

	// triggerReconnect with nil conn should not panic
	c.triggerReconnect()

	c.mu.Lock()
	if c.conn != nil {
		t.Error("conn should still be nil")
	}
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Close — with active connection
// ---------------------------------------------------------------------------

func TestConnector_CloseWithConn(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		// Hold connection open
		_, _, err := ws.ReadMessage()
		if err != nil {
			return
		}
	})
	defer srv.Close()

	ws, _, err := websocket.DefaultDialer.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	c := NewConnector(ConnectorOptions{ServerURL: wsURL(srv)})
	c.conn = ws

	err = c.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Send — buffer full
// ---------------------------------------------------------------------------

func TestConnector_SendBufferFull(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://localhost:0"})

	// Fill the 64-message buffer (without a consumer)
	for i := 0; i < 64; i++ {
		msg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{})
		if err := c.Send(msg); err != nil {
			t.Fatalf("unexpected error on msg %d: %v", i, err)
		}
	}

	// 65th should fail
	msg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{})
	if err := c.Send(msg); err == nil {
		t.Error("expected 'send buffer full' error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Backoff — Wait returns a channel
// ---------------------------------------------------------------------------

func TestBackoff_WaitChannel(t *testing.T) {
	b := NewBackoff(50*time.Millisecond, 60*time.Second)

	// Wait() should return a channel that fires after current duration
	start := time.Now()
	<-b.Wait()
	elapsed := time.Since(start)
	if elapsed < 25*time.Millisecond {
		t.Errorf("Wait() fired too quickly: %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// Backoff — Next triggers Wait channel immediately
// ---------------------------------------------------------------------------

func TestBackoff_NextTriggersTrigger(t *testing.T) {
	b := NewBackoff(1*time.Second, 60*time.Second)

	// Next sends to trigger channel
	b.Next()

	// trigger channel should have one item
	select {
	case <-b.trigger:
		// OK
	default:
		t.Error("expected trigger channel to have item after Next()")
	}
}

// ---------------------------------------------------------------------------
// Backoff — multiple Next calls don't overflow trigger
// ---------------------------------------------------------------------------

func TestBackoff_MultipleNextDontOverflow(t *testing.T) {
	b := NewBackoff(1*time.Second, 60*time.Second)

	b.Next()
	b.Next()
	b.Next()

	// Only one item in trigger channel (buffered size 1)
	count := 0
loop:
	for {
		select {
		case <-b.trigger:
			count++
		default:
			break loop
		}
	}
	if count != 1 {
		t.Errorf("trigger channel had %d items, want 1", count)
	}
}

// ---------------------------------------------------------------------------
// Connector — context cancellation stops loops
// ---------------------------------------------------------------------------

func TestConnector_Connect_ContextCancelled(t *testing.T) {
	srv := wsTestServer(t, func(ws *websocket.Conn) {
		_, _, _ = ws.ReadMessage()
		time.Sleep(5 * time.Second) // hold forever
	})
	defer srv.Close()

	c := NewConnector(ConnectorOptions{
		ServerURL:         wsURL(srv),
		ClientID:          "test-client",
		Role:              "builder",
		HeartbeatInterval: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}

	// Let loops start
	time.Sleep(200 * time.Millisecond)

	// Cancel context — all loops should exit
	cancel()

	// Give goroutines time to exit
	time.Sleep(300 * time.Millisecond)

	// Send after cancel should still work (buffer not closed yet)
	// but Close should clean up cleanly
	err := c.Close()
	if err != nil {
		t.Errorf("Close() after cancel returned error: %v", err)
	}
}
