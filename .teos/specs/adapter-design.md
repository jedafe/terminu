# Terminu — Screen Interpreter & Agent Adapter Design

**Document type:** Engineering design — Phase 2 implementation spec
**Implements:** product brief v2, §5 (Command Intelligence) and §6 (Agent
Session Management)
**Status:** Design draft — per-tool patterns are hypotheses pending
fixture capture (§8)
**Date:** 23 November 2026

------------------------------------------------------------------------

## 1. Purpose

Define how Terminu detects *what is running*, *what state it is in*,
and *what it needs* — and how detected prompts become actionable UI.

Two products ride this one subsystem:

- **Agent Session Management** (the wedge): state registry,
  "needs you" view, prompt→button overlays, alerts.
- **Command Intelligence context**: which CLI → which actions are
  relevant in this session right now.

------------------------------------------------------------------------

## 2. Detection pipeline

``` text
PTY output stream
      │
      ▼
Headless VT parser (per session) ──► Screen model (grid + attrs + cursor)
      │                                   ▲ incremental: dirty regions only
      ├──► Process-tree walk (periodic, ~1s)
      │        └──► "devin" runs in session S → bind adapter(devin)
      ▼
Trigger gate (§4) ── decides WHEN to run detection, not every frame
      ▼
Adapter.Detect(screen, ctx) ──► *StateMatch
      │
      ├──► state: working | waiting{kind} | done | error
      ├──► prompt: extracted options / question text
      └──► actions: [{label, keystroke}] → overlay UI
      ▼
State registry → notify on transitions → "needs you" view + alerts
```

Process binding is the routing key: the adapter for a session is chosen
by *which process* owns the PTY foreground group — not by screen
guessing. Screen guessing is the fallback when the process isn't a
known CLI (generic heuristics, §7).

------------------------------------------------------------------------

## 3. Screen model

- One headless VT emulator per session (`hinshun/vt10x` — detection-
  fidelity is sufficient; a `wazero` + ghostty-WASM spike could share the
  exact client parser server-side).
- Maintains: cell grid, cursor pos/style, scrollback tail (bounded),
  dirty-region set since last scan.
- Adapters read the **model**, never raw bytes — no ANSI parsing inside
  adapters.
- The visible-region model is what the user sees; prompt detection runs
  on the **bottom K rows** (default K=12) + the region above cursor —
  where TUIs put their prompts. Full-screen scans only for adapters
  that declare `scan_scope: full`.

------------------------------------------------------------------------

## 4. Trigger gate — the cost model

Detection must never run per frame. Run an adapter pass when ANY of:

| Trigger | Rationale |
|---------|-----------|
| Output quiescence (~400 ms silence after output) | Prompts are emitted then wait — silence is the signal |
| Bottom-region dirty + process is adapted | Cheap prefilter |
| Cursor moved to bottom line after activity | Input region reached |
| Client requests current state (view open) | On-demand refresh |
| Foreground process changed | Re-bind adapter, re-scan |

A quiesced session costs **zero** — the strongest property of this
design: idle panes are free.

------------------------------------------------------------------------

## 5. Adapter interface

``` go
type Adapter interface {
    // Process-name match, e.g. "devin", "claude".
    MatchesProcess(procName string, argv []string) bool

    // Inspect the screen model; return classified state + extraction.
    // Called only through the trigger gate — may be expensive-ish.
    Detect(screen *Screen, ctx *SessionCtx) *StateMatch

    // Version fingerprint this adapter was written against.
    // Reported in UI ("patterns: devin-cli ≥1.x, last verified <date>").
    Pin() AdapterPin
}

type StateMatch struct {
    State      SessionState   // working | waiting{kind} | done | error
    Confidence Confidence     // high | medium | low
    Prompt     *Prompt        // extracted question + options (nil if none)
    Region     Region         // where on screen it was found
    Evidence   string         // what matched — for debugging/UI tooltip
}

type Prompt struct {
    Kind    PromptKind      // options | confirm | permission | freetext
    Title   string          // "Do you want to proceed?"
    Options []PromptOption  // for options/confirm/permission
}

type PromptOption struct {
    Label   string      // "1. Yes"
    Inject  Keystroke   // what answering sends to the PTY
    Default bool
}

// Keystroke — one of the tagged variants:
//   KeyChar('y')             — single byte ('y', '1', 'a')
//   KeyText("org-name")      — free-text reply + Enter
//   KeyArrowSelect{Down: 2}  — arrows ×N + Enter (numbered TUIs)
//   KeyEscape                — cancel/deny
```

Rules for adapters:

1. **Pure functions over the screen model.** No I/O, no PTY access —
   testable against fixtures.
2. **Return `nil` when uncertain** — a false negative falls back to
   raw pane; a false positive (wrong buttons) is worse.
3. **`evidence` is mandatory** — every match records what fired, so the
   UI can show "detected: claude permission prompt" and debugging stays
   possible.
4. **Version-pinned per tool** (`AdapterPin`) — patterns verified
   against a recorded fixture set; a pin mismatch warns, still runs.

------------------------------------------------------------------------

## 6. State machine

``` text
         output streaming              quiescence + adapter wait-match
WORKING ──────────────► WORKING ──────────────────────────► WAITING
   ▲                                                        │
   │ input injected /                              input injected
   │ output resumes                                        ▼
   └────────────────────────────────────────────── WORKING (resume)
        process exits 0 ──► DONE
        process exits ≠0 or adapter error-match ──► ERROR
```

`WAITING` carries `kind`: `permission | option | confirm | freetext |
unknown`. `unknown` = heuristic-only wait (low confidence) — rendered
in UI as "may be waiting," never auto-promoted to the "needs you" view
at high priority.

Transition → alert rules (§6.2 of brief): `WORKING→WAITING` and
`*→DONE/ERROR` notify in-app.

------------------------------------------------------------------------

## 7. Per-tool adapter tables — initial hypotheses

**Every pattern below is a starting hypothesis written from known tool
behavior. Before implementation: capture real PTY transcripts per tool
(§8) and correct these tables. TUIs reskin without notice — these are
living documents, not specs of the tools.**

Column legend: *Signature* = what the process/screen looks like;
*Prompt* = what the blocking state looks like; *Inject* = what the
answer button sends.

### 7.1 claude (Claude Code)

| Aspect | Hypothesis |
|--------|------------|
| Process | `claude` / `node … claude` |
| Signature | Boxed bottom input region, status line with model/context info, `─` separators |
| Permission prompt | `Do you want to proceed?` + numbered options `1. Yes` / `2. Yes, and don't ask again for <cmd>` / `3. No` |
| Other prompts | Trust dialog (`Do you trust the files in this folder?`), plan approval (`Approve plan?`), MCP tool approval |
| Inject | Option number key `1`/`2`/`3`, or arrow-select + Enter; `Esc` = cancel → map to "No" |
| Drift risk | High — actively developed; option wording shifts between releases |

### 7.2 devin (Devin CLI)

| Aspect | Hypothesis |
|--------|------------|
| Process | `devin` |
| Signature | Interactive numbered option lists (org selection, project prompts), text questions |
| Option prompt | Numbered list `1) … 2) …` with selection cursor — observed in setup/auth flows |
| Free-text prompt | Bare `>` / label + cursor (e.g. org ID entry) → `PromptFreetext` |
| Inject | Option number, or arrow-select + Enter; free-text → Text + Enter |
| Drift risk | Medium — less churn than claude; fixture capture is essential (thinner public docs) |

### 7.3 codex (Codex CLI, OpenAI)

| Aspect | Hypothesis |
|--------|------------|
| Process | `codex` |
| Signature | TUI with bottom composer; approval overlay interrupts |
| Permission prompt | Exec-approval dialog — approve once / approve for session / deny (letter or arrow select) |
| Inject | Letter keys (`y`/`a`/`n` family) or arrow-select + Enter per option |
| Drift risk | High — codex UX iterating fast; expect per-minor-version pins |

### 7.4 gemini (Gemini CLI)

| Aspect | Hypothesis |
|--------|------------|
| Process | `gemini` / `node … gemini` |
| Signature | Bottom input box, status bar; confirmations appear as bordered prompts |
| Permission prompt | `Allow execution of: '<cmd>'?` — `1. Yes, allow once` / `2. Yes, allow always` / `3. No` (esc) |
| Other | "Apply this change?" confirmations on edits |
| Inject | Number keys `1`/`2`/`3`; `Esc` = deny |
| Drift risk | Medium-high |

### 7.5 opencode (OpenCode)

| Aspect | Hypothesis |
|--------|------------|
| Process | `opencode` |
| Signature | Full TUI (bubbletea-style layout); permission modal overlays center/bottom |
| Permission prompt | `Permission required` modal — tool name + command shown; `Allow` / `Always` / `Reject` options |
| Question prompt | `question` tool renders selectable option list |
| Inject | Arrow-select + Enter; option labels may need position mapping rather than keys |
| Drift risk | Medium-high — heavy TUI, most likely to need full-screen scan (`scan_scope: full`) |

### 7.6 aider

| Aspect | Hypothesis |
|--------|------------|
| Process | `aider` / `python … aider` |
| Signature | **Line-based, not TUI** — plain scrolling prompts; easiest adapter |
| Prompts | `Add file to the chat? (Y)es/(N)o/(D)on't ask again` / `Run shell command? (Y)es/(N)o/(A)ll` — literal trailing `(…)…(…)` tokens |
| Inject | Literal single char `y`/`n`/`a`/`d` + implicit Enter (line-mode) |
| Drift risk | **Low** — stable prompt strings; the reliable baseline adapter to build first |
| Note | Order matters: simplest adapter first proves the whole pipeline before tackling full TUIs |

### 7.7 Generic fallback (no adapter bound)

| Tier | Detection | Confidence |
|------|-----------|------------|
| G1 | Trailing `(y/n)`, `(Y)es/(N)o`, `[y/N]` token on bottom line | high-ish → `confirm` |
| G2 | Numbered/lettered list (`1.`/`a)`) in bottom region + cursor at end | medium → `option` (inject arrows) |
| G3 | `?` on bottom line + quiescence + cursor present | medium → `freetext` |
| G4 | Quiescence + cursor at bottom input line only | low → `waiting{unknown}` |

Generic matches render buttons from extracted options where possible;
G4 renders only "may be waiting" — never fabricated options.

------------------------------------------------------------------------

## 8. Fixture strategy — how adapters get written and stay correct

1. **Capture**: `asciinema`/raw PTY dump of each tool's real sessions —
   permission prompts, option lists, confirms, error exits — per tool
   version. Store as `fixtures/<tool>/<version>/<scenario>.cast`.
2. **Replay tests**: adapter test harness feeds the fixture through the
   same headless VT parser → asserts `Detect()` returns the expected
   `StateMatch`. Golden-file compare on extracted options.
3. **Pin CI**: a nightly/weekly job could re-run the newest tool
   versions through the capture harness (manual gate — licenses/
   accounts required); pin mismatch → issue filed, UI badge degrades to
   "unverified."
4. **Negative fixtures**: busy screens (spinners, diffs, scrollback)
   must return `nil` or `working` — false-positive cost is wrong
   buttons injected into a live session.

------------------------------------------------------------------------

## 9. Fallback & degradation contract

| Situation | Behavior |
|-----------|----------|
| Adapter detects nothing | Raw pane + key bar — today's UX, never worse |
| Adapter pin mismatches binary version | Run anyway + "unverified" badge |
| Detection lost mid-prompt (reskin) | Next trigger re-evaluates; stale buttons expire with the screen region |
| Generic-only match | `unknown` wait — demoted in "needs you" ordering |
| Screen parser failure | Session marked `undetectable`; registry shows process state only |

Never: injected keystrokes from a `low`-confidence match without user
tap — the overlay only *offers* buttons; the human always sends.

------------------------------------------------------------------------

## 10. Build order inside Phase 2

| Step | Delivers |
|------|----------|
| 1 | Headless screen model + process-tree binder |
| 2 | State registry + transition notifications (process-only states: running/exited) |
| 3 | **aider adapter + generic G1** — proves end-to-end with the easiest tool |
| 4 | Trigger gate + "needs you" view + prompt→button overlay UI |
| 5 | claude adapter (biggest user base, highest drift — forces fixture infra to mature early) |
| 6 | devin + codex |
| 7 | gemini + opencode (most complex TUI last) |
| 8 | Generic G2–G4 + LLM opt-in screen-reading hook (deferred-safe) |

Step 3 before 4 deliberately: pipeline plumbing proven on a line-based
tool before UI work compounds unknowns.

------------------------------------------------------------------------

## 11. Open questions

1. **Injection for arrow-only TUIs** — some prompts may ignore number
   keys; `ArrowSelect` needs cursor-position reading from the model to
   compute `down` count. Verify per tool.
2. **Multi-prompt TUIs** — opencode/claude can stack dialogs; adapter
   returns the *topmost* (nearest cursor) only.
3. **Half-duplex CLIs** — tools that redraw the whole prompt on state
   change may look perpetually dirty; tune quiescence window per
   adapter if needed.
4. **Remote targets** — process-tree walk over SSH needs a remote
   probe (`ps` scrape) rather than /proc reads; SSH sessions bind
   adapters with higher latency. Acceptable for v1; note it.
