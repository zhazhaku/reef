// Package e2e provides end-to-end integration tests for Reef.
// It spins up real Server and Client components over actual WebSocket connections.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server"
)

// E2EServer wraps a real Reef Server with test helpers.
type E2EServer struct {
	*server.Server
	WSAddr    string
	AdminAddr string
	token     string
}

// NewE2EServer creates a Reef Server on ephemeral ports.
func NewE2EServer(t *testing.T, token string) *E2EServer {
	// Find two ephemeral ports
	wsPort := getFreePort(t)
	adminPort := getFreePort(t)

	cfg := server.Config{
		WebSocketAddr:    fmt.Sprintf("127.0.0.1:%d", wsPort),
		AdminAddr:        fmt.Sprintf("127.0.0.1:%d", adminPort),
		Token:            token,
		HeartbeatTimeout: 5 * time.Second,
		HeartbeatScan:    1 * time.Second,
		QueueMaxLen:      100,
		QueueMaxAge:      5 * time.Minute,
		MaxEscalations:   1,
	}

	srv := server.NewServer(cfg, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}

	// Wait for listeners to be ready
	time.Sleep(100 * time.Millisecond)

	return &E2EServer{
		Server:    srv,
		WSAddr:    cfg.WebSocketAddr,
		AdminAddr: cfg.AdminAddr,
		token:     token,
	}
}

// WSURL returns the WebSocket URL for clients to connect.
func (s *E2EServer) WSURL() string {
	return fmt.Sprintf("ws://%s/ws", s.WSAddr)
}

// AdminURL returns the base HTTP URL for admin endpoints.
func (s *E2EServer) AdminURL() string {
	return fmt.Sprintf("http://%s", s.AdminAddr)
}

// Shutdown gracefully stops the server.
func (s *E2EServer) Shutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Stop(); err != nil {
		t.Logf("server shutdown error: %v", err)
	}
	_ = ctx
}

// setAuth adds the Authorization header if a token is configured.
func (s *E2EServer) setAuth(req *http.Request) {
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
}

// SubmitTask submits a task via the Admin HTTP API.
func (s *E2EServer) SubmitTask(t *testing.T, instruction, requiredRole string, skills []string) string {
	reqBody, _ := json.Marshal(map[string]any{
		"instruction":     instruction,
		"required_role":   requiredRole,
		"required_skills": skills,
		"max_retries":     1,
	})

	req, _ := http.NewRequest(http.MethodPost, s.AdminURL()+"/tasks", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	s.setAuth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit task: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("submit task: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	return result["task_id"]
}

// SubmitTaskWithModelHint submits a task with a model_hint via the Admin HTTP API.
func (s *E2EServer) SubmitTaskWithModelHint(t *testing.T, instruction, requiredRole string, skills []string, modelHint string) string {
	reqBody, _ := json.Marshal(map[string]any{
		"instruction":     instruction,
		"required_role":   requiredRole,
		"required_skills": skills,
		"max_retries":     1,
		"model_hint":      modelHint,
	})

	req, _ := http.NewRequest(http.MethodPost, s.AdminURL()+"/tasks", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	s.setAuth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit task with model_hint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("submit task with model_hint: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	return result["task_id"]
}

// GetStatus queries /admin/status.
func (s *E2EServer) GetStatus(t *testing.T) *StatusResponse {
	req, _ := http.NewRequest(http.MethodGet, s.AdminURL()+"/admin/status", nil)
	s.setAuth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return &status
}

// GetStatusRaw returns the raw HTTP response for /admin/status (for auth testing).
func (s *E2EServer) GetStatusRaw(t *testing.T, authHeader string) *http.Response {
	req, _ := http.NewRequest(http.MethodGet, s.AdminURL()+"/admin/status", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get status raw: %v", err)
	}
	return resp
}

// GetTasks queries /admin/tasks with optional filters.
func (s *E2EServer) GetTasks(t *testing.T, role, taskStatus string) *TasksResponse {
	url := s.AdminURL() + "/admin/tasks"
	if role != "" || taskStatus != "" {
		url += "?"
		if role != "" {
			url += "role=" + role + "&"
		}
		if taskStatus != "" {
			url += "status=" + taskStatus + "&"
		}
	}

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	s.setAuth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get tasks: %v", err)
	}
	defer resp.Body.Close()

	var tasks TasksResponse
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode tasks: %v", err)
	}
	return &tasks
}

// WaitForTaskStatus polls /admin/tasks until the task reaches the expected status.
func (s *E2EServer) WaitForTaskStatus(t *testing.T, taskID string, expected reef.TaskStatus, timeout time.Duration) *TaskSummary {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tasks := s.GetTasks(t, "", "")
		all := append(tasks.QueuedTasks, tasks.InflightTasks...)
		all = append(all, tasks.CompletedTasks...)
		for _, ts := range all {
			if ts.TaskID == taskID && ts.Status == expected {
				return &ts
			}
		}
		// Also check scheduler directly for faster access
		if task := s.Scheduler().GetTask(taskID); task != nil && task.Status == expected {
			return &TaskSummary{
				TaskID:         task.ID,
				Status:         task.Status,
				RequiredRole:   task.RequiredRole,
				RequiredSkills: task.RequiredSkills,
				AssignedClient: task.AssignedClient,
				CreatedAt:      task.CreatedAt.UnixMilli(),
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for task %s to reach status %s", taskID, expected)
	return nil
}

// StatusResponse mirrors server.AdminServer's response shape.
type StatusResponse struct {
	ServerVersion     string             `json:"server_version"`
	StartTime         int64              `json:"start_time"`
	UptimeMs          int64              `json:"uptime_ms"`
	ConnectedClients  []*reef.ClientInfo `json:"connected_clients"`
	DisconnectedCount int                `json:"disconnected_count"`
	StaleCount        int                `json:"stale_count"`
}

// TasksResponse mirrors server.AdminServer's tasks response.
type TasksResponse struct {
	QueuedTasks    []TaskSummary `json:"queued_tasks"`
	InflightTasks  []TaskSummary `json:"inflight_tasks"`
	CompletedTasks []TaskSummary `json:"completed_tasks"`
	Stats          TaskStats     `json:"stats"`
}

// TaskSummary mirrors server.TaskSummary.
type TaskSummary struct {
	TaskID         string          `json:"task_id"`
	Status         reef.TaskStatus `json:"status"`
	RequiredRole   string          `json:"required_role"`
	RequiredSkills []string        `json:"required_skills"`
	AssignedClient string          `json:"assigned_client_id,omitempty"`
	CreatedAt      int64           `json:"created_at"`
	StartedAt      *int64          `json:"started_at,omitempty"`
	CompletedAt    *int64          `json:"completed_at,omitempty"`
}

// TaskStats mirrors server.TaskStats.
type TaskStats struct {
	Total     int `json:"total"`
	Success   int `json:"success"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Queued    int `json:"queued"`
	Running   int `json:"running"`
}

// getFreePort returns an available TCP port.
func getFreePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("get free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitFor waits until the condition returns true or timeout.
func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("waitFor timeout")
}
