package client

import (
	"testing"

	"github.com/sipeed/reef/pkg/reef"
)

func TestCNPHandler_New(t *testing.T) {
	var captured reef.CNPMessage
	sendFunc := func(msg reef.CNPMessage) error { captured = msg; return nil }

	h := NewCNPHandler("t-1", sendFunc)
	if h == nil {
		t.Fatal("nil handler")
	}

	h.SendCorruption("tool_loop", "exec", 5, "loop detected")

	if captured.Type != reef.MsgContextCorruption {
		t.Errorf("type = %s", captured.Type)
	}
	if captured.TaskID != "t-1" {
		t.Errorf("taskID = %s", captured.TaskID)
	}
}

func TestCNPHandler_SendMemoryUpdate(t *testing.T) {
	var captured reef.CNPMessage
	sendFunc := func(msg reef.CNPMessage) error { captured = msg; return nil }
	h := NewCNPHandler("t-1", sendFunc)

	h.SendMemoryUpdate("success", "done", []string{"go"})
	if captured.Type != reef.MsgMemoryUpdate {
		t.Errorf("type = %s", captured.Type)
	}
}

func TestCNPHandler_SendCheckpoint(t *testing.T) {
	var captured reef.CNPMessage
	sendFunc := func(msg reef.CNPMessage) error { captured = msg; return nil }
	h := NewCNPHandler("t-1", sendFunc)

	h.SendCheckpoint(5, "midway")
	if captured.Type != reef.MsgCheckpointSave {
		t.Errorf("type = %s", captured.Type)
	}
}

func TestCNPHandler_Heartbeat(t *testing.T) {
	var captured reef.CNPMessage
	sendFunc := func(msg reef.CNPMessage) error { captured = msg; return nil }
	h := NewCNPHandler("t-1", sendFunc)

	h.SendHeartbeat()
	if captured.Type != reef.MsgLongTaskHeartbeat {
		t.Errorf("type = %s", captured.Type)
	}
}

func TestCNPHandler_HandleServerMessage(t *testing.T) {
	h := NewCNPHandler("t-1", nil)

	for _, mt := range []reef.CNPMessageType{
		reef.MsgStrategySuggest, reef.MsgMemoryInject,
		reef.MsgContextInject, reef.MsgCheckpointRestore,
	} {
		if err := h.HandleServerMessage(reef.CNPMessage{Type: mt, TaskID: "t-1"}); err != nil {
			t.Errorf("Handle(%s) = %v", mt, err)
		}
	}

	if err := h.HandleServerMessage(reef.CNPMessage{Type: "unknown"}); err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestCNPHandler_ConsensusRouting(t *testing.T) {
	var lastType reef.CNPMessageType
	sendFunc := func(msg reef.CNPMessage) error { lastType = msg.Type; return nil }
	h := NewCNPHandler("t-1", sendFunc)

	h.SendMemoryUpdate("success", "ok", nil)
	if !reef.IsConsensus(lastType) {
		t.Error("memory_update should be consensus")
	}

	h.SendHeartbeat()
	if reef.IsConsensus(lastType) {
		t.Error("heartbeat should not be consensus")
	}
}
