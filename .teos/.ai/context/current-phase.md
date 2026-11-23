# Current Phase — live tracker

**Corpus status:** v0.3.4 — specification + interim implementation shipped
**Active phase:** Phase 02 — Command Intelligence (next)
**Last completed:** Phase 00 + Phase 01 (interim shell build, deployed)

## Shipped (2026-11-23)

Phase 0+1 interim build, live at shell.japa.to (retired ttyd):

- Backend: TU_* env + termiu.toml, SQLite/modernc + embedded migration
  runner (6 tables), sqlc codegen, argon2id cookie auth, broker
  Check/Audit skeleton, PTY session manager (server-owned, seq'd ring
  buffer), WS attach/hello/data/input/resize/detach/eof + /ws/events
- Frontend: SvelteKit SPA (adapter-static, single bundle, go:embed) —
  login, session drawer, pane grid, xterm.js (fit/WebGL/search/OSC52),
  KeyBar sticky modifiers, select/mouse touch toggle, bracketed paste,
  workspace layout persistence
- Deploy: Dockerfile (bun→go→debian-slim multi-stage), compose swap in
  japaconnect deploy/shell (ttyd→terminu, pinned git-context builds,
  acme/caddy/Traefik unchanged), nsenter host-access verified
  (HOST=mail, USER=japa, job control via script-pty relay)

## Verified on production

login → spawn → attach{last_seq} → hello{next_seq} → seq'd data frames →
resync replay; host shell job control (^Z/jobs) and interrupts (^C);
restart honesty (ended state); scrollback; audit rows on spawn/kill.

## Deferred to next phases

- Phase 02: screen model (vt10x or wazero spike), adapters, needs-you
- Phase 03: file providers + SFTP endpoint + docker pane
- Phase 04: multi-user roles, confirmation tokens, desktop
- Ring: scrollback is in-memory only (by design — restart ends sessions)
- ttyd app record lingers as exited in Coolify — prune via UI when
  confident

Update this file when a phase starts/closes or a gate is evaluated.
