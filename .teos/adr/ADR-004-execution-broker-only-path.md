# ADR-004 — The Execution Broker Is the Only Privileged-Action Path

**Status:** Accepted · **Date:** 2026-11-23 · **Owner:** security substrate

## Decision

**Every** privileged action in Terminu — terminal spawn, command execution,
file write, SSH exec, Docker operation — flows through one authenticated
path:

``` text
Authenticated user → API → identity → policy evaluation →
confirmation (per risk class) → execution broker → target
```

No feature code calls an execution target directly. Risk classification
(`informational | controlled | confirmed | restricted`) is evaluated per
call against user role, target, command, argv, working dir, environment
profile, and command risk class. LLM-originated commands inherit the same
gate — there is no AI fast-path.

## Context

A web platform that spawns shells, moves files, and drives Docker is a
remote-code-execution product wearing a management UI. The security model
cannot be a collection of per-endpoint checks — it must be a single
chokepoint, or the first missed endpoint is the breach.

## Analysis

| Option | Verdict |
|---|---|
| Per-endpoint auth checks | **Rejected.** Checks scatter across routes; a new endpoint forgetting its check = an unguarded RCE. Doesn't scale with feature count. |
| Client-side permission gating | **Rejected.** UI gating is theater — the WS/API is reachable without the UI. |
| **Single broker chokepoint** (chosen) | **Chosen.** One place for policy, one place for confirmation, one place for audit — correctness scales with surface area instead of against it. Cost: every privileged op pays broker latency — acceptable, it's the price of the guarantee. |

The honesty rule comes with it: **a general-purpose shell is arbitrary code
execution as the user** — documented, gated by risk class, never disguised
behind the UI. Terminu doesn't pretend the terminal is safe; it makes the
unsafe thing *explicit*.

## Consequences

**Positive.** One audit trail for everything privileged; policy changes
land in one place; the risk model applies uniformly whether a command came
from a user tap, a manifest, or an LLM suggestion; review surface for
security = the broker, not the codebase.

**Negative / accepted.** All privileged ops pay policy-eval latency; hot
paths (PTY data plane — actual terminal I/O) must be carved out carefully:
the broker gates *spawn/execute*, not the *byte stream* of an authorized
session — per-frame broker checks would violate the latency budget.

## When this would be revisited

- Multi-user/org RBAC arrives post-1.0 → policy evaluation gains role
  matrices; the chokepoint architecture is unchanged, its policy table
  grows.
- Plugin system (deferred) → third-party code enters only behind broker-
  mediated capabilities; never raw target access.

## See Also

`specs/product-brief-v2.md` §10 (security model, risk table, hard rules);
`sections/09-security-execution-broker.md`; ADR-001 (LLM gating).
