package gormstore

import (
	"github.com/wssto2/go-core/authz/migrations"
	"gorm.io/gorm"
)

// Migrate creates or updates the authz tables (roles, role_permissions,
// role_bindings). Call it at start-up, like audit.Migrate. The audit_logs table
// is separate: call audit.Migrate too when the store is built WithAudit.
func Migrate(db *gorm.DB) error { return MigrateWithPrefix(db, "") }

// MigrateWithPrefix is Migrate for a store built WithTablePrefix(prefix).
func MigrateWithPrefix(db *gorm.DB, prefix string) error {
	n := namesFor(prefix)
	for _, t := range []struct {
		name  string
		model any
	}{{n.roles, &roleModel{}}, {n.perms, &rolePermissionModel{}}, {n.bindings, &roleBindingModel{}}} {
		if err := db.Table(t.name).AutoMigrate(t.model); err != nil {
			return err
		}
	}
	return nil
}

// MySQLSchemaWithPrefix is MySQLSchema for the tables named with prefix.
func MySQLSchemaWithPrefix(prefix string) string { return migrations.PrefixSQL(MySQLSchema, prefix) }

// MySQLSchema is the same schema as SQL for MariaDB 10.3+ and MySQL, for
// applications that run their migrations as SQL scripts instead of calling
// Migrate. Keep it in step with the models (a test compares the columns).
const MySQLSchema = `
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

CREATE TABLE IF NOT EXISTS role_permissions (
  role_id INT NOT NULL,
  permission VARCHAR(100) NOT NULL,
  qualifier VARCHAR(16) NOT NULL,
  PRIMARY KEY (role_id, permission)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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
`
