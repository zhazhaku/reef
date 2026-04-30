# Proposal: Reef E2E Integration Tests

## Intent
Reef v1 implements a distributed multi-agent orchestration system with 5 phases of
functionality (protocol, server, client, lifecycle, roles). While each package has
unit tests, there is **no end-to-end validation** of the full Server → Client →
AgentLoop → Result flow over real WebSocket connections.

This change adds comprehensive E2E integration tests that spin up a real Reef Server,
connect mock Clients over actual WebSocket, exercise the full task lifecycle, and
verify correctness through both message exchange and Admin HTTP API.

## Scope

**In Scope:**
- Real WebSocket E2E tests (no mocks for transport)
- Task dispatch → execution → completion full cycle
- Multi-role routing (coder vs analyst vs tester)
- Task failure with retry and escalation
- Cancel / pause / resume lifecycle
- Client disconnection → pause → reconnection → resume
- Admin API `/admin/status` and `/admin/tasks` validation
- Role-based skill loading verification

**Out of Scope:**
- Performance / load tests (beyond basic multi-client)
- Real LLM provider calls (mock AgentLoop execution)
- Web UI dashboard testing
- Multi-Server federation

## Approach
Use Go's `testing` package with a test helper that:
1. Starts a Reef Server on ephemeral ports
2. Connects mock Clients via gorilla/websocket
3. Exchanges real protocol messages
4. Uses `agent.EventObserver` mock for AgentLoop integration
5. Asserts on message sequences, state transitions, and Admin API responses

## Success Criteria
- All E2E tests pass with `go test ./test/e2e/... -v`
- Tests cover all Phase 5 success criteria from ROADMAP
- Test runtime < 30 seconds total
- No flakes (deterministic with timeouts)
