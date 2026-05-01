package client

import (
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhazhaku/reef/pkg/reef"
)

func TestBackoff_NextReset(t *testing.T) {
	b := NewBackoff(1*time.Second, 60*time.Second)

	if b.current != 1*time.Second {
		t.Errorf("initial = %v, want 1s", b.current)
	}

	b.Next()
	if b.current != 2*time.Second {
		t.Errorf("after 1st next = %v, want 2s", b.current)
	}
	b.Next()
	if b.current != 4*time.Second {
		t.Errorf("after 2nd next = %v, want 4s", b.current)
	}
	b.Next()
	b.Next()
	b.Next() // 32s
	b.Next() // should cap at 60s
	if b.current != 60*time.Second {
		t.Errorf("after cap = %v, want 60s", b.current)
	}

	b.Reset()
	if b.current != 1*time.Second {
		t.Errorf("after reset = %v, want 1s", b.current)
	}
	if b.Attempt() != 0 {
		t.Errorf("attempt = %d, want 0", b.Attempt())
	}
}

func TestBackoff_Wait(t *testing.T) {
	b := NewBackoff(100*time.Millisecond, 60*time.Second)
	start := time.Now()
	select {
	case <-b.Wait():
		elapsed := time.Since(start)
		if elapsed < 50*time.Millisecond || elapsed > 200*time.Millisecond {
			t.Errorf("waited %v, expected ~100ms", elapsed)
		}
	}
}

func TestConnectorOptions_Defaults(t *testing.T) {
	c := NewConnector(ConnectorOptions{})
	if c.capacity != 1 {
		t.Errorf("capacity = %d, want 1", c.capacity)
	}
	if c.heartbeatInterval != 10*time.Second {
		t.Errorf("heartbeat = %v, want 10s", c.heartbeatInterval)
	}
}

func TestConnector_SendClosed(t *testing.T) {
	c := NewConnector(ConnectorOptions{})
	_ = c.Close()

	msg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{})
	if err := c.Send(msg); err == nil {
		t.Error("expected error when sending to closed connector")
	}
}

// ---------------------------------------------------------------------------
// Reconnect callback tests
// ---------------------------------------------------------------------------

func TestConnector_WSConn_NilWhenDisconnected(t *testing.T) {
	c := NewConnector(ConnectorOptions{})
	conn := c.WSConn()
	if conn != nil {
		t.Error("WSConn should return nil when not connected")
	}
}

func TestConnector_OnReconnect_RegistersCallback(t *testing.T) {
	c := NewConnector(ConnectorOptions{})

	called := false
	c.OnReconnect(func(conn *websocket.Conn) {
		called = true
	})

	// Fire callbacks directly (testing the mechanism)
	c.fireReconnectCallbacks(nil)

	// Give goroutine time to execute
	time.Sleep(50 * time.Millisecond)

	if !called {
		t.Error("reconnect callback was not called")
	}
}

func TestConnector_OnReconnect_MultipleCallbacks(t *testing.T) {
	c := NewConnector(ConnectorOptions{})

	count := 0
	var mu sync.Mutex
	cb := func(conn *websocket.Conn) {
		mu.Lock()
		count++
		mu.Unlock()
	}

	c.OnReconnect(cb)
	c.OnReconnect(cb)
	c.OnReconnect(cb)

	c.fireReconnectCallbacks(nil)
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if count != 3 {
		t.Errorf("expected 3 callbacks, got %d", count)
	}
	mu.Unlock()
}

func TestConnector_OnReconnect_PanicsDontCrash(t *testing.T) {
	c := NewConnector(ConnectorOptions{})

	normalCalled := false
	c.OnReconnect(func(conn *websocket.Conn) {
		panic("intentional panic in callback")
	})
	c.OnReconnect(func(conn *websocket.Conn) {
		normalCalled = true
	})

	// Should not panic
	c.fireReconnectCallbacks(nil)
	time.Sleep(100 * time.Millisecond)

	if !normalCalled {
		t.Error("second callback should still be called after panic in first")
	}
}

func TestConnector_OnReconnect_ReceivesConn(t *testing.T) {
	c := NewConnector(ConnectorOptions{})

	var receivedConn *websocket.Conn
	c.OnReconnect(func(conn *websocket.Conn) {
		receivedConn = conn
	})

	// Use a minimal websocket.Conn — just verify the callback receives it
	dummyConn := &websocket.Conn{}
	c.fireReconnectCallbacks(dummyConn)
	time.Sleep(50 * time.Millisecond)

	if receivedConn != dummyConn {
		t.Error("callback should receive the same conn that was passed")
	}
}

// ---------------------------------------------------------------------------
// Gene broadcast tests (Task 4)
// ---------------------------------------------------------------------------

func TestConnectorGeneBroadcast_CallbackCalled(t *testing.T) {
	c := NewConnector(ConnectorOptions{})

	var receivedPayload reef.GeneBroadcastPayload
	done := make(chan struct{})
	c.SetOnGeneBroadcast(func(p reef.GeneBroadcastPayload) {
		receivedPayload = p
		close(done)
	})

	// Build a valid gene_broadcast message.
	msg, err := reef.NewMessage(reef.MsgGeneBroadcast, "", reef.GeneBroadcastPayload{
		GeneID:         "gene-001",
		GeneData:       []byte(`{"id":"gene-001","strategy_name":"test"}`),
		SourceClientID: "client-1",
		ApprovedAt:     1714500000000,
		BroadcastBy:    "server",
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}

	c.handleGeneBroadcast(msg)

	select {
	case <-done:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("callback was not called within timeout")
	}

	if receivedPayload.GeneID != "gene-001" {
		t.Errorf("gene ID = %q, want gene-001", receivedPayload.GeneID)
	}
	if receivedPayload.SourceClientID != "client-1" {
		t.Errorf("source client = %q, want client-1", receivedPayload.SourceClientID)
	}
	if receivedPayload.BroadcastBy != "server" {
		t.Errorf("broadcast by = %q, want server", receivedPayload.BroadcastBy)
	}
}

func TestConnectorGeneBroadcast_InvalidJSON_ErrorLogged(t *testing.T) {
	c := NewConnector(ConnectorOptions{})

	var callbackCalled bool
	c.SetOnGeneBroadcast(func(p reef.GeneBroadcastPayload) {
		callbackCalled = true
	})

	// Build a message with invalid payload (not a valid GeneBroadcastPayload).
	msg := reef.Message{
		MsgType:   reef.MsgGeneBroadcast,
		Timestamp: 1714500000000,
		Payload:   []byte(`{"gene_id": 123, "gene_data": "not-json"}`), // gene_id should be string
	}

	c.handleGeneBroadcast(msg)

	// The callback should NOT be called because decode fails.
	time.Sleep(50 * time.Millisecond)
	if callbackCalled {
		t.Error("callback should not be called for invalid payload")
	}
}

func TestConnectorGeneBroadcast_CallbackNotSet_NoCrash(t *testing.T) {
	c := NewConnector(ConnectorOptions{})

	msg, err := reef.NewMessage(reef.MsgGeneBroadcast, "", reef.GeneBroadcastPayload{
		GeneID:         "gene-001",
		GeneData:       []byte(`{"id":"gene-001","strategy_name":"test"}`),
		SourceClientID: "client-1",
		ApprovedAt:     1714500000000,
		BroadcastBy:    "server",
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}

	// Should not panic when callback is nil.
	c.handleGeneBroadcast(msg)
}

func TestConnectorGeneBroadcast_DuplicateGene_NoCrash(t *testing.T) {
	c := NewConnector(ConnectorOptions{})

	callCount := 0
	done := make(chan struct{})
	c.SetOnGeneBroadcast(func(p reef.GeneBroadcastPayload) {
		callCount++
		if callCount == 2 {
			close(done)
		}
	})

	msg, err := reef.NewMessage(reef.MsgGeneBroadcast, "", reef.GeneBroadcastPayload{
		GeneID:         "gene-001",
		GeneData:       []byte(`{"id":"gene-001","strategy_name":"test"}`),
		SourceClientID: "client-1",
		ApprovedAt:     1714500000000,
		BroadcastBy:    "server",
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}

	// Simulate duplicate broadcast.
	c.handleGeneBroadcast(msg)
	c.handleGeneBroadcast(msg)

	select {
	case <-done:
		if callCount != 2 {
			t.Errorf("expected 2 callbacks, got %d", callCount)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("second callback was not called within timeout")
	}
}

func TestConnectorGeneBroadcast_SetOnGeneBroadcast(t *testing.T) {
	c := NewConnector(ConnectorOptions{})
	if c.onGeneBroadcast != nil {
		t.Error("onGeneBroadcast should be nil initially")
	}

	c.SetOnGeneBroadcast(func(p reef.GeneBroadcastPayload) {})
	if c.onGeneBroadcast == nil {
		t.Error("onGeneBroadcast should be set after SetOnGeneBroadcast")
	}
}
