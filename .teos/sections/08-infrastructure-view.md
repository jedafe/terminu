# Section 08 — Infrastructure View (thin)

Owns: SSH profiles/connections, host health, Docker pane. Table
`ssh_profiles`. Deliberately thin — no port/env/db managers (deferred).

## SSH

- Profiles: host, port, user, auth (`key` | `agent` | `password`),
  `secret_ref` → encrypted secret store — never a credential column.
- `golang.org/x/crypto/ssh` client. Host verification via
  `TU_SSH_KNOWN_HOSTS`;
  unknown host → explicit trust prompt.
- A profile is an `ExecutionTarget` of kind `ssh` — sessions and file
  ops run over it. Remote process detection uses `ps` scrape (ADR-001
  accepted latency).

## Server health

- `GET /api/health/host?target=` → CPU/RAM/disk/load/top-processes. Local
  host via sysinfo; `target=<ssh_profile_id>` runs a one-shot remote
  command. Read-only, informational risk. Refresh on view open + interval
  while visible — no always-on poller (weight budget).

## Docker pane

- Engine API via `TU_DOCKER_SOCKET` through the broker. Capabilities:
  list containers/images, logs, start/stop/restart (`confirmed` risk),
  exec shell — `POST …/exec` returns a `session_id` attached via the
  normal `/ws/session/{id}` path (it reuses §02/§03 wholesale — no
  separate socket contract).
- No compose editor, no image workflows (deferred — brief §13).

## Acceptance

- An SSH profile connects (key auth), spawns a remote session, and binds
  detection with documented latency; health endpoint answers read-only;
  docker start/stop requires confirmation + writes audit; unauth socket
  never exposed.
