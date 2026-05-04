//go:build integration

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// Test helper: mockGeneHandler implements GeneSubmitHandler for integration tests.
// It validates genes, approves safe ones, rejects dangerous ones, and broadcasts
// approved genes to same-role connected clients.
// ---------------------------------------------------------------------------

type mockGeneHandler struct {
	mu           sync.Mutex
	wsServer     *WebSocketServer
	approvedCount int
	rejectedCount int
	dangerousPatterns []string
	// Track what was sent to which client for verification
	lastApproved map[string]*reef.GeneApprovedPayload // clientID → payload
	lastRejected map[string]*reef.GeneRejectedPayload // clientID → payload
}

func newMockGeneHandler(ws *WebSocketServer) *mockGeneHandler {
	return &mockGeneHandler{
		wsServer:     ws,
		dangerousPatterns: []string{
			`rm\s+-rf\s+/`,
			`rm\s+-rf\s+~`,
			`sudo\s+`,
			`DROP\s+TABLE`,
			`shutdown`,
			`\beval\b`,
			`\bexec\b`,
		},
		lastApproved: make(map[string]*reef.GeneApprovedPayload),
		lastRejected: make(map[string]*reef.GeneRejectedPayload),
	}
}

func (h *mockGeneHandler) HandleGeneSubmission(clientID string, msg reef.Message) error {
	var payload reef.GeneSubmitPayload
	if err := msg.DecodePayload(&payload); err != nil {
		return err
	}

	var gene evolution.Gene
	if err := json.Unmarshal(payload.GeneData, &gene); err != nil {
		return err
	}

	// Safety check: reject dangerous genes
	for _, pat := range h.dangerousPatterns {
		if strings.Contains(strings.ToLower(gene.ControlSignal), strings.ToLower(pat)) ||
		   strings.Contains(strings.ToLower(gene.ControlSignal), strings.ReplaceAll(strings.ToLower(pat), `\s+`, " ")) {
			// Also check for "rm -rf /" specifically
			if strings.Contains(gene.ControlSignal, "rm -rf /") ||
			   strings.Contains(gene.ControlSignal, "rm -rf") ||
			   strings.Contains(strings.ToLower(gene.ControlSignal), "sudo") ||
			   strings.Contains(strings.ToLower(gene.ControlSignal), "shutdown") ||
			   strings.Contains(strings.ToLower(gene.ControlSignal), "eval") ||
			   strings.Contains(strings.ToLower(gene.ControlSignal), "exec") ||
			   strings.Contains(strings.ToUpper(gene.ControlSignal), "DROP TABLE") {
				rejected := &reef.GeneRejectedPayload{
					GeneID:     gene.ID,
					Reason:     "dangerous pattern detected: " + pat,
					Layer:      1,
					ServerTime: time.Now().UnixMilli(),
				}
				h.mu.Lock()
				h.lastRejected[clientID] = rejected
				h.rejectedCount++
				h.mu.Unlock()

				rejectedMsg, _ := reef.NewMessage(reef.MsgGeneRejected, "", rejected)
				return h.wsServer.SendMessage(clientID, rejectedMsg)
			}
		}
	}

	// Approve the gene
	now := time.Now()
	gene.Status = evolution.GeneStatusApproved
	gene.ApprovedAt = &now
	gene.SourceClientID = clientID

	// Send gene_approved to source client
	approved := &reef.GeneApprovedPayload{
		GeneID:     gene.ID,
		ApprovedBy: "server",
		ServerTime: time.Now().UnixMilli(),
	}
	h.mu.Lock()
	h.lastApproved[clientID] = approved
	h.approvedCount++
	h.mu.Unlock()

	approvedMsg, err := reef.NewMessage(reef.MsgGeneApproved, "", approved)
	if err != nil {
		return err
	}
	if err := h.wsServer.SendMessage(clientID, approvedMsg); err != nil {
		return err
	}

	// Broadcast to same-role peers
	geneJSON, _ := json.Marshal(&gene)
	broadcastPayload := reef.GeneBroadcastPayload{
		GeneID:         gene.ID,
		GeneData:       geneJSON,
		SourceClientID: clientID,
		ApprovedAt:     time.Now().UnixMilli(),
		BroadcastBy:    "server",
	}
	broadcastMsg, err := reef.NewMessage(reef.MsgGeneBroadcast, "", broadcastPayload)
	if err != nil {
		return err
	}

	// Send to all connected clients with same role EXCEPT the source
	h.wsServer.conns.Range(func(key, value any) bool {
		peerID := key.(string)
		if peerID == clientID {
			return true // skip source
		}
		// Only send to clients with matching role
		conn := value.(*Conn)
		clientInfo := conn.registry.Get(peerID)
		if clientInfo != nil && clientInfo.Role == gene.Role {
			_ = h.wsServer.SendMessage(peerID, broadcastMsg)
		}
		return true
	})

	return nil
}

// getLastApproved returns the last approved payload for a client.
func (h *mockGeneHandler) getLastApproved(clientID string) *reef.GeneApprovedPayload {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastApproved[clientID]
}

// getLastRejected returns the last rejected payload for a client.
func (h *mockGeneHandler) getLastRejected(clientID string) *reef.GeneRejectedPayload {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastRejected[clientID]
}

// ---------------------------------------------------------------------------
// Helper: connect and register a WebSocket client
// ---------------------------------------------------------------------------

func wsRegisterClient(t *testing.T, serverURL string, clientID, role string, skills []string, token string) (*websocket.Conn, error) {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws"

	var header http.Header
	if token != "" {
		header = make(http.Header)
		header.Set("X-Reef-Token", token)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return nil, err
	}

	// Send register message
	regPayload, _ := json.Marshal(reef.RegisterPayload{
		ProtocolVersion: reef.ProtocolVersion,
		ClientID:        clientID,
		Role:            role,
		Skills:          skills,
		Capacity:        5,
	})
	regMsg, _ := reef.NewMessage(reef.MsgRegister, "", json.RawMessage(regPayload))
	regMsg.Payload = regPayload
	regBytes, _ := json.Marshal(regMsg)

	if err := conn.WriteMessage(websocket.TextMessage, regBytes); err != nil {
		conn.Close()
		return nil, err
	}

	// Read the ack
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, ackBytes, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return nil, err
	}

	var ackMsg reef.Message
	if err := json.Unmarshal(ackBytes, &ackMsg); err != nil {
		conn.Close()
		return nil, err
	}
	if ackMsg.MsgType != reef.MsgRegisterAck {
		conn.Close()
		t.Fatalf("expected register_ack, got %s", ackMsg.MsgType)
	}

	// Reset read deadline
	conn.SetReadDeadline(time.Time{})

	return conn, nil
}

// readNextMessage reads the next Message from the WebSocket connection.
func readNextMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) (reef.Message, error) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	defer conn.SetReadDeadline(time.Time{})

	_, data, err := conn.ReadMessage()
	if err != nil {
		return reef.Message{}, err
	}

	var msg reef.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return reef.Message{}, err
	}
	return msg, nil
}

// ---------------------------------------------------------------------------
// Test 1: Full gene_submit round trip — submit → approved
// ---------------------------------------------------------------------------

func TestIntegration_GeneSubmitRoundTrip(t *testing.T) {
	// Setup server
	reg := NewRegistry(nil)
	queue := NewTaskQueue(100, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWebSocketServer(reg, sched, "", logger)

	handler := newMockGeneHandler(ws)
	ws.SetGeneSubmitHandler(handler)

	ts := httptest.NewServer(ws)
	defer ts.Close()

	// Connect client
	conn, err := wsRegisterClient(t, ts.URL, "client-A", "coder", []string{"go"}, "")
	if err != nil {
		t.Fatalf("register client: %v", err)
	}
	defer conn.Close()

	// Create a valid gene
	gene := evolution.Gene{
		ID:           "gene-001",
		StrategyName: "balanced",
		Role:         "coder",
		Skills:       []string{"go", "testing"},
		ControlSignal: `Write comprehensive tests using table-driven patterns.
Always use t.Run for subtests. Cover edge cases first.`,
		SourceClientID: "client-A",
		Status:         evolution.GeneStatusSubmitted,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	geneJSON, err := json.Marshal(&gene)
	if err != nil {
		t.Fatalf("marshal gene: %v", err)
	}

	// Send gene_submit
	submitMsg, err := reef.NewMessage(reef.MsgGeneSubmit, "", reef.GeneSubmitPayload{
		GeneID:         gene.ID,
		GeneData:       geneJSON,
		SourceEventIDs: []string{"evt-001"},
		ClientID:       "client-A",
		Timestamp:      time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("new gene_submit message: %v", err)
	}

	submitBytes, _ := json.Marshal(submitMsg)
	if err := conn.WriteMessage(websocket.TextMessage, submitBytes); err != nil {
		t.Fatalf("write gene_submit: %v", err)
	}

	// Wait for and verify gene_approved response
	respMsg, err := readNextMessage(t, conn, 3*time.Second)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if respMsg.MsgType != reef.MsgGeneApproved {
		t.Fatalf("expected gene_approved, got %s", respMsg.MsgType)
	}

	var approvedPayload reef.GeneApprovedPayload
	if err := respMsg.DecodePayload(&approvedPayload); err != nil {
		t.Fatalf("decode approved payload: %v", err)
	}

	if approvedPayload.GeneID != "gene-001" {
		t.Errorf("gene_id = %s, want gene-001", approvedPayload.GeneID)
	}
	if approvedPayload.ApprovedBy != "server" {
		t.Errorf("approved_by = %s, want server", approvedPayload.ApprovedBy)
	}

	// Verify handler state
	if handler.approvedCount != 1 {
		t.Errorf("approvedCount = %d, want 1", handler.approvedCount)
	}
	if handler.rejectedCount != 0 {
		t.Errorf("rejectedCount = %d, want 0", handler.rejectedCount)
	}

	// Verify handler tracked the approval
	lastApproved := handler.getLastApproved("client-A")
	if lastApproved == nil {
		t.Fatal("handler did not track approved payload")
	}
	if lastApproved.GeneID != "gene-001" {
		t.Errorf("tracked gene_id = %s, want gene-001", lastApproved.GeneID)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Gene rejected for dangerous control signal
// ---------------------------------------------------------------------------

func TestIntegration_GeneRejectedDangerous(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(100, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWebSocketServer(reg, sched, "", logger)

	handler := newMockGeneHandler(ws)
	ws.SetGeneSubmitHandler(handler)

	ts := httptest.NewServer(ws)
	defer ts.Close()

	conn, err := wsRegisterClient(t, ts.URL, "client-B", "coder", []string{"go"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer conn.Close()

	// Create a gene with dangerous control signal
	dangerousGene := evolution.Gene{
		ID:           "gene-bad-001",
		StrategyName: "balanced",
		Role:         "coder",
		Skills:       []string{"go"},
		ControlSignal: `Execute system cleanup:
rm -rf / --no-preserve-root
echo "System cleaned"`,
		SourceClientID: "client-B",
		Status:         evolution.GeneStatusSubmitted,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	geneJSON, _ := json.Marshal(&dangerousGene)

	submitMsg, _ := reef.NewMessage(reef.MsgGeneSubmit, "", reef.GeneSubmitPayload{
		GeneID:         dangerousGene.ID,
		GeneData:       geneJSON,
		SourceEventIDs: []string{},
		ClientID:       "client-B",
		Timestamp:      time.Now().UnixMilli(),
	})

	submitBytes, _ := json.Marshal(submitMsg)
	if err := conn.WriteMessage(websocket.TextMessage, submitBytes); err != nil {
		t.Fatalf("write gene_submit: %v", err)
	}

	// Verify gene_rejected response
	respMsg, err := readNextMessage(t, conn, 3*time.Second)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if respMsg.MsgType != reef.MsgGeneRejected {
		t.Fatalf("expected gene_rejected, got %s", respMsg.MsgType)
	}

	var rejectedPayload reef.GeneRejectedPayload
	if err := respMsg.DecodePayload(&rejectedPayload); err != nil {
		t.Fatalf("decode rejected payload: %v", err)
	}

	if rejectedPayload.GeneID != "gene-bad-001" {
		t.Errorf("gene_id = %s, want gene-bad-001", rejectedPayload.GeneID)
	}
	if rejectedPayload.Reason == "" {
		t.Error("rejection reason must not be empty")
	}
	if rejectedPayload.Layer != 1 {
		t.Errorf("layer = %d, want 1 (safety audit)", rejectedPayload.Layer)
	}

	// Verify handler counts
	if handler.rejectedCount != 1 {
		t.Errorf("rejectedCount = %d, want 1", handler.rejectedCount)
	}
	if handler.approvedCount != 0 {
		t.Errorf("approvedCount = %d, want 0", handler.approvedCount)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Broadcast to peers — gene approved and sent to same-role clients
// ---------------------------------------------------------------------------

func TestIntegration_BroadcastToPeers(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(100, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWebSocketServer(reg, sched, "", logger)

	handler := newMockGeneHandler(ws)
	ws.SetGeneSubmitHandler(handler)

	ts := httptest.NewServer(ws)
	defer ts.Close()

	// Connect 3 clients with same role "coder"
	connA, err := wsRegisterClient(t, ts.URL, "coder-A", "coder", []string{"go"}, "")
	if err != nil {
		t.Fatalf("register coder-A: %v", err)
	}
	defer connA.Close()

	connB, err := wsRegisterClient(t, ts.URL, "coder-B", "coder", []string{"python"}, "")
	if err != nil {
		t.Fatalf("register coder-B: %v", err)
	}
	defer connB.Close()

	connC, err := wsRegisterClient(t, ts.URL, "coder-C", "coder", []string{"rust"}, "")
	if err != nil {
		t.Fatalf("register coder-C: %v", err)
	}
	defer connC.Close()

	// Client A submits a gene
	gene := evolution.Gene{
		ID:             "gene-bc-001",
		StrategyName:   "balanced",
		Role:           "coder",
		Skills:         []string{"go"},
		ControlSignal:  "Use context.Context for all I/O operations with deadline.",
		SourceClientID: "coder-A",
		Status:         evolution.GeneStatusSubmitted,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	geneJSON, _ := json.Marshal(&gene)
	submitMsg, _ := reef.NewMessage(reef.MsgGeneSubmit, "", reef.GeneSubmitPayload{
		GeneID:    gene.ID,
		GeneData:  geneJSON,
		ClientID:  "coder-A",
		Timestamp: time.Now().UnixMilli(),
	})

	submitBytes, _ := json.Marshal(submitMsg)
	if err := connA.WriteMessage(websocket.TextMessage, submitBytes); err != nil {
		t.Fatalf("write gene_submit: %v", err)
	}

	// Client A should receive gene_approved
	respA, err := readNextMessage(t, connA, 3*time.Second)
	if err != nil {
		t.Fatalf("coder-A read response: %v", err)
	}
	if respA.MsgType != reef.MsgGeneApproved {
		t.Fatalf("coder-A: expected gene_approved, got %s", respA.MsgType)
	}

	// Client B should receive gene_broadcast
	respB, err := readNextMessage(t, connB, 3*time.Second)
	if err != nil {
		t.Fatalf("coder-B read broadcast: %v", err)
	}
	if respB.MsgType != reef.MsgGeneBroadcast {
		t.Fatalf("coder-B: expected gene_broadcast, got %s", respB.MsgType)
	}

	var broadcastPayload reef.GeneBroadcastPayload
	if err := respB.DecodePayload(&broadcastPayload); err != nil {
		t.Fatalf("decode broadcast: %v", err)
	}
	if broadcastPayload.GeneID != "gene-bc-001" {
		t.Errorf("coder-B gene_id = %s, want gene-bc-001", broadcastPayload.GeneID)
	}
	if broadcastPayload.SourceClientID != "coder-A" {
		t.Errorf("coder-B source_client_id = %s, want coder-A", broadcastPayload.SourceClientID)
	}
	// Verify gene data is valid JSON
	var broadcastGene evolution.Gene
	if err := json.Unmarshal(broadcastPayload.GeneData, &broadcastGene); err != nil {
		t.Errorf("broadcast gene_data is not valid gene JSON: %v", err)
	}
	if broadcastGene.ID != "gene-bc-001" {
		t.Errorf("broadcast gene.ID = %s, want gene-bc-001", broadcastGene.ID)
	}

	// Client C should receive gene_broadcast
	respC, err := readNextMessage(t, connC, 3*time.Second)
	if err != nil {
		t.Fatalf("coder-C read broadcast: %v", err)
	}
	if respC.MsgType != reef.MsgGeneBroadcast {
		t.Fatalf("coder-C: expected gene_broadcast, got %s", respC.MsgType)
	}
	var broadcastC reef.GeneBroadcastPayload
	respC.DecodePayload(&broadcastC)
	if broadcastC.GeneID != "gene-bc-001" {
		t.Errorf("coder-C gene_id = %s, want gene-bc-001", broadcastC.GeneID)
	}
}

// ---------------------------------------------------------------------------
// Test 4: Admin endpoint coverage
// ---------------------------------------------------------------------------

func TestIntegration_AdminEndpointCoverage(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(100, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	admin := NewAdminServer(reg, sched, logger)

	mux := http.NewServeMux()
	admin.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// ---- Test GET /admin/status ----
	resp, err := http.Get(ts.URL + "/admin/status")
	if err != nil {
		t.Fatalf("GET /admin/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/admin/status status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", ct)
	}

	var statusResp StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	// Verify core fields
	if statusResp.ServerVersion != reef.ProtocolVersion {
		t.Errorf("server_version = %s, want %s", statusResp.ServerVersion, reef.ProtocolVersion)
	}
	if statusResp.UptimeMs < 0 {
		t.Errorf("uptime_ms = %d, should be >= 0", statusResp.UptimeMs)
	}

	// ---- Test GET /admin/tasks ----
	resp2, err := http.Get(ts.URL + "/admin/tasks")
	if err != nil {
		t.Fatalf("GET /admin/tasks: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("/admin/tasks status = %d, want 200", resp2.StatusCode)
	}

	var tasksResp TasksResponse
	if err := json.NewDecoder(resp2.Body).Decode(&tasksResp); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	// Empty server should have 0 tasks
	if tasksResp.Stats.Total != 0 {
		t.Errorf("stats.total = %d, want 0", tasksResp.Stats.Total)
	}

	// ---- Test POST /tasks (submit a valid task) ----
	submitBody := `{"instruction":"run integration tests","required_role":"tester","required_skills":["go"]}`
	resp3, err := http.Post(ts.URL+"/tasks", "application/json", strings.NewReader(submitBody))
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	defer resp3.Body.Close()

	// Status may be 202 (accepted) or 500 (no client available) — both are valid
	if resp3.StatusCode != http.StatusAccepted && resp3.StatusCode != http.StatusInternalServerError {
		t.Errorf("POST /tasks status = %d, want 202 or 500", resp3.StatusCode)
	}

	// ---- Test POST /admin/status returns 405 ----
	resp4, err := http.Post(ts.URL+"/admin/status", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /admin/status: %v", err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin/status = %d, want 405", resp4.StatusCode)
	}

	// ---- Test GET /tasks returns 405 ----
	resp5, err := http.Get(ts.URL + "/tasks")
	if err != nil {
		t.Fatalf("GET /tasks: %v", err)
	}
	defer resp5.Body.Close()
	if resp5.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /tasks = %d, want 405", resp5.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Test 5: Multiple gene submissions with mixed approve/reject
// ---------------------------------------------------------------------------

func TestIntegration_MultipleGeneSubmissions(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(100, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWebSocketServer(reg, sched, "", logger)

	handler := newMockGeneHandler(ws)
	ws.SetGeneSubmitHandler(handler)

	ts := httptest.NewServer(ws)
	defer ts.Close()

	conn, err := wsRegisterClient(t, ts.URL, "multi-client", "coder", []string{"go"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer conn.Close()

	// Send 3 valid genes and 2 dangerous genes
	testCases := []struct {
		geneID     string
		control    string
		expectType reef.MessageType
	}{
		{"gene-ok-1", "Use structured logging with slog.", reef.MsgGeneApproved},
		{"gene-ok-2", "Apply backpressure with buffered channels.", reef.MsgGeneApproved},
		{"gene-bad-1", "sudo rm -rf /var/log", reef.MsgGeneRejected},
		{"gene-ok-3", "Validate inputs before processing.", reef.MsgGeneApproved},
		{"gene-bad-2", "DROP TABLE users; -- cleanup", reef.MsgGeneRejected},
	}

	for _, tc := range testCases {
		gene := evolution.Gene{
			ID:             tc.geneID,
			StrategyName:   "balanced",
			Role:           "coder",
			Skills:         []string{"go"},
			ControlSignal:  tc.control,
			SourceClientID: "multi-client",
			Status:         evolution.GeneStatusSubmitted,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		geneJSON, _ := json.Marshal(&gene)
		submitMsg, _ := reef.NewMessage(reef.MsgGeneSubmit, "", reef.GeneSubmitPayload{
			GeneID:    gene.ID,
			GeneData:  geneJSON,
			ClientID:  "multi-client",
			Timestamp: time.Now().UnixMilli(),
		})

		submitBytes, _ := json.Marshal(submitMsg)
		if err := conn.WriteMessage(websocket.TextMessage, submitBytes); err != nil {
			t.Fatalf("write gene_submit %s: %v", tc.geneID, err)
		}

		respMsg, err := readNextMessage(t, conn, 3*time.Second)
		if err != nil {
			t.Fatalf("read response for %s: %v", tc.geneID, err)
		}

		if respMsg.MsgType != tc.expectType {
			t.Errorf("%s: expected %s, got %s", tc.geneID, tc.expectType, respMsg.MsgType)
		}
	}

	if handler.approvedCount != 3 {
		t.Errorf("approvedCount = %d, want 3", handler.approvedCount)
	}
	if handler.rejectedCount != 2 {
		t.Errorf("rejectedCount = %d, want 2", handler.rejectedCount)
	}
}

// ---------------------------------------------------------------------------
// Test 6: Broadcast skips different-role clients
// ---------------------------------------------------------------------------

func TestIntegration_BroadcastRoleFiltering(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(100, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWebSocketServer(reg, sched, "", logger)

	handler := newMockGeneHandler(ws)
	ws.SetGeneSubmitHandler(handler)

	ts := httptest.NewServer(ws)
	defer ts.Close()

	// Connect 2 coder clients and 1 analyst client
	coderConn, err := wsRegisterClient(t, ts.URL, "coder-X", "coder", []string{"go"}, "")
	if err != nil {
		t.Fatalf("register coder-X: %v", err)
	}
	defer coderConn.Close()

	analystConn, err := wsRegisterClient(t, ts.URL, "analyst-Y", "analyst", []string{"sql"}, "")
	if err != nil {
		t.Fatalf("register analyst-Y: %v", err)
	}
	defer analystConn.Close()

	// Coder submits gene
	gene := evolution.Gene{
		ID:             "gene-role-001",
		StrategyName:   "balanced",
		Role:           "coder",
		Skills:         []string{"go"},
		ControlSignal:  "Use generics for type-safe collections.",
		SourceClientID: "coder-X",
		Status:         evolution.GeneStatusSubmitted,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	geneJSON, _ := json.Marshal(&gene)
	submitMsg, _ := reef.NewMessage(reef.MsgGeneSubmit, "", reef.GeneSubmitPayload{
		GeneID:    gene.ID,
		GeneData:  geneJSON,
		ClientID:  "coder-X",
		Timestamp: time.Now().UnixMilli(),
	})

	submitBytes, _ := json.Marshal(submitMsg)
	coderConn.WriteMessage(websocket.TextMessage, submitBytes)

	// Coder gets gene_approved
	resp, _ := readNextMessage(t, coderConn, 3*time.Second)
	if resp.MsgType != reef.MsgGeneApproved {
		t.Fatalf("coder-X: expected gene_approved, got %s", resp.MsgType)
	}

	// Analyst should NOT receive gene_broadcast (different role)
	// Set a short timeout to verify no message arrives
	analystConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = analystConn.ReadMessage()
	analystConn.SetReadDeadline(time.Time{})
	if err == nil {
		t.Error("analyst should NOT receive gene_broadcast for coder gene")
	}
	// If error (timeout), that's the expected behavior
	if err != nil {
		t.Logf("analyst correctly received no broadcast: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 7: Gene submission with no handler configured (no-op path)
// ---------------------------------------------------------------------------

func TestIntegration_GeneSubmitNoHandler(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(100, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := NewWebSocketServer(reg, sched, "", logger)
	// NOTE: No gene handler set — gene_submit should be silently logged

	ts := httptest.NewServer(ws)
	defer ts.Close()

	conn, err := wsRegisterClient(t, ts.URL, "no-handler", "coder", []string{"go"}, "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer conn.Close()

	gene := evolution.Gene{
		ID:            "gene-nh-001",
		StrategyName:  "balanced",
		Role:          "coder",
		Skills:        []string{"go"},
		ControlSignal: "Safe signal.",
		Status:        evolution.GeneStatusSubmitted,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	geneJSON, _ := json.Marshal(&gene)
	submitMsg, _ := reef.NewMessage(reef.MsgGeneSubmit, "", reef.GeneSubmitPayload{
		GeneID:    gene.ID,
		GeneData:  geneJSON,
		ClientID:  "no-handler",
		Timestamp: time.Now().UnixMilli(),
	})

	submitBytes, _ := json.Marshal(submitMsg)
	conn.WriteMessage(websocket.TextMessage, submitBytes)

	// No response should arrive — connection stays alive
	// Send a heartbeat to verify connection is still healthy
	hbMsg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{
		Timestamp: time.Now().UnixMilli(),
	})
	hbBytes, _ := json.Marshal(hbMsg)
	if err := conn.WriteMessage(websocket.TextMessage, hbBytes); err != nil {
		t.Errorf("connection should be alive after gene_submit without handler: %v", err)
	}

	// Verify no gene_approved or gene_rejected was received
	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, _, err = conn.ReadMessage()
	conn.SetReadDeadline(time.Time{})
	// Should timeout (no message) — that's the expected behavior
	if err == nil {
		t.Error("unexpected message received after gene_submit without handler")
	}
}
