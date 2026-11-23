# TEOS — Master Index

**TEOS v0.3.4** | Terminu Engineering Operating System
**Status:** Pre-implementation — corpus seeded from v2 product decisions

This is the single file an agent should load first. Every file in this
corpus is listed here with a one-line description.

---

## Roadmaps

| File | Scope | Phases | Tasks | Status |
|------|-------|--------|-------|--------|
| `roadmaps/ROADMAP-V1.md` | v1 product build | 5 (0–4) + post-1.0 program | 70 | 📋 Planning |

## Sections — the source of truth

Each section owns the schema, routes, and logic for one feature area.
Read the section before implementing.

| # | File | What it owns |
|---|------|--------------|
| 01 | `sections/01-foundation-tech-stack.md` | Package layout, toolchain, config, CI baseline |
| 02 | `sections/02-terminal-engine.md` | Renderer, PTY data plane, touch layer, key bar, clipboard, reconnect, tabs/splits, search — solves P1–P3, P5 |
| 03 | `sections/03-session-management.md` | Session lifecycle, registry, workspaces, attach/detach/resync — `sessions`, `session_state_log`, `workspaces` |
| 04 | `sections/04-command-intelligence.md` | Registry, manifests, discovery, palette, context actions — `command_manifests` |
| 05 | `sections/05-agent-session-management.md` | Agent registry, needs-you view, overlays, alerts, launch profiles — `launch_profiles`, `alerts` |
| 06 | `sections/06-screen-interpreter-adapters.md` | Screen model, trigger gate, `Adapter` interface, generic tiers, fixtures — `adapter_pins`, `fixtures` |
| 07 | `sections/07-file-manager.md` | Storage providers (local/S3/SFTP), transfers, bookmarks, **SFTP protocol endpoint** (ADR-006) — `file_bookmarks`, `file_transfers` |
| 08 | `sections/08-infrastructure-view.md` | SSH profiles/targets, host health, Docker pane — `ssh_profiles` |
| 09 | `sections/09-security-execution-broker.md` | Auth, roles, broker, risk model, audit, secret store — `users`, `auth_sessions`, `audit_log` |
| 10 | `sections/10-api-ws-contracts.md` | Route surface, WS framing, middleware, versioning policy |
| 11 | `sections/11-frontend-shell.md` | SvelteKit shell, chrome, touch UI behaviors, data flow |
| 12 | `sections/12-deployment-operations.md` | Build, env wiring, gate-friendly ops, budgets — `settings` |

## ADRs — why decisions were made

| # | File | Decision |
|---|------|----------|
| 001 | `adr/ADR-001-screen-interpreter-adapter-detection.md` | Detect CLI/agent state via headless screen model + version-pinned per-tool adapters selected by process binding; trigger-gated, LLM opt-in |
| 002 | `adr/ADR-002-edition-sequencing-web-first.md` | Web-only v1; desktop = post-1.0 thick wrapper sharing the core; agent daemon deferred |
| 003 | `adr/ADR-003-sqlite-committed-day-1.md` | SQLite (WAL, modernc+sqlc) committed; no Postgres migration planned |
| 004 | `adr/ADR-004-execution-broker-only-path.md` | All privileged ops through one broker; risk classes; no AI fast-path |
| 005 | `adr/ADR-005-backend-language-go.md` | Backend language = Go (I/O-plumbing fit, agent velocity, owner's Go toolchain); architecture unchanged |
| 006 | `adr/ADR-006-retire-ttyd-sftpgo.md` | Terminu retires ttyd+SFTPGo: SFTP protocol endpoint over providers + host-access modes; Phase-03 retirement gate |

## Conventions — catalogs and contracts (register first, use second)

| File | Registry |
|------|----------|
| `conventions/env-vars-catalog.md` | All `TU_*` env vars |
| `conventions/api-routes-catalog.md` | All HTTP routes + broker annotations |
| `conventions/websocket-message-catalog.md` | All WS frame types, both sockets |
| `conventions/session-state-catalog.md` | Session states, wait-kinds, confidence levels |
| `conventions/adapter-manifest-format.md` | Command manifest YAML schema |
| `conventions/api-response-format.md` | Response/error envelope + error codes |
| `conventions/sqlite-migration-naming.md` | Migration file convention |

## Schemas

| File | Purpose |
|------|---------|
| `schemas/MIGRATION-ORDER.md` | Ordered table registry (16 tables) — register before migrating |

## Specs — product/design depth

| File | Content |
|------|---------|
| `specs/product-brief-v2.md` | Product vision, wedge, requirements P1–P7, architecture, roadmap rationale, deferred table, risks, distribution — the parent document |
| `specs/adapter-design.md` | Screen Interpreter + adapter engineering spec: pipeline, `Adapter` interface, per-tool pattern hypotheses, fixture strategy, build order, open questions |

## Skills — reusable implementation patterns

| File | Pattern |
|------|---------|
| `skills/pty-session-lifecycle.md` | Server-owned PTY lifecycle, attach/resync, backpressure |
| `skills/screen-adapter-pattern.md` | Capture→implement→extract→replay-test adapter recipe |

## `.ai/` — machine-readable config

| File | Content |
|------|---------|
| `.ai/context/current-phase.md` | Live phase/task tracker — agents check here first |
| `.ai/project.yaml` | Product metadata, personas, wedge |
| `.ai/stack.yaml` | Pinned tech stack |
| `.ai/architecture.yaml` | Principles AP1–AP7, layers, package map |
| `.ai/glossary.yaml` | Terminu-specific term definitions |

## Root operating files

| File | Purpose |
|------|---------|
| `README.md` | What TEOS is + how to use |
| `AGENTS.md` | The 18 golden rules + secrets/safety |
| `CLAUDE.md` | Pointer to AGENTS.md |
| `RULES.md` | Hard coding/operational rules |
| `CHANGELOG.md` | Corpus version history |

## Supporting dirs

`audit/` — milestone audits · `plans/` — dated implementation plans ·
`ideas/` — uncommitted ideas (Rule 10 gate) · `documentations/` —
post-phase write-ups · `diagrams/` — Mermaid sources.

---

## Status summary

Everything in this corpus is **specification** — no code exists yet. The
v1 roadmap carries 70 tasks across 5 phases (~16–20 weeks). The wedge —
agent control room + touch-native terminal — lands in Phase 02.
