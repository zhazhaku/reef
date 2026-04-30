// Package server provides the Reef distributed multi-agent swarm orchestration system.
// GatewayBridge connects the Reef Scheduler to the Gateway's channel system,
// enabling task results to be delivered back to chat channels.
//
// Based on PicoClaw (github.com/sipeed/picoclaw) by Sipeed.

package server

import (
	"context"
	"log/slog"
	"sync"
)

// ChannelSender is the minimal interface for delivering outbound messages.
type ChannelSender interface {
	Send(channel, chatID string, content string) error
}

// GatewayBridge connects the Reef Scheduler to the Gateway runtime.
// It wires ResultDelivery to the channel manager for result routing,
// and provides lifecycle management for the bridge.
type GatewayBridge struct {
	mu              sync.Mutex
	scheduler       *Scheduler
	resultDelivery  *ResultDelivery
	channelSender   ChannelSender
	logger          *slog.Logger
	started         bool
}

// NewGatewayBridge creates a new GatewayBridge.
func NewGatewayBridge(scheduler *Scheduler, logger *slog.Logger) *GatewayBridge {
	if logger == nil {
		logger = slog.Default()
	}
	rd := NewResultDelivery(logger)

	// Wire result delivery callback into scheduler
	scheduler.resultCallback = rd.OnTaskResult

	return &GatewayBridge{
		scheduler:      scheduler,
		resultDelivery: rd,
		logger:         logger,
	}
}

// SetChannelSender sets the channel sender for result delivery.
// This should be called before Start.
func (gb *GatewayBridge) SetChannelSender(cs ChannelSender) {
	gb.mu.Lock()
	defer gb.mu.Unlock()
	gb.channelSender = cs
	gb.resultDelivery.SetChannelManager(cs)
}

// Start initializes the bridge. Currently a no-op since the
// scheduler and channel manager are started externally.
func (gb *GatewayBridge) Start(ctx context.Context) error {
	gb.mu.Lock()
	defer gb.mu.Unlock()

	if gb.started {
		return nil
	}

	gb.logger.Info("GatewayBridge started")
	gb.started = true
	return nil
}

// Stop gracefully shuts down the bridge.
func (gb *GatewayBridge) Stop(ctx context.Context) error {
	gb.mu.Lock()
	defer gb.mu.Unlock()

	if !gb.started {
		return nil
	}

	gb.logger.Info("GatewayBridge stopped")
	gb.started = false
	return nil
}

// IsStarted returns whether the bridge is running.
func (gb *GatewayBridge) IsStarted() bool {
	gb.mu.Lock()
	defer gb.mu.Unlock()
	return gb.started
}
