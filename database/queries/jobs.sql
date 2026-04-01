-- SCAFFOLD: SQLC query definitions for the DB-backed job queue.
-- Status: NOT CONNECTED to any Go code in this repository.
-- See database/jobs/schema.sql for the table definition and activation steps.

-- name: FailJob :exec
UPDATE jobs
SET
    status = CASE
        WHEN attempts + 1 >= max_attempts THEN 'failed'
        ELSE 'pending'
    END,
    attempts = attempts + 1,
    available_at = ?,
    last_error = ?,
    reserved_at = NULL
WHERE id = ?;

-- name: CompleteJob :exec
UPDATE jobs SET status = 'completed', reserved_at = NULL WHERE id = ?;

-- name: PopNextJob :one
UPDATE jobs
SET reserved_at = NOW(), status = 'processing'
WHERE id = (
    SELECT id
    FROM jobs
    WHERE queue_name = ?
      AND (status = 'pending' OR (status = 'failed' AND attempts < max_attempts))
      AND available_at <= NOW()
      AND reserved_at IS NULL
    ORDER BY available_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;
