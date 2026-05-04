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
	MsgGeneSubmit          MessageType = "gene_submit"
	MsgGeneApproved        MessageType = "gene_approved"
	MsgGeneRejected        MessageType = "gene_rejected"
	MsgGeneBroadcast       MessageType = "gene_broadcast"
	MsgSkillDraftProposed  MessageType = "skill_draft_proposed"
	MsgTaskClaim           MessageType = "task_claim"
	MsgTaskAvailable       MessageType = "task_available"
	MsgTaskClaimed         MessageType = "task_claimed"
	MsgTaskBlock           MessageType = "task_block"

	// Phase 7: Raft messages
	MsgRaftLeaderChange MessageType = "raft_leader_change"
)

// IsValid returns true if the message type is a known enum value.
func (mt MessageType) IsValid() bool {
	switch mt {
	case MsgRegister, MsgRegisterAck, MsgRegisterNack, MsgHeartbeat,
		MsgTaskDispatch, MsgTaskProgress, MsgTaskCompleted, MsgTaskFailed,
		MsgCancel, MsgPause, MsgResume, MsgControlAck:
		return true
	// Phase 6: Evolution Engine messages
	case MsgGeneSubmit, MsgGeneApproved, MsgGeneRejected, MsgGeneBroadcast,
		MsgSkillDraftProposed, MsgTaskClaim, MsgTaskAvailable, MsgTaskClaimed, MsgTaskBlock:
		return true
	// Phase 7: Raft messages
	case MsgRaftLeaderChange:
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
	ModelHint      string            `json:"model_hint,omitempty"`
	CreatedAt      int64             `json:"created_at"`
	ReplyTo        *ReplyToContext   `json:"reply_to,omitempty"`
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
	TaskID           string         `json:"task_id"`
	Result           map[string]any `json:"result"`
	ExecutionTimeMs  int64          `json:"execution_time_ms"`
	Timestamp        int64          `json:"timestamp"`
}

// TaskFailedPayload is sent by Client when a task fails permanently.
type TaskFailedPayload struct {
	TaskID          string          `json:"task_id"`
	ErrorType       string          `json:"error_type"`       // "execution_error", "timeout", "cancelled", "escalated"
	ErrorMessage    string          `json:"error_message"`
	ErrorDetail     string          `json:"error_detail,omitempty"`
	AttemptHistory  []AttemptRecord `json:"attempt_history"`
	Timestamp       int64           `json:"timestamp"`
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

// ---- Phase 6-7: Evolution + Raft Payloads ----

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

// SkillDraftProposedPayload is sent when a skill draft is proposed from merged genes.
type SkillDraftProposedPayload struct {
	DraftID   string `json:"draft_id"`
	Role      string `json:"role"`
	SkillName string `json:"skill_name"`
	GeneCount int    `json:"gene_count"`
	Timestamp int64  `json:"timestamp"`
}

// TaskClaimPayload is sent by Client to claim a task.
type TaskClaimPayload struct {
	TaskID    string `json:"task_id"`
	ClientID  string `json:"client_id"`
	Role      string `json:"role"`
	ClaimedAt int64  `json:"claimed_at"`
}

// TaskAvailablePayload is sent by Server to announce an available task.
type TaskAvailablePayload struct {
	TaskID         string   `json:"task_id"`
	RequiredRole   string   `json:"required_role"`
	RequiredSkills []string `json:"required_skills"`
	Priority       int      `json:"priority"`
	Instruction    string   `json:"instruction"` // first 200 chars summary
	ExpiresAt      int64    `json:"expires_at"`  // Unix millis
}

// TaskClaimedPayload is sent by Server to confirm a task has been claimed.
type TaskClaimedPayload struct {
	TaskID    string `json:"task_id"`
	ClaimedBy string `json:"claimed_by"`
	ClaimedAt int64  `json:"claimed_at"`
}

// TaskBlockPayload is sent by Client to report a blocking condition.
type TaskBlockPayload struct {
	TaskID    string `json:"task_id"`
	ClientID  string `json:"client_id"`
	BlockType string `json:"block_type"` // "tool_error", "context_corruption", "resource_unavailable"
	Message   string `json:"message"`
	Context   string `json:"context"`
	Timestamp int64  `json:"timestamp"`
}

// RaftLeaderChangePayload is sent when the Raft cluster leadership changes.
type RaftLeaderChangePayload struct {
	NewLeaderID   string `json:"new_leader_id"`
	NewLeaderAddr string `json:"new_leader_addr"`
	OldLeaderID   string `json:"old_leader_id,omitempty"`
	OldLeaderAddr string `json:"old_leader_addr,omitempty"`
	Term          uint64 `json:"term"`
	Timestamp     int64  `json:"timestamp"`
}

// ---- Validate methods for Phase 6-7 Payloads ----

func (p GeneSubmitPayload) Validate() error {
	if p.GeneID == "" {
		return fmt.Errorf("gene_id is required")
	}
	if len(p.GeneData) <= 2 {
		return fmt.Errorf("gene_data must be non-empty valid JSON")
	}
	if p.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}
	return nil
}

func (p GeneApprovedPayload) Validate() error {
	if p.GeneID == "" {
		return fmt.Errorf("gene_id is required")
	}
	return nil
}

func (p GeneRejectedPayload) Validate() error {
	if p.GeneID == "" {
		return fmt.Errorf("gene_id is required")
	}
	if p.Reason == "" {
		return fmt.Errorf("reason is required")
	}
	if p.Layer < 1 || p.Layer > 3 {
		return fmt.Errorf("layer must be 1, 2, or 3")
	}
	return nil
}

func (p GeneBroadcastPayload) Validate() error {
	if p.GeneID == "" {
		return fmt.Errorf("gene_id is required")
	}
	if len(p.GeneData) == 0 {
		return fmt.Errorf("gene_data must be non-empty")
	}
	if p.SourceClientID == "" {
		return fmt.Errorf("source_client_id is required")
	}
	return nil
}

func (p SkillDraftProposedPayload) Validate() error {
	if p.DraftID == "" {
		return fmt.Errorf("draft_id is required")
	}
	if p.Role == "" {
		return fmt.Errorf("role is required")
	}
	if p.SkillName == "" {
		return fmt.Errorf("skill_name is required")
	}
	if p.GeneCount <= 0 {
		return fmt.Errorf("gene_count must be greater than 0")
	}
	return nil
}

func (p TaskClaimPayload) Validate() error {
	if p.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if p.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}
	return nil
}

func (p TaskAvailablePayload) Validate() error {
	if p.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if p.RequiredRole == "" {
		return fmt.Errorf("required_role is required")
	}
	if p.Priority < 1 || p.Priority > 10 {
		return fmt.Errorf("priority must be between 1 and 10")
	}
	if p.ExpiresAt <= 0 {
		return fmt.Errorf("expires_at must be positive")
	}
	return nil
}

func (p TaskClaimedPayload) Validate() error {
	if p.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if p.ClaimedBy == "" {
		return fmt.Errorf("claimed_by is required")
	}
	return nil
}

func (p TaskBlockPayload) Validate() error {
	if p.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if p.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}
	switch p.BlockType {
	case "tool_error", "context_corruption", "resource_unavailable":
		return nil
	default:
		return fmt.Errorf("block_type must be one of: tool_error, context_corruption, resource_unavailable")
	}
}

func (p RaftLeaderChangePayload) Validate() error {
	if p.NewLeaderID == "" {
		return fmt.Errorf("new_leader_id is required")
	}
	if p.NewLeaderAddr == "" {
		return fmt.Errorf("new_leader_addr is required")
	}
	if p.Term <= 0 {
		return fmt.Errorf("term must be positive")
	}
	return nil
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

// NewRaftLeaderChangeMessage creates a raft_leader_change message.
func NewRaftLeaderChangeMessage(newAddr, newID, oldAddr, oldID string, term uint64) Message {
	payload := RaftLeaderChangePayload{
		NewLeaderAddr: newAddr,
		NewLeaderID:   newID,
		OldLeaderAddr: oldAddr,
		OldLeaderID:   oldID,
		Term:          term,
		Timestamp:     time.Now().UnixMilli(),
	}
	msg, _ := NewMessage(MsgRaftLeaderChange, "", payload)
	return msg
}
