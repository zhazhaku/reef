// Package reef defines the Swarm Protocol message types and serialization
// used for communication between Reef Server and Reef Client nodes.
package reef

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProtocolVersion is the current wire protocol version.
const ProtocolVersion = "reef-v1"

// MessageType enumerates all valid message types in the Swarm Protocol.
type MessageType string

const (
	MsgRegister      MessageType = "register"
	MsgRegisterAck   MessageType = "register_ack"
	MsgRegisterNack  MessageType = "register_nack"
	MsgHeartbeat     MessageType = "heartbeat"
	MsgTaskDispatch  MessageType = "task_dispatch"
	MsgTaskProgress  MessageType = "task_progress"
	MsgTaskCompleted MessageType = "task_completed"
	MsgTaskFailed    MessageType = "task_failed"
	MsgCancel        MessageType = "cancel"
	MsgPause         MessageType = "pause"
	MsgResume        MessageType = "resume"
	MsgControlAck    MessageType = "control_ack"

	// Phase 6: Evolution Engine messages
	MsgGeneSubmit    MessageType = "gene_submit"
	MsgGeneApproved  MessageType = "gene_approved"
	MsgGeneRejected  MessageType = "gene_rejected"
	MsgGeneBroadcast MessageType = "gene_broadcast"

	// Phase 6 — Claim Board messages
	MsgTaskAvailable MessageType = "task_available"
	MsgTaskClaimed   MessageType = "task_claimed"
	MsgTaskClaim     MessageType = "task_claim"

	// Phase 7 — Federation messages
	MsgRaftLeaderChange MessageType = "raft_leader_change"
)

// IsValid returns true if the message type is a known enum value.
func (mt MessageType) IsValid() bool {
	switch mt {
	case MsgRegister, MsgRegisterAck, MsgRegisterNack, MsgHeartbeat,
		MsgTaskDispatch, MsgTaskProgress, MsgTaskCompleted, MsgTaskFailed,
		MsgCancel, MsgPause, MsgResume, MsgControlAck,
		// Phase 6: Evolution Engine messages
		MsgGeneSubmit, MsgGeneApproved, MsgGeneRejected, MsgGeneBroadcast,
		// Phase 6 — Claim Board messages
		MsgTaskAvailable, MsgTaskClaimed, MsgTaskClaim,
		// Phase 7 — Federation messages
		MsgRaftLeaderChange:
		return true
	}
	return false
}

// Message is the top-level envelope for all Swarm Protocol messages.
// It uses json.RawMessage for the payload to enable two-step decoding:
// first decode the envelope to read MsgType, then unmarshal Payload into
// the concrete struct for that message type.
type Message struct {
	MsgType   MessageType     `json:"msg_type"`
	TaskID    string          `json:"task_id,omitempty"`
	Timestamp int64           `json:"timestamp"`        // Unix milliseconds
	Payload   json.RawMessage `json:"payload"`          // concrete type depends on MsgType
}

// NewMessage creates a Message envelope with the current timestamp.
func NewMessage(msgType MessageType, taskID string, payload any) (Message, error) {
	var m Message
	if !msgType.IsValid() {
		return m, fmt.Errorf("unknown message type: %s", msgType)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return m, fmt.Errorf("marshal payload: %w", err)
	}
	m = Message{
		MsgType:   msgType,
		TaskID:    taskID,
		Timestamp: time.Now().UnixMilli(),
		Payload:   payloadBytes,
	}
	return m, nil
}

// DecodePayload unmarshals the raw payload into the provided concrete value.
func (m Message) DecodePayload(v any) error {
	if err := json.Unmarshal(m.Payload, v); err != nil {
		return fmt.Errorf("decode payload for %s: %w", m.MsgType, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Payload structs — one per message type
// ---------------------------------------------------------------------------

// RegisterPayload is sent by Client to Server on initial connection.
type RegisterPayload struct {
	ProtocolVersion string   `json:"protocol_version"`
	ClientID        string   `json:"client_id"`
	Role            string   `json:"role"`
	Skills          []string `json:"skills"`
	Providers       []string `json:"providers,omitempty"`
	Capacity        int      `json:"capacity"`
	Timestamp       int64    `json:"timestamp"`
}

// RegisterAckPayload is sent by Server to confirm successful registration.
type RegisterAckPayload struct {
	ClientID  string `json:"client_id"`
	ServerTime int64 `json:"server_time"`
}

// RegisterNackPayload is sent by Server to reject registration.
type RegisterNackPayload struct {
	Reason string `json:"reason"`
}

// HeartbeatPayload is sent periodically by Client.
type HeartbeatPayload struct {
	Timestamp int64 `json:"timestamp"`
}

// TaskDispatchPayload is sent by Server to assign a task to a Client.
type TaskDispatchPayload struct {
	TaskID         string            `json:"task_id"`
	Instruction    string            `json:"instruction"`
	Context        map[string]any    `json:"context,omitempty"`
	RequiredRole   string            `json:"required_role"`
	RequiredSkills []string          `json:"required_skills,omitempty"`
	MaxRetries     int               `json:"max_retries"`
	TimeoutMs      int64             `json:"timeout_ms"`
	CreatedAt      int64             `json:"created_at"`
}

// TaskProgressPayload is sent by Client to report task execution progress.
type TaskProgressPayload struct {
	TaskID          string `json:"task_id"`
	Status          string `json:"status"`           // "started", "running", "paused"
	ProgressPercent int    `json:"progress_percent,omitempty"`
	Message         string `json:"message,omitempty"`
	Timestamp       int64  `json:"timestamp"`
}

// TaskCompletedPayload is sent by Client when a task finishes successfully.
type TaskCompletedPayload struct {
	TaskID              string         `json:"task_id"`
	Result              map[string]any `json:"result"`
	ExecutionTimeMs     int64          `json:"execution_time_ms"`
	Timestamp           int64          `json:"timestamp"`
	RoundsExecuted      int            `json:"rounds_executed,omitempty"`
	CorruptionsDetected int            `json:"corruptions_detected,omitempty"`
	WorkingSummary      string         `json:"working_summary,omitempty"`
}

// TaskFailedPayload is sent by Client when a task fails permanently.
type TaskFailedPayload struct {
	TaskID          string          `json:"task_id"`
	ErrorType       string          `json:"error_type"`       // "execution_error", "timeout", "cancelled", "escalated"
	ErrorMessage    string          `json:"error_message"`
	ErrorDetail     string          `json:"error_detail,omitempty"`
	AttemptHistory  []AttemptRecord `json:"attempt_history"`
	Timestamp       int64           `json:"timestamp"`
	RoundsExecuted  int             `json:"rounds_executed,omitempty"`
}

// ControlPayload is used for cancel/pause/resume control messages.
type ControlPayload struct {
	ControlType string `json:"control_type"` // "cancel", "pause", "resume"
	TaskID      string `json:"task_id"`
}

// ControlAckPayload is sent by Client to acknowledge a control message.
type ControlAckPayload struct {
	ControlType string `json:"control_type"`
	TaskID      string `json:"task_id"`
	Timestamp   int64  `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// Phase 6 — Claim Board payloads
// ---------------------------------------------------------------------------

// TaskAvailablePayload is sent by Server to eligible clients when a task
// is posted on the claim board.
type TaskAvailablePayload struct {
	TaskID         string   `json:"task_id"`
	RequiredRole   string   `json:"required_role"`
	RequiredSkills []string `json:"required_skills,omitempty"`
	Priority       int      `json:"priority"`
	Instruction    string   `json:"instruction"`    // first 200 chars
	ExpiresAt      int64    `json:"expires_at"`     // Unix milliseconds
}

// TaskClaimedPayload is sent by Server to other candidates when a task
// on the claim board is claimed by a client.
type TaskClaimedPayload struct {
	TaskID    string `json:"task_id"`
	ClaimedBy string `json:"claimed_by"`
	ClaimedAt int64  `json:"claimed_at"` // Unix milliseconds
}

// TaskClaimPayload is sent by Client to claim a task on the claim board.
type TaskClaimPayload struct {
	TaskID   string `json:"task_id"`
	ClientID string `json:"client_id"`
}

// ---------------------------------------------------------------------------
// Phase 6 — Evolution payloads
// ---------------------------------------------------------------------------

// GeneSubmitPayload is sent by Client to submit a gene for evolution approval.
type GeneSubmitPayload struct {
	GeneID         string          `json:"gene_id"`
	GeneData       json.RawMessage `json:"gene_data"`
	SourceEventIDs []string        `json:"source_event_ids"`
	ClientID       string          `json:"client_id"`
	Timestamp      int64           `json:"timestamp"`
}

// GeneApprovedPayload is sent by Server to approve a submitted gene.
type GeneApprovedPayload struct {
	GeneID     string `json:"gene_id"`
	ApprovedBy string `json:"approved_by"`
	ServerTime int64  `json:"server_time"`
}

// GeneRejectedPayload is sent by Server to reject a submitted gene.
type GeneRejectedPayload struct {
	GeneID     string `json:"gene_id"`
	Reason     string `json:"reason"`
	Layer      int    `json:"layer"` // which gatekeeper layer rejected: 1/2/3
	ServerTime int64  `json:"server_time"`
}

// GeneBroadcastPayload is sent by Server to broadcast an approved gene to all Clients.
type GeneBroadcastPayload struct {
	GeneID         string          `json:"gene_id"`
	GeneData       json.RawMessage `json:"gene_data"`
	SourceClientID string          `json:"source_client_id"`
	ApprovedAt     int64           `json:"approved_at"` // Unix millis
	BroadcastBy    string          `json:"broadcast_by"`
}

// ---------------------------------------------------------------------------
// Phase 7 — Federation payloads
// ---------------------------------------------------------------------------

// RaftLeaderChangePayload is sent by the Raft cluster to all connected clients
// when a new Leader is elected. Clients use this to update their connection pool.
type RaftLeaderChangePayload struct {
	NewLeaderAddr string `json:"new_leader_addr"` // WebSocket address of the new Leader
	NewLeaderID   string `json:"new_leader_id"`   // Raft node ID of the new Leader
	OldLeaderAddr string `json:"old_leader_addr"` // Previous Leader address (empty on first election)
	OldLeaderID   string `json:"old_leader_id"`   // Previous Leader ID (empty on first election)
	Term          uint64 `json:"term"`            // Raft term number
	Timestamp     int64  `json:"timestamp"`       // Unix milliseconds when the change occurred
}

// NewRaftLeaderChangeMessage creates a properly typed Message with the
// RaftLeaderChange payload.
func NewRaftLeaderChangeMessage(newAddr, newID, oldAddr, oldID string, term uint64) Message {
	payload := RaftLeaderChangePayload{
		NewLeaderAddr: newAddr,
		NewLeaderID:   newID,
		OldLeaderAddr: oldAddr,
		OldLeaderID:   oldID,
		Term:          term,
		Timestamp:     time.Now().UnixMilli(),
	}
	payloadBytes, _ := json.Marshal(payload)
	return Message{
		MsgType:   MsgRaftLeaderChange,
		Timestamp: time.Now().UnixMilli(),
		Payload:   payloadBytes,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ValidateProtocolVersion returns an error if the version does not match.
func ValidateProtocolVersion(v string) error {
	if v != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %q (expected %s)", v, ProtocolVersion)
	}
	return nil
}
