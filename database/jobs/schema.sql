-- SCAFFOLD: DB-backed job queue schema.
-- Status: NOT CONNECTED to any Go code in this repository.
-- To activate:
--   1. Run this DDL against your MySQL/SQLite database.
--   2. Implement a Go repository in database/jobs/ using the SQLC queries
--      in database/queries/jobs.sql (run `sqlc generate` first).
--   3. Wire a worker.Worker that calls PopNextJob, FailJob, CompleteJob.
-- See go-core/worker/worker.go for the Worker interface.

CREATE TABLE jobs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    queue_name VARCHAR(50) NOT NULL DEFAULT 'default',
    payload JSON NOT NULL, -- The serialized job data
    status ENUM('pending', 'processing', 'failed', 'completed') DEFAULT 'pending',
    attempts TINYINT DEFAULT 0,
    max_attempts TINYINT DEFAULT 3,
    available_at TIMESTAMP NOT NULL, -- When the job can be run
    reserved_at TIMESTAMP NULL,      -- When a worker picked it up
    last_error TEXT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
