package swarm

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/reef"
)

func TestHandleServerMessage_TaskDispatch(t *testing.T) {
	inCh := make(chan bus.Message, 1)
	ch := &SwarmChannel{inCh: inCh, logger: testLogger(t)}

	msg, _ := reef.NewMessage(reef.MsgTaskDispatch, "t1", reef.TaskDispatchPayload{
		TaskID:      "t1",
		Instruction: "write a test",
		RequiredRole: "coder",
		MaxRetries:  3,
	})
	ch.handleServerMessage(msg)

	select {
	case bmsg := <-inCh:
		if bmsg.Type != bus.TypeInbound {
			t.Errorf("type = %s, want inbound", bmsg.Type)
		}
		if bmsg.Text != "write a test" {
			t.Errorf("text = %s, want 'write a test'", bmsg.Text)
		}
		if bmsg.Payload["task_id"] != "t1" {
			t.Errorf("task_id = %v, want t1", bmsg.Payload["task_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for inbound message")
	}
}

func TestHandleServerMessage_Control(t *testing.T) {
	inCh := make(chan bus.Message, 3)
	ch := &SwarmChannel{inCh: inCh, logger: testLogger(t)}

	for _, typ := range []reef.MessageType{reef.MsgCancel, reef.MsgPause, reef.MsgResume} {
		msg, _ := reef.NewMessage(typ, "t1", reef.ControlPayload{
			ControlType: string(typ),
			TaskID:      "t1",
		})
		ch.handleServerMessage(msg)
	}

	for _, expected := range []string{"cancel", "pause", "resume"} {
		select {
		case bmsg := <-inCh:
			if bmsg.Type != bus.TypeSystem {
				t.Errorf("type = %s, want system", bmsg.Type)
			}
			if bmsg.Text != expected {
				t.Errorf("text = %s, want %s", bmsg.Text, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for %s message", expected)
		}
	}
}

func TestHandleAgentMessage_Progress(t *testing.T) {
	// We can't easily test handleAgentMessage without a real connector,
	// so verify the helper functions instead.
	m := map[string]any{
		"task_id":          "t1",
		"status":           "running",
		"progress_percent": 42,
		"execution_time_ms": float64(1500),
	}

	if got := getString(m, "status"); got != "running" {
		t.Errorf("getString = %s, want running", got)
	}
	if got := getInt(m, "progress_percent"); got != 42 {
		t.Errorf("getInt = %d, want 42", got)
	}
	if got := getInt64(m, "execution_time_ms"); got != 1500 {
		t.Errorf("getInt64 = %d, want 1500", got)
	}
}

func testLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
