-- name: GetUserByName :one
SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?;

-- name: GetUserByID :one
SELECT id, username, password_hash, role, created_at FROM users WHERE id = ?;

-- name: CreateUser :execresult
INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?);

-- name: CountUsers :one
SELECT COUNT(*) FROM users;
