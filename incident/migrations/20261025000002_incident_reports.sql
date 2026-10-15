-- +goose Up
-- incident_reports: a person's bug report behind an incident.
-- The DDL is what an application that grew the same tables under other names has once it renames them, so it can
-- adopt this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `incident_reports` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `incident_id` bigint unsigned DEFAULT NULL,
  `reported_by_user_id` bigint unsigned NOT NULL,
  `report_type` enum('manual','auto_frontend') COLLATE utf8mb3_unicode_ci NOT NULL DEFAULT 'manual',
  `status` enum('submitted','linked','closed') COLLATE utf8mb3_unicode_ci NOT NULL DEFAULT 'submitted',
  `title` varchar(255) COLLATE utf8mb3_unicode_ci NOT NULL,
  `description` mediumtext COLLATE utf8mb3_unicode_ci NOT NULL,
  `steps_to_reproduce` mediumtext COLLATE utf8mb3_unicode_ci,
  `expected_result` mediumtext COLLATE utf8mb3_unicode_ci,
  `actual_result` mediumtext COLLATE utf8mb3_unicode_ci,
  `severity` enum('low','medium','high','critical') COLLATE utf8mb3_unicode_ci NOT NULL DEFAULT 'medium',
  `affected_module` varchar(100) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `page_url` varchar(1000) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `browser_info` varchar(500) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `app_version` varchar(100) COLLATE utf8mb3_unicode_ci DEFAULT NULL,
  `frontend_error_message` text COLLATE utf8mb3_unicode_ci,
  `frontend_stacktrace` mediumtext COLLATE utf8mb3_unicode_ci,
  `attachment_json` json DEFAULT NULL,
  `extra_json` json DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_incident_reports_incident_id` (`incident_id`),
  KEY `idx_incident_reports_reported_by_user_id` (`reported_by_user_id`),
  KEY `idx_incident_reports_status` (`status`),
  KEY `idx_incident_reports_created_at` (`created_at`),
  CONSTRAINT `fk_incident_reports_incident` FOREIGN KEY (`incident_id`) REFERENCES `incidents` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
