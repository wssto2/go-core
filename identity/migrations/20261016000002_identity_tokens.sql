-- +goose Up
-- identity: sessions, as go-core's auth.Token stores them: an opaque access token, a hashed refresh
-- token, the device in name (a session opened by signing in as somebody else is named
-- "login-as:<actor id>|<device>"). The same table as arv-next's, so an application that already
-- has it adopts this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `tokens` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL,
  `token_value` varchar(255) NOT NULL,
  `name` varchar(255) DEFAULT NULL,
  `last_used_at` datetime(3) DEFAULT NULL,
  `last_used_ip` varchar(45) DEFAULT NULL,
  `expires_at` datetime(3) NOT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `refresh_prefix` varchar(8) DEFAULT NULL,
  `refresh_token` varchar(255) DEFAULT NULL,
  `revoked` tinyint(1) DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_tokens_token_value` (`token_value`),
  KEY `idx_tokens_user_id` (`user_id`),
  KEY `idx_tokens_expires_at` (`expires_at`),
  KEY `idx_tokens_refresh_prefix` (`refresh_prefix`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
