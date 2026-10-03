-- +goose Up
-- identity: one row per sign-in event of a person (signed_in, wrong_password, locked_out, refused_inactive,
-- signed_in_as, unlocked, signed_out_everywhere, session_revoked). actor_id is who did it when it was not
-- the person. The lock after wrong passwords is derived from these rows, never stored. The same table
-- as arv-next's, so an application that already has it adopts this file with MarkApplied.
CREATE TABLE IF NOT EXISTS user_signins (
  id INT NOT NULL AUTO_INCREMENT,
  user_id INT NOT NULL,
  `event` VARCHAR(32) NOT NULL,
  ip VARCHAR(45) NOT NULL DEFAULT '',
  user_agent VARCHAR(255) NOT NULL DEFAULT '',
  actor_id INT NULL,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (id),
  KEY idx_user_signins_user (user_id, id),
  KEY idx_user_signins_event_created (`event`, created_at),
  KEY idx_user_signins_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
