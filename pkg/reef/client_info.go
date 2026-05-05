package reef

import "time"

// ClientState represents the connection state of a Client from Server's perspective.
type ClientState string

const (
	ClientConnected    ClientState = "connected"
	ClientDisconnected ClientState = "disconnected"
	ClientStale        ClientState = "stale"
)

// ClientInfo holds the capabilities and runtime state of a connected Client.
type ClientInfo struct {
	ID            string      `json:"client_id"`
	Role          string      `json:"role"`
	Skills        []string    `json:"skills"`
	Providers     []string    `json:"providers,omitempty"`
	Capacity      int         `json:"capacity"`
	CurrentLoad   int         `json:"current_load"`
	LastHeartbeat time.Time   `json:"last_heartbeat"`
	State         ClientState `json:"state"`
}

// IsAvailable returns true if the client is connected and has spare capacity.
func (c *ClientInfo) IsAvailable() bool {
	return c.State == ClientConnected && c.CurrentLoad < c.Capacity
}

// Matches checks whether the client satisfies the role and skill requirements.
func (c *ClientInfo) Matches(requiredRole string, requiredSkills []string) bool {
	if c.Role != requiredRole {
		return false
	}
	if len(requiredSkills) == 0 {
		return true
	}
	skillSet := make(map[string]struct{}, len(c.Skills))
	for _, s := range c.Skills {
		skillSet[s] = struct{}{}
	}
	for _, req := range requiredSkills {
		if _, ok := skillSet[req]; !ok {
			return false
		}
	}
	return true
}

// RemainingCapacity returns how many additional tasks the client can accept.
func (c *ClientInfo) RemainingCapacity() int {
	rem := c.Capacity - c.CurrentLoad
	if rem < 0 {
		return 0
	}
	return rem
}

// SystemStatus describes the overall system state for API queries.
type SystemStatus struct {
	ConnectedClients  int              `json:"connected_clients"`
	DisconnectedCount int              `json:"disconnected_count"`
	QueuedTasks       int              `json:"queued_tasks"`
	RunningTasks      int              `json:"running_tasks"`
	CompletedTasks    int              `json:"completed_tasks"`
	FailedTasks       int              `json:"failed_tasks"`
	Clients           []ClientSnapshot `json:"clients"`
}

// ClientSnapshot is a read-only view of a connected client's state.
type ClientSnapshot struct {
	ClientID    string `json:"client_id"`
	Role        string `json:"role"`
	Skills      []string `json:"skills"`
	State       string `json:"state"`
	CurrentLoad int    `json:"current_load"`
	Capacity    int    `json:"capacity"`
}
