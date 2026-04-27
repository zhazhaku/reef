package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

// Config holds all server configuration.
type Config struct {
	WebSocketAddr    string
	AdminAddr        string
	Token            string
	HeartbeatTimeout time.Duration
	HeartbeatScan    time.Duration
	QueueMaxLen      int
	QueueMaxAge      time.Duration
	MaxEscalations   int
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		WebSocketAddr:    ":8080",
		AdminAddr:        ":8081",
		Token:            "",
		HeartbeatTimeout: 30 * time.Second,
		HeartbeatScan:    5 * time.Second,
		QueueMaxLen:      1000,
		QueueMaxAge:      10 * time.Minute,
		MaxEscalations:   2,
	}
}

// Server is the top-level Reef Server orchestrator.
type Server struct {
	config    Config
	registry  *Registry
	queue     *TaskQueue
	scheduler *Scheduler
	wsServer  *WebSocketServer
	admin     *AdminServer
	logger    *slog.Logger

	httpServer   *http.Server
	wsHTTPServer *http.Server
	cancelCtx    context.CancelFunc
}

// NewServer creates and wires all server components.
func NewServer(cfg Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	s := &Server{config: cfg, logger: logger}

	// Registry with stale callback
	s.registry = NewRegistry(func(clientID string) {
		logger.Warn("client marked stale", slog.String("client_id", clientID))
	})

	// Task queue
	s.queue = NewTaskQueue(cfg.QueueMaxLen, cfg.QueueMaxAge)

	// Scheduler
	s.scheduler = NewScheduler(s.registry, s.queue, SchedulerOptions{
		MaxEscalations: cfg.MaxEscalations,
		OnDispatch: func(taskID, clientID string) error {
			// Actually send the task_dispatch message via WebSocket
			if s.wsServer == nil {
				return fmt.Errorf("websocket server not ready")
			}
			return s.wsServer.SendMessage(clientID, msgTaskDispatch(taskID))
		},
		OnRequeue: func(task *reef.Task) {
			logger.Info("task requeued", slog.String("task_id", task.ID))
		},
	})

	// WebSocket server
	s.wsServer = NewWebSocketServer(s.registry, s.scheduler, cfg.Token, logger)

	// Admin server
	s.admin = NewAdminServer(s.registry, s.scheduler, logger)

	return s
}

// Start begins listening on WebSocket and Admin ports.
func (s *Server) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelCtx = cancel

	// WebSocket HTTP server
	wsMux := http.NewServeMux()
	wsMux.Handle("/ws", s.wsServer)
	s.wsHTTPServer = &http.Server{
		Addr:    s.config.WebSocketAddr,
		Handler: wsMux,
	}

	// Admin HTTP server
	adminMux := http.NewServeMux()
	s.admin.RegisterRoutes(adminMux)
	s.httpServer = &http.Server{
		Addr:    s.config.AdminAddr,
		Handler: adminMux,
	}

	// Start listeners
	wsListener, err := net.Listen("tcp", s.config.WebSocketAddr)
	if err != nil {
		return fmt.Errorf("websocket listen: %w", err)
	}
	adminListener, err := net.Listen("tcp", s.config.AdminAddr)
	if err != nil {
		return fmt.Errorf("admin listen: %w", err)
	}

	go func() {
		s.logger.Info("websocket server listening", slog.String("addr", s.config.WebSocketAddr))
		if err := s.wsHTTPServer.Serve(wsListener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("websocket server error", slog.String("error", err.Error()))
		}
	}()

	go func() {
		s.logger.Info("admin server listening", slog.String("addr", s.config.AdminAddr))
		if err := s.httpServer.Serve(adminListener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("admin server error", slog.String("error", err.Error()))
		}
	}()

	// Heartbeat scanner
	go s.heartbeatScanner(ctx)

	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	if s.cancelCtx != nil {
		s.cancelCtx()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if s.wsHTTPServer != nil {
		_ = s.wsHTTPServer.Shutdown(shutdownCtx)
	}
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(shutdownCtx)
	}

	s.logger.Info("server stopped")
	return nil
}

// heartbeatScanner periodically checks for stale clients.
func (s *Server) heartbeatScanner(ctx context.Context) {
	ticker := time.NewTicker(s.config.HeartbeatScan)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			staleIDs := s.registry.ScanStale(s.config.HeartbeatTimeout)
			for _, id := range staleIDs {
				// Pause any in-flight tasks for this client
				for _, task := range s.scheduler.TasksSnapshot() {
					if task.AssignedClient == id && task.Status == reef.TaskRunning {
						_ = task.Transition(reef.TaskPaused)
						task.PauseReason = "disconnect"
					}
				}
			}
			// Also expire old queued tasks
			expired := s.queue.Expire(time.Now())
			for _, task := range expired {
				s.logger.Warn("task expired from queue", slog.String("task_id", task.ID))
				_ = task.Transition(reef.TaskFailed)
			}
		}
	}
}

// Registry exposes the client registry for testing.
func (s *Server) Registry() *Registry { return s.registry }

// Scheduler exposes the task scheduler for testing.
func (s *Server) Scheduler() *Scheduler { return s.scheduler }

// Queue exposes the task queue for testing.
func (s *Server) Queue() *TaskQueue { return s.queue }

// WSServer exposes the WebSocket server for testing.
func (s *Server) WSServer() *WebSocketServer { return s.wsServer }

// msgTaskDispatch creates a task_dispatch message for the given task ID.
// The actual payload is populated by the caller (scheduler) with full task details.
func msgTaskDispatch(taskID string) reef.Message {
	msg, _ := reef.NewMessage(reef.MsgTaskDispatch, taskID, reef.TaskDispatchPayload{
		TaskID: taskID,
	})
	return msg
}
