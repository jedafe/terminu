-- Ref: .teos/schemas/MIGRATION-ORDER.md #7, .teos/sections/05
-- Saved per-tool launch configs — POST /api/agents/launch spawns a
-- session with the profile bound so state detection arms from byte one.
CREATE TABLE launch_profiles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    tool       TEXT NOT NULL DEFAULT '',      -- adapter the launch binds (devin|claude|...)
    argv       TEXT NOT NULL DEFAULT '[]',    -- JSON array: executable + args
    env        TEXT NOT NULL DEFAULT '{}',    -- JSON object: KEY -> value
    cwd        TEXT NOT NULL DEFAULT '',
    resume_cmd TEXT NOT NULL DEFAULT '',      -- relaunch variant (e.g. ["claude","--resume"])
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_profiles_tool ON launch_profiles(tool);
