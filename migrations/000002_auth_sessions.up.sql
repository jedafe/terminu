-- Ref: .teos/schemas/MIGRATION-ORDER.md #2, .teos/sections/09
CREATE TABLE auth_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    expires_at TEXT NOT NULL,
    revoked_at TEXT
);
CREATE INDEX idx_auth_sessions_token ON auth_sessions(token_hash);
