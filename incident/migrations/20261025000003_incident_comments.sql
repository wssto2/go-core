-- +goose Up
-- incident_comments: remarks on an incident; an internal comment is read by the staff only.
-- The DDL is what an application that grew the same tables under other names has once it renames them, so it can
-- adopt this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `incident_comments` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `incident_id` bigint unsigned NOT NULL,
  `author_user_id` bigint unsigned NOT NULL,
  `visibility` enum('public','internal') COLLATE utf8mb3_unicode_ci NOT NULL DEFAULT 'public',
  `body` mediumtext COLLATE utf8mb3_unicode_ci NOT NULL,
  `edited_at` datetime DEFAULT NULL,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_incident_comments_incident_id` (`incident_id`),
  KEY `idx_incident_comments_author_user_id` (`author_user_id`),
  KEY `idx_incident_comments_created_at` (`created_at`),
  KEY `idx_incident_comments_visibility` (`visibility`),
  CONSTRAINT `fk_incident_comments_incident` FOREIGN KEY (`incident_id`) REFERENCES `incidents` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
