package database

import (
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
)

// Connection names a database connection of the Registry. Declare each one
// once, where the application configures its databases, and pass the value
// around instead of a string:
//
//	const Shared database.Connection = "shared"
//
//	db, err := reg.Database(Shared)
//
// Registry.Get keeps taking the plain string name.
type Connection string

// String returns the registered name.
func (c Connection) String() string { return string(c) }

// Database returns the pool registered under conn. An unknown connection
// reports the registered ones, so a typo is fixed from the message alone; the
// error still matches ErrConnectionNotFound with errors.As.
func (r *Registry) Database(conn Connection) (*gorm.DB, error) {
	db, err := r.Get(string(conn))
	if err != nil {
		names := r.Names()
		slices.Sort(names)

		registered := "none"
		if len(names) > 0 {
			registered = strings.Join(names, ", ")
		}

		return nil, fmt.Errorf("%w (registered connections: %s)", err, registered)
	}

	return db, nil
}

// Unavailable returns a *gorm.DB whose every query fails with err. It stands
// in for a connection that could not be resolved while an application is still
// being assembled, so the failure is reported once, with the rest of the
// start-up problems, instead of as a nil pointer in the middle of wiring.
func Unavailable(err error) *gorm.DB {
	db, openErr := openSQLiteMemory()
	if openErr != nil {
		return &gorm.DB{Config: &gorm.Config{}, Error: err}
	}

	failed := db.Session(&gorm.Session{})
	_ = failed.AddError(err)

	return failed
}
