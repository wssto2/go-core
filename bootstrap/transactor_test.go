package bootstrap

import (
	"testing"

	"github.com/wssto2/go-core/database"
)

func TestResolveTransactor(t *testing.T) {
	reg, cleanup := database.NewTestRegistry("primary")
	defer func() { _ = cleanup() }()
	c := NewContainer()
	OverwriteBind(c, reg)

	tr, err := ResolveTransactor(c, "primary")
	if err != nil {
		t.Fatalf("resolve transactor: %v", err)
	}
	if tr == nil {
		t.Fatalf("expected transactor, got nil")
	}
}
