// Package swarm implements a PicoClaw Channel that communicates over
// WebSocket with a Reef Server, enabling distributed task execution.
package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sipeed/reef/pkg/bus"
	"github.com/sipeed/reef/pkg/reef"
	"github.com/sipeed/reef/pkg/reef/client"
)

// SwarmChannel bridges PicoClaw's MessageBus with Reef's WebSocket protocol.
type SwarmChannel struct {
	connector *client.Connector
	inCh      chan bus.Message   // to AgentLoop
	outCh     chan bus.Message   // from AgentLoop
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	logger    *slog.Logger
}

// Options configures the SwarmChannel.
type Options struct {
	Connector *client.Connector
	Logger    *slog.Logger
}

// New creates a SwarmChannel.
func New(opts Options) *SwarmChannel {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &SwarmChannel{
		connector: opts.Connector,
		inCh:      make(chan bus.Message, 16),
		outCh:     make(chan bus.Message, 16),
		logger:    opts.Logger,
	}
}

// Start connects to the Reef Server and begins relaying messages.
func (s *SwarmChannel) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	if err := s.connector.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	s.wg.Add(1)
	go s.receiveLoop(ctx)
	s.wg.Add(1)
	go s.sendLoop(ctx)

	return nil
}

// Stop shuts down the channel gracefully.
func (s *SwarmChannel) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	_ = s.connector.Close()
	s.wg.Wait()
	close(s.inCh)
	close(s.outCh)
	return nil
}

// Send delivers an outbound message (from AgentLoop) toward the Server.
func (s *SwarmChannel) Send(msg bus.Message) error {
	select {
	case s.outCh <- msg:
		return nil
	default:
		return fmt.Errorf("outbound buffer full")
	}
}

// Receive returns the channel of inbound messages (to AgentLoop).
func (s *SwarmChannel) Receive() <-chan bus.Message {
	return s.inCh
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
			s.handleServerMessage(msg)
		}
	}
}

func (s *SwarmChannel) sendLoop(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-s.outCh:
			if !ok {
				return
			}
			s.handleAgentMessage(msg)
		}
	}
}

// handleServerMessage converts a Reef protocol message to a bus.Message.
func (s *SwarmChannel) handleServerMessage(msg reef.Message) {
	switch msg.MsgType {
	case reef.MsgTaskDispatch:
		var payload reef.TaskDispatchPayload
		if err := msg.DecodePayload(&payload); err != nil {
			s.logger.Warn("decode task_dispatch", slog.String("error", err.Error()))
			return
		}
		busMsg := bus.Message{
			Type: bus.TypeInbound,
			Text: payload.Instruction,
			Payload: map[string]any{
				"task_id":         payload.TaskID,
				"required_role":   payload.RequiredRole,
				"required_skills": payload.RequiredSkills,
				"max_retries":     payload.MaxRetries,
				"timeout_ms":      payload.TimeoutMs,
			},
		}
		select {
		case s.inCh <- busMsg:
		default:
			s.logger.Warn("inbound buffer full, dropped task_dispatch")
		}

	case reef.MsgCancel:
		var payload reef.ControlPayload
		_ = msg.DecodePayload(&payload)
		s.inCh <- bus.Message{
			Type: bus.TypeSystem,
			Text: "cancel",
			Payload: map[string]any{
				"task_id": payload.TaskID,
			},
		}

	case reef.MsgPause:
		var payload reef.ControlPayload
		_ = msg.DecodePayload(&payload)
		s.inCh <- bus.Message{
			Type: bus.TypeSystem,
			Text: "pause",
			Payload: map[string]any{
				"task_id": payload.TaskID,
			},
		}

	case reef.MsgResume:
		var payload reef.ControlPayload
		_ = msg.DecodePayload(&payload)
		s.inCh <- bus.Message{
			Type: bus.TypeSystem,
			Text: "resume",
			Payload: map[string]any{
				"task_id": payload.TaskID,
			},
		}

	default:
		s.logger.Debug("unhandled server message", slog.String("msg_type", string(msg.MsgType)))
	}
}

// handleAgentMessage converts a bus.Message to a Reef protocol message.
func (s *SwarmChannel) handleAgentMessage(msg bus.Message) {
	switch msg.Type {
	case bus.TypeOutbound:
		// Map outbound messages to task_progress or task_completed
		if taskID, ok := msg.Payload["task_id"].(string); ok {
			status, _ := msg.Payload["status"].(string)
			switch status {
			case "completed":
				out, _ := reef.NewMessage(reef.MsgTaskCompleted, taskID, reef.TaskCompletedPayload{
					TaskID:          taskID,
					Result:          map[string]any{"text": msg.Text},
					ExecutionTimeMs: getInt64(msg.Payload, "execution_time_ms"),
				})
				_ = s.connector.Send(out)
			case "failed":
				out, _ := reef.NewMessage(reef.MsgTaskFailed, taskID, reef.TaskFailedPayload{
					TaskID:         taskID,
					ErrorType:      getString(msg.Payload, "error_type"),
					ErrorMessage:   msg.Text,
					AttemptHistory: []reef.AttemptRecord{},
				})
				_ = s.connector.Send(out)
			default:
				percent := getInt(msg.Payload, "progress_percent")
				out, _ := reef.NewMessage(reef.MsgTaskProgress, taskID, reef.TaskProgressPayload{
					TaskID:          taskID,
					Status:          status,
					ProgressPercent: percent,
					Message:         msg.Text,
				})
				_ = s.connector.Send(out)
			}
		}
	}
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	if v, ok := m[key].(int); ok {
		return v
	}
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func getInt64(m map[string]any, key string) int64 {
	if v, ok := m[key].(int64); ok {
		return v
	}
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	return 0
}
