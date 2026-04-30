// Package api provides Reef swarm orchestration endpoints for the web UI.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Provider interface — decouples web backend from reef internals
// ---------------------------------------------------------------------------

// ReefProvider abstracts the Reef Server for the web backend.
type ReefProvider interface {
	Status() (ReefStatus, error)
	Tasks(filter ReefTaskFilter) ([]ReefTask, int, error)
	TaskByID(id string) (*ReefTask, error)
	SubTasks(id string) ([]ReefTask, error)
	Clients() ([]ReefClient, error)
	CancelTask(id string) error
}

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

// ReefStatus represents overall swarm system health.
type ReefStatus struct {
	ServerVersion    string    `json:"server_version"`
	Uptime           string    `json:"uptime"`
	ConnectedClients int       `json:"connected_clients"`
	QueuedTasks      int       `json:"queued_tasks"`
	RunningTasks     int       `json:"running_tasks"`
	CompletedTasks   int       `json:"completed_tasks"`
	FailedTasks      int       `json:"failed_tasks"`
	TotalTasks       int       `json:"total_tasks"`
	StartedAt        time.Time `json:"started_at"`
}

// ReefTask represents a task in the swarm.
type ReefTask struct {
	ID               string       `json:"id"`
	Instruction      string       `json:"instruction"`
	Status           string       `json:"status"`
	RequiredRole     string       `json:"required_role"`
	RequiredSkills   []string     `json:"required_skills,omitempty"`
	Priority         int          `json:"priority"`
	AssignedClient   string       `json:"assigned_client,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
	TimeoutMs        int64        `json:"timeout_ms"`
	MaxRetries       int          `json:"max_retries"`
	AttemptCount     int          `json:"attempt_count"`
	EscalationCount  int          `json:"escalation_count"`
	ParentTaskID     string       `json:"parent_task_id,omitempty"`
	ChildCount       int          `json:"child_count"`
	DependencyCount  int          `json:"dependency_count"`
	ReplyTo          *ReefReplyTo `json:"reply_to,omitempty"`
}

// ReefTaskFilter specifies query parameters for listing tasks.
type ReefTaskFilter struct {
	Status string // "" means all
	Role   string // "" means all
	Search string // "" means no search
	Limit  int
	Offset int
}

// ReefClient represents a connected swarm node.
type ReefClient struct {
	ID           string    `json:"id"`
	Role         string    `json:"role"`
	Skills       []string  `json:"skills"`
	State        string    `json:"state"` // connected / stale / disconnected
	CurrentLoad  int       `json:"current_load"`
	Capacity     int       `json:"capacity"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

// ReefReplyTo mirrors the reef.ReplyToContext.
type ReefReplyTo struct {
	Channel   string `json:"channel"`
	ChatID    string `json:"chat_id"`
	UserID    string `json:"user_id"`
	MessageID string `json:"message_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
}

// ---------------------------------------------------------------------------
// SSE event types
// ---------------------------------------------------------------------------

// ReefSSEEvent is pushed to connected clients via SSE.
type ReefSSEEvent struct {
	Type string      `json:"type"` // stats_update, task_created, task_completed, task_failed, client_connected, client_disconnected
	Data interface{} `json:"data"`
}

// ---------------------------------------------------------------------------
// SSE hub
// ---------------------------------------------------------------------------

type reefSSEClient struct {
	ch     chan []byte
	done   chan struct{}
}

type reefSSEHub struct {
	mu      sync.RWMutex
	clients map[int]*reefSSEClient
	nextID  int
}

var sseHub = &reefSSEHub{clients: make(map[int]*reefSSEClient)}

func (hub *reefSSEHub) subscribe() (int, <-chan []byte, <-chan struct{}) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	id := hub.nextID
	hub.nextID++
	c := &reefSSEClient{ch: make(chan []byte, 64), done: make(chan struct{})}
	hub.clients[id] = c
	return id, c.ch, c.done
}

func (hub *reefSSEHub) unsubscribe(id int) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if c, ok := hub.clients[id]; ok {
		close(c.done)
		delete(hub.clients, id)
	}
}

func (hub *reefSSEHub) broadcast(e ReefSSEEvent) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	payload := []byte(fmt.Sprintf("data: %s\n\n", data))
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	for _, c := range hub.clients {
		select {
		case c.ch <- payload:
		default:
			// client too slow, drop
		}
	}
}

// BroadcastReefEvent sends an SSE event to all connected SSE clients.
func BroadcastReefEvent(e ReefSSEEvent) {
	sseHub.broadcast(e)
}

// ---------------------------------------------------------------------------
// ReefProvider adapter (global, set by launcher main or test)
// ---------------------------------------------------------------------------

var reefProvider ReefProvider
var reefMu sync.RWMutex

// SetReefProvider sets the Reef backend for web API handlers.
func SetReefProvider(p ReefProvider) {
	reefMu.Lock()
	defer reefMu.Unlock()
	reefProvider = p
}

func getReefProvider() ReefProvider {
	reefMu.RLock()
	defer reefMu.RUnlock()
	return reefProvider
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

func (h *Handler) registerReefRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/reef/status", h.handleReefStatus)
	mux.HandleFunc("GET /api/reef/tasks", h.handleReefTasks)
	mux.HandleFunc("GET /api/reef/tasks/{id}", h.handleReefTask)
	mux.HandleFunc("GET /api/reef/tasks/{id}/subtasks", h.handleReefSubTasks)
	mux.HandleFunc("POST /api/reef/tasks/{id}/cancel", h.handleReefCancelTask)
	mux.HandleFunc("GET /api/reef/clients", h.handleReefClients)
	mux.HandleFunc("GET /api/reef/events", h.handleReefEvents)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (h *Handler) handleReefStatus(w http.ResponseWriter, r *http.Request) {
	p := getReefProvider()
	if p == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "reef provider not available"})
		return
	}
	status, err := p.Status()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) handleReefTasks(w http.ResponseWriter, r *http.Request) {
	p := getReefProvider()
	if p == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "reef provider not available"})
		return
	}
	q := r.URL.Query()
	filter := ReefTaskFilter{
		Status: q.Get("status"),
		Role:   q.Get("role"),
		Search: q.Get("search"),
		Limit:  50,
		Offset: 0,
	}
	if l := q.Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &filter.Limit)
	}
	if o := q.Get("offset"); o != "" {
		fmt.Sscanf(o, "%d", &filter.Offset)
	}
	tasks, total, err := p.Tasks(filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
		"total": total,
	})
}

func (h *Handler) handleReefTask(w http.ResponseWriter, r *http.Request) {
	p := getReefProvider()
	if p == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "reef provider not available"})
		return
	}
	id := r.PathValue("id")
	task, err := p.TaskByID(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) handleReefSubTasks(w http.ResponseWriter, r *http.Request) {
	p := getReefProvider()
	if p == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "reef provider not available"})
		return
	}
	id := r.PathValue("id")
	tasks, err := p.SubTasks(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
	})
}

func (h *Handler) handleReefCancelTask(w http.ResponseWriter, r *http.Request) {
	p := getReefProvider()
	if p == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "reef provider not available"})
		return
	}
	id := r.PathValue("id")
	if err := p.CancelTask(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *Handler) handleReefClients(w http.ResponseWriter, r *http.Request) {
	p := getReefProvider()
	if p == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "reef provider not available"})
		return
	}
	clients, err := p.Clients()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"clients": clients,
	})
}

func (h *Handler) handleReefEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id, ch, done := sseHub.subscribe()
	defer sseHub.unsubscribe(id)

	// Send initial connected event
	fmt.Fprintf(w, "event: connected\ndata: {\"client_id\":%d}\n\n", id)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			return
		case data := <-ch:
			if _, err := w.Write(data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeJSON is a helper to write JSON responses.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Ensure unused import is fine.
