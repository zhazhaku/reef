package reef

import (
	"encoding/json"
	"time"
)

// CNPMessageType identifies the kind of Cognitive Network Protocol message.
type CNPMessageType string

// All 16 CNP message types.
const (
	// Context layer messages
	MsgContextCorruption  CNPMessageType = "context_corruption"
	MsgContextCompactDone CNPMessageType = "context_compact_done"
	MsgContextInject      CNPMessageType = "context_inject"
	MsgContextRestore     CNPMessageType = "context_restore"

	// Memory layer messages
	MsgMemoryUpdate CNPMessageType = "memory_update"
	MsgMemoryQuery  CNPMessageType = "memory_query"
	MsgMemoryInject CNPMessageType = "memory_inject"
	MsgMemoryPrune  CNPMessageType = "memory_prune"

	// Strategy messages
	MsgStrategySuggest CNPMessageType = "strategy_suggest"
	MsgStrategyAck     CNPMessageType = "strategy_ack"
	MsgStrategyResult  CNPMessageType = "strategy_result"

	// Checkpoint messages
	MsgCheckpointSave    CNPMessageType = "checkpoint_save"
	MsgCheckpointRestore CNPMessageType = "checkpoint_restore"

	// Long-running task messages
	MsgLongTaskHeartbeat CNPMessageType = "long_task_heartbeat"
	MsgLongTaskProgress  CNPMessageType = "long_task_progress"
	MsgLongTaskComplete  CNPMessageType = "long_task_complete"
)

// CNPMessage is the wire format for cognitive protocol messages.
type CNPMessage struct {
	Type      CNPMessageType `json:"type"`
	TaskID    string         `json:"task_id"`
	Timestamp int64          `json:"timestamp"`
	Payload   interface{}    `json:"payload"`
}

// ConsensusTypes maps message types that require Raft consensus.
var ConsensusTypes = map[CNPMessageType]bool{
	MsgMemoryUpdate:   true,
	MsgMemoryPrune:    true,
	MsgCheckpointSave: true,
	MsgContextInject:  true,
	MsgStrategyResult: true,
}

// IsConsensus returns true if the message type requires Raft consensus.
func IsConsensus(msgType CNPMessageType) bool {
	return ConsensusTypes[msgType]
}

// NewCNPMessage creates a CNP message with the current timestamp.
func NewCNPMessage(msgType CNPMessageType, taskID string, payload interface{}) CNPMessage {
	return CNPMessage{
		Type:      msgType,
		TaskID:    taskID,
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}
}

// ---------------------------------------------------------------------------
// Payload types
// ---------------------------------------------------------------------------

// CorruptionPayload carries context corruption details.
type CorruptionPayload struct {
	Type    string `json:"corruption_type"`
	Tool    string `json:"tool,omitempty"`
	Count   int    `json:"count,omitempty"`
	Message string `json:"message"`
}

// MemoryUpdatePayload carries episodic memory extraction results.
type MemoryUpdatePayload struct {
	EventType string   `json:"event_type"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags,omitempty"`
}

// MemoryQueryPayload carries a memory search query.
type MemoryQueryPayload struct {
	Query string   `json:"query"`
	Tags  []string `json:"tags,omitempty"`
	Limit int      `json:"limit"`
}

// MemoryInjectPayload carries retrieved memories for context injection.
type MemoryInjectPayload struct {
	Genes    []GeneInject    `json:"genes,omitempty"`
	Episodes []EpisodicInject `json:"episodes,omitempty"`
}

// GeneInject is a gene snippet for context injection.
type GeneInject struct {
	Content   string  `json:"content"`
	Relevance float64 `json:"relevance"`
}

// EpisodicInject is an episodic snippet for context injection.
type EpisodicInject struct {
	Summary   string  `json:"summary"`
	Relevance float64 `json:"relevance"`
}

// StrategySuggestPayload carries a suggested strategy change.
type StrategySuggestPayload struct {
	Reason     string `json:"reason"`
	Suggestion string `json:"suggestion"`
	Priority   string `json:"priority"` // "high", "medium", "low"
}

// StrategyAckPayload acknowledges a strategy suggestion.
type StrategyAckPayload struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

// StrategyResultPayload reports the outcome of a strategy execution.
type StrategyResultPayload struct {
	Success bool   `json:"success"`
	Summary string `json:"summary"`
	Metrics map[string]float64 `json:"metrics,omitempty"`
}

// CheckpointSavePayload carries a checkpoint snapshot.
type CheckpointSavePayload struct {
	RoundNum int    `json:"round_num"`
	Summary  string `json:"summary"`
}

// LongTaskProgressPayload carries progress for a long-running task.
type LongTaskProgressPayload struct {
	RoundNum  int     `json:"round_num"`
	Progress  float64 `json:"progress"` // 0.0–1.0
	Message   string  `json:"message"`
}

// Serialize marshals a CNPMessage to JSON bytes.
func (m CNPMessage) Serialize() ([]byte, error) {
	return json.Marshal(m)
}

// DeserializeCNP unmarshals JSON bytes into a CNPMessage.
func DeserializeCNP(data []byte) (*CNPMessage, error) {
	var msg CNPMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
