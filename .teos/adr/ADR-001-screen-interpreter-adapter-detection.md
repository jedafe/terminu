# ADR-001 — Screen-Interpreter Adapter Architecture for CLI & Agent State Detection

**Status:** Accepted · **Date:** 2026-11-23 · **Implements:** Product brief v2 §5–§6; `specs/adapter-design.md`

## Decision

Terminu determines *which CLI is running* and *what state it is in* by parsing a
**headless VT screen model per session through version-pinned, per-tool adapters**,
with adapter selection driven by **process-tree binding** — not by agent APIs, not
by screen content guessing, and not by an LLM on the hot path. Detection runs only
through a **trigger gate** (output quiescence, bottom-region change, cursor-to-input,
on-demand) — never per frame. An LLM screen-reading path exists as opt-in fallback
for unadopted TUIs and is never on the deterministic path.

## Context

The product wedge — "the control room for your AI agents" — requires knowing when an
agent is `waiting-for-input` and rendering its prompt as actionable UI. That requires
answers to three questions per session: *what runs*, *what state*, *what does it need*.

The constraint that shapes everything: **the tools being managed are black-box CLIs
inside PTY streams.** They were not designed to be observed. Whatever mechanism reads
their state must work on what a terminal already produces: a byte stream plus a
process tree.

A failed attempt informs this: making arbitrary in-terminal TUIs generically
"clickable" is not feasible — prompts are terminal bytes, not semantic events. The
achievable version is *know what is on screen for known tools, and turn it into
buttons* — which is exactly what this architecture scopes.

## Analysis

### Candidate detection mechanisms

| Mechanism | How it works | Verdict |
|---|---|---|
| **Agent-internal APIs / SDKs** | Integrate each tool's session/state API | **Rejected as primary.** AI CLIs expose no stable programmatic state APIs; those that exist are undocumented and churn per release. Integrations would break silently and can't cover "any CLI." Kept as an adapter *supplement* where a tool happens to emit structured output. |
| **Screen parsing — per-tool adapters** (chosen) | Headless VT model + pattern adapters keyed to each tool's known prompt shapes | **Chosen.** Works on every CLI (the PTY stream is the universal interface); depth is curated where it matters; failures degrade to raw pane rather than breaking. |
| **Generic heuristics only** | `(y/n)` tokens, numbered lists, trailing `?` | **Retained as fallback, insufficient alone.** G1–G4 tiers produce useful coverage, but can't extract structured options reliably or distinguish `working` from `waiting` for full TUIs — the "needs you" view would be mostly `unknown` states. |
| **LLM screen interpretation** | Feed the screen model to a model → "what is this, what's it asking" | **Rejected on the hot path, kept opt-in.** Continuous invocation is unbounded cost; screen content is sensitive for a self-hosted product; latency adds seconds to a detection that must feel instant. Right-sized as user-triggered "what is this?" for unadopted TUIs. |
| **OS-level tracing (ptrace/inotify)** | Watch syscalls for read-blocked states | **Rejected.** Root/syscall-level access for marginal state gain; wrong portability story; "blocked on read" ≠ "waiting for *user* input" (agent mid-tool-call looks identical). |

### Sub-decision: how the adapter is chosen for a session

| Approach | Verdict |
|---|---|
| **Process-tree binding** — PTY foreground process name → adapter | **Chosen.** "devin runs in this pane" is a syscall — free, reliable, universal. Screen content can't identify the app nearly as cheaply. |
| Screen signature guessing | Fallback only — when the process isn't a known CLI, generic tiers apply. |

### Sub-decision: when detection runs

| Approach | Verdict |
|---|---|
| Per-frame / on-every-output scan | **Rejected.** Full-TUI tools redraw constantly — cost scales with output rate; a busy pane is the *least* likely to be waiting. |
| **Trigger gate** — quiescence (~400 ms silence), bottom-region dirty, cursor-to-input, on-demand | **Chosen.** Prompts are emitted then *wait* — silence is the signal. Idle panes cost zero; busy panes are scanned only at pauses. This is what keeps the 50-session/15 MB budgets honest. |

### Sub-decision: adapter correctness over time

| Approach | Verdict |
|---|---|
| Hand-maintained regex in code | **Rejected** — untestable drift. |
| **Version-pinned adapters + golden fixtures** (`.cast` transcripts per tool per version, replay-tested) | **Chosen.** TUIs reskin without notice; the pin says which version patterns were verified against, fixtures make updates regression-tested, and pin-mismatch degrades to a badge — not silence. |

## Decision (full)

Adopt the **Screen Interpreter + adapter** architecture as specified in
`specs/adapter-design.md`:

1. **Screen model:** one headless VT emulator per session (grid + attrs + cursor +
   bounded scrollback + dirty-region tracking). Adapters read the model, never bytes.
2. **Binding:** PTY foreground process → `MatchesProcess()` → adapter. Unknown
   processes fall to generic tiers G1–G4.
3. **Adapter contract:** pure functions over the screen model; `Detect()` returns
   `*StateMatch`; `nil` on uncertainty (false-negative is graceful, false-
   positive injects wrong buttons); mandatory `evidence` string; `AdapterPin` declares
   verified-version coverage.
4. **States:** `working | waiting{permission|option|confirm|freetext|unknown} |
   done | error`, each carrying `Confidence`. `unknown` waits are demoted in the
   "needs you" view.
5. **Injection:** detected options render as buttons that send the option's
   keystroke (char / arrows+Enter / free-text / Escape). **The human always sends —
   the overlay never auto-injects.**
6. **Cost:** trigger gate only; bottom-K-row scan by default; `scan_scope: full`
   opt-in per adapter (opencode).
7. **Launch coverage:** adapters for all six target AI CLIs at v1 launch — devin,
   claude, codex, gemini, opencode, aider — built in the design doc's order (aider
   first: line-based, proves the pipeline; opencode last: heaviest TUI).
8. **LLM:** opt-in only, user-supplied key or local model, on-demand invocation —
   and LLM-suggested commands pass the same execution-broker risk gate as everything.

## When this would be revisited

- **AI CLIs ship stable state APIs.** If two or more of the six expose documented,
  versioned session/state interfaces, adapters become hybrid (API-primary, screen-
  fallback) — revisit the screen-primary stance, not the registry/UI built on it.
- **Adapter maintenance burden proves unsustainable.** If keeping six adapters
  current consumes outsized effort, the fallback ladder already absorbs load —
  the revisit would weigh *dropping to generic+LLM* versus funding a community
  adapter program (which interacts with the deferred plugin system).
- **Remote-target detection latency is unacceptable.** SSH sessions can't walk
  /proc — they scrape `ps`, adding latency. If remote "needs you" latency becomes
  a support burden, this is the earliest concrete argument for the deferred
  Terminu Agent daemon (brief §13).
- **A tool redesigns its TUI fundamentally** (e.g., moves to a GUI/web UI). Its
  adapter is retired or rewritten — per-tool, never the architecture.
- **False-positive rate in the field is meaningful.** If wrong buttons appear at
  non-trivial rates, tighten confidence thresholds or drop generic tiers — the
  contract already routes `nil` to raw pane, so the fix is calibration, not
  architecture.

## Consequences

**Positive.** Universal process+screen awareness for every CLI, free; curated depth
exactly where the market cares (the six AI CLIs); detection cost decoupled from
output rate; graceful degradation at every tier; a fixture/test story that makes
"living adapters" an engineering discipline rather than a hope; the same screen
model powers both the wedge feature and the touch layer — one investment, two
differentiators.

**Negative / accepted.** Adapter maintenance is a permanent, ongoing cost — the
six tools reskin on their own release cadence, and each pattern must be re-verified.
Coverage honesty caps marketing ("universal detection, curated depth" — never
"aware of any CLI"). Remote-target process binding is slower than local. The
design buys correctness by refusing false confidence — `unknown` waits will exist
in the UI, and that restraint is the price of the buttons being trustworthy.

## See Also

`specs/product-brief-v2.md` §5 (Command Intelligence — the four levels of
awareness this ADR implements) and §6 (Agent Session Management — the consumer);
`specs/adapter-design.md` (full engineering spec: pipeline, interface, per-tool
pattern tables, fixture strategy, build order, open questions).
