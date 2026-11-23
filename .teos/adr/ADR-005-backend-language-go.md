# ADR-005 — Backend Language: Go

**Status:** Accepted · **Date:** 2026-11-23 · **Amends:** brief v2 §11 stack (Rust + Tokio + Axum), ADR-002/003 implementation references

## Decision

Terminu's backend is **Go**, not Rust. The architecture is unchanged — same
modular monolith, same subsystem boundaries, same contracts — expressed as Go
packages instead of Rust crates. Stack: stdlib `net/http` (or `chi`) +
`coder/websocket`, `creack/pty`, `x/crypto/ssh`, official Docker Engine API
client, sqlc over `modernc.org/sqlite` (pure-Go, no cgo), screen model via
`hinshun/vt10x` — with a flagged spike for `wazero` + ghostty-WASM.

## Context

The stack choice is a workload question, not a language-preference question.
Terminu's actual work: PTY↔WebSocket fan-out, SSH connections, Docker/S3/file
providers, a screen-parsing layer, SQLite persistence — I/O plumbing with
moderate CPU parsing, at 50-session scale.

Two facts specific to this product's reality weigh in: it is built primarily
by **AI coding agents**, and it lives in a world where the owner's production
backend (JapaConnect) and engineering conventions (JEOS) are **already Go**.

## Analysis

| Axis | Rust | Go | Winner |
|---|---|---|---|
| Per-session PTY↔WS fan-out | tokio — capable, ceremonious | goroutines are shaped exactly like this problem | **Go** |
| SSH ecosystem | russh — good | `x/crypto/ssh` lineage (Teleport, Boundary) | **Go** |
| Headless screen model | `vte` (alacritty), `vt100` (zellij — the reference impl of §6) | `vt10x` suffices for *detection* (text regions, not pixels); `wazero`+ghostty-WASM possible | Rust, closable |
| Domain modeling | enums — elegant | interfaces + constants — workable | Rust, minor |
| Resource ceiling | tighter | comfortably inside <150MB/15MB budgets | tie |
| Single static binary | musl dance | `CGO_ENABLED=0 go build` — trivial | Go |
| **Agent codegen velocity** | borrow-checker = top agent failure loop | simplest semantics, largest corpus | **Go** |
| **Owner's existing toolchain** | new | Go conventions + sqlc workflow carry over | **Go** |

The decisive reasoning:

1. **The product is I/O plumbing.** Managing PTYs ≠ rendering them — Rust's
   advantages (emulator internals, no-GC ceilings) attach to a workload
   Terminu doesn't have. Go's advantage (concurrent I/O fan-out) attaches to
   exactly the workload it does.
2. **The screen-model gap is real but closable.** Adapters need text-region
   fidelity, not pixel fidelity — `vt10x` suffices; `wazero` running the
   ghostty WASM parser (already proven client-side) is a spike that would
   give one parser with identical behavior both sides.
3. **Agent-ease was already ratified as a decision axis** (JEOS ADR-113
   precedent). Borrow-checker friction is where agent-generated Rust stalls;
   Go maximizes iteration throughput for this build model.
4. **One toolchain across the owner's estate** — same conventions, same
   sqlc workflow, same agent muscle memory.

The desktop story survives: Tauri wraps the webview regardless; the
"thick wrapper" host adapter is a Go sidecar/library the shell bridges —
a standard Tauri pattern. Shared-core honesty (ADR-002) is language-
agnostic.

## Consequences

**Positive.** Faster agent-driven iteration; the strongest SSH/infra
ecosystem; trivial static binaries; JEOS conventions (sqlc, migration
naming, generated-file discipline) transfer verbatim; the corpus's
architecture docs needed edits only at the stack layer — boundaries held.

**Negative / accepted.** The screen model loses the `vt100`-class pedigree
— mitigated by detection-sufficient fidelity + the wazero spike. Rust's
enum-driven state modeling is missed; Go interfaces + the state catalog
convention compensate. If a future requirement ever demands emulator-grade
internals (it shouldn't — we manage PTYs), that component can be a Rust
carve-out without changing the language story.

## When this would be revisited

- `vt10x`/`wazero` spike fails to deliver adapter-sufficient screen fidelity
  AND no Go alternative works → the screen crate alone could be a Rust
  component behind the same interface.
- The desktop edition needs in-process Rust-specific Tauri APIs that a Go
  sidecar can't express → revisit the host adapter, not the whole stack.

## See Also

ADR-002 (editions — host-agnosticism is language-independent), ADR-003
(SQLite — sqlc/modernc replaces sqlx), `.ai/stack.yaml` (updated pins),
`specs/product-brief-v2.md` §11 (stack table updated), JEOS ADR-113
(agent-ease precedent).
