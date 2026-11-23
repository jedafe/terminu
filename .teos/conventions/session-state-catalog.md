# Session State Catalog

Canonical session/agent states (AGENTS.md Rule 11). These names are the
contract across `SessionState`, the registry, WS `state` frames, and the UI.

## Lifecycle states

| State | Meaning | Source |
|-------|---------|--------|
| `starting` | PTY spawning, no output yet | Session Manager |
| `working` | running; output streaming / agent busy | adapter `working` or default live state |
| `waiting-for-input` | blocked awaiting user; **carries `kind`** | adapter detection |
| `done` | exited code 0 | process exit |
| `error` | exited non-zero or adapter error-match | process exit / adapter |
| `detached` | no client attached beyond `TU_RECONNECT_WINDOW_S` | Session Manager |
| `ended` | process lost on server restart — record kept, honest not fake-resumed | Session Manager |
| `undetectable` | screen model failed; process state only | Screen Interpreter |

## `waiting-for-input` kinds

| Kind | Meaning | Overlay |
|------|---------|---------|
| `permission` | tool asking to allow an action (e.g. "Allow this tool?") | option buttons |
| `option` | numbered/lettered choices | option buttons |
| `confirm` | y/n / confirmation | option buttons |
| `freetext` | open question expecting typed reply | inline reply field |
| `unknown` | heuristic-only wait (low confidence) | none — "may be waiting" |

## Confidence levels

| Level | Meaning | UI treatment |
|-------|---------|--------------|
| `high` | adapter pattern matched, evidence recorded | full: state + buttons + needs-you top rank |
| `medium` | generic tier G1–G3 matched | state shown; buttons if options extracted |
| `low` | G4 only (quiescence + cursor) | "may be waiting"; demoted; no buttons |

## Transitions that notify

`working→waiting-for-input`, `*→done`, `*→error` — emitted as `agent.state`
+ `alert` on `/ws/events` (AGENTS.md: emit, never poll).

Rules: state/kind names are exactly these strings; a new state needs this
table updated first, then the enum.
