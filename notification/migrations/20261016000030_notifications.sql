-- +goose Up
-- notification: the in-app inbox. One row is one notification for one person: the record of what
-- happened and the source of truth for read and unread on every device the person is signed in on.
-- Rows are made only by notification.Notices.Send, from an event consumer.
--
-- dedupe_key is "<event id>:<user id>:<category>". Its unique index makes Send idempotent: an event
-- delivered twice inserts no second row. The DDL is what an application that already ran this inbox
-- (ARV's local database) has, so it adopts this file as it is; `json` is an alias of longtext on
-- MariaDB 10.3 and the column holds a flat string map, never queried with JSON functions.
CREATE TABLE IF NOT EXISTS `notifications` (
  `id`         bigint unsigned NOT NULL AUTO_INCREMENT,
  `user_id`    int unsigned    NOT NULL COMMENT 'FK: users.id - the recipient',
  `category`   varchar(64)     NOT NULL COMMENT 'Category code from the code registry, e.g. lead.assigned',
  `title`      varchar(160)    NOT NULL COMMENT 'Rendered in the recipient locale at dispatch time',
  `body`       varchar(500)    NOT NULL DEFAULT '' COMMENT 'Rendered text; never customer phone or e-mail',
  `link`       varchar(255)    NOT NULL DEFAULT '' COMMENT 'In-app path the notification opens, e.g. /crm/leads/12',
  `data`       json            NOT NULL COMMENT 'Ids and facts for the client (flat string map, {} when none); no secrets',
  `dedupe_key` varchar(128)    NOT NULL COMMENT 'event ref:user id:category - one notification per event, user and category',
  `read_at`    datetime        NULL     COMMENT 'Set when read on any device',
  `created_at` datetime        NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `notifications_dedupe_key` (`dedupe_key`),
  KEY `notifications_user_id` (`user_id`, `id`),
  KEY `notifications_user_read` (`user_id`, `read_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
