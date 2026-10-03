-- +goose Up
-- event: one consumer's state for one event. A consumer claims the due events of its name by
-- leasing them here (next_attempt_at = the end of a 5-minute lease), a failure counts in
-- `attempts` and sets next_attempt_at with exponential backoff, a success sets done_at, and
-- giving up (too many failures, or an event that can never be read) sets dead_at: the dead
-- letter, with last_error. So the table holds events being leased or retried, the ones
-- finished, and the dead letters:
--   SELECT * FROM event_consumer_attempts WHERE dead_at IS NOT NULL ORDER BY dead_at DESC;
-- The primary key is per consumer, so one failing consumer holds back no other. Times are the
-- server's wall time, to the second, like every DATETIME column of go-core's modules.
CREATE TABLE IF NOT EXISTS `event_consumer_attempts` (
  `event_id`        bigint unsigned   NOT NULL COMMENT 'outbox_events.id',
  `consumer`        varchar(100)      NOT NULL COMMENT 'The consumer''s durable name',
  `attempts`        smallint unsigned NOT NULL DEFAULT 0 COMMENT 'Failed attempts so far',
  `next_attempt_at` datetime          NOT NULL COMMENT 'Due again at: the backoff after a failure, or the end of a claim lease',
  `last_error`      varchar(500)      NOT NULL DEFAULT '' COMMENT 'Last failure',
  `done_at`         datetime          NULL     COMMENT 'Handled successfully',
  `dead_at`         datetime          NULL     COMMENT 'Given up on; NULL while retried',
  `updated_at`      datetime          NOT NULL,
  PRIMARY KEY (`event_id`, `consumer`),
  KEY `event_consumer_attempts_dead` (`dead_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
