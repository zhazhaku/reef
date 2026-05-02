// Package raft provides Reef v1 Raft-based federation.
// HTTPTransport — HTTP-based Raft node-to-node message transport with optional TLS.
package raft

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogo/protobuf/proto"
	"go.etcd.io/raft/v3/raftpb"
)

// =====================================================================
// peerClient — a peer's address reference
// =====================================================================

type peerClient struct {
	addr string
}

// =====================================================================
// HTTPTransport
// =====================================================================

// HTTPTransport implements the Transport interface using HTTP POST to
// deliver raftpb.Message protobufs between cluster peers. It supports
// optional TLS for production deployments.
type HTTPTransport struct {
	mu         sync.RWMutex
	nodeID     uint64
	addr       string // This node's listen address (e.g., "127.0.0.1:9090")
	peers      map[uint64]*peerClient
	incomingCh chan raftpb.Message
	server     *http.Server
	tlsConfig  *tls.Config // nil = plain HTTP
	client     *http.Client
	logger     *slog.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	stopped    atomic.Bool
	stopOnce   sync.Once
	startOnce  sync.Once
}

// NewHTTPTransport creates a new HTTPTransport.
//   - nodeID: this node's Raft ID
//   - addr: listen address (e.g., "127.0.0.1:9090")
//   - tlsConfig: optional TLS config; nil means plain HTTP
//   - logger: optional logger; defaults to slog.Default()
func NewHTTPTransport(nodeID uint64, addr string, tlsConfig *tls.Config, logger *slog.Logger) *HTTPTransport {
	if logger == nil {
		logger = slog.Default()
	}

	tr := &http.Transport{
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     tlsConfig,
	}

	return &HTTPTransport{
		nodeID:     nodeID,
		addr:       addr,
		peers:      make(map[uint64]*peerClient),
		incomingCh: make(chan raftpb.Message, 256),
		tlsConfig:  tlsConfig,
		client:     &http.Client{Transport: tr},
		logger:     logger,
	}
}

// =====================================================================
// Transport interface implementation
// =====================================================================

// Addr returns this transport's listen address.
func (t *HTTPTransport) Addr() string {
	return t.addr
}

// Incoming returns the channel of messages received from peers.
func (t *HTTPTransport) Incoming() <-chan raftpb.Message {
	return t.incomingCh
}

// =====================================================================
// Lifecycle: Start / Stop
// =====================================================================

// Start begins listening for incoming HTTP requests.
func (t *HTTPTransport) Start() error {
	var startErr error
	t.startOnce.Do(func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/raft/message", t.handleMessage)

		t.server = &http.Server{
			Addr:    t.addr,
			Handler: mux,
		}
		if t.tlsConfig != nil {
			t.server.TLSConfig = t.tlsConfig
			// Disable HTTP/2 to keep the transport simple (raft messages are
			// small, single-request POSTs that don't benefit from multiplexing).
			t.server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
		}

		// Try to bind the address early so we can fail fast if port is taken.
		ln, err := net.Listen("tcp", t.addr)
		if err != nil {
			startErr = fmt.Errorf("transport listen %s: %w", t.addr, err)
			return
		}

		t.ctx, t.cancel = context.WithCancel(context.Background())

		go func() {
			if t.tlsConfig != nil {
				ln = tls.NewListener(ln, t.tlsConfig)
			}
			_ = t.server.Serve(ln)
		}()

		t.logger.Info("transport started", "addr", t.addr, "tls", t.tlsConfig != nil)
	})
	return startErr
}

// Stop gracefully shuts down the transport. Idempotent.
func (t *HTTPTransport) Stop() {
	t.stopOnce.Do(func() {
		t.logger.Info("transport stopping", "addr", t.addr)

		// Cancel context to abort in-flight requests
		if t.cancel != nil {
			t.cancel()
		}

		// Shut down the HTTP server
		if t.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := t.server.Shutdown(ctx); err != nil {
				t.logger.Warn("transport shutdown error", "addr", t.addr, "error", err)
			}
		}

		t.stopped.Store(true)

		// Close incoming channel only after server is shut down
		close(t.incomingCh)

		t.logger.Info("transport stopped", "addr", t.addr)
	})
}

// =====================================================================
// Peer management
// =====================================================================

// AddPeer registers a peer. Idempotent: no-op if already present.
// Logs a warning and skips if the peer address matches our own.
func (t *HTTPTransport) AddPeer(id uint64, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.peers[id]; ok {
		return // already present
	}

	// Refuse to add self as peer
	if addr == t.addr && id == t.nodeID {
		t.logger.Warn("refusing to add self as peer", "id", id, "addr", addr)
		return
	}

	t.peers[id] = &peerClient{addr: addr}
	t.logger.Info("peer added", "id", id, "addr", addr)
}

// RemovePeer unregisters a peer. Idempotent: no-op if not present.
func (t *HTTPTransport) RemovePeer(id uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.peers[id]; !ok {
		return
	}
	delete(t.peers, id)
	t.logger.Info("peer removed", "id", id)
}

// =====================================================================
// Send — dispatch messages to peers
// =====================================================================

// Send delivers a batch of messages to their destination peers.
// Each message is sent concurrently via goroutine. This method returns
// immediately without waiting for delivery.
func (t *HTTPTransport) Send(messages []raftpb.Message) {
	for i := range messages {
		msg := &messages[i]

		t.mu.RLock()
		peer, ok := t.peers[msg.To]
		t.mu.RUnlock()

		if !ok {
			t.logger.Warn("send: peer not found", "to", msg.To, "type", msg.Type)
			continue
		}

		go t.sendToPeer(peer, msg)
	}
}

// sendToPeer marshals a message to protobuf and POSTs it to the peer.
func (t *HTTPTransport) sendToPeer(peer *peerClient, msg *raftpb.Message) {
	data, err := proto.Marshal(msg)
	if err != nil {
		t.logger.Error("marshal message failed", "error", err)
		return
	}

	scheme := "http"
	if t.tlsConfig != nil {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/raft/message", scheme, peer.addr)

	// Use a timeout context derived from t.ctx so Stop() cancels in-flight requests.
	var reqCtx context.Context
	var cancel context.CancelFunc
	if t.ctx != nil {
		reqCtx, cancel = context.WithTimeout(t.ctx, 5*time.Second)
	} else {
		reqCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		t.logger.Error("create request failed", "url", url, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := t.client.Do(req)
	if err != nil {
		t.logger.Error("send failed", "to", msg.To, "addr", peer.addr, "type", msg.Type, "error", err)
		return
	}
	defer resp.Body.Close()

	// Discard body to allow connection reuse
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.logger.Warn("unexpected status from peer",
			"to", msg.To,
			"addr", peer.addr,
			"status", resp.StatusCode,
		)
	}
}

// =====================================================================
// HTTP handler — receive messages from peers
// =====================================================================

// handleMessage is the HTTP handler for POST /raft/message.
// It deserializes a protobuf raftpb.Message and delivers it to the
// incoming channel for processing by the RaftNode's receive loop.
func (t *HTTPTransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit body size to 16MB to prevent OOM from large snapshots
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<24))
	if err != nil {
		t.logger.Error("read request body failed", "error", err)
		http.Error(w, "read body failed", http.StatusInternalServerError)
		return
	}

	var msg raftpb.Message
	if err := proto.Unmarshal(body, &msg); err != nil {
		t.logger.Warn("invalid protobuf in request", "error", err)
		http.Error(w, "invalid protobuf", http.StatusBadRequest)
		return
	}

	// Non-blocking send to incoming channel
	select {
	case t.incomingCh <- msg:
	default:
		t.logger.Warn("incoming channel full, dropping message",
			"from", msg.From,
			"to", msg.To,
			"type", msg.Type,
		)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
