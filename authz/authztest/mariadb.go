package authztest

import (
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// MariaDBEnv names the environment variable holding a DSN for a real MariaDB
// (10.3 is what the production targets run), for the integration tests that
// check SQL a SQLite run cannot: for example
//
//	AUTHZ_MARIADB_DSN='root:authzpw@tcp(127.0.0.1:33063)/authz?parseTime=true&charset=utf8mb4&loc=UTC'
const MariaDBEnv = "AUTHZ_MARIADB_DSN"

// MariaDB opens the database named by MariaDBEnv. It reports false when the
// variable is unset, so the caller can skip; a set but unusable DSN fails the
// test. The connection is closed when the test ends.
//
// The caller owns the schema: drop and create the tables it needs, because tests
// share the one database and run one after another.
func MariaDB(tb testing.TB) (*gorm.DB, bool) {
	tb.Helper()
	dsn := os.Getenv(MariaDBEnv)
	if dsn == "" {
		return nil, false
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		tb.Fatalf("authztest: open %s: %v", MariaDBEnv, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatalf("authztest: %v", err)
	}
	tb.Cleanup(func() { _ = sqlDB.Close() })
	if err := sqlDB.Ping(); err != nil {
		tb.Fatalf("authztest: ping %s: %v", MariaDBEnv, err)
	}
	return db, true
}
