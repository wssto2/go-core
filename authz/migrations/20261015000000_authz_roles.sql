-- +goose Up
-- authz: custom roles. Predefined (code-defined) roles need no row.
-- `attrs` is the role's attribute constraints as a JSON object (opaque text, no JSON functions).
-- Same DDL as go-core authz/gormstore.MySQLSchema (MariaDB 10.3+); a test holds them equal.
CREATE TABLE IF NOT EXISTS roles (
  id INT NOT NULL AUTO_INCREMENT,
  name VARCHAR(100) NOT NULL,
  description VARCHAR(255) NOT NULL DEFAULT '',
  attrs TEXT NULL,
  created_by INT NOT NULL DEFAULT 0,
  updated_by INT NOT NULL DEFAULT 0,
  created_at DATETIME NULL,
  updated_at DATETIME NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
