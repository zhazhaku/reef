package client

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// SubmitterConfig configures the GeneSubmitter.
type SubmitterConfig struct {
	// MaxQueueSize is the maximum number of genes in the offline queue.
	// Defaults to 10.
	MaxQueueSize int

	// RetryOnReconnect controls whether the queue is drained on WebSocket reconnect.
	// Defaults to true.
	RetryOnReconnect bool

	// QueueFilePath is the filesystem path for persisting the offline queue.
	// Defaults to ".reef/offline_genes.json" relative to the workspace (set externally).
	QueueFilePath string

	// MaxRetries is the max submission attempts for submitWithRetry.
	// Defaults to 3.
	MaxRetries int
}

// setDefaults applies sensible defaults to zero-valued config fields.
func (c *SubmitterConfig) setDefaults() {
	if c.MaxQueueSize <= 0 {
		c.MaxQueueSize = 10
	}
	// RetryOnReconnect defaults to true via zero-value bool.
	// Since Go cannot distinguish "unset" from "false", we always enable it
	// unless explicitly set to false by the caller.
	c.RetryOnReconnect = true
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
}

// GeneSubmitter handles client-side gene submission via WebSocket with an
// offline FIFO queue for disconnected operation and ordered drain on reconnect.
//
// Submitter assumes serialized access to the WebSocket connection.
// Concurrent calls to Submit are safe via the queue mutex, but concurrent
// writes to the WebSocket connection are NOT mutex-protected.
type GeneSubmitter struct {
	conn        *websocket.Conn
	offlineQueue []*evolution.Gene
	queueMu     sync.Mutex
	config      SubmitterConfig
	logger      *slog.Logger
	reconnectCh chan struct{}
}

// NewGeneSubmitter creates a new GeneSubmitter.
func NewGeneSubmitter(config SubmitterConfig, logger *slog.Logger) *GeneSubmitter {
	config.setDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &GeneSubmitter{
		config:      config,
		logger:      logger,
		reconnectCh: make(chan struct{}, 1),
	}
}

// SetConn updates the WebSocket connection used for submission.
func (s *GeneSubmitter) SetConn(conn *websocket.Conn) {
	s.conn = conn
}

// Submit implements GeneSubmittor. It submits a gene via WebSocket when online,
// or enqueues it when offline. Stagnant genes are never submitted.
func (s *GeneSubmitter) Submit(gene *evolution.Gene) {
	if gene == nil {
		s.logger.Warn("submitter: cannot submit nil gene")
		return
	}

	// Stagnant genes are never submitted
	if gene.Status == evolution.GeneStatusStagnant {
		s.logger.Warn("submitter: skipping stagnant gene",
			slog.String("gene_id", gene.ID))
		return
	}

	// Serialize gene to JSON for the payload
	geneJSON, err := json.Marshal(gene)
	if err != nil {
		s.logger.Error("submitter: failed to marshal gene",
			slog.String("gene_id", gene.ID),
			slog.String("error", err.Error()))
		return
	}

	// Check connection status
	if s.conn == nil {
		s.enqueue(gene)
		return
	}

	// Build reef.Message
	msg, err := reef.NewMessage(reef.MsgGeneSubmit, "", reef.GeneSubmitPayload{
		GeneID:         gene.ID,
		GeneData:       geneJSON,
		SourceEventIDs: gene.SourceEvents,
		ClientID:       gene.SourceClientID,
		Timestamp:      time.Now().UnixMilli(),
	})
	if err != nil {
		s.logger.Error("submitter: failed to build gene submit message",
			slog.String("gene_id", gene.ID),
			slog.String("error", err.Error()))
		return
	}

	// Write to WebSocket
	if err := s.conn.WriteJSON(msg); err != nil {
		s.logger.Warn("submitter: write failed, enqueuing gene",
			slog.String("gene_id", gene.ID),
			slog.String("error", err.Error()))
		s.enqueue(gene)
		return
	}

	// Update gene status
	gene.Status = evolution.GeneStatusSubmitted
	gene.UpdatedAt = time.Now().UTC()

	s.logger.Debug("submitter: gene submitted",
		slog.String("gene_id", gene.ID))
}

// enqueue adds a gene to the offline FIFO queue.
// If the queue is full, the oldest entry is dropped.
// The gene's status stays Draft (not Submitted) while queued.
func (s *GeneSubmitter) enqueue(gene *evolution.Gene) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()

	if len(s.offlineQueue) >= s.config.MaxQueueSize {
		// Drop oldest (FIFO)
		dropped := s.offlineQueue[0]
		s.offlineQueue = s.offlineQueue[1:]
		s.logger.Warn("submitter: queue full, dropping oldest gene",
			slog.String("dropped_gene_id", dropped.ID),
			slog.Int("queue_size", len(s.offlineQueue)))
	}

	s.offlineQueue = append(s.offlineQueue, gene)
	s.logger.Debug("submitter: gene enqueued",
		slog.String("gene_id", gene.ID),
		slog.Int("queue_size", len(s.offlineQueue)))
}

// drainQueue processes all queued genes in FIFO order.
// Genes that fail to submit are re-enqueued.
func (s *GeneSubmitter) drainQueue() {
	s.queueMu.Lock()
	queue := s.offlineQueue
	s.offlineQueue = nil
	s.queueMu.Unlock()

	if len(queue) == 0 {
		return
	}

	s.logger.Info("submitter: draining offline queue",
		slog.Int("count", len(queue)))

	for _, gene := range queue {
		if !s.submitWithRetry(gene) {
			s.enqueue(gene)
		}
	}
}

// submitWithRetry attempts to submit a gene with exponential backoff.
// Returns true if submission succeeded, false after exhausting retries.
// Backoff schedule: 1s, 2s, 4s (max 3 attempts total).
func (s *GeneSubmitter) submitWithRetry(gene *evolution.Gene) bool {
	backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
		if s.conn == nil {
			return false
		}

		geneJSON, err := json.Marshal(gene)
		if err != nil {
			s.logger.Error("submitter: retry marshal failed",
				slog.String("gene_id", gene.ID),
				slog.String("error", err.Error()))
			return false
		}

		msg, err := reef.NewMessage(reef.MsgGeneSubmit, "", reef.GeneSubmitPayload{
			GeneID:         gene.ID,
			GeneData:       geneJSON,
			SourceEventIDs: gene.SourceEvents,
			ClientID:       gene.SourceClientID,
			Timestamp:      time.Now().UnixMilli(),
		})
		if err != nil {
			return false
		}

		if err := s.conn.WriteJSON(msg); err == nil {
			gene.Status = evolution.GeneStatusSubmitted
			gene.UpdatedAt = time.Now().UTC()
			s.logger.Debug("submitter: retry submit succeeded",
				slog.String("gene_id", gene.ID),
				slog.Int("attempt", attempt+1))
			return true
		}

		if attempt < s.config.MaxRetries {
			delay := backoffs[attempt]
			s.logger.Debug("submitter: retry backoff",
				slog.String("gene_id", gene.ID),
				slog.Int("attempt", attempt+1),
				slog.Duration("delay", delay))
			time.Sleep(delay)
		}
	}

	s.logger.Warn("submitter: retry exhausted",
		slog.String("gene_id", gene.ID),
		slog.Int("max_retries", s.config.MaxRetries))
	return false
}

// PersistQueue serializes the offline queue to a JSON file.
// Called on graceful shutdown. Returns an error on failure.
func (s *GeneSubmitter) PersistQueue() error {
	if s.config.QueueFilePath == "" {
		return fmt.Errorf("submitter: QueueFilePath not configured")
	}

	s.queueMu.Lock()
	defer s.queueMu.Unlock()

	if len(s.offlineQueue) == 0 {
		// Remove the file if it exists (empty queue)
		_ = os.Remove(s.config.QueueFilePath)
		return nil
	}

	type queueEntry struct {
		GeneID   string          `json:"gene_id"`
		GeneJSON json.RawMessage `json:"gene_json"`
		QueuedAt string          `json:"queued_at"`
	}

	entries := make([]queueEntry, 0, len(s.offlineQueue))
	for _, gene := range s.offlineQueue {
		geneJSON, err := json.Marshal(gene)
		if err != nil {
			s.logger.Error("submitter: persist marshal failed",
				slog.String("gene_id", gene.ID),
				slog.String("error", err.Error()))
			return fmt.Errorf("submitter: marshal gene %s: %w", gene.ID, err)
		}
		entries = append(entries, queueEntry{
			GeneID:   gene.ID,
			GeneJSON: geneJSON,
			QueuedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}

	// Ensure parent directory exists
	dir := filepath.Dir(s.config.QueueFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("submitter: create queue dir: %w", err)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("submitter: marshal queue: %w", err)
	}

	if err := os.WriteFile(s.config.QueueFilePath, data, 0644); err != nil {
		return fmt.Errorf("submitter: write queue file: %w", err)
	}

	s.logger.Info("submitter: queue persisted",
		slog.String("path", s.config.QueueFilePath),
		slog.Int("count", len(entries)))
	return nil
}

// RestoreQueue reads the persisted queue file and enqueues genes.
// If the file doesn't exist, it's a no-op.
// If the file is corrupted, it logs the error, deletes the file, and returns an error.
func (s *GeneSubmitter) RestoreQueue() error {
	if s.config.QueueFilePath == "" {
		return fmt.Errorf("submitter: QueueFilePath not configured")
	}

	data, err := os.ReadFile(s.config.QueueFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no queue file, nothing to restore
		}
		return fmt.Errorf("submitter: read queue file: %w", err)
	}

	type queueEntry struct {
		GeneID   string          `json:"gene_id"`
		GeneJSON json.RawMessage `json:"gene_json"`
		QueuedAt string          `json:"queued_at"`
	}

	var entries []queueEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// Corrupted file: log, delete, start empty
		s.logger.Error("submitter: corrupted queue file, deleting",
			slog.String("path", s.config.QueueFilePath),
			slog.String("error", err.Error()))
		_ = os.Remove(s.config.QueueFilePath)
		return fmt.Errorf("submitter: corrupted queue file: %w", err)
	}

	s.queueMu.Lock()
	defer s.queueMu.Unlock()

	for _, entry := range entries {
		var gene evolution.Gene
		if err := json.Unmarshal(entry.GeneJSON, &gene); err != nil {
			s.logger.Error("submitter: failed to restore gene entry",
				slog.String("gene_id", entry.GeneID),
				slog.String("error", err.Error()))
			continue
		}
		// Respect queue size limit during restore
		if len(s.offlineQueue) >= s.config.MaxQueueSize {
			s.logger.Warn("submitter: queue full during restore, dropping oldest")
			s.offlineQueue = s.offlineQueue[1:]
		}
		s.offlineQueue = append(s.offlineQueue, &gene)
	}

	s.logger.Info("submitter: queue restored",
		slog.String("path", s.config.QueueFilePath),
		slog.Int("count", len(s.offlineQueue)))
	return nil
}

// OnReconnect is called after a successful WebSocket reconnection.
// It updates the connection and drains the offline queue.
func (s *GeneSubmitter) OnReconnect(conn *websocket.Conn) {
	s.conn = conn

	// Signal reconnect to any waiters
	select {
	case s.reconnectCh <- struct{}{}:
	default:
	}

	if s.config.RetryOnReconnect {
		go s.drainQueue()
	}
}

// ReconnectCh returns the reconnect signal channel for external waiters.
func (s *GeneSubmitter) ReconnectCh() <-chan struct{} {
	return s.reconnectCh
}

// QueueLen returns the current offline queue length (thread-safe).
func (s *GeneSubmitter) QueueLen() int {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	return len(s.offlineQueue)
}
