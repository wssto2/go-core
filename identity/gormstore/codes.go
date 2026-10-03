package gormstore

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/wssto2/go-core/identity/account"
	"gorm.io/gorm"
)

func (m codeModel) code() account.Code {
	c := account.Code{
		ID: signed(m.ID), AccountID: signed(m.UserID), Purpose: account.Purpose(m.Purpose), Hash: m.CodeHash, Attempts: int(m.Attempts),
		ExpiresAt: m.ExpiresAt.UTC(), CreatedAt: m.CreatedAt.UTC(),
	}

	if m.Target != nil {
		c.Target = *m.Target
	}

	if m.CreatedIP != nil {
		c.IP = *m.CreatedIP
	}

	if m.ConsumedAt != nil {
		at := m.ConsumedAt.UTC()
		c.ConsumedAt = &at
	}

	if m.InvalidatedAt != nil {
		at := m.InvalidatedAt.UTC()
		c.InvalidatedAt = &at
	}

	return c
}

// signed and unsigned32 move an id between the unsigned columns arv-next made
// and the int ids of the module; a value out of range reads as zero, which no
// row has.
func signed[T uint32 | uint64](v T) int {
	if v > math.MaxInt32 {
		return 0
	}

	return int(v)
}

func unsigned32(n int) uint32 {
	if n < 0 || n > math.MaxUint32 {
		return 0
	}

	return uint32(n)
}

func orNil(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func wholeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}

	return whole(*t)
}

// Latest implements account.CodeStore.
func (s *Codes) Latest(ctx context.Context, accountID int, p account.Purpose) (account.Code, error) {
	var m codeModel

	err := dbOf(s.db, ctx).Where("user_id = ? AND purpose = ?", accountID, string(p)).Order("id DESC").Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return account.Code{}, account.ErrCodeNotFound
	}

	if err != nil {
		return account.Code{}, err
	}

	return m.code(), nil
}

// IssuedSince implements account.CodeStore.
func (s *Codes) IssuedSince(ctx context.Context, accountID int, p account.Purpose, since time.Time) (int, time.Time, error) {
	where := "user_id = ? AND purpose = ? AND created_at > ?"

	var n int64
	if err := dbOf(s.db, ctx).Model(&codeModel{}).Where(where, accountID, string(p), whole(since)).Count(&n).Error; err != nil {
		return 0, time.Time{}, err
	}

	if n == 0 {
		return 0, time.Time{}, nil
	}

	var oldest codeModel
	if err := dbOf(s.db, ctx).Where(where, accountID, string(p), whole(since)).Order("created_at ASC").Take(&oldest).Error; err != nil {
		return 0, time.Time{}, err
	}

	return int(n), oldest.CreatedAt.UTC(), nil
}

// Issue implements account.CodeStore: the live codes end and the new one is
// stored in one transaction (IAM-OTP-004).
func (s *Codes) Issue(ctx context.Context, c account.Code, now time.Time) (account.Code, error) {
	m := codeModel{
		UserID: unsigned32(c.AccountID), Purpose: string(c.Purpose), Target: orNil(c.Target), CodeHash: c.Hash,
		ExpiresAt: whole(c.ExpiresAt), CreatedIP: orNil(c.IP), CreatedAt: whole(c.CreatedAt), UpdatedAt: whole(now),
	}

	err := dbOf(s.db, ctx).Transaction(func(tx *gorm.DB) error {
		if err := ends(tx, c.AccountID, c.Purpose, now); err != nil {
			return err
		}

		return tx.Create(&m).Error
	})
	if err != nil {
		return account.Code{}, err
	}

	return m.code(), nil
}

// ends invalidates the live codes of an account and purpose.
func ends(db *gorm.DB, accountID int, p account.Purpose, now time.Time) error {
	return db.Model(&codeModel{}).
		Where("user_id = ? AND purpose = ? AND consumed_at IS NULL AND invalidated_at IS NULL", accountID, string(p)).
		Updates(map[string]any{"invalidated_at": whole(now), "updated_at": whole(now)}).Error
}

// SaveVerification implements account.CodeStore: a compare-and-set on the
// state the code was read in. InnoDB evaluates the condition against the
// latest committed row, so of two requests that read one state exactly one
// updates it; the other matches nothing.
func (s *Codes) SaveVerification(ctx context.Context, c account.Code, readAttempts int, now time.Time) error {
	tx := dbOf(s.db, ctx).Model(&codeModel{}).
		Where("id = ? AND consumed_at IS NULL AND invalidated_at IS NULL AND attempts = ?", c.ID, readAttempts).
		Updates(map[string]any{
			"attempts": c.Attempts, "consumed_at": wholeOrNil(c.ConsumedAt), "invalidated_at": wholeOrNil(c.InvalidatedAt), "updated_at": whole(now),
		})
	if tx.Error != nil {
		return tx.Error
	}

	if tx.RowsAffected == 0 {
		return account.ErrCodeConflict
	}

	return nil
}

// InvalidateLive implements account.CodeStore.
func (s *Codes) InvalidateLive(ctx context.Context, accountID int, p account.Purpose, now time.Time) error {
	return ends(dbOf(s.db, ctx), accountID, p, now)
}
