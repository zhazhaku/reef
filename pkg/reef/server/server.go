package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server/notify"
	"github.com/zhazhaku/reef/pkg/reef/server/store"
	"github.com/zhazhaku/reef/pkg/reef/server/ui"
)

// Config holds all server configuration.
type Config struct {
	WebSocketAddr         string
	AdminAddr             string
	Token                 string
	HeartbeatTimeout      time.Duration
	HeartbeatScan         time.Duration
	QueueMaxLen           int
	QueueMaxAge           time.Duration
	MaxEscalations        int
	WebhookURLs           []string
	StoreType             string // "memory" (default) or "sqlite"
	StorePath             string // SQLite database file path
	TLS                   *TLSConfig
	Notifications         []NotificationConfig
	Strategy              string        // "least_load" | "round_robin" | "affinity"
	DefaultTimeoutMs      int64         // default task timeout in ms (0 = 5min)
	TimeoutScanIntervalSec int          // timeout scanner interval in seconds (0 = 10s)
	StarvationThresholdMs  int64        // starvation boost threshold in ms (0 = disabled)
}

// NotificationConfig configures a notification channel.
type NotificationConfig struct {
	Type       string   `json:"type"`                  // "webhook" | "slack" | "smtp" | "feishu" | "wecom"
	URL        string   `json:"url,omitempty"`         // Webhook URL
	WebhookURL string   `json:"webhook_url,omitempty"` // Slack webhook URL
	HookURL    string   `json:"hook_url,omitempty"`    // Feishu/WeCom webhook URL
	SMTPHost   string   `json:"smtp_host,omitempty"`   // SMTP host
	SMTPPort   int      `json:"smtp_port,omitempty"`   // SMTP port
	From       string   `json:"from,omitempty"`        // SMTP from address
	To         []string `json:"to,omitempty"`          // SMTP recipients
	Username   string   `json:"username,omitempty"`     // SMTP username
	Password   string   `json:"password,omitempty"`     // SMTP password
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
	config         Config
	registry       *Registry
	queue          Queue
	scheduler      *Scheduler
	wsServer       *WebSocketServer
	admin          *AdminServer
	ui             *ui.Handler
	logger         *slog.Logger

	httpServer     *http.Server
	wsHTTPServer   *http.Server
	cancelCtx      context.CancelFunc
	bridge         *ServerBridge
	timeoutScanner *TimeoutScanner
}

// Bridge returns the ReefBridge for in-process access to the scheduler.
// Used by the AgentLoop's coordination tools.
func (s *Server) Bridge() *ServerBridge {
	return s.bridge
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

	// Task queue — use priority queue with optional persistence
	var taskQueue Queue
	if cfg.StoreType == "sqlite" && cfg.StorePath != "" {
		sqliteStore, err := store.NewSQLiteStore(cfg.StorePath)
		if err != nil {
			logger.Error("failed to open SQLite store, falling back to memory",
				slog.String("error", err.Error()))
			taskQueue = NewPriorityQueue(cfg.QueueMaxLen, cfg.QueueMaxAge)
		} else {
			logger.Info("using SQLite persistent store", slog.String("path", cfg.StorePath))
			// Use persistent wrapper around priority queue
			taskQueue = NewPersistentPriorityQueue(sqliteStore, cfg.QueueMaxLen, cfg.QueueMaxAge, logger)
		}
	} else {
		taskQueue = NewPriorityQueue(cfg.QueueMaxLen, cfg.QueueMaxAge)
	}
	s.queue = taskQueue

	// Notification manager
	notifyMgr := notify.NewManager(logger)
	for _, nc := range cfg.Notifications {
		switch nc.Type {
		case "webhook":
			urls := []string{}
			if nc.URL != "" {
				urls = append(urls, nc.URL)
			}
			notifyMgr.Add(notify.NewWebhookNotifier(urls))
		case "slack":
			notifyMgr.Add(notify.NewSlackNotifier(nc.WebhookURL))
		case "smtp":
			notifyMgr.Add(notify.NewSMTPNotifier(nc.SMTPHost, nc.SMTPPort, nc.Username, nc.Password, nc.From, nc.To))
		case "feishu":
			notifyMgr.Add(notify.NewFeishuNotifier(nc.HookURL))
		case "wecom":
			notifyMgr.Add(notify.NewWeComNotifier(nc.HookURL))
		}
	}
	// Fallback: if no notifications configured but legacy webhook_urls is set,
	// auto-create a WebhookNotifier for backward compatibility.
	if notifyMgr.Count() == 0 && len(cfg.WebhookURLs) > 0 {
		notifyMgr.Add(notify.NewWebhookNotifier(cfg.WebhookURLs))
	}

	// Match strategy
	var strategy MatchStrategy
	switch cfg.Strategy {
	case "round_robin":
		strategy = &RoundRobinStrategy{}
	case "affinity":
		// Created below after scheduler is available
	default:
		// "least_load" or empty → nil (default = LeastLoad)
	}

	// Scheduler
	s.scheduler = NewScheduler(s.registry, s.queue, SchedulerOptions{
		MaxEscalations: cfg.MaxEscalations,
		WebhookURLs:    cfg.WebhookURLs,
		Logger:         logger,
		NotifyManager:  notifyMgr,
		MatchStrategy:  strategy,
		OnDispatch: func(task *reef.Task, clientID string) error {
			if s.wsServer == nil {
				return fmt.Errorf("websocket server not ready")
			}
			return s.wsServer.SendMessage(clientID, msgTaskDispatch(task))
		},
		OnRequeue: func(task *reef.Task) {
			logger.Info("task requeued", slog.String("task_id", task.ID))
		},
		OnTaskStateChanged: func(task *reef.Task) {
			// Persist state changes to the SQLite store.
			if sa, ok := s.queue.(storeAccess); ok {
				if err := sa.Store().UpdateTask(task); err != nil {
					logger.Warn("failed to persist task state change",
						slog.String("task_id", task.ID),
						slog.String("error", err.Error()))
				}
			}
		},
	})

	// Register restored tasks from persistent queue into the scheduler's index.
	// PersistentQueue.restore() loads non-terminal tasks into the queue cache,
	// but the scheduler's internal task map needs to be synced for GetTask() etc.
	//
	// IMPORTANT: Register the SAME pointers from the queue's snapshot first.
	// The queue cache and the store may return different task objects; only the
	// queue's copies are modified during dispatch/completion.
	for _, task := range s.queue.Snapshot() {
		s.scheduler.RegisterTask(task)
	}

	// Also register terminal tasks (completed/failed/etc.) from the store
	// so they're queryable via GetTask() and admin endpoints.
	if sa, ok := s.queue.(storeAccess); ok {
		terminalTasks, err := sa.Store().ListTasks(store.TaskFilter{
			Statuses: []reef.TaskStatus{
				reef.TaskCompleted, reef.TaskFailed, reef.TaskCancelled, reef.TaskEscalated,
			},
		})
		if err == nil {
			for _, task := range terminalTasks {
				s.scheduler.RegisterTask(task)
			}
		}
	}

	// WebSocket server
	s.wsServer = NewWebSocketServer(s.registry, s.scheduler, cfg.Token, logger)

	// Admin server
	s.admin = NewAdminServer(s.registry, s.scheduler, cfg.Token, logger)

	// Web UI dashboard
	s.ui = ui.NewHandler(s.registry, s.scheduler, time.Now(), logger)

	// Bridge for in-process coordination tools
	s.bridge = NewServerBridge(s.scheduler, s.registry)

	// Post-init: affinity strategy needs the scheduler for task history
	if cfg.Strategy == "affinity" {
		s.scheduler.matchStrategy = NewAffinityStrategy(s.scheduler.TasksSnapshot)
	}

	// Timeout scanner
	timeoutInterval := time.Duration(cfg.TimeoutScanIntervalSec) * time.Second
	if timeoutInterval <= 0 {
		timeoutInterval = 10 * time.Second
	}
	s.timeoutScanner = NewTimeoutScanner(
		timeoutInterval,
		logger,
		s.scheduler.TasksSnapshot,
		func(task *reef.Task) {
			s.scheduler.HandleClientAvailable("") // trigger re-dispatch
		},
		nil, // store is handled separately via OnTaskStateChanged
	)
	// Wire up store if available
	if sa, ok := s.queue.(storeAccess); ok {
		s.timeoutScanner.store = sa.Store()
	}

	return s
}

// Start begins listening on WebSocket and Admin ports.
func (s *Server) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelCtx = cancel

	// Load TLS config if enabled
	var tlsCfg *tls.Config
	if s.config.TLS != nil && s.config.TLS.Enabled {
		var err error
		tlsCfg, err = s.config.TLS.LoadTLSConfig()
		if err != nil {
			return fmt.Errorf("load TLS: %w", err)
		}
		s.logger.Info("TLS enabled")
	}

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
	s.ui.RegisterRoutes(adminMux)
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

	// Wrap with TLS if configured
	if tlsCfg != nil {
		wsListener = tls.NewListener(wsListener, tlsCfg)
		adminListener = tls.NewListener(adminListener, tlsCfg)
	}

	wsScheme := "ws"
	adminScheme := "http"
	if tlsCfg != nil {
		wsScheme = "wss"
		adminScheme = "https"
	}

	go func() {
		s.logger.Info("websocket server listening",
			slog.String("addr", s.config.WebSocketAddr),
			slog.String("scheme", wsScheme))
		if err := s.wsHTTPServer.Serve(wsListener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("websocket server error", slog.String("error", err.Error()))
		}
	}()

	go func() {
		s.logger.Info("admin server listening",
			slog.String("addr", s.config.AdminAddr),
			slog.String("scheme", adminScheme))
		if err := s.httpServer.Serve(adminListener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("admin server error", slog.String("error", err.Error()))
		}
	}()

	// Heartbeat scanner
	go s.heartbeatScanner(ctx)

	// Timeout scanner
	if s.timeoutScanner != nil {
		go s.timeoutScanner.Run(ctx)
	}

	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	if s.cancelCtx != nil {
		s.cancelCtx()
	}

	// Stop timeout scanner
	if s.timeoutScanner != nil {
		s.timeoutScanner.Stop()
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
func (s *Server) Queue() Queue { return s.queue }

// WSServer exposes the WebSocket server for testing.
func (s *Server) WSServer() *WebSocketServer { return s.wsServer }

// AdminHandler exposes the admin server for external route registration.
func (s *Server) AdminHandler() *AdminServer { return s.admin }

// UIHandler exposes the UI handler for external access (e.g., event publishing).
func (s *Server) UIHandler() *ui.Handler { return s.ui }

// msgTaskDispatch creates a task_dispatch message populated from the full task.
func msgTaskDispatch(task *reef.Task) reef.Message {
	msg, _ := reef.NewMessage(reef.MsgTaskDispatch, task.ID, reef.TaskDispatchPayload{
		TaskID:         task.ID,
		Instruction:    task.Instruction,
		RequiredRole:   task.RequiredRole,
		RequiredSkills: task.RequiredSkills,
		MaxRetries:     task.MaxRetries,
		TimeoutMs:      task.TimeoutMs,
		ModelHint:      task.ModelHint,
		CreatedAt:      task.CreatedAt.UnixMilli(),
		ReplyTo:        task.ReplyTo,
	})
	return msg
}
