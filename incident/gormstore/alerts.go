package gormstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wssto2/go-core/incident/core"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LastAlert implements core.Store.
func (s *Store) LastAlert(ctx context.Context, tier string) (time.Time, bool, error) {
	var row cooldownModel

	err := s.conn(ctx).Where("severity_tier = ?", tier).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, false, nil
	}

	if err != nil {
		return time.Time{}, false, fmt.Errorf("read alert cooldown of %s: %w", tier, err)
	}

	return row.LastSentAt.UTC(), true, nil
}

// StampAlert implements core.Store.
func (s *Store) StampAlert(ctx context.Context, tier string, at time.Time) error {
	row := cooldownModel{SeverityTier: tier, LastSentAt: at.UTC().Truncate(time.Millisecond)}

	err := s.conn(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "severity_tier"}}, DoUpdates: clause.AssignmentColumns([]string{"last_sent_at"}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("stamp alert cooldown of %s: %w", tier, err)
	}

	return nil
}

// QueueDigest implements core.Store.
func (s *Store) QueueDigest(ctx context.Context, item core.DigestItem) error {
	var waiting int64

	err := s.conn(ctx).Model(&digestModel{}).Where("incident_id = ? AND digested_at IS NULL", item.IncidentID).Count(&waiting).Error
	if err != nil {
		return fmt.Errorf("read digest queue: %w", err)
	}

	if waiting > 0 {
		return nil
	}

	row := digestModel{
		IncidentID: item.IncidentID, PublicCode: item.PublicCode, Title: item.Title, Severity: string(item.Severity),
		CreatedAt: item.CreatedAt.UTC().Truncate(time.Millisecond),
	}

	if err := s.conn(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("queue digest item: %w", err)
	}

	return nil
}

// ClaimDigest implements core.Store. Run it in a transaction: the rows are locked by the select and marked by the
// update, so two workers never take the same item.
func (s *Store) ClaimDigest(ctx context.Context, at time.Time, limit int) ([]core.DigestItem, error) {
	var rows []digestModel

	err := s.conn(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("digested_at IS NULL").Order("id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("read digest queue: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	ids := make([]int, len(rows))
	items := make([]core.DigestItem, len(rows))

	for i, row := range rows {
		ids[i] = row.ID
		items[i] = core.DigestItem{
			ID: row.ID, IncidentID: row.IncidentID, PublicCode: row.PublicCode, Title: row.Title,
			Severity: core.Severity(row.Severity), CreatedAt: row.CreatedAt.UTC(),
		}
	}

	stamped := at.UTC().Truncate(time.Millisecond)

	err = s.conn(ctx).Model(&digestModel{}).Where("id IN ? AND digested_at IS NULL", ids).Update("digested_at", stamped).Error
	if err != nil {
		return nil, fmt.Errorf("claim digest items: %w", err)
	}

	return items, nil
}

// PruneDigest implements core.Store.
func (s *Store) PruneDigest(ctx context.Context, before time.Time, limit int) (int, error) {
	var ids []int

	err := s.conn(ctx).Model(&digestModel{}).Where("digested_at < ?", before.UTC().Truncate(time.Millisecond)).
		Order("id ASC").Limit(limit).Pluck("id", &ids).Error
	if err != nil {
		return 0, fmt.Errorf("select old digest items: %w", err)
	}

	if len(ids) == 0 {
		return 0, nil
	}

	res := s.conn(ctx).Where("id IN ?", ids).Delete(&digestModel{})
	if res.Error != nil {
		return 0, fmt.Errorf("delete old digest items: %w", res.Error)
	}

	return int(res.RowsAffected), nil
}
