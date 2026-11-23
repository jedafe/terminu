# Section 05 — Agent Session Management

Owns: agent registry, "needs you" view, prompt overlays, alerts, launch
profiles. Tables `launch_profiles`, `alerts`. The wedge — brief §6.

## Agent registry

- A session becomes an "agent session" when its bound adapter (§06) or
  launch profile marks it an agent CLI. Registry view: `GET /api/agents`
  ordered — `waiting-for-input` (high conf first, then oldest wait), then
  `working`, then `done`/`error` recent.
- `/ws/events` `agent.state` frames power the view — emitted on
  transition, never polled.

## "Needs you" view

- Agents in `waiting-for-input` surface with `prompt` payload: `kind`,
  `title`, `options[]`. Rendered as buttons/inline-reply per kind.
- A tap sends the option's `inject` keystroke through the session's normal
  input path — **the human always sends** (ADR-001).

## Alerts

- Transitions `working→waiting-for-input`, `*→done`, `*→error` emit
  `alert` events + `alerts` rows (ack-able). In-app only in v1 — push/
  email deferred (brief §13).

## Launch profiles

- Saved per-tool launch config: tool, argv, env, cwd, resume command.
  `POST /api/agents/launch` → broker → spawn with profile bound so state
  detection is armed from first byte.

## Non-goals (v1)

- No agent-internal APIs, no orchestration, no task graphs (§6.3 brief).
- Remote-target detection: process binding via `ps` scrape — slower is
  accepted (ADR-001 revisit #3).

## Acceptance

- Fixture-driven: a recorded devin option-prompt → `waiting-for-input`
  `option` + correct buttons; answer tap resumes agent; transition alert
  emitted; multi-agent list orders waiting-first.
