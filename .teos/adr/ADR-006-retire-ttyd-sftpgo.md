# ADR-006 — Terminu Retires ttyd + SFTPGo

**Status:** Accepted · **Date:** 2026-11-23 · **Owner:** product scope
**Implements:** brief §8 amendment; adds Phase-03 deliverables

## Decision

Terminu is explicitly designed to **retire the ttyd + SFTPGo operational
stack** — not merely overlap it. Three requirements follow:

1. **Terminal + file web surfaces** — already core (§02/§03 terminal,
   §07 file manager). ttyd's surface is fully subsumed, including the
   multiplexer's job: server-owned sessions replace tmux/zellij.
2. **An SFTP protocol endpoint** — exposed over the same
   `StorageProvider` abstraction that backs the web UI, so native SFTP
   clients keep protocol access (the one thing SFTPGo does that a web
   file manager doesn't).
3. **A host-access pattern for the local target** — containerized
   Terminu must reach the real host the way the current ttyd deployment
   does via `nsenter`-class namespace entry (or host-deployed variant).
   Specified in §12 — retirement isn't honest without it.

Retirement becomes a **roadmap deliverable**: Phase 03's gate includes a
migration checklist — ttyd and SFTPGo stay gated+stopped until their
workflows are verified covered, then removed.

## Context

The current VPS tooling (ttyd host terminal, SFTPGo file manager) is a
hand-assembled approximation of what Terminu productizes — the problems
that drove the product (P1–P7) were lived on this stack. The question
was whether Terminu leaves residual dependencies on either tool.

## Analysis

| SFTPGo capability | Terminu coverage |
|---|---|
| Web file manager over local FS + S3 virtual folders | §07 `local` + `s3` providers — covered |
| SFTP protocol server for native clients | **This ADR adds it** — SFTP ops → provider calls via `gliderlabs/ssh` + `pkg/sftp` |
| Per-user auth/quota/sharing links/event hooks | Deferred — single-admin product; the deferred table stands |
| WebDAV / FTP / SCP | WebDAV/SCP post-1.0 (already deferred); FTP never |

| ttyd capability | Terminu coverage |
|---|---|
| Terminal in browser | §02 — plus persistence, touch, clipboard, reconnect it lacks |
| Host shell via nsenter + privileged container | §12 host-access pattern — same technique, now spec'd |
| Persistence via tmux/zellij glue | §03 server-owned sessions — multiplexer eliminated |

## Consequences

**Positive.** One deployment replaces two services + a multiplexer + their
perimeter/gate config; the "ops access gate" model maps cleanly (Terminu
itself is gate-friendly, P6); protocol access isn't lost — `sftp` CLI,
Cyberduck-class clients, and `rsync`-style workflows keep working against
the same providers the web UI uses.

**Negative / accepted.** Scope gains one protocol surface — bounded by
design (auth reuses §09 users/keys; ops map 1:1 onto the existing provider
interface; writes/deletes pass the broker + audit like any mutation). The
full SFTPGo feature surface is *not* replicated — quotas, sharing links,
multi-user ACLs, event hooks remain deferred; if they're ever needed,
they're a phase, not a hidden gap.

## When this would be revisited

- SFTP-server scope balloons toward "another SFTPGo" (virtual-folder admin
  UI, per-user quotas) → split it post-1.0; OpenSSH suffices for interim
  protocol access.
- The host-access pattern proves unsafe/insupportable in some deployment →
  documented degraded mode (container-only local target) with the caveat
  surfaced.

## See Also

`specs/product-brief-v2.md` §7–§8, §12, §16 · `sections/07` (SFTP
endpoint), `sections/12` (host access) · `roadmaps/ROADMAP-V1.md` Phase 03
tasks 3.12–3.15 · `.ai/stack.yaml` (ssh-server libs).
