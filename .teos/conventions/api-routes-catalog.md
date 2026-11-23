# API Routes Catalog

Canonical route registry (AGENTS.md Rule 9). v1 surface — each route's
owning section in parens. All routes are authenticated unless marked;
all privileged ops pass the broker.

## Auth (§09)
- `POST /api/auth/login` — username/password → session cookie
- `POST /api/auth/logout`
- `GET  /api/auth/me`

## Sessions (§03, §02)
- `GET    /api/sessions` — live session list + states
- `POST   /api/sessions` — spawn (broker: controlled)
- `GET    /api/sessions/{id}` — detail + state + scrollback tail
- `DELETE /api/sessions/{id}` — terminate (broker: controlled)
- `GET    /api/sessions/{id}/scrollback` — paged scrollback
- `WS     /ws/session/{id}` — attach: PTY I/O + state events

## Agent registry (§05)
- `GET /api/agents` — agent sessions + states (needs-you ordering)
- `GET /api/agents/{id}/prompt` — current detected prompt + options
- `POST /api/agents/launch` — launch profile execution (broker)

## Command Intelligence (§04)
- `GET  /api/commands` — registry query (palette search)
- `GET  /api/commands/{id}` — manifest detail
- `POST /api/commands/{id}/execute` — broker-gated execution
- `GET  /api/commands/context/{session_id}` — context-relevant actions
- `GET  /api/discovered` — auto-discovered commands on target

## Files (§07)
- `GET    /api/files/{provider}/list?path=` — listing
- `GET    /api/files/{provider}/stat`
- `POST   /api/files/{provider}/mkdir|rename|delete` — broker-gated
- `POST   /api/files/{provider}/upload` — chunked
- `GET    /api/files/{provider}/download`
- `GET    /api/files/bookmarks` · `POST /api/files/bookmarks`
- *(TCP, not HTTP)* `sftp://:{TU_SFTP_PORT}` — SFTP protocol endpoint
  over the same providers (§07, ADR-006); `TU_SFTP_ENABLED`-gated

## Infrastructure (§08)
- `GET  /api/ssh/profiles` · `POST /api/ssh/profiles` — CRUD (secret store)
- `POST /api/ssh/profiles/{id}/connect` — test/target bind
- `GET  /api/health/host` — CPU/RAM/disk/load/processes
- `GET  /api/docker/containers` · `GET /api/docker/images`
- `GET  /api/docker/containers/{id}/logs`
- `POST /api/docker/containers/{id}/{start|stop|restart}` — broker: confirmed
- `WS   /ws/docker/exec/{id}` — exec shell (broker; a session into container)

## Users/Roles (§09, Phase 4)
- `GET/POST/PATCH/DELETE /api/users` — admin only
- `GET /api/audit` — audit log (admin)

## System
- `GET /api/system/version` — public
- `GET /api/system/health` — public (probe-friendly for gate model)

Rules: catalog entry before implementation; broker-permission annotation
mandatory on every mutating route; version prefix reserved (`/api/v1`
if a breaking surface ever ships).
