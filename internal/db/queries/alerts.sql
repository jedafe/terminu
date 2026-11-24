-- name: CreateAlert :exec
INSERT INTO alerts (session_id, level, transition, process, kind, confidence)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListAlerts :many
SELECT * FROM alerts ORDER BY (acked_at IS NULL) DESC, id DESC LIMIT ?;

-- name: AckAlert :exec
UPDATE alerts SET acked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?;
