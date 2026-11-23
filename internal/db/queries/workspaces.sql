-- name: ListWorkspaces :many
SELECT id, name, layout, last_open_session, updated_at FROM workspaces ORDER BY name;

-- name: GetWorkspace :one
SELECT id, name, layout, last_open_session, updated_at FROM workspaces WHERE id = ?;

-- name: UpsertWorkspace :exec
INSERT INTO workspaces (id, name, layout, last_open_session)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    layout = excluded.layout,
    last_open_session = excluded.last_open_session,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now');
