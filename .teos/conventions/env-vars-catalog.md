# Environment Variables Catalog — `TU_*` only

Canonical registry. New vars are added here **before** use in code
(AGENTS.md Rule 3 + 11). All have sane defaults for the single-container
model; none are required to boot except where marked.

| Var | Default | Purpose |
|-----|---------|---------|
| `TU_PORT` | `7700` | HTTP/WS listen port |
| `TU_BIND` | `127.0.0.1` | Listen address (behind TLS-terminating proxy) |
| `TU_PUBLIC_URL` | — | External origin for cookies/links |
| `TU_DATA_DIR` | `./data` | SQLite file + workspace roots + fixtures |
| `TU_SQLITE_PATH` | `$TU_DATA_DIR/terminu.db` | SQLite file (WAL) |
| `TU_SESSION_SECRET` | *required* | Cookie/session-token signing |
| `TU_ADMIN_USER` | *required first boot* | Initial super-admin username |
| `TU_ADMIN_PASS_HASH` | *required first boot* | Initial admin password — pre-hashed |
| `TU_ADMIN_PASSWORD` | — | Alternative: plaintext at first boot only — hashed into DB, never stored |
| `TU_ALLOWED_ORIGINS` | same-origin | WS/HTTP origin allowlist — set every proxy domain |
| `TU_DEFAULT_SHELL` | user shell | Fallback shell for new terminal sessions |
| `TU_SESSION_BUFFER_KB` | `512` | Per-session scrollback cap |
| `TU_MAX_SESSIONS` | `50` | Concurrent session ceiling |
| `TU_RECONNECT_WINDOW_S` | `300` | Resync window before session marked detached |
| `TU_QUIESCENCE_MS` | `400` | Trigger-gate silence window |
| `TU_SCAN_BOTTOM_ROWS` | `12` | Default adapter scan scope |
| `TU_LLM_ENABLED` | `false` | Master switch for LLM features |
| `TU_LLM_PROVIDER` | — | Provider id (`openai`/`anthropic`/`ollama`…) |
| `TU_LLM_ENDPOINT` | — | Custom endpoint (local models) |
| `TU_LLM_API_KEY` | — | Provider key — secret store, never logged |
| `TU_LLM_MODEL` | — | Model id for screen-reading/suggestions |
| `TU_DOCKER_SOCKET` | `/var/run/docker.sock` | Engine API socket (broker-gated) |
| `TU_SFTP_ENABLED` | `false` | SFTP protocol endpoint (ADR-006) — gate-friendly |
| `TU_SFTP_PORT` | `2022` | SFTP listener port |
| `TU_LOCAL_TARGET_MODE` | `container` | `host\|nsenter\|container` — how `local` target reaches the host (§12) |
| `TU_SSH_KNOWN_HOSTS` | `$TU_DATA_DIR/known_hosts` | Host verification store |
| `TU_LOG_LEVEL` | `info` | tracing filter |
| `TU_SECRETS_KEY` | *required* | Master key for encrypted secret store |
| `TU_WS_MAX_FRAME_KB` | `64` | Per-frame size cap on session/event sockets |
| `TU_WEB_DEV_PORT` | `5173` | Dev-mode proxy target for the Bun dev server (§10) |

Rules: `TU_` prefix mandatory; secrets (`*_SECRET`, `*_KEY`, `*_HASH`)
never logged, exported, or committed; a var used in code but missing here
is a Rule-11 violation.

`terminu.toml` overrides (§01): key = var name minus `TU_`, lowercased,
first `_` → `.` (namespace): `TU_MAX_SESSIONS` → `max.sessions`,
`TU_SFTP_PORT` → `sftp.port`, `TU_ADMIN_USER` → `admin.user`. Env beats
file beats default.
