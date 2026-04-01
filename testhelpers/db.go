package testhelpers

import (
	"testing"

	"github.com/wssto2/go-core/database"
)

// NewTestRegistry returns an in-memory sqlite *gorm.DB and a cleanup function for tests.
func NewTestRegistry(t *testing.T, models ...any) (*database.Registry, func()) {
	t.Helper()
	reg, cleanup := database.NewTestRegistry("test")
	// Optionally migrate models into the "test" connection
	conn := reg.MustGet("test")
	if len(models) > 0 {
		if err := database.SafeMigrate(conn, models...); err != nil {
			err = cleanup()
			if err != nil {
				t.Fatalf("failed to cleanup: %v", err)
			}
			t.Fatalf("failed to migrate models: %v", err)
		}
	}
	return reg, func() { _ = cleanup() }
}
