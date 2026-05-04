package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// Mock implementations for broadcaster testing
// ---------------------------------------------------------------------------

// bcMockRoleFinder implements RoleFinder for tests.
type bcMockRoleFinder struct {
	mu      sync.Mutex
	clients map[string]ClientInfo
}

func newBCMockRoleFinder() *bcMockRoleFinder {
	return &bcMockRoleFinder{
		clients: make(map[string]ClientInfo),
	}
}

func (m *bcMockRoleFinder) addClient(clientID, role string, skills []string, isOnline bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[clientID] = ClientInfo{
		ID:       clientID,
		Role:     role,
		Skills:   skills,
		IsOnline: isOnline,
	}
}

func (m *bcMockRoleFinder) setOnline(clientID string, isOnline bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[clientID]; ok {
		c.IsOnline = isOnline
		m.clients[clientID] = c
	}
}

func (m *bcMockRoleFinder) FindByRole(role string) []ClientInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ClientInfo
	for _, c := range m.clients {
		if c.Role == role {
			out = append(out, c)
		}
	}
	return out
}

func (m *bcMockRoleFinder) FindBySkills(skills []string) []ClientInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(skills) == 0 {
		return nil
	}
	skillSet := make(map[string]bool, len(skills))
	for _, s := range skills {
		skillSet[s] = true
	}
	var out []ClientInfo
	for _, c := range m.clients {
		for _, cs := range c.Skills {
			if skillSet[cs] {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// bcMockCM implements ConnManager for broadcaster tests.
type bcMockCM struct {
	mu       sync.Mutex
	messages []bcConnMsg
	err      error
}

type bcConnMsg struct {
	ClientID string
	Msg      reef.Message
}

func newBCMockCM() *bcMockCM {
	return &bcMockCM{}
}

func (m *bcMockCM) SendToClient(clientID string, msg reef.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, bcConnMsg{ClientID: clientID, Msg: msg})
	return nil
}

func (m *bcMockCM) BroadcastToRole(role string, msg reef.Message) []error {
	return nil
}

func (m *bcMockCM) sentMessages() []bcConnMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]bcConnMsg, len(m.messages))
	copy(out, m.messages)
	return out
}

func (m *bcMockCM) messagesForClient(clientID string) []bcConnMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []bcConnMsg
	for _, msg := range m.messages {
		if msg.ClientID == clientID {
			out = append(out, msg)
		}
	}
	return out
}

// bcMockStore implements GeneStore for broadcaster tests.
type bcMockStore struct {
	mu    sync.Mutex
	genes map[string]*evolution.Gene
}

func newBCMockStore() *bcMockStore {
	return &bcMockStore{
		genes: make(map[string]*evolution.Gene),
	}
}

func (m *bcMockStore) addGene(gene *evolution.Gene) {
	m.mu.Lock()
	defer m.mu.Unlock()
	geneCopy := *gene
	m.genes[gene.ID] = &geneCopy
}

func (m *bcMockStore) InsertGene(gene *evolution.Gene) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.genes[gene.ID] = gene
	return nil
}

func (m *bcMockStore) UpdateGene(gene *evolution.Gene) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.genes[gene.ID] = gene
	return nil
}

func (m *bcMockStore) GetGene(geneID string) (*evolution.Gene, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.genes[geneID]
	if !ok {
		return nil, nil
	}
	return g, nil
}

func (m *bcMockStore) CountApprovedGenes(role string) (int, error) {
	return 0, nil
}

func (m *bcMockStore) CountByStatus(status string) (int, error) {
	return 0, nil
}

func (m *bcMockStore) GetApprovedGenes(role string, limit int) ([]*evolution.Gene, error) {
	return nil, nil
}

// makeBroadcasterGene creates a clean gene for testing.
func makeBroadcasterGene(id, role string) *evolution.Gene {
	return &evolution.Gene{
		ID:              id,
		StrategyName:    string(evolution.StrategyBalanced),
		Role:            role,
		Skills:          []string{"test"},
		MatchCondition:  "on_error",
		ControlSignal:   "run safety checks and validate results",
		FailureWarnings: []string{},
		SourceEvents:    []string{"evt-001"},
		SourceClientID:  "source-client",
		Version:         1,
		Status:          evolution.GeneStatusApproved,
	}
}

// =========================================================================
// Task 1: FindByRole returns 0 clients → no-op
// =========================================================================

func TestBroadcaster_FindByRoleZero_NoOp(t *testing.T) {
	rf := newBCMockRoleFinder()
	cm := newBCMockCM()
	store := newBCMockStore()

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	gene := makeBroadcasterGene("gene-1", "nonexistent-role")

	err := b.Broadcast(context.Background(), gene, "source-client")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cm.sentMessages()) != 0 {
		t.Errorf("expected 0 sends for no matching clients, got %d", len(cm.sentMessages()))
	}
}

// =========================================================================
// Task 2: Broadcast sends to matching online clients, excludes source
// =========================================================================

func TestBroadcaster_SendsToMatchingClients(t *testing.T) {
	rf := newBCMockRoleFinder()
	rf.addClient("client-src", "tester", nil, true)
	rf.addClient("client-a", "tester", nil, true)
	rf.addClient("client-b", "tester", nil, true)
	rf.addClient("client-other", "other-role", nil, true)

	cm := newBCMockCM()
	store := newBCMockStore()

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	gene := makeBroadcasterGene("gene-1", "tester")

	err := b.Broadcast(context.Background(), gene, "client-src")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := cm.sentMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	clientIDs := make(map[string]bool)
	for _, m := range msgs {
		clientIDs[m.ClientID] = true
		if m.Msg.MsgType != reef.MsgGeneBroadcast {
			t.Errorf("expected MsgGeneBroadcast, got %s", m.Msg.MsgType)
		}
	}

	if clientIDs["client-src"] {
		t.Error("source client should NOT receive broadcast")
	}
	if !clientIDs["client-a"] {
		t.Error("client-a should receive broadcast")
	}
	if !clientIDs["client-b"] {
		t.Error("client-b should receive broadcast")
	}
	if clientIDs["client-other"] {
		t.Error("client-other (different role) should NOT receive broadcast")
	}
}

func TestBroadcaster_EmptyRole_Error(t *testing.T) {
	rf := newBCMockRoleFinder()
	cm := newBCMockCM()
	store := newBCMockStore()

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	gene := makeBroadcasterGene("gene-1", "")
	gene.Role = ""

	err := b.Broadcast(context.Background(), gene, "source-client")
	if err == nil {
		t.Error("expected error for empty role")
	}
}

func TestBroadcaster_OfflineClient_PendingSync(t *testing.T) {
	rf := newBCMockRoleFinder()
	rf.addClient("client-online", "tester", nil, true)
	rf.addClient("client-offline", "tester", nil, false)

	cm := newBCMockCM()
	store := newBCMockStore()

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	gene := makeBroadcasterGene("gene-1", "tester")

	err := b.Broadcast(context.Background(), gene, "source-client")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cm.messagesForClient("client-online")) != 1 {
		t.Errorf("expected 1 message for online client, got %d", len(cm.messagesForClient("client-online")))
	}

	if b.PendingSyncCount("client-offline") != 1 {
		t.Errorf("expected 1 pending gene for offline client, got %d", b.PendingSyncCount("client-offline"))
	}
}

func TestBroadcaster_SourceClientExcluded(t *testing.T) {
	rf := newBCMockRoleFinder()
	rf.addClient("myself", "tester", nil, true)
	rf.addClient("other", "tester", nil, true)

	cm := newBCMockCM()
	store := newBCMockStore()

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	gene := makeBroadcasterGene("gene-1", "tester")

	err := b.Broadcast(context.Background(), gene, "myself")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cm.messagesForClient("myself")) != 0 {
		t.Error("source client should not receive its own broadcast")
	}
	if len(cm.messagesForClient("other")) != 1 {
		t.Error("other client should receive broadcast")
	}
}

// =========================================================================
// Task 2: MaxConcurrentSends and context cancellation
// =========================================================================

func TestBroadcaster_MaxConcurrentSends(t *testing.T) {
	rf := newBCMockRoleFinder()
	for i := 0; i < 10; i++ {
		rf.addClient(fmt.Sprintf("client-%d", i), "tester", nil, true)
	}

	cm := newBCMockCM()
	store := newBCMockStore()

	cfg := DefaultBroadcasterConfig()
	cfg.MaxConcurrentSends = 3
	cfg.SendTimeout = 1 * time.Second

	b := NewBroadcaster(store, cm, rf, cfg, nil)
	gene := makeBroadcasterGene("gene-1", "tester")

	err := b.Broadcast(context.Background(), gene, "source-client")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cm.sentMessages()) != 10 {
		t.Errorf("expected 10 messages, got %d", len(cm.sentMessages()))
	}
}

func TestBroadcaster_ContextCancelled(t *testing.T) {
	rf := newBCMockRoleFinder()
	for i := 0; i < 5; i++ {
		rf.addClient(fmt.Sprintf("client-%d", i), "tester", nil, true)
	}

	cm := newBCMockCM()
	store := newBCMockStore()

	cfg := DefaultBroadcasterConfig()
	cfg.MaxConcurrentSends = 1
	cfg.SendTimeout = 10 * time.Second

	b := NewBroadcaster(store, cm, rf, cfg, nil)
	gene := makeBroadcasterGene("gene-1", "tester")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := b.Broadcast(ctx, gene, "source-client")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cm.sentMessages()) != 0 {
		t.Errorf("expected 0 messages with cancelled context, got %d", len(cm.sentMessages()))
	}
}

// =========================================================================
// Task 3: pendingSync and resync on reconnect
// =========================================================================

func TestBroadcasterResync_ClientReconnects_ReceivesMissedGene(t *testing.T) {
	rf := newBCMockRoleFinder()
	rf.addClient("client-x", "tester", nil, true)
	rf.addClient("client-off", "tester", nil, false)

	cm := newBCMockCM()
	store := newBCMockStore()

	gene := makeBroadcasterGene("gene-1", "tester")
	gene.Status = evolution.GeneStatusApproved
	store.addGene(gene)

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	b.RecordOffline("client-off", "gene-1")

	b.OnClientReconnect("client-off", "tester")

	msgs := cm.messagesForClient("client-off")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 resync message, got %d", len(msgs))
	}
	if msgs[0].Msg.MsgType != reef.MsgGeneBroadcast {
		t.Errorf("expected MsgGeneBroadcast, got %s", msgs[0].Msg.MsgType)
	}
}

func TestBroadcasterResync_GeneRetired_Skipped(t *testing.T) {
	rf := newBCMockRoleFinder()
	cm := newBCMockCM()
	store := newBCMockStore()

	gene := makeBroadcasterGene("gene-1", "tester")
	gene.Status = evolution.GeneStatusRetired
	store.addGene(gene)

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	b.RecordOffline("client-retired", "gene-1")

	b.OnClientReconnect("client-retired", "tester")

	if len(cm.messagesForClient("client-retired")) != 0 {
		t.Error("retired gene should not be resynced")
	}
}

func TestBroadcasterResync_GeneRejected_Skipped(t *testing.T) {
	rf := newBCMockRoleFinder()
	cm := newBCMockCM()
	store := newBCMockStore()

	gene := makeBroadcasterGene("gene-1", "tester")
	gene.Status = evolution.GeneStatusRejected
	store.addGene(gene)

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	b.RecordOffline("client-rej", "gene-1")

	b.OnClientReconnect("client-rej", "tester")

	if len(cm.messagesForClient("client-rej")) != 0 {
		t.Error("rejected gene should not be resynced")
	}
}

func TestBroadcasterResync_EmptyPendingSync_NoMessages(t *testing.T) {
	rf := newBCMockRoleFinder()
	cm := newBCMockCM()
	store := newBCMockStore()

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	b.OnClientReconnect("client-empty", "tester")

	if len(cm.sentMessages()) != 0 {
		t.Error("expected no messages for empty pendingSync")
	}
}

func TestBroadcasterResync_ResyncDisabled_Skipped(t *testing.T) {
	rf := newBCMockRoleFinder()
	cm := newBCMockCM()
	store := newBCMockStore()

	gene := makeBroadcasterGene("gene-1", "tester")
	gene.Status = evolution.GeneStatusApproved
	store.addGene(gene)

	cfg := DefaultBroadcasterConfig()
	cfg.ResyncOnReconnect = false

	b := NewBroadcaster(store, cm, rf, cfg, nil)
	b.RecordOffline("client-noresync", "gene-1")

	b.OnClientReconnect("client-noresync", "tester")

	if len(cm.sentMessages()) != 0 {
		t.Error("resync disabled should not send messages")
	}
}

// =========================================================================
// Task 3: pendingSync cap
// =========================================================================

func TestBroadcaster_PendingSyncCap(t *testing.T) {
	rf := newBCMockRoleFinder()
	cm := newBCMockCM()
	store := newBCMockStore()

	cfg := DefaultBroadcasterConfig()
	cfg.MaxPendingPerClient = 3

	b := NewBroadcaster(store, cm, rf, cfg, nil)

	for i := 0; i < 5; i++ {
		b.RecordOffline("client-cap", fmt.Sprintf("gene-%d", i))
	}

	ids := b.GetPendingSyncIDs("client-cap")
	if len(ids) != 3 {
		t.Errorf("expected 3 genes (capped), got %d: %v", len(ids), ids)
	}
	if ids[0] != "gene-2" {
		t.Errorf("expected gene-2 as oldest remaining, got %s", ids[0])
	}
}

func TestBroadcaster_PendingSyncNoDuplicates(t *testing.T) {
	rf := newBCMockRoleFinder()
	cm := newBCMockCM()
	store := newBCMockStore()

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)

	b.RecordOffline("client-dup", "gene-1")
	b.RecordOffline("client-dup", "gene-1")
	b.RecordOffline("client-dup", "gene-2")

	ids := b.GetPendingSyncIDs("client-dup")
	if len(ids) != 2 {
		t.Errorf("expected 2 (no dup), got %d: %v", len(ids), ids)
	}
}

// =========================================================================
// Task 5: Integration test — full broadcast flow
// =========================================================================

func TestBroadcastEndToEnd(t *testing.T) {
	rf := newBCMockRoleFinder()
	rf.addClient("source-cli", "worker", nil, true)
	rf.addClient("target-a", "worker", nil, true)
	rf.addClient("target-b", "worker", nil, true)

	cm := newBCMockCM()
	store := newBCMockStore()

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	gene := makeBroadcasterGene("gene-e2e", "worker")

	// Step 1: Broadcast.
	err := b.Broadcast(context.Background(), gene, "source-cli")
	if err != nil {
		t.Fatalf("broadcast failed: %v", err)
	}

	msgs := cm.sentMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (target-a, target-b), got %d", len(msgs))
	}

	for _, m := range msgs {
		if m.ClientID == "source-cli" {
			t.Error("source client received broadcast")
		}
		if m.Msg.MsgType != reef.MsgGeneBroadcast {
			t.Errorf("wrong msg type for %s: %s", m.ClientID, m.Msg.MsgType)
		}

		var payload reef.GeneBroadcastPayload
		if err := m.Msg.DecodePayload(&payload); err != nil {
			t.Errorf("decode payload for %s: %v", m.ClientID, err)
		}
		if payload.GeneID != "gene-e2e" {
			t.Errorf("wrong GeneID for %s: %s", m.ClientID, payload.GeneID)
		}
		if payload.SourceClientID != "source-cli" {
			t.Errorf("wrong SourceClientID for %s: %s", m.ClientID, payload.SourceClientID)
		}
		if payload.BroadcastBy != "server" {
			t.Errorf("wrong BroadcastBy for %s: %s", m.ClientID, payload.BroadcastBy)
		}

		var decodedGene evolution.Gene
		if err := json.Unmarshal(payload.GeneData, &decodedGene); err != nil {
			t.Errorf("GeneData is not valid gene JSON for %s: %v", m.ClientID, err)
		}
		if decodedGene.ID != "gene-e2e" {
			t.Errorf("decoded gene ID mismatch for %s", m.ClientID)
		}
	}

	// Step 2: Re-broadcast with one client offline.
	cm2 := newBCMockCM()
	b.connManager = cm2

	rf.setOnline("target-a", false)
	gene2 := makeBroadcasterGene("gene-e2e-2", "worker")

	err = b.Broadcast(context.Background(), gene2, "source-cli")
	if err != nil {
		t.Fatalf("broadcast 2 failed: %v", err)
	}

	if len(cm2.messagesForClient("target-b")) != 1 {
		t.Errorf("target-b should get message, got %d", len(cm2.messagesForClient("target-b")))
	}
	if b.PendingSyncCount("target-a") != 1 {
		t.Errorf("target-a should have 1 pending gene, got %d", b.PendingSyncCount("target-a"))
	}

	// Step 3: Simulate reconnect of target-a, verify resync.
	cm3 := newBCMockCM()
	b.connManager = cm3
	store.addGene(gene2)

	b.OnClientReconnect("target-a", "worker")

	msgs3 := cm3.messagesForClient("target-a")
	if len(msgs3) != 1 {
		t.Fatalf("expected 1 resync message for target-a, got %d", len(msgs3))
	}
}

func TestBroadcastEndToEnd_SourceExcluded(t *testing.T) {
	rf := newBCMockRoleFinder()
	rf.addClient("self", "worker", nil, true)
	rf.addClient("peer", "worker", nil, true)

	cm := newBCMockCM()
	store := newBCMockStore()

	b := NewBroadcaster(store, cm, rf, DefaultBroadcasterConfig(), nil)
	gene := makeBroadcasterGene("gene-excl", "worker")

	err := b.Broadcast(context.Background(), gene, "self")
	if err != nil {
		t.Fatalf("broadcast failed: %v", err)
	}

	msgs := cm.sentMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (peer only), got %d", len(msgs))
	}
	if msgs[0].ClientID != "peer" {
		t.Errorf("expected message to peer, got %s", msgs[0].ClientID)
	}
}

// =========================================================================
// Test helpers
// =========================================================================

func TestBroadcaster_PendingSyncCount_Zero(t *testing.T) {
	b := NewBroadcaster(nil, nil, nil, DefaultBroadcasterConfig(), nil)
	if b.PendingSyncCount("nonexistent") != 0 {
		t.Error("expected 0 for nonexistent client")
	}
}

func TestBroadcaster_GetPendingSyncIDs_Empty(t *testing.T) {
	b := NewBroadcaster(nil, nil, nil, DefaultBroadcasterConfig(), nil)
	ids := b.GetPendingSyncIDs("nonexistent")
	if len(ids) != 0 {
		t.Error("expected empty slice for nonexistent client")
	}
}
