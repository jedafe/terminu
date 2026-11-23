# WebSocket Message Catalog

Canonical WS contract registry (AGENTS.md Rule 8). Two sockets exist:
`/ws/session/{id}` (PTY attach) and `/ws/events` (global state feed).

Framing: **JSON text control frames + binary data frames** on the session
socket; JSON only on the events socket. Every frame has `t` (type).

## Session socket `/ws/session/{id}`

| `t` | Dir | Payload | Purpose |
|-----|-----|---------|---------|
| `attach` | c→s | `{last_seq}` | **first frame after upgrade** — carries resume cursor (`0`/absent = fresh attach) |
| `hello` | s→c | `{session_id, cols, rows, state, next_seq}` | attach ack + resync cursor |
| `data` | s→c | binary frame: `seq` (8B uint64 BE prefix) + payload | PTY output — `seq` drives reconnect resync |
| `input` | c→s | binary frame | keystrokes/paste → PTY |
| `resize` | c→s | `{cols, rows}` | propagate size to PTY |
| `state` | s→c | `{state, kind?, confidence, evidence}` | state transition push (`kind` set when `waiting-for-input`) |
| `prompt` | s→c | `{kind, title, options:[{label,inject}]}` | detected prompt → overlay (`options:[]` for `freetext`) |
| `prompt_clear` | s→c | `{region}` | detected prompt no longer present |
| `detach` | c→s | `{}` | clean detach (session persists) |
| `eof` | s→c | `{exit_code}` | process exited → done/error |
| `error` | s→c | `{code, message}` | fatal for this attach |

Resync contract: server→client `data` frames carry a monotonically
increasing `seq`; the client tracks its last-received seq and sends it as
`attach.last_seq` — the first frame after every upgrade. The server
replays buffered `data` frames with `seq > last_seq`, then sends `hello`
with `next_seq`. Same session, no respawn (Rule 6). Client→server `input`
frames carry no seq (order is the WS stream itself).

## Events socket `/ws/events`

| `t` | Payload | Purpose |
|-----|---------|---------|
| `agent.state` | `{session_id, process, state, kind?, confidence}` | feeds needs-you view |
| `session.added` / `session.removed` | `{session_id}` | registry deltas |
| `alert` | `{level, session_id, transition}` | waiting/done/error alerts |
| `files.progress` | `{transfer_id, done, total}` | upload/download queues |
| `docker.delta` | `{container, status}` | docker pane refresh |

Rules: register here first; per-frame `t` is required; authz is
per-connection + per-message on session sockets; frame size caps enforced
server-side.
