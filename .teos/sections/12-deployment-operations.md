# Section 12 — Deployment & Operations

Owns: container/binary build, env wiring, gate-friendly operational model,
budgets. Table `settings`.

## Deliverables

- One OCI image (`terminu/terminu`) and one static binary — same artifact
  pipeline. The single binary **embeds the built web assets** (`go:embed`)
  so it serves the UI itself — no separate static host, true single-
  artifact deploy. Binds `TU_BIND:TU_PORT`.
- Boot requirements: `TU_SESSION_SECRET`, `TU_ADMIN_USER`, and either
  `TU_ADMIN_PASS_HASH` or `TU_ADMIN_PASSWORD` — everything else defaults.

## Operational model (gate-friendly)

- Designed to run **stopped-by-default** behind an admin gate: no always-
  on requirement, no external services, cold start <3 s, idle <150 MB.
- `/api/system/health` is public and cheap — a gate/allowlist probe can
  check it without auth (mirrors the JC access-gate probe model).

## Budgets (hard — Rule 13)

| Metric | Line |
|--------|------|
| Idle RSS | <150 MB |
| Per-session overhead | <15 MB |
| Cold start | <3 s |
| Reconnect resync | <3 s |
| Input latency (LAN) | <50 ms |
| Concurrent sessions | 50+ |

Measured in CI: container smoke test asserts idle RSS; load harness
asserts resync + latency.

## Host access (ADR-006 — retires ttyd's model)

A containerized Terminu's `local` execution target sees only the
container — to reach the real host (the ttyd deployment's entire value),
one of these is required, in preference order:

1. **Host-deployed binary** — run `terminu-server` on the host; `local`
   target is the real FS/shell by definition. Simplest, most honest.
2. **Privileged container + nsenter-class exec** — replicate the current
   ttyd pattern: the container enters host mount/PID namespaces for
   local-target operations. Documented privilege requirement; the gate
   model applies (stopped-by-default).
3. **Degraded mode** — container-only local target + SSH profiles for
   host access (host's own sshd as the target). Zero extra privileges;
   remote-detection latency applies.

Choice is a deployment-time env (`TU_LOCAL_TARGET_MODE=host|nsenter|
container`), default `container` — least privilege unless the operator
opts in.

## Persistence & backup

- `TU_DATA_DIR` holds `terminu.db` (WAL) + secret store + fixtures +
  workspace roots. Backup = copy the dir (service stopped) — documented.

## TLS

- Terminu binds loopback/plain HTTP behind a TLS terminator (Caddy/
  Traefik) — the deployment model we've already proven; no in-app ACME
  in v1.

## Acceptance

- `docker run` with three env vars → login screen <3 s; idle RSS under
  the line; `system/health` 200 unauthenticated; restart keeps data;
  stopped service leaves no orphan requirement.
