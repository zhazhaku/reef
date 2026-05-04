package client

import (
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/raft"
)

func poolCfg(addrs ...string) raft.PoolConfig {
	return raft.PoolConfig{
		ServerAddrs:      addrs,
		PingInterval:     10 * time.Second,
		ReconnectBackoff: 1 * time.Second,
		MaxReconnect:     30 * time.Second,
	}
}

func TestNewPoolConnector_EmptyPoolConfig(t *testing.T) {
	_, err := NewPoolConnector(ConnectorOptions{ServerURL: "", Role: "coder"}, raft.PoolConfig{})
	if err == nil {
		t.Error("expected error for empty pool config")
	}
}

func TestNewPoolConnector_SingleServer(t *testing.T) {
	c, err := NewPoolConnector(
		ConnectorOptions{ServerURL: "ws://localhost:1", Role: "coder"},
		poolCfg("ws://localhost:1"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil || c.Pool() == nil {
		t.Fatal("nil connector or pool")
	}
}

func TestNewPoolConnector_MultipleAddrs(t *testing.T) {
	c, err := NewPoolConnector(
		ConnectorOptions{ServerURL: "ws://n1:8080", Role: "coder"},
		poolCfg("ws://n1:8080", "ws://n2:8081", "ws://n3:8082"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Pool() == nil {
		t.Fatal("pool is nil")
	}
}

func TestPoolConnector_SendFallbackNonPool(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://localhost:1", Role: "coder"})
	msg, _ := reef.NewMessage(reef.MsgTaskCompleted, "", nil)
	_ = c.Send(msg)
}

func TestPoolConnector_PoolAccessor(t *testing.T) {
	cfg := poolCfg("ws://n1:8080", "ws://n2:8081")
	c, _ := NewPoolConnector(ConnectorOptions{ServerURL: "ws://n1:8080", Role: "coder"}, cfg)
	pool := c.Pool()
	if pool == nil {
		t.Fatal("pool is nil")
	}
	pool.SetOnLeaderChange(func(addr string) {})
	pool.OnLeaderChange(reef.RaftLeaderChangePayload{NewLeaderAddr: "ws://n1:8080"})
}

func TestPoolConnector_LeaderAddr(t *testing.T) {
	cfg := poolCfg("ws://n1:8080")
	c, _ := NewPoolConnector(ConnectorOptions{ServerURL: "ws://n1:8080", Role: "coder"}, cfg)
	if addr := c.LeaderAddr(); addr != "" {
		t.Errorf("expected empty, got %s", addr)
	}
}

func TestPoolConnector_SendToAll(t *testing.T) {
	cfg := poolCfg("ws://localhost:1")
	c, _ := NewPoolConnector(ConnectorOptions{ServerURL: "ws://localhost:1", Role: "coder"}, cfg)
	c.SendToAll(reef.Message{})
}

func TestPoolConnector_MessagesChannel(t *testing.T) {
	cfg := poolCfg("ws://localhost:1")
	c, _ := NewPoolConnector(ConnectorOptions{ServerURL: "ws://localhost:1", Role: "coder"}, cfg)
	if ch := c.Messages(); ch == nil {
		t.Fatal("messages channel nil")
	}
}
