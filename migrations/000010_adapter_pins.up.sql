-- Ref: .teos/schemas/MIGRATION-ORDER.md #8, .teos/sections/06
-- Per-tool pin records: the declared coverage range, the fixture-verified
-- date, and the last binary version seen on a bound session. A version
-- outside the constraint lands `unverified` — the adapter still runs.
CREATE TABLE adapter_pins (
    tool           TEXT PRIMARY KEY,             -- devin|claude|codex|gemini|opencode|aider
    pin_constraint TEXT NOT NULL DEFAULT '',     -- declared coverage range, e.g. ">=1.0 <2.0"
    verified_at    TEXT NOT NULL DEFAULT '',     -- ISO date of last fixture verification
    seen_version   TEXT NOT NULL DEFAULT '',     -- last probed binary version
    status         TEXT NOT NULL DEFAULT 'unknown', -- verified | unverified | unknown
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
