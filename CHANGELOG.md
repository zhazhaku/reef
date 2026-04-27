# Changelog

All notable changes to the Reef distributed multi-agent swarm system are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **Reef v1.0.0** — Distributed multi-agent swarm orchestration system
  - WebSocket-based hub-and-spoke topology for Server-Client communication
  - Role-based task routing (`coder`, `analyst`, `tester`)
  - Skill-based client matching with load balancing
  - Task lifecycle management: dispatch, progress, completion, cancellation, pause/resume
  - Automatic failure retry with escalation policy (max 2 retries by default)
  - Client heartbeat and stale detection
  - Connection resilience: buffered control messages, reconnection support
  - HTTP Admin API: `/admin/status`, `/admin/tasks`, `POST /tasks`
  - YAML-based custom role configuration in `skills/roles/`
  - CLI command: `picoclaw reef-server`
  - Comprehensive E2E integration test suite (17 scenarios)
  - Full documentation: architecture, deployment, API reference, protocol spec

### Fixed

- WebSocket handshake now calls `scheduler.HandleClientAvailable()` after client registration, ensuring queued tasks are dispatched to newly connected clients.

## [0.x.x] — Prior to Reef

See git history for changes before the Reef distributed swarm feature.
