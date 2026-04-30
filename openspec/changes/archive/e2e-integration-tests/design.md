# Design: Reef E2E Integration Tests

## Architecture

```
test/e2e/
├── reef_e2e_test.go      # Main test file with all scenarios
├── mock_client.go        # Mock WebSocket client for E2E
├── mock_agent.go         # Mock AgentLoop execution
└── helpers.go            # Shared test utilities
```

## Test Infrastructure

### E2EServer Helper
- Spins up `server.Server` on `127.0.0.1:0` (ephemeral port)
- Provides `WSURL()` and `AdminURL()` for clients
- Cleans up with `Shutdown()`

### MockClient
- Connects via `gorilla/websocket` to real Server
- Sends protocol messages (register, heartbeat, progress, completed, failed)
- Receives messages in a channel for test assertions
- Supports manual close/reconnect for resilience tests

### MockAgentLoop
- Implements `agent.EventObserver` interface
- Simulates task execution with configurable duration/result/error
- Emits `TurnStart`, `ToolExecEnd`, `TurnEnd` events
- Integrates with `TaskContext` for cancel/pause/resume

## Test Execution Strategy

Each test follows this pattern:
1. `setup()` → create E2EServer
2. `connectClient()` → mock client registers
3. `submitTask()` → via Admin API or direct scheduler call
4. `expectMessage()` → assert on WebSocket messages
5. `assertState()` → query Admin API for server-side state
6. `teardown()` → shutdown server, close clients

## Synchronization

Tests use channel-based synchronization rather than `time.Sleep`:
- `msgReceived := make(chan reef.Message, 10)`
- `taskDone := make(chan string)`
- Timeouts on all blocking operations (default 5s)

## Determinism

- Ephemeral ports eliminate port conflicts
- Fixed task IDs for traceability
- Mock time for heartbeat tests (if needed)
- Sequential test execution (no parallel Server reuse)

## Dependencies

All dependencies are already in `go.mod`:
- `github.com/gorilla/websocket` — real WebSocket transport
- `github.com/stretchr/testify` — assertions (already used in unit tests)
- Standard `testing` package

## No External Services

- No real LLM calls — mock AgentLoop
- No real channels (Telegram/Discord) — swarm channel only
- No database — in-memory queue/registry
