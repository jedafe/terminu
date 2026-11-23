# ADR-002 — Edition Sequencing: Web First, Desktop as Thick Wrapper, Agent Deferred

**Status:** Accepted · **Date:** 2026-11-23 · **Amends:** Product brief v1 (parallel desktop+server plan)

## Decision

Terminu ships **one edition at v1: the self-hosted web app**. The desktop
edition is deferred to post-1.0 and will be a **thick wrapper** — a Tauri
shell with the shared Go core as a sidecar — not a parallel
codebase and not a thin URL-in-a-window. The remote agent daemon is deferred
indefinitely; SSH covers remote targets in v1.

## Context

Brief v1 scoped desktop (Tauri) + server + agents as a parallel six-person,
~48-week program. The product reality: the wedge needs validation before
platform breadth, and the differentiators (touch UX, agent detection) are
all web-achievable. Sequencing decides what gets built and what gets
*learned* first.

## Analysis

| Option | Verdict |
|---|---|
| **Parallel desktop + server (v1 plan)** | **Rejected.** Two codebases, two PTY layers, ~double maintenance before product-market fit; the riskiest assumption (does the UX land?) gets validated last. |
| **Web only, forever** | **Rejected.** Desktop is a real acquisition surface (try-an-app before self-hosting) and local-terminal workflows need it — it stays in the product, sequenced. |
| **Web first; desktop = thin wrapper loading the server URL** | **Rejected.** Fails "desktop independence" — no offline local terminals, no local PTY, little reason to exist. |
| **Web first; desktop = thick wrapper sharing the core** (chosen) | **Chosen.** One codebase's worth of domain logic; desktop = different host adapter (local PTY + OS vault + tray + deep links + signed updates) over identical UI. Web validates the UX cheaply (a URL beats three OS installers); desktop inherits it proven. |
| **Agent daemon in v1** | **Rejected.** SSH covers remote targets without per-host daemons; the daemon is the single hardest component — revisit only if SSH-only proves insufficient (ADR-001 revisit trigger #3). |

## Consequences

**Positive.** The web build *is* the desktop's UI and most of its core —
nothing is thrown away; the package seams (`internal/domain`, `internal/session`,
`internal/screen`) are designed host-agnostic from day 1 so the wrapper is
genuinely thin where it should be and genuinely native where it counts.
Validation cost drops: deploy once, hand out a URL.

**Negative / accepted.** Desktop-only expectations (offline local PTY,
vault, hotkey) wait past 1.0 — a real cost if desktop turns out to be the
primary demand signal. The shared-core design must resist web-only shortcuts
that would make the future host adapter impossible — `.ai/architecture.yaml`
records which packages must stay host-agnostic.

## When this would be revisited

- Web traction shows demand concentrated on "manage THIS machine" (not
  remote) — pulls desktop forward.
- Tauri/system-webview limits bite the UI (Linux WebKitGTK is the known
  risk — brief §15).
- SSH remote coverage fails users → agent daemon earlier than post-1.0.

## See Also

`specs/product-brief-v2.md` §4.2 (editions), §12 (roadmap), §13 (deferred);
ADR-001 revisit conditions.
