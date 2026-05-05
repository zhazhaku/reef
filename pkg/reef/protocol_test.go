package reef

import (
	"encoding/json"
	"testing"
)

func TestMessageType_IsValid(t *testing.T) {
	tests := []struct {
		mt      MessageType
		want    bool
	}{
		{MsgRegister, true},
		{MsgHeartbeat, true},
		{MsgTaskDispatch, true},
		{MessageType("unknown"), false},
		{MessageType(""), false},
	}
	for _, tt := range tests {
		if got := tt.mt.IsValid(); got != tt.want {
			t.Errorf("%q.IsValid() = %v, want %v", tt.mt, got, tt.want)
		}
	}
}

func TestNewMessage_RoundTrip(t *testing.T) {
	payload := RegisterPayload{
		ProtocolVersion: ProtocolVersion,
		ClientID:        "client-42",
		Role:            "coder",
		Skills:          []string{"github", "docker"},
		Capacity:        2,
	}

	msg, err := NewMessage(MsgRegister, "", payload)
	if err != nil {
		t.Fatalf("NewMessage failed: %v", err)
	}

	if msg.MsgType != MsgRegister {
		t.Errorf("MsgType = %s, want %s", msg.MsgType, MsgRegister)
	}
	if msg.Timestamp == 0 {
		t.Error("Timestamp should be set")
	}

	var decoded RegisterPayload
	if err := msg.DecodePayload(&decoded); err != nil {
		t.Fatalf("DecodePayload failed: %v", err)
	}
	if decoded.ClientID != payload.ClientID {
		t.Errorf("ClientID = %s, want %s", decoded.ClientID, payload.ClientID)
	}
	if decoded.Role != payload.Role {
		t.Errorf("Role = %s, want %s", decoded.Role, payload.Role)
	}
}

func TestNewMessage_UnknownType(t *testing.T) {
	_, err := NewMessage(MessageType("bogus"), "", struct{}{})
	if err == nil {
		t.Error("expected error for unknown message type")
	}
}

func TestAllPayloadTypes_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		msgType MessageType
		payload any
	}{
		{
			name:    "register",
			msgType: MsgRegister,
			payload: RegisterPayload{ProtocolVersion: ProtocolVersion, ClientID: "c1", Role: "coder", Capacity: 1},
		},
		{
			name:    "register_ack",
			msgType: MsgRegisterAck,
			payload: RegisterAckPayload{ClientID: "c1", ServerTime: 1234567890000},
		},
		{
			name:    "register_nack",
			msgType: MsgRegisterNack,
			payload: RegisterNackPayload{Reason: "bad token"},
		},
		{
			name:    "heartbeat",
			msgType: MsgHeartbeat,
			payload: HeartbeatPayload{Timestamp: 1234567890000},
		},
		{
			name:    "task_dispatch",
			msgType: MsgTaskDispatch,
			payload: TaskDispatchPayload{TaskID: "t1", Instruction: "hello", RequiredRole: "coder", MaxRetries: 3, TimeoutMs: 300000},
		},
		{
			name:    "task_progress",
			msgType: MsgTaskProgress,
			payload: TaskProgressPayload{TaskID: "t1", Status: "running", ProgressPercent: 50},
		},
		{
			name:    "task_completed",
			msgType: MsgTaskCompleted,
			payload: TaskCompletedPayload{TaskID: "t1", Result: map[string]any{"text": "done"}, ExecutionTimeMs: 1200},
		},
		{
			name:    "task_failed",
			msgType: MsgTaskFailed,
			payload: TaskFailedPayload{TaskID: "t1", ErrorType: "escalated", ErrorMessage: "max retries exceeded"},
		},
		{
			name:    "cancel",
			msgType: MsgCancel,
			payload: ControlPayload{ControlType: "cancel", TaskID: "t1"},
		},
		{
			name:    "control_ack",
			msgType: MsgControlAck,
			payload: ControlAckPayload{ControlType: "cancel", TaskID: "t1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := NewMessage(tt.msgType, "task-123", tt.payload)
			if err != nil {
				t.Fatalf("NewMessage: %v", err)
			}

			// Full JSON round-trip
			bytes, err := json.Marshal(msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var back Message
			if err := json.Unmarshal(bytes, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if back.MsgType != tt.msgType {
				t.Errorf("MsgType = %s, want %s", back.MsgType, tt.msgType)
			}
			if back.TaskID != "task-123" {
				t.Errorf("TaskID = %s, want task-123", back.TaskID)
			}
		})
	}
}

func TestValidateProtocolVersion(t *testing.T) {
	if err := ValidateProtocolVersion(ProtocolVersion); err != nil {
		t.Errorf("expected nil for valid version, got %v", err)
	}
	if err := ValidateProtocolVersion("reef-v0"); err == nil {
		t.Error("expected error for invalid version")
	}
}

func TestMsgErrorRoundTrip(t *testing.T) {
	msg, err := NewMessage(MsgError, "t-1", ErrorPayload{
		Code:         "ERR_UNKNOWN_TYPE",
		Message:      "unknown message type: bogus",
		OriginalType: "bogus",
	})
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	if msg.MsgType != MsgError {
		t.Errorf("MsgType = %s, want %s", msg.MsgType, MsgError)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.MsgType != MsgError {
		t.Errorf("decoded MsgType = %s", decoded.MsgType)
	}

	var payload ErrorPayload
	if err := decoded.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if payload.Code != "ERR_UNKNOWN_TYPE" {
		t.Errorf("Code = %s", payload.Code)
	}
	if payload.Message == "" {
		t.Error("Message should not be empty")
	}
	if payload.OriginalType != "bogus" {
		t.Errorf("OriginalType = %s", payload.OriginalType)
	}
}

func TestRaftLeaderChangeMessage(t *testing.T) {
	// First election: old addresses are empty
	msg := NewRaftLeaderChangeMessage("ws://n1:8080", "node-1", "", "", 1)
	if msg.MsgType != MsgRaftLeaderChange {
		t.Errorf("MsgType = %s, want %s", msg.MsgType, MsgRaftLeaderChange)
	}

	// Full JSON round-trip
	bytes, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var back Message
	if err := json.Unmarshal(bytes, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.MsgType != MsgRaftLeaderChange {
		t.Errorf("MsgType = %s after round-trip", back.MsgType)
	}

	// Decode payload
	var payload RaftLeaderChangePayload
	if err := back.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if payload.NewLeaderAddr != "ws://n1:8080" {
		t.Errorf("NewLeaderAddr = %s", payload.NewLeaderAddr)
	}
	if payload.NewLeaderID != "node-1" {
		t.Errorf("NewLeaderID = %s", payload.NewLeaderID)
	}
	if payload.Term != 1 {
		t.Errorf("Term = %d, want 1", payload.Term)
	}
	if payload.Timestamp == 0 {
		t.Error("Timestamp should be set")
	}

	// Test with empty old addresses (first election)
	if payload.OldLeaderAddr != "" {
		t.Errorf("OldLeaderAddr should be empty on first election, got %q", payload.OldLeaderAddr)
	}
	if payload.OldLeaderID != "" {
		t.Errorf("OldLeaderID should be empty on first election, got %q", payload.OldLeaderID)
	}

	// Test with term=0
	msg2 := NewRaftLeaderChangeMessage("ws://n1:8080", "node-1", "ws://n0:8080", "node-0", 0)
	var payload2 RaftLeaderChangePayload
	msg2.DecodePayload(&payload2)
	if payload2.Term != 0 {
		t.Errorf("Term = %d, want 0", payload2.Term)
	}
	if payload2.OldLeaderAddr != "ws://n0:8080" {
		t.Errorf("OldLeaderAddr = %s", payload2.OldLeaderAddr)
	}
}
