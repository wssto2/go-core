-- +goose Up
-- notification: a person's own settings (NOTIF-PREF-001, NOTIF-QUIET-001).
--
-- notification_preferences keeps a person's own choice for one category and channel. Only choices that
-- differ from the category's default are stored, so a person who never touched a setting follows the default
-- if it changes. The channel is 'email' (the in-app notification is always on); 'push' joins it later.
--
-- notification_quiet_hours keeps a person's quiet hours. No row means the default for everyone: on, 21:00-07:00
-- in the application's time zone.
--
-- The DDL is what an application that already runs these tables (arv-next's local database) has, so it adopts
-- this file as it is.
CREATE TABLE IF NOT EXISTS `notification_preferences` (
  `user_id`    int unsigned NOT NULL COMMENT 'FK: users.id',
  `category`   varchar(64)  NOT NULL COMMENT 'Category code, e.g. lead.assigned',
  `channel`    varchar(16)  NOT NULL COMMENT 'push or email (in-app is always on)',
  `enabled`    tinyint(1)   NOT NULL,
  `updated_at` datetime     NOT NULL,
  PRIMARY KEY (`user_id`, `category`, `channel`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `notification_quiet_hours` (
  `user_id`      int unsigned      NOT NULL COMMENT 'FK: users.id',
  `enabled`      tinyint(1)        NOT NULL,
  `start_minute` smallint unsigned NOT NULL COMMENT 'Minutes after midnight when quiet hours start (1260 = 21:00)',
  `end_minute`   smallint unsigned NOT NULL COMMENT 'Minutes after midnight when they end (420 = 07:00); may be earlier than start (overnight)',
  `updated_at`   datetime          NOT NULL,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
