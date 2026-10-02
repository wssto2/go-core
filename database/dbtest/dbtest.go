// Package dbtest runs a test on every database a store has to work on: SQLite
// always, MySQL and MariaDB when their DSNs are set.
//
//	func TestStore(t *testing.T) {
//	    dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
//	        // db is empty and private to this run
//	    })
//	}
//
// Each target is a subtest (sqlite, mysql, mariadb) that skips with a message
// naming the missing variable:
//
//	GOCORE_MYSQL_DSN='root:pw@tcp(127.0.0.1:3306)/'   MySQL 9.x
//	GOCORE_MARIADB_DSN='root:pw@tcp(127.0.0.1:3310)/' MariaDB 10.3 (what production runs)
//
// A server target never touches an existing schema: the run creates a
// throw-away schema on that server, gives the test a connection to it and drops
// it when the test ends. The DSN's user therefore needs CREATE and DROP on
// databases; the database name inside the DSN is only where the connection
// starts. The test may run DDL and use several connections (concurrency
// tests), because the schema is its own.
package dbtest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Target is a database kind a test can run on.
type Target struct {
	// Name is the subtest name.
	Name string
	// Env is the environment variable holding the DSN; empty for SQLite.
	Env string
}

// The targets Run goes through.
var (
	SQLite  = Target{Name: "sqlite"}
	MySQL   = Target{Name: "mysql", Env: "GOCORE_MYSQL_DSN"}
	MariaDB = Target{Name: "mariadb", Env: "GOCORE_MARIADB_DSN"}
)

// Run calls fn once per target, each in its own subtest with an empty database
// of its own. A server target whose DSN is not set skips.
func Run(t *testing.T, fn func(t *testing.T, db *gorm.DB), targets ...Target) {
	t.Helper()

	if len(targets) == 0 {
		targets = []Target{SQLite, MySQL, MariaDB}
	}

	for _, target := range targets {
		t.Run(target.Name, func(t *testing.T) {
			db, ok := Open(t, target)
			if !ok {
				t.Skipf("%s is not set: set it to a %s server to run this test there", target.Env, target.Name)
			}

			fn(t, db)
		})
	}
}

// Open returns an empty database of the target's kind, closed (and a server
// schema dropped) when the test ends. It reports false when the target's DSN
// variable is unset, so the caller can skip; a set but unusable DSN fails the
// test.
func Open(tb testing.TB, target Target) (*gorm.DB, bool) {
	tb.Helper()

	if target.Env == "" {
		return openSQLite(tb), true
	}

	dsn := os.Getenv(target.Env)
	if dsn == "" {
		return nil, false
	}

	return Scratch(tb, dsn), true
}

func openSQLite(tb testing.TB) *gorm.DB {
	tb.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		tb.Fatalf("dbtest: open sqlite: %v", err)
	}

	// An in-memory SQLite database is private to one connection.
	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatalf("dbtest: %v", err)
	}

	sqlDB.SetMaxOpenConns(1)
	tb.Cleanup(func() { _ = sqlDB.Close() })

	return db
}

// Scratch creates a throw-away schema on the MySQL or MariaDB server the DSN
// names, returns a connection to it and drops the schema when the test ends.
// The DSN's own database is not touched. Use it when a DSN comes from
// somewhere other than the GOCORE_* variables.
func Scratch(tb testing.TB, dsn string) *gorm.DB {
	tb.Helper()

	dialector, ok := mysql.Open(dsn).(*mysql.Dialector)
	if !ok || dialector.DSNConfig == nil {
		tb.Fatalf("dbtest: the DSN is not a MySQL DSN of the form user:password@tcp(host:port)/")
	}

	cfg := dialector.DSNConfig.Clone()
	cfg.ParseTime = true // goose's lock reads DATETIME columns
	cfg.DBName = ""

	admin := openServer(tb, cfg.FormatDSN())

	var token [6]byte
	if _, err := rand.Read(token[:]); err != nil {
		tb.Fatalf("dbtest: %v", err)
	}

	schema := "gocore_test_" + hex.EncodeToString(token[:])

	if err := admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4").Error; err != nil {
		tb.Fatalf("dbtest: creating a throw-away schema needs CREATE on databases: %v", err)
	}

	cfg.DBName = schema
	db := openServer(tb, cfg.FormatDSN())

	tb.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}

		// Only the schema this call created: its name is generated above.
		if !strings.HasPrefix(schema, "gocore_test_") {
			return
		}

		if err := admin.Exec("DROP DATABASE IF EXISTS `" + schema + "`").Error; err != nil {
			tb.Logf("dbtest: dropping %s: %v", schema, err)
		}
	})

	return db
}

func openServer(tb testing.TB, dsn string) *gorm.DB {
	tb.Helper()

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		tb.Fatalf("dbtest: %v", redact(err, dsn))
	}

	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatalf("dbtest: %v", err)
	}

	tb.Cleanup(func() { _ = sqlDB.Close() })

	if err := sqlDB.Ping(); err != nil {
		tb.Fatalf("dbtest: cannot reach the server: %v", redact(err, dsn))
	}

	return db
}

// redact keeps a DSN, which holds the password, out of failure messages.
func redact(err error, dsn string) error {
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), dsn, "<dsn>"))
}
