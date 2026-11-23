# ADR-003 — SQLite (WAL) Committed From Day 1; No Postgres Migration Planned

**Status:** Accepted · **Date:** 2026-11-23 · **Owner:** persistence layer

## Decision

Terminu's datastore is **SQLite in WAL mode** (driver `modernc.org/sqlite`,
queries via sqlc — ADR-005) — committed at day 1
for all persistence (workspaces, sessions, registry, audit, files bookmarks,
profiles). PostgreSQL is **not** a planned migration target; multi-node
horizontal scale is a deferred capability (brief §13), not a roadmap
destination.

## Context

v1 required a store matching the deployment model: single container, single
node, <150 MB idle, no required external services. The question was whether
to hedge toward Postgres for future multi-user/multi-node scale or commit
to SQLite fully.

## Analysis

| Option | Verdict |
|---|---|
| SQLite now, "migrate to Postgres later" | **Rejected.** The hedge is dishonest — code accretes SQLite-isms and the "easy migration" never is. Also implies a multi-node future that isn't in scope. |
| Postgres from day 1 | **Rejected.** External service breaks the single-container weight budget and the gate-friendly operational model; operationally unjustifiable at single-node scale. |
| **SQLite (WAL) committed** (chosen) | **Chosen.** WAL gives concurrent readers + one writer — sufficient for a single-admin/small-team product; zero-dep keeps the container and the 150 MB line honest. sqlc gives compile-checked queries so the seam is typed, not stringly. |

Single-writer is a *feature* of the scope, not a limitation to apologize
for: the v1 concurrency profile (sessions, registry, audit) is well inside
WAL's envelope, and write contention is bounded by the broker's serialized
confirmation path anyway.

## Consequences

**Positive.** Zero external services; backup = one file; migration story is
embedded and versioned (see `schemas/MIGRATION-ORDER.md`); matches the
"stopped-by-default, gate-friendly" operational model — a stopped service
needs no live DB to babysit.

**Negative / accepted.** True horizontal scale and multi-node deployments
would need a different store — that's the deferred enterprise product, and
this ADR says plainly it isn't planned. Concurrent-write bursts (audit
heavy-write sessions) must be batched — WAL single-writer makes writers
queue; that's the accepted envelope.

## When this would be revisited

- A real multi-node/server-fleet edition enters the roadmap (deferred
  capability, not v1) — the store seam is `sqlc`-generated typed queries, so a
  Postgres port is a new build, not a surprise.

## See Also

`schemas/MIGRATION-ORDER.md`; `.ai/stack.yaml`; brief §11 (stack), §14
(budgets), §13 (deferred multi-server scale).
