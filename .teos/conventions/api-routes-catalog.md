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
- `POST   /api/sessions` — spawn `{target, cwd, shell}`; target =
  `local` | `ssh:<profile_id>` | `docker:<container_id>` (broker: controlled)
- `GET    /api/sessions/{id}` — detail + state + scrollback tail
- `PATCH  /api/sessions/{id}` — rename/annotate
- `DELETE /api/sessions/{id}` — terminate (broker: controlled)
- `GET    /api/sessions/{id}/scrollback` — paged scrollback
- `WS     /ws/session/{id}` — attach: PTY I/O + state events

## Workspaces (§03)
- `GET /api/workspaces` — saved workspaces
- `PUT /api/workspaces/{id}` — save layout (tabs/splits → session ids)

## Agent registry (§05)
- `GET    /api/agents` — agent sessions + states (needs-you ordering)
- `GET    /api/agents/{id}/prompt` — current detected prompt + options
- `GET/POST/PATCH/DELETE /api/agents/profiles` — launch-profile CRUD
- `POST   /api/agents/launch` — launch profile execution (broker)
- `GET    /api/alerts` — alert list (unack first)
- `POST   /api/alerts/{id}/ack` — acknowledge

## Adapters (§06)
- `GET /api/adapters` — tool → pin status (`verified|unverified`) for badges
- `GET /api/adapters/{tool}` — pin detail + fixture/replay status

## Command Intelligence (§04)
- `GET  /api/commands?q=` — registry query (palette search)
- `GET  /api/commands/{id}` — manifest detail
- `POST /api/commands/{id}/execute` — broker-gated execution
- `GET  /api/commands/context/{session_id}` — context-relevant actions
- `GET  /api/discovered?target=` — auto-discovered commands on target

## Files (§07)
`{provider}` = `local` | `s3` | `sftp:<profile_id>` — same namespacing as
the SFTP endpoint's virtual root (§07).
- `GET    /api/files/{provider}/list?path=` — listing
- `GET    /api/files/{provider}/stat?path=`
- `POST   /api/files/{provider}/mkdir|rename|delete` — broker-gated
- `POST   /api/files/{provider}/upload` — chunked
- `GET    /api/files/{provider}/download`
- `GET    /api/files/transfers` — transfer queue + resume cursors
- `GET    /api/files/bookmarks` · `POST /api/files/bookmarks` · `DELETE /api/files/bookmarks/{id}`
- *(TCP, not HTTP)* `sftp://:{TU_SFTP_PORT}` — SFTP protocol endpoint
  over the same providers (§07, ADR-006); `TU_SFTP_ENABLED`-gated

## Infrastructure (§08)
- `GET  /api/ssh/profiles` · `POST /api/ssh/profiles` — CRUD (secret store)
- `POST /api/ssh/profiles/{id}/connect` — test/target bind
- `GET  /api/health/host?target=` — CPU/RAM/disk/load/processes; `target` =
  SSH profile id for remote hosts (§08)
- `GET  /api/docker/containers` · `GET /api/docker/images`
- `GET  /api/docker/containers/{id}/logs`
- `POST /api/docker/containers/{id}/{start|stop|restart}` — broker: confirmed
- `POST /api/docker/containers/{id}/exec` — exec shell → `{session_id}`;
  attach via `/ws/session/{id}` (it IS a session — §08 reuses §02/§03)

## Users/Roles (§09, Phase 4)
- `GET/POST/PATCH/DELETE /api/users` — admin only
- `GET /api/audit` — audit log (admin)

## System (§12)
- `GET   /api/system/version` — public
- `GET   /api/system/health` — public (probe-friendly for gate model)
- `GET/PATCH /api/settings` — runtime knobs (admin; `settings` table)

Rules: catalog entry before implementation; broker-permission annotation
mandatory on every mutating route; version prefix reserved (`/api/v1`
if a breaking surface ever ships).
