package notification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// claimDue leases the due deliveries to one worker (NOTIF-DELIVERY-001, rule 4) in one short transaction: a plain
// read picks the candidates, a locking read by primary key takes them and re-checks that they are still due, and their
// next_attempt_at moves to leaseUntil. A worker claiming at the same moment waits on the rows this one locked; once this
// commits, its locking read sees the lease (a locking read reads the latest committed row, not the snapshot) and skips
// them. No SKIP LOCKED, FOR SHARE or FOR UPDATE OF: MariaDB 10.3 has none of them.
func (s *store) claimDue(ctx context.Context, now, leaseUntil time.Time, limit int) ([]deliveryRow, error) {
	var rows []deliveryRow

	due := func(q *gorm.DB) *gorm.DB { return q.Where("status IN ? AND next_attempt_at <= ?", dueStatuses, now) }

	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var candidates []uint64

		if err := due(tx.Model(&deliveryRow{})).Order("next_attempt_at ASC, id ASC").Limit(limit).Pluck("id", &candidates).Error; err != nil {
			return err
		}

		if len(candidates) == 0 {
			return nil
		}

		if err := due(tx.Where("id IN ?", candidates)).Order("next_attempt_at ASC, id ASC").
			Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
			return err
		}

		if len(rows) == 0 {
			return nil
		}

		ids := make([]uint64, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
		}

		return tx.Model(&deliveryRow{}).Where("id IN ?", ids).Update("next_attempt_at", leaseUntil).Error
	})
	if err != nil {
		return nil, fmt.Errorf("notification: claim the due deliveries: %w", err)
	}

	for i := range rows {
		rows[i].NextAttemptAt = leaseUntil
	}

	return rows, nil
}

// saveDelivery writes the result of an attempt. It is conditional on the row still being pending or held, the state the
// worker claimed it in, so a delivery that was ended meanwhile keeps its state and the result is dropped.
func (s *store) saveDelivery(ctx context.Context, d deliveryRow) error {
	err := s.conn(ctx).Model(&deliveryRow{}).Where("id = ? AND status IN ?", d.ID, dueStatuses).Updates(map[string]any{
		"status": d.Status, "attempts": d.Attempts, "next_attempt_at": d.NextAttemptAt, "expires_at": d.ExpiresAt,
		"sent_at": d.SentAt, "last_status": d.LastStatus, "last_error": d.LastError, "updated_at": d.UpdatedAt,
	}).Error
	if err != nil {
		return fmt.Errorf("notification: save the delivery: %w", err)
	}

	return nil
}

// byID reads one notification, and false when it does not exist (any more).
func (s *store) byID(ctx context.Context, id uint64) (row, bool, error) {
	var r row

	err := s.conn(ctx).Where("id = ?", id).First(&r).Error

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return row{}, false, nil
	case err != nil:
		return row{}, false, fmt.Errorf("notification: read the notification: %w", err)
	}

	return r, true, nil
}
