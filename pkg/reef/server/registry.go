package server

import (
	"sync"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

// Registry maintains a thread-safe map of connected Clients.
type Registry struct {
	mu       sync.RWMutex
	clients  map[string]*reef.ClientInfo
	onStale  func(clientID string)
}

// NewRegistry creates a new empty registry.
func NewRegistry(onStale func(clientID string)) *Registry {
	return &Registry{
		clients: make(map[string]*reef.ClientInfo),
		onStale: onStale,
	}
}

// Register adds or updates a client in the registry.
func (r *Registry) Register(info *reef.ClientInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[info.ID] = info
}

// Unregister removes a client from the registry.
func (r *Registry) Unregister(clientID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, clientID)
}

// Get returns a client by ID, or nil if not found.
func (r *Registry) Get(clientID string) *reef.ClientInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[clientID]
}

// UpdateHeartbeat updates the last heartbeat time for a client.
func (r *Registry) UpdateHeartbeat(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[clientID]
	if !ok {
		return false
	}
	c.LastHeartbeat = time.Now()
	c.State = reef.ClientConnected
	return true
}

// MarkStale marks a client as stale and invokes the onStale callback.
func (r *Registry) MarkStale(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[clientID]
	if !ok || c.State == reef.ClientStale {
		return false
	}
	c.State = reef.ClientStale
	if r.onStale != nil {
		r.onStale(clientID)
	}
	return true
}

// MarkDisconnected marks a client as disconnected.
func (r *Registry) MarkDisconnected(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[clientID]
	if !ok {
		return false
	}
	c.State = reef.ClientDisconnected
	return true
}

// List returns a snapshot of all registered clients.
func (r *Registry) List() []*reef.ClientInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*reef.ClientInfo, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, c)
	}
	return out
}

// ListByRole returns clients matching a specific role.
func (r *Registry) ListByRole(role string) []*reef.ClientInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*reef.ClientInfo
	for _, c := range r.clients {
		if c.Role == role {
			out = append(out, c)
		}
	}
	return out
}

// ScanStale iterates all clients and marks those whose heartbeat has
// exceeded the given timeout as stale. Returns the list of newly stale IDs.
func (r *Registry) ScanStale(timeout time.Duration) []string {
	r.mu.RLock()
	now := time.Now()
	var staleIDs []string
	for id, c := range r.clients {
		if c.State != reef.ClientConnected {
			continue
		}
		if now.Sub(c.LastHeartbeat) > timeout {
			staleIDs = append(staleIDs, id)
		}
	}
	r.mu.RUnlock()

	// Mark outside the read lock to avoid deadlock if onStale triggers registry ops.
	for _, id := range staleIDs {
		r.MarkStale(id)
	}
	return staleIDs
}

// IncrementLoad atomically increments a client's current load.
func (r *Registry) IncrementLoad(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[clientID]
	if !ok {
		return false
	}
	c.CurrentLoad++
	return true
}

// DecrementLoad atomically decrements a client's current load.
func (r *Registry) DecrementLoad(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[clientID]
	if !ok {
		return false
	}
	if c.CurrentLoad > 0 {
		c.CurrentLoad--
	}
	return true
}
