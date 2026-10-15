-- +goose Up
-- which condition of which key its owner was told about. The unique (key_id, kind) keeps each condition of each
-- key to one notice: a row is written in the transaction that publishes the notice, and a second run or a
-- concurrent one cannot write it again.
--   key_id   service_account_keys.id (no foreign key: keys are never deleted, and the table is a record)
--   kind     'expiring' or 'unused'
--   sent_at  when the notice was recorded
CREATE TABLE IF NOT EXISTS service_account_key_notices (
  id INT NOT NULL AUTO_INCREMENT,
  key_id INT NOT NULL,
  kind VARCHAR(20) NOT NULL,
  sent_at DATETIME NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_service_account_key_notices (key_id, kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
