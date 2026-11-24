-- Ref: .teos/schemas/MIGRATION-ORDER.md #14, .teos/sections/05
-- Ack-able alert rows emitted on catalog transitions (working→waiting,
-- *→done, *→error) — the WS `alert` frame is the live edge; this table
-- is its history.
CREATE TABLE alerts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    level       TEXT NOT NULL,
    transition  TEXT NOT NULL,
    process     TEXT NOT NULL DEFAULT '',
    kind        TEXT,
    confidence  TEXT,
    acked_at    TEXT,
    at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_alerts_unack ON alerts(acked_at, id);
