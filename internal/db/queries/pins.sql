-- name: UpsertAdapterPin :exec
-- Declared pins sync at boot from the compiled adapters; probe results
-- update seen_version/status in place.
INSERT INTO adapter_pins (tool, pin_constraint, verified_at)
VALUES (?, ?, ?)
ON CONFLICT(tool) DO UPDATE SET
    pin_constraint = excluded.pin_constraint,
    verified_at = excluded.verified_at,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now');

-- name: GetAdapterPin :one
SELECT * FROM adapter_pins WHERE tool = ?;

-- name: ListAdapterPins :many
SELECT * FROM adapter_pins ORDER BY tool;

-- name: UpdatePinSeen :exec
UPDATE adapter_pins SET seen_version = ?, status = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE tool = ?;

-- name: UpsertFixture :exec
INSERT INTO fixtures (tool, version, scenario, path)
VALUES (?, ?, ?, ?)
ON CONFLICT(tool, version, scenario) DO UPDATE SET path = excluded.path;

-- name: ListFixtures :many
SELECT * FROM fixtures ORDER BY tool, version, scenario;

-- name: ListFixturesByTool :many
SELECT * FROM fixtures WHERE tool = ? ORDER BY version, scenario;

-- name: UpdateFixtureResult :exec
UPDATE fixtures SET last_result = ?, replayed_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE tool = ? AND version = ? AND scenario = ?;
