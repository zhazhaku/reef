package client

// Sandbox is the interface a cognitive sandbox must implement.
// It provides isolated context and lifecycle for a single task.
type Sandbox interface {
	// TaskID returns the unique task identifier.
	TaskID() string

	// AppendRound records a tool execution round.
	AppendRound(call, output, thought string)

	// RecordProgress logs an intermediate progress update.
	RecordProgress(round int, message string)

	// Destroy cleans up the sandbox resources.
	Destroy() error
}

// SandboxFactory creates a new Sandbox for a given task.
type SandboxFactory func(taskID, baseDir string) (Sandbox, error)
