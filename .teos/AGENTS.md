# AGENTS.md — AI Agent Operating Rules

**For:** Claude Code, Cursor, Devin, and any other AI agent implementing Terminu
**Code repo:** `~/websites/terminu` (this corpus lives inside it at `.teos/`)

Read this file completely before touching any code. These rules are
non-negotiable — they exist because violations cause implementation lapses.

---

## Step 0 — What to Load First

Before writing a single line of code for any task:

1. Load `TEOS.md` — the master index; know what exists
2. Load `roadmaps/ROADMAP-V1.md` — find your task's phase and task number
3. Load the `sections/` files referenced by that task
4. Load any `conventions/` files referenced by those sections
5. Load the ADR if you need to understand *why* a decision was made
6. Only then write code

**Never implement from memory.** Your training data does not contain
Terminu-specific contracts. The section files do. Read them.

---

## The 18 Golden Rules

### Rule 1 — Section files are the source of truth
The roadmap says WHAT and WHEN. The section file says HOW. If they disagree,
the section file wins — flag the discrepancy in a code comment and proceed
with the section's version.

### Rule 2 — Every privileged action passes the execution broker
Terminal spawn, command execution, file write, Docker action, SSH exec — all
flow through the broker (permission → policy → confirmation → audit). No
feature code calls the OS or an execution target directly. This is ADR-004;
it is the security model, not a suggestion.

### Rule 3 — No hardcoded config values
Every configurable value comes from `TU_*` environment variables or the
config file (see `conventions/env-vars-catalog.md`). No magic numbers, no
hardcoded URLs, no hardcoded limits. If you need a new knob, add it to the
catalog first.

### Rule 4 — Structured argv, never string interpolation
Command execution builds `executable + argv[]`. Never format user input into
a shell string. LLM-suggested commands obey this too — and pass the same
risk gate (Rule 12).

### Rule 5 — API errors use the standard format
Every error response follows `conventions/api-response-format.md` — a
`snake_case` machine-readable `code`, not a sentence.

### Rule 6 — Session lifecycle is server-owned
The Session Manager owns the PTY. Clients attach, detach, resync — a client
disconnect never kills a session and a reattach never spawns a duplicate
process (see `sections/03-session-management.md`). "Sessions, not windows."

### Rule 7 — Adapters are pure functions with mandatory honesty
Per `sections/06` + `specs/adapter-design.md`: no I/O inside `Detect()`;
return `nil` when uncertain (false-negative is graceful, false-positive
injects wrong buttons); every match carries `evidence`; every adapter
declares an `AdapterPin`; every adapter ships with fixture replay tests.
An adapter without fixtures does not merge.

### Rule 8 — Every WS message type must be in the catalog
`conventions/websocket-message-catalog.md` is canonical. New message types
get added there first — an unregistered type silently breaks clients.

### Rule 9 — Every route must be in the routes catalog
`conventions/api-routes-catalog.md` first, code second. Same discipline as
Rule 8.

### Rule 10 — Feature scope gates on the roadmap, not enthusiasm
Any feature request must map to a numbered roadmap phase or the deferred
table (brief §13 / roadmap Phase X). Un-mappable ideas don't enter the
codebase — add them to `ideas/` and flag the human. This is the
anti-scope-creep rule; it protects the product's wedge.

### Rule 11 — No new env var, state name, manifest field, or error code
without its catalog entry
Catalogs in `conventions/` are the registries. Register first, use second.

### Rule 12 — LLM features are opt-in and never on the execution path
No LLM call happens without explicit user configuration; no LLM-originated
command bypasses the broker; no screen content leaves the deployment without
explicit user action (ADR-001, brief §5.4).

### Rule 13 — Performance budgets are hard constraints
<150 MB idle, <15 MB per session, <3 s reconnect resync, <50 ms LAN input
latency. A change that blows a budget doesn't merge until it's under the
line or the ADR revises the line.

### Rule 14 — SQLite migrations follow MIGRATION-ORDER.md
Numbered `migrations/NNNNNN_description.up.sql` (golang-migrate style, sqlc
for queries — never hand-edit `internal/db/`). New tables go in the
ordered list before the migration is written. Migrations run as a distinct
step before the new binary serves traffic.

### Rule 15 — Frontend consumes the backend API only
No hardcoded data arrays, mock objects, or local-only state mutations for
server data in production Svelte code. Same discipline as JEOS Rule 16:
ask where the data comes from; if the route doesn't exist, add it to the
backend + catalog first.

### Rule 16 — Never label code as AI-generated
No commit message, comment, changelog, or doc string may attribute work to
an AI or agent. Same as JEOS Rule 17 — a hard product requirement.

### Rule 17 — Bun for all TypeScript/frontend tooling
`bun`, `bunx`, `bun run`, `bun test`, `bun build`. Never npm/npx/pnpm/yarn.
Go for the backend; `gofmt -l` + `go vet` + `go test ./...` clean before
commit. Never hand-edit `internal/db/` (sqlc-generated) — edit the query
files, regenerate.

### Rule 18 — Commit to `main` only
Same convention as the JapaConnect repos: work commits directly to `main`,
never left uncommitted or parked on feature branches, unless the user
explicitly asks otherwise.

---

## Secrets & Safety (absolute)

- Never commit secrets, tokens, keys, or `.env` contents — `TU_*` vars only.
- Never run the server or any service as root in spec or code; no silent
  privilege elevation.
- Every privileged execution writes an audit row (actor, command, target,
  result).
- The overlay never auto-injects keystrokes — the human always sends
  (ADR-001 §Decision 5).
- Screen content is sensitive: it never leaves the deployment except via an
  explicit user-invoked LLM call.
- If three consecutive fix attempts fail, STOP — report what is known vs
  unknown and propose (a) revert, (b) a different approach.

---

## How to Reference TEOS from Code

```go
// Ref: .teos/sections/06-screen-interpreter-adapters.md
// Trigger gate: quiescence ~400ms, bottom-region dirty, on-demand
```

```svelte
<!-- Ref: .teos/sections/02-terminal-engine.md (Key bar) -->
```

---

## Agent Context Window Strategy

For a focused task, load:
1. `TEOS.md` — always
2. The roadmap phase you're in — always
3. The 1–3 section files your task references — always
4. Convention files the sections cite — when needed
5. The ADR — only if you need the *why*

Do not load all sections at once.

---

## What NOT to Do

| Do not | Because |
|---|---|
| Call an execution target outside the broker | Breaks the security substrate (ADR-004) |
| Scan screens per frame | The trigger gate is the cost model — busy panes are the least likely to be waiting |
| Promote a low-confidence wait | `unknown` shows "may be waiting" only — never buttons, never needs-you top rank |
| Auto-inject into a PTY | Human-sends-only is a hard contract |
| Invent adapter patterns without fixtures | Patterns are hypotheses until a real transcript verifies them |
| Invent env vars/routes/WS types/states | Catalogs are canonical — register first |
| Mix package managers | Bun for TS; go modules for Go |
| Write npm/`better-sqlite3`/`ws`/`express` | Bun stdlib + the pinned stack in `.ai/stack.yaml` |
| Ship an unregistered migration | MIGRATION-ORDER.md first |
| Add scope not in the roadmap | Rule 10 — deferred table or `ideas/` |

---

## Asking for Help

If a task is ambiguous after reading the section file, add a `// QUESTION:`
comment and flag the human. Do not guess on security boundaries, session
lifecycle semantics, or adapter confidence semantics.
