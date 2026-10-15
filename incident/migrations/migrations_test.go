//nolint:paralleltest // tests in the style of the package's others
package migrations_test

import (
	"fmt"
	"testing"

	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/incident/migrations"
)

func TestMigrationsAreMariaDB103Portable(t *testing.T) {
	dbtest.RequirePortable(t, migrations.Files)
}

func ExampleFiles() {
	entries, _ := migrations.Files.ReadDir(".")
	for _, e := range entries {
		fmt.Println(e.Name())
	}
	// Output:
	// 20261025000000_incidents.sql
	// 20261025000001_incident_occurrences.sql
	// 20261025000002_incident_reports.sql
	// 20261025000003_incident_comments.sql
	// 20261025000004_incident_watchers.sql
	// 20261025000005_incident_alert_cooldowns.sql
	// 20261025000006_incident_digest_queue.sql
	// 20261025000007_incidents_client_error_source.sql
	// 20261025000008_incident_digest_queue_digested.sql
}
