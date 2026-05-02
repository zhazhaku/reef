// Package raft provides Federation types for Reef v1.
// This is the type definition for TDD — implementation follows.
package raft

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sipeed/reef/pkg/reef"
	"github.com/sipeed/reef/pkg/reef/evolution"
	bolt "go.etcd.io/bbolt"
	"go.etcd.io/raft/v3"
	"go.etcd.io/raft/v3/raftpb"
)

// =====================================================================
// RaftCommand types (P7-03)
// =====================================================================

type RaftCommandType int32

const (
	CmdTaskEnqueue   RaftCommandType = 1
	CmdTaskAssign    RaftCommandType = 2
	CmdTaskStart     RaftCommandType = 3
	CmdTaskComplete  RaftCommandType = 4
	CmdTaskFailed    RaftCommandType = 5
	CmdTaskCancel    RaftCommandType = 6
	CmdTaskEscalate  RaftCommandType = 7
	CmdTaskTimedOut  RaftCommandType = 8
	// 9-19 reserved for future task operations

	CmdClientRegister   RaftCommandType = 20
	CmdClientUnregister RaftCommandType = 21
	CmdClientStale      RaftCommandType = 22
	// 23-29 reserved for future client operations

	CmdGeneSubmit   RaftCommandType = 30
	CmdGeneApprove  RaftCommandType = 31
	CmdGeneReject   RaftCommandType = 32
	CmdSkillDraft   RaftCommandType = 33
	CmdSkillApprove RaftCommandType = 34
	CmdSkillReject  RaftCommandType = 35
	// 36-39 reserved for future evolution operations

	CmdClaimPost   RaftCommandType = 40
	CmdClaimAssign RaftCommandType = 41
	CmdClaimExpire RaftCommandType = 42
	// 43-49 reserved for future claim operations

	CmdDagUpdate RaftCommandType = 50
	// 51-MaxInt32 reserved for future DAG and other operations
)

// String returns the human-readable name of the command type.
func (t RaftCommandType) String() string {
	switch t {
	case CmdTaskEnqueue:
		return "CmdTaskEnqueue"
	case CmdTaskAssign:
		return "CmdTaskAssign"
	case CmdTaskStart:
		return "CmdTaskStart"
	case CmdTaskComplete:
		return "CmdTaskComplete"
	case CmdTaskFailed:
		return "CmdTaskFailed"
	case CmdTaskCancel:
		return "CmdTaskCancel"
	case CmdTaskEscalate:
		return "CmdTaskEscalate"
	case CmdTaskTimedOut:
		return "CmdTaskTimedOut"
	case CmdClientRegister:
		return "CmdClientRegister"
	case CmdClientUnregister:
		return "CmdClientUnregister"
	case CmdClientStale:
		return "CmdClientStale"
	case CmdGeneSubmit:
		return "CmdGeneSubmit"
	case CmdGeneApprove:
		return "CmdGeneApprove"
	case CmdGeneReject:
		return "CmdGeneReject"
	case CmdSkillDraft:
		return "CmdSkillDraft"
	case CmdSkillApprove:
		return "CmdSkillApprove"
	case CmdSkillReject:
		return "CmdSkillReject"
	case CmdClaimPost:
		return "CmdClaimPost"
	case CmdClaimAssign:
		return "CmdClaimAssign"
	case CmdClaimExpire:
		return "CmdClaimExpire"
	case CmdDagUpdate:
		return "CmdDagUpdate"
	default:
		return fmt.Sprintf("RaftCommandType(%d)", int32(t))
	}
}

// IsTaskOp returns true for task-related command types (1-8).
func (t RaftCommandType) IsTaskOp() bool {
	return t >= 1 && t <= 8
}

// IsClientOp returns true for client management command types (20-22).
func (t RaftCommandType) IsClientOp() bool {
	return t >= 20 && t <= 22
}

// IsEvolutionOp returns true for evolution-related command types (30-35).
func (t RaftCommandType) IsEvolutionOp() bool {
	return t >= 30 && t <= 35
}

// IsClaimOp returns true for claim board command types (40-42).
func (t RaftCommandType) IsClaimOp() bool {
	return t >= 40 && t <= 42
}

// IsDagOp returns true for DAG command types (50).
func (t RaftCommandType) IsDagOp() bool {
	return t == 50
}

// Domain returns the domain name for this command type.
func (t RaftCommandType) Domain() string {
	switch {
	case t.IsTaskOp():
		return "task"
	case t.IsClientOp():
		return "client"
	case t.IsEvolutionOp():
		return "evolution"
	case t.IsClaimOp():
		return "claim"
	case t.IsDagOp():
		return "dag"
	default:
		return "unknown"
	}
}

type RaftCommand struct {
	Type      RaftCommandType `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp"`
	Proposer  string          `json:"proposer"`
}

// NewRaftCommand creates a RaftCommand with the current timestamp and
// serialized payload. Returns error if payload marshaling fails.
func NewRaftCommand(typ RaftCommandType, payload interface{}, proposer string) (*RaftCommand, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	return &RaftCommand{
		Type:      typ,
		Payload:   json.RawMessage(payloadBytes),
		Timestamp: time.Now().UnixMilli(),
		Proposer:  proposer,
	}, nil
}

// Serialize marshals the RaftCommand to JSON bytes (for writing to the Raft log).
func (c *RaftCommand) Serialize() ([]byte, error) {
	return json.Marshal(c)
}

// DeserializeRaftCommand unmarshals JSON bytes into a RaftCommand.
func DeserializeRaftCommand(data []byte) (*RaftCommand, error) {
	var cmd RaftCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}

// UnmarshalPayload deserializes the command payload into the given target.
func (c *RaftCommand) UnmarshalPayload(target interface{}) error {
	return json.Unmarshal(c.Payload, target)
}

// Validate checks that the command has a known type and valid JSON payload.
func (c *RaftCommand) Validate() error {
	if c == nil {
		return fmt.Errorf("nil RaftCommand")
	}
	if c.Type.Domain() == "unknown" {
		return fmt.Errorf("unknown RaftCommandType: %s", c.Type.String())
	}
	if len(c.Payload) > 0 {
		var js json.RawMessage
		if err := json.Unmarshal(c.Payload, &js); err != nil {
			return fmt.Errorf("invalid payload JSON: %w", err)
		}
	}
	return nil
}

// IsConsensus returns true if this command must go through Raft consensus.
// Local-only commands (currently none defined) return false.
func (c *RaftCommand) IsConsensus() bool {
	if c == nil {
		return false
	}
	// All currently defined command types require consensus.
	// Future local-only types would be excluded here.
	return c.Type.Domain() != "unknown"
}

func (c *RaftCommand) IsLocal() bool {
	return !c.IsConsensus()
}

var ErrNotLeader = fmt.Errorf("not leader")

// =====================================================================
// BoltStore (P7-02)
// =====================================================================

type BoltStore struct {
	db *bolt.DB
}

func NewBoltStore(db *bolt.DB) *BoltStore {
	return &BoltStore{db: db}
}

func (s *BoltStore) InitBuckets() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{"raft_log", "raft_state", "reef_state"} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BoltStore) BucketExists(name string) bool {
	exists := false
	s.db.View(func(tx *bolt.Tx) error {
		exists = tx.Bucket([]byte(name)) != nil
		return nil
	})
	return exists
}

func (s *BoltStore) SaveEntries(entries []raftpb.Entry) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("raft_log"))
		for _, e := range entries {
			data, err := json.Marshal(e)
			if err != nil {
				return err
			}
			key := fmt.Sprintf("entry_%020d", e.Index)
			if err := b.Put([]byte(key), data); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BoltStore) LoadEntries(lo, hi uint64) ([]raftpb.Entry, error) {
	var entries []raftpb.Entry
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("raft_log"))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		start := []byte(fmt.Sprintf("entry_%020d", lo))
		for k, v := c.Seek(start); k != nil; k, v = c.Next() {
			var e raftpb.Entry
			if err := json.Unmarshal(v, &e); err != nil {
				return err
			}
			if e.Index > hi {
				break
			}
			entries = append(entries, e)
		}
		return nil
	})
	return entries, err
}

func (s *BoltStore) TruncateEntries(hi uint64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("raft_log"))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			var idx uint64
			fmt.Sscanf(string(k), "entry_%d", &idx)
			if idx > hi {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *BoltStore) SaveHardState(hs raftpb.HardState) error {
	data, err := json.Marshal(hs)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("raft_state")).Put([]byte("hard_state"), data)
	})
}

func (s *BoltStore) LoadHardState() (raftpb.HardState, error) {
	var hs raftpb.HardState
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("raft_state"))
		if b == nil {
			return nil
		}
		data := b.Get([]byte("hard_state"))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &hs)
	})
	return hs, err
}

func (s *BoltStore) SaveSnapshot(snap Fsmsnapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("reef_state")).Put([]byte("snapshot"), data)
	})
}

func (s *BoltStore) LoadSnapshot() (Fsmsnapshot, error) {
	var snap Fsmsnapshot
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("reef_state"))
		if b == nil {
			return nil
		}
		data := b.Get([]byte("snapshot"))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &snap)
	})
	return snap, err
}

func (s *BoltStore) CompactLog(hi uint64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("raft_log"))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			var idx uint64
			fmt.Sscanf(string(k), "entry_%d", &idx)
			if idx <= hi {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *BoltStore) Close() error {
	return s.db.Close()
}

func (s *BoltStore) DB() *bolt.DB {
	return s.db
}

func (s *BoltStore) ClearSnapshot() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("reef_state"))
		if b == nil {
			return nil
		}
		return b.Delete([]byte("snapshot"))
	})
}

// PessimisticLockAcquire attempts to acquire a distributed lock for a key.
// Stub for future implementation with etcd lease or consensus-based locking.
func (s *BoltStore) PessimisticLockAcquire(key string, ttl time.Duration) (bool, error) {
	return false, fmt.Errorf("not implemented: PessimisticLockAcquire")
}

// PessimisticLockRelease releases a previously held lock.
// Stub for future implementation.
func (s *BoltStore) PessimisticLockRelease(key string) error {
	return fmt.Errorf("not implemented: PessimisticLockRelease")
}

// PessimisticLeaseRenew extends the TTL of an existing lease.
// Stub for future implementation.
func (s *BoltStore) PessimisticLeaseRenew(key string, ttl time.Duration) error {
	return fmt.Errorf("not implemented: PessimisticLeaseRenew")
}

type Fsmsnapshot struct {
	Tasks    map[string]*reef.Task            `json:"tasks"`
	Clients  map[string]*reef.ClientInfo      `json:"clients"`
	Genes    map[string]*evolution.Gene       `json:"genes"`
	Drafts   map[string]*evolution.SkillDraft `json:"drafts"`
}

type ReefFSM struct {
	db       *bolt.DB
	mu       sync.RWMutex

	Tasks   map[string]*reef.Task
	Clients map[string]*reef.ClientInfo
	Genes   map[string]*evolution.Gene
	Drafts  map[string]*evolution.SkillDraft

	appliedIndex  uint64
	snapshotIndex uint64
}

func NewReefFSM(db *bolt.DB) *ReefFSM {
	return &ReefFSM{
		db:       db,
		Tasks:    make(map[string]*reef.Task),
		Clients:  make(map[string]*reef.ClientInfo),
		Genes:    make(map[string]*evolution.Gene),
		Drafts:   make(map[string]*evolution.SkillDraft),
	}
}

func (fsm *ReefFSM) Apply(entry *raftpb.Entry) error {
	if entry.Type != raftpb.EntryNormal {
		return nil
	}

	var cmd RaftCommand
	if err := json.Unmarshal(entry.Data, &cmd); err != nil {
		return fmt.Errorf("deserialize raft command: %w", err)
	}

	fsm.mu.Lock()
	defer fsm.mu.Unlock()
	fsm.appliedIndex = entry.Index

	ts := time.UnixMilli(cmd.Timestamp)

	switch cmd.Type {
	case CmdTaskEnqueue:
		var task reef.Task
		if err := json.Unmarshal(cmd.Payload, &task); err != nil {
			return err
		}
		fsm.Tasks[task.ID] = &task

	case CmdTaskAssign:
		var payload struct {
			TaskID   string `json:"task_id"`
			ClientID string `json:"client_id"`
		}
		json.Unmarshal(cmd.Payload, &payload)
		if task, ok := fsm.Tasks[payload.TaskID]; ok {
			task.AssignedClient = payload.ClientID
			task.Status = reef.TaskRunning
			task.StartedAt = &ts
		}

	case CmdTaskComplete:
		var payload struct {
			TaskID          string           `json:"task_id"`
			Result          *reef.TaskResult `json:"result"`
			ExecutionTimeMs int64            `json:"execution_time_ms"`
		}
		json.Unmarshal(cmd.Payload, &payload)
		if task, ok := fsm.Tasks[payload.TaskID]; ok {
			task.Status = reef.TaskCompleted
			task.Result = payload.Result
			task.CompletedAt = &ts
		}

	case CmdTaskFailed:
		var payload struct {
			TaskID         string              `json:"task_id"`
			Error          *reef.TaskError     `json:"error"`
			AttemptHistory []reef.AttemptRecord `json:"attempt_history"`
		}
		json.Unmarshal(cmd.Payload, &payload)
		if task, ok := fsm.Tasks[payload.TaskID]; ok {
			task.Status = reef.TaskFailed
			task.Error = payload.Error
			task.AttemptHistory = append(task.AttemptHistory, payload.AttemptHistory...)
		}

	case CmdTaskCancel:
		var payload struct {
			TaskID string `json:"task_id"`
		}
		json.Unmarshal(cmd.Payload, &payload)
		if task, ok := fsm.Tasks[payload.TaskID]; ok {
			task.Status = reef.TaskCancelled
			task.CompletedAt = &ts
		}

	case CmdClientRegister:
		var info reef.ClientInfo
		json.Unmarshal(cmd.Payload, &info)
		fsm.Clients[info.ID] = &info

	case CmdClientUnregister:
		var payload struct{ ClientID string `json:"client_id"` }
		json.Unmarshal(cmd.Payload, &payload)
		delete(fsm.Clients, payload.ClientID)

	case CmdClientStale:
		var payload struct{ ClientID string `json:"client_id"` }
		json.Unmarshal(cmd.Payload, &payload)
		if c, ok := fsm.Clients[payload.ClientID]; ok {
			c.State = reef.ClientStale
		}

	case CmdGeneApprove:
		var gene evolution.Gene
		json.Unmarshal(cmd.Payload, &gene)
		gene.Status = evolution.GeneStatusApproved
		gene.ApprovedAt = timePtr(time.UnixMilli(cmd.Timestamp))
		fsm.Genes[gene.ID] = &gene

	case CmdGeneReject:
		var gene evolution.Gene
		json.Unmarshal(cmd.Payload, &gene)
		gene.Status = evolution.GeneStatusRejected
		fsm.Genes[gene.ID] = &gene

	case CmdClaimAssign:
		var payload struct {
			TaskID   string `json:"task_id"`
			ClientID string `json:"client_id"`
		}
		json.Unmarshal(cmd.Payload, &payload)
		if task, ok := fsm.Tasks[payload.TaskID]; ok {
			task.AssignedClient = payload.ClientID
			task.Status = reef.TaskRunning
		}

	case CmdTaskStart:
		var payload struct{ TaskID string `json:"task_id"` }
		json.Unmarshal(cmd.Payload, &payload)
		if task, ok := fsm.Tasks[payload.TaskID]; ok {
			task.Status = reef.TaskRunning
			task.StartedAt = &ts
		}
	}
	return nil
}

func (fsm *ReefFSM) Snapshot() ([]byte, error) {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()

	snap := Fsmsnapshot{
		Tasks:   fsm.Tasks,
		Clients: fsm.Clients,
		Genes:   fsm.Genes,
		Drafts:  fsm.Drafts,
	}
	data, err := json.Marshal(&snap)
	fsm.snapshotIndex = fsm.appliedIndex
	return data, err
}

func (fsm *ReefFSM) Restore(snapshot []byte) error {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	var snap Fsmsnapshot
	if err := json.Unmarshal(snapshot, &snap); err != nil {
		return err
	}
	fsm.Tasks = snap.Tasks
	fsm.Clients = snap.Clients
	fsm.Genes = snap.Genes
	fsm.Drafts = snap.Drafts
	return nil
}

func (fsm *ReefFSM) Equal(other *ReefFSM) bool {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	a, _ := json.Marshal(Fsmsnapshot{Tasks: fsm.Tasks, Clients: fsm.Clients, Genes: fsm.Genes, Drafts: fsm.Drafts})
	b, _ := json.Marshal(Fsmsnapshot{Tasks: other.Tasks, Clients: other.Clients, Genes: other.Genes, Drafts: other.Drafts})
	return string(a) == string(b)
}

// =====================================================================
// ClientConnPool (P7-08)
// =====================================================================

type PoolConfig struct {
	ServerAddrs      []string      `json:"server_addrs"`
	ReconnectBackoff time.Duration `json:"reconnect_backoff"`
	MaxReconnect     time.Duration `json:"max_reconnect"`
	PingInterval     time.Duration `json:"ping_interval"`
}

type serverConn struct {
	Addr     string
	IsLeader bool
}

type ClientConnPool struct {
	Servers   []*serverConn
	LeaderIdx int
	mu        sync.RWMutex
	config    PoolConfig
}

func NewClientConnPool(cfg PoolConfig) *ClientConnPool {
	servers := make([]*serverConn, len(cfg.ServerAddrs))
	for i, addr := range cfg.ServerAddrs {
		servers[i] = &serverConn{Addr: addr}
	}
	return &ClientConnPool{
		Servers:   servers,
		LeaderIdx: -1,
		config:    cfg,
	}
}

func (p *ClientConnPool) OnLeaderChange(leaderAddr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, s := range p.Servers {
		if s.Addr == leaderAddr {
			s.IsLeader = true
			p.LeaderIdx = i
		} else {
			s.IsLeader = false
		}
	}
}

func (p *ClientConnPool) SendToLeader(msg reef.Message) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.LeaderIdx < 0 {
		return fmt.Errorf("no leader")
	}
	return nil
}

// =====================================================================
// LeaderedServer (P7-07)
// =====================================================================

type LeaderedServer struct {
	isLeader atomic.Bool
	nodeID   string
	raftNode raft.Node
	fsm      *ReefFSM
}

func (s *LeaderedServer) Propose(cmd RaftCommand) error {
	if !s.isLeader.Load() {
		return ErrNotLeader
	}
	return nil
}

func (s *LeaderedServer) onBecomeLeader() {}
func (s *LeaderedServer) onLoseLeadership() {}

// =====================================================================
// Helpers
// =====================================================================

func timePtr(t time.Time) *time.Time { return &t }
