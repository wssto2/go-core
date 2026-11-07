-- +goose Up
-- characteristics_level2: the second level; level1_id is its parent.
CREATE TABLE IF NOT EXISTS `characteristics_level2` (
  `id`             bigint unsigned NOT NULL AUTO_INCREMENT,
  `level1_id`      bigint unsigned NOT NULL,
  `name`           varchar(255)    NOT NULL COMMENT 'Name in the default language',
  `name_i18n`      longtext                    NULL COMMENT 'JSON: {"hr": "...", "en": "..."}',
  `meta`           longtext                    NULL COMMENT 'JSON object',
  `order`          int unsigned    NOT NULL DEFAULT 1 COMMENT 'Position among the siblings, from 1',
  `active`         tinyint(1)      NOT NULL DEFAULT 0,
  `created_by`     int unsigned        NULL,
  `updated_by`     int unsigned        NULL,
  `activated_by`   int unsigned        NULL,
  `deactivated_by` int unsigned        NULL,
  `created_at`     datetime            NULL,
  `updated_at`     datetime            NULL,
  `activated_at`   datetime            NULL,
  `deactivated_at` datetime            NULL,
  PRIMARY KEY (`id`),
  KEY `characteristics_level2_level1_id_index` (`level1_id`),
  KEY `characteristics_level2_order_index` (`order`),
  KEY `characteristics_level2_active_index` (`active`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
