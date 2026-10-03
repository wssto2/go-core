-- +goose Up
-- authz: a role's grants. qualifier is all, own_location or own.
CREATE TABLE IF NOT EXISTS role_permissions (
  role_id INT NOT NULL,
  permission VARCHAR(100) NOT NULL,
  qualifier VARCHAR(16) NOT NULL,
  PRIMARY KEY (role_id, permission)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
