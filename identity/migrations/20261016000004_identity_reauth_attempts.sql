-- +goose Up
-- identity: the password re-confirmation counter (IAM-REAUTH-001). A profile asks for the current password
-- before a password or e-mail change; one row per account counts the attempts since the last correct
-- password, the 5th locks re-confirmation for 15 minutes, and while locked no password is checked. A
-- correct password clears the count. The same table as arv-next's (only the comments name accounts), so an
-- application that already has it adopts this file with MarkApplied.
CREATE TABLE IF NOT EXISTS `user_reauth_attempts` (
  `user_id`      int unsigned      NOT NULL COMMENT 'FK: accounts.id - the account being re-confirmed',
  `failures`     smallint unsigned NOT NULL DEFAULT 0 COMMENT 'Attempts since the last correct password; counted before the check',
  `locked_until` datetime          NULL     COMMENT 'Re-confirmation refused until then; set by the 5th attempt, cleared by a correct password',
  `updated_at`   datetime          NOT NULL,
  PRIMARY KEY (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3;
