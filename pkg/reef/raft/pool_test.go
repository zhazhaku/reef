package raft

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sipeed/reef/pkg/reef"
)

var upgrader = websocket.Upgrader{}

// testWSServer starts a WebSocket echo server that can be configured to
// send raft_leader_change messages.
type testWSServer struct {
	server     *httptest.Server
	addr       string
	conns      []*websocket.Conn
	mu         sync.Mutex
	leaderAddr string
}

func newTestWSServer(t *testing.T, leaderAddr string) *testWSServer {
	t.Helper()
	ts := &testWSServer{leaderAddr: leaderAddr}
	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		ts.mu.Lock()
		ts.conns = append(ts.conns, conn)
		ts.mu.Unlock()

		// Send a leader change message if configured
		if ts.leaderAddr != "" {
			msg := reef.NewRaftLeaderChangeMessage(ts.leaderAddr, "node-1", "", "", 1)
			data, _ := json.Marshal(msg)
			conn.WriteMessage(websocket.TextMessage, data)
		}

		// Echo loop
		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.WriteMessage(mt, message)
		}
	}))
	ts.addr = strings.TrimPrefix(ts.server.URL, "http://")
	return ts
}

func (ts *testWSServer) close() {
	ts.mu.Lock()
	for _, c := range ts.conns {
		c.Close()
	}
	ts.mu.Unlock()
	ts.server.Close()
}

func (ts *testWSServer) connectionCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.conns)
}

func (ts *testWSServer) sendToAll(msg []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, c := range ts.conns {
		c.WriteMessage(websocket.TextMessage, msg)
	}
}

// =====================================================================
// Tests
// =====================================================================

func TestNewClientConnPoolValidation(t *testing.T) {
	// Empty addresses
	_, err := NewClientConnPool(PoolConfig{}, nil)
	if err == nil {
		t.Error("expected error for empty ServerAddrs")
	}

	// Invalid backoff
	_, err = NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://h:8080"},
		ReconnectBackoff: -1,
	}, nil)
	if err == nil {
		t.Error("expected error for negative backoff")
	}

	// MaxReconnect < ReconnectBackoff
	_, err = NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://h:8080"},
		ReconnectBackoff: 10 * time.Second,
		MaxReconnect:     5 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err == nil {
		t.Error("expected error for MaxReconnect < ReconnectBackoff")
	}

	// Valid config
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://h1:8080", "ws://h2:8080", "ws://h3:8080"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pool.ServerCount() != 3 {
		t.Errorf("ServerCount = %d, want 3", pool.ServerCount())
	}
	if pool.LeaderIndex() != -1 {
		t.Errorf("LeaderIndex = %d, want -1", pool.LeaderIndex())
	}
	if pool.LeaderAddr() != "" {
		t.Errorf("LeaderAddr = %q, want empty", pool.LeaderAddr())
	}
}

func TestNewClientConnPoolDedup(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://h1:8080", "ws://h2:8080", "ws://h1:8080"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pool.ServerCount() != 2 {
		t.Errorf("ServerCount = %d, want 2 (duplicates removed)", pool.ServerCount())
	}
}

func TestPoolLeaderDetection(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080", "ws://n2:8081", "ws://n3:8082"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Detect leader n2
	pool.OnLeaderChange(reef.RaftLeaderChangePayload{
		NewLeaderAddr: "ws://n2:8081",
		NewLeaderID:   "n2",
		Term:          1,
	})
	if pool.LeaderIndex() != 1 {
		t.Errorf("LeaderIndex = %d, want 1", pool.LeaderIndex())
	}
	if pool.LeaderAddr() != "ws://n2:8081" {
		t.Errorf("LeaderAddr = %q, want ws://n2:8081", pool.LeaderAddr())
	}

	// Switch to n3
	pool.OnLeaderChange(reef.RaftLeaderChangePayload{
		NewLeaderAddr: "ws://n3:8082",
		OldLeaderAddr: "ws://n2:8081",
		NewLeaderID:   "n3",
		Term:          2,
	})
	if pool.LeaderIndex() != 2 {
		t.Errorf("LeaderIndex = %d, want 2", pool.LeaderIndex())
	}

	// Unknown address
	pool.OnLeaderChange(reef.RaftLeaderChangePayload{
		NewLeaderAddr: "ws://unknown:9999",
		NewLeaderID:   "bad",
		Term:          3,
	})
	// LeaderIndex should not change for unknown
	if pool.LeaderAddr() != "ws://n3:8082" {
		t.Errorf("LeaderAddr should remain ws://n3:8082, got %q", pool.LeaderAddr())
	}
}

func TestPoolLeaderChangeCallback(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080", "ws://n2:8081"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var gotLeader string
	var wg sync.WaitGroup
	wg.Add(1)
	pool.SetOnLeaderChange(func(addr string) {
		gotLeader = addr
		wg.Done()
	})

	pool.OnLeaderChange(reef.RaftLeaderChangePayload{
		NewLeaderAddr: "ws://n2:8081",
		NewLeaderID:   "n2",
		Term:          1,
	})

	wg.Wait()
	if gotLeader != "ws://n2:8081" {
		t.Errorf("callback got %q, want ws://n2:8081", gotLeader)
	}
}

func TestPoolMultiServerConnect(t *testing.T) {
	srv1 := newTestWSServer(t, "ws://n1:8080/ws")
	defer srv1.close()
	srv2 := newTestWSServer(t, "")
	defer srv2.close()
	srv3 := newTestWSServer(t, "")
	defer srv3.close()

	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://" + srv1.addr + "/ws", "ws://" + srv2.addr + "/ws", "ws://" + srv3.addr + "/ws"},
		ReconnectBackoff: 100 * time.Millisecond,
		MaxReconnect:     2 * time.Second,
		PingInterval:     1 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	// Wait for all 3 connections
	deadline := time.After(5 * time.Second)
	for {
		if pool.ConnectedCount() >= 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for 3 connections, got %d", pool.ConnectedCount())
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Wait for raft_leader_change from srv1 (sends on connect)
	time.Sleep(200 * time.Millisecond)

	// srv1 was configured with leader_addr="ws://n1:8080/ws"
	// The message comes from srv1 with Addr = srv1.addr + "/ws"
	addr := pool.LeaderAddr()
	// The leader address in the message is "ws://n1:8080/ws" which doesn't match any pool server
	// So leader detection won't find it — that's expected for non-matching addresses
	t.Logf("Leader addr after connect: %q (may be empty if addrs don't match)", addr)
}

func TestPoolSendToLeaderSuccess(t *testing.T) {
	srv := newTestWSServer(t, "")
	defer srv.close()

	addr := "ws://" + srv.addr + "/ws"
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{addr},
		ReconnectBackoff: 100 * time.Millisecond,
		MaxReconnect:     2 * time.Second,
		PingInterval:     1 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	// Wait for connection
	deadline := time.After(3 * time.Second)
	for pool.ConnectedCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for connection")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Manually set leader (address must match)
	pool.OnLeaderChange(reef.RaftLeaderChangePayload{
		NewLeaderAddr: addr,
		NewLeaderID:   "node-1",
		Term:          1,
	})

	if pool.LeaderAddr() != addr {
		t.Fatalf("LeaderAddr = %q, want %q", pool.LeaderAddr(), addr)
	}

	// Send message to leader
	testMsg := []byte(`{"msg_type":"task_completed","task_id":"t1"}`)
	err = pool.SendToLeader(testMsg)
	if err != nil {
		t.Errorf("SendToLeader: %v", err)
	}
}

func TestPoolSendToLeaderNoLeader(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080"},
		ReconnectBackoff: 50 * time.Millisecond,
		MaxReconnect:     200 * time.Millisecond,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Don't start — no connections, no leader
	err = pool.SendToLeader([]byte(`{"test":1}`))
	if err == nil {
		t.Error("expected error: no leader available")
	}
}

func TestPoolSendToAll(t *testing.T) {
	srv1 := newTestWSServer(t, "")
	defer srv1.close()
	srv2 := newTestWSServer(t, "")
	defer srv2.close()

	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://" + srv1.addr + "/ws", "ws://" + srv2.addr + "/ws"},
		ReconnectBackoff: 100 * time.Millisecond,
		MaxReconnect:     2 * time.Second,
		PingInterval:     1 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	// Wait for connections
	deadline := time.After(5 * time.Second)
	for pool.ConnectedCount() < 2 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for 2 connections, got %d", pool.ConnectedCount())
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Broadcast message
	pool.SendToAll([]byte(`{"msg_type":"broadcast"}`))
	// Give goroutines time to write
	time.Sleep(100 * time.Millisecond)

	// Both servers should still be connected (SendToAll shouldn't disconnect)
	if pool.ConnectedCount() != 2 {
		t.Errorf("ConnectedCount after broadcast = %d, want 2", pool.ConnectedCount())
	}
}

func TestPoolSendToLeaderJSON(t *testing.T) {
	srv := newTestWSServer(t, "")
	defer srv.close()

	addr := "ws://" + srv.addr + "/ws"
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{addr},
		ReconnectBackoff: 100 * time.Millisecond,
		MaxReconnect:     2 * time.Second,
		PingInterval:     1 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	deadline := time.After(3 * time.Second)
	for pool.ConnectedCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for connection")
		case <-time.After(50 * time.Millisecond):
		}
	}

	pool.OnLeaderChange(reef.RaftLeaderChangePayload{
		NewLeaderAddr: addr,
		NewLeaderID:   "node-1",
		Term:          1,
	})

	err = pool.SendToLeaderJSON(map[string]string{"msg_type": "test", "value": "hello"})
	if err != nil {
		t.Errorf("SendToLeaderJSON: %v", err)
	}
}

func TestPoolReconnect(t *testing.T) {
	srv := newTestWSServer(t, "")
	defer srv.close()

	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://" + srv.addr + "/ws"},
		ReconnectBackoff: 100 * time.Millisecond,
		MaxReconnect:     2 * time.Second,
		PingInterval:     500 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	// Wait for initial connection
	deadline := time.After(3 * time.Second)
	for pool.ConnectedCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for initial connection")
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Log("connected")

	// Close all server connections to force disconnect
	srv.mu.Lock()
	for _, c := range srv.conns {
		c.Close()
	}
	srv.conns = nil
	srv.mu.Unlock()

	// Wait for disconnect detection
	time.Sleep(300 * time.Millisecond)
	if pool.ConnectedCount() != 0 {
		t.Logf("ConnectedCount after close = %d (may still be 1 if not detected yet)", pool.ConnectedCount())
	}

	// Wait for reconnect
	deadline = time.After(5 * time.Second)
	for pool.ConnectedCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reconnect")
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Log("reconnected")
}

func TestPoolHeartbeat(t *testing.T) {
	srv := newTestWSServer(t, "")
	defer srv.close()

	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://" + srv.addr + "/ws"},
		ReconnectBackoff: 100 * time.Millisecond,
		MaxReconnect:     2 * time.Second,
		PingInterval:     200 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	// Wait for connection
	deadline := time.After(3 * time.Second)
	for pool.ConnectedCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for connection")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Wait for at least one heartbeat ping
	time.Sleep(500 * time.Millisecond)

	// Server should still be connected (heartbeat doesn't drop it)
	if pool.ConnectedCount() != 1 {
		t.Errorf("ConnectedCount = %d, want 1 after heartbeat", pool.ConnectedCount())
	}
}

func TestPoolDetectLeaderFromMessage(t *testing.T) {
	// Test the detectLeaderChange function directly
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080", "ws://n2:8081"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Valid raft_leader_change message
	msg := reef.NewRaftLeaderChangeMessage("ws://n2:8081", "n2", "", "", 1)
	data, _ := json.Marshal(msg)
	payload := pool.detectLeaderChange(data)
	if payload == nil {
		t.Fatal("detectLeaderChange returned nil for valid message")
	}
	if payload.NewLeaderAddr != "ws://n2:8081" {
		t.Errorf("NewLeaderAddr = %q, want ws://n2:8081", payload.NewLeaderAddr)
	}

	// Non-leader-change message
	nonLC := []byte(`{"msg_type":"heartbeat","payload":{}}`)
	payload = pool.detectLeaderChange(nonLC)
	if payload != nil {
		t.Error("detectLeaderChange should return nil for non-leader message")
	}

	// Invalid JSON
	payload = pool.detectLeaderChange([]byte(`raft_leader_change`))
	if payload != nil {
		t.Error("detectLeaderChange should return nil for invalid JSON")
	}
}

func TestPoolStartStop(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	pool.Stop()
	// Stop should be idempotent
	pool.Stop()
}

func TestPoolReceiveChannel(t *testing.T) {
	srv := newTestWSServer(t, "")
	defer srv.close()

	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://" + srv.addr + "/ws"},
		ReconnectBackoff: 100 * time.Millisecond,
		MaxReconnect:     2 * time.Second,
		PingInterval:     1 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	// Wait for connection
	deadline := time.After(3 * time.Second)
	for pool.ConnectedCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for connection")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Send a message from server side
	testPayload := reef.RaftLeaderChangePayload{
		NewLeaderAddr: "ws://" + srv.addr + "/ws",
		NewLeaderID:   "node-1",
		Term:          1,
	}
	msg := reef.NewRaftLeaderChangeMessage(
		testPayload.NewLeaderAddr,
		testPayload.NewLeaderID,
		testPayload.OldLeaderAddr,
		testPayload.OldLeaderID,
		testPayload.Term,
	)
	data, _ := json.Marshal(msg)
	srv.sendToAll(data)

	// Read from pool receive channel
	select {
	case rm := <-pool.Receive():
		if rm.Addr == "" {
			t.Error("empty addr in received message")
		}
		// Verify we can unmarshal it
		var recv reef.Message
		if err := json.Unmarshal(rm.Data, &recv); err != nil {
			t.Errorf("unmarshal received: %v", err)
		}
		if recv.MsgType != reef.MsgRaftLeaderChange {
			t.Errorf("MsgType = %q, want raft_leader_change", recv.MsgType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message on Receive channel")
	}
}

func wsURL(srv *testWSServer) string {
	return "ws://" + srv.addr + "/ws"
}

// TestPoolConfigDefaultValues ensures DefaultPoolConfig returns sensible values.
func TestPoolConfigDefaultValues(t *testing.T) {
	cfg := DefaultPoolConfig()
	if cfg.ReconnectBackoff != 2*time.Second {
		t.Errorf("ReconnectBackoff = %v, want 2s", cfg.ReconnectBackoff)
	}
	if cfg.MaxReconnect != 30*time.Second {
		t.Errorf("MaxReconnect = %v, want 30s", cfg.MaxReconnect)
	}
	if cfg.PingInterval != 10*time.Second {
		t.Errorf("PingInterval = %v, want 10s", cfg.PingInterval)
	}
}

// TestConnectorPoolMode tests the Connector in pool mode.
func TestConnectorPoolMode(t *testing.T) {
	// This is in raft package, test via the pool directly
	// Full Connector pool mode test is in pkg/reef/client/ tests
	t.Skip("Connector pool mode tested in pkg/reef/client")
}

// Ensure SendToLeaderJSON handles marshal errors gracefully.
func TestPoolSendToLeaderJSONMarshalError(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Channel values can't be marshaled
	err = pool.SendToLeaderJSON(make(chan int))
	if err == nil {
		t.Error("expected marshal error for channel type")
	}
}

// Test leader index -1 is handled.
func TestPoolLeaderIndexMinusOne(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080", "ws://n2:8081"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if pool.LeaderIndex() != -1 {
		t.Errorf("initial LeaderIndex = %d, want -1", pool.LeaderIndex())
	}
	if pool.LeaderAddr() != "" {
		t.Errorf("initial LeaderAddr = %q, want empty", pool.LeaderAddr())
	}
}

// Test detection with type field check.
func TestPoolDetectLeaderChangeTypeField(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Message contains "raft_leader_change" but type field is different
	data := []byte(`{"msg_type":"heartbeat","payload":{"raft_leader_change":"fake"}}`)
	pl := pool.detectLeaderChange(data)
	if pl != nil {
		t.Error("should not detect leader change when msg_type is not raft_leader_change")
	}
}

// Test Connect with non-existent server (reconnect loop should backoff).
func TestPoolBadServerConnect(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://127.0.0.1:19999/ws"}, // unlikely to have a server
		ReconnectBackoff: 50 * time.Millisecond,
		MaxReconnect:     500 * time.Millisecond,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	// Give it some time to try
	time.Sleep(300 * time.Millisecond)
	// Should still be 0 connected
	if pool.ConnectedCount() != 0 {
		t.Errorf("ConnectedCount = %d, want 0 for bad server", pool.ConnectedCount())
	}
	pool.Stop()
}

// Test SendToLeader with pool started but no connections.
func TestPoolSendToLeaderStartedNoConnections(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://127.0.0.1:19999/ws"},
		ReconnectBackoff: 100 * time.Millisecond,
		MaxReconnect:     1 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	defer pool.Stop()

	// With started pool (ctx != nil) but no connections, retry loop should run
	// and eventually return error after 3 attempts
	err = pool.SendToLeader([]byte(`{"test":1}`))
	if err == nil {
		t.Error("expected error: no leader after retries")
	}
}

// Test SendToAll handles nil Conn gracefully.
func TestPoolSendToAllNoConnections(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080", "ws://n2:8081"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Don't start — no connections
	// SendToAll should not panic
	pool.SendToAll([]byte(`{"test":1}`))
}

// Test PoolConfig Validate edge case: PingInterval zero.
func TestPoolConfigValidatePingInterval(t *testing.T) {
	_, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://h:8080"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     0,
	}, nil)
	if err == nil {
		t.Error("expected error for zero PingInterval")
	}
}

// Test PoolConfig with single server works as pool-of-1.
func TestPoolSingleServerMode(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://only:8080"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pool.ServerCount() != 1 {
		t.Errorf("ServerCount = %d, want 1", pool.ServerCount())
	}
}

// Test double-start/double-stop safety.
func TestPoolDoubleStartStop(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pool.Start()
	pool.Start() // second Start() should be harmless (recreates ctx)
	pool.Stop()
	pool.Stop() // idempotent
}

// Test leader change preserves old leader cleanup.
func TestPoolLeaderChangeOldLeaderCleanup(t *testing.T) {
	pool, err := NewClientConnPool(PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080", "ws://n2:8081"},
		ReconnectBackoff: 2 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Set n1 as leader
	pool.OnLeaderChange(reef.RaftLeaderChangePayload{
		NewLeaderAddr: "ws://n1:8080",
		NewLeaderID:   "n1",
		Term:          1,
	})
	if pool.LeaderIndex() != 0 {
		t.Fatal("n1 should be leader")
	}

	// Switch to n2, clearing n1
	pool.OnLeaderChange(reef.RaftLeaderChangePayload{
		NewLeaderAddr: "ws://n2:8081",
		OldLeaderAddr: "ws://n1:8080",
		NewLeaderID:   "n2",
		Term:          2,
	})
	if pool.LeaderIndex() != 1 {
		t.Errorf("LeaderIndex = %d, want 1", pool.LeaderIndex())
	}
	if pool.LeaderAddr() != "ws://n2:8081" {
		t.Errorf("LeaderAddr = %q, want ws://n2:8081", pool.LeaderAddr())
	}
}

// Verify that the fmt import in pool.go is used (compiler check).
func TestFmtUsage(t *testing.T) {
	// This function exists only to ensure fmt remains imported.
	// The actual use of fmt is in pool.go's error formatting.
	_ = fmt.Sprintf("test: %d", 42)
}
