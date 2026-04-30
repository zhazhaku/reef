package perf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef/server"
)

// PerfServer wraps a Reef server for performance testing.
type PerfServer struct {
	Server    *server.Server
	AdminAddr string
	WSAddr    string
}

// NewPerfServer starts a Reef server on ephemeral ports for performance testing.
func NewPerfServer(t *testing.T) *PerfServer {
	t.Helper()

	wsPort := getFreePort(t)
	adminPort := getFreePort(t)

	cfg := server.Config{
		WebSocketAddr:    fmt.Sprintf("127.0.0.1:%d", wsPort),
		AdminAddr:        fmt.Sprintf("127.0.0.1:%d", adminPort),
		HeartbeatTimeout: 30 * time.Second,
		HeartbeatScan:    5 * time.Second,
		QueueMaxLen:      10000,
		QueueMaxAge:      10 * time.Minute,
		MaxEscalations:   2,
	}

	srv := server.NewServer(cfg, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}

	// Wait for listeners to be ready
	time.Sleep(100 * time.Millisecond)

	return &PerfServer{
		Server:    srv,
		AdminAddr: cfg.AdminAddr,
		WSAddr:    cfg.WebSocketAddr,
	}
}

// Shutdown stops the server.
func (ps *PerfServer) Shutdown() {
	ps.Server.Stop()
}

// SubmitTask submits a task via Admin API and returns the task ID.
func (ps *PerfServer) SubmitTask(instruction, role string) (string, error) {
	body := fmt.Sprintf(`{"instruction":"%s","required_role":"%s"}`, instruction, role)
	resp, err := http.Post(
		"http://"+ps.AdminAddr+"/tasks",
		"application/json",
		bytes.NewReader([]byte(body)),
	)
	if err != nil {
		return "", fmt.Errorf("post task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result["task_id"], nil
}

// GetStatus queries /admin/status.
func (ps *PerfServer) GetStatus() error {
	resp, err := http.Get("http://" + ps.AdminAddr + "/admin/status")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

// GetTasks queries /admin/tasks.
func (ps *PerfServer) GetTasks() error {
	resp, err := http.Get("http://" + ps.AdminAddr + "/admin/tasks")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

// RunConcurrent executes fn concurrently with the given concurrency level,
// running totalOps total invocations. Returns per-invocation latencies in
// microseconds and the number of errors.
func RunConcurrent(concurrency, totalOps int, fn func() error) ([]int64, int) {
	latencies := make([]int64, totalOps)
	var errCount int64
	var wg sync.WaitGroup
	var opIdx int64 = -1

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				idx := int(atomic.AddInt64(&opIdx, 1))
				if idx >= totalOps {
					return
				}
				start := time.Now()
				err := fn()
				elapsed := time.Since(start).Microseconds()
				latencies[idx] = elapsed
				if err != nil {
					atomic.AddInt64(&errCount, 1)
				}
			}
		}()
	}
	wg.Wait()
	return latencies, int(errCount)
}

// LatenciesToMs converts microsecond latencies to milliseconds (integer).
func LatenciesToMs(latencies []int64) []int64 {
	result := make([]int64, len(latencies))
	for i, l := range latencies {
		result[i] = l / 1000 // us -> ms
		if result[i] == 0 && l > 0 {
			result[i] = 1 // at least 1ms if non-zero
		}
	}
	return result
}

// getFreePort returns an available TCP port.
func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("get free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
