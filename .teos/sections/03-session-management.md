# Section 03 — Session Management

Owns: session lifecycle, registry, persistence, attach/detach/resync,
workspaces. Tables `workspaces`, `sessions`, `session_state_log`.

## Model

- A **session** = an owned PTY process tree + screen model + state. The
  server owns it; clients attach as views. Session ≠ window (AP1/AP2).
- States/kinds/confidence per `conventions/session-state-catalog.md` —
  the catalog is the contract; this section owns transitions.

## Lifecycle

``` text
spawn → starting → working ⇄ waiting-for-input → done | error
                      ↘ detached (no client > window) → reattach
```

- `spawn` — broker-gated (`controlled`); records target, cwd, shell, argv.
- `attach` — idempotent; replays scrollback from client cursor; never
  respawns (Rule 6).
- `detach` — marks window timer; session keeps running.
- `terminate` — broker-gated; SIGTERM → grace → SIGKILL; exit recorded.
- `exit` — exit code → `done`/`error`; state_log row + `agent.state` event.

## Persistence

- `sessions` row per PTY; `session_state_log` append-only transitions.
- Scrollback lives in the per-session in-memory ring buffer
  (`TU_SESSION_BUFFER_KB`); `GET /api/sessions/{id}/scrollback` pages it —
  it is not persisted separately (a restart ends the session anyway).
- Restart policy: a server restart does NOT respawn PTYs (they died with
  the host); sessions are marked `ended` — honest, not fake-restored.
- `workspaces` persist layout (which session ids in which tabs/splits) —
  views, not processes.

## Registry

- In-memory live index projected from `session_state_log` latest-per-
  session; `/api/sessions` + `agent.state` events read it. Emits on
  transition only — never polled.

## Concurrency

- `TU_MAX_SESSIONS` ceiling; spawn returns `session_limit` when hit;
  out-of-order lifecycle ops (attach to `ended`, re-exec on `done`)
  return `invalid_transition`.
- Per-session: one writer task (PTY read → broadcast), N client attaches.
  Fan-out with per-client bounded channels (RULES backpressure).

## Acceptance

- Disconnect mid-agent-run → session intact, state kept; reconnect within
  window → same screen; two attaches → one process; server restart →
  sessions listed `ended`, no ghosts.
