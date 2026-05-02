// Package reef provides domain types for the Reef distributed task execution system.
package reef

import "time"

// DAGNode represents a node in the distributed execution graph (DAG).
// Used by the Raft FSM to track pipeline stage outputs.
type DAGNode struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parent_id,omitempty"`
	Status    string    `json:"status"`
	Output    string    `json:"output,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}
