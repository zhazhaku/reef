// Package server implements the server-side evolution engine components.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// RoleFinder — abstracts client lookup to avoid import cycles
// ---------------------------------------------------------------------------

// RoleFinder abstracts the Registry's role-based client lookup.
// Implementations typically wrap server.Registry.
type RoleFinder interface {
	// FindByRole returns all clients with the given role.
	FindByRole(role string) []ClientInfo
	// FindBySkills returns all clients matching the given skills.
	FindBySkills(skills []string) []ClientInfo
}

// ClientInfo is a simplified client view used by the broadcaster.
// It decouples the evolution/server package from server.ClientInfo.
type ClientInfo struct {
	ID       string
	Role     string
	Skills   []string
	IsOnline bool
}

// ---------------------------------------------------------------------------
// BroadcasterConfig
// ---------------------------------------------------------------------------

// BroadcasterConfig configures the GeneBroadcaster.
type BroadcasterConfig struct {
	// SendTimeout is the maximum time to wait for a single client send.
	// Default: 5s.
	SendTimeout time.Duration

	// MaxConcurrentSends limits the number of concurrent goroutine sends.
	// 0 means unlimited. Default: 20.
	MaxConcurrentSends int

	// ResyncOnReconnect enables pending gene sync when a client reconnects.
	// Default: true.
	ResyncOnReconnect bool

	// MaxPendingPerClient caps the number of pending gene IDs stored per
	// offline client. Default: 50.
	MaxPendingPerClient int
}

// DefaultBroadcasterConfig returns sensible defaults.
func DefaultBroadcasterConfig() BroadcasterConfig {
	return BroadcasterConfig{
		SendTimeout:         5 * time.Second,
		MaxConcurrentSends:  20,
		ResyncOnReconnect:   true,
		MaxPendingPerClient: 50,
	}
}

// setDefaults applies defaults for zero-valued fields.
func (c *BroadcasterConfig) setDefaults() {
	if c.SendTimeout <= 0 {
		c.SendTimeout = 5 * time.Second
	}
	if c.MaxConcurrentSends < 0 {
		c.MaxConcurrentSends = 20
	}
	if c.MaxPendingPerClient <= 0 {
		c.MaxPendingPerClient = 50
	}
}

// ---------------------------------------------------------------------------
// GeneBroadcaster — concrete implementation
// ---------------------------------------------------------------------------

// Broadcaster implements GeneBroadcaster. It distributes approved genes
// to all online clients with matching roles using concurrent goroutine-per-client
// sends. It also tracks offline clients and resyncs missed genes on reconnect.
type Broadcaster struct {
	store       GeneStore
	connManager ConnManager
	registry    RoleFinder
	config      BroadcasterConfig
	logger      *slog.Logger
	mu          sync.Mutex
	pendingSync map[string][]string // clientID → []geneID
}

// NewBroadcaster creates a new Broadcaster that implements GeneBroadcaster.
func NewBroadcaster(
	store GeneStore,
	connManager ConnManager,
	registry RoleFinder,
	config BroadcasterConfig,
	logger *slog.Logger,
) *Broadcaster {
	config.setDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Broadcaster{
		store:       store,
		connManager: connManager,
		registry:    registry,
		config:      config,
		logger:      logger,
		pendingSync: make(map[string][]string),
	}
}

// Broadcast sends an approved gene to all online clients with matching role.
// It implements the GeneBroadcaster interface used by EvolutionHub.
func (b *Broadcaster) Broadcast(ctx context.Context, gene *evolution.Gene, sourceClientID string) error {
	// Validate: empty role is invalid.
	if gene.Role == "" {
		b.logger.Error("cannot broadcast gene with empty role",
			slog.String("gene_id", gene.ID))
		return fmt.Errorf("gene %s has empty role: cannot broadcast", gene.ID)
	}

	// Find target clients matching the gene's role.
	clients := b.registry.FindByRole(gene.Role)

	// No matching clients: no-op, log at DEBUG.
	if len(clients) == 0 {
		b.logger.Debug("no clients match gene role, broadcast no-op",
			slog.String("gene_id", gene.ID),
			slog.String("role", gene.Role))
		return nil
	}

	// Serialize gene to JSON.
	geneJSON, err := json.Marshal(gene)
	if err != nil {
		return fmt.Errorf("marshal gene %s: %w", gene.ID, err)
	}

	// Build broadcast message.
	broadcastMsg, err := reef.NewMessage(reef.MsgGeneBroadcast, "", reef.GeneBroadcastPayload{
		GeneID:         gene.ID,
		GeneData:       geneJSON,
		SourceClientID: sourceClientID,
		ApprovedAt:     time.Now().UnixMilli(),
		BroadcastBy:    "server",
	})
	if err != nil {
		return fmt.Errorf("build gene_broadcast message: %w", err)
	}

	// Filter out offline clients and the source client.
	var onlineTargets []ClientInfo
	var offlineTargets []ClientInfo
	for _, c := range clients {
		// Skip the source client.
		if c.ID == sourceClientID {
			continue
		}
		if c.IsOnline {
			onlineTargets = append(onlineTargets, c)
		} else {
			offlineTargets = append(offlineTargets, c)
		}
	}

	// Record gene for offline clients.
	for _, c := range offlineTargets {
		b.RecordOffline(c.ID, gene.ID)
	}

	// No online targets: done.
	if len(onlineTargets) == 0 {
		b.logger.Debug("no online targets for broadcast",
			slog.String("gene_id", gene.ID),
			slog.String("role", gene.Role))
		return nil
	}

	// Semaphore for concurrency control.
	var sem chan struct{}
	if b.config.MaxConcurrentSends > 0 {
		sem = make(chan struct{}, b.config.MaxConcurrentSends)
	}

	// Collect errors from goroutines.
	errCh := make(chan error, len(onlineTargets))

	for _, c := range onlineTargets {
		// Check context cancellation before launching more goroutines.
		select {
		case <-ctx.Done():
			b.logger.Warn("broadcast context cancelled, stopping new sends",
				slog.String("gene_id", gene.ID),
				slog.Int("remaining_targets", len(onlineTargets)))
			return nil
		default:
		}

		// Acquire semaphore if limited.
		if sem != nil {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
		}

		clientID := c.ID
		go func() {
			defer func() {
				if sem != nil {
					<-sem
				}
			}()
			errCh <- b.sendToClient(ctx, clientID, broadcastMsg, gene.ID)
		}()
	}

	// Collect results (best-effort: log failures, return nil).
	for i := 0; i < len(onlineTargets); i++ {
		if err := <-errCh; err != nil {
			b.logger.Warn("broadcast to client failed",
				slog.String("gene_id", gene.ID),
				slog.String("error", err.Error()))
		}
	}

	return nil
}

// sendToClient sends a message to a single client with a timeout.
func (b *Broadcaster) sendToClient(ctx context.Context, clientID string, msg reef.Message, geneID string) error {
	sendCtx, cancel := context.WithTimeout(ctx, b.config.SendTimeout)
	defer cancel()

	// Use a goroutine so we can select on both the send and context.
	done := make(chan error, 1)
	go func() {
		done <- b.connManager.SendToClient(clientID, msg)
	}()

	select {
	case err := <-done:
		if err != nil {
			b.RecordOffline(clientID, geneID)
			return fmt.Errorf("client %s: %w", clientID, err)
		}
		return nil
	case <-sendCtx.Done():
		b.RecordOffline(clientID, geneID)
		return fmt.Errorf("client %s: send timeout", clientID)
	}
}

// OnClientReconnect resyncs missed genes to a client that has reconnected.
// It retrieves all pending gene IDs for the client, fetches each gene from
// the store, and sends gene_broadcast messages for still-approved genes.
func (b *Broadcaster) OnClientReconnect(clientID string, role string) {
	if !b.config.ResyncOnReconnect {
		return
	}

	b.mu.Lock()
	missedGeneIDs := b.pendingSync[clientID]
	delete(b.pendingSync, clientID)
	b.mu.Unlock()

	if len(missedGeneIDs) == 0 {
		return
	}

	b.logger.Info("resyncing missed genes on reconnect",
		slog.String("client_id", clientID),
		slog.Int("count", len(missedGeneIDs)))

	for _, geneID := range missedGeneIDs {
		gene, err := b.store.GetGene(geneID)
		if err != nil {
			b.logger.Warn("resync: failed to fetch gene",
				slog.String("gene_id", geneID),
				slog.String("error", err.Error()))
			continue
		}
		if gene == nil {
			continue
		}

		// Only resync if gene is still approved.
		if gene.Status != evolution.GeneStatusApproved {
			b.logger.Debug("resync: skipping non-approved gene",
				slog.String("gene_id", geneID),
				slog.String("status", string(gene.Status)))
			continue
		}

		geneJSON, err := json.Marshal(gene)
		if err != nil {
			b.logger.Warn("resync: failed to marshal gene",
				slog.String("gene_id", geneID),
				slog.String("error", err.Error()))
			continue
		}

		msg, err := reef.NewMessage(reef.MsgGeneBroadcast, "", reef.GeneBroadcastPayload{
			GeneID:         gene.ID,
			GeneData:       geneJSON,
			SourceClientID: gene.SourceClientID,
			ApprovedAt:     time.Now().UnixMilli(),
			BroadcastBy:    "server",
		})
		if err != nil {
			b.logger.Warn("resync: failed to build message",
				slog.String("gene_id", geneID),
				slog.String("error", err.Error()))
			continue
		}

		if err := b.connManager.SendToClient(clientID, msg); err != nil {
			b.logger.Warn("resync: send failed",
				slog.String("client_id", clientID),
				slog.String("gene_id", geneID),
				slog.String("error", err.Error()))
			// Re-queue for next reconnect attempt.
			b.mu.Lock()
			b.pendingSync[clientID] = append(b.pendingSync[clientID], geneID)
			b.mu.Unlock()
			return // Stop on first send failure to maintain ordering.
		}
	}
}

// RecordOffline records a gene ID for a client that was offline during broadcast.
// Caps pending entries at MaxPendingPerClient; drops oldest if exceeded.
func (b *Broadcaster) RecordOffline(clientID string, geneID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entries := b.pendingSync[clientID]

	// Check for duplicates.
	for _, id := range entries {
		if id == geneID {
			return
		}
	}

	entries = append(entries, geneID)

	// Enforce cap.
	if len(entries) > b.config.MaxPendingPerClient {
		removed := entries[0]
		entries = entries[1:]
		b.logger.Warn("pendingSync cap exceeded, dropped oldest gene",
			slog.String("client_id", clientID),
			slog.String("dropped_gene_id", removed),
			slog.Int("total_remaining", len(entries)))
	}

	b.pendingSync[clientID] = entries
}

// PendingSyncCount returns the number of pending gene IDs for a client.
// Useful for testing and monitoring.
func (b *Broadcaster) PendingSyncCount(clientID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pendingSync[clientID])
}

// GetPendingSyncIDs returns the pending gene IDs for a client.
// Useful for testing.
func (b *Broadcaster) GetPendingSyncIDs(clientID string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, len(b.pendingSync[clientID]))
	copy(ids, b.pendingSync[clientID])
	return ids
}
