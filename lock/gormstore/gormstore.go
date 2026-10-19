// Package gormstore is the SQL store of the lock package: GORM over the table of lock/migrations, MariaDB 10.3
// and MySQL safe (an UPDATE, then an INSERT that treats a duplicate key as "not taken"; no SKIP LOCKED, no
// RETURNING). It passes storetest.
package gormstore

import (
	"context"
	"fmt"
	"time"

	"github.com/wssto2/go-core/database"
	"gorm.io/gorm"
)

// model mirrors the migration file (schema_test.go holds them equal).
type model struct {
	LockKey    string    `gorm:"primaryKey;size:190;autoIncrement:false"`
	Owner      string    `gorm:"size:100;not null"`
	LeaseUntil time.Time `gorm:"type:datetime(3);not null"`
}

func (model) TableName() string { return "locks" }

// Migrate creates the table from the model, for tests on SQLite; production runs the file in migrations.
func Migrate(db *gorm.DB) error {
	err := db.AutoMigrate(new(model))
	if err != nil {
		return fmt.Errorf("lock: migrate: %w", err)
	}

	return nil
}

// Store is the SQL store of leases (a lock.Store; storetest holds it to that). It always uses its own connection,
// never the transaction of the request: a lease is taken and released on its own, outside any business transaction.
type Store struct{ db *gorm.DB }

// New returns the store over db.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// attempts bounds the retries of an acquire that InnoDB chose as the victim of a deadlock.
const attempts = 5

// Acquire implements lock.Store. Two first acquires of a key that does not exist yet can deadlock on InnoDB (each
// UPDATE takes a gap lock, then both INSERT); the loser wrote nothing, so it starts again and finds the winner's row.
func (s *Store) Acquire(ctx context.Context, key, owner string, now, until time.Time) (bool, error) {
	for range attempts - 1 {
		got, err := s.acquire(ctx, key, owner, now, until)
		if !database.IsDeadlock(err) {
			return got, err
		}
	}

	return s.acquire(ctx, key, owner, now, until)
}

// Refresh implements lock.Store.
func (s *Store) Refresh(ctx context.Context, key, owner string, until time.Time) (bool, error) {
	res := s.db.WithContext(ctx).Model(new(model)).
		Where("lock_key = ? AND owner = ?", key, owner).
		Update("lease_until", until)
	if res.Error != nil {
		return false, fmt.Errorf("lock: refresh: %w", res.Error)
	}

	if res.RowsAffected > 0 {
		return true, nil
	}

	return s.mine(ctx, key, owner) // an UPDATE that changed nothing reports 0 without clientFoundRows
}

// Release implements lock.Store.
func (s *Store) Release(ctx context.Context, key, owner string) error {
	err := s.db.WithContext(ctx).Where("lock_key = ? AND owner = ?", key, owner).Delete(new(model)).Error
	if err != nil {
		return fmt.Errorf("lock: release: %w", err)
	}

	return nil
}

// mine reports whether the owner holds the row, whatever its lease.
func (s *Store) mine(ctx context.Context, key, owner string) (bool, error) {
	var count int64

	err := s.db.WithContext(ctx).Model(new(model)).Where("lock_key = ? AND owner = ?", key, owner).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("lock: read: %w", err)
	}

	return count > 0, nil
}

// acquire is one attempt of Acquire.
func (s *Store) acquire(ctx context.Context, key, owner string, now, until time.Time) (bool, error) {
	// The common path, a heartbeat: update the row when it is mine or its lease ran out. Inserting first would log
	// a failed INSERT on every heartbeat.
	res := s.db.WithContext(ctx).Model(new(model)).
		Where("lock_key = ? AND (owner = ? OR lease_until < ?)", key, owner, now).
		Updates(map[string]any{"owner": owner, "lease_until": until})
	if res.Error != nil {
		return false, fmt.Errorf("lock: acquire: %w", res.Error)
	}

	if res.RowsAffected > 0 {
		return true, nil
	}

	// Without clientFoundRows an UPDATE that changes nothing reports 0: the lease may be mine and unchanged.
	held, err := s.mine(ctx, key, owner)
	if err != nil || held {
		return held, err
	}

	// Nothing to take over: the row is free to create, or held by a live owner (the duplicate key).
	row := &model{LockKey: key, Owner: owner, LeaseUntil: until}

	created, err := database.CreateUnlessDuplicate(s.db.WithContext(ctx), row)
	if err != nil {
		return false, fmt.Errorf("lock: acquire: %w", err)
	}

	return created, nil
}
