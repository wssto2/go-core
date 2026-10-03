package notification

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm/clause"
)

// choicesOf returns the person's own choices of e-mail, by category code.
func (s *store) choicesOf(ctx context.Context, userID int) (map[string]bool, error) {
	var rows []preferenceRow

	if err := s.conn(ctx).Where("user_id = ? AND channel = ?", userID, emailChannel).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("notification: read the person's choices: %w", err)
	}

	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.Category] = r.Enabled
	}

	return out, nil
}

// choicesFor returns the choices of the people for one category and channel, by person.
func (s *store) choicesFor(ctx context.Context, ids []int, category, channel string) (map[int]bool, error) {
	var rows []preferenceRow

	if err := s.conn(ctx).Where("user_id IN ? AND category = ? AND channel = ?", ids, category, channel).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("notification: read the people's choices: %w", err)
	}

	out := make(map[int]bool, len(rows))
	for _, r := range rows {
		out[int(r.UserID)] = r.Enabled
	}

	return out, nil
}

// setChoice stores the person's choice, replacing the one they had.
func (s *store) setChoice(ctx context.Context, userID int, category, channel string, enabled bool, now time.Time) error {
	row := preferenceRow{UserID: uint32(userID), Category: category, Channel: channel, Enabled: enabled, UpdatedAt: now} //nolint:gosec // a session's account id

	err := s.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "category"}, {Name: "channel"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("notification: save the person's choice: %w", err)
	}

	return nil
}

// clearChoice forgets the person's choice: the default applies again.
func (s *store) clearChoice(ctx context.Context, userID int, category, channel string) error {
	err := s.conn(ctx).Where("user_id = ? AND category = ? AND channel = ?", userID, category, channel).Delete(&preferenceRow{}).Error
	if err != nil {
		return fmt.Errorf("notification: forget the person's choice: %w", err)
	}

	return nil
}

// quietOf returns the quiet hours of the people who set their own, by person.
func (s *store) quietOf(ctx context.Context, ids []int) (map[int]QuietHours, error) {
	var rows []quietRow

	if err := s.conn(ctx).Where("user_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("notification: read quiet hours: %w", err)
	}

	out := make(map[int]QuietHours, len(rows))
	for _, r := range rows {
		out[int(r.UserID)] = QuietHours{Enabled: r.Enabled, Start: int(r.StartMinute), End: int(r.EndMinute)}
	}

	return out, nil
}

// saveQuiet stores the person's quiet hours, replacing the ones they had.
func (s *store) saveQuiet(ctx context.Context, userID int, q QuietHours, now time.Time) error {
	row := quietRow{UserID: uint32(userID), Enabled: q.Enabled, StartMinute: uint16(q.Start), EndMinute: uint16(q.End), UpdatedAt: now} //nolint:gosec // validated to 0..1439

	err := s.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "start_minute", "end_minute", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("notification: save quiet hours: %w", err)
	}

	return nil
}
