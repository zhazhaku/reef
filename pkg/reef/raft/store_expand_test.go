package raft

import (
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	raftpb "go.etcd.io/raft/v3/raftpb"
)

func openBolt(t *testing.T) (*bolt.DB, *BoltStore) {
	t.Helper()
	db, err := bolt.Open(t.TempDir()+"/test.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	store := NewBoltStore(db)
	if err := store.InitBuckets(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, store
}

func TestStore_BucketsExist(t *testing.T) {
	_, store := openBolt(t)
	for _, name := range []string{"raft_log", "raft_state", "reef_state"} {
		if !store.BucketExists(name) {
			t.Errorf("bucket %q missing", name)
		}
	}
	if store.BucketExists("ghost_bucket") {
		t.Error("ghost_bucket should not exist")
	}
}

func TestStore_HardState(t *testing.T) {
	_, store := openBolt(t)

	hs, err := store.LoadHardState()
	if err != nil {
		t.Fatal(err)
	}
	if hs.Term != 0 || hs.Vote != 0 || hs.Commit != 0 {
		t.Errorf("expected zero, got %+v", hs)
	}

	if err := store.SaveHardState(raftpb.HardState{Term: 7, Vote: 3, Commit: 99}); err != nil {
		t.Fatal(err)
	}
	hs2, err := store.LoadHardState()
	if err != nil {
		t.Fatal(err)
	}
	if hs2.Term != 7 || hs2.Vote != 3 || hs2.Commit != 99 {
		t.Errorf("roundtrip: %+v", hs2)
	}
}

func TestStore_ConfState(t *testing.T) {
	_, store := openBolt(t)
	cs := raftpb.ConfState{Voters: []uint64{1, 2, 3}}
	if err := store.SaveConfState(cs); err != nil {
		t.Fatal(err)
	}
	cs2, err := store.LoadConfState()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs2.Voters) != 3 {
		t.Errorf("expected 3 voters, got %d", len(cs2.Voters))
	}
}

func TestStore_LoadConfState_Empty(t *testing.T) {
	_, store := openBolt(t)
	cs, err := store.LoadConfState()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Voters) > 0 {
		t.Errorf("expected empty, got %+v", cs)
	}
}

func TestStore_LoadEntries_Empty(t *testing.T) {
	_, store := openBolt(t)
	entries, err := store.LoadEntries(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0, got %d", len(entries))
	}
}

func TestStore_SaveAndLoadEntries(t *testing.T) {
	_, store := openBolt(t)

	entries := []raftpb.Entry{
		{Term: 1, Index: 1, Type: raftpb.EntryNormal, Data: []byte("a")},
		{Term: 1, Index: 2, Type: raftpb.EntryNormal, Data: []byte("b")},
		{Term: 2, Index: 3, Type: raftpb.EntryConfChange, Data: []byte("c")},
	}
	if err := store.SaveEntries(entries); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadEntries(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3, got %d", len(loaded))
	}
	if loaded[0].Index != 1 || loaded[2].Index != 3 {
		t.Error("index mismatch")
	}
}

func TestStore_TruncateEntries(t *testing.T) {
	_, store := openBolt(t)
	store.SaveEntries([]raftpb.Entry{
		{Index: 1, Term: 1}, {Index: 2, Term: 1}, {Index: 3, Term: 2},
	})
	if err := store.TruncateEntries(2); err != nil {
		t.Fatal(err)
	}
	entries, _ := store.LoadEntries(1, 5)
	if len(entries) != 2 {
		t.Errorf("expected 2 after truncate, got %d", len(entries))
	}
}

func TestStore_CompactLog(t *testing.T) {
	_, store := openBolt(t)
	store.SaveEntries([]raftpb.Entry{
		{Index: 1, Term: 1}, {Index: 2, Term: 1}, {Index: 3, Term: 2},
	})
	if err := store.CompactLog(2); err != nil {
		t.Fatal(err)
	}
}

func TestStore_LoadSnapshot_Empty(t *testing.T) {
	_, store := openBolt(t)
	snap, err := store.LoadSnapshot()
	if err != nil {
		t.Logf("LoadSnapshot on empty returns: err=%v snap=%+v", err, snap)
	}
}

func TestStore_DB_Accessor(t *testing.T) {
	db, store := openBolt(t)
	if store.DB() != db {
		t.Error("DB() mismatch")
	}
}

func TestStore_Close(t *testing.T) {
	_, store := openBolt(t)
	store.Close()
}

func TestStore_PessimisticLock(t *testing.T) {
	_, store := openBolt(t)
	_, err := store.PessimisticLockAcquire("key", time.Hour)
	if err == nil {
		t.Error("expected not-implemented")
	}
	if err := store.PessimisticLockRelease("key"); err == nil {
		t.Error("expected not-implemented")
	}
	if err := store.PessimisticLeaseRenew("key", time.Hour); err == nil {
		t.Error("expected not-implemented")
	}
}
