// Package server implements the server-side evolution engine components:
// EvolutionHub (lifecycle orchestration), ServerGatekeeper, GeneBroadcaster,
// and SkillMerger.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// Interfaces (defined here to avoid circular imports with pkg/reef/server)
// ---------------------------------------------------------------------------

// ConnManager abstracts WebSocket message delivery for evolution components.
// Implemented by WebSocketServer in pkg/reef/server.
type ConnManager interface {
	SendToClient(clientID string, msg reef.Message) error
	BroadcastToRole(role string, msg reef.Message) []error
}

// GeneStore is the persistence interface for genes required by EvolutionHub.
// Implemented by store.SQLiteStore or store.MemoryStore (extended).
type GeneStore interface {
	InsertGene(gene *evolution.Gene) error
	UpdateGene(gene *evolution.Gene) error
	GetGene(geneID string) (*evolution.Gene, error)
	CountApprovedGenes(role string) (int, error)
	CountByStatus(status string) (int, error)
	GetApprovedGenes(role string, limit int) ([]*evolution.Gene, error)
}

// GeneBroadcaster sends approved genes to same-role clients.
type GeneBroadcaster interface {
	Broadcast(ctx context.Context, gene *evolution.Gene, sourceClientID string) error
}

// ServerGatekeeper performs server-side gene validation.
type ServerGatekeeper interface {
	Review(ctx context.Context, gene *evolution.Gene) (*GateResult, error)
}

// SkillMerger checks and triggers gene-to-skill-draft merging.
type SkillMerger interface {
	CheckAndMerge(ctx context.Context, role string)
}

// GateResult is the output of a ServerGatekeeper review.
type GateResult struct {
	Passed        bool   `json:"passed"`
	Reason        string `json:"reason,omitempty"`
	RejectedLayer int    `json:"rejected_layer,omitempty"` // 1, 2, or 3
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// HubConfig configures the EvolutionHub.
type HubConfig struct {
	// Enabled toggles the evolution engine on the server side.
	// When false, all methods return nil immediately (no-op).
	Enabled bool

	// MaxPendingReview is the maximum number of genes in "submitted" state
	// awaiting approval. Default: 100.
	MaxPendingReview int
}

// DefaultHubConfig returns a HubConfig with sensible defaults.
func DefaultHubConfig() HubConfig {
	return HubConfig{
		Enabled:          true,
		MaxPendingReview: 100,
	}
}

// ---------------------------------------------------------------------------
// HubStats
// ---------------------------------------------------------------------------

// HubStats holds thread-safe counters for the EvolutionHub.
type HubStats struct {
	TotalSubmitted   int64
	TotalApproved    int64
	TotalRejected    int64
	TotalBroadcasted int64
	LastActivityTime time.Time // Protected by mu, not atomic
}

// Snapshot returns a copy of the current stats.
func (hs *HubStats) Snapshot() HubStats {
	return HubStats{
		TotalSubmitted:   atomic.LoadInt64(&hs.TotalSubmitted),
		TotalApproved:    atomic.LoadInt64(&hs.TotalApproved),
		TotalRejected:    atomic.LoadInt64(&hs.TotalRejected),
		TotalBroadcasted: atomic.LoadInt64(&hs.TotalBroadcasted),
		LastActivityTime: hs.LastActivityTime,
	}
}

// ---------------------------------------------------------------------------
// EvolutionHub
// ---------------------------------------------------------------------------

// EvolutionHub orchestrates the full gene lifecycle on the server side:
// receive gene_submit → deserialize → Gate → save approved → broadcast →
// check SkillMerger trigger.
type EvolutionHub struct {
	store       GeneStore
	broadcaster GeneBroadcaster
	gatekeeper  ServerGatekeeper
	merger      SkillMerger
	connManager ConnManager
	config      HubConfig
	logger      *slog.Logger
	mu          sync.RWMutex
	stats       HubStats
}

// NewEvolutionHub creates a new EvolutionHub with the given dependencies.
// All components except config and logger may be nil; the hub validates
// required components at call time.
func NewEvolutionHub(
	store GeneStore,
	broadcaster GeneBroadcaster,
	gatekeeper ServerGatekeeper,
	merger SkillMerger,
	connManager ConnManager,
	config HubConfig,
	logger *slog.Logger,
) *EvolutionHub {
	if logger == nil {
		logger = slog.Default()
	}
	if config.MaxPendingReview <= 0 {
		config.MaxPendingReview = 100
	}
	return &EvolutionHub{
		store:       store,
		broadcaster: broadcaster,
		gatekeeper:  gatekeeper,
		merger:      merger,
		connManager: connManager,
		config:      config,
		logger:      logger,
	}
}

// HandleGeneSubmission implements the full gene submission lifecycle.
func (h *EvolutionHub) HandleGeneSubmission(ctx context.Context, msg reef.Message, clientID string) error {
	// If disabled, no-op.
	if !h.config.Enabled {
		return nil
	}

	// Validate required components.
	if h.store == nil {
		return fmt.Errorf("evolution hub: store not initialized")
	}
	if h.gatekeeper == nil {
		return fmt.Errorf("evolution hub: gatekeeper not initialized")
	}

	// Deserialize payload.
	var payload reef.GeneSubmitPayload
	if err := msg.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode gene_submit payload: %w", err)
	}

	// Deserialize gene from gene_data.
	var gene evolution.Gene
	if err := json.Unmarshal(payload.GeneData, &gene); err != nil {
		return fmt.Errorf("unmarshal gene data: %w", err)
	}

	// Validate gene structural integrity.
	if errs := gene.Validate(); len(errs) > 0 {
		h.logger.Warn("gene validation failed",
			slog.String("gene_id", gene.ID),
			slog.Any("errors", errs))
		// Reject with validation error.
		return h.handleRejectedNotify(clientID, &gene, &GateResult{
			Passed:        false,
			Reason:        fmt.Sprintf("validation: %v", errs),
			RejectedLayer: 0,
		})
	}

	// Ensure gene has client metadata.
	if gene.SourceClientID == "" {
		gene.SourceClientID = clientID
	}

	// Track total submitted.
	atomic.AddInt64(&h.stats.TotalSubmitted, 1)

	// Server Gatekeeper review.
	result, err := h.gatekeeper.Review(ctx, &gene)
	if err != nil {
		return fmt.Errorf("gatekeeper review: %w", err)
	}

	now := time.Now().UTC()

	if result.Passed {
		// Approve the gene.
		gene.Status = evolution.GeneStatusApproved
		gene.ApprovedAt = &now
		gene.UpdatedAt = now

		// Save to store.
		existing, err := h.store.GetGene(gene.ID)
		if err != nil {
			return fmt.Errorf("check existing gene: %w", err)
		}
		if existing != nil {
			if err := h.store.UpdateGene(&gene); err != nil {
				h.logger.Error("update gene failed",
					slog.String("gene_id", gene.ID),
					slog.String("error", err.Error()))
				return fmt.Errorf("update gene: %w", err)
			}
		} else {
			if err := h.store.InsertGene(&gene); err != nil {
				// Duplicate key: gene already processed by another path.
				h.logger.Warn("insert gene failed (duplicate?), already processed",
					slog.String("gene_id", gene.ID),
					slog.String("error", err.Error()))
				return nil
			}
		}

		// Broadcast to same-role clients.
		if h.broadcaster != nil {
			if err := h.broadcaster.Broadcast(ctx, &gene, clientID); err != nil {
				h.logger.Warn("broadcast failed",
					slog.String("gene_id", gene.ID),
					slog.String("error", err.Error()))
			} else {
				atomic.AddInt64(&h.stats.TotalBroadcasted, 1)
			}
		} else {
			h.logger.Warn("broadcaster not initialized, skipping broadcast",
				slog.String("gene_id", gene.ID))
		}

		// Check SkillMerger trigger.
		if h.merger != nil {
			h.merger.CheckAndMerge(ctx, gene.Role)
		} else {
			h.logger.Debug("merger not initialized, skipping merge check",
				slog.String("gene_id", gene.ID))
		}

		// Send approved notification to source client.
		approvedPayload := reef.GeneApprovedPayload{
			GeneID:     gene.ID,
			ApprovedBy: "server",
			ServerTime: time.Now().UnixMilli(),
		}
		approvedMsg, err := reef.NewMessage(reef.MsgGeneApproved, "", approvedPayload)
		if err != nil {
			h.logger.Warn("build approved message failed",
				slog.String("gene_id", gene.ID),
				slog.String("error", err.Error()))
		} else if h.connManager != nil {
			if err := h.connManager.SendToClient(clientID, approvedMsg); err != nil {
				h.logger.Warn("send approved notification failed",
					slog.String("gene_id", gene.ID),
					slog.String("client_id", clientID),
					slog.String("error", err.Error()))
			}
		}

		atomic.AddInt64(&h.stats.TotalApproved, 1)
	} else {
		// Rejected by gatekeeper.
		gene.Status = evolution.GeneStatusRejected
		gene.UpdatedAt = now

		// Save rejected gene for audit.
		if err := h.store.InsertGene(&gene); err != nil {
			h.logger.Warn("insert rejected gene failed",
				slog.String("gene_id", gene.ID),
				slog.String("error", err.Error()))
		}

		// Notify source client.
		if err := h.handleRejectedNotify(clientID, &gene, result); err != nil {
			h.logger.Warn("send rejected notification failed",
				slog.String("gene_id", gene.ID),
				slog.String("error", err.Error()))
		}

		atomic.AddInt64(&h.stats.TotalRejected, 1)
	}

	// Update last activity time.
	h.mu.Lock()
	h.stats.LastActivityTime = time.Now()
	h.mu.Unlock()

	return nil
}

// handleRejectedNotify sends a gene_rejected message to the source client.
func (h *EvolutionHub) handleRejectedNotify(clientID string, gene *evolution.Gene, result *GateResult) error {
	payload := reef.GeneRejectedPayload{
		GeneID:     gene.ID,
		Reason:     result.Reason,
		Layer:      result.RejectedLayer,
		ServerTime: time.Now().UnixMilli(),
	}

	msg, err := reef.NewMessage(reef.MsgGeneRejected, "", payload)
	if err != nil {
		return fmt.Errorf("build rejected message: %w", err)
	}

	if h.connManager == nil {
		return nil
	}

	if err := h.connManager.SendToClient(clientID, msg); err != nil {
		h.logger.Warn("send rejected notification to client",
			slog.String("gene_id", gene.ID),
			slog.String("client_id", clientID),
			slog.String("reason", result.Reason),
			slog.Int("layer", result.RejectedLayer),
			slog.String("error", err.Error()))
		return err
	}

	return nil
}

// HandleGeneRejected is a public method for external rejection (e.g., admin rejects a gene).
func (h *EvolutionHub) HandleGeneRejected(ctx context.Context, geneID string, reason string, layer int) error {
	if !h.config.Enabled {
		return nil
	}

	if h.store == nil {
		return fmt.Errorf("evolution hub: store not initialized")
	}

	gene, err := h.store.GetGene(geneID)
	if err != nil {
		return fmt.Errorf("get gene %s: %w", geneID, err)
	}
	if gene == nil {
		return fmt.Errorf("gene %s not found", geneID)
	}

	result := &GateResult{
		Passed:        false,
		Reason:        reason,
		RejectedLayer: layer,
	}

	gene.Status = evolution.GeneStatusRejected
	gene.UpdatedAt = time.Now().UTC()
	if err := h.store.UpdateGene(gene); err != nil {
		return fmt.Errorf("update gene: %w", err)
	}

	atomic.AddInt64(&h.stats.TotalRejected, 1)

	if err := h.handleRejectedNotify(gene.SourceClientID, gene, result); err != nil {
		h.logger.Warn("rejected notify failed",
			slog.String("gene_id", geneID),
			slog.String("error", err.Error()))
	}

	return nil
}

// GetStats returns a snapshot of the current hub statistics.
func (h *EvolutionHub) GetStats() HubStats {
	return h.stats.Snapshot()
}

// IsEnabled returns whether the evolution hub is enabled.
func (h *EvolutionHub) IsEnabled() bool {
	return h.config.Enabled
}
