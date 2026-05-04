package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

//go:embed static
var staticFiles embed.FS

// ClientRegistry is the interface the UI needs to query connected clients.
type ClientRegistry interface {
	List() []*reef.ClientInfo
}

// TaskScheduler is the interface the UI needs to query tasks.
type TaskScheduler interface {
	TasksSnapshot() []*reef.Task
	GetTask(id string) *reef.Task
}

// Event represents an SSE event.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// EventBus manages SSE subscribers.
type EventBus struct {
	mu      sync.RWMutex
	clients map[chan Event]struct{}
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		clients: make(map[chan Event]struct{}),
	}
}

// Subscribe adds a new subscriber and returns its channel.
func (eb *EventBus) Subscribe() chan Event {
	ch := make(chan Event, 64)
	eb.mu.Lock()
	eb.clients[ch] = struct{}{}
	eb.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber.
func (eb *EventBus) Unsubscribe(ch chan Event) {
	eb.mu.Lock()
	delete(eb.clients, ch)
	eb.mu.Unlock()
	close(ch)
}

// Publish sends an event to all subscribers (non-blocking).
func (eb *EventBus) Publish(event Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for ch := range eb.clients {
		select {
		case ch <- event:
		default:
			// Drop if subscriber is slow
		}
	}
}

// Handler serves the Web UI and API endpoints.
type Handler struct {
	registry  ClientRegistry
	scheduler TaskScheduler
	startTime time.Time
	logger    *slog.Logger
	eventBus  *EventBus
}

// NewHandler creates a new UI handler.
func NewHandler(registry ClientRegistry, scheduler TaskScheduler, startTime time.Time, logger *slog.Logger) *Handler {
	return &Handler{
		registry:  registry,
		scheduler: scheduler,
		startTime: startTime,
		logger:    logger,
		eventBus:  NewEventBus(),
	}
}

// EventBus exposes the event bus for external publishers.
func (h *Handler) EventBus() *EventBus {
	return h.eventBus
}

// RegisterRoutes registers UI routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Create a sub-filesystem rooted at "static/" so that index.html is served at /ui/
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		h.logger.Error("failed to create sub filesystem", slog.String("error", err.Error()))
		return
	}

	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})
	mux.Handle("/ui/", http.StripPrefix("/ui", http.FileServerFS(subFS)))
	mux.HandleFunc("/api/v2/status", h.handleV2Status)
	mux.HandleFunc("/api/v2/tasks", h.handleV2Tasks)
	mux.HandleFunc("/api/v2/clients", h.handleV2Clients)
	mux.HandleFunc("/api/v2/events", h.handleEvents)
}

// V2StatusResponse is the JSON shape for /api/v2/status.
type V2StatusResponse struct {
	ServerVersion string      `json:"server_version"`
	StartTime     int64       `json:"start_time"`
	UptimeMs      int64       `json:"uptime_ms"`
	ClientCount   int         `json:"connected_clients"`
	QueueDepth    int         `json:"queue_depth"`
	TaskStats     V2TaskStats `json:"task_stats"`
}

// V2TaskStats holds aggregate task statistics.
type V2TaskStats struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Escalated int `json:"escalated"`
}

func (h *Handler) handleV2Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clients := h.registry.List()
	connectedCount := 0
	for _, c := range clients {
		if c.State == reef.ClientConnected {
			connectedCount++
		}
	}

	tasks := h.scheduler.TasksSnapshot()
	stats := V2TaskStats{}
	for _, t := range tasks {
		switch t.Status {
		case reef.TaskQueued:
			stats.Queued++
		case reef.TaskRunning, reef.TaskAssigned:
			stats.Running++
		case reef.TaskCompleted:
			stats.Completed++
		case reef.TaskFailed:
			stats.Failed++
		case reef.TaskCancelled:
			stats.Cancelled++
		case reef.TaskEscalated:
			stats.Escalated++
		}
	}

	resp := V2StatusResponse{
		ServerVersion: reef.ProtocolVersion,
		StartTime:     h.startTime.UnixMilli(),
		UptimeMs:      time.Since(h.startTime).Milliseconds(),
		ClientCount:   connectedCount,
		QueueDepth:    stats.Queued,
		TaskStats:     stats,
	}
	writeJSON(w, resp)
}

// V2TaskResponse is a single task in the v2 API response.
type V2TaskResponse struct {
	ID              string               `json:"id"`
	Status          string               `json:"status"`
	Instruction     string               `json:"instruction"`
	RequiredRole    string               `json:"required_role"`
	RequiredSkills  []string             `json:"required_skills,omitempty"`
	AssignedClient  string               `json:"assigned_client,omitempty"`
	CreatedAt       int64                `json:"created_at"`
	StartedAt       *int64               `json:"started_at,omitempty"`
	CompletedAt     *int64               `json:"completed_at,omitempty"`
	Result          *reef.TaskResult     `json:"result,omitempty"`
	Error           *reef.TaskError      `json:"error,omitempty"`
	AttemptHistory  []reef.AttemptRecord `json:"attempt_history,omitempty"`
	EscalationCount int                  `json:"escalation_count"`
}

// V2TasksResponse is the paginated response for /api/v2/tasks.
type V2TasksResponse struct {
	Tasks  []V2TaskResponse `json:"tasks"`
	Total  int              `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

func (h *Handler) handleV2Tasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	filterStatus := q.Get("status")
	filterRole := q.Get("role")
	limit := 50
	offset := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	allTasks := h.scheduler.TasksSnapshot()
	var filtered []V2TaskResponse
	for _, t := range allTasks {
		if filterStatus != "" && string(t.Status) != filterStatus {
			continue
		}
		if filterRole != "" && t.RequiredRole != filterRole {
			continue
		}
		tr := V2TaskResponse{
			ID:              t.ID,
			Status:          string(t.Status),
			Instruction:     t.Instruction,
			RequiredRole:    t.RequiredRole,
			RequiredSkills:  t.RequiredSkills,
			AssignedClient:  t.AssignedClient,
			CreatedAt:       t.CreatedAt.UnixMilli(),
			Result:          t.Result,
			Error:           t.Error,
			AttemptHistory:  t.AttemptHistory,
			EscalationCount: t.EscalationCount,
		}
		if t.StartedAt != nil {
			ts := t.StartedAt.UnixMilli()
			tr.StartedAt = &ts
		}
		if t.CompletedAt != nil {
			ts := t.CompletedAt.UnixMilli()
			tr.CompletedAt = &ts
		}
		filtered = append(filtered, tr)
	}

	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := filtered[offset:end]

	resp := V2TasksResponse{
		Tasks:  page,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}
	writeJSON(w, resp)
}

// V2ClientResponse is a single client in the v2 API.
type V2ClientResponse struct {
	ID            string   `json:"id"`
	Role          string   `json:"role"`
	Skills        []string `json:"skills"`
	State         string   `json:"state"`
	CurrentLoad   int      `json:"load"`
	LastHeartbeat int64    `json:"last_heartbeat"`
}

func (h *Handler) handleV2Clients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clients := h.registry.List()
	out := make([]V2ClientResponse, 0, len(clients))
	for _, c := range clients {
		out = append(out, V2ClientResponse{
			ID:            c.ID,
			Role:          c.Role,
			Skills:        c.Skills,
			State:         string(c.State),
			CurrentLoad:   c.CurrentLoad,
			LastHeartbeat: c.LastHeartbeat.UnixMilli(),
		})
	}
	writeJSON(w, out)
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := h.eventBus.Subscribe()
	defer h.eventBus.Unsubscribe(ch)

	ctx := r.Context()

	// Send initial stats
	statsEvent := h.buildStatsEvent()
	fmt.Fprintf(w, "event: stats_update\ndata: %s\n\n", mustJSON(statsEvent))
	flusher.Flush()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-ch:
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, mustJSON(event.Data))
			flusher.Flush()
		case <-ticker.C:
			statsEvt := h.buildStatsEvent()
			fmt.Fprintf(w, "event: stats_update\ndata: %s\n\n", mustJSON(statsEvt))
			flusher.Flush()
		}
	}
}

func (h *Handler) buildStatsEvent() V2StatusResponse {
	clients := h.registry.List()
	connectedCount := 0
	for _, c := range clients {
		if c.State == reef.ClientConnected {
			connectedCount++
		}
	}

	tasks := h.scheduler.TasksSnapshot()
	stats := V2TaskStats{}
	for _, t := range tasks {
		switch t.Status {
		case reef.TaskQueued:
			stats.Queued++
		case reef.TaskRunning, reef.TaskAssigned:
			stats.Running++
		case reef.TaskCompleted:
			stats.Completed++
		case reef.TaskFailed:
			stats.Failed++
		case reef.TaskCancelled:
			stats.Cancelled++
		case reef.TaskEscalated:
			stats.Escalated++
		}
	}

	return V2StatusResponse{
		ServerVersion: reef.ProtocolVersion,
		StartTime:     h.startTime.UnixMilli(),
		UptimeMs:      time.Since(h.startTime).Milliseconds(),
		ClientCount:   connectedCount,
		QueueDepth:    stats.Queued,
		TaskStats:     stats,
	}
}

// PublishTaskUpdate publishes a task_update event.
func (h *Handler) PublishTaskUpdate(task *reef.Task) {
	h.eventBus.Publish(Event{
		Type: "task_update",
		Data: V2TaskResponse{
			ID:              task.ID,
			Status:          string(task.Status),
			Instruction:     task.Instruction,
			RequiredRole:    task.RequiredRole,
			RequiredSkills:  task.RequiredSkills,
			AssignedClient:  task.AssignedClient,
			CreatedAt:       task.CreatedAt.UnixMilli(),
			EscalationCount: task.EscalationCount,
		},
	})
}

// PublishClientUpdate publishes a client_update event.
func (h *Handler) PublishClientUpdate(client *reef.ClientInfo) {
	h.eventBus.Publish(Event{
		Type: "client_update",
		Data: V2ClientResponse{
			ID:            client.ID,
			Role:          client.Role,
			Skills:        client.Skills,
			State:         string(client.State),
			CurrentLoad:   client.CurrentLoad,
			LastHeartbeat: client.LastHeartbeat.UnixMilli(),
		},
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
