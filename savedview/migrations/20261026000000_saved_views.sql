-- +goose Up
-- saved_views: the lists' filters and search a person saved under a name, to apply them again with one click.
-- Rows belong to one person and one list; nobody else reads or writes them, so there is no tenant column: the owning
-- user_id is the whole access rule and every query filters on it.
-- The DDL is what an application that grew the same table as saved_filters has once it renames the table and its two
-- indexes, so it can adopt this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `saved_views` (
  `id`         bigint unsigned NOT NULL AUTO_INCREMENT,
  `user_id`    int unsigned    NOT NULL COMMENT 'FK: users.id - the only user who may read or change the row',
  `list_key`   varchar(100)    NOT NULL COMMENT 'Which list the filters belong to, e.g. internalmarket.catalogue',
  `name`       varchar(60)     NOT NULL COMMENT 'User-chosen label, unique per user and list',
  `state`      text            NOT NULL COMMENT 'JSON: {"filters":{...},"search":"..."} as the list sends them',
  `created_at` datetime        NOT NULL,
  `updated_at` datetime        NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `saved_views_user_list_name` (`user_id`, `list_key`, `name`),
  KEY `saved_views_user_list` (`user_id`, `list_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3;
