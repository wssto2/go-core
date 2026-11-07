-- +goose Up
-- characteristics_level1: the top of the tree of editor-maintained lists (categories, brands, countries). The tables are the shape
-- site-builder's Laravel migrations created, so such a project adopts the files with MarkApplied. Extra columns belong to
-- the project's own migration and description.
CREATE TABLE IF NOT EXISTS `characteristics_level1` (
  `id`             bigint unsigned NOT NULL AUTO_INCREMENT,
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
  KEY `characteristics_level1_order_index` (`order`),
  KEY `characteristics_level1_active_index` (`active`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
