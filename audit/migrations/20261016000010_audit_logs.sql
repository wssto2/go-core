-- +goose Up
-- audit: go-core's audit trail (audit.AuditLog), one row per recorded change: what kind of thing
-- (entity_type) and which one (entity_id), what was done (action), by whom (actor_id), and the
-- before and after states as JSON. The columns are what audit.Migrate (GORM) creates, so a database
-- that already has the table from it adopts this file as it is; the two indexes serve the readers
-- of a record's history (by entity) and of a person's actions (by actor).
CREATE TABLE IF NOT EXISTS `audit_logs` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `entity_type` longtext,
  `entity_id` bigint DEFAULT NULL,
  `action` longtext,
  `actor_id` bigint DEFAULT NULL,
  `metadata` longblob,
  `before_state` longblob,
  `after_state` longblob,
  `changed_fields` longblob,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_audit_logs_actor_created` (`actor_id`, `created_at`),
  KEY `idx_audit_logs_entity` (`entity_type`(32), `entity_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
