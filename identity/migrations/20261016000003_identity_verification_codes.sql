-- +goose Up
-- identity: one-time codes (IAM-OTP-001..004). A code proves a person controls a mailbox before an action
-- that depends on it takes effect: today confirming a new e-mail address, later a password reset. The
-- plain code is never stored: code_hash is an HMAC over user_id|purpose|code keyed by the application's
-- secret. At most one live code exists per (user_id, purpose); MySQL has no partial unique index, so the
-- service ends the previous one in the transaction that inserts the new. The same table as arv-next's
-- (only the comments name accounts), so an application that already has it adopts this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `user_verification_codes` (
  `id`             bigint unsigned  NOT NULL AUTO_INCREMENT,
  `user_id`        int unsigned     NOT NULL COMMENT 'FK: accounts.id - the account the code belongs to',
  `purpose`        varchar(32)      NOT NULL COMMENT 'What the code authorizes: email_change, password_reset',
  `target`         varchar(255)     NULL     COMMENT 'Value the code confirms, e.g. the new email for email_change; NULL when the purpose needs none',
  `code_hash`      char(64)         NOT NULL COMMENT 'Hex HMAC-SHA256 of user_id|purpose|code; never the plain code',
  `attempts`       tinyint unsigned NOT NULL DEFAULT 0 COMMENT 'Wrong guesses so far; the code dies at the limit',
  `expires_at`     datetime         NOT NULL,
  `consumed_at`    datetime         NULL     COMMENT 'Set when the code was verified successfully',
  `invalidated_at` datetime         NULL     COMMENT 'Set when superseded, cancelled or out of attempts',
  `created_ip`     varchar(45)      NULL     COMMENT 'Client IP that requested the code (IPv6 max length)',
  `created_at`     datetime         NOT NULL COMMENT 'Also the send time: resend cooldown and hourly cap count from it',
  `updated_at`     datetime         NOT NULL,
  PRIMARY KEY (`id`),
  KEY `user_verification_codes_user_purpose_created` (`user_id`, `purpose`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3;
