# Section 01 — Foundation & Tech Stack

Owns: package layout, toolchain, CI baseline, config plumbing.

## Stack

See `.ai/stack.yaml` for the pinned table (ADR-005): Go + stdlib net/http
(+ `coder/websocket`); `creack/pty`; xterm.js (ghostty-web alt.); SvelteKit
+ Svelte 5 + Bun; SQLite (WAL, `modernc.org/sqlite` + sqlc);
`x/crypto/ssh`; official Docker Engine API client; opt-in LLM interface.
Single static binary / single container.

## Package layout (monorepo)

``` text
terminu/
├── cmd/
│   └── terminu-server/       # main — wires everything
├── internal/
│   ├── server/               # http+ws wiring, auth middleware, routes
│   ├── session/              # lifecycle, PTY ownership, resync (goroutine/session)
│   ├── screen/               # headless VT model, dirty regions, trigger gate
│   ├── adapter/              # Adapter interface + per-tool impls + replay tests
│   ├── registry/             # command manifests, discovery, palette index
│   ├── broker/               # policy eval, risk gate, confirmations, audit
│   ├── files/                # storage providers (local/S3/SFTP)
│   ├── conn/                 # SSH profiles, connection manager, credentials
│   ├── domain/               # host-agnostic models — desktop sidecar reuses (ADR-002)
│   └── db/                   # sqlc generated — NEVER hand-edit
├── apps/
│   └── web/                  # SvelteKit UI (Bun tooling)
├── migrations/               # golang-migrate — see schemas/MIGRATION-ORDER.md
├── registry/                 # builtin command manifests (YAML)
├── fixtures/<tool>/          # adapter transcripts
└── .teos/                    # this corpus
```

## Config

`TU_*` env vars per `conventions/env-vars-catalog.md`; optional
`terminu.toml` overrides file; precedence env > file > default. No config
value may be hardcoded — catalog first.

## CI baseline (Phase 0 deliverable)

- `gofmt -l`, `go vet`, `go test ./...`
- `bunx svelte-check`, `bun test` for the web app
- adapter fixture replay suite (`go test ./internal/adapter/...`)
- container build smoke test + size/memory budget check

## Acceptance

- `CGO_ENABLED=0 go build` produces one static server binary;
  `bun run build` produces the web app; `docker build` produces a single
  image that boots to login with
  only `TU_SESSION_SECRET` + `TU_ADMIN_*` set.
