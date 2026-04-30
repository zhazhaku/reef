package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

// AdminServer exposes HTTP endpoints for observability and control.
type AdminServer struct {
	registry  *Registry
	scheduler *Scheduler
	token     string // Bearer token for admin API authentication; empty = no auth
	logger    *slog.Logger
}

// NewAdminServer creates an admin HTTP handler.
func NewAdminServer(registry *Registry, scheduler *Scheduler, token string, logger *slog.Logger) *AdminServer {
	return &AdminServer{
		registry:  registry,
		scheduler: scheduler,
		token:     token,
		logger:    logger,
	}
}

// RegisterRoutes registers admin routes on the provided ServeMux.
// UI routes are registered separately via ui.Handler.RegisterRoutes.
func (a *AdminServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/status", a.authMiddleware(a.handleStatus))
	mux.HandleFunc("/admin/tasks", a.authMiddleware(a.handleTasks))
	mux.HandleFunc("/tasks", a.authMiddleware(a.handleSubmitTask))
}

// authMiddleware wraps a handler with Bearer token authentication.
// If no token is configured, authentication is skipped (dev mode).
func (a *AdminServer) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.token == "" {
			next(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+a.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// StatusResponse is the JSON shape for /admin/status.
type StatusResponse struct {
	ServerVersion     string                `json:"server_version"`
	StartTime         int64                 `json:"start_time"`
	UptimeMs          int64                 `json:"uptime_ms"`
	ConnectedClients  []*reef.ClientInfo    `json:"connected_clients"`
	DisconnectedCount int                   `json:"disconnected_count"`
	StaleCount        int                   `json:"stale_count"`
}

func (a *AdminServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	all := a.registry.List()
	var connected []*reef.ClientInfo
	discCount, staleCount := 0, 0
	for _, c := range all {
		switch c.State {
		case reef.ClientConnected:
			connected = append(connected, c)
		case reef.ClientDisconnected:
			discCount++
		case reef.ClientStale:
			staleCount++
		}
	}

	resp := StatusResponse{
		ServerVersion:     reef.ProtocolVersion,
		StartTime:         serverStartTime.UnixMilli(),
		UptimeMs:          time.Since(serverStartTime).Milliseconds(),
		ConnectedClients:  connected,
		DisconnectedCount: discCount,
		StaleCount:        staleCount,
	}
	writeJSON(w, resp)
}

// TasksResponse is the JSON shape for /admin/tasks.
type TasksResponse struct {
	QueuedTasks    []TaskSummary `json:"queued_tasks"`
	InflightTasks  []TaskSummary `json:"inflight_tasks"`
	CompletedTasks []TaskSummary `json:"completed_tasks"`
	Stats          TaskStats     `json:"stats"`
}

// TaskSummary is a lightweight representation of a task for the API.
type TaskSummary struct {
	TaskID         string           `json:"task_id"`
	Status         reef.TaskStatus  `json:"status"`
	RequiredRole   string           `json:"required_role"`
	RequiredSkills []string         `json:"required_skills"`
	Priority       int              `json:"priority"`
	AssignedClient string           `json:"assigned_client_id,omitempty"`
	CreatedAt      int64            `json:"created_at"`
	StartedAt      *int64           `json:"started_at,omitempty"`
	CompletedAt    *int64           `json:"completed_at,omitempty"`
}

// TaskStats holds aggregate statistics.
type TaskStats struct {
	Total     int `json:"total"`
	Success   int `json:"success"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Queued    int `json:"queued"`
	Running   int `json:"running"`
}

func (a *AdminServer) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filterStatus := r.URL.Query().Get("status")
	filterRole := r.URL.Query().Get("role")
	filterPriority := r.URL.Query().Get("priority")

	allTasks := a.scheduler.TasksSnapshot()
	var resp TasksResponse
	stats := TaskStats{Total: len(allTasks)}

	// Keep last 100 completed
	var completed []TaskSummary

	for _, t := range allTasks {
		if filterRole != "" && t.RequiredRole != filterRole {
			continue
		}
		if filterStatus != "" && string(t.Status) != filterStatus {
			continue
		}
		if filterPriority != "" {
			var p int
			fmt.Sscanf(filterPriority, "%d", &p)
			if t.Priority != p {
				continue
			}
		}

		sum := TaskSummary{
			TaskID:         t.ID,
			Status:         t.Status,
			RequiredRole:   t.RequiredRole,
			RequiredSkills: t.RequiredSkills,
			Priority:       t.Priority,
			AssignedClient: t.AssignedClient,
			CreatedAt:      t.CreatedAt.UnixMilli(),
		}
		if t.StartedAt != nil {
			ts := t.StartedAt.UnixMilli()
			sum.StartedAt = &ts
		}
		if t.CompletedAt != nil {
			ts := t.CompletedAt.UnixMilli()
			sum.CompletedAt = &ts
		}

		switch t.Status {
		case reef.TaskQueued:
			resp.QueuedTasks = append(resp.QueuedTasks, sum)
			stats.Queued++
		case reef.TaskRunning, reef.TaskAssigned, reef.TaskPaused:
			resp.InflightTasks = append(resp.InflightTasks, sum)
			if t.Status == reef.TaskRunning {
				stats.Running++
			}
		case reef.TaskCompleted:
			completed = append(completed, sum)
			stats.Success++
		case reef.TaskFailed:
			completed = append(completed, sum)
			stats.Failed++
		case reef.TaskCancelled:
			completed = append(completed, sum)
			stats.Cancelled++
		}
	}

	// Ring buffer: keep last 100
	if len(completed) > 100 {
		completed = completed[len(completed)-100:]
	}
	resp.CompletedTasks = completed
	resp.Stats = stats

	writeJSON(w, resp)
}

// SubmitTaskRequest is the payload for POST /tasks.
type SubmitTaskRequest struct {
	Instruction    string   `json:"instruction"`
	RequiredRole   string   `json:"required_role"`
	RequiredSkills []string `json:"required_skills,omitempty"`
	MaxRetries     int      `json:"max_retries,omitempty"`
	TimeoutMs      int64    `json:"timeout_ms,omitempty"`
	ModelHint      string   `json:"model_hint,omitempty"`
}

func (a *AdminServer) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SubmitTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Instruction == "" {
		http.Error(w, "instruction is required", http.StatusBadRequest)
		return
	}
	if req.RequiredRole == "" {
		http.Error(w, "required_role is required", http.StatusBadRequest)
		return
	}

	task := reef.NewTask(
		generateTaskID(),
		req.Instruction,
		req.RequiredRole,
		req.RequiredSkills,
	)
	if req.MaxRetries > 0 {
		task.MaxRetries = req.MaxRetries
	}
	if req.TimeoutMs > 0 {
		task.TimeoutMs = req.TimeoutMs
	}
	if req.ModelHint != "" {
		task.ModelHint = req.ModelHint
	}

	if err := a.scheduler.Submit(task); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.logger.Info("task submitted",
		slog.String("task_id", task.ID),
		slog.String("role", task.RequiredRole))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"task_id": task.ID,
		"status":  string(task.Status),
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var (
	serverStartTime = time.Now()
	taskIDCounter   = 0
	taskIDMu        sync.Mutex
)

func generateTaskID() string {
	taskIDMu.Lock()
	defer taskIDMu.Unlock()
	taskIDCounter++
	return "task-" + strconv.Itoa(taskIDCounter) + "-" + strconv.FormatInt(time.Now().UnixMilli(), 36)
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
