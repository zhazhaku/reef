package client

import (
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

func TestBackoff_NextReset(t *testing.T) {
	b := NewBackoff(1*time.Second, 60*time.Second)

	if b.current != 1*time.Second {
		t.Errorf("initial = %v, want 1s", b.current)
	}

	b.Next()
	if b.current != 2*time.Second {
		t.Errorf("after 1st next = %v, want 2s", b.current)
	}
	b.Next()
	if b.current != 4*time.Second {
		t.Errorf("after 2nd next = %v, want 4s", b.current)
	}
	b.Next()
	b.Next()
	b.Next() // 32s
	b.Next() // should cap at 60s
	if b.current != 60*time.Second {
		t.Errorf("after cap = %v, want 60s", b.current)
	}

	b.Reset()
	if b.current != 1*time.Second {
		t.Errorf("after reset = %v, want 1s", b.current)
	}
	if b.Attempt() != 0 {
		t.Errorf("attempt = %d, want 0", b.Attempt())
	}
}

func TestBackoff_Wait(t *testing.T) {
	b := NewBackoff(100*time.Millisecond, 60*time.Second)
	start := time.Now()
	select {
	case <-b.Wait():
		elapsed := time.Since(start)
		if elapsed < 50*time.Millisecond || elapsed > 200*time.Millisecond {
			t.Errorf("waited %v, expected ~100ms", elapsed)
		}
	}
}

func TestConnectorOptions_Defaults(t *testing.T) {
	c := NewConnector(ConnectorOptions{})
	if c.capacity != 1 {
		t.Errorf("capacity = %d, want 1", c.capacity)
	}
	if c.heartbeatInterval != 10*time.Second {
		t.Errorf("heartbeat = %v, want 10s", c.heartbeatInterval)
	}
}

func TestConnector_SendClosed(t *testing.T) {
	c := NewConnector(ConnectorOptions{})
	_ = c.Close()

	msg, _ := reef.NewMessage(reef.MsgHeartbeat, "", reef.HeartbeatPayload{})
	if err := c.Send(msg); err == nil {
		t.Error("expected error when sending to closed connector")
	}
}
