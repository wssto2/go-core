package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// closeFailure is a database/sql driver whose connections fail to close with err.
type closeFailure struct{ err error }

func (d closeFailure) Open(string) (driver.Conn, error) { return failingConn(d), nil }

type failingConn struct{ err error }

func (failingConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c failingConn) Close() error                      { return c.err }
func (failingConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }

func failingOnClose(t *testing.T, name string, err error) *gorm.DB {
	t.Helper()

	sql.Register("closefail-"+name, closeFailure{err: err})

	pool, openErr := sql.Open("closefail-"+name, "")
	if openErr != nil {
		t.Fatal(openErr)
	}

	// A connection must be open for Close to have one to fail on.
	conn, connErr := pool.Conn(context.Background())
	if connErr != nil {
		t.Fatal(connErr)
	}

	_ = conn.Raw(func(any) error { return nil })
	_ = conn.Close()

	db, gormErr := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{})
	if gormErr != nil {
		t.Fatal(gormErr)
	}

	return db
}

func TestCloseAllJoinsEveryError(t *testing.T) {
	errA, errB := errors.New("a is stuck"), errors.New("b is stuck")

	reg := NewRegistry(slog.New(slog.DiscardHandler), RegistryConfig{})
	reg.AddConnection("a", failingOnClose(t, "a", errA))
	reg.AddConnection("b", failingOnClose(t, "b", errB))

	err := reg.CloseAll()
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Fatalf("CloseAll must keep both causes inspectable, got %v", err)
	}
}

func TestUnsupportedDriverListsTheSupportedOnes(t *testing.T) {
	reg := NewRegistry(slog.New(slog.DiscardHandler), RegistryConfig{})

	err := reg.Register(ConnectionConfig{Name: "bad", Driver: "postgres", Database: "db"})
	if err == nil || !strings.Contains(err.Error(), `"postgres"`) || !strings.Contains(err.Error(), DriverMySQL) || !strings.Contains(err.Error(), DriverSQLite) {
		t.Fatalf("want the driver and the supported ones named, got %v", err)
	}
}

func TestConnMaxLifetimeIsADuration(t *testing.T) {
	cfg := ConnectionConfig{ConnMaxLifetime: 90 * time.Second}
	p := &fakePool{}
	ApplyPoolSettings(p, cfg.withDefaults())

	if p.lt != 90*time.Second {
		t.Fatalf("lifetime %v", p.lt)
	}

	if got := (ConnectionConfig{}).withDefaults().ConnMaxLifetime; got != 5*time.Minute {
		t.Fatalf("default %v", got)
	}
}
