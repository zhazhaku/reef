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
	Get(id string) *reef.ClientInfo
}

// ClientController is an optional interface for client lifecycle control.
// Implementations may support pause/resume/restart operations.
type ClientController interface {
	Pause(id string) error
	Resume(id string) error
	Restart(id string) error
}

// TaskScheduler is the interface the UI needs to query tasks.
type TaskScheduler interface {
	TasksSnapshot() []*reef.Task
	GetTask(id string) *reef.Task
}

// TaskBoardMover is an optional interface for moving tasks between board columns.
type TaskBoardMover interface {
	MoveTask(taskID string, newStatus reef.TaskStatus) error
}

// TaskDecomposer is an optional interface for task decomposition.
type TaskDecomposer interface {
	Decompose(taskID string) ([]TaskNode, error)
	CreateDecompose(taskID string, nodes []TaskNode) ([]TaskNode, error)
}

// ChatStore manages chat messages per task.
type ChatStore interface {
	Messages(taskID string) []ChatMessage
	Send(taskID string, msg ChatMessage)
}

// ChatMessage represents a single message in a task chatroom.
type ChatMessage struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"task_id"`
	Sender      string    `json:"sender"`
	SenderType  string    `json:"sender_type"` // "user", "agent", "system"
	Content     string    `json:"content"`
	ContentType string    `json:"content_type"` // "text", "code", "image", "file"
	Timestamp   time.Time `json:"timestamp"`
}

// TaskNode represents a node in a task decomposition tree.
type TaskNode struct {
	ID          string     `json:"id"`
	Instruction string     `json:"instruction"`
	Assignee    string     `json:"assignee,omitempty"`
	Status      string     `json:"status"`
	Children    []TaskNode `json:"children,omitempty"`
	ParentID    string     `json:"parent_id,omitempty"`
}

// MemoryChatStore is an in-memory implementation of ChatStore.
type MemoryChatStore struct {
	mu       sync.RWMutex
	messages map[string][]ChatMessage
}

// NewMemoryChatStore creates a new MemoryChatStore.
func NewMemoryChatStore() *MemoryChatStore {
	return &MemoryChatStore{
		messages: make(map[string][]ChatMessage),
	}
}

// Messages returns all messages for a given task ID.
func (s *MemoryChatStore) Messages(taskID string) []ChatMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs, ok := s.messages[taskID]
	if !ok {
		return []ChatMessage{}
	}
	// Return a copy to avoid data races
	out := make([]ChatMessage, len(msgs))
	copy(out, msgs)
	return out
}

// Send appends a message to the given task's chat.
func (s *MemoryChatStore) Send(taskID string, msg ChatMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[taskID] = append(s.messages[taskID], msg)
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
	registry    ClientRegistry
	scheduler   TaskScheduler
	startTime   time.Time
	logger      *slog.Logger
	eventBus    *EventBus
	chatStore   ChatStore
}

// NewHandler creates a new UI handler.
func NewHandler(registry ClientRegistry, scheduler TaskScheduler, startTime time.Time, logger *slog.Logger) *Handler {
	return &Handler{
		registry:  registry,
		scheduler: scheduler,
		startTime: startTime,
		logger:    logger,
		eventBus:  NewEventBus(),
		chatStore: NewMemoryChatStore(),
	}
}

// ChatStore returns the handler's chat store for external use.
func (h *Handler) ChatStore() ChatStore {
	return h.chatStore
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

	// Wave 1.1: Client detail/control routes
	mux.HandleFunc("/api/v2/client/", h.routeV2Client)

	// Wave 1.2: Board routes
	mux.HandleFunc("/api/v2/board", h.handleV2Board)
	mux.HandleFunc("/api/v2/board/move", h.handleV2BoardMove)

	// Wave 1.3: Chatroom routes
	mux.HandleFunc("/api/v2/chatroom/", h.routeV2Chatroom)

	// Wave 1.4: Task decomposition routes
	mux.HandleFunc("/api/v2/tasks/", h.routeV2TaskSub)

	// Wave 1.5 + 1.6: Evolution, Activity, Config, Hermes, Logs
	h.RegisterV2Routes(mux)
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

// ---------------------------------------------------------------------------
// Wave 1.1: Client Detail / Session / Control
// ---------------------------------------------------------------------------

// V2ClientDetailResponse is the full client detail response.
type V2ClientDetailResponse struct {
	ID            string   `json:"id"`
	Role          string   `json:"role"`
	Skills        []string `json:"skills"`
	Providers     []string `json:"providers,omitempty"`
	Capacity      int      `json:"capacity"`
	CurrentLoad   int      `json:"current_load"`
	State         string   `json:"state"`
	LastHeartbeat int64    `json:"last_heartbeat"`
	CurrentTaskID string   `json:"current_task_id,omitempty"`
}

// routeV2Client dispatches /api/v2/client/{id}[/*] to the appropriate handler.
func (h *Handler) routeV2Client(w http.ResponseWriter, r *http.Request) {
	// Strip the prefix "/api/v2/client/" and parse path segments.
	path := r.URL.Path[len("/api/v2/client/"):]
	if path == "" || path == "/" {
		http.Error(w, "client ID required", http.StatusBadRequest)
		return
	}

	// Split into id and rest
	rest := ""
	id := path
	for i, c := range path {
		if c == '/' {
			id = path[:i]
			rest = path[i+1:]
			break
		}
	}

	// Trim trailing slash from rest
	if rest != "" && rest[len(rest)-1] == '/' {
		rest = rest[:len(rest)-1]
	}

	switch rest {
	case "":
		h.handleV2ClientDetail(w, r, id)
	case "session":
		h.handleV2ClientSession(w, r, id)
	case "pause":
		h.handleV2ClientPause(w, r, id)
	case "resume":
		h.handleV2ClientResume(w, r, id)
	case "restart":
		h.handleV2ClientRestart(w, r, id)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *Handler) handleV2ClientDetail(w http.ResponseWriter, r *http.Request, clientID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client := h.registry.Get(clientID)
	if client == nil {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	// Find the current task assigned to this client
	currentTaskID := ""
	tasks := h.scheduler.TasksSnapshot()
	for _, t := range tasks {
		if t.AssignedClient == clientID && !t.Status.IsTerminal() {
			currentTaskID = t.ID
			break
		}
	}

	resp := V2ClientDetailResponse{
		ID:            client.ID,
		Role:          client.Role,
		Skills:        client.Skills,
		Providers:     client.Providers,
		Capacity:      client.Capacity,
		CurrentLoad:   client.CurrentLoad,
		State:         string(client.State),
		LastHeartbeat: client.LastHeartbeat.UnixMilli(),
		CurrentTaskID: currentTaskID,
	}
	writeJSON(w, resp)
}

func (h *Handler) handleV2ClientSession(w http.ResponseWriter, r *http.Request, clientID string) {
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

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-ch:
			// Filter: only forward events relevant to this client
			if !isClientEvent(event, clientID) {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, mustJSON(event.Data))
			flusher.Flush()
		case <-ticker.C:
			// Send periodic heartbeat/ping
			client := h.registry.Get(clientID)
			if client == nil {
				fmt.Fprintf(w, "event: client_disconnect\ndata: %s\n\n", mustJSON(map[string]string{"id": clientID}))
				flusher.Flush()
				return
			}
			fmt.Fprintf(w, "event: client_heartbeat\ndata: %s\n\n", mustJSON(V2ClientResponse{
				ID:            client.ID,
				Role:          client.Role,
				Skills:        client.Skills,
				State:         string(client.State),
				CurrentLoad:   client.CurrentLoad,
				LastHeartbeat: client.LastHeartbeat.UnixMilli(),
			}))
			flusher.Flush()
		}
	}
}

// isClientEvent checks if an event is relevant to a specific client.
func isClientEvent(event Event, clientID string) bool {
	switch event.Type {
	case "client_update":
		if data, ok := event.Data.(V2ClientResponse); ok {
			return data.ID == clientID
		}
	case "task_update":
		if data, ok := event.Data.(V2TaskResponse); ok {
			return data.AssignedClient == clientID
		}
	}
	return false
}

func (h *Handler) handleV2ClientPause(w http.ResponseWriter, r *http.Request, clientID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client := h.registry.Get(clientID)
	if client == nil {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	if cc, ok := h.registry.(ClientController); ok {
		if err := cc.Pause(clientID); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"status": "paused", "client_id": clientID})
		return
	}

	http.Error(w, "pause not implemented", http.StatusNotImplemented)
}

func (h *Handler) handleV2ClientResume(w http.ResponseWriter, r *http.Request, clientID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client := h.registry.Get(clientID)
	if client == nil {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	if cc, ok := h.registry.(ClientController); ok {
		if err := cc.Resume(clientID); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"status": "resumed", "client_id": clientID})
		return
	}

	http.Error(w, "resume not implemented", http.StatusNotImplemented)
}

func (h *Handler) handleV2ClientRestart(w http.ResponseWriter, r *http.Request, clientID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client := h.registry.Get(clientID)
	if client == nil {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	if cc, ok := h.registry.(ClientController); ok {
		if err := cc.Restart(clientID); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"status": "restarting", "client_id": clientID})
		return
	}

	http.Error(w, "restart not implemented", http.StatusNotImplemented)
}

// ---------------------------------------------------------------------------
// Wave 1.2: Board API
// ---------------------------------------------------------------------------

// V2BoardResponse is the response for /api/v2/board.
type V2BoardResponse struct {
	Backlog    []V2TaskResponse `json:"backlog"`
	InProgress []V2TaskResponse `json:"in_progress"`
	Review     []V2TaskResponse `json:"review"`
	Done       []V2TaskResponse `json:"done"`
}

// V2BoardMoveRequest is the request body for /api/v2/board/move.
type V2BoardMoveRequest struct {
	TaskID    string `json:"task_id"`
	NewStatus string `json:"new_status"`
}

func (h *Handler) handleV2Board(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tasks := h.scheduler.TasksSnapshot()
	board := V2BoardResponse{
		Backlog:    make([]V2TaskResponse, 0),
		InProgress: make([]V2TaskResponse, 0),
		Review:     make([]V2TaskResponse, 0),
		Done:       make([]V2TaskResponse, 0),
	}

	for _, t := range tasks {
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

		switch t.Status {
		case reef.TaskCreated, reef.TaskQueued, reef.TaskBlocked:
			board.Backlog = append(board.Backlog, tr)
		case reef.TaskAssigned, reef.TaskRunning, reef.TaskRecovering:
			board.InProgress = append(board.InProgress, tr)
		case reef.TaskEscalated:
			board.Review = append(board.Review, tr)
		case reef.TaskCompleted, reef.TaskFailed, reef.TaskCancelled:
			board.Done = append(board.Done, tr)
		default:
			board.Backlog = append(board.Backlog, tr)
		}
	}

	writeJSON(w, board)
}

func (h *Handler) handleV2BoardMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req V2BoardMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.TaskID == "" || req.NewStatus == "" {
		http.Error(w, "task_id and new_status are required", http.StatusBadRequest)
		return
	}

	// Try using TaskBoardMover if available
	if bm, ok := h.scheduler.(TaskBoardMover); ok {
		if err := bm.MoveTask(req.TaskID, reef.TaskStatus(req.NewStatus)); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"status": "moved", "task_id": req.TaskID, "new_status": req.NewStatus})
		return
	}

	// Fallback: verify task exists and status is valid
	task := h.scheduler.GetTask(req.TaskID)
	if task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	if !task.Status.CanTransitionTo(reef.TaskStatus(req.NewStatus)) {
		writeJSON(w, map[string]string{"error": fmt.Sprintf("invalid transition from %s to %s", task.Status, req.NewStatus)})
		return
	}

	writeJSON(w, map[string]string{"status": "moved", "task_id": req.TaskID, "new_status": req.NewStatus})
}

// ---------------------------------------------------------------------------
// Wave 1.3: Chatroom API
// ---------------------------------------------------------------------------

// routeV2Chatroom dispatches /api/v2/chatroom/{task_id}[/send].
func (h *Handler) routeV2Chatroom(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/v2/chatroom/"):]
	if path == "" || path == "/" {
		http.Error(w, "task ID required", http.StatusBadRequest)
		return
	}

	rest := ""
	taskID := path
	for i, c := range path {
		if c == '/' {
			taskID = path[:i]
			rest = path[i+1:]
			break
		}
	}

	if rest != "" && rest[len(rest)-1] == '/' {
		rest = rest[:len(rest)-1]
	}

	switch rest {
	case "":
		h.handleV2ChatroomMessages(w, r, taskID)
	case "send":
		h.handleV2ChatroomSend(w, r, taskID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *Handler) handleV2ChatroomMessages(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	messages := h.chatStore.Messages(taskID)
	writeJSON(w, map[string]any{
		"task_id":  taskID,
		"messages": messages,
		"total":    len(messages),
	})
}

// V2ChatSendRequest is the request body for sending a chat message.
type V2ChatSendRequest struct {
	Sender      string `json:"sender"`
	SenderType  string `json:"sender_type"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
}

func (h *Handler) handleV2ChatroomSend(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req V2ChatSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	// Default values
	if req.SenderType == "" {
		req.SenderType = "user"
	}
	if req.ContentType == "" {
		req.ContentType = "text"
	}

	msg := ChatMessage{
		ID:          fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		TaskID:      taskID,
		Sender:      req.Sender,
		SenderType:  req.SenderType,
		Content:     req.Content,
		ContentType: req.ContentType,
		Timestamp:   time.Now(),
	}

	h.chatStore.Send(taskID, msg)

	// Publish chat event
	h.eventBus.Publish(Event{
		Type: "chat_message",
		Data: msg,
	})

	writeJSON(w, msg)
}

// ---------------------------------------------------------------------------
// Wave 1.4: Task Decomposition API
// ---------------------------------------------------------------------------

// routeV2TaskSub dispatches /api/v2/tasks/{id}[/decompose].
func (h *Handler) routeV2TaskSub(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/v2/tasks/"):]
	if path == "" || path == "/" {
		http.Error(w, "task ID required", http.StatusBadRequest)
		return
	}

	rest := ""
	taskID := path
	for i, c := range path {
		if c == '/' {
			taskID = path[:i]
			rest = path[i+1:]
			break
		}
	}

	if rest != "" && rest[len(rest)-1] == '/' {
		rest = rest[:len(rest)-1]
	}

	switch rest {
	case "decompose":
		h.handleV2TaskDecompose(w, r, taskID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *Handler) handleV2TaskDecompose(w http.ResponseWriter, r *http.Request, taskID string) {
	switch r.Method {
	case http.MethodGet:
		h.handleV2TaskDecomposeGet(w, r, taskID)
	case http.MethodPost:
		h.handleV2TaskDecomposeCreate(w, r, taskID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// V2DecomposeResponse is the response for task decomposition.
type V2DecomposeResponse struct {
	TaskID string     `json:"task_id"`
	Nodes  []TaskNode `json:"nodes"`
}

func (h *Handler) handleV2TaskDecomposeGet(w http.ResponseWriter, r *http.Request, taskID string) {
	task := h.scheduler.GetTask(taskID)
	if task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// Build decomposition tree from task's SubTaskIDs
	nodes := buildDecomposeTree(task, h.scheduler)

	writeJSON(w, V2DecomposeResponse{
		TaskID: taskID,
		Nodes:  nodes,
	})
}

// buildDecomposeTree recursively builds a TaskNode tree from task subtask IDs.
func buildDecomposeTree(task *reef.Task, scheduler TaskScheduler) []TaskNode {
	if len(task.SubTaskIDs) == 0 {
		return []TaskNode{}
	}

	nodes := make([]TaskNode, 0, len(task.SubTaskIDs))
	for _, subID := range task.SubTaskIDs {
		subTask := scheduler.GetTask(subID)
		if subTask == nil {
			continue
		}
		node := TaskNode{
			ID:          subTask.ID,
			Instruction: subTask.Instruction,
			Assignee:    subTask.AssignedClient,
			Status:      string(subTask.Status),
			ParentID:    subTask.ParentTaskID,
			Children:    buildDecomposeTree(subTask, scheduler),
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// V2DecomposeCreateRequest is the request body for creating a decomposition.
type V2DecomposeCreateRequest struct {
	Nodes []TaskNode `json:"nodes"`
}

func (h *Handler) handleV2TaskDecomposeCreate(w http.ResponseWriter, r *http.Request, taskID string) {
	task := h.scheduler.GetTask(taskID)
	if task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	var req V2DecomposeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Try TaskDecomposer if available
	if td, ok := h.scheduler.(TaskDecomposer); ok {
		nodes, err := td.CreateDecompose(taskID, req.Nodes)
		if err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, V2DecomposeResponse{
			TaskID: taskID,
			Nodes:  nodes,
		})
		return
	}

	// Fallback: return the submitted nodes as-is (acknowledgment only)
	writeJSON(w, V2DecomposeResponse{
		TaskID: taskID,
		Nodes:  req.Nodes,
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
