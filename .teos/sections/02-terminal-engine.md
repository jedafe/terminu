# Section 02 — Terminal Engine

Owns: terminal rendering, PTY data plane, touch layer, key bar, clipboard,
reconnect, tabs/splits. Solves P1, P2, P3, P5 of the brief.

## Renderer

- xterm.js (FitAddon, WebGL addon, unicode11). Swap boundary is the
  `TerminalSurface` component — ghostty-web may replace internals without
  touching session/transport code.
- No other terminal renderer without an ADR.

## Data plane

- `WS /ws/session/{id}` per `conventions/websocket-message-catalog.md`:
  binary `data`/`input` frames, JSON control. Per-client bounded buffers;
  a lagging client drops frames, never stalls the PTY read loop (RULES).
- Broker gates *attach* (who may open the socket), not the byte stream —
  see ADR-004 consequence note.

## Touch layer (P5)

- Pointer-event translation: tap → mouse click; drag → selection or TUI
  mouse-reporting (mode-aware); two-finger drag → scroll wheel events;
  long-press → right-click.
- ≥44px tap targets on all Terminu chrome (tabs, panes, sidebar, key bar).
- When mouse reporting is active in the TUI, a mode toggle switches touch
  between "TUI mouse" and "browser select" — the conflict is surfaced, not
  silently wrong.

## Key bar (P1)

- Collapsible overlay strip: Esc · Ctrl (sticky-toggle) · Alt · arrows ·
  Tab · Fn page · common sequences.
- Per-profile layout config; state persists per user.
- Sends via the same `input` frame path as physical keys — no special
  channel.

## Clipboard (P2)

- OSC52 sequences in PTY output → device clipboard (bridge runs before
  renderer consumes them; emit `clipboard.copy` as a browser CustomEvent —
  client-internal, not a WS type).
- Select-to-copy on mouse/touch release (where the TUI isn't holding mouse
  capture); dedicated paste button → `navigator.clipboard.readText` →
  bracketed-paste into PTY.
- Clipboard permission prompt handled once per session.

## Reconnect contract (P3)

- WS drop → client retries with backoff → `attach{last_seq}` frame →
  server replays buffered frames → same session, scrollback tail
  restored. Never respawns.
- Beyond `TU_RECONNECT_WINDOW_S`, session shows `detached` and a manual
  attach resumes it (session outlives the window — Rule 6).

## Tabs/splits

- Tabs and splits are views onto sessions; layouts persist per workspace
  (§03). A split of the same session mirrors output; input routing follows
  focus.

## Search

- Buffer search client-side; scrollback cap `TU_SESSION_BUFFER_KB`.

## Acceptance

- P1–P5 verified on a real touch device: prompt answered via key bar,
  drag-select copies to device clipboard, two-finger scrolls, plane-mode
  reconnect resumes mid-session, tap targets pass ≥44px.
