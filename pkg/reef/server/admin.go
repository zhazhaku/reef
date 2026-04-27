package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

// AdminServer exposes HTTP endpoints for observability and control.
type AdminServer struct {
	registry  *Registry
	scheduler *Scheduler
	logger    *slog.Logger
}

// NewAdminServer creates an admin HTTP handler.
func NewAdminServer(registry *Registry, scheduler *Scheduler, logger *slog.Logger) *AdminServer {
	return &AdminServer{
		registry:  registry,
		scheduler: scheduler,
		logger:    logger,
	}
}

// RegisterRoutes registers admin routes on the provided ServeMux.
func (a *AdminServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/status", a.handleStatus)
	mux.HandleFunc("/admin/tasks", a.handleTasks)
	mux.HandleFunc("/tasks", a.handleSubmitTask)
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

		sum := TaskSummary{
			TaskID:         t.ID,
			Status:         t.Status,
			RequiredRole:   t.RequiredRole,
			RequiredSkills: t.RequiredSkills,
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
