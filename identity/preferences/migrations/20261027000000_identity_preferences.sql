-- +goose Up
-- identity: a person's preferences, one JSON object per account (dark mode, dismissed tips, whatever the client
-- remembers between devices). The object is capped by the application (16 KiB by default), so LONGTEXT is room
-- to raise the cap, not a promise of size. No foreign key: accounts may live in the application's own table.
--   account_id   accounts.id
--   preferences  the JSON object
CREATE TABLE IF NOT EXISTS account_preferences (
  account_id INT NOT NULL,
  preferences LONGTEXT NOT NULL,
  PRIMARY KEY (account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
