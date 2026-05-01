package server

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// Mock implementations for testing
// ---------------------------------------------------------------------------

type mockGeneStore struct {
	mu           sync.Mutex
	genes        map[string]*evolution.Gene
	insertErr    error
	updateErr    error
	getErr       error
	insertCalled bool
	updateCalled bool
}

func newMockGeneStore() *mockGeneStore {
	return &mockGeneStore{genes: make(map[string]*evolution.Gene)}
}

func (m *mockGeneStore) InsertGene(gene *evolution.Gene) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insertCalled = true
	m.genes[gene.ID] = gene
	return nil
}

func (m *mockGeneStore) UpdateGene(gene *evolution.Gene) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalled = true
	m.genes[gene.ID] = gene
	return nil
}

func (m *mockGeneStore) GetGene(geneID string) (*evolution.Gene, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.genes[geneID]
	if !ok {
		return nil, nil
	}
	return g, nil
}

func (m *mockGeneStore) CountApprovedGenes(role string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, g := range m.genes {
		if g.Role == role && g.Status == evolution.GeneStatusApproved {
			count++
		}
	}
	return count, nil
}

func (m *mockGeneStore) CountByStatus(status string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, g := range m.genes {
		if string(g.Status) == status {
			count++
		}
	}
	return count, nil
}

func (m *mockGeneStore) GetApprovedGenes(role string, limit int) ([]*evolution.Gene, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*evolution.Gene
	for _, g := range m.genes {
		if g.Role == role && g.Status == evolution.GeneStatusApproved {
			result = append(result, g)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

type mockConnManager struct {
	mu          sync.Mutex
	sentTo      map[string][]reef.Message
	sendErr     error
	sendCalled  bool
	broadcasted map[string][]reef.Message
}

func newMockConnManager() *mockConnManager {
	return &mockConnManager{
		sentTo:      make(map[string][]reef.Message),
		broadcasted: make(map[string][]reef.Message),
	}
}

func (m *mockConnManager) SendToClient(clientID string, msg reef.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalled = true
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentTo[clientID] = append(m.sentTo[clientID], msg)
	return nil
}

func (m *mockConnManager) BroadcastToRole(role string, msg reef.Message) []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasted[role] = append(m.broadcasted[role], msg)
	return nil
}

type mockGatekeeper struct {
	result *GateResult
	err    error
}

func (m *mockGatekeeper) Review(ctx context.Context, gene *evolution.Gene) (*GateResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

type mockBroadcaster struct {
	broadcasted []string
	err         error
}

func (m *mockBroadcaster) Broadcast(ctx context.Context, gene *evolution.Gene, sourceClientID string) error {
	if m.err != nil {
		return m.err
	}
	m.broadcasted = append(m.broadcasted, gene.ID)
	return nil
}

type mockMerger struct {
	checkedRoles []string
}

func (m *mockMerger) CheckAndMerge(ctx context.Context, role string) {
	m.checkedRoles = append(m.checkedRoles, role)
}

// ---------------------------------------------------------------------------
// Helper to create a valid gene payload
// ---------------------------------------------------------------------------

func validGene() evolution.Gene {
	return evolution.Gene{
		ID:             "gene-test-001",
		StrategyName:   "test_strategy",
		Role:           "coder",
		Skills:         []string{"go", "testing"},
		MatchCondition: "when_testing",
		ControlSignal:  "run tests and verify output",
		SourceClientID: "client-1",
		Version:        1,
		Status:         evolution.GeneStatusSubmitted,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
}

func makeGeneSubmitMsg(gene evolution.Gene) reef.Message {
	geneJSON, _ := json.Marshal(gene)
	msg, _ := reef.NewMessage(reef.MsgGeneSubmit, "", reef.GeneSubmitPayload{
		GeneID:         gene.ID,
		GeneData:       geneJSON,
		SourceEventIDs: gene.SourceEvents,
		ClientID:       gene.SourceClientID,
		Timestamp:      time.Now().UnixMilli(),
	})
	return msg
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestHubStatsConcurrency verifies atomic stats counters with concurrent access.
func TestHubStatsConcurrency(t *testing.T) {
	var stats HubStats
	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt64(&stats.TotalSubmitted, 1)
		}()
	}

	wg.Wait()

	if got := atomic.LoadInt64(&stats.TotalSubmitted); got != int64(n) {
		t.Errorf("TotalSubmitted = %d, want %d", got, n)
	}
}

// TestHubStatsSnapshot verifies Snapshot returns correct values.
func TestHubStatsSnapshot(t *testing.T) {
	var stats HubStats

	atomic.AddInt64(&stats.TotalSubmitted, 10)
	atomic.AddInt64(&stats.TotalApproved, 8)
	atomic.AddInt64(&stats.TotalRejected, 2)
	atomic.AddInt64(&stats.TotalBroadcasted, 7)
	stats.LastActivityTime = time.Now().UTC()

	snap := stats.Snapshot()

	if snap.TotalSubmitted != 10 {
		t.Errorf("snap TotalSubmitted = %d, want 10", snap.TotalSubmitted)
	}
	if snap.TotalApproved != 8 {
		t.Errorf("snap TotalApproved = %d, want 8", snap.TotalApproved)
	}
	if snap.TotalRejected != 2 {
		t.Errorf("snap TotalRejected = %d, want 2", snap.TotalRejected)
	}
	if snap.TotalBroadcasted != 7 {
		t.Errorf("snap TotalBroadcasted = %d, want 7", snap.TotalBroadcasted)
	}
	if !snap.LastActivityTime.Equal(stats.LastActivityTime) {
		t.Errorf("snap LastActivityTime = %v, want %v", snap.LastActivityTime, stats.LastActivityTime)
	}
}

// TestHubHandleSubmission_Approved tests the full approved flow:
// gatekeeper passes → gene saved → broadcaster called → merger called → approved message sent.
func TestHubHandleSubmission_Approved(t *testing.T) {
	store := newMockGeneStore()
	broadcaster := &mockBroadcaster{}
	gatekeeper := &mockGatekeeper{result: &GateResult{Passed: true}}
	merger := &mockMerger{}
	connMgr := newMockConnManager()

	hub := NewEvolutionHub(store, broadcaster, gatekeeper, merger, connMgr, DefaultHubConfig(), nil)

	gene := validGene()
	msg := makeGeneSubmitMsg(gene)

	err := hub.HandleGeneSubmission(context.Background(), msg, "client-1")
	if err != nil {
		t.Fatalf("HandleGeneSubmission returned error: %v", err)
	}

	// Verify gene was inserted
	if !store.insertCalled {
		t.Error("expected InsertGene to be called")
	}

	// Verify broadcaster was called
	if len(broadcaster.broadcasted) != 1 || broadcaster.broadcasted[0] != gene.ID {
		t.Errorf("broadcaster not called with correct gene: got %v", broadcaster.broadcasted)
	}

	// Verify merger was called
	if len(merger.checkedRoles) != 1 || merger.checkedRoles[0] != "coder" {
		t.Errorf("merger not called with correct role: got %v", merger.checkedRoles)
	}

	// Verify approved message was sent to client
	if !connMgr.sendCalled {
		t.Error("expected SendToClient to be called")
	}
	if msgs, ok := connMgr.sentTo["client-1"]; !ok || len(msgs) == 0 {
		t.Error("no message sent to client-1")
	} else if msgs[0].MsgType != reef.MsgGeneApproved {
		t.Errorf("expected MsgGeneApproved, got %s", msgs[0].MsgType)
	}

	// Verify stats
	stats := hub.GetStats()
	if stats.TotalSubmitted != 1 {
		t.Errorf("TotalSubmitted = %d, want 1", stats.TotalSubmitted)
	}
	if stats.TotalApproved != 1 {
		t.Errorf("TotalApproved = %d, want 1", stats.TotalApproved)
	}
}

// TestHubHandleSubmission_Rejected tests the rejected flow:
// gatekeeper rejects → gene saved (for audit) → rejected notification sent.
func TestHubHandleSubmission_Rejected(t *testing.T) {
	store := newMockGeneStore()
	broadcaster := &mockBroadcaster{}
	gatekeeper := &mockGatekeeper{result: &GateResult{
		Passed:        false,
		Reason:        "dangerous pattern detected",
		RejectedLayer: 2,
	}}
	merger := &mockMerger{}
	connMgr := newMockConnManager()

	hub := NewEvolutionHub(store, broadcaster, gatekeeper, merger, connMgr, DefaultHubConfig(), nil)

	gene := validGene()
	msg := makeGeneSubmitMsg(gene)

	err := hub.HandleGeneSubmission(context.Background(), msg, "client-1")
	if err != nil {
		t.Fatalf("HandleGeneSubmission returned error: %v", err)
	}

	// Verify gene was inserted (for audit)
	if !store.insertCalled {
		t.Error("expected InsertGene to be called for audit")
	}

	// Verify broadcaster was NOT called
	if len(broadcaster.broadcasted) != 0 {
		t.Error("broadcaster should not be called for rejected genes")
	}

	// Verify merger was NOT called
	if len(merger.checkedRoles) != 0 {
		t.Error("merger should not be called for rejected genes")
	}

	// Verify rejected message was sent
	if !connMgr.sendCalled {
		t.Error("expected SendToClient to be called for rejection")
	}
	msgs := connMgr.sentTo["client-1"]
	if len(msgs) == 0 || msgs[0].MsgType != reef.MsgGeneRejected {
		t.Errorf("expected MsgGeneRejected, got %v", msgs)
	}

	// Verify stats
	stats := hub.GetStats()
	if stats.TotalSubmitted != 1 {
		t.Errorf("TotalSubmitted = %d, want 1", stats.TotalSubmitted)
	}
	if stats.TotalRejected != 1 {
		t.Errorf("TotalRejected = %d, want 1", stats.TotalRejected)
	}
	if stats.TotalApproved != 0 {
		t.Errorf("TotalApproved = %d, want 0", stats.TotalApproved)
	}
}

// TestHubDisabled_NoOp verifies that when Enabled=false, all methods return nil immediately.
func TestHubDisabled_NoOp(t *testing.T) {
	store := newMockGeneStore()
	gatekeeper := &mockGatekeeper{result: &GateResult{Passed: true}}
	connMgr := newMockConnManager()

	config := HubConfig{Enabled: false}
	hub := NewEvolutionHub(store, nil, gatekeeper, nil, connMgr, config, nil)

	gene := validGene()
	msg := makeGeneSubmitMsg(gene)

	err := hub.HandleGeneSubmission(context.Background(), msg, "client-1")
	if err != nil {
		t.Fatalf("HandleGeneSubmission with disabled hub returned error: %v", err)
	}

	// No store operations should have occurred
	if store.insertCalled {
		t.Error("disabled hub should not insert genes")
	}

	// No messages sent
	if connMgr.sendCalled {
		t.Error("disabled hub should not send messages")
	}
}

// TestHubNilComponents_Error verifies that nil store/gatekeeper returns error when Enabled.
func TestHubNilComponents_Error(t *testing.T) {
	hub := NewEvolutionHub(nil, nil, nil, nil, nil, DefaultHubConfig(), nil)

	gene := validGene()
	msg := makeGeneSubmitMsg(gene)

	err := hub.HandleGeneSubmission(context.Background(), msg, "client-1")
	if err == nil {
		t.Fatal("expected error for nil components, got nil")
	}
}

// TestHubNilBroadcaster_SkipBroadcast verifies broadcast is skipped when broadcaster is nil.
func TestHubNilBroadcaster_SkipBroadcast(t *testing.T) {
	store := newMockGeneStore()
	gatekeeper := &mockGatekeeper{result: &GateResult{Passed: true}}
	connMgr := newMockConnManager()

	hub := NewEvolutionHub(store, nil, gatekeeper, nil, connMgr, DefaultHubConfig(), nil)

	gene := validGene()
	msg := makeGeneSubmitMsg(gene)

	err := hub.HandleGeneSubmission(context.Background(), msg, "client-1")
	if err != nil {
		t.Fatalf("HandleGeneSubmission returned error: %v", err)
	}

	// Gene should still be saved and approved
	if !store.insertCalled {
		t.Error("expected gene to be saved")
	}
	if !connMgr.sendCalled {
		t.Error("expected approved message to be sent")
	}
}

// TestRejectedNotify verifies that HandleGeneRejected sends the correct notification.
func TestRejectedNotify(t *testing.T) {
	store := newMockGeneStore()
	gatekeeper := &mockGatekeeper{result: &GateResult{
		Passed:        false,
		Reason:        "semantic violation",
		RejectedLayer: 2,
	}}
	connMgr := newMockConnManager()

	hub := NewEvolutionHub(store, nil, gatekeeper, nil, connMgr, DefaultHubConfig(), nil)

	gene := validGene()
	msg := makeGeneSubmitMsg(gene)

	err := hub.HandleGeneSubmission(context.Background(), msg, "client-1")
	if err != nil {
		t.Fatalf("HandleGeneSubmission returned error: %v", err)
	}

	msgs := connMgr.sentTo["client-1"]
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 message")
	}

	var lastMsg reef.Message
	for _, m := range msgs {
		if m.MsgType == reef.MsgGeneRejected {
			lastMsg = m
		}
	}
	if lastMsg.MsgType != reef.MsgGeneRejected {
		t.Fatalf("expected MsgGeneRejected, got %s", lastMsg.MsgType)
	}

	var payload reef.GeneRejectedPayload
	if err := lastMsg.DecodePayload(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.GeneID != gene.ID {
		t.Errorf("GeneID = %s, want %s", payload.GeneID, gene.ID)
	}
	if payload.Reason != "semantic violation" {
		t.Errorf("Reason = %s, want 'semantic violation'", payload.Reason)
	}
	if payload.Layer != 2 {
		t.Errorf("Layer = %d, want 2", payload.Layer)
	}
}

// TestDuplicateGene_Graceful tests that re-submission of same gene is handled gracefully.
func TestDuplicateGene_Graceful(t *testing.T) {
	store := newMockGeneStore()
	// Pre-insert the gene to simulate duplicate
	existing := validGene()
	existing.Status = evolution.GeneStatusApproved
	store.genes[existing.ID] = &existing

	gatekeeper := &mockGatekeeper{result: &GateResult{Passed: true}}
	connMgr := newMockConnManager()

	hub := NewEvolutionHub(store, &mockBroadcaster{}, gatekeeper, &mockMerger{}, connMgr, DefaultHubConfig(), nil)

	gene := validGene()
	msg := makeGeneSubmitMsg(gene)

	// Should not fail - UpdateGene should be called
	err := hub.HandleGeneSubmission(context.Background(), msg, "client-1")
	if err != nil {
		t.Fatalf("HandleGeneSubmission returned error: %v", err)
	}
	if !store.updateCalled {
		t.Error("expected UpdateGene to be called for existing gene")
	}
}

// TestGetStats_Initial returns zero stats for a new hub.
func TestGetStats_Initial(t *testing.T) {
	hub := NewEvolutionHub(newMockGeneStore(), nil, &mockGatekeeper{result: &GateResult{Passed: true}}, nil, nil, DefaultHubConfig(), nil)

	stats := hub.GetStats()
	if stats.TotalSubmitted != 0 || stats.TotalApproved != 0 || stats.TotalRejected != 0 || stats.TotalBroadcasted != 0 {
		t.Error("expected zero stats for new hub")
	}
}

// TestIsEnabled returns the hub's enabled state.
func TestIsEnabled(t *testing.T) {
	hub := NewEvolutionHub(newMockGeneStore(), nil, &mockGatekeeper{}, nil, nil, DefaultHubConfig(), nil)
	if !hub.IsEnabled() {
		t.Error("expected hub to be enabled by default")
	}

	disabledCfg := HubConfig{Enabled: false}
	hubDisabled := NewEvolutionHub(nil, nil, nil, nil, nil, disabledCfg, nil)
	if hubDisabled.IsEnabled() {
		t.Error("expected hub to be disabled")
	}
}

// TestHubStatsRace tests stats with race detector.
func TestHubStatsRace(t *testing.T) {
	store := newMockGeneStore()
	gatekeeper := &mockGatekeeper{result: &GateResult{Passed: true}}
	connMgr := newMockConnManager()

	hub := NewEvolutionHub(store, &mockBroadcaster{}, gatekeeper, &mockMerger{}, connMgr, DefaultHubConfig(), nil)

	var wg sync.WaitGroup
	n := 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			gene := validGene()
			gene.ID = "gene-race-" + string(rune('a'+idx%26)) + string(rune('0'+idx/26))
			msg := makeGeneSubmitMsg(gene)
			_ = hub.HandleGeneSubmission(context.Background(), msg, "client-1")
		}(i)
	}

	wg.Wait()

	stats := hub.GetStats()
	if stats.TotalSubmitted != int64(n) {
		t.Errorf("TotalSubmitted = %d, want %d", stats.TotalSubmitted, n)
	}
}
