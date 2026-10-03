-- +goose Up
-- identity: the people who sign in. login is stored lower-case and is unique; locale is a BCP-47
-- tag ("hr", "en"); password_hash is whatever the application's PasswordHasher made (bcrypt by default).
CREATE TABLE IF NOT EXISTS accounts (
  id INT NOT NULL AUTO_INCREMENT,
  login VARCHAR(100) NOT NULL,
  email VARCHAR(255) NOT NULL,
  name VARCHAR(150) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  locale VARCHAR(16) NOT NULL,
  active TINYINT(1) NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_accounts_login (login)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
