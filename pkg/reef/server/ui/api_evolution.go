package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Evolution API structs (Wave 1.5)
// ---------------------------------------------------------------------------

// GeneResponse represents a single gene in the evolution system.
type GeneResponse struct {
	ID            string  `json:"id"`
	Role          string  `json:"role"`
	ControlSignal float64 `json:"control_signal"`
	Status        string  `json:"status"`
	CreatedAt     int64   `json:"created_at"`
}

// EvolutionStrategyResponse represents the current evolution strategy.
type EvolutionStrategyResponse struct {
	Strategy string `json:"strategy"`
	Innovate int    `json:"innovate"`
	Optimize int    `json:"optimize"`
	Repair   int    `json:"repair"`
}

// CapsuleResponse represents a capsule in the evolution system.
type CapsuleResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Role       string  `json:"role"`
	SkillCount int     `json:"skill_count"`
	Rating     float64 `json:"rating"`
}

// EvolutionHub is the interface for accessing evolution data.
type EvolutionHub interface {
	Genes() []GeneResponse
	ApproveGene(id string) error
	RejectGene(id string, reason string) error
	Strategy() EvolutionStrategyResponse
	SetStrategy(strategy string) error
	Capsules() []CapsuleResponse
}

// ---------------------------------------------------------------------------
// Activity API structs (Wave 1.6)
// ---------------------------------------------------------------------------

// ActivityEvent represents a single activity event.
type ActivityEvent struct {
	ID          string `json:"id"`
	Type        string `json:"type"` // agent/task/evolution/system
	Icon        string `json:"icon"`
	Actor       string `json:"actor"`
	Description string `json:"description"`
	Timestamp   int64  `json:"timestamp"`
}

// ActivityStore is a ring-buffer store for activity events.
type ActivityStore struct {
	mu     sync.RWMutex
	events []ActivityEvent
	max    int // ring buffer capacity, default 1000
}

// NewActivityStore creates a new ActivityStore with the given max capacity.
func NewActivityStore(max int) *ActivityStore {
	if max <= 0 {
		max = 1000
	}
	return &ActivityStore{
		events: make([]ActivityEvent, 0, max),
		max:    max,
	}
}

// Add appends an event to the store, evicting the oldest if at capacity.
func (s *ActivityStore) Add(ev ActivityEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) >= s.max {
		// Drop oldest
		s.events = s.events[1:]
	}
	s.events = append(s.events, ev)
}

// Query returns events filtered by type (empty = all) and limited to the most
// recent `limit` events. If limit <= 0, defaults to 50.
func (s *ActivityStore) Query(eventType string, limit int) []ActivityEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	// Walk backwards (newest first) and collect up to `limit` matching events.
	var result []ActivityEvent
	for i := len(s.events) - 1; i >= 0; i-- {
		ev := s.events[i]
		if eventType != "" && ev.Type != eventType {
			continue
		}
		result = append(result, ev)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Config & Hermes in-memory stores (Wave 1.6)
// ---------------------------------------------------------------------------

// ConfigStore holds the general configuration.
type ConfigStore struct {
	mu   sync.RWMutex
	data map[string]interface{}
}

func newConfigStore() *ConfigStore {
	return &ConfigStore{data: make(map[string]interface{})}
}

func (s *ConfigStore) Get() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]interface{}, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

func (s *ConfigStore) Put(cfg map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = cfg
}

// HermesConfig stores Hermes-related settings.
type HermesConfig struct {
	mu   sync.RWMutex
	data map[string]interface{}
}

func newHermesConfig() *HermesConfig {
	return &HermesConfig{
		data: map[string]interface{}{
			"mode":             "standard",
			"fallback_enabled": false,
			"fallback_timeout": 30,
			"allowed_tools":    []string{},
		},
	}
}

func (s *HermesConfig) Get() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]interface{}, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

func (s *HermesConfig) Put(cfg map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range cfg {
		s.data[k] = v
	}
}

// ---------------------------------------------------------------------------
// Package-level singleton stores (since we cannot modify Handler in ui.go)
// ---------------------------------------------------------------------------

var (
	evolutionHubInstance  EvolutionHub
	activityStoreInstance *ActivityStore
	configStoreInstance   *ConfigStore
	hermesConfigInstance  *HermesConfig
)

func ensureStores() {
	if activityStoreInstance == nil {
		activityStoreInstance = NewActivityStore(1000)
	}
	if configStoreInstance == nil {
		configStoreInstance = newConfigStore()
	}
	if hermesConfigInstance == nil {
		hermesConfigInstance = newHermesConfig()
	}
}

// SetEvolutionHub sets the global EvolutionHub implementation.
// Call this from main/server setup if you have a real implementation.
func SetEvolutionHub(hub EvolutionHub) {
	evolutionHubInstance = hub
}

// GetActivityStore returns the global ActivityStore so other packages can push events.
func GetActivityStore() *ActivityStore {
	ensureStores()
	return activityStoreInstance
}

// ---------------------------------------------------------------------------
// Helper: extract path parameter
// ---------------------------------------------------------------------------

// extractPathParam removes the prefix from path and returns the next segment.
// Example: extractPathParam("/api/v2/evolution/genes/abc123/approve", "/api/v2/evolution/genes/") → "abc123"
func extractPathParam(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	// Take the first segment (before next /)
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

// RegisterV2Routes registers all v2 API routes on the given mux.
// Call this from RegisterRoutes in ui.go (e.g. h.RegisterV2Routes(mux)).
func (h *Handler) RegisterV2Routes(mux *http.ServeMux) {
	ensureStores()

	// Evolution
	mux.HandleFunc("/api/v2/evolution/genes", h.handleV2GenesList)
	mux.HandleFunc("/api/v2/evolution/genes/", h.handleV2GeneAction)
	mux.HandleFunc("/api/v2/evolution/strategy", h.handleV2EvolutionStrategy)
	mux.HandleFunc("/api/v2/evolution/capsules", h.handleV2CapsulesList)

	// Activity
	mux.HandleFunc("/api/v2/activity", h.handleV2Activity)

	// Config
	mux.HandleFunc("/api/v2/config", h.handleV2Config)

	// Hermes
	mux.HandleFunc("/api/v2/hermes", h.handleV2Hermes)

	// Logs (SSE)
	mux.HandleFunc("/api/v2/logs", h.handleV2Logs)
}

// ---------------------------------------------------------------------------
// Evolution handlers
// ---------------------------------------------------------------------------

// handleV2GenesList — GET /api/v2/evolution/genes
func (h *Handler) handleV2GenesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if evolutionHubInstance == nil {
		writeJSON(w, []GeneResponse{})
		return
	}
	writeJSON(w, evolutionHubInstance.Genes())
}

// handleV2GeneAction routes to approve/reject based on suffix.
// POST /api/v2/evolution/genes/{id}/approve
// POST /api/v2/evolution/genes/{id}/reject
func (h *Handler) handleV2GeneAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if evolutionHubInstance == nil {
		http.Error(w, "evolution hub not configured", http.StatusServiceUnavailable)
		return
	}

	path := r.URL.Path

	// Determine action suffix
	var geneID, action string
	if strings.HasSuffix(path, "/approve") {
		action = "approve"
		// Strip /approve to get the ID
		trimmed := strings.TrimSuffix(path, "/approve")
		geneID = extractPathParam(trimmed+"/", "/api/v2/evolution/genes/")
	} else if strings.HasSuffix(path, "/reject") {
		action = "reject"
		trimmed := strings.TrimSuffix(path, "/reject")
		geneID = extractPathParam(trimmed+"/", "/api/v2/evolution/genes/")
	} else {
		http.NotFound(w, r)
		return
	}

	if geneID == "" {
		http.Error(w, "missing gene id", http.StatusBadRequest)
		return
	}

	switch action {
	case "approve":
		if err := evolutionHubInstance.ApproveGene(geneID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "approved", "id": geneID})

	case "reject":
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if err := evolutionHubInstance.RejectGene(geneID, body.Reason); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "rejected", "id": geneID})
	}
}

// handleV2EvolutionStrategy — GET / PUT /api/v2/evolution/strategy
func (h *Handler) handleV2EvolutionStrategy(w http.ResponseWriter, r *http.Request) {
	if evolutionHubInstance == nil {
		http.Error(w, "evolution hub not configured", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, evolutionHubInstance.Strategy())

	case http.MethodPut:
		var body struct {
			Strategy string `json:"strategy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Strategy == "" {
			http.Error(w, "strategy is required", http.StatusBadRequest)
			return
		}
		if err := evolutionHubInstance.SetStrategy(body.Strategy); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "updated"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleV2CapsulesList — GET /api/v2/evolution/capsules
func (h *Handler) handleV2CapsulesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if evolutionHubInstance == nil {
		writeJSON(w, []CapsuleResponse{})
		return
	}
	writeJSON(w, evolutionHubInstance.Capsules())
}

// ---------------------------------------------------------------------------
// Activity handler
// ---------------------------------------------------------------------------

// handleV2Activity — GET /api/v2/activity?type=...&limit=50
func (h *Handler) handleV2Activity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ensureStores()

	q := r.URL.Query()
	filterType := q.Get("type")
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	events := activityStoreInstance.Query(filterType, limit)
	if events == nil {
		events = []ActivityEvent{}
	}
	writeJSON(w, events)
}

// ---------------------------------------------------------------------------
// Config handler
// ---------------------------------------------------------------------------

// handleV2Config — GET / PUT /api/v2/config
func (h *Handler) handleV2Config(w http.ResponseWriter, r *http.Request) {
	ensureStores()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, configStoreInstance.Get())

	case http.MethodPut:
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		configStoreInstance.Put(body)
		writeJSON(w, map[string]string{"status": "updated"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// Hermes handler
// ---------------------------------------------------------------------------

// handleV2Hermes — GET / PUT /api/v2/hermes
func (h *Handler) handleV2Hermes(w http.ResponseWriter, r *http.Request) {
	ensureStores()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, hermesConfigInstance.Get())

	case http.MethodPut:
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		hermesConfigInstance.Put(body)
		writeJSON(w, map[string]string{"status": "updated"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// Logs SSE handler (Wave 1.6)
// ---------------------------------------------------------------------------

// LogEntry is the JSON payload for a single log event in the SSE stream.
type LogEntry struct {
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// logSSEEvent represents an internal log event pushed to the EventBus.
type logSSEEvent struct {
	Level     string
	Message   string
	Timestamp int64
}

// PublishLog pushes a log event onto the EventBus for SSE consumers.
// External packages can call this to feed the /api/v2/logs SSE stream.
func (h *Handler) PublishLog(level, message string) {
	h.eventBus.Publish(Event{
		Type: "log",
		Data: LogEntry{
			Level:     level,
			Message:   message,
			Timestamp: time.Now().UnixMilli(),
		},
	})
}

// handleV2Logs — GET /api/v2/logs?level=INFO/WARN/ERROR
// Returns an SSE stream of log events filtered by level.
func (h *Handler) handleV2Logs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	filterLevel := strings.ToUpper(r.URL.Query().Get("level"))

	ch := h.eventBus.Subscribe()
	defer h.eventBus.Unsubscribe(ch)

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-ch:
			// Only forward log events
			if event.Type != "log" {
				continue
			}

			entry, ok := event.Data.(LogEntry)
			if !ok {
				continue
			}

			// Apply level filter
			if filterLevel != "" && !strings.EqualFold(entry.Level, filterLevel) {
				continue
			}

			fmt.Fprintf(w, "event: log\ndata: %s\n\n", mustJSON(entry))
			flusher.Flush()
		}
	}
}

// ---------------------------------------------------------------------------
// Compile-time interface assertions
// ---------------------------------------------------------------------------

var _ = (*ActivityStore)(nil)
var _ = (*ConfigStore)(nil)
var _ = (*HermesConfig)(nil)
