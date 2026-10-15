-- +goose Up
-- incident_occurrences: one sighting of an incident, with the request it happened in. Kept 90 days, except the newest of each incident.
-- The DDL is what an application that grew the same tables under other names has once it renames them, so it can
-- adopt this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `incident_occurrences` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `incident_id` bigint unsigned NOT NULL,
  `fingerprint` char(64) COLLATE utf8mb3_unicode_ci NOT NULL,
  `environment` varchar(50) COLLATE utf8mb3_unicode_ci NOT NULL,
  `affected_module` varchar(100) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `affected_route` varchar(255) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `request_method` varchar(10) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `user_id` bigint unsigned DEFAULT NULL,
  `tenant_id` bigint unsigned DEFAULT NULL,
  `ip_address` varchar(45) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `request_id` varchar(100) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `error_class` varchar(255) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `error_code` varchar(100) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `message` text COLLATE utf8mb3_unicode_ci NOT NULL,
  `normalized_message` text COLLATE utf8mb3_unicode_ci,
  `stacktrace` mediumtext COLLATE utf8mb3_unicode_ci,
  `context_json` json DEFAULT NULL,
  `request_json` json DEFAULT NULL,
  `occurred_at` datetime NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_incident_occurrences_incident_id` (`incident_id`),
  KEY `idx_incident_occurrences_fingerprint` (`fingerprint`),
  KEY `idx_incident_occurrences_user_id` (`user_id`),
  KEY `idx_incident_occurrences_occurred_at` (`occurred_at`),
  KEY `idx_incident_occurrences_module` (`affected_module`),
  CONSTRAINT `fk_incident_occurrences_incident` FOREIGN KEY (`incident_id`) REFERENCES `incidents` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
