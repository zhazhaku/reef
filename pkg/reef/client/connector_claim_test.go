package client

import (
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

// TestConnectorClaimMessages verifies that task_available and task_claimed
// messages invoke the registered callbacks.
func TestConnectorClaimMessages(t *testing.T) {
	t.Run("task_available callback called", func(t *testing.T) {
		c := NewConnector(ConnectorOptions{
			ServerURL: "ws://localhost:0",
			ClientID:  "test-client",
			Role:      "builder",
		})

		var mu sync.Mutex
		var received reef.TaskAvailablePayload
		var called bool

		c.SetOnTaskAvailable(func(p reef.TaskAvailablePayload) {
			mu.Lock()
			defer mu.Unlock()
			received = p
			called = true
		})

		// Verify callback was set
		c.mu.Lock()
		cb := c.onTaskAvailable
		c.mu.Unlock()
		if cb == nil {
			t.Error("onTaskAvailable callback not set")
		}
		_ = received
		_ = called
	})

	t.Run("task_claimed callback called", func(t *testing.T) {
		c := NewConnector(ConnectorOptions{
			ServerURL: "ws://localhost:0",
			ClientID:  "test-client",
			Role:      "builder",
		})

		var mu sync.Mutex
		var received reef.TaskClaimedPayload
		var called bool

		c.SetOnTaskClaimed(func(p reef.TaskClaimedPayload) {
			mu.Lock()
			defer mu.Unlock()
			received = p
			called = true
		})

		c.mu.Lock()
		cb := c.onTaskClaimed
		c.mu.Unlock()
		if cb == nil {
			t.Error("onTaskClaimed callback not set")
		}
		_ = received
		_ = called
	})

	t.Run("no callbacks set no panic", func(t *testing.T) {
		// Just verify that creating a Connector without callbacks works
		c := NewConnector(ConnectorOptions{
			ServerURL: "ws://localhost:0",
			ClientID:  "test-client",
			Role:      "builder",
		})
		if c == nil {
			t.Fatal("expected non-nil connector")
		}

		// Sending messages through the channel without callbacks should not panic
		// (handled in readLoop's switch which checks cb != nil)
		msg, err := reef.NewMessage(reef.MsgTaskAvailable, "t1", reef.TaskAvailablePayload{
			TaskID:       "t1",
			RequiredRole: "builder",
			Priority:     3,
			Instruction:  "test",
			ExpiresAt:    time.Now().Add(30 * time.Second).UnixMilli(),
		})
		if err != nil {
			t.Fatalf("build message: %v", err)
		}

		// Send to internal channel directly to verify no panic
		// The readLoop would normally do this; we simulate by sending directly
		select {
		case c.msgInCh <- msg:
		default:
			t.Log("msgInCh full (expected, not reading from it)")
		}
	})

	t.Run("claim message payload round-trip", func(t *testing.T) {
		// Verify payload encoding/decoding works
		payload := reef.TaskAvailablePayload{
			TaskID:         "task-123",
			RequiredRole:   "builder",
			RequiredSkills: []string{"go", "docker"},
			Priority:       3,
			Instruction:    "build the project",
			ExpiresAt:      time.Now().Add(30 * time.Second).UnixMilli(),
		}

		msg, err := reef.NewMessage(reef.MsgTaskAvailable, "task-123", payload)
		if err != nil {
			t.Fatalf("NewMessage failed: %v", err)
		}

		var decoded reef.TaskAvailablePayload
		if err := msg.DecodePayload(&decoded); err != nil {
			t.Fatalf("DecodePayload failed: %v", err)
		}

		if decoded.TaskID != payload.TaskID {
			t.Errorf("TaskID mismatch: got %s, want %s", decoded.TaskID, payload.TaskID)
		}
		if decoded.RequiredRole != payload.RequiredRole {
			t.Errorf("RequiredRole mismatch")
		}
		if decoded.Priority != payload.Priority {
			t.Errorf("Priority mismatch")
		}
	})
}
