-- +goose Up
-- incident_digest_queue: new incidents waiting for the next digest alert.
-- The DDL is what an application that grew the same tables under other names has once it renames them, so it can
-- adopt this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `incident_digest_queue` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `incident_id` bigint unsigned NOT NULL,
  `public_code` varchar(32) COLLATE utf8mb3_unicode_ci NOT NULL,
  `title` varchar(255) COLLATE utf8mb3_unicode_ci NOT NULL,
  `severity` varchar(20) COLLATE utf8mb3_unicode_ci NOT NULL,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_incident_digest_queue_incident_id` (`incident_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
