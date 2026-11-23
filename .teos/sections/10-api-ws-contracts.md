# Section 10 — API & WebSocket Contracts

Owns: HTTP route surface, WS framing, auth middleware, versioning policy.
Catalogs: `conventions/api-routes-catalog.md`,
`conventions/websocket-message-catalog.md`, `api-response-format.md`.

## Rules

- Every route in the catalog before implementation (Rule 9); every
  mutating route carries a broker-permission annotation.
- Error envelope per `api-response-format.md`; codes registered in its
  table.
- No versioning prefix in v1 (`/api/...`); if a breaking surface ever
  ships, `/api/v2` is introduced then — never silently break clients.

## WS transport

- Two sockets: `/ws/session/{id}` (PTY attach — binary data frames + JSON
  control) and `/ws/events` (global feed — JSON).
- Auth: session cookie at upgrade + per-message authz on session sockets;
  origin allowlist enforced; frame size caps; per-client bounded buffers.
- Sequence numbers on `data` frames drive reconnect resync (§02/§03).

## Static assets

The built `apps/web` output is embedded via `go:embed` and served at `/`
by the same server — required for the single-static-binary deliverable
(§12). Dev mode proxies to the Bun dev server instead.

## Middleware order

`tracing → auth → origin-check → route → broker (mutating ops)`.

## Acceptance

- Catalog ↔ route table parity checked in CI (a route not in the catalog
  fails the build check); error envelope shape asserted in tests; WS
  upgrade rejects bad origins.
