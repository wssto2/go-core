package migrate_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing/fstest"

	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/migrate"
)

// A module ships a flat directory of migration files and Add hands it over
// with the connection it lives on; the application's own migrations keep one
// directory per connection. All of them run as one set.
func ExampleMigrator_Add() {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	app := fstest.MapFS{"local/20260101000000_orders.sql": file("CREATE TABLE orders (id INTEGER);")}
	module := fstest.MapFS{"20261015000000_roles.sql": file("CREATE TABLE roles (id INTEGER);")}

	m := migrate.New(reg, app, slog.New(slog.DiscardHandler)).Add("", module) // "" is the primary connection

	pending, _ := m.Pending(context.Background())
	fmt.Println("pending:", len(pending))

	_ = m.Up(context.Background())
	pending, _ = m.Pending(context.Background())
	fmt.Println("pending after Up:", len(pending))
	// Output:
	// pending: 2
	// pending after Up: 0
}
