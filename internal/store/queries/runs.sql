-- name: CreateRun :one
INSERT INTO runs (id, goal, status)
VALUES ($1, $2, 'pending')
RETURNING *;

-- name: GetRun :one
SELECT * FROM runs WHERE id = $1;

-- name: ListRuns :many
SELECT * FROM runs ORDER BY created_at DESC LIMIT $1;

-- name: UpdateRunStatus :exec
UPDATE runs
SET status = $2, error = $3, updated_at = now()
WHERE id = $1;

-- name: ListRunsByStatus :many
SELECT * FROM runs WHERE status = $1 ORDER BY created_at ASC;