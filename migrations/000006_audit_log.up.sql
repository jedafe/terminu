-- Ref: .teos/schemas/MIGRATION-ORDER.md #10, .teos/sections/09
-- Append-only; never cascades on user delete — audit integrity outlives
-- accounts (actor stored as text).
CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    actor      TEXT NOT NULL,
    actor_id   INTEGER,
    action     TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    argv       TEXT NOT NULL DEFAULT '',
    result     TEXT NOT NULL DEFAULT '',
    at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_audit_at ON audit_log(at);
