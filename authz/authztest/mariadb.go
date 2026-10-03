package authztest

import (
	"os"
	"testing"

	"github.com/wssto2/go-core/database/dbtest"
	"gorm.io/gorm"
)

// MariaDBEnv names the variable holding a DSN for a real MariaDB (10.3 is what
// the production targets run), for the integration tests that check SQL a
// SQLite run cannot: for example
//
//	AUTHZ_MARIADB_DSN='root:authzpw@tcp(127.0.0.1:33063)/'
//
// It still works; dbtest.MariaDB.Env (GOCORE_MARIADB_DSN) is the name every
// go-core test uses now and is read when this one is unset.
const MariaDBEnv = "AUTHZ_MARIADB_DSN"

// MariaDB opens an empty throw-away schema on the server named by MariaDBEnv
// (or GOCORE_MARIADB_DSN), see dbtest.Scratch. It reports false when neither
// variable is set, so the caller can skip; a set but unusable DSN fails the
// test. The schema is dropped when the test ends, so tests may create and drop
// whatever tables they need.
func MariaDB(tb testing.TB) (*gorm.DB, bool) {
	tb.Helper()

	dsn := os.Getenv(MariaDBEnv)
	if dsn == "" {
		dsn = os.Getenv(dbtest.MariaDB.Env)
	}

	if dsn == "" {
		return nil, false
	}

	return dbtest.Scratch(tb, dsn), true
}
