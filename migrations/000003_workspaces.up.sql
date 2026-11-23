-- Ref: .teos/schemas/MIGRATION-ORDER.md #3, .teos/sections/03
-- last_open_session is deliberately FK-free (circular-FK guard, MIGRATION-ORDER).
CREATE TABLE workspaces (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT NOT NULL UNIQUE,
    layout            TEXT NOT NULL DEFAULT '{}',
    last_open_session TEXT,
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
