# SQLite Migration Naming

Same convention as the JapaConnect Go-migrate workflow — identical file
format, same tooling.

- Location: `migrations/` at repo root.
- Format: `NNNNNN_description.up.sql` + `NNNNNN_description.down.sql` —
  6-digit zero-padded sequence (golang-migrate convention, same as
  JapaConnect). `up` applies; `down` reverses for local/dev use —
  destructive rollbacks in production are a new forward migration.
- Driver: `modernc.org/sqlite` (pure Go, no cgo — single static binary).
  Queries are codegen'd via **sqlc** against the SQLite dialect — write
  `.sql` query files, run `sqlc generate`, never hand-edit `internal/db/`
  output (JEOS Rule 2 carries over verbatim).
- New tables must first be registered in `schemas/MIGRATION-ORDER.md`
  (AGENTS.md Rule 14).
- Migrations run as a distinct step before the binary serves traffic —
  embedded via `embed.FS` + golang-migrate at boot against
  `TU_SQLITE_PATH`.
- WAL mode, `foreign_keys = ON`, and `busy_timeout` are set on every
  connection — in the pool builder (`?_pragma=busy_timeout(5000)` or
  connector hooks), not per-migration.
- Breaking changes use the two-phase pattern: migrate supporting both
  shapes → deploy code → migrate to final shape.
- Migrations are append-only once shipped; never edit a merged migration —
  fix forward.
