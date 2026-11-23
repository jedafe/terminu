-- Ref: .teos/schemas/MIGRATION-ORDER.md #5, .teos/sections/03
-- Append-only transition history; the registry reads latest-per-session.
CREATE TABLE session_state_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    state       TEXT NOT NULL,
    kind        TEXT,
    confidence  TEXT,
    evidence    TEXT,
    at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_state_log_session ON session_state_log(session_id, id);
