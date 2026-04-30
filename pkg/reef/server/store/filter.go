package store

import "github.com/zhazhaku/reef/pkg/reef"

// TaskFilter defines filtering criteria for listing tasks.
type TaskFilter struct {
	Statuses []reef.TaskStatus
	Roles    []string
	Limit    int
	Offset   int
}
