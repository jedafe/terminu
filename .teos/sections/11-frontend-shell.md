# Section 11 — Frontend Shell (SvelteKit)

Owns: `apps/web` SvelteKit app — layout, navigation, chrome, theming,
touch-first UI behaviors. Consumes §10 API only (Rule 15).

## Shell

- Left rail: sessions/agents/files/servers/commands — collapsible;
  ≥44px targets everywhere (P5).
- Main area: tab bar + pane grid over `TerminalSurface` components;
  right drawer: agent state + prompt overlays + context actions.
- Needs-you badge on rail + top strip when any agent waits (§05).
- Prompt overlays render on **any** session with a `prompt` frame — agent
  or plain CLI (P1: a git rebase prompt gets buttons too, per its generic
  tier + confidence; `unknown` waits get none).

## Touch chrome

- Key bar component (§02) global, collapsible, per-profile layout.
- Mode toggle for TUI-mouse vs browser-select; long-press context menus;
  two-finger scroll everywhere.

## Data flow

- API via typed client (`/api`); session WS per attached pane; `/ws/events`
  singleton feed → store. No hardcoded data anywhere (Rule 15); every
  store's source is an endpoint.

## Theming

- Dark-first; CSS vars; density toggle (comfort/compact) — terminal work
  needs compact without breaking touch targets.

## Acceptance

- Zero hardcoded data in production pages; touch targets verified ≥44px;
  needs-you updates within one event round-trip; shell renders inside a
  390px viewport without horizontal scroll.
