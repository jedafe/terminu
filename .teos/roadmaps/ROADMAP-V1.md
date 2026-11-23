# ROADMAP-V1 — Terminu v1 Build Plan

The wedge first: touch-native terminal + agent control room. Task format:
`- [ ] N.NN: description — Ref: Section NN`. Phases are sequential; tasks
within a phase may parallelize where marked `⟂`.

Estimates assume a small focused effort; the deferred list (brief §13)
admits nothing without a phase slot or `ideas/` entry (Rule 10).

---

## Phase 00 — Project Bootstrap

- [ ] 0.01: Go module + package skeleton per `.ai/architecture.yaml` — Ref: §01
- [ ] 0.02: `apps/web` SvelteKit init (Bun, Svelte 5) — Ref: §01, §11
- [ ] 0.03: Config plumbing — `TU_*` env + `terminu.toml` precedence — Ref: §01
- [ ] 0.04: SQLite (modernc + sqlc, WAL, pragmas) + migration runner — Ref: §01, §12
- [ ] 0.05: CI: gofmt/vet/test/svelte-check + container smoke — Ref: §01
- [ ] 0.06: `.teos` reference-comment convention in code — Ref: AGENTS.md

## Phase 01 — Web Terminal Core

- [ ] 1.01: `internal/session` — PTY spawn via creack/pty, goroutine per session — Ref: §03
- [ ] 1.02: Session registry + `sessions`/`session_state_log` tables — Ref: §03
- [ ] 1.03: `WS /ws/session/{id}` — binary data/input frames, seq numbers — Ref: §10
- [ ] 1.04: Attach/detach/resync protocol + reconnect window — Ref: §02, §03
- [ ] 1.05: Backpressure — per-client bounded buffers, drop-not-stall — Ref: §02
- [ ] 1.06: Auth (sessions cookie, Argon2, admin seed) + auth middleware — Ref: §09 ⟂
- [ ] 1.07: Execution broker skeleton — identity→policy→execute→audit — Ref: §09 ⟂
- [ ] 1.08: `TerminalSurface` component (xterm.js, fit, WebGL) — Ref: §02
- [ ] 1.09: Tabs/splits UI over sessions; workspace layout persist — Ref: §02, §03
- [ ] 1.10: **Key bar** — Esc/Ctrl/Alt/arrows/Tab/Fn, sticky modifiers — Ref: §02
- [ ] 1.11: **Touch layer** — tap→click, two-finger scroll, long-press RMB — Ref: §02
- [ ] 1.12: **Clipboard** — OSC52 bridge, select-copy, paste button, bracketed paste — Ref: §02
- [ ] 1.13: TUI-mouse vs browser-select mode toggle — Ref: §02
- [ ] 1.14: Buffer search + scrollback cap enforcement — Ref: §02
- [ ] 1.15: `WS /ws/events` feed + `session.added/removed` events — Ref: §10 ⟂

**Gate:** all P1–P5 acceptance on a real touch device (§02 Acceptance).

## Phase 02 — Command Intelligence + Agent Sessions (the wedge)

- [ ] 2.01: `internal/screen` — headless VT model, grid/cursor/dirty regions — Ref: §06
- [ ] 2.02: Process-tree binder — PTY fg process → session → adapter slot — Ref: §06
- [ ] 2.03: Trigger gate — quiescence/dirty/cursor/on-demand/process-change — Ref: §06
- [ ] 2.04: `Adapter` interface + `StateMatch`/`Prompt`/`Keystroke` types — Ref: §06
- [ ] 2.05: Fixture harness — `.cast` replay → `Detect()` assertions — Ref: §06
- [ ] 2.06: **aider adapter + G1 generic** (line-based — proves pipeline) — Ref: §06
- [ ] 2.07: Session-state registry → `waiting-*` states + confidence — Ref: §03, §05
- [ ] 2.08: `state`/`prompt`/`prompt_clear` WS frames + `agent.state` events — Ref: §10
- [ ] 2.09: **"Needs you" view** — waiting-first ordering, prompt overlay buttons — Ref: §05, §11
- [ ] 2.10: Prompt→keystroke injection path (human-sends-only) — Ref: §05
- [ ] 2.11: Transition alerts — `alerts` table + `alert` events + ack UI — Ref: §05
- [ ] 2.12: **claude adapter** (fixtures first — forces harness maturity) — Ref: §06
- [ ] 2.13: devin adapter — Ref: §06
- [ ] 2.14: codex adapter — Ref: §06
- [ ] 2.15: gemini adapter — Ref: §06
- [ ] 2.16: opencode adapter (`scan_scope: full`) — Ref: §06
- [ ] 2.17: Generic tiers G2–G4 + `unknown` demotion — Ref: §06
- [ ] 2.18: `command_manifests` + manifest loader (builtin/custom) — Ref: §04
- [ ] 2.19: Curated v1 registry — six AI CLIs + dev core (git/docker/go/node/python/aws/kubectl) — Ref: §04
- [ ] 2.20: Command palette — `GET /api/commands` fuzzy <100 ms — Ref: §04, §11
- [ ] 2.21: Context actions — `context/{session_id}` via detected process — Ref: §04
- [ ] 2.22: Manifest execution → broker (param resolution, argv build) — Ref: §04, §09
- [ ] 2.23: Auto-discovery — `--help`/completion parsing → provisional manifests — Ref: §04
- [ ] 2.24: Launch profiles — save/list/launch bound to adapters — Ref: §05
- [ ] 2.25: Adapter pins + `unverified` badge on mismatch — Ref: §06

**Gate:** fixture replay green all six; needs-you + buttons verified against a live devin/claude session; zero per-frame scans (§06 Acceptance).

## Phase 03 — Files + SSH

- [ ] 3.01: `StorageProvider` interface + `local` provider (rooted, traversal-safe) — Ref: §07
- [ ] 3.02: File list/stat/mkdir/rename/delete routes (broker-gated) — Ref: §07, §10
- [ ] 3.03: Chunked upload/download + resume + `files.progress` — Ref: §07
- [ ] 3.04: S3 provider (configurable endpoint) — Ref: §07
- [ ] 3.05: `internal/conn` — x/crypto/ssh client, profiles, secret store — Ref: §08, §09
- [ ] 3.06: SSH profiles CRUD + host verification + trust prompt — Ref: §08
- [ ] 3.07: SFTP provider over SSH connections — Ref: §07, §08
- [ ] 3.08: SSH execution target — remote sessions via profile — Ref: §08
- [ ] 3.09: Remote process binder (`ps` scrape) → detection on SSH sessions — Ref: §06, §08
- [ ] 3.10: File manager UI — tree/list, previews, bookmarks, transfer queue — Ref: §07, §11
- [ ] 3.11: Remote edit-in-terminal — open session at file path — Ref: §07
- [ ] 3.12: **SFTP server** — gliderlabs/ssh + pkg/sftp scaffold, host keys, §09 auth — Ref: §07, ADR-006
- [ ] 3.13: SFTP→StorageProvider op mapping + broker gating on mutations — Ref: §07, §09
- [ ] 3.14: `TU_LOCAL_TARGET_MODE` — host/nsenter/container host-access modes — Ref: §12, ADR-006
- [ ] 3.15: **Retirement checklist** — verify ttyd+SFTPGo workflows covered; gate both off; remove services — Ref: ADR-006

**Gate:** SFTP endpoint live against all three providers with §09 auth;
a native SFTP client lists/writes through it; every workflow previously
done via ttyd/SFTPGo demonstrably works in Terminu (ADR-006).

## Phase 04 — Team + Infrastructure

- [ ] 4.01: Users CRUD + roles (admin/operator/viewer) enforcement — Ref: §09
- [ ] 4.02: `GET /api/health/host` — CPU/RAM/disk/load/processes — Ref: §08
- [ ] 4.03: Server health UI (on-open + visible-refresh only) — Ref: §08, §11
- [ ] 4.04: Docker provider — containers/images list + logs — Ref: §08
- [ ] 4.05: Docker start/stop/restart (confirmed) + exec shell → session — Ref: §08, §02
- [ ] 4.06: Audit log viewer (admin) — Ref: §09, §11
- [ ] 4.07: Budget gates in CI — idle RSS, resync, latency assertions — Ref: §12
- [ ] 4.08: Single-container release pipeline + `system/health` — Ref: §12
- [ ] 4.09: Docs: self-host guide, env reference, adapter-writing guide — Ref: §12

## Phase X — Post-1.0 (unscheduled; Rule 10 gates entry)

Desktop thick wrapper (Tauri + in-process core) → LLM opt-in polish →
OIDC/orgs → agent daemon → plugins → workflows → marketplace → WebDAV/SCP.
Each requires a phase assignment + sections before code (Rule 10).

---

## Estimates

| Phase | Weeks | Note |
|-------|-------|------|
| 00 | 2–3 | foundation |
| 01 | 4–5 | includes all touch/clipboard/reconnect work |
| 02 | 4–5 | the wedge — heaviest single phase |
| 03 | 3–4 | files + SSH |
| 04 | 3 | team + infra + release |
| **1.0 total** | **~16–20** | demoable wedge after Phase 02 (~10–13) |
