-- +goose Up
-- authz: who has which role where. role_id names a custom role, role_key a predefined one:
-- exactly one of them is set (the CHECK; the other holds 0 or '', never NULL, so the unique
-- index also refuses duplicates). scope_level is organization, dealer or location.
CREATE TABLE IF NOT EXISTS role_bindings (
  id INT NOT NULL AUTO_INCREMENT,
  subject_kind VARCHAR(16) NOT NULL,
  subject_id INT NOT NULL,
  role_id INT NOT NULL DEFAULT 0,
  role_key VARCHAR(64) NOT NULL DEFAULT '',
  scope_level VARCHAR(32) NOT NULL,
  scope_id INT NOT NULL DEFAULT 0,
  created_by INT NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_role_bindings (subject_kind, subject_id, role_id, role_key, scope_level, scope_id),
  KEY idx_role_bindings_subject (subject_kind, subject_id),
  KEY idx_role_bindings_role (role_id),
  CONSTRAINT chk_role_bindings_role CHECK ((role_id > 0 AND role_key = '') OR (role_id = 0 AND role_key <> ''))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
