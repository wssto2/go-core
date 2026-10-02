package database_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wssto2/go-core/database"
)

const shared database.Connection = "shared"

func ExampleRegistry_Database() {
	reg, cleanup := database.NewTestRegistry("local", "shared")
	defer func() { _ = cleanup() }()

	if _, err := reg.Database(shared); err == nil {
		fmt.Println("shared is registered")
	}

	_, err := reg.Database("sharde")
	fmt.Println(err != nil)
	// Output:
	// shared is registered
	// true
}

func TestRegistryDatabaseUnknownListsRegistered(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("local", "shared")
	defer func() { _ = cleanup() }()

	_, err := reg.Database("sharde")
	if err == nil {
		t.Fatal("want error for unknown connection")
	}

	var notFound database.ErrConnectionNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("want ErrConnectionNotFound, got %T", err)
	}

	if !strings.Contains(err.Error(), "local, shared") {
		t.Fatalf("error should list registered connections, got %q", err)
	}
}

func TestRegistryGetKeepsStringForm(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("local")
	defer func() { _ = cleanup() }()

	if _, err := reg.Get("local"); err != nil {
		t.Fatal(err)
	}
}
