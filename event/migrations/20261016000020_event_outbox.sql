-- +goose Up
-- event: the transactional outbox (event.OutboxEvent). Publishing an event inserts one row in the
-- publisher's own transaction; consumers read the rows of their event name from here, and the
-- row is marked processed once every consumer of it has finished. event_type holds the event's
-- durable name ("tickets.assigned"); envelope holds the version and the JSON payload.
-- The DDL is what an application that already ran the outbox (ARV's local database) has, so
-- it adopts this file as it is; `json` is an alias of longtext on MariaDB 10.3.
CREATE TABLE IF NOT EXISTS `outbox_events` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `request_id` varchar(191) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `source` varchar(191) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `event_type` varchar(191) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `envelope` json DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `processed_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_request_id` (`request_id`),
  KEY `idx_processed` (`processed_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
