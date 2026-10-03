package gormstore_test

import (
	"context"
	"fmt"
	"log"
	"testing"

	"github.com/wssto2/go-core/audit"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
)

// exampleT lets an Example open a throw-away database, which wants a testing.TB.
type exampleT struct {
	testing.TB
	cleanups []func()
}

func (*exampleT) Helper()                      {}
func (t *exampleT) Cleanup(f func())           { t.cleanups = append(t.cleanups, f) }
func (*exampleT) Fatalf(f string, args ...any) { log.Fatalf(f, args...) }

func (t *exampleT) done() {
	for _, f := range t.cleanups {
		f()
	}
}

// What a person did is the audit rows they wrote, newest first.
func ExampleNewActivityLog() {
	t := &exampleT{}
	defer t.done()

	db, _ := dbtest.Open(t, dbtest.SQLite)
	_ = gormstore.Migrate(db)
	_ = audit.Migrate(db)

	trail, ctx := audit.NewRepository(database.NewTransactor(db)), context.Background()
	_ = trail.Write(ctx, audit.NewEntry("offers", 12, 7, "create"))
	_ = trail.Write(ctx, audit.NewEntry("offers", 12, 7, "update"))

	rows, total, _ := gormstore.NewActivityLog(db).Activity(ctx, account.ActivityQuery{ActorID: 7, Limit: 10})
	for _, r := range rows {
		fmt.Println(r.RecordType, r.RecordID, r.Action)
	}

	fmt.Println(total)
	// Output:
	// offers 12 changed
	// offers 12 created
	// 2
}
