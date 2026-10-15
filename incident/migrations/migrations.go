// Package migrations holds the goose migrations of the incident module, as MariaDB 10.3 / MySQL DDL: the seven tables
// (incidents, incident_occurrences, incident_reports, incident_comments, incident_watchers, incident_alert_cooldowns
// and incident_digest_queue) and two changes to them. incident.Install registers them; an application that wants the
// tables on its own passes them to app.Migrations:
//
//	app.Migrations(migrations.Files)
//
// The first seven files create the tables exactly as an application that grew them under other names has them after
// renaming them, so such an application renames its tables and adopts the seven versions with MarkApplied; the last
// two files then run as usual. Versions are the release date; a released file is never edited, a change is a new file.
package migrations

import "embed"

// Files are the migrations, flat at the root of the FS.
//
//go:embed *.sql
var Files embed.FS
