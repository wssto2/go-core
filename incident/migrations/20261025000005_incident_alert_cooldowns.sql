-- +goose Up
-- incident_alert_cooldowns: when an immediate alert of each severity was last sent, so a restart does not reset the cooldown.
-- The DDL is what an application that grew the same tables under other names has once it renames them, so it can
-- adopt this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `incident_alert_cooldowns` (
  `severity_tier` varchar(20) COLLATE utf8mb3_unicode_ci NOT NULL,
  `last_sent_at` datetime(3) NOT NULL,
  PRIMARY KEY (`severity_tier`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
