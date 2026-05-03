package reef

import (
	"encoding/json"
	"testing"
)

func TestCNPMessage_All16Types(t *testing.T) {
	allTypes := []CNPMessageType{
		MsgContextCorruption,
		MsgContextCompactDone,
		MsgContextInject,
		MsgContextRestore,
		MsgMemoryUpdate,
		MsgMemoryQuery,
		MsgMemoryInject,
		MsgMemoryPrune,
		MsgStrategySuggest,
		MsgStrategyAck,
		MsgStrategyResult,
		MsgCheckpointSave,
		MsgCheckpointRestore,
		MsgLongTaskHeartbeat,
		MsgLongTaskProgress,
		MsgLongTaskComplete,
	}

	if len(allTypes) != 16 {
		t.Errorf("got %d types, want 16", len(allTypes))
	}

	seen := make(map[CNPMessageType]bool)
	for _, mt := range allTypes {
		if seen[mt] {
			t.Errorf("duplicate type: %s", mt)
		}
		seen[mt] = true
	}
}

func TestCNPMessage_Serialize(t *testing.T) {
	msg := CNPMessage{
		Type:      MsgContextCorruption,
		TaskID:    "task-001",
		Timestamp: 1712345678,
		Payload:   map[string]interface{}{"tool": "exec", "count": 5},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded CNPMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Type != MsgContextCorruption {
		t.Errorf("Type = %s", decoded.Type)
	}
	if decoded.TaskID != "task-001" {
		t.Errorf("TaskID = %s", decoded.TaskID)
	}
}

func TestCNPMessage_ContextCorruption(t *testing.T) {
	msg := NewCNPMessage(MsgContextCorruption, "t-1", CorruptionPayload{
		Type:    "tool_loop",
		Tool:    "exec",
		Count:   5,
		Message: "loop detected",
	})

	if msg.Type != MsgContextCorruption {
		t.Errorf("Type = %s", msg.Type)
	}
}

func TestCNPMessage_MemoryUpdate(t *testing.T) {
	msg := NewCNPMessage(MsgMemoryUpdate, "t-1", MemoryUpdatePayload{
		EventType: "success",
		Summary:   "task completed",
		Tags:      []string{"go", "cli"},
	})

	data, _ := json.Marshal(msg)

	var decoded CNPMessage
	json.Unmarshal(data, &decoded)

	if decoded.Type != MsgMemoryUpdate {
		t.Errorf("Type = %s", decoded.Type)
	}
}

func TestCNPMessage_CheckpointSave(t *testing.T) {
	msg := NewCNPMessage(MsgCheckpointSave, "t-1", CheckpointSavePayload{
		RoundNum: 5,
		Summary:  "checkpoint at round 5",
	})

	data, _ := json.Marshal(msg)

	var decoded CNPMessage
	json.Unmarshal(data, &decoded)

	if decoded.Type != MsgCheckpointSave {
		t.Errorf("Type = %s", decoded.Type)
	}
}

func TestCNPMessage_InvalidType(t *testing.T) {
	invalidType := CNPMessageType("invalid_made_up_type")
	msg := NewCNPMessage(invalidType, "t-1", nil)

	if !IsConsensus(msg.Type) {
		// Non-consensus type, should not panic
		if msg.Type != invalidType {
			t.Error("type was changed unexpectedly")
		}
	}
}

func TestCNPMessage_ConsensusRouting(t *testing.T) {
	consensusTypes := []CNPMessageType{
		MsgMemoryUpdate,
		MsgMemoryPrune,
		MsgCheckpointSave,
		MsgContextInject,
		MsgStrategyResult,
	}

	for _, mt := range consensusTypes {
		if !IsConsensus(mt) {
			t.Errorf("%s should be consensus, but IsConsensus returned false", mt)
		}
	}

	nonConsensusTypes := []CNPMessageType{
		MsgContextCorruption,
		MsgContextCompactDone,
		MsgContextRestore,
		MsgMemoryQuery,
		MsgMemoryInject,
		MsgStrategySuggest,
		MsgStrategyAck,
		MsgCheckpointRestore,
		MsgLongTaskHeartbeat,
		MsgLongTaskProgress,
		MsgLongTaskComplete,
	}

	for _, mt := range nonConsensusTypes {
		if IsConsensus(mt) {
			t.Errorf("%s should NOT be consensus, but IsConsensus returned true", mt)
		}
	}
}

func TestCNPMessage_Timestamp(t *testing.T) {
	msg := NewCNPMessage(MsgLongTaskHeartbeat, "t-1", nil)
	if msg.Timestamp == 0 {
		t.Error("timestamp is zero")
	}
}
