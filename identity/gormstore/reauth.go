package gormstore

import (
	"context"
	"errors"
	"math"

	"github.com/wssto2/go-core/identity/account"
	"gorm.io/gorm"
)

// Find implements account.ReauthStore.
func (s *Reauth) Find(ctx context.Context, accountID int) (account.Attempts, error) {
	var m reauthModel

	err := s.db.WithContext(ctx).Where("user_id = ?", accountID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return account.Attempts{}, account.ErrReauthNotFound
	}

	if err != nil {
		return account.Attempts{}, err
	}

	a := account.Attempts{AccountID: signed(m.UserID), Failures: int(m.Failures), UpdatedAt: m.UpdatedAt.UTC()}

	if m.LockedUntil != nil {
		at := m.LockedUntil.UTC()
		a.LockedUntil = &at
	}

	return a, nil
}

// Create implements account.ReauthStore: a row another request inserted first
// is refused by the primary key, which is the conflict.
func (s *Reauth) Create(ctx context.Context, a account.Attempts) error {
	m := reauthModel{UserID: unsigned32(a.AccountID), Failures: failures(a.Failures), UpdatedAt: whole(a.UpdatedAt)}
	if a.LockedUntil != nil {
		at := whole(*a.LockedUntil)
		m.LockedUntil = &at
	}

	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		// The driver's error for a duplicate key differs, so ask the table.
		var n int64
		if s.db.WithContext(ctx).Model(&reauthModel{}).Where("user_id = ?", a.AccountID).Count(&n).Error == nil && n > 0 {
			return account.ErrReauthConflict
		}

		return err
	}

	return nil
}

// Save implements account.ReauthStore: a compare-and-set on the counter as it
// was read, which InnoDB evaluates against the latest committed row.
func (s *Reauth) Save(ctx context.Context, a, read account.Attempts) error {
	q := s.db.WithContext(ctx).Model(&reauthModel{}).Where("user_id = ? AND failures = ?", read.AccountID, read.Failures)

	if read.LockedUntil == nil {
		q = q.Where("locked_until IS NULL")
	} else {
		q = q.Where("locked_until = ?", whole(*read.LockedUntil))
	}

	tx := q.Updates(map[string]any{"failures": a.Failures, "locked_until": wholeOrNil(a.LockedUntil), "updated_at": whole(a.UpdatedAt)})
	if tx.Error != nil {
		return tx.Error
	}

	if tx.RowsAffected == 0 {
		return account.ErrReauthConflict
	}

	return nil
}

func failures(n int) uint16 {
	if n < 0 || n > math.MaxUint16 {
		return math.MaxUint16
	}

	return uint16(n)
}
