package client

import (
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

func TestConnector_LeaderAddr_NotConnected(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://h1:8080/ws", Role: "coder"})
	// LeaderAddr returns configured URL even when not connected
	addr := c.LeaderAddr()
	_ = addr
}

func TestConnector_CloseBeforeConnect(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://h1:8080/ws", Role: "coder"})
	if err := c.Close(); err != nil {
		t.Logf("Close before connect: %v", err)
	}
}

func TestConnector_Pool_NilForSingleServer(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://h1:8080/ws", Role: "coder"})
	if p := c.Pool(); p != nil {
		t.Error("Pool should be nil when no pool config")
	}
}

func TestConnector_Messages_Channel(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://h1:8080/ws", Role: "coder"})
	ch := c.Messages()
	if ch == nil {
		t.Fatal("messages channel nil")
	}
}

func TestConnector_SetCallbacks(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://h1:8080/ws", Role: "coder"})
	c.SetOnTaskAvailable(func(t reef.TaskAvailablePayload) {})
	c.SetOnTaskClaimed(func(t reef.TaskClaimedPayload) {})
}

func TestConnector_Send_NotConnected(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://h1:8080/ws", Role: "coder"})
	msg := reef.Message{MsgType: "ping"}
	err := c.Send(msg)
	t.Logf("Send not connected: %v", err)
}

func TestConnector_SendToAll_NotConnected(t *testing.T) {
	c := NewConnector(ConnectorOptions{ServerURL: "ws://h1:8080/ws", Role: "coder"})
	msg := reef.Message{MsgType: "ping"}
	err := c.SendToAll(msg)
	t.Logf("SendToAll not connected: %v", err)
}

func TestNewConnector_WithHeartbeat(t *testing.T) {
	c := NewConnector(ConnectorOptions{
		ServerURL:         "ws://h1:8080/ws",
		Role:              "coder",
		HeartbeatInterval: 25 * time.Second,
	})
	_ = c
}
