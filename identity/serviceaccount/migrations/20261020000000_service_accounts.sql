-- +goose Up
-- service accounts: systems that only ever call the API (a DMS export, a portal feed). No password, never signs
-- in; gets roles at a place like a person, through authz role_bindings with subject_kind = 'service'.
--   tenant_id  NULL = the organization's account; an account of a tenant can only be bound within that tenant.
--   owner_id   the person (an identity account) answerable for it, told when its keys expire or go unused.
CREATE TABLE IF NOT EXISTS service_accounts (
  id INT NOT NULL AUTO_INCREMENT,
  name VARCHAR(100) NOT NULL,
  description VARCHAR(500) NOT NULL DEFAULT '',
  tenant_id INT NULL,
  owner_id INT NOT NULL,
  created_by INT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_by INT NOT NULL DEFAULT 0,
  updated_at DATETIME NULL,
  deactivated_at DATETIME NULL,
  deactivated_by INT NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_service_accounts_tenant (tenant_id),
  KEY idx_service_accounts_owner (owner_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
