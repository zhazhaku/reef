package agent

import (
	"time"

	client "github.com/zhazhaku/reef/pkg/reef/client"
	"github.com/zhazhaku/reef/pkg/memory"
)

// ReefMemoryRecorder implements client.MemoryRecorder using the P8 episodic memory store.
type ReefMemoryRecorder struct {
	store *memory.EpisodicStore
}

// NewReefMemoryRecorder creates a memory recorder backed by the SQLite episodic store.
func NewReefMemoryRecorder(store *memory.EpisodicStore) *ReefMemoryRecorder {
	return &ReefMemoryRecorder{store: store}
}

var _ client.MemoryRecorder = (*ReefMemoryRecorder)(nil)

func (r *ReefMemoryRecorder) RecordComplete(taskID, instruction string, result string, roundsExecuted int, duration time.Duration, corruptions int) {
	r.store.Save(&memory.EpisodicEntry{
		TaskID:    taskID,
		EventType: "task_completed",
		Summary:   trunc(result, 200),
		Tags:      []string{"success"},
		Timestamp: time.Now().Unix(),
	})
}

func (r *ReefMemoryRecorder) RecordFailed(taskID, instruction string, errMsg string, roundsExecuted int, attempts int, corruptions int) {
	r.store.Save(&memory.EpisodicEntry{
		TaskID:    taskID,
		EventType: "task_failed",
		Summary:   trunc(errMsg, 200),
		Tags:      []string{"failure"},
		Timestamp: time.Now().Unix(),
	})
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
