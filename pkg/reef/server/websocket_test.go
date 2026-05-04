package server

import (
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

func TestWebSocketServer_PendingControls(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	ws := NewWebSocketServer(reg, sched, "", nil)

	// Try to send a cancel to a disconnected client
	msg, _ := reef.NewMessage(reef.MsgCancel, "t1", reef.ControlPayload{
		ControlType: "cancel",
		TaskID:      "t1",
	})
	err := ws.SendMessage("c1", msg)
	if err != nil {
		t.Fatalf("expected buffering without error, got %v", err)
	}

	// Verify message is buffered
	ws.pendingMu.Lock()
	pending := ws.pendingControls["c1"]
	ws.pendingMu.Unlock()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending control, got %d", len(pending))
	}
	if pending[0].MsgType != reef.MsgCancel {
		t.Errorf("msg type = %s, want cancel", pending[0].MsgType)
	}
}

func TestWebSocketServer_NonControlMessage_Dropped(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, queue, SchedulerOptions{})
	ws := NewWebSocketServer(reg, sched, "", nil)

	msg, _ := reef.NewMessage(reef.MsgTaskDispatch, "t1", reef.TaskDispatchPayload{
		TaskID: "t1",
	})
	err := ws.SendMessage("c1", msg)
	if err == nil {
		t.Error("expected error for non-control message to disconnected client")
	}
}

func TestWebSocketServer_isControlMessage(t *testing.T) {
	if !isControlMessage(reef.MsgCancel) {
		t.Error("cancel should be control")
	}
	if !isControlMessage(reef.MsgPause) {
		t.Error("pause should be control")
	}
	if !isControlMessage(reef.MsgResume) {
		t.Error("resume should be control")
	}
	if isControlMessage(reef.MsgTaskDispatch) {
		t.Error("task_dispatch should not be control")
	}
}
