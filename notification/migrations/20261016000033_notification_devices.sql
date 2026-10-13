-- +goose Up
-- notification: web push devices (NOTIF-DEVICE-001). One row is one browser or installed app on one machine that
-- enabled push: its Web Push subscription bound to the person who enabled it. The endpoint is a capability URL (whoever
-- holds it and the keys can push to that browser), so it is never sent to a client; endpoint_hash (hex SHA-256) is the
-- unique key that makes registering the same browser twice one row, and the key the client recognises "this device" by.
--
-- The deliveries of a device are in notification_deliveries (channel 'push', device_id). A device that is removed
-- cancels its pending deliveries; there is no database constraint, as for the notification they point to.
CREATE TABLE IF NOT EXISTS `notification_devices` (
  `id`              bigint unsigned NOT NULL AUTO_INCREMENT,
  `user_id`         int unsigned    NOT NULL COMMENT 'FK: users.id - the person who enabled push here; moves on registration by another person',
  `endpoint`        varchar(1024)   NOT NULL COMMENT 'Push service URL of the subscription; never returned to clients',
  `endpoint_hash`   char(64)        NOT NULL COMMENT 'Hex SHA-256 of endpoint - the upsert key',
  `p256dh`          varchar(128)    NOT NULL COMMENT 'Subscription public key (base64url)',
  `auth`            varchar(64)     NOT NULL COMMENT 'Subscription auth secret (base64url)',
  `label`           varchar(100)    NOT NULL COMMENT 'Shown to the person, e.g. Safari · iPhone; they may rename it',
  `platform`        varchar(32)     NOT NULL DEFAULT '' COMMENT 'ios, android, macos, windows, linux or empty',
  `created_at`      datetime        NOT NULL,
  `last_seen_at`    datetime        NOT NULL COMMENT 'Last registration from the app (every app start)',
  `last_success_at` datetime        NULL     COMMENT 'Last push the push service accepted',
  `failure_count`   int unsigned    NOT NULL DEFAULT 0 COMMENT 'Consecutive pushes that failed through the device''s fault; it is disabled at the limit',
  `disabled_at`     datetime        NULL     COMMENT 'Set when failures reached the limit; registering again clears it',
  `updated_at`      datetime        NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `notification_devices_endpoint_hash` (`endpoint_hash`),
  KEY `notification_devices_user` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
