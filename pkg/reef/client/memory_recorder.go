package client

import "time"

// MemoryRecorder is an optional hook for recording task execution memories.
// Implementations can persist episodic memories (success/failure patterns,
// tool loops, etc.) for future retrieval. Set in TaskRunnerOptions.
type MemoryRecorder interface {
	// RecordComplete is called when a task finishes successfully.
	RecordComplete(taskID, instruction string, result string, roundsExecuted int, duration time.Duration, corruptions int)

	// RecordFailed is called when a task fails after all retries.
	RecordFailed(taskID, instruction string, errMsg string, roundsExecuted int, attempts int, corruptions int)
}

// nopMemoryRecorder is a no-op default.
type nopMemoryRecorder struct{}

func (nopMemoryRecorder) RecordComplete(string, string, string, int, time.Duration, int) {}
func (nopMemoryRecorder) RecordFailed(string, string, string, int, int, int)              {}
