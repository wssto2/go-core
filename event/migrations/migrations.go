// Package migrations holds the goose migrations of the event queue's two
// tables, as MariaDB 10.3 / MySQL DDL: outbox_events (event.OutboxEvent, the
// DDL an application that already ran the outbox has) and
// event_consumer_attempts (each consumer's lease, retries and dead letters).
// gocore's App.Events registers them with its Schema; an application that wants
// the tables on their own passes them to app.Migrations:
//
//	app.Migrations(migrations.Files)
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
