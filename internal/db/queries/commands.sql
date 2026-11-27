-- name: UpsertManifest :exec
-- Boot-sync + discovery writes; shadowed_by is set by the resolver,
-- never by the writer.
INSERT INTO command_manifests (id, application, name, description, params,
    executable, arguments, risk, confirmation, timeout_s, working_dir,
    ctx_processes, ctx_files, tags, source)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id, source) DO UPDATE SET
    application = excluded.application,
    name = excluded.name,
    description = excluded.description,
    params = excluded.params,
    executable = excluded.executable,
    arguments = excluded.arguments,
    risk = excluded.risk,
    confirmation = excluded.confirmation,
    timeout_s = excluded.timeout_s,
    working_dir = excluded.working_dir,
    ctx_processes = excluded.ctx_processes,
    ctx_files = excluded.ctx_files,
    tags = excluded.tags,
    source = excluded.source,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now');

-- name: GetManifest :one
-- Source priority resolves the winner on an id shared across sources.
SELECT * FROM command_manifests WHERE id = ?
ORDER BY CASE source WHEN 'builtin' THEN 0 WHEN 'custom' THEN 1 ELSE 2 END
LIMIT 1;

-- name: GetManifestBySource :one
SELECT * FROM command_manifests WHERE id = ? AND source = ?;

-- name: DeleteManifestsBySource :exec
-- Builtin/custom are file-driven: the whole source reloads at boot so
-- rows whose file vanished are dropped - the table never serves stale
-- manifests. (json_each() can't take a bind param under sqlc's sqlite
-- grammar, hence delete-all + re-upsert rather than NOT IN.)
DELETE FROM command_manifests WHERE source = ?;

-- name: DeleteDiscoveredManifests :exec
-- Re-discovery replaces one exe's provisional set wholesale - subs that
-- vanished from --help since the last run leave no stale rows. Scoped
-- per application so other exes' discovered rows survive.
DELETE FROM command_manifests WHERE source = 'discovered' AND application = ?;

-- name: DeleteDiscoveredSlashManifests :exec
-- Slash re-detection replaces only an app's *.slash.* rows - its
-- --help-derived subs/flags are untouched by a slash set change.
DELETE FROM command_manifests
WHERE source = 'discovered' AND application = ? AND id LIKE ?;

-- name: ListManifests :many
SELECT * FROM command_manifests ORDER BY application, id;

-- name: ListEnabledManifests :many
SELECT * FROM command_manifests WHERE enabled = 1 AND shadowed_by IS NULL
ORDER BY application, id;

-- name: SetManifestShadowed :exec
-- Resolve marks every row: the winner id for losers, NULL for winners.
UPDATE command_manifests SET shadowed_by = ? WHERE id = ? AND source = ?;
