# Section 09 — Security & Execution Broker

Owns: auth, roles, broker pipeline, risk model, audit, secret store.
Tables `users`, `auth_sessions`, `audit_log`, `user_public_keys`.
Implements ADR-004.

## Auth

- Session-cookie auth (Argon2 password hashes; `TU_ADMIN_*` seeds the
  first admin). `auth_sessions` rows; secure+httponly+samesite cookies;
  expiry + revocation.
- `user_public_keys` holds per-user SSH public keys — used by the SFTP
  endpoint (§07, ADR-006); managed via `/api/users` admin surface.
- Roles v1: `admin` (all) · `operator` (run/attach/execute non-restricted)
  · `viewer` (read/attach, no mutating ops). Org/OIDC deferred.

## Broker pipeline — the only path

``` text
op → {identity, target, action, argv, cwd, env} →
risk classify → policy check → confirmation (if required) → execute → audit
```

- Risk classes: `informational` (immediate) · `controlled` (policy/confirm)
  · `confirmed` (explicit confirmation token) · `restricted` (blocked by
  default).
- Classification inputs: role, target, command, argv, cwd, environment
  profile, manifest-declared risk.
- Confirmation: short-TTL token bound to the exact argv — can't be
  replayed against a different command.
- LLM-originated ops: identical gate (Rule 12).

## Audit

- `audit_log` append-only: actor, action, target, argv (redacted where
  secret-bearing), result, timestamp. Never cascades on user delete —
  audit integrity outlives accounts.

## Secret store

- `TU_SECRETS_KEY`-encrypted store (`x/crypto/nacl` secretbox) for SSH keys/passwords,
  S3 creds, API keys. Desktop later uses OS vault via the same `SecretStore`
  interface — the interface is the seam (ADR-002).
- Secrets never logged, exported, or in session metadata; audit shows
  `secret_ref`, never value.

## Hard rules carried from brief §10.3

Structured argv · server non-root · shell=RCE documented+gated · WS
per-message authz + origin allowlist + size caps + backpressure · secret
redaction in logs/audit where detectable.

## Acceptance

- An unauthenticated request to any privileged route → 401; a `confirmed`
  op without a valid token → `confirmation_required`; every privileged
  execution produces an audit row; no code path reaches a target outside
  `broker.Execute` (enforced by package boundaries — `internal/` import
  discipline + review).
