package store

import (
	"fmt"
	"sync"

	"github.com/zhazhaku/reef/pkg/reef"
)

// MemoryStore is a thread-safe in-memory implementation of TaskStore.
type MemoryStore struct {
	mu        sync.RWMutex
	tasks     map[string]*reef.Task
	attempts  map[string][]reef.AttemptRecord
	relations map[string][]string // parentID → []childID
	parentOf  map[string]string   // childID → parentID
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tasks:     make(map[string]*reef.Task),
		attempts:  make(map[string][]reef.AttemptRecord),
		relations: make(map[string][]string),
		parentOf:  make(map[string]string),
	}
}

// SaveTask stores a new task. Returns an error if the task already exists.
func (m *MemoryStore) SaveTask(task *reef.Task) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tasks[task.ID]; exists {
		return fmt.Errorf("task %s already exists", task.ID)
	}
	// Store a shallow copy to avoid external mutation.
	cp := *task
	m.tasks[task.ID] = &cp
	return nil
}

// GetTask retrieves a task by ID.
func (m *MemoryStore) GetTask(id string) (*reef.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %s not found", id)
	}
	cp := *t
	return &cp, nil
}

// UpdateTask replaces an existing task. Returns an error if the task doesn't exist.
func (m *MemoryStore) UpdateTask(task *reef.Task) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tasks[task.ID]; !exists {
		return fmt.Errorf("task %s not found", task.ID)
	}
	cp := *task
	m.tasks[task.ID] = &cp
	return nil
}

// DeleteTask removes a task and its attempts by ID.
func (m *MemoryStore) DeleteTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tasks[id]; !exists {
		return fmt.Errorf("task %s not found", id)
	}
	delete(m.tasks, id)
	delete(m.attempts, id)
	return nil
}

// ListTasks returns tasks matching the given filter criteria.
func (m *MemoryStore) ListTasks(filter TaskFilter) ([]*reef.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Build status set for fast lookup.
	statusSet := make(map[reef.TaskStatus]bool, len(filter.Statuses))
	for _, s := range filter.Statuses {
		statusSet[s] = true
	}
	roleSet := make(map[string]bool, len(filter.Roles))
	for _, r := range filter.Roles {
		roleSet[r] = true
	}

	var matched []*reef.Task
	for _, t := range m.tasks {
		if len(statusSet) > 0 && !statusSet[t.Status] {
			continue
		}
		if len(roleSet) > 0 && !roleSet[t.RequiredRole] {
			continue
		}
		cp := *t
		matched = append(matched, &cp)
	}

	// Apply offset.
	if filter.Offset > 0 && filter.Offset < len(matched) {
		matched = matched[filter.Offset:]
	} else if filter.Offset >= len(matched) {
		return []*reef.Task{}, nil
	}

	// Apply limit.
	if filter.Limit > 0 && filter.Limit < len(matched) {
		matched = matched[:filter.Limit]
	}

	return matched, nil
}

// SaveAttempt appends an attempt record for a task.
func (m *MemoryStore) SaveAttempt(taskID string, attempt reef.AttemptRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tasks[taskID]; !exists {
		return fmt.Errorf("task %s not found", taskID)
	}
	m.attempts[taskID] = append(m.attempts[taskID], attempt)
	return nil
}

// GetAttempts returns all attempt records for a task.
func (m *MemoryStore) GetAttempts(taskID string) ([]reef.AttemptRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, exists := m.tasks[taskID]; !exists {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	attempts := m.attempts[taskID]
	if attempts == nil {
		return []reef.AttemptRecord{}, nil
	}
	out := make([]reef.AttemptRecord, len(attempts))
	copy(out, attempts)
	return out, nil
}

// Close clears the store. After Close, the store should not be used.
func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = make(map[string]*reef.Task)
	m.attempts = make(map[string][]reef.AttemptRecord)
	m.relations = make(map[string][]string)
	m.parentOf = make(map[string]string)
	return nil
}

// SaveRelation records a parent-child relationship.
func (m *MemoryStore) SaveRelation(parentID, childID, dependency string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.relations[parentID] = append(m.relations[parentID], childID)
	m.parentOf[childID] = parentID
	return nil
}

// GetSubTaskIDs returns all child task IDs for a parent.
func (m *MemoryStore) GetSubTaskIDs(parentID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.relations[parentID], nil
}

// GetParentTaskID returns the parent task ID for a child.
func (m *MemoryStore) GetParentTaskID(childID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.parentOf[childID], nil
}
