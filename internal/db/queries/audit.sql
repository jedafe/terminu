-- name: WriteAudit :exec
INSERT INTO audit_log (actor, actor_id, action, target, argv, result) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListAudit :many
SELECT id, actor, actor_id, action, target, argv, result, at
FROM audit_log ORDER BY id DESC LIMIT ?;
