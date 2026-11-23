# RULES.md — Terminu Coding & Operational Rules

Hard rules for any agent or developer working on this codebase. Where a rule
conflicts with convenience, the rule wins. Pinned stack versions live in
`.ai/stack.yaml` — don't duplicate version numbers here.

## Important Note

- After any non-trivial change, give a 3-step manual test I can run myself
  (in the browser or with one command) — verify behavior, not just "it
  builds."
- Before touching files, list every file you plan to modify. If >5, stop
  and propose splitting the task.
- If three consecutive fix attempts fail, STOP: propose (a) revert,
  (b) what's known vs unknown, (c) a different approach.

## Never Do

**Never execute a command without the broker.** No `exec.Command`, no
`creack/pty` spawn, no SSH exec, no Docker API call outside
`broker.Execute()` — policy, confirmation, and audit live there.

**Never interpolate user input into a command string.** Structured argv
only — `executable` + `arguments[]`. No `format!("rm -rf {path}")` anywhere,
ever.

**Never let a client attach kill or duplicate a session.** Attach/detach are
idempotent view operations. A second attach resyncs the same PTY; it never
spawns a second shell (Section 03).

**Never auto-send keystrokes from detected prompts.** Overlay buttons
propose; the human's tap sends. `low`-confidence detections render no
buttons at all.

**Never scan the screen model per frame.** Detection runs only through the
trigger gate (quiescence, bottom-region dirty, cursor-to-input, on-demand).
A change that adds per-frame detection is a budget violation.

**Never return a fabricated option.** Adapter prompts must extract options
from real screen content — `label` + `inject` both come from what the tool
actually printed. `unknown` waits get no buttons.

**Never store secrets in the database, logs, session metadata, or exports.**
Credentials live in the encrypted secret store / OS vault (desktop). Audit
and logs get redacted forms.

**Never ship an adapter pattern untested against a real transcript.** Every
pattern is a hypothesis until a `.cast` fixture proves it — then it's a
regression test.

**Never run Terminu components as root** in container spec, docs, or code.
No silent setuid, no privileged ports, no host mounts beyond declared
workspace roots.

**Never make the LLM path load-bearing.** The product works fully with no
LLM configured; LLM output never bypasses the risk gate; screen content
never leaves the deployment without an explicit user action.

**Never let a session's scrollback/state grow unbounded.** Per-session
buffer caps; SQLite session rows carry a byte budget.

**Never mix package managers or add unvetted deps.** Bun for TypeScript
(`bun`/`bunx`), go modules for Go. A new dependency needs a note in the PR
justifying it.

**Never label code as AI-generated.** No agent attribution anywhere in the
tree or history.

**Never use `localStorage`/`sessionStorage`** in frontend code paths.

**Never expose the Docker socket unauthenticated** — the Docker pane talks
to the Engine API through the broker with its own permission gate.

**Never block a terminal WS write on a slow client.** Per-client backpressure
with bounded buffers; drop frames for a lagging client before stalling the
PTY read loop.

## Always Do

- Always namespace env vars with `TU_`.
- Always run migrations as a distinct step before a new binary serves
  traffic; breaking changes use two-phase deploys.
- Always emit the session-state transition events on state change — the
  "needs you" view and alerts consume them; nothing else polls.
- Always record `evidence` on every adapter match — debugging a reskin
  depends on knowing what fired.
- Always version-pin adapters (`AdapterPin`) and degrade to "unverified"
  badge on mismatch — never silence.
- Always validate file-manager paths server-side (traversal, symlink
  escapes, declared workspace roots).
- Always bound WS frames (size limit, origin allowlist, per-message authz).
- Always `bunx svelte-check` + `go vet` + `gofmt -l` + `go test ./...`
  before commit.
- Always write the post-implementation doc for a phase into
  `documentations/` and the reusable pattern into `skills/` when a phase
  closes.
- Always add a `.teos` reference comment when implementing a section's
  feature (`// Ref: .teos/sections/NN-*.md`).
- Always fix root causes — never patch symptoms with quick hacks.
- Always stop and ask on security boundaries, session semantics, or
  ambiguity — `// QUESTION:` + flag the human.

## PR / Commit Discipline

- Reference the owning `sections/NN-*.md` in the commit/PR when a change
  touches that scope.
- If a change introduces a pattern not in `skills/`, add the skill file.
- If a change makes an architectural decision not in an ADR, add the ADR.
- If a change adds a catalog entry (env var, route, WS type, state, error
  code), the catalog update is part of the same change.
- If a fix addresses a non-obvious bug, note root cause in the commit body.
- Commit to `main`; never leave work uncommitted or parked on branches.
