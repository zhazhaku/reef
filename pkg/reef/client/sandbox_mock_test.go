package client

import "sync"

// mockSandbox implements Sandbox for testing.
type mockSandbox struct {
	mu     sync.Mutex
	id     string
	rounds []sandboxRound
}

type sandboxRound struct {
	Call   string
	Output string
	Thought string
}

func newMockSandbox(taskID string) *mockSandbox {
	return &mockSandbox{id: taskID}
}

func (m *mockSandbox) TaskID() string { return m.id }

func (m *mockSandbox) AppendRound(call, output, thought string) {
	m.mu.Lock()
	m.rounds = append(m.rounds, sandboxRound{Call: call, Output: output, Thought: thought})
	m.mu.Unlock()
}

func (m *mockSandbox) RecordProgress(round int, message string) {}

func (m *mockSandbox) Destroy() error { return nil }

func mockSandboxFactory(taskID, baseDir string) (Sandbox, error) {
	return newMockSandbox(taskID), nil
}
