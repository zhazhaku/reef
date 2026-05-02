// Package raft provides Reef v1 Raft-based federation.
// Tests for HTTPTransport: send/receive, peer management, TLS, shutdown, integration.
package raft

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.etcd.io/raft/v3/raftpb"
)

// =====================================================================
// Helpers
// =====================================================================

// freePort returns a free TCP port on localhost.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func newHTTPTestTransport(t *testing.T, nodeID uint64) *HTTPTransport {
	t.Helper()
	addr := freePort(t)
	return NewHTTPTransport(nodeID, addr, nil, slog.New(slog.DiscardHandler))
}

// selfSignedCert generates a self-signed TLS certificate for "127.0.0.1".
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(1 * time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	return cert
}

func newTLSTransport(t *testing.T, nodeID uint64) *HTTPTransport {
	t.Helper()
	addr := freePort(t)
	cert := selfSignedCert(t)
	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	return NewHTTPTransport(nodeID, addr, tlsCfg, slog.New(slog.DiscardHandler))
}

// =====================================================================
// Task 1: NewHTTPTransport tests
// =====================================================================

func TestNewHTTPTransport(t *testing.T) {
	tr := NewHTTPTransport(1, "127.0.0.1:9999", nil, nil)
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
	if tr.Addr() != "127.0.0.1:9999" {
		t.Errorf("Addr = %q, want %q", tr.Addr(), "127.0.0.1:9999")
	}
	if tr.Incoming() == nil {
		t.Fatal("Incoming() returned nil channel")
	}
	if tr.nodeID != 1 {
		t.Errorf("nodeID = %d, want 1", tr.nodeID)
	}
}

func TestNewHTTPTransportNilLogger(t *testing.T) {
	tr := NewHTTPTransport(1, "127.0.0.1:9999", nil, nil)
	if tr == nil {
		t.Fatal("expected non-nil transport with nil logger")
	}
}

func TestNewHTTPTransportWithTLS(t *testing.T) {
	cert := selfSignedCert(t)
	tr := NewHTTPTransport(1, "127.0.0.1:9999", &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}, nil)
	if tr == nil {
		t.Fatal("expected non-nil transport with TLS")
	}
	if tr.tlsConfig == nil {
		t.Fatal("expected TLS config to be set")
	}
}

// =====================================================================
// Task 2: Start / Stop and receive
// =====================================================================

func TestTransportStartStop(t *testing.T) {
	tr := newHTTPTestTransport(t, 1)

	if err := tr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify it's listening
	addr := tr.Addr()
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		t.Fatalf("cannot connect to transport at %s: %v", addr, err)
	}
	conn.Close()

	tr.Stop()

	// Double stop should not panic
	tr.Stop()
}

func TestTransportSendReceive(t *testing.T) {
	tr1 := newHTTPTestTransport(t, 1)
	tr2 := newHTTPTestTransport(t, 2)

	if err := tr1.Start(); err != nil {
		t.Fatalf("tr1.Start: %v", err)
	}
	defer tr1.Stop()

	if err := tr2.Start(); err != nil {
		t.Fatalf("tr2.Start: %v", err)
	}
	defer tr2.Stop()

	tr1.AddPeer(2, tr2.Addr())
	tr2.AddPeer(1, tr1.Addr())

	// Send a message from node 1 to node 2
	msg := raftpb.Message{
		Type: raftpb.MsgApp,
		From: 1,
		To:   2,
		Term: 1,
	}
	tr1.Send([]raftpb.Message{msg})

	// Wait for node 2 to receive it
	select {
	case received := <-tr2.Incoming():
		if received.From != 1 || received.To != 2 || received.Type != raftpb.MsgApp {
			t.Errorf("received unexpected message: %+v", received)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

// =====================================================================
// Task 3: Send batch
// =====================================================================

func TestTransportSendBatch(t *testing.T) {
	tr1 := newHTTPTestTransport(t, 1)
	tr2 := newHTTPTestTransport(t, 2)
	tr3 := newHTTPTestTransport(t, 3)

	for _, tr := range []*HTTPTransport{tr1, tr2, tr3} {
		if err := tr.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer tr.Stop()
	}

	// Full mesh
	tr1.AddPeer(2, tr2.Addr())
	tr1.AddPeer(3, tr3.Addr())
	tr2.AddPeer(1, tr1.Addr())
	tr2.AddPeer(3, tr3.Addr())
	tr3.AddPeer(1, tr1.Addr())
	tr3.AddPeer(2, tr2.Addr())

	// Send 100 messages from node 1 to node 2
	const N = 100
	for i := 0; i < N; i++ {
		msg := raftpb.Message{
			Type:    raftpb.MsgApp,
			From:    1,
			To:      2,
			Term:    1,
			Index:   uint64(i),
			Context: []byte{byte(i)},
		}
		tr1.Send([]raftpb.Message{msg})
	}

	// Collect messages on node 2
	received := 0
	timeout := time.After(5 * time.Second)
	for received < N {
		select {
		case <-tr2.Incoming():
			received++
		case <-timeout:
			t.Fatalf("received %d/%d messages before timeout", received, N)
		}
	}

	if received != N {
		t.Errorf("received %d messages, want %d", received, N)
	}
}

// =====================================================================
// Task 3b: Send empty batch
// =====================================================================

func TestTransportSendEmpty(t *testing.T) {
	tr := newHTTPTestTransport(t, 1)
	if err := tr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Stop()

	// Send empty slice — should not panic
	tr.Send([]raftpb.Message{})
	tr.Send(nil)
}

// =====================================================================
// Task 4: Peer management
// =====================================================================

func TestPeerManagement(t *testing.T) {
	tr := newHTTPTestTransport(t, 1)

	// Add 3 peers
	tr.AddPeer(2, "127.0.0.1:10002")
	tr.AddPeer(3, "127.0.0.1:10003")
	tr.AddPeer(4, "127.0.0.1:10004")

	tr.mu.RLock()
	count := len(tr.peers)
	tr.mu.RUnlock()
	if count != 3 {
		t.Errorf("peer count = %d, want 3", count)
	}

	// Remove peer 3
	tr.RemovePeer(3)

	tr.mu.RLock()
	count = len(tr.peers)
	tr.mu.RUnlock()
	if count != 2 {
		t.Errorf("peer count after remove = %d, want 2", count)
	}

	// Re-add peer 3
	tr.AddPeer(3, "127.0.0.1:10003")

	tr.mu.RLock()
	count = len(tr.peers)
	tr.mu.RUnlock()
	if count != 3 {
		t.Errorf("peer count after re-add = %d, want 3", count)
	}

	// Remove non-existent peer (idempotent)
	tr.RemovePeer(99)

	// Add duplicate (idempotent)
	tr.AddPeer(2, "127.0.0.1:10002")
	tr.AddPeer(2, "127.0.0.1:99999") // different address, same ID — should be no-op

	tr.mu.RLock()
	count = len(tr.peers)
	tr.mu.RUnlock()
	if count != 3 {
		t.Errorf("peer count after idempotent ops = %d, want 3", count)
	}
}

func TestAddPeerSelf(t *testing.T) {
	tr := newHTTPTestTransport(t, 1)
	tr.AddPeer(1, tr.Addr()) // should log warning and skip

	tr.mu.RLock()
	_, ok := tr.peers[1]
	tr.mu.RUnlock()

	if ok {
		t.Error("transport should not add self as peer")
	}
}

func TestAddPeerBeforeStart(t *testing.T) {
	tr := newHTTPTestTransport(t, 1)
	// Add peers before Start() — should be fine
	tr.AddPeer(2, "127.0.0.1:20000")
	tr.AddPeer(3, "127.0.0.1:30000")

	tr.mu.RLock()
	count := len(tr.peers)
	tr.mu.RUnlock()
	if count != 2 {
		t.Errorf("peer count before start = %d, want 2", count)
	}
}

// =====================================================================
// Task 5: Unreachable peer
// =====================================================================

func TestTransportUnreachablePeer(t *testing.T) {
	tr1 := newHTTPTestTransport(t, 1)
	if err := tr1.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr1.Stop()

	// Send to a peer that doesn't exist (no AddPeer)
	msg := raftpb.Message{
		Type: raftpb.MsgApp,
		From: 1,
		To:   99,
		Term: 1,
	}
	// Should not panic
	tr1.Send([]raftpb.Message{msg})

	// Add a peer pointing to a closed port
	tr1.AddPeer(99, "127.0.0.1:1") // unlikely to be open
	// Should not panic — just log an error
	tr1.Send([]raftpb.Message{msg})

	// Give goroutines time to complete
	time.Sleep(100 * time.Millisecond)
}

// =====================================================================
// Task 5b: Heartbeat broadcast
// =====================================================================

func TestTransportHeartbeatBroadcast(t *testing.T) {
	tr1 := newHTTPTestTransport(t, 1)
	tr2 := newHTTPTestTransport(t, 2)
	tr3 := newHTTPTestTransport(t, 3)

	for _, tr := range []*HTTPTransport{tr1, tr2, tr3} {
		if err := tr.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer tr.Stop()
	}

	tr1.AddPeer(2, tr2.Addr())
	tr1.AddPeer(3, tr3.Addr())

	// Send heartbeats to both peers
	tr1.Send([]raftpb.Message{
		{Type: raftpb.MsgHeartbeat, From: 1, To: 2, Term: 1},
		{Type: raftpb.MsgHeartbeat, From: 1, To: 3, Term: 1},
	})

	// Both should receive their heartbeat
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		select {
		case msg := <-tr2.Incoming():
			if msg.Type != raftpb.MsgHeartbeat || msg.From != 1 {
				t.Errorf("tr2 received unexpected msg: %+v", msg)
			}
		case <-time.After(3 * time.Second):
			t.Error("tr2 timeout waiting for heartbeat")
		}
	}()
	go func() {
		defer wg.Done()
		select {
		case msg := <-tr3.Incoming():
			if msg.Type != raftpb.MsgHeartbeat || msg.From != 1 {
				t.Errorf("tr3 received unexpected msg: %+v", msg)
			}
		case <-time.After(3 * time.Second):
			t.Error("tr3 timeout waiting for heartbeat")
		}
	}()
	wg.Wait()
}

// =====================================================================
// Task 5c: Stop idempotent and goroutine leak
// =====================================================================

func TestTransportStopIdempotent(t *testing.T) {
	tr := newHTTPTestTransport(t, 1)
	if err := tr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	goroutinesBefore := runtime.NumGoroutine()

	// Multiple concurrent stops
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.Stop()
		}()
	}
	wg.Wait()

	// Allow goroutines to settle
	time.Sleep(200 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	if goroutinesAfter > goroutinesBefore+5 {
		t.Errorf("possible goroutine leak: before=%d after=%d", goroutinesBefore, goroutinesAfter)
	}
}

func TestTransportStopBeforeStart(t *testing.T) {
	tr := newHTTPTestTransport(t, 1)

	// Stop() before Start() should not panic
	tr.Stop()
	tr.Stop() // idempotent
}

// =====================================================================
// Task 6: 3-node message exchange simulation
// =====================================================================

func TestThreeNodeMessageExchange(t *testing.T) {
	tr1 := newHTTPTestTransport(t, 1)
	tr2 := newHTTPTestTransport(t, 2)
	tr3 := newHTTPTestTransport(t, 3)

	transports := []*HTTPTransport{tr1, tr2, tr3}
	for _, tr := range transports {
		if err := tr.Start(); err != nil {
			t.Fatalf("Start node %d: %v", tr.nodeID, err)
		}
	}

	// Full mesh
	tr1.AddPeer(2, tr2.Addr())
	tr1.AddPeer(3, tr3.Addr())
	tr2.AddPeer(1, tr1.Addr())
	tr2.AddPeer(3, tr3.Addr())
	tr3.AddPeer(1, tr1.Addr())
	tr3.AddPeer(2, tr2.Addr())

	// Send: 1→2, 2→3, 3→1
	tr1.Send([]raftpb.Message{{Type: raftpb.MsgApp, From: 1, To: 2, Term: 1}})
	tr2.Send([]raftpb.Message{{Type: raftpb.MsgApp, From: 2, To: 3, Term: 1}})
	tr3.Send([]raftpb.Message{{Type: raftpb.MsgApp, From: 3, To: 1, Term: 1}})

	// Each node should receive exactly 1 message from the correct sender
	recv := make(map[uint64][]raftpb.Message)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, tr := range transports {
		wg.Add(1)
		go func(tr *HTTPTransport) {
			defer wg.Done()
			select {
			case msg := <-tr.Incoming():
				mu.Lock()
				recv[tr.nodeID] = append(recv[tr.nodeID], msg)
				mu.Unlock()
			case <-time.After(3 * time.Second):
				t.Errorf("node %d timeout waiting for message", tr.nodeID)
			}
		}(tr)
	}
	wg.Wait()

	// Verify each received exactly 1 from the correct sender
	expected := map[uint64]uint64{1: 3, 2: 1, 3: 2} // nodeID -> expected From
	for nodeID, expFrom := range expected {
		msgs := recv[nodeID]
		if len(msgs) != 1 {
			t.Errorf("node %d received %d messages, want 1", nodeID, len(msgs))
			continue
		}
		if msgs[0].From != expFrom {
			t.Errorf("node %d received msg from %d, want from %d", nodeID, msgs[0].From, expFrom)
		}
	}

	// Stop all
	for _, tr := range transports {
		tr.Stop()
	}
}

// =====================================================================
// TLS tests
// =====================================================================

func TestTransportTLS(t *testing.T) {
	tr1 := newTLSTransport(t, 1)
	tr2 := newTLSTransport(t, 2)

	if err := tr1.Start(); err != nil {
		t.Fatalf("tr1.Start: %v", err)
	}
	defer tr1.Stop()

	if err := tr2.Start(); err != nil {
		t.Fatalf("tr2.Start: %v", err)
	}
	defer tr2.Stop()

	tr1.AddPeer(2, tr2.Addr())
	tr2.AddPeer(1, tr1.Addr())

	// Send message over TLS
	msg := raftpb.Message{
		Type:    raftpb.MsgApp,
		From:    1,
		To:      2,
		Term:    1,
		Context: []byte("tls-test"),
	}
	tr1.Send([]raftpb.Message{msg})

	select {
	case received := <-tr2.Incoming():
		if received.From != 1 || received.To != 2 {
			t.Errorf("TLS: received unexpected message: %+v", received)
		}
		if string(received.Context) != "tls-test" {
			t.Errorf("TLS: context = %q, want %q", string(received.Context), "tls-test")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TLS: timeout waiting for message")
	}
}

// =====================================================================
// Port conflict test
// =====================================================================

func TestTransportPortConflict(t *testing.T) {
	tr1 := newHTTPTestTransport(t, 1)
	if err := tr1.Start(); err != nil {
		t.Fatalf("tr1.Start: %v", err)
	}
	defer tr1.Stop()

	// Try to start another transport on the same address
	tr2 := NewHTTPTransport(2, tr1.Addr(), nil, slog.New(slog.DiscardHandler))
	if err := tr2.Start(); err == nil {
		tr2.Stop()
		t.Fatal("expected error starting on occupied port")
	}
}

// =====================================================================
// Large message test
// =====================================================================

func TestTransportLargeMessage(t *testing.T) {
	tr1 := newHTTPTestTransport(t, 1)
	tr2 := newHTTPTestTransport(t, 2)

	if err := tr1.Start(); err != nil {
		t.Fatalf("tr1.Start: %v", err)
	}
	defer tr1.Stop()

	if err := tr2.Start(); err != nil {
		t.Fatalf("tr2.Start: %v", err)
	}
	defer tr2.Stop()

	tr1.AddPeer(2, tr2.Addr())

	// Send a message with ~500KB of context data
	largeContext := make([]byte, 500000)
	for i := range largeContext {
		largeContext[i] = byte(i % 256)
	}

	msg := raftpb.Message{
		Type:    raftpb.MsgApp,
		From:    1,
		To:      2,
		Term:    1,
		Context: largeContext,
	}
	tr1.Send([]raftpb.Message{msg})

	select {
	case received := <-tr2.Incoming():
		if len(received.Context) != 500000 {
			t.Errorf("large msg: context length = %d, want %d", len(received.Context), 500000)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for large message")
	}
}

// =====================================================================
// Integration test: transport with snapshot entries
// =====================================================================

func TestTransportSnapshotContext(t *testing.T) {
	tr1 := newHTTPTestTransport(t, 1)
	tr2 := newHTTPTestTransport(t, 2)

	if err := tr1.Start(); err != nil {
		t.Fatalf("tr1.Start: %v", err)
	}
	defer tr1.Stop()

	if err := tr2.Start(); err != nil {
		t.Fatalf("tr2.Start: %v", err)
	}
	defer tr2.Stop()

	tr1.AddPeer(2, tr2.Addr())

	// Send a message with snapshot data in context
	snapData := make([]byte, 1024)
	for i := range snapData {
		snapData[i] = 0xAB
	}

	msg := raftpb.Message{
		Type: raftpb.MsgSnap,
		From: 1,
		To:   2,
		Term: 1,
		Snapshot: &raftpb.Snapshot{
			Data: snapData,
			Metadata: raftpb.SnapshotMetadata{
				Index: 100,
				Term:  5,
			},
		},
	}
	tr1.Send([]raftpb.Message{msg})

	select {
	case received := <-tr2.Incoming():
		if received.Type != raftpb.MsgSnap {
			t.Errorf("expected MsgSnap, got %v", received.Type)
		}
		if received.Snapshot == nil {
			t.Fatal("snapshot is nil in received message")
		}
		if received.Snapshot.Metadata.Index != 100 {
			t.Errorf("snapshot index = %d, want 100", received.Snapshot.Metadata.Index)
		}
		if len(received.Snapshot.Data) != 1024 {
			t.Errorf("snapshot data length = %d, want 1024", len(received.Snapshot.Data))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for snapshot message")
	}
}

// =====================================================================
// Multiple Start calls
// =====================================================================

func TestTransportDoubleStart(t *testing.T) {
	tr := newHTTPTestTransport(t, 1)
	if err := tr.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer tr.Stop()

	// Second Start should be a no-op (due to sync.Once)
	if err := tr.Start(); err != nil {
		t.Fatalf("second Start should be no-op, got: %v", err)
	}
}
