// Package raft provides Reef v1 Raft-based federation.
// Command payload structs and dispatch routing.
package raft

import "encoding/json"

// =====================================================================
// Task payloads
// =====================================================================

// TaskEnqueuePayload is the payload for CmdTaskEnqueue.
type TaskEnqueuePayload struct {
	TaskID   string          `json:"task_id"`
	TaskData json.RawMessage `json:"task_data"`
}

// TaskAssignPayload is the payload for CmdTaskAssign.
type TaskAssignPayload struct {
	TaskID   string `json:"task_id"`
	ClientID string `json:"client_id"`
}

// TaskStartPayload is the payload for CmdTaskStart.
type TaskStartPayload struct {
	TaskID   string `json:"task_id"`
	ClientID string `json:"client_id"`
}

// TaskCompletePayload is the payload for CmdTaskComplete.
type TaskCompletePayload struct {
	TaskID          string          `json:"task_id"`
	ClientID        string          `json:"client_id"`
	Result          json.RawMessage `json:"result"`
	ExecutionTimeMs int64           `json:"execution_time_ms"`
}

// TaskFailedPayload is the payload for CmdTaskFailed.
type TaskFailedPayload struct {
	TaskID       string          `json:"task_id"`
	ClientID     string          `json:"client_id"`
	Error        json.RawMessage `json:"error"`
	AttemptCount int             `json:"attempt_count"`
}

// TaskCancelPayload is the payload for CmdTaskCancel.
type TaskCancelPayload struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

// TaskEscalatePayload is the payload for CmdTaskEscalate.
type TaskEscalatePayload struct {
	TaskID          string `json:"task_id"`
	EscalationLevel int    `json:"escalation_level"`
}

// TaskTimedOutPayload is the payload for CmdTaskTimedOut.
type TaskTimedOutPayload struct {
	TaskID    string `json:"task_id"`
	TimeoutAt int64  `json:"timeout_at"`
}

// =====================================================================
// Client payloads
// =====================================================================

// ClientRegisterPayload is the payload for CmdClientRegister.
type ClientRegisterPayload struct {
	ClientID string   `json:"client_id"`
	Role     string   `json:"role"`
	Skills   []string `json:"skills"`
	Capacity int      `json:"capacity"`
}

// ClientUnregisterPayload is the payload for CmdClientUnregister.
type ClientUnregisterPayload struct {
	ClientID string `json:"client_id"`
}

// ClientStalePayload is the payload for CmdClientStale.
type ClientStalePayload struct {
	ClientID string `json:"client_id"`
}

// =====================================================================
// Evolution payloads
// =====================================================================

// GeneSubmitPayload is the payload for CmdGeneSubmit.
type GeneSubmitPayload struct {
	Gene json.RawMessage `json:"gene"`
}

// GeneApprovePayload is the payload for CmdGeneApprove.
type GeneApprovePayload struct {
	GeneID       string `json:"gene_id"`
	ApproverNode string `json:"approver_node"`
}

// GeneRejectPayload is the payload for CmdGeneReject.
type GeneRejectPayload struct {
	GeneID       string `json:"gene_id"`
	Reason       string `json:"reason"`
	RejecterNode string `json:"rejecter_node"`
}

// SkillDraftPayload is the payload for CmdSkillDraft.
type SkillDraftPayload struct {
	Draft json.RawMessage `json:"draft"`
}

// SkillApprovePayload is the payload for CmdSkillApprove.
type SkillApprovePayload struct {
	DraftID  string `json:"draft_id"`
	Approver string `json:"approver"`
}

// SkillRejectPayload is the payload for CmdSkillReject.
type SkillRejectPayload struct {
	DraftID  string `json:"draft_id"`
	Reason   string `json:"reason"`
	Rejecter string `json:"rejecter"`
}

// =====================================================================
// Claim payloads
// =====================================================================

// ClaimPostPayload is the payload for CmdClaimPost.
type ClaimPostPayload struct {
	TaskID   string          `json:"task_id"`
	TaskData json.RawMessage `json:"task_data"`
}

// ClaimAssignPayload is the payload for CmdClaimAssign.
type ClaimAssignPayload struct {
	TaskID   string `json:"task_id"`
	ClientID string `json:"client_id"`
}

// ClaimExpirePayload is the payload for CmdClaimExpire.
type ClaimExpirePayload struct {
	TaskID     string `json:"task_id"`
	RetryCount int    `json:"retry_count"`
}

// =====================================================================
// DAG payload
// =====================================================================

// DagUpdatePayload is the payload for CmdDagUpdate.
type DagUpdatePayload struct {
	NodeID    string          `json:"node_id"`
	Status    string          `json:"status"`
	Output    json.RawMessage `json:"output"`
	UpdatedAt int64           `json:"updated_at"`
}

// =====================================================================
// Dispatch: Consensus vs NonConsensus routing
// =====================================================================

// DispatchTarget indicates where a command should be routed.
type DispatchTarget int

const (
	DispatchConsensus    DispatchTarget = iota // Route through Raft consensus
	DispatchNonConsensus                        // Execute locally without consensus
)

// DispatchTable maps each RaftCommandType to its dispatch target.
var DispatchTable = map[RaftCommandType]DispatchTarget{
	CmdTaskEnqueue:      DispatchConsensus,
	CmdTaskAssign:       DispatchConsensus,
	CmdTaskStart:        DispatchConsensus,
	CmdTaskComplete:     DispatchConsensus,
	CmdTaskFailed:       DispatchConsensus,
	CmdTaskCancel:       DispatchConsensus,
	CmdTaskEscalate:     DispatchConsensus,
	CmdTaskTimedOut:     DispatchConsensus,
	CmdClientRegister:   DispatchConsensus,
	CmdClientUnregister: DispatchConsensus,
	CmdClientStale:      DispatchConsensus,
	CmdGeneSubmit:       DispatchConsensus,
	CmdGeneApprove:      DispatchConsensus,
	CmdGeneReject:       DispatchConsensus,
	CmdSkillDraft:       DispatchConsensus,
	CmdSkillApprove:     DispatchConsensus,
	CmdSkillReject:      DispatchConsensus,
	CmdClaimPost:        DispatchConsensus,
	CmdClaimAssign:      DispatchConsensus,
	CmdClaimExpire:      DispatchConsensus,
	CmdDagUpdate:        DispatchConsensus,
}

// GetDispatchTarget returns the dispatch target for a command type.
// Unknown types default to DispatchNonConsensus.
func GetDispatchTarget(typ RaftCommandType) DispatchTarget {
	if target, ok := DispatchTable[typ]; ok {
		return target
	}
	return DispatchNonConsensus
}

// DispatchesToConsensus is a convenience wrapper.
func DispatchesToConsensus(typ RaftCommandType) bool {
	return GetDispatchTarget(typ) == DispatchConsensus
}
