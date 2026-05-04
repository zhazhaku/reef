package server

import (
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

// Queue is the interface for task queues (both in-memory and persistent).
type Queue interface {
	Enqueue(task *reef.Task) error
	Dequeue() *reef.Task
	Peek() *reef.Task
	Len() int
	Snapshot() []*reef.Task
	Expire(now time.Time) []*reef.Task
}
