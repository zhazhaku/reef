package server

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// mockCS implements ChannelSender for testing.
type mockCS struct {
	sends []mockCSSend
}

type mockCSSend struct {
	channel string
	chatID  string
	content string
}

func (m *mockCS) Send(channel, chatID, content string) error {
	m.sends = append(m.sends, mockCSSend{channel: channel, chatID: chatID, content: content})
	return nil
}

func TestGatewayBridge_Lifecycle(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		WebSocketAddr: ":0",
		AdminAddr:     ":0",
		QueueMaxLen:   10,
		MaxEscalations: 1,
	}
	srv := NewServer(cfg, slog.Default())
	defer srv.Stop()

	gb := NewGatewayBridge(srv.scheduler, nil)

	if gb.IsStarted() {
		t.Error("expected not started")
	}

	if err := gb.Start(ctx); err != nil {
		t.Errorf("Start() error = %v", err)
	}
	if !gb.IsStarted() {
		t.Error("expected started after Start()")
	}

	if err := gb.Start(ctx); err != nil {
		t.Errorf("Start() error on second call = %v", err)
	}

	if err := gb.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
	if gb.IsStarted() {
		t.Error("expected not started after Stop()")
	}

	if err := gb.Stop(ctx); err != nil {
		t.Errorf("Stop() error on second call = %v", err)
	}
}

func TestGatewayBridge_SetChannelSender(t *testing.T) {
	cfg := Config{
		WebSocketAddr: ":0",
		AdminAddr:     ":0",
		QueueMaxLen:   10,
		MaxEscalations: 1,
	}
	srv := NewServer(cfg, slog.Default())
	defer srv.Stop()

	gb := NewGatewayBridge(srv.scheduler, nil)
	cs := &mockCS{}
	gb.SetChannelSender(cs)

	if gb.resultDelivery.channelManager == nil {
		t.Error("expected channelManager to be set after SetChannelSender")
	}
}

func TestGatewayBridge_ResultDeliveryWired(t *testing.T) {
	cfg := Config{
		WebSocketAddr: ":0",
		AdminAddr:     ":0",
		QueueMaxLen:   10,
		MaxEscalations: 1,
	}
	srv := NewServer(cfg, slog.Default())
	defer srv.Stop()

	gb := NewGatewayBridge(srv.scheduler, nil)

	if srv.scheduler.resultCallback == nil {
		t.Error("expected resultCallback to be wired by NewGatewayBridge")
	}

	cs := &mockCS{}
	gb.SetChannelSender(cs)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := gb.Start(ctx); err != nil {
		t.Errorf("Start() error = %v", err)
	}
	if err := gb.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}
