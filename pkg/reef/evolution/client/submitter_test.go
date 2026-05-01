package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

var wsUpgrader = websocket.Upgrader{}

// mockWSServer creates an httptest server that accepts WS connections and
// sends received messages to a channel for verification.
type mockWSServer struct {
	server   *httptest.Server
	messages chan reef.Message
	mu       sync.Mutex
	conn     *websocket.Conn
}

func newMockWSServer() *mockWSServer {
	m := &mockWSServer{
		messages: make(chan reef.Message, 32),
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conn = conn
		m.mu.Unlock()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg reef.Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			select {
			case m.messages <- msg:
			default:
			}
		}
	}))
	return m
}

func (m *mockWSServer) Close() {
	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
	}
	m.mu.Unlock()
	m.server.Close()
}

func (m *mockWSServer) URL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

func (m *mockWSServer) NextMessage(timeout time.Duration) *reef.Message {
	select {
	case msg := <-m.messages:
		return &msg
	case <-time.After(timeout):
		return nil
	}
}

// connectToMockWS dials the mock WS server and returns the client connection.
func connectToMockWS(url string) (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	return conn, err
}

func makeTestGene(id string) *evolution.Gene {
	return &evolution.Gene{
		ID:              id,
		StrategyName:    "test-strategy",
		Role:            "tester",
		Skills:          []string{"test"},
		MatchCondition:  "error",
		ControlSignal:   "echo hello",
		FailureWarnings: []string{},
		SourceEvents:    []string{"evt-001"},
		SourceClientID:  "client-1",
		Version:         1,
		Status:          evolution.GeneStatusDraft,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
}

// ---------------------------------------------------------------------------
// Submit tests
// ---------------------------------------------------------------------------

func TestSubmitter_OnlineSubmit(t *testing.T) {
	mock := newMockWSServer()
	defer mock.Close()

	conn, err := connectToMockWS(mock.URL())
	if err != nil {
		t.Fatalf("failed to connect to mock WS: %v", err)
	}
	defer conn.Close()

	s := NewGeneSubmitter(SubmitterConfig{}, nil)
	s.SetConn(conn)

	gene := makeTestGene("gene-online-1")
	s.Submit(gene)

	// Verify message received
	msg := mock.NextMessage(2 * time.Second)
	if msg == nil {
		t.Fatal("expected gene_submit message, got nil")
	}
	if msg.MsgType != reef.MsgGeneSubmit {
		t.Errorf("expected MsgGeneSubmit, got %s", msg.MsgType)
	}

	// Verify payload
	var payload reef.GeneSubmitPayload
	if err := msg.DecodePayload(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.GeneID != "gene-online-1" {
		t.Errorf("GeneID = %s, want gene-online-1", payload.GeneID)
	}
	if payload.ClientID != "client-1" {
		t.Errorf("ClientID = %s, want client-1", payload.ClientID)
	}
	if len(payload.SourceEventIDs) != 1 || payload.SourceEventIDs[0] != "evt-001" {
		t.Errorf("SourceEventIDs = %v, want [evt-001]", payload.SourceEventIDs)
	}

	// Verify gene status updated
	if gene.Status != evolution.GeneStatusSubmitted {
		t.Errorf("gene status = %s, want submitted", gene.Status)
	}
}

func TestSubmitter_OfflineEnqueue(t *testing.T) {
	s := NewGeneSubmitter(SubmitterConfig{}, nil)
	// No WS connection — offline

	gene := makeTestGene("gene-offline-1")
	s.Submit(gene)

	if s.QueueLen() != 1 {
		t.Errorf("queue length = %d, want 1", s.QueueLen())
	}

	// Gene status should stay Draft while queued
	if gene.Status != evolution.GeneStatusDraft {
		t.Errorf("gene status = %s, want draft (not submitted while queued)", gene.Status)
	}
}

func TestSubmitter_QueueFull_DropsOldest(t *testing.T) {
	s := NewGeneSubmitter(SubmitterConfig{MaxQueueSize: 3}, nil)

	// Fill queue
	for i := 0; i < 3; i++ {
		gene := makeTestGene("gene-" + string(rune('a'+i)))
		s.Submit(gene)
	}
	if s.QueueLen() != 3 {
		t.Fatalf("queue length = %d, want 3", s.QueueLen())
	}

	// Add 4th — oldest dropped
	geneD := makeTestGene("gene-d")
	s.Submit(geneD)

	if s.QueueLen() != 3 {
		t.Errorf("queue length = %d, want 3 after overflow", s.QueueLen())
	}

	// The oldest (gene-a) should be gone. gene-b, gene-c, gene-d remain.
	s.queueMu.Lock()
	ids := make([]string, len(s.offlineQueue))
	for i, g := range s.offlineQueue {
		ids[i] = g.ID
	}
	s.queueMu.Unlock()

	if ids[0] == "gene-a" {
		t.Error("oldest gene should have been dropped")
	}
}

func TestSubmitter_StagnantGene_NotSubmitted(t *testing.T) {
	mock := newMockWSServer()
	defer mock.Close()

	conn, err := connectToMockWS(mock.URL())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	s := NewGeneSubmitter(SubmitterConfig{}, nil)
	s.SetConn(conn)

	gene := makeTestGene("gene-stagnant")
	gene.Status = evolution.GeneStatusStagnant
	s.Submit(gene)

	// Should not be in queue
	if s.QueueLen() != 0 {
		t.Errorf("queue length = %d, want 0 (stagnant should not be queued)", s.QueueLen())
	}

	// No message should arrive
	msg := mock.NextMessage(500 * time.Millisecond)
	if msg != nil {
		t.Error("stagnant gene should not be submitted")
	}
}

func TestSubmitter_NilGene(t *testing.T) {
	s := NewGeneSubmitter(SubmitterConfig{}, nil)
	s.Submit(nil) // should not panic
	if s.QueueLen() != 0 {
		t.Errorf("nil gene should not be queued, got %d", s.QueueLen())
	}
}

// ---------------------------------------------------------------------------
// Drain queue tests
// ---------------------------------------------------------------------------

func TestSubmitter_DrainQueue_OnReconnect(t *testing.T) {
	mock := newMockWSServer()
	defer mock.Close()

	s := NewGeneSubmitter(SubmitterConfig{}, nil)

	// Enqueue 2 genes while offline
	gene1 := makeTestGene("gene-drain-1")
	gene2 := makeTestGene("gene-drain-2")
	s.Submit(gene1)
	s.Submit(gene2)

	if s.QueueLen() != 2 {
		t.Fatalf("queue = %d, want 2", s.QueueLen())
	}

	// Reconnect
	conn, err := connectToMockWS(mock.URL())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	s.OnReconnect(conn)

	// Wait for drain (happens in goroutine)
	time.Sleep(200 * time.Millisecond)

	// Should receive 2 messages
	msg1 := mock.NextMessage(2 * time.Second)
	msg2 := mock.NextMessage(2 * time.Second)

	if msg1 == nil || msg2 == nil {
		t.Fatal("expected 2 drained messages")
	}

	// Verify FIFO order
	var p1, p2 reef.GeneSubmitPayload
	msg1.DecodePayload(&p1)
	msg2.DecodePayload(&p2)

	if p1.GeneID != "gene-drain-1" {
		t.Errorf("first drained = %s, want gene-drain-1", p1.GeneID)
	}
	if p2.GeneID != "gene-drain-2" {
		t.Errorf("second drained = %s, want gene-drain-2", p2.GeneID)
	}

	// Queue should be empty
	time.Sleep(100 * time.Millisecond)
	if s.QueueLen() != 0 {
		t.Errorf("queue after drain = %d, want 0", s.QueueLen())
	}
}

// ---------------------------------------------------------------------------
// Queue persistence tests
// ---------------------------------------------------------------------------

func TestSubmitter_PersistAndRestore(t *testing.T) {
	tmpDir := t.TempDir()
	queuePath := filepath.Join(tmpDir, "offline_genes.json")

	// Create submitter, enqueue 3 genes
	s1 := NewGeneSubmitter(SubmitterConfig{QueueFilePath: queuePath}, nil)
	for i := 0; i < 3; i++ {
		gene := makeTestGene("gene-persist-" + string(rune('0'+i)))
		s1.Submit(gene)
	}

	// Persist
	if err := s1.PersistQueue(); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(queuePath); err != nil {
		t.Fatalf("queue file not created: %v", err)
	}

	// Create new submitter, restore
	s2 := NewGeneSubmitter(SubmitterConfig{QueueFilePath: queuePath}, nil)
	if err := s2.RestoreQueue(); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if s2.QueueLen() != 3 {
		t.Errorf("restored queue length = %d, want 3", s2.QueueLen())
	}

	// Verify gene IDs
	s2.queueMu.Lock()
	ids := make([]string, len(s2.offlineQueue))
	for i, g := range s2.offlineQueue {
		ids[i] = g.ID
	}
	s2.queueMu.Unlock()

	expected := []string{"gene-persist-0", "gene-persist-1", "gene-persist-2"}
	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("restored[%d] = %s, want %s", i, id, expected[i])
		}
	}
}

func TestSubmitter_PersistEmptyQueue_RemovesFile(t *testing.T) {
	tmpDir := t.TempDir()
	queuePath := filepath.Join(tmpDir, "offline_empty.json")

	// Create queue file first
	os.WriteFile(queuePath, []byte(`[]`), 0644)

	s := NewGeneSubmitter(SubmitterConfig{QueueFilePath: queuePath}, nil)
	if err := s.PersistQueue(); err != nil {
		t.Fatalf("persist empty: %v", err)
	}

	// File should be removed
	if _, err := os.Stat(queuePath); !os.IsNotExist(err) {
		t.Error("queue file should be removed for empty queue")
	}
}

func TestSubmitter_RestoreMissingFile_NoOp(t *testing.T) {
	tmpDir := t.TempDir()
	queuePath := filepath.Join(tmpDir, "nonexistent.json")

	s := NewGeneSubmitter(SubmitterConfig{QueueFilePath: queuePath}, nil)
	if err := s.RestoreQueue(); err != nil {
		t.Errorf("restore missing file should be no-op, got: %v", err)
	}
	if s.QueueLen() != 0 {
		t.Errorf("queue length = %d, want 0", s.QueueLen())
	}
}

func TestSubmitter_RestoreCorruptedFile_Error(t *testing.T) {
	tmpDir := t.TempDir()
	queuePath := filepath.Join(tmpDir, "corrupted.json")

	// Write corrupted JSON
	os.WriteFile(queuePath, []byte(`{corrupted json!!!`), 0644)

	s := NewGeneSubmitter(SubmitterConfig{QueueFilePath: queuePath}, nil)
	err := s.RestoreQueue()
	if err == nil {
		t.Error("expected error for corrupted file")
	}

	// Queue should be empty
	if s.QueueLen() != 0 {
		t.Errorf("queue should be empty after corrupted restore, got %d", s.QueueLen())
	}

	// File should be deleted
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("corrupted file should be deleted")
	}
}

func TestSubmitter_PersistNonExistentDir_Error(t *testing.T) {
	// Use a path where the parent doesn't exist and can't be created
	// (e.g., /nonexistent/... — but that might work on some systems)
	// Use a path that's clearly invalid
	s := NewGeneSubmitter(SubmitterConfig{QueueFilePath: "/dev/null/sub/dir/test.json"}, nil)
	gene := makeTestGene("gene-bad-path")
	s.Submit(gene)

	err := s.PersistQueue()
	if err == nil {
		t.Error("expected error when persisting to invalid path")
	}
	// Queue should still be intact in memory
	if s.QueueLen() != 1 {
		t.Errorf("queue length = %d, want 1 (in-memory queue preserved)", s.QueueLen())
	}
}

// ---------------------------------------------------------------------------
// Concurrent access test
// ---------------------------------------------------------------------------

func TestSubmitter_ConcurrentSubmit_Safe(t *testing.T) {
	s := NewGeneSubmitter(SubmitterConfig{MaxQueueSize: 100}, nil)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		id := "gene-conc-" + string(rune('a'+i%26))
		go func(geneID string) {
			defer wg.Done()
			gene := makeTestGene(geneID)
			s.Submit(gene)
		}(id)
	}
	wg.Wait()

	// Should not panic, queue should have entries
	if s.QueueLen() == 0 {
		t.Error("expected genes in queue after concurrent submits")
	}
}

// ---------------------------------------------------------------------------
// Submit with retry test
// ---------------------------------------------------------------------------

func TestSubmitter_SubmitWithRetry_NoConnection(t *testing.T) {
	s := NewGeneSubmitter(SubmitterConfig{MaxRetries: 3}, nil)
	gene := makeTestGene("gene-retry")

	// No connection
	ok := s.submitWithRetry(gene)
	if ok {
		t.Error("submitWithRetry should fail when no connection")
	}
}

// ---------------------------------------------------------------------------
// GeneSubmitPayload validation test
// ---------------------------------------------------------------------------

func TestSubmitter_PayloadFields(t *testing.T) {
	mock := newMockWSServer()
	defer mock.Close()

	conn, err := connectToMockWS(mock.URL())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	s := NewGeneSubmitter(SubmitterConfig{}, nil)
	s.SetConn(conn)

	gene := makeTestGene("gene-payload-test")
	gene.SourceEvents = []string{"evt-a", "evt-b"}
	gene.SourceClientID = "client-xyz"

	s.Submit(gene)

	msg := mock.NextMessage(2 * time.Second)
	if msg == nil {
		t.Fatal("no message received")
	}

	var payload reef.GeneSubmitPayload
	if err := msg.DecodePayload(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	// Verify GeneData is valid JSON and contains gene data
	var geneFromPayload evolution.Gene
	if err := json.Unmarshal(payload.GeneData, &geneFromPayload); err != nil {
		t.Fatalf("GeneData is not valid gene JSON: %v", err)
	}
	if geneFromPayload.ID != "gene-payload-test" {
		t.Errorf("GeneData.ID = %s, want gene-payload-test", geneFromPayload.ID)
	}

	// Verify SourceEventIDs
	if len(payload.SourceEventIDs) != 2 {
		t.Errorf("SourceEventIDs len = %d, want 2", len(payload.SourceEventIDs))
	}

	// Verify ClientID
	if payload.ClientID != "client-xyz" {
		t.Errorf("ClientID = %s, want client-xyz", payload.ClientID)
	}
}

// ---------------------------------------------------------------------------
// End-to-end integration test: Gate → Submit → Queue → Drain
// ---------------------------------------------------------------------------

func TestGateSubmitEndToEnd(t *testing.T) {
	// 1. Create LocalGatekeeper with default config
	gk := NewGatekeeper(GateConfig{EnableSemanticCheck: true}, nil)

	// 2. Create GeneSubmitter (offline initially)
	s := NewGeneSubmitter(SubmitterConfig{MaxRetries: 1}, nil)

	// 3. Generate a valid Gene, run through Gate → should pass
	gene1 := makeTestGene("gene-e2e-1")
	gene1.ControlSignal = "echo hello world"

	pass, reason := gk.CheckWithReason(gene1)
	if !pass {
		t.Fatalf("gate rejected valid gene: %s", reason)
	}

	// 4. Submit with nil conn → gene goes to queue
	s.Submit(gene1)
	if s.QueueLen() != 1 {
		t.Errorf("queue length = %d, want 1", s.QueueLen())
	}

	// 5. Create mock WebSocket and reconnect — drain should submit queued gene
	mock := newMockWSServer()
	defer mock.Close()

	conn, err := connectToMockWS(mock.URL())
	if err != nil {
		t.Fatalf("connect mock WS: %v", err)
	}
	defer conn.Close()

	s.OnReconnect(conn)

	// Wait for drain goroutine
	time.Sleep(200 * time.Millisecond)

	// Should receive MsgGeneSubmit for gene1
	msg1 := mock.NextMessage(2 * time.Second)
	if msg1 == nil {
		t.Fatal("expected gene_submit message after drain, got nil")
	}
	if msg1.MsgType != reef.MsgGeneSubmit {
		t.Errorf("msg type = %s, want gene_submit", msg1.MsgType)
	}

	var payload1 reef.GeneSubmitPayload
	if err := msg1.DecodePayload(&payload1); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload1.GeneID != "gene-e2e-1" {
		t.Errorf("GeneID = %s, want gene-e2e-1", payload1.GeneID)
	}
	if payload1.ClientID != "client-1" {
		t.Errorf("ClientID = %s, want client-1", payload1.ClientID)
	}

	// 6. Submit a valid gene while connected → goes directly to WS
	gene2 := makeTestGene("gene-e2e-2")
	gene2.ControlSignal = "ls -la"

	pass, _ = gk.CheckWithReason(gene2)
	if !pass {
		t.Fatalf("gate rejected valid gene2")
	}

	s.Submit(gene2)

	msg2 := mock.NextMessage(2 * time.Second)
	if msg2 == nil {
		t.Fatal("expected gene_submit for gene2, got nil")
	}
	var payload2 reef.GeneSubmitPayload
	msg2.DecodePayload(&payload2)
	if payload2.GeneID != "gene-e2e-2" {
		t.Errorf("GeneID = %s, want gene-e2e-2", payload2.GeneID)
	}

	// Verify gene2 status is submitted
	if gene2.Status != evolution.GeneStatusSubmitted {
		t.Errorf("gene2 status = %s, want submitted", gene2.Status)
	}

	// 7. Submit a gene with dangerous pattern → Gate rejects → not submitted
	gene3 := makeTestGene("gene-e2e-3")
	gene3.ControlSignal = "rm -rf /tmp/data"

	pass, reason = gk.CheckWithReason(gene3)
	if pass {
		t.Error("gate should reject dangerous gene")
	}
	if !strings.Contains(reason, "semantics") {
		t.Errorf("rejection reason should be semantic, got: %s", reason)
	}

	// Gate rejected — do NOT submit. Verify gene status unchanged.
	if gene3.Status != evolution.GeneStatusDraft {
		t.Errorf("rejected gene status = %s, want draft", gene3.Status)
	}

	// 8. Disconnect mock WS → Submit → gene goes to queue
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	gene4 := makeTestGene("gene-e2e-4")
	gene4.ControlSignal = "cat /etc/hosts"

	pass, _ = gk.CheckWithReason(gene4)
	if !pass {
		t.Fatalf("gate rejected valid gene4")
	}

	// Set conn to nil to simulate disconnection
	s.SetConn(nil)
	s.Submit(gene4)

	if s.QueueLen() != 1 {
		t.Errorf("queue length after disconnect = %d, want 1", s.QueueLen())
	}

	// 9. Reconnect → drainQueue → mock WS receives queued message
	conn2, err := connectToMockWS(mock.URL())
	if err != nil {
		t.Fatalf("reconnect mock WS: %v", err)
	}
	defer conn2.Close()

	s.OnReconnect(conn2)
	time.Sleep(200 * time.Millisecond)

	msg4 := mock.NextMessage(2 * time.Second)
	if msg4 == nil {
		t.Fatal("expected gene_submit for gene4 after reconnect drain")
	}
	var payload4 reef.GeneSubmitPayload
	msg4.DecodePayload(&payload4)
	if payload4.GeneID != "gene-e2e-4" {
		t.Errorf("GeneID = %s, want gene-e2e-4", payload4.GeneID)
	}

	// Queue should be empty after drain
	time.Sleep(100 * time.Millisecond)
	if s.QueueLen() != 0 {
		t.Errorf("queue should be empty after drain, got %d", s.QueueLen())
	}
}
