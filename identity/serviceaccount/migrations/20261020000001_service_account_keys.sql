-- +goose Up
-- service account API keys. Only the SHA-256 of the secret is stored (key_hash, hex); prefix is the first
-- characters of the secret, kept for display. A key stops working when revoked_at is set, when expires_at has
-- passed, or when its account is deactivated.
CREATE TABLE IF NOT EXISTS service_account_keys (
  id INT NOT NULL AUTO_INCREMENT,
  service_account_id INT NOT NULL,
  name VARCHAR(100) NOT NULL,
  prefix VARCHAR(16) NOT NULL,
  key_hash CHAR(64) NOT NULL,
  created_by INT NOT NULL,
  created_at DATETIME NOT NULL,
  last_used_at DATETIME NULL,
  expires_at DATETIME NULL,
  revoked_at DATETIME NULL,
  revoked_by INT NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uq_service_account_keys_hash (key_hash),
  KEY idx_service_account_keys_account (service_account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
