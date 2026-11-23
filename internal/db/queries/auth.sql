-- name: CreateAuthSession :execresult
INSERT INTO auth_sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?);

-- name: GetAuthSession :one
SELECT s.id, s.user_id, s.expires_at, s.revoked_at, u.username, u.role
FROM auth_sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ?;

-- name: RevokeAuthSession :exec
UPDATE auth_sessions SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE token_hash = ?;
