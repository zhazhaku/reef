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

// ---- Phase 6-7 Tests ----

func TestPayloadValidate(t *testing.T) {
	t.Run("GeneSubmitPayload", func(t *testing.T) {
		valid := GeneSubmitPayload{GeneID: "g1", GeneData: json.RawMessage(`{"x":1}`), ClientID: "c1"}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		if err := (GeneSubmitPayload{GeneData: json.RawMessage(`{}`), ClientID: "c1"}).Validate(); err == nil {
			t.Error("expected error for empty gene_id")
		}
		if err := (GeneSubmitPayload{GeneID: "g1", GeneData: json.RawMessage(``), ClientID: "c1"}).Validate(); err == nil {
			t.Error("expected error for empty gene_data")
		}
		if err := (GeneSubmitPayload{GeneID: "g1", GeneData: json.RawMessage(`{"x":1}`)}).Validate(); err == nil {
			t.Error("expected error for empty client_id")
		}
	})

	t.Run("GeneApprovedPayload", func(t *testing.T) {
		valid := GeneApprovedPayload{GeneID: "g1", ApprovedBy: "srv1"}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		if err := (GeneApprovedPayload{ApprovedBy: "srv1"}).Validate(); err == nil {
			t.Error("expected error for empty gene_id")
		}
	})

	t.Run("GeneRejectedPayload", func(t *testing.T) {
		valid := GeneRejectedPayload{GeneID: "g1", Reason: "bad", Layer: 2}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		if err := (GeneRejectedPayload{Reason: "bad", Layer: 2}).Validate(); err == nil {
			t.Error("expected error for empty gene_id")
		}
		if err := (GeneRejectedPayload{GeneID: "g1", Layer: 2}).Validate(); err == nil {
			t.Error("expected error for empty reason")
		}
		if err := (GeneRejectedPayload{GeneID: "g1", Reason: "bad", Layer: 0}).Validate(); err == nil {
			t.Error("expected error for layer out of range (0)")
		}
		if err := (GeneRejectedPayload{GeneID: "g1", Reason: "bad", Layer: 4}).Validate(); err == nil {
			t.Error("expected error for layer out of range (4)")
		}
	})

	t.Run("GeneBroadcastPayload", func(t *testing.T) {
		valid := GeneBroadcastPayload{GeneID: "g1", GeneData: json.RawMessage(`{"x":1}`), SourceClientID: "c1"}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		if err := (GeneBroadcastPayload{GeneData: json.RawMessage(`{"x":1}`), SourceClientID: "c1"}).Validate(); err == nil {
			t.Error("expected error for empty gene_id")
		}
		if err := (GeneBroadcastPayload{GeneID: "g1", SourceClientID: "c1"}).Validate(); err == nil {
			t.Error("expected error for nil/empty gene_data")
		}
		if err := (GeneBroadcastPayload{GeneID: "g1", GeneData: json.RawMessage(`{"x":1}`)}).Validate(); err == nil {
			t.Error("expected error for empty source_client_id")
		}
	})

	t.Run("SkillDraftProposedPayload", func(t *testing.T) {
		valid := SkillDraftProposedPayload{DraftID: "d1", Role: "coder", SkillName: "test", GeneCount: 3}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		if err := (SkillDraftProposedPayload{Role: "coder", SkillName: "test", GeneCount: 3}).Validate(); err == nil {
			t.Error("expected error for empty draft_id")
		}
		if err := (SkillDraftProposedPayload{DraftID: "d1", SkillName: "test", GeneCount: 3}).Validate(); err == nil {
			t.Error("expected error for empty role")
		}
		if err := (SkillDraftProposedPayload{DraftID: "d1", Role: "coder", GeneCount: 3}).Validate(); err == nil {
			t.Error("expected error for empty skill_name")
		}
		if err := (SkillDraftProposedPayload{DraftID: "d1", Role: "coder", SkillName: "test", GeneCount: 0}).Validate(); err == nil {
			t.Error("expected error for zero gene_count")
		}
		if err := (SkillDraftProposedPayload{DraftID: "d1", Role: "coder", SkillName: "test", GeneCount: -1}).Validate(); err == nil {
			t.Error("expected error for negative gene_count")
		}
	})

	t.Run("TaskClaimPayload", func(t *testing.T) {
		valid := TaskClaimPayload{TaskID: "t1", ClientID: "c1", Role: "coder"}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		if err := (TaskClaimPayload{ClientID: "c1"}).Validate(); err == nil {
			t.Error("expected error for empty task_id")
		}
		if err := (TaskClaimPayload{TaskID: "t1"}).Validate(); err == nil {
			t.Error("expected error for empty client_id")
		}
	})

	t.Run("TaskAvailablePayload", func(t *testing.T) {
		valid := TaskAvailablePayload{TaskID: "t1", RequiredRole: "coder", Priority: 5, ExpiresAt: 1000}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		if err := (TaskAvailablePayload{RequiredRole: "coder", Priority: 5, ExpiresAt: 1000}).Validate(); err == nil {
			t.Error("expected error for empty task_id")
		}
		if err := (TaskAvailablePayload{TaskID: "t1", Priority: 5, ExpiresAt: 1000}).Validate(); err == nil {
			t.Error("expected error for empty required_role")
		}
		if err := (TaskAvailablePayload{TaskID: "t1", RequiredRole: "coder", Priority: 0, ExpiresAt: 1000}).Validate(); err == nil {
			t.Error("expected error for priority out of range (0)")
		}
		if err := (TaskAvailablePayload{TaskID: "t1", RequiredRole: "coder", Priority: 11, ExpiresAt: 1000}).Validate(); err == nil {
			t.Error("expected error for priority out of range (11)")
		}
		if err := (TaskAvailablePayload{TaskID: "t1", RequiredRole: "coder", Priority: 5, ExpiresAt: 0}).Validate(); err == nil {
			t.Error("expected error for zero expires_at")
		}
	})

	t.Run("TaskClaimedPayload", func(t *testing.T) {
		valid := TaskClaimedPayload{TaskID: "t1", ClaimedBy: "c1"}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		if err := (TaskClaimedPayload{ClaimedBy: "c1"}).Validate(); err == nil {
			t.Error("expected error for empty task_id")
		}
		if err := (TaskClaimedPayload{TaskID: "t1"}).Validate(); err == nil {
			t.Error("expected error for empty claimed_by")
		}
	})

	t.Run("TaskBlockPayload", func(t *testing.T) {
		valid := TaskBlockPayload{TaskID: "t1", ClientID: "c1", BlockType: "tool_error"}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		// All valid block types
		for _, bt := range []string{"tool_error", "context_corruption", "resource_unavailable"} {
			if err := (TaskBlockPayload{TaskID: "t1", ClientID: "c1", BlockType: bt}).Validate(); err != nil {
				t.Errorf("valid block_type %q should pass: %v", bt, err)
			}
		}
		if err := (TaskBlockPayload{ClientID: "c1", BlockType: "tool_error"}).Validate(); err == nil {
			t.Error("expected error for empty task_id")
		}
		if err := (TaskBlockPayload{TaskID: "t1", BlockType: "tool_error"}).Validate(); err == nil {
			t.Error("expected error for empty client_id")
		}
		if err := (TaskBlockPayload{TaskID: "t1", ClientID: "c1", BlockType: "unknown_type"}).Validate(); err == nil {
			t.Error("expected error for unknown block_type")
		}
	})

	t.Run("RaftLeaderChangePayload", func(t *testing.T) {
		valid := RaftLeaderChangePayload{NewLeaderID: "n1", NewLeaderAddr: "10.0.0.1:8080", Term: 5}
		if err := valid.Validate(); err != nil {
			t.Errorf("valid payload should pass: %v", err)
		}
		if err := (RaftLeaderChangePayload{NewLeaderAddr: "10.0.0.1:8080", Term: 5}).Validate(); err == nil {
			t.Error("expected error for empty new_leader_id")
		}
		if err := (RaftLeaderChangePayload{NewLeaderID: "n1", Term: 5}).Validate(); err == nil {
			t.Error("expected error for empty new_leader_addr")
		}
		if err := (RaftLeaderChangePayload{NewLeaderID: "n1", NewLeaderAddr: "10.0.0.1:8080", Term: 0}).Validate(); err == nil {
			t.Error("expected error for zero term")
		}
	})
}

func TestEvolutionMessageRoundTrip(t *testing.T) {
	geneJSON := json.RawMessage(`{"gene_id":"g1","fitness":0.95,"dna":"AABBCC"}`)

	payloads := []struct {
		name    string
		msgType MessageType
		payload any
		decode  func(*Message) error
		verify  func(t *testing.T)
	}{
		{
			name:    "gene_submit",
			msgType: MsgGeneSubmit,
			payload: GeneSubmitPayload{
				GeneID: "gene-001", GeneData: geneJSON,
				SourceEventIDs: []string{"evt-1", "evt-2"},
				ClientID: "client-1", Timestamp: 1700000000000,
			},
			decode: func(m *Message) error {
				var p GeneSubmitPayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if p.GeneID != "gene-001" {
					t.Errorf("GeneID = %s, want gene-001", p.GeneID)
				}
				if string(p.GeneData) != string(geneJSON) {
					t.Errorf("GeneData mismatch: %s vs %s", p.GeneData, geneJSON)
				}
				if len(p.SourceEventIDs) != 2 {
					t.Errorf("SourceEventIDs len = %d, want 2", len(p.SourceEventIDs))
				}
				if p.ClientID != "client-1" {
					t.Errorf("ClientID = %s, want client-1", p.ClientID)
				}
				return nil
			},
		},
		{
			name:    "gene_approved",
			msgType: MsgGeneApproved,
			payload: GeneApprovedPayload{GeneID: "gene-001", ApprovedBy: "server-1", ServerTime: 1700000001000},
			decode: func(m *Message) error {
				var p GeneApprovedPayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if p.GeneID != "gene-001" {
					t.Errorf("GeneID = %s, want gene-001", p.GeneID)
				}
				return nil
			},
		},
		{
			name:    "gene_rejected",
			msgType: MsgGeneRejected,
			payload: GeneRejectedPayload{GeneID: "gene-001", Reason: "fitness too low", Layer: 2, ServerTime: 1700000002000},
			decode: func(m *Message) error {
				var p GeneRejectedPayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if p.Layer != 2 {
					t.Errorf("Layer = %d, want 2", p.Layer)
				}
				return nil
			},
		},
		{
			name:    "gene_broadcast",
			msgType: MsgGeneBroadcast,
			payload: GeneBroadcastPayload{
				GeneID: "gene-001", GeneData: geneJSON,
				SourceClientID: "client-1", ApprovedAt: 1700000003000, BroadcastBy: "server-1",
			},
			decode: func(m *Message) error {
				var p GeneBroadcastPayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if string(p.GeneData) != string(geneJSON) {
					t.Errorf("GeneData byte-for-byte mismatch")
				}
				if p.SourceClientID != "client-1" {
					t.Errorf("SourceClientID = %s, want client-1", p.SourceClientID)
				}
				return nil
			},
		},
		{
			name:    "skill_draft_proposed",
			msgType: MsgSkillDraftProposed,
			payload: SkillDraftProposedPayload{
				DraftID: "draft-1", Role: "coder", SkillName: "github-integration",
				GeneCount: 5, Timestamp: 1700000004000,
			},
			decode: func(m *Message) error {
				var p SkillDraftProposedPayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if p.GeneCount != 5 {
					t.Errorf("GeneCount = %d, want 5", p.GeneCount)
				}
				return nil
			},
		},
		{
			name:    "task_claim",
			msgType: MsgTaskClaim,
			payload: TaskClaimPayload{
				TaskID: "task-1", ClientID: "client-1", Role: "coder", ClaimedAt: 1700000005000,
			},
			decode: func(m *Message) error {
				var p TaskClaimPayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if p.TaskID != "task-1" {
					t.Errorf("TaskID = %s, want task-1", p.TaskID)
				}
				return nil
			},
		},
		{
			name:    "task_available",
			msgType: MsgTaskAvailable,
			payload: TaskAvailablePayload{
				TaskID: "task-1", RequiredRole: "coder",
				RequiredSkills: []string{"github", "docker"},
				Priority: 7, Instruction: "Fix the login bug", ExpiresAt: 1700000100000,
			},
			decode: func(m *Message) error {
				var p TaskAvailablePayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if p.Priority != 7 {
					t.Errorf("Priority = %d, want 7", p.Priority)
				}
				if len(p.RequiredSkills) != 2 {
					t.Errorf("RequiredSkills len = %d, want 2", len(p.RequiredSkills))
				}
				return nil
			},
		},
		{
			name:    "task_claimed",
			msgType: MsgTaskClaimed,
			payload: TaskClaimedPayload{TaskID: "task-1", ClaimedBy: "client-1", ClaimedAt: 1700000006000},
			decode: func(m *Message) error {
				var p TaskClaimedPayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if p.ClaimedBy != "client-1" {
					t.Errorf("ClaimedBy = %s, want client-1", p.ClaimedBy)
				}
				return nil
			},
		},
		{
			name:    "task_block",
			msgType: MsgTaskBlock,
			payload: TaskBlockPayload{
				TaskID: "task-1", ClientID: "client-1",
				BlockType: "resource_unavailable", Message: "GPU OOM",
				Context: "trying to allocate 8GB", Timestamp: 1700000007000,
			},
			decode: func(m *Message) error {
				var p TaskBlockPayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if p.BlockType != "resource_unavailable" {
					t.Errorf("BlockType = %s, want resource_unavailable", p.BlockType)
				}
				return nil
			},
		},
		{
			name:    "raft_leader_change",
			msgType: MsgRaftLeaderChange,
			payload: RaftLeaderChangePayload{
				NewLeaderID: "node-1", NewLeaderAddr: "10.0.0.1:8080",
				Term: 42, Timestamp: 1700000008000,
			},
			decode: func(m *Message) error {
				var p RaftLeaderChangePayload
				if err := m.DecodePayload(&p); err != nil {
					return err
				}
				if p.Term != 42 {
					t.Errorf("Term = %d, want 42", p.Term)
				}
				if p.NewLeaderAddr != "10.0.0.1:8080" {
					t.Errorf("NewLeaderAddr = %s, want 10.0.0.1:8080", p.NewLeaderAddr)
				}
				return nil
			},
		},
	}

	for _, tt := range payloads {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := NewMessage(tt.msgType, "task-123", tt.payload)
			if err != nil {
				t.Fatalf("NewMessage failed for %s: %v", tt.name, err)
			}

			if msg.MsgType != tt.msgType {
				t.Errorf("MsgType = %s, want %s", msg.MsgType, tt.msgType)
			}

			// Full JSON marshal/unmarshal round-trip
			bytes, err := json.Marshal(msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var back Message
			if err := json.Unmarshal(bytes, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if back.MsgType != tt.msgType {
				t.Errorf("round-trip MsgType = %s, want %s", back.MsgType, tt.msgType)
			}
			if back.TaskID != "task-123" {
				t.Errorf("round-trip TaskID = %s, want task-123", back.TaskID)
			}

			// Decode and verify
			if tt.decode != nil {
				if err := tt.decode(&back); err != nil {
					t.Fatalf("decode verification failed: %v", err)
				}
			}
		})
	}

	// Edge case: verify NewMessage with nonexistent type still errors
	t.Run("unknown_type_still_errors", func(t *testing.T) {
		_, err := NewMessage(MessageType("nonexistent"), "", GeneSubmitPayload{})
		if err == nil {
			t.Error("expected error for nonexistent message type")
		}
	})

	// Edge case: nil payload should not panic
	t.Run("nil_payload_no_panic", func(t *testing.T) {
		for _, mt := range []MessageType{
			MsgGeneSubmit, MsgGeneApproved, MsgGeneRejected, MsgGeneBroadcast,
			MsgSkillDraftProposed, MsgTaskClaim, MsgTaskAvailable, MsgTaskClaimed,
			MsgTaskBlock, MsgRaftLeaderChange,
		} {
			_, err := NewMessage(mt, "", nil)
			if err != nil {
				t.Fatalf("NewMessage with nil payload for %s should not error: %v", mt, err)
			}
		}
	})
}
