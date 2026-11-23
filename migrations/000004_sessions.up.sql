-- Ref: .teos/schemas/MIGRATION-ORDER.md #4, .teos/sections/03
CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL DEFAULT '',
    target_kind  TEXT NOT NULL DEFAULT 'local',
    target_id    TEXT NOT NULL DEFAULT '',
    cwd          TEXT NOT NULL DEFAULT '',
    shell        TEXT NOT NULL DEFAULT '',
    state        TEXT NOT NULL DEFAULT 'starting',
    exit_code    INTEGER,
    workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
    cols         INTEGER NOT NULL DEFAULT 80,
    rows         INTEGER NOT NULL DEFAULT 24,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    detached_at  TEXT,
    ended_at     TEXT
);
CREATE INDEX idx_sessions_state ON sessions(state);
