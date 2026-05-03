# Agent Developer Guide

## Writing a Reef Agent

An agent is a PicoClaw runtime that connects to Reef Server via WebSocket and executes tasks.

### Minimal Agent

```go
package main

import (
    client "github.com/sipeed/reef/pkg/reef/client"
)

func main() {
    conn := client.NewConnector(client.ConnectorOptions{
        ServerURL: "ws://reef-server:8765",
    })
    
    runner := client.NewTaskRunner(client.TaskRunnerOptions{
        Connector:      conn,
        Exec:           myExecFunc,
        SandboxDir:     "/var/reef/sandboxes",
        SandboxFactory: mySandboxFactory,
    })
    
    conn.Connect(ctx)
}

func myExecFunc(ctx context.Context, instruction string) (string, error) {
    // Your agent logic here
    return result, nil
}
```

### With Cognitive Sandbox (P8)

```go
import (
    "github.com/zhazhaku/reef/pkg/agent"
    client "github.com/sipeed/reef/pkg/reef/client"
)

runner := client.NewTaskRunner(client.TaskRunnerOptions{
    Connector:      conn,
    Exec:           myExecFunc,
    SandboxDir:     "/var/reef/sandboxes",
    SandboxFactory: agent.ReefSandboxFactory,  // P8 sandbox
    MaxRounds:      50,
    MemoryRecorder: agent.NewReefMemoryRecorder(store),
})
```

### Hooks

Use the Hook system to intercept lifecycle events:

```go
// On task start
hook := &agent.ReefTaskHook{}
hook.OnTurnStart = func(ctx context.Context, tc *reef.TaskContext) {
    tc.InjectGene("quality: high")
}
```

## Role Manifest

Define your agent's capabilities in YAML:

```yaml
name: coder
skills:
  - go
  - bash
  - docker
system_prompt_override: |
  You are a senior Go engineer. Always write tests.
capacity: 3
```

See `pkg/reef/role/role.go` for the full schema.
