-- Ref: .teos/schemas/MIGRATION-ORDER.md #9, .teos/sections/06
-- Registered .cast transcripts (embedded under internal/adapter/fixtures)
-- + their last boot-time replay result — the replay suite's CI state,
-- readable via GET /api/adapters/{tool}.
CREATE TABLE fixtures (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tool        TEXT NOT NULL,
    version     TEXT NOT NULL,
    scenario    TEXT NOT NULL,
    path        TEXT NOT NULL,               -- embedded FS path
    last_result TEXT NOT NULL DEFAULT '',    -- pass | fail | '' (unreplayed)
    replayed_at TEXT,
    UNIQUE(tool, version, scenario)
);
CREATE INDEX idx_fixtures_tool ON fixtures(tool);
