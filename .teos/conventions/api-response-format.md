# API Response & Error Format

Standard envelope for all `/api` responses (AGENTS.md Rule 5).

## Success

```json
{ "data": { "...": "..." } }
```

## Error

```json
{
  "error": {
    "code": "session_not_found",
    "message": "Session 7f3c was terminated or never existed",
    "status": 404,
    "detail": {}
  }
}
```

Rules:

- `code` is `snake_case`, machine-readable, and stable — clients branch on
  it; `message` is human text and may change.
- New error codes are registered in this file's table below before use.
- `status` mirrors the HTTP status.
- `detail` optional: structured context (e.g. `{"allowed": ["admin"]}`).

## Registered error codes

| Code | Status | When |
|------|--------|------|
| `unauthorized` | 401 | no/expired session |
| `forbidden` | 403 | authenticated, lacks permission |
| `session_not_found` | 404 | unknown/dead session id |
| `session_limit` | 409 | `TU_MAX_SESSIONS` reached |
| `confirmation_required` | 409 | broker: risk needs confirm token |
| `action_restricted` | 403 | broker: restricted risk class |
| `provider_unavailable` | 502 | file/ssh/docker target unreachable |
| `target_unreachable` | 502 | SSH/docker target down |
| `rate_limited` | 429 | endpoint tier exceeded |
| `invalid_manifest` | 400 | manifest schema violation |
| `invalid_transition` | 409 | out-of-order session lifecycle op (§03) |
| `ws_upgrade_failed` | 400 | WS handshake rejected |
| `internal` | 500 | unhandled — logged, never leaked detail |
