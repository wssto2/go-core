-- +goose Up
-- notification: deliveries. One row is one send of one notification: today an e-mail to one address
-- (channel 'email', device_id 0, address filled in). Web push to a device is a later phase; its rows go in
-- this same table, which is why device_id and the HTTP status of a push answer are here. The unique key
-- makes making a delivery idempotent: a notification that was already written gets no second e-mail.
--
-- status: pending (due at next_attempt_at), held (waits for the person's quiet hours to end, then due),
-- sent, failed (gave up: permanent error, or the TTL ran out) and cancelled (read before it was sent, or the
-- notification was deleted). A worker claims due rows by moving next_attempt_at to the end of a lease.
--
-- The DDL is what an application that already runs this table (arv-next's local database: its push and
-- settings migrations and the retention index) has, so it adopts this file as it is. The _finished key serves
-- the retention sweep: finished rows older than the cutoff, by status and updated_at.
CREATE TABLE IF NOT EXISTS `notification_deliveries` (
  `id`              bigint unsigned  NOT NULL AUTO_INCREMENT,
  `notification_id` bigint unsigned  NOT NULL COMMENT 'FK: notifications.id',
  `device_id`       bigint unsigned  NOT NULL COMMENT 'FK: notification_devices.id for push; 0 for email',
  `address`         varchar(255)     NULL     COMMENT 'E-mail address for the email channel',
  `channel`         varchar(16)      NOT NULL COMMENT 'push or email',
  `status`          varchar(16)      NOT NULL COMMENT 'pending, held (quiet hours), sent, failed, cancelled',
  `attempts`        int unsigned     NOT NULL DEFAULT 0,
  `next_attempt_at` datetime         NOT NULL COMMENT 'When a pending delivery is due',
  `expires_at`      datetime         NOT NULL COMMENT 'created_at + TTL of the category priority; no retry after it',
  `sent_at`         datetime         NULL,
  `last_status`     smallint         NULL     COMMENT 'HTTP status of the last push service answer',
  `last_error`      varchar(500)     NULL,
  `created_at`      datetime         NOT NULL,
  `updated_at`      datetime         NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `notification_deliveries_target` (`notification_id`, `device_id`, `channel`),
  KEY `notification_deliveries_due` (`status`, `next_attempt_at`),
  KEY `notification_deliveries_device` (`device_id`),
  KEY `notification_deliveries_finished` (`status`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
