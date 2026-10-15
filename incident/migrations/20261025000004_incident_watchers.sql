-- +goose Up
-- incident_watchers: people who watch an incident (they reported or commented on it) and so see it.
-- The DDL is what an application that grew the same tables under other names has once it renames them, so it can
-- adopt this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `incident_watchers` (
  `incident_id` bigint unsigned NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`incident_id`,`user_id`),
  KEY `idx_incident_watchers_user_id` (`user_id`),
  CONSTRAINT `fk_incident_watchers_incident` FOREIGN KEY (`incident_id`) REFERENCES `incidents` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
