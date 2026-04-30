# Tasks: Reef E2E Integration Tests

## Phase 1: Test Infrastructure

- [x] Create `test/e2e/` directory
- [x] Create OpenSpec change directory and metadata
- [x] Implement `E2EServer` helper (spin up Server on ephemeral port)
- [x] Implement `MockClient` (WebSocket client with message channels)
- [x] Implement test utilities (waitFor, retry, port allocation)
- [x] Add `go test` build tag and verify compilation

## Phase 2: Core Flow Tests

- [x] Test: Server startup and single client registration
- [x] Test: Multiple clients with different roles register
- [x] Test: Client heartbeat keeps connection alive
- [x] Test: Task dispatched to matching role client
- [x] Test: Task queued when no matching client
- [x] Test: Task dispatched when matching client later connects
- [x] Test: Task completion reported correctly
- [x] Test: Admin status reflects accurate system state
- [x] Test: Admin tasks filtered by role
- [x] Test: Task submission via Admin API

## Phase 3: Advanced Flow Tests

- [x] Test: Task routed to lowest-load client
- [x] Test: Failed task reassigned to another client
- [x] Test: Task escalated after max retries
- [x] Test: Client local retry before reporting failure
- [x] Test: Server cancels a running task
- [x] Test: Server pauses and resumes a task
- [x] Test: Client reconnects after disconnection
- [x] Test: Client exponential backoff reconnection

## Phase 4: Integration & Verification

- [x] Run all E2E tests: `go test ./test/e2e/... -v`
- [x] Verify test runtime < 30 seconds (actual: ~8.3s)
- [x] Verify no flaky tests (run 5 times)
- [x] Verify coverage of all Phase 5 ROADMAP success criteria
- [x] Commit with descriptive message
- [x] Update `.planning/STATE.md`

## Bug Fixes Discovered During E2E

- [x] Fix: WebSocket handshake missing `HandleClientAvailable` call — new clients
  registering did not trigger task dispatch for queued tasks.
  Fixed in `pkg/reef/server/websocket.go`.
