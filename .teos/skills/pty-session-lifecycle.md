# Skill — PTY Session Lifecycle Pattern

Reusable pattern for the Session Manager (§03). Written ahead of
implementation as the canonical shape — update post-implementation.

## Invariant

The server owns the PTY. A session is `spawn → live(working/waiting) →
done|error|detached`, never client-owned. Any attach is a view.

## Shape

``` text
spawn(broker-approved spec)
  → pty pair + reader task + writer handle + screen model
  → registry.insert(id, Live{state: starting})
  → broadcast agent.state

PTY reader loop:
  bytes → screen_model.apply(bytes)
        → broadcast(seq++, bytes) to attached clients (bounded chan)
        → trigger_gate.feed(event)   // detection happens elsewhere

attach(client):
  → auth via broker → send hello{cursor} → replay scrollback from cursor
  → register client chan (bounded) — lagging client gets DropOldest

detach(client): drop chan; start detached timer
exit: code → done|error → state_log row + agent.state event
terminate(broker): SIGTERM → grace → SIGKILL → cleanup
```

## Rules baked in

- Attach is idempotent; a second attach = second bounded chan, same PTY.
- Never block the reader loop on a client (drop frames, don't stall).
- Server restart: sessions end honestly (`ended`), never fake-resumed.
- Every transition writes `session_state_log` + emits `agent.state`.
