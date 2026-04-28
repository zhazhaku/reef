// Package swarm implements a PicoClaw Channel that connects to a Reef Server
// over WebSocket, enabling distributed multi-agent task execution.
package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/reef"
	"github.com/sipeed/picoclaw/pkg/reef/client"
)

const (
	channelName       = "swarm"
	metadataKeyTaskID   = "reef_task_id"
	metadataKeyRole     = "reef_role"
	metadataKeySkills   = "reef_skills"
	metadataKeyModelHint = "reef_model_hint"
)

// SwarmChannel bridges PicoClaw's MessageBus with Reef's WebSocket protocol.
// It implements channels.Channel and agent.EventObserver.
type SwarmChannel struct {
	*channels.BaseChannel
	bc *config.Channel

	connector *client.Connector
	msgBus    *bus.MessageBus
	agentLoop *agent.AgentLoop
	hookReg   string

	mu          sync.RWMutex
	activeTasks map[string]*activeTask // task_id -> task
	turnTasks   map[string]string      // turn_id -> task_id

	logger *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// activeTask tracks a task dispatched from Reef Server.
type activeTask struct {
	taskID       string
	sessionKey   string
	turnID       string
	instruction  string
	finalContent string
	status       string
	startTime    time.Time
}

// NewSwarmChannel creates a new SwarmChannel.
func NewSwarmChannel(
	bc *config.Channel,
	cfg *config.SwarmSettings,
	msgBus *bus.MessageBus,
) (*SwarmChannel, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("swarm server_url is required")
	}
	if cfg.Role == "" {
		return nil, fmt.Errorf("swarm role is required")
	}
	if cfg.ClientID == "" {
		cfg.ClientID = generateClientID()
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 3
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 30
	}

	base := channels.NewBaseChannel(channelName, cfg, msgBus, bc.AllowFrom)

	connectorOpts := client.ConnectorOptions{
		ServerURL:         cfg.ServerURL,
		Token:             cfg.Token,
		ClientID:          cfg.ClientID,
		Role:              cfg.Role,
		Skills:            cfg.Skills,
		Providers:         cfg.Providers,
		Capacity:          cfg.Capacity,
		HeartbeatInterval: time.Duration(cfg.HeartbeatInterval) * time.Second,
		TLSCertFile:       cfg.TLSCertFile,
		TLSKeyFile:        cfg.TLSKeyFile,
		TLSCAFile:         cfg.TLSCAFile,
		TLSSkipVerify:     cfg.TLSSkipVerify,
	}

	ch := &SwarmChannel{
		BaseChannel: base,
		bc:          bc,
		connector:   client.NewConnector(connectorOpts),
		msgBus:      msgBus,
		activeTasks: make(map[string]*activeTask),
		turnTasks:   make(map[string]string),
		logger:      slog.Default(),
	}
	return ch, nil
}

// SetAgentLoop wires the SwarmChannel to the AgentLoop for event observation.
// If the channel is already running, the hook is mounted immediately.
// Also sets the AgentLoop to HermesExecutor mode when running as a client.
func (s *SwarmChannel) SetAgentLoop(al *agent.AgentLoop) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.agentLoop = al

	// When running as a client (SwarmChannel), set HermesExecutor mode.
	// This ensures the AgentLoop doesn't try to delegate tasks externally
	// — it executes tasks received from the server using all available tools.
	if al != nil {
		al.SetHermesMode(agent.HermesExecutor)
		s.logger.Info("Hermes mode set to executor for swarm client",
			slog.String("client_id", s.connector.ClientID()))
	}

	// If already running, mount the hook now (normally Start() does this,
	// but SetAgentLoop may be called after Start due to startup ordering).
	if al != nil && s.IsRunning() && s.hookReg == "" {
		reg := agent.NamedHook("reef-swarm", s)
		if err := al.MountHook(reg); err != nil {
			s.logger.Warn("failed to mount reef hook in SetAgentLoop", slog.String("error", err.Error()))
		} else {
			s.hookReg = reg.Name
			s.logger.Info("reef-swarm hook mounted via SetAgentLoop")
		}
	}
}

// Start implements channels.Channel.
func (s *SwarmChannel) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.SetRunning(true)

	if err := s.connector.Connect(s.ctx); err != nil {
		return fmt.Errorf("swarm connect: %w", err)
	}

	// Register as event observer if agent loop is available
	if s.agentLoop != nil {
		reg := agent.NamedHook("reef-swarm", s)
		if err := s.agentLoop.MountHook(reg); err != nil {
			s.logger.Warn("failed to mount reef hook", slog.String("error", err.Error()))
		} else {
			s.hookReg = reg.Name
		}
	}

	s.wg.Add(1)
	go s.receiveLoop(s.ctx)

	s.logger.Info("swarm channel started",
		slog.String("client_id", s.connector.ClientID()),
		slog.String("role", s.connector.Role()),
		slog.String("server", s.connector.ServerURL()))
	return nil
}

// Stop implements channels.Channel.
func (s *SwarmChannel) Stop(ctx context.Context) error {
	s.SetRunning(false)
	if s.cancel != nil {
		s.cancel()
	}

	if s.hookReg != "" && s.agentLoop != nil {
		s.agentLoop.UnmountHook(s.hookReg)
	}

	_ = s.connector.Close()
	s.wg.Wait()
	return nil
}

// Send implements channels.Channel — receives outbound messages from AgentLoop.
func (s *SwarmChannel) Send(ctx context.Context, msg bus.OutboundMessage) ([]string, error) {
	if !s.IsRunning() {
		return nil, channels.ErrNotRunning
	}

	// Only handle messages for the swarm channel
	if msg.Context.Channel != channelName {
		return nil, nil
	}

	taskID := msg.Context.ChatID
	if taskID == "" {
		return nil, nil
	}

	s.mu.RLock()
	task, ok := s.activeTasks[taskID]
	s.mu.RUnlock()
	if !ok {
		return nil, nil
	}

	kind := strings.TrimSpace(msg.Context.Raw["message_kind"])
	switch kind {
	case "tool_feedback", "thought", "tool_calls":
		// Intermediate progress
		s.reportProgress(taskID, "running", 0, msg.Content)
	default:
		// Final response candidate
		s.mu.Lock()
		task.finalContent = msg.Content
		s.mu.Unlock()

		// Report completion immediately — the OnEvent path may not fire
		// if SetAgentLoop was never called, so Send() is the reliable path.
		s.reportCompleted(taskID, msg.Content, 0)
		s.removeActiveTask(taskID)
	}

	return nil, nil
}

// OnEvent implements agent.EventObserver.
func (s *SwarmChannel) OnEvent(ctx context.Context, evt agent.Event) error {
	switch evt.Kind {
	case agent.EventKindTurnStart:
		payload, ok := evt.Payload.(agent.TurnStartPayload)
		if !ok {
			return nil
		}
		_ = payload
		// Map turn to task via session key
		s.mu.Lock()
		for taskID, task := range s.activeTasks {
			if task.sessionKey == evt.Meta.SessionKey {
				task.turnID = evt.Meta.TurnID
				s.turnTasks[evt.Meta.TurnID] = taskID
				break
			}
		}
		s.mu.Unlock()

	case agent.EventKindToolExecEnd:
		payload, ok := evt.Payload.(agent.ToolExecEndPayload)
		if !ok {
			return nil
		}
		taskID := s.taskIDForTurn(evt.Meta.TurnID)
		if taskID != "" {
			msg := fmt.Sprintf("tool %s finished (%s)", payload.Tool, payload.Duration)
			s.reportProgress(taskID, "running", 0, msg)
		}

	case agent.EventKindTurnEnd:
		payload, ok := evt.Payload.(agent.TurnEndPayload)
		if !ok {
			return nil
		}
		taskID := s.taskIDForTurn(evt.Meta.TurnID)
		if taskID == "" {
			return nil
		}

		s.mu.Lock()
		task := s.activeTasks[taskID]
		if task != nil {
			task.status = string(payload.Status)
		}
		s.mu.Unlock()

		// If the task was already removed (completed via Send path),
		// skip duplicate reporting.
		if task == nil {
			return nil
		}

		switch payload.Status {
		case agent.TurnEndStatusCompleted:
			var result string
			if task != nil {
				result = task.finalContent
			}
			s.reportCompleted(taskID, result, payload.Duration.Milliseconds())
		case agent.TurnEndStatusError:
			s.reportFailed(taskID, "execution_error", "turn ended with error", payload.Iterations)
		case agent.TurnEndStatusAborted:
			s.reportFailed(taskID, "cancelled", "turn was aborted", payload.Iterations)
		}

		s.removeActiveTask(taskID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal loops
// ---------------------------------------------------------------------------

func (s *SwarmChannel) receiveLoop(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-s.connector.Messages():
			if !ok {
				return
			}
			s.handleServerMessage(ctx, msg)
		}
	}
}

func (s *SwarmChannel) handleServerMessage(ctx context.Context, msg reef.Message) {
	switch msg.MsgType {
	case reef.MsgTaskDispatch:
		var payload reef.TaskDispatchPayload
		if err := msg.DecodePayload(&payload); err != nil {
			s.logger.Warn("decode task_dispatch", slog.String("error", err.Error()))
			return
		}
		s.dispatchTask(ctx, payload)

	case reef.MsgCancel:
		var payload reef.ControlPayload
		_ = msg.DecodePayload(&payload)
		s.cancelTask(payload.TaskID)

	case reef.MsgPause:
		var payload reef.ControlPayload
		_ = msg.DecodePayload(&payload)
		s.pauseTask(payload.TaskID)

	case reef.MsgResume:
		var payload reef.ControlPayload
		_ = msg.DecodePayload(&payload)
		s.resumeTask(payload.TaskID)

	default:
		s.logger.Debug("unhandled server message", slog.String("msg_type", string(msg.MsgType)))
	}
}

func (s *SwarmChannel) dispatchTask(ctx context.Context, payload reef.TaskDispatchPayload) {
	taskID := payload.TaskID

	s.mu.Lock()
	if _, exists := s.activeTasks[taskID]; exists {
		s.mu.Unlock()
		s.logger.Warn("task already active", slog.String("task_id", taskID))
		return
	}
	s.activeTasks[taskID] = &activeTask{
		taskID:      taskID,
		instruction: payload.Instruction,
		status:      "running",
		startTime:   time.Now(),
	}
	s.mu.Unlock()

	// Build inbound message for AgentLoop
	raw := map[string]string{
		metadataKeyTaskID: taskID,
	}
	if payload.RequiredRole != "" {
		raw[metadataKeyRole] = payload.RequiredRole
	}
	if len(payload.RequiredSkills) > 0 {
		raw[metadataKeySkills] = strings.Join(payload.RequiredSkills, ",")
	}
	if payload.ModelHint != "" {
		raw[metadataKeyModelHint] = payload.ModelHint
	}

	inboundCtx := bus.InboundContext{
		Channel:  channelName,
		ChatID:   taskID,
		ChatType: "direct",
		SenderID: "reef-server",
		Raw:      raw,
	}

	inboundMsg := bus.InboundMessage{
		Context:    inboundCtx,
		Content:    payload.Instruction,
		SessionKey: fmt.Sprintf("reef:%s", taskID),
	}
	inboundMsg = bus.NormalizeInboundMessage(inboundMsg)

	// Track session key for turn mapping
	s.mu.Lock()
	if task := s.activeTasks[taskID]; task != nil {
		task.sessionKey = inboundMsg.SessionKey
	}
	s.mu.Unlock()

	if err := s.msgBus.PublishInbound(ctx, inboundMsg); err != nil {
		s.logger.Error("failed to publish inbound task", slog.String("error", err.Error()))
		s.reportFailed(taskID, "execution_error", err.Error(), 0)
		s.removeActiveTask(taskID)
	}
}

func (s *SwarmChannel) cancelTask(taskID string) {
	s.mu.Lock()
	task, ok := s.activeTasks[taskID]
	s.mu.Unlock()
	if !ok {
		return
	}
	// There is no direct cancel on AgentLoop per task.
	// The turn will continue; we mark it and send failed on TurnEnd.
	s.mu.Lock()
	task.status = "cancelled"
	s.mu.Unlock()
	s.reportProgress(taskID, "cancelled", 0, "task cancelled by server")
}

func (s *SwarmChannel) pauseTask(taskID string) {
	s.mu.Lock()
	task, ok := s.activeTasks[taskID]
	s.mu.Unlock()
	if !ok {
		return
	}
	s.mu.Lock()
	task.status = "paused"
	s.mu.Unlock()
	s.reportProgress(taskID, "paused", 0, "task paused by server")
}

func (s *SwarmChannel) resumeTask(taskID string) {
	s.mu.Lock()
	task, ok := s.activeTasks[taskID]
	s.mu.Unlock()
	if !ok {
		return
	}
	s.mu.Lock()
	task.status = "running"
	s.mu.Unlock()
	s.reportProgress(taskID, "running", 0, "task resumed by server")
}

func (s *SwarmChannel) removeActiveTask(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.activeTasks[taskID]
	if task != nil && task.turnID != "" {
		delete(s.turnTasks, task.turnID)
	}
	delete(s.activeTasks, taskID)
}

func (s *SwarmChannel) taskIDForTurn(turnID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.turnTasks[turnID]
}

// ---------------------------------------------------------------------------
// Reporting helpers
// ---------------------------------------------------------------------------

func (s *SwarmChannel) reportProgress(taskID, status string, percent int, message string) {
	msg, _ := reef.NewMessage(reef.MsgTaskProgress, taskID, reef.TaskProgressPayload{
		TaskID:          taskID,
		Status:          status,
		ProgressPercent: percent,
		Message:         message,
		Timestamp:       time.Now().UnixMilli(),
	})
	_ = s.connector.Send(msg)
}

func (s *SwarmChannel) reportCompleted(taskID, result string, execTimeMs int64) {
	msg, _ := reef.NewMessage(reef.MsgTaskCompleted, taskID, reef.TaskCompletedPayload{
		TaskID:          taskID,
		Result:          map[string]any{"text": result},
		ExecutionTimeMs: execTimeMs,
		Timestamp:       time.Now().UnixMilli(),
	})
	_ = s.connector.Send(msg)
}

func (s *SwarmChannel) reportFailed(taskID string, errorType, errorMsg string, attempts int) {
	msg, _ := reef.NewMessage(reef.MsgTaskFailed, taskID, reef.TaskFailedPayload{
		TaskID:         taskID,
		ErrorType:      errorType,
		ErrorMessage:   errorMsg,
		AttemptHistory: []reef.AttemptRecord{{AttemptNumber: attempts}},
		Timestamp:      time.Now().UnixMilli(),
	})
	_ = s.connector.Send(msg)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func generateClientID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "picoclaw"
	}
	return fmt.Sprintf("%s-%d", host, time.Now().Unix())
}
