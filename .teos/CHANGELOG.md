# TEOS Changelog

## v0.3.2 — 2026-11-23

Deep-review pass #2 — full-file read-through (routes↔tables, env↔sections,
rules↔modes):

- Contradiction fixed: no-root rule now carries the explicit `nsenter`
  carve-out (RULES + AGENTS) — it conflicted with ADR-006 host access
- Route gaps closed — every table now has a route: `alerts` (`/api/alerts`
  + ack), `launch_profiles` (`/api/agents/profiles` CRUD), `workspaces`
  (`/api/workspaces`), `settings` (`/api/settings`), `file_transfers`
  (`/api/files/transfers`), `adapter_pins` (`/api/adapters` pin/badge),
  session `PATCH`, bookmark `DELETE`, health `?target=` for remote hosts
- `invalid_transition` error code registered (was used, never registered)
- Rate-limit tiers defined in §10 (was a `rate_limited` code with no
  tiers); `/ws/events` clarified as read-only broadcast, auth at upgrade
- New env vars: `TU_ADMIN_PASSWORD` (first-boot plaintext alternative),
  `TU_ALLOWED_ORIGINS` (was enforced with no knob), `TU_WS_MAX_FRAME_KB`,
  `TU_WEB_DEV_PORT`
- `detached` clarified as an overlay flag on process state, not a state
  itself; §03 restart wording + scrollback-from-ring-buffer documented
- MIGRATION-ORDER: `session_state_log` single-owner (§03; §05 consumes)

## v0.3.1 — 2026-11-23

Full-corpus review pass against the product decisions (spec §3 P1–P7,
§13 deferrals, ADR-001–006). Consistency fixes:

- `CliAdapter`/`detect()` → `Adapter`/`Detect()` corpus-wide
- `ended` state registered in `session-state-catalog` (§03 already used it)
- `risk: medium` → `controlled` in both manifest examples (enum violation)
- WS catalog: `seq`/`next_seq` documented on `data`/`hello` (§10 resync
  contract now has a catalog entry)
- ADR-001 stale path refs → `specs/`; ADR-004 `PTTY` typo
- Roadmap: task count corrected (70), honest phase-sum estimate (~16–20),
  crate-name leftovers → `internal/*`, `trait` → `interface`
- Coverage gaps closed: §10/§12 embedded web assets (`go:embed` — the
  single-binary deliverable was unspecified); §11 prompt overlays now
  explicitly apply to non-agent sessions (P1); §03 `invalid_transition`;
  stack.yaml argon2/nacl auth libs; `.ai/context/current-phase.md`
- RULES gains the manual-test/file-list/fail-threshold operating notes

## v0.3.0 — 2026-11-23

Scope decision: **Terminu retires ttyd + SFTPGo** (ADR-006).

- New `adr/ADR-006-retire-ttyd-sftpgo.md`
- §07 gains SFTP protocol endpoint (gliderlabs/ssh + pkg/sftp ops →
  `StorageProvider` calls; §09 auth; broker-gated mutations)
- §12 gains host-access modes — `TU_LOCAL_TARGET_MODE=host|nsenter|
  container` (replicates the ttyd nsenter pattern honestly)
- `.ai/stack.yaml` (ssh-server libs), `conventions/` (TU_SFTP_*,
  TU_LOCAL_TARGET_MODE, sftp:// endpoint), `roadmaps/` (tasks 3.12–3.15
  + Phase-03 retirement gate), `specs/` §8 + §16
- SFTPGo features consciously NOT replicated (quotas, sharing links,
  multi-user ACLs, event hooks, WebDAV/FTP) remain deferred.

## v0.2.0 — 2026-11-23

Backend language decision: **Rust → Go** (ADR-005).

- New `adr/ADR-005-backend-language-go.md`
- Stack layer updated corpus-wide: `.ai/stack.yaml` (Go pins: net/http +
  coder/websocket, creack/pty, vt10x + wazero spike, modernc+sqlc,
  x/crypto/ssh, official Docker SDK), `.ai/architecture.yaml` (crate map →
  package map: `cmd/` + `internal/*`), `sections/01` (layout, CI), `06`
  (Go `Adapter` interface), `07` (Go `StorageProvider`), `08`–`09`,
  `conventions/sqlite-migration-naming.md` (golang-migrate + sqlc, same
  as JapaConnect), `skills/`, `RULES.md`, `AGENTS.md` (Go tooling,
  internal/db generated-file rule), `README`, `TEOS.md`, `specs/`
- Architecture unchanged — boundaries expressed as Go packages.

## v0.1.0 — 2026-11-23

Initial corpus. Seeded from the Terminu product-brief revision (v2) and the
adapter design spec.

- `specs/product-brief-v2.md` — revised product vision/architecture/roadmap
  (wedge: agent control room + touch-native terminal; deferred scope table)
- `specs/adapter-design.md` — Screen Interpreter + adapter engineering spec
- `adr/ADR-001` — screen-interpreter adapter detection architecture
- `adr/ADR-002` — edition sequencing (web v1; desktop thick wrapper post-1.0;
  agent daemon deferred)
- `adr/ADR-003` — SQLite (WAL) committed from day 1; no Postgres migration
- `adr/ADR-004` — execution broker as the only privileged-action path
- `sections/01–12` — initial feature spec files
- `roadmaps/ROADMAP-V1.md` — Phase 0–4 task plan
- `conventions/` — env vars, API/WS catalogs, session states, manifest format,
  response format, migration naming
- `schemas/MIGRATION-ORDER.md` — initial SQLite table ordering
- Root operating files: `TEOS.md`, `AGENTS.md`, `CLAUDE.md`, `RULES.md`,
  `.ai/` config
