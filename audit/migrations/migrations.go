// Package migrations holds the goose migration of the audit_logs table, as
// MariaDB 10.3 / MySQL DDL, equal to what audit.Migrate creates from
// audit.AuditLog plus two indexes for reading a record's history. The modules
// that write an audit trail (identity for account changes, access for role
// changes) register it with their Schema, and an application that wants the
// table on its own passes it to app.Migrations:
//
//	app.Migrations(migrations.Files)
//
// The application collects each FS once: registering this one from two features
// is the same as registering it once.
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
