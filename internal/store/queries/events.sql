-- name: AppendEvent :one
INSERT INTO events (run_id, seq, type, payload)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListEventsByRun :many
SELECT * FROM events
WHERE run_id = $1
ORDER BY seq ASC;

-- name: GetLastSeq :one
SELECT COALESCE(MAX(seq), 0)::int AS last_seq
FROM events
WHERE run_id = $1;