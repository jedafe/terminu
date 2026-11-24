-- name: CreateSession :exec
INSERT INTO sessions (id, name, target_kind, target_id, cwd, shell, state, workspace_id, cols, rows)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT id, name, target_kind, target_id, cwd, shell, state, exit_code,
       workspace_id, cols, rows, created_at, detached_at, ended_at
FROM sessions WHERE id = ?;

-- name: ListSessions :many
SELECT id, name, target_kind, target_id, cwd, shell, state, exit_code,
       workspace_id, cols, rows, created_at, detached_at, ended_at
FROM sessions ORDER BY created_at DESC;

-- name: UpdateSessionState :exec
UPDATE sessions SET state = ? WHERE id = ?;

-- name: UpdateSessionExit :exec
UPDATE sessions SET state = ?, exit_code = ?, ended_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE id = ?;

-- name: UpdateSessionDetached :exec
UPDATE sessions SET detached_at = ? WHERE id = ?;

-- name: UpdateSessionName :exec
UPDATE sessions SET name = ? WHERE id = ?;

-- name: MarkLiveSessionsEnded :exec
UPDATE sessions SET state = 'ended', ended_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE state IN ('starting','working','waiting-for-input','detached','undetectable');

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteFinishedSessions :execrows
-- Finished = process is gone; the row is only a log record.
DELETE FROM sessions WHERE state IN ('done', 'error', 'ended');

-- name: LogSessionState :exec
INSERT INTO session_state_log (session_id, state, kind, confidence, evidence)
VALUES (?, ?, ?, ?, ?);

-- name: CountLiveSessions :one
SELECT COUNT(*) FROM sessions
WHERE state IN ('starting','working','waiting-for-input','undetectable');
