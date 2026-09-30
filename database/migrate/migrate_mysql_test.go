package migrate_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/migrate"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestMySQL runs Up, MarkApplied and Status against a real MySQL / MariaDB, which
// adds goose's table lock. It needs an empty scratch database:
//
//	GOCORE_MYSQL_TEST_DSN='user:pass@tcp(127.0.0.1:3306)/gocore_migrate_test?charset=utf8mb4' go test ./database/migrate/
func TestMySQL(t *testing.T) {
	dsn := os.Getenv("GOCORE_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("GOCORE_MYSQL_TEST_DSN not set")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	noParseTime := database.NewRegistry(slog.Default(), database.RegistryConfig{})
	noParseTime.AddConnection("local", db)

	if err := migrate.New(noParseTime, fstest.MapFS{"local/1_a.sql": file("SELECT 1;")}, slog.Default()).Up(context.Background()); err == nil {
		t.Fatal("want an error for a DSN without parseTime=true, not a five-minute wait")
	}

	if db, err = gorm.Open(mysql.Open(dsn+"&parseTime=true"), &gorm.Config{}); err != nil {
		t.Fatal(err)
	}

	cleanup := func() {
		for _, table := range []string{"a", "b", "c", "goose_db_version", "goose_lock"} {
			db.Exec("DROP TABLE IF EXISTS `" + table + "`")
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	reg := database.NewRegistry(slog.Default(), database.RegistryConfig{})
	reg.AddConnection("local", db)

	local := fstest.MapFS{}
	for name, f := range migrations {
		if !strings.HasPrefix(name, "shared/") {
			local[name] = f
		}
	}

	ctx := context.Background()
	m := migrate.New(reg, local, slog.Default())

	if err := db.Exec("CREATE TABLE b (id INTEGER)").Error; err != nil {
		t.Fatal(err)
	}

	if err := m.MarkApplied(ctx, "local", []int64{20260102000000}); err != nil {
		t.Fatal(err)
	}

	if err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}

	statuses, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range statuses {
		if !s.Applied {
			t.Errorf("%s is pending", s.File)
		}
	}
}
