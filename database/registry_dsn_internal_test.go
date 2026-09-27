package database

import (
	"strings"
	"testing"
)

func testMySQLConfig() ConnectionConfig {
	var cfg ConnectionConfig

	cfg.Username = "u"
	cfg.Password = "p"
	cfg.Host = "127.0.0.1"
	cfg.Port = "3306"
	cfg.Database = "db"

	return cfg
}

// An UPDATE that writes a row's current values must still count as affecting
// it, or RowsAffected == 0 reads as "not found" for a row that exists.
func TestMySQLDSNReportsMatchedRows(t *testing.T) {
	t.Parallel()

	dsn := mysqlDSN(testMySQLConfig())

	if !strings.Contains(dsn, "clientFoundRows=true") {
		t.Fatalf("dsn %q lacks clientFoundRows=true", dsn)
	}

	for _, param := range []string{"charset=utf8mb4", "parseTime=true", "loc=Local"} {
		if !strings.Contains(dsn, param) {
			t.Fatalf("dsn %q lost %s", dsn, param)
		}
	}
}

func TestMySQLDSNKeepsTheSQLMode(t *testing.T) {
	t.Parallel()

	cfg := testMySQLConfig()
	cfg.SQLMode = "NO_ENGINE_SUBSTITUTION"

	dsn := mysqlDSN(cfg)

	if !strings.HasSuffix(dsn, "&sql_mode=%27NO_ENGINE_SUBSTITUTION%27") {
		t.Fatalf("dsn %q: sql_mode not appended as before", dsn)
	}
}
