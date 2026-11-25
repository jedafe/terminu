-- name: CreateProfile :one
INSERT INTO launch_profiles (name, tool, argv, env, cwd, resume_cmd)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetProfile :one
SELECT * FROM launch_profiles WHERE id = ?;

-- name: ListProfiles :many
SELECT * FROM launch_profiles ORDER BY tool, name;

-- name: UpdateProfile :exec
UPDATE launch_profiles SET name = ?, tool = ?, argv = ?, env = ?, cwd = ?,
    resume_cmd = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE id = ?;

-- name: DeleteProfile :exec
DELETE FROM launch_profiles WHERE id = ?;
