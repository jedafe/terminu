# Terminu — Product Vision, Architecture & Roadmap (v2)

**Document type:** Product and engineering architecture
**Version:** 2.0 — revised scope
**Date:** 23 November 2026
**Status:** Supersedes v1; scope reduced to a sellable wedge

------------------------------------------------------------------------

## 1. What Terminu is

Terminu is a **self-hosted web platform for managing terminals, CLI
applications and AI coding agents** — a control room for CLI-native work.

It is not another terminal emulator. It is a management layer that sits
on top of real terminal sessions: it keeps them alive, watches them,
understands what is running inside them, and turns that understanding
into actionable UI.

### The wedge

**"The control room for your AI agents."**

Developers increasingly run multiple AI coding agents — Claude Code,
Devin, Codex, Gemini CLI, OpenCode, Aider — across panes, windows and
machines. Every one of these agents periodically *blocks*: permission
prompts, question dialogs, plan approvals, free-text questions. Today
there is no way to know which agent needs you without watching every
pane. Terminu's first differentiator: **a dashboard that watches your
agents and tells you when they need you** — and lets you answer from a
browser, including a phone.

The second differentiator: **touch-native terminal UX**. No existing
web terminal treats mobile interaction as first-class. Terminu does.

### Who it is for

1. **Primary:** developers running AI coding agents on remote machines —
   self-hosted VPSes, dev boxes, homelabs.
2. **Secondary:** self-hosters and ops-minded developers managing one to
   twenty servers who want a terminal + file manager + light
   infrastructure view without enterprise tooling.

### What it is not

- Not a terminal emulator — xterm.js renders; Terminu manages.
- Not an IDE — no editor, no debugger.
- Not an enterprise fleet platform — no agent daemons, orgs or fleets
  in v1 (see Deferred).
- Not a desktop app in v1 — the desktop edition is a later wrapper
  around the same web core.

------------------------------------------------------------------------

## 2. Design principles

1. **Sessions, not windows.** The unit of persistence is the running
   session, not the visual layout. A disconnect must never kill work.
2. **The session is server-side.** The PTY and its process tree belong
   to Terminu Server. Clients attach and detach; nothing is lost.
3. **Universal detection, curated depth.** Every CLI gets process and
   screen awareness for free. Deep interpretation is curated per tool —
   never promised for "any CLI."
4. **AI enhances, never gates.** Deterministic discovery and adapters
   provide the always-on path. LLM features are opt-in, scoped, and
   never bypass the execution policy broker.
5. **Security is a substrate, not a feature.** Every privileged action
   passes the execution broker — permission, policy, confirmation,
   audit.
6. **Lightweight by budget.** Single container, SQLite, no required
   external services, <150 MB idle RAM. Resource ceiling is a spec, not
   a hope.
7. **Touch is first-class.** Tap targets, gestures, key bar, clipboard —
   designed for a phone, not adapted to one.

------------------------------------------------------------------------

## 3. Problem statements the product must solve

These are observed failure modes of the current generation of web
terminals (ttyd/wetty/tmux-over-WS). Each maps to a hard requirement.

| # | Problem | Requirement |
|---|---------|-------------|
| P1 | In-terminal TUIs are unreachable on touch — prompts expect arrow keys/Esc that phone keyboards lack | R1: On-screen key bar (Esc, Ctrl, Alt, arrows, Tab, Fn) + prompt-to-button overlays for known tools |
| P2 | Terminal mouse reporting conflicts with browser selection — users can't select/copy | R2: OSC52 → device clipboard bridge, select-on-drag copies, explicit paste button |
| P3 | Network drops kill the client and orphan the display | R3: Client auto-reconnect that *resyncs* an existing session — never spawns duplicates |
| P4 | Agents block on input with no way to know | R4: Session state registry — `working / waiting-for-input / done / error` — with the waiting prompt surfaced as actionable UI |
| P5 | Web terminals ignore touch gestures entirely | R5: Touch layer — tap→mouse click, two-finger scroll, long-press→right-click, ≥44px targets |
| P6 | Admin tooling should not run always-on | R6: Designed to run stopped-by-default behind an admin gate; minimal always-on surface |
| P7 | "Management" tools weigh hundreds of MB | R7: <150 MB idle, single binary/container |

------------------------------------------------------------------------

## 4. Architecture

### 4.1 Shape

**Modular monolith — one server, one web UI, SSH targets.**

``` text
Browser (SvelteKit web UI)
        |
  HTTPS + WSS (authenticated)
        |
Terminu Server (Go)
  ├── Session Manager ........... owns PTYs; attach/detach/resync
  ├── Command Intelligence ...... discovery, registry, context, policy
  ├── Screen Interpreter ........ headless terminal state extraction
  ├── Agent Session Manager ..... agent registry, prompt detection
  ├── File Service .............. storage-provider abstraction
  ├── Connection Manager ........ SSH targets, credentials
  ├── Execution Broker .......... risk policy, confirmation, audit
  └── SQLite .................... workspaces, sessions, registry, audit
        |
Execution targets:
  ├── Local host (PTY via creack/pty)
  └── Remote hosts via SSH (no agent daemon in v1)
```

The web frontend never gets direct OS access. Every privileged
operation — terminal spawn, command execution, file write, Docker
action — flows through the authenticated execution broker.

### 4.2 Editions (sequenced, not parallel)

- **Terminu Web (v1):** the product. Self-hosted, single container.
- **Terminu Desktop (post-1.0):** Tauri shell + the *same* shared Go core
  linked as a sidecar for local PTY — thick wrapper, not a second
  codebase. Native surface: OS credential vault (keychain/DPAPI/Secret
  Service), global hotkey, `terminu://` deep links, tray + menu bar,
  native notifications, native file dialogs, signed auto-updates,
  single-instance + window-state restore, offline local terminals.
  Honest boundary: the UI is still the Svelte app in a system webview —
  same model as Tabby/Wave/Termius. Only Warp is truly pixel-native
  (custom GPU renderer), at a cost this product should not pay.
- **Terminu Agent (deferred):** remote daemon for richer multi-node
  management. Built only if SSH-only proves insufficient at scale.

### 4.3 Shared Go core

Domain packages are independent of HTTP/Svelte: domain models, session
lifecycle, command registry, execution abstraction, storage providers,
policy engine (`internal/domain` + siblings). The desktop edition reuses
them behind a different host adapter — a Go sidecar under the Tauri shell
(ADR-005). This is what makes "thick wrapper" honest.

### 4.4 Execution targets

``` text
ExecutionTarget = LocalHost | SSHConnection | DockerContainer | RemoteWorkspace
```

Every terminal, command, file op and Docker action carries its target.
The resolver hides connection complexity from the user.

------------------------------------------------------------------------

## 5. Command Intelligence Engine — the differentiator

Terminu's defining subsystem: it knows *what is running and what it can
do*.

### 5.1 The four levels of awareness — the honest spec

| Level | Meaning | Coverage |
|-------|---------|----------|
| **Process** | Which CLI process runs in a session (PTY process-tree walk) | **Universal** — every CLI, free |
| **Screen** | The rendered terminal state (headless VT emulation of the PTY stream) | **Universal** — every CLI, free |
| **State** | What the app is *doing* — working / waiting-for-input / done / error | **Curated** — per known TUI, best-effort otherwise |
| **Semantic** | What commands/options *mean* and what is safe | **Curated** — registry manifests + LLM assist |

Do not market "aware of any CLI." Market **universal detection +
curated depth for the CLIs that matter** — AI agents first (devin,
claude, codex, gemini, opencode, aider), then the developer core (git,
docker, go, node/bun, python, postgres, aws, kubectl, system).

### 5.2 Registry manifests

Commands are structured manifests, not frontend strings:

``` yaml
id: docker.container.logs
application: docker
name: View Container Logs
parameters:
  - name: container
    type: resource
    resource: docker.container
    required: true
command:
  executable: docker
  arguments: [logs, "{container.id}"]
execution:
  risk: controlled
  confirmation: true
```

Sources: built-in curated definitions + auto-discovery (`--help`/completion
parsing) + custom user manifests. Never string-interpolate user input —
executable + structured argv only.

### 5.3 The Screen Interpreter

A headless VT-emulated model of each session's visible screen, shared
by three consumers:

- **State detection** — adapters pattern-match known TUI states
  (prompt shapes, spinners, permission dialogs).
- **Prompt-to-button overlays** — detected prompts render as tappable
  UI that injects the corresponding keystroke.
- **LLM screen-reading (opt-in)** — for unadopted TUIs, an on-demand
  "what is this?" interpretation. Never continuous — invocation is
  user-triggered or state-transition-triggered to bound cost.

This subsystem is the same investment as the touch layer: both are
"know what is on screen, turn it into buttons."

### 5.4 LLM integration policy

- Opt-in only; user supplies provider/key or local model.
- Terminal contents are sensitive — screen content never leaves the
  deployment without explicit user action.
- LLM-suggested commands pass the same risk-gating as curated ones.
- Core product works fully without any LLM configured.

------------------------------------------------------------------------

## 6. Agent Session Management — the killer feature

The wedge in product form. For AI coding CLIs, Terminu tracks not just
*that* an agent runs, but *what it needs*.

### 6.1 Session states

``` text
RUNNING
   ├── working          (output streaming, agent busy)
   ├── waiting-for-input
   │     ├── permission prompt   ("Allow this tool?")
   │     ├── option prompt        (numbered/lettered choices)
   │     ├── confirmation         (y/n)
   │     └── free-text question
   ├── done               (exited cleanly)
   └── error              (nonzero exit / detected error state)
```

### 6.2 v1 capabilities

- **Agent registry** — detected agents across all sessions and targets,
  each with live state.
- **"Needs you" view** — agents in `waiting-for-input` rise to the top
  with the actual prompt rendered as buttons (option chips → tap to
  send the keystroke; y/n → tap; free-text → inline reply field).
- **State-change alerts** — notify on transitions to waiting/done/error
  (in-app; push/email deferred).
- **Launch profiles** — working dir, flags, env, resume command.
- **Adapter-per-tool detection** — all six target AI CLIs ship at
  launch (devin, claude, codex, gemini, opencode, aider) — not a phased
  subset; each adapter is version-pinned to known output patterns and
  falls back gracefully when a tool reskins.
- **Graceful degradation** — detection loss falls back to raw pane +
  key bar, never a broken screen.

### 6.3 Explicit non-goals for v1

- No agent-internal APIs — CLIs have no stable session APIs; adapters
  are screen-parsers, not integrations.
- No orchestration — Terminu shows state and relays input; it does not
  drive agents autonomously.
- No cross-agent task graphs.

------------------------------------------------------------------------

## 7. Terminal engine

- **Renderer:** xterm.js (mature). ghostty-web evaluated as alternative
  (WASM Ghostty parser; better grapheme/complex-script correctness —
  swap is an interface decision, not a rewrite).
- **PTY:** `creack/pty`.
- **Persistence:** Session Manager owns the PTY server-side (headless-
  multiplexer model). Tabs/splits are views onto sessions.
- **Reconnect contract (P3):** WS drop → client retries → resync to the
  *same* session with scrollback tail. Duplicate attach never spawns a
  second process. Explicit detached-indicator when a session can't
  resume.
- **Touch layer (P5):** touch→mouse translation, two-finger scroll,
  long-press→right-click, ≥44px targets everywhere Terminu's chrome is
  clickable (tabs, panes, sidebar items).
- **Key bar (P1):** Esc/Ctrl/Alt/arrows/Tab/Fn overlay — collapsible,
  per-profile configurable.
- **Clipboard (P2):** OSC52 → device clipboard, select-to-copy on
  mouse-up, dedicated paste button, browser permission prompt handled
  once.
- **Multi-session:** tabs + splits as UI; arbitrary named sessions.
- **Search** across buffer; per-session scrollback limits.

------------------------------------------------------------------------

## 8. File manager

Storage-provider abstraction, three providers in v1:

| Provider | Why |
|----------|-----|
| Local filesystem | The managed host itself |
| S3 + S3-compatible | Object storage (e.g. SeaweedFS) — configurable endpoint |
| SFTP | Remote files over the existing SSH layer |

Capabilities: tree/list views, upload/download queues with resume,
preview, remote edit-in-terminal, bookmarks, transfer history.
WebDAV/SCP post-1.0.

**SFTP protocol endpoint (ADR-006):** the same providers are exposed over
an SFTP server (per-user auth, gate-toggled) so native SFTP clients keep
protocol access — this is what retires SFTPGo-class tooling entirely, not
just its web UI. Combined with §12's host-access modes, Terminu subsumes
both the ttyd host terminal and SFTPGo file manager it replaces.

------------------------------------------------------------------------

## 9. Infrastructure view (kept thin)

- **SSH connection profiles** — key/agent/password auth, host
  verification, OS-vault-stored credentials.
- **Server health** — CPU/RAM/disk/load/processes. No port manager, no
  env manager, no scheduled-task manager.
- **Docker pane** — list containers/images, logs, exec shell (a
  terminal into the container), start/stop/restart. Docker Engine API;
  never expose an unauthenticated socket. No compose editor.

------------------------------------------------------------------------

## 10. Security

### 10.1 Execution broker — the only path

``` text
Authenticated user → API → identity → policy evaluation →
confirmation (per risk) → execution broker → target
```

### 10.2 Risk model

| Mode | Example | Behavior |
|------|---------|----------|
| Informational | `docker ps` | Immediate |
| Controlled | `docker restart` | Policy or confirm |
| Confirmed | `docker volume rm` | Explicit confirmation |
| Restricted | `rm -rf /` | Blocked by default |

Risk evaluation considers: user role, target, command, argv, working
dir, environment profile, command risk class. LLM-originated commands
inherit the same gate.

### 10.3 Hard rules

1. Structured argv, never string interpolation.
2. Server runs non-root; no silent elevation.
3. A general-purpose shell = arbitrary code as the user — documented,
  gated, never hidden behind UI.
4. Secrets: OS vault (desktop) / encrypted store (server); never in
  logs, exports, or session metadata.
5. WS per-message authz + origin allowlist + size limits + backpressure.
6. Audit every privileged execution (actor, command, target, result).
7. Secret redaction in logs and audit where detectable.

### 10.4 Operational model (P6)

Single-admin by default in v1 (user accounts + admin/operator/viewer
roles for small teams). Designed to be toggled off — the service holds
no always-on requirement, matching the gate-style deployment model:
start when needed, stop when done.

------------------------------------------------------------------------

## 11. Tech stack

| Layer | Choice | Why |
|-------|--------|-----|
| Core | Go | Concurrency, agent velocity (ADR-005) |
| API | net/http + coder/websocket | HTTP + WS |
| Frontend | SvelteKit + Svelte 5 | Reactive, light |
| Terminal | xterm.js | Mature (ghostty-web alt.) |
| PTY | creack/pty | Cross-platform |
| Screen interp. | vt10x; wazero+ghostty-WASM spike | Screen state |
| DB | SQLite (WAL) — committed from day 1; no Postgres migration planned | Zero-dep, single file, single-node scope |
| SSH | x/crypto/ssh | Remote targets |
| Docker | Engine API | Native management |
| LLM | Provider-agnostic interface, opt-in | Screen reading, suggestions |
| Deploy | Single container / single binary | Weight budget |

Not in v1 stack: PostgreSQL (SQLite suffices single-node), Redis,
Protobuf agent protocol, WASM plugin runtime, Tauri — all deferred
with their features.

------------------------------------------------------------------------

## 12. Roadmap — revised

| Phase | Scope | Weeks |
|-------|-------|-------|
| **0. Foundation** | Module, package boundaries, contracts, CI, session model | 2–3 |
| **1. Web terminal core** | Server, WS transport, persistent sessions, tabs/splits, **touch layer + key bar + clipboard + reconnect** | 4–5 |
| **2. Command Intelligence + Agent sessions** | Process/screen awareness, Screen Interpreter, registry v1, agent adapters, **"needs you" view + prompt buttons + alerts** | 4–5 |
| **3. Files + SSH** | Local/S3/SFTP providers, SSH profiles, remote targets | 3–4 |
| **4. Team + infra** | Users/roles lite, Docker pane, server health, audit UI | 3 |
| **Post-1.0** | Desktop wrapper → LLM polish → OIDC/orgs → agent daemon → plugins → workflows → marketplace | — |

Phases 1–3 are the wedge: touch-native terminal + agent control room +
files. Estimated 14–18 weeks to a genuinely demoable v1 with a small
focused effort — front-loaded with what nobody else has, not with what
incumbents already do better.

### MVP (public v1)

- Web terminal: persistent sessions, reconnect-resume, tabs/splits
- Touch layer: key bar, gestures, clipboard bridge
- Agent sessions: registry, states, prompt-buttons, alerts — all six
  target AI CLIs (devin, claude, codex, gemini, opencode, aider)
- Command Intelligence: discovery + curated registry + command palette
  (fuzzy search) + context actions
- Files: local + S3 + SFTP + SFTP protocol endpoint (ADR-006)
- SSH targets; server health; Docker pane
- Single-admin + lite roles; session auth; audit log
- One container; SQLite; <150 MB idle

------------------------------------------------------------------------

## 13. Deferred — explicit, with rationale

The scope fear is justified. These are deliberately *not* v1 — each
re-entered only via a phase or a validated need.

| Deferred | Why not v1 |
|----------|------------|
| Desktop app (Tauri) | Web proves the UX first; desktop is a wrapper around the same core, built cheaper later |
| Agent daemon / multi-node | SSH covers remote targets; the daemon is the hardest single component |
| Plugin platform / WASM sandbox / marketplace | Internal adapters now; a plugin SDK is a product of its own |
| Orgs / OIDC / full RBAC | Admin + 3 roles covers self-host teams; orgs are enterprise pull |
| Workflow engine / automations | Command favorites covers 80% of the value |
| Database / port / env managers | Three separate products |
| Cross-device workspace sync | One deployment, no sync problem |
| AI agent orchestration / APIs | Screen-parsers now; agent APIs are unstable anyway |
| Notifications service | In-app alerts first; push/email later |
| Multi-server horizontal scale | Single-node + SQLite until demand proves otherwise |

Governance rule: **any feature request must map to a numbered phase or
this table.** Un-mappable ideas don't enter the codebase.

------------------------------------------------------------------------

## 14. Performance budgets (hard)

| Metric | Target |
|--------|--------|
| Idle memory (server) | < 150 MB |
| Additional per-session | < 15 MB |
| Cold start | < 3 s |
| Command search | < 100 ms |
| Terminal input latency (LAN) | < 50 ms |
| Reconnect resync | < 3 s to interactive |
| Concurrent sessions | 50+ per node |

------------------------------------------------------------------------

## 15. Risks

| Risk | Mitigation |
|------|------------|
| **TUI parsing fragility** — agent CLIs reskin their TUIs | Adapters version-pinned; graceful fallback to raw pane; adapter updates shipped independent of releases |
| **AI CLI instability** — no stable APIs | By design: screen-parsing, not integration. Track each tool's release notes; treat as living adapters |
| **"Any CLI" overpromise** | Marketing = universal *detection*, curated *depth*. Never claim generic screen semantics |
| **LLM cost/privacy** | Opt-in only, on-demand invocation, self-host/local-model path, never on the deterministic path |
| **Distribution > build** | The hard problem is switching users, not architecture — see §16 |
| **Scope creep (meta-risk)** | The phase/deferred table in §13 is the only admission path |
| **WebKitGTK (desktop, later)** | Budget a Linux-webview QA pass when desktop lands |

------------------------------------------------------------------------

## 16. Distribution & positioning — who switches, and why

The v1 brief had 2,200 lines of how-to-build and two paragraphs on
who-for. That ratio is inverted for a product. The wedge:

- **Pain story (AI agent users):** "You run 3–5 agents across panes and
  machines. They block on input constantly. You can't tell without
  watching. Terminu shows you which one needs you — and lets you answer
  from your phone." Nobody else does this.
- **Adjacent story (self-hosters):** "tmux/screen + SFTPGo + a web
  terminal, unified, touch-friendly, light" — Terminu retires that whole
  stack (ADR-006): terminal + files + SFTP endpoint + server-owned
  sessions in one deployment.
- **What they abandon:** raw SSH + tmux (works but blind + hostile on
  mobile), code-server (heavy, IDE-shaped, not CLI-management),
  Tabby/Termius (desktop-first, no agent awareness, weak web),
  Guacamole (enterprise-shaped).

First-100-users path: dogfood on own infra (the AI-agent workflow is
already real), demo the "needs you" view to agent-using developers —
the wedge demo is *"watch this agent block, see it flagged, answer it
from a phone."*

------------------------------------------------------------------------

## 17. What changed from v1, and why

- **Desktop deferred to post-1.0 thick wrapper** — web validates the
  UX; Tauri wraps the same core later (not parallel development).
- **Agent daemon, plugins, orgs, workflows, managers — deferred table.**
- **Command Intelligence + Agent sessions promoted to Phase 2** — the
  wedge, built right after the terminal core that powers it.
- **Mobile/touch promoted to first-class requirements** — v1 doc had
  zero mobile mentions in a "UX-first" brief; these are now the
  differentiator (P1–P5).
- **Awareness decomposed into four levels** — honest spec: universal
  detection, curated interpretation, LLM opt-in.
- **Weight/gating as hard budgets** — not aspirations.
- **Positioning section added** — product, not just architecture.
- **v2.1:** Backend language switched Rust → Go (ADR-005) — the product
  is I/O plumbing (PTY↔WS fan-out, SSH, providers) which is Go's shape;
  goroutines beat async ceremony here, agent codegen velocity is higher,
  and the owner's production conventions are already Go. Architecture
  unchanged — boundaries expressed as packages, not crates.

*This revision keeps the strong bones of v1 — shared core, execution
broker, sessions-not-windows, manifest registry, security substrate —
while cutting roughly half the scope and aiming the remainder at the
one market position nobody occupies.*
