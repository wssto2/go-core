package notification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The columns the device updates name more than once.
const (
	colUserID       = "user_id"
	colFailureCount = "failure_count"
	colUpdatedAt    = "updated_at"
)

var errDeviceVanished = errors.New("notification: the device vanished while it was being saved")

// deviceByID reads one device, and false when it does not exist (any more).
func (s *store) deviceByID(ctx context.Context, id uint64) (deviceRow, bool, error) {
	return s.oneDevice(ctx, "id = ?", id)
}

func (s *store) deviceByHash(ctx context.Context, hash string) (deviceRow, bool, error) {
	return s.oneDevice(ctx, "endpoint_hash = ?", hash)
}

func (s *store) oneDevice(ctx context.Context, where string, arg any) (deviceRow, bool, error) {
	var dev deviceRow

	err := s.conn(ctx).Where(where, arg).First(&dev).Error

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return deviceRow{}, false, nil //nolint:exhaustruct,exhaustruct_v5 // zero by design
	case err != nil:
		return deviceRow{}, false, fmt.Errorf("notification: read the device: %w", err)
	}

	return dev, true, nil
}

// devicesOfUser lists a person's devices, the most recently seen first.
func (s *store) devicesOfUser(ctx context.Context, userID int) ([]deviceRow, error) {
	var rows []deviceRow

	err := s.conn(ctx).Where("user_id = ?", userID).Order("last_seen_at DESC, id DESC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("notification: list the devices: %w", err)
	}

	return rows, nil
}

// activeDevices returns the devices pushes still go to, by person, in one query.
func (s *store) activeDevices(ctx context.Context, ids []int) (map[uint32][]deviceRow, error) {
	var rows []deviceRow

	err := s.conn(ctx).Where("user_id IN ? AND disabled_at IS NULL", ids).Order("id ASC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("notification: read the devices: %w", err)
	}

	out := map[uint32][]deviceRow{}
	for _, r := range rows {
		out[r.UserID] = append(out[r.UserID], r)
	}

	return out, nil
}

// registerDevice saves a subscription: a new device, or the refresh of the one with the same endpoint
// (NOTIF-DEVICE-001, rule 2). Two registrations of one endpoint racing end in one row: the unique endpoint_hash refuses
// the second insert, which then refreshes the first. The refresh writes only the registration's own columns, so a push
// result recorded meanwhile keeps its time.
func (s *store) registerDevice(ctx context.Context, next deviceRow, now time.Time) (deviceRow, error) {
	cur, found, err := s.deviceByHash(ctx, next.EndpointHash)
	if err != nil {
		return deviceRow{}, err
	}

	if !found {
		created, err := s.insertDevice(ctx, &next)
		if err != nil || created {
			return next, err
		}

		cur, found, err = s.deviceByHash(ctx, next.EndpointHash)
		if err != nil || !found {
			return deviceRow{}, errors.Join(err, errDeviceVanished)
		}
	}

	return s.refreshDevice(ctx, cur, next, now)
}

// insertDevice writes a new device, and reports false when the endpoint exists already.
func (s *store) insertDevice(ctx context.Context, next *deviceRow) (bool, error) {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	res := s.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(next)
	if res.Error != nil {
		return false, fmt.Errorf("notification: save the device: %w", res.Error)
	}

	return res.RowsAffected == 1, nil
}

// refreshDevice writes a registration over the device cur, which another person may have had until now.
func (s *store) refreshDevice(ctx context.Context, cur, next deviceRow, now time.Time) (deviceRow, error) {
	moved := cur.UserID != next.UserID
	fields := map[string]any{
		colUserID: next.UserID, "p256dh": next.P256DH, "auth": next.Auth, "platform": next.Platform,
		"last_seen_at": now, colFailureCount: 0, "disabled_at": nil, colUpdatedAt: now,
	}

	if moved {
		fields["label"] = next.Label // a label the earlier person chose is not the new person's
	}

	var cancelled map[string]int64

	err := s.conn(ctx).Transaction(func(conn *gorm.DB) error {
		if moved {
			// Pushes queued for the earlier person must not reach the new one.
			var err error

			cancelled, err = s.cancelPushOfDevice(conn, cur.ID, "device moved to another user", now)
			if err != nil {
				return err
			}
		}

		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		return conn.Model(&deviceRow{}).Where("id = ?", cur.ID).Updates(fields).Error
	})
	if err != nil {
		return deviceRow{}, fmt.Errorf("notification: refresh the device: %w", err)
	}

	s.metrics.cancelPushes(cancelled)

	out, _, err := s.deviceByID(ctx, cur.ID)

	return out, err
}

// renameDevice writes the label of the person's own device.
func (s *store) renameDevice(ctx context.Context, userID int, id uint64, label string, now time.Time) error {
	err := s.conn(ctx).
		Model(&deviceRow{}). //nolint:exhaustruct,exhaustruct_v5 // zero by design
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{"label": label, colUpdatedAt: now}).
		Error
	if err != nil {
		return fmt.Errorf("notification: rename the device: %w", err)
	}

	return nil
}

// removeDevice deletes the person's device and cancels the pushes queued for it, in one transaction.
func (s *store) removeDevice(ctx context.Context, userID int, deviceID uint64, reason string, now time.Time) error {
	var cancelled map[string]int64

	err := s.conn(ctx).Transaction(func(conn *gorm.DB) error {
		var err error

		cancelled, err = s.cancelPushOfDevice(conn, deviceID, reason, now)
		if err != nil {
			return err
		}

		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		return conn.Where("id = ? AND user_id = ?", deviceID, userID).Delete(&deviceRow{}).Error
	})
	if err != nil {
		return fmt.Errorf("notification: remove the device: %w", err)
	}

	s.metrics.cancelPushes(cancelled)

	return nil
}

// dropGoneDevice deletes the device whose subscription the push service says is gone, by its endpoint: the device may
// have moved to another person while the push was in flight, and its subscription is still the dead one.
func (s *store) dropGoneDevice(ctx context.Context, id uint64, hash string, now time.Time) error {
	var cancelled map[string]int64

	err := s.conn(ctx).Transaction(func(conn *gorm.DB) error {
		var err error

		cancelled, err = s.cancelPushOfDevice(conn, id, "subscription gone", now)
		if err != nil {
			return err
		}

		//nolint:exhaustruct,exhaustruct_v5 // zero by design
		return conn.Where("endpoint_hash = ?", hash).Delete(&deviceRow{}).Error
	})
	if err != nil {
		return fmt.Errorf("notification: remove the gone device: %w", err)
	}

	s.metrics.cancelPushes(cancelled)

	return nil
}

// cancelPushOfDevice ends the pushes a device is still owed, and returns how many per category it ended, for the
// metrics (nil when they are not kept). The caller counts them once its transaction has committed.
func (s *store) cancelPushOfDevice(
	conn *gorm.DB, deviceID uint64, reason string, now time.Time,
) (map[string]int64, error) {
	owed := func(q *gorm.DB) *gorm.DB {
		return q.Where("device_id = ? AND channel = ? AND status IN ?", deviceID, pushChannel, dueStatuses)
	}

	var counts map[string]int64

	if s.metrics.enabled() {
		var rows []struct {
			Category string
			Total    int64
		}

		err := owed(conn.Table("notification_deliveries AS d").
			Select("COALESCE(n.category, ?) AS category, COUNT(*) AS total", unknownCategory).
			Joins("LEFT JOIN notifications n ON n.id = d.notification_id")).
			Group("category").Scan(&rows).Error
		if err != nil {
			return nil, err
		}

		counts = map[string]int64{}
		for _, r := range rows {
			counts[r.Category] = r.Total
		}
	}

	err := owed(conn.Model(&deliveryRow{})). //nolint:exhaustruct,exhaustruct_v5 // zero by design
							Updates(map[string]any{"status": statusCancelled, "last_error": reason, colUpdatedAt: now}).Error

	return counts, err
}

// cancelPushOf is cancelPushOfDevice for a caller outside a transaction.
func (s *store) cancelPushOf(ctx context.Context, deviceID uint64, reason string, now time.Time) error {
	counts, err := s.cancelPushOfDevice(s.conn(ctx), deviceID, reason, now)
	if err != nil {
		return fmt.Errorf("notification: cancel the pushes of the device: %w", err)
	}

	s.metrics.cancelPushes(counts)

	return nil
}

// pushSucceeded notes a push the push service took: only this, so a registration written meanwhile stands.
func (s *store) pushSucceeded(ctx context.Context, id uint64, now time.Time) error {
	err := s.conn(ctx).Model(&deviceRow{}).Where("id = ?", id). //nolint:exhaustruct,exhaustruct_v5 // zero by design
									Updates(map[string]any{"last_success_at": now, colFailureCount: 0, colUpdatedAt: now}).Error
	if err != nil {
		return fmt.Errorf("notification: note the push on the device: %w", err)
	}

	return nil
}

// pushFailed counts a failure that was the device's fault and disables the device at the limit (NOTIF-DEVICE-001,
// rule 5). It reports whether the device was just disabled.
func (s *store) pushFailed(ctx context.Context, deviceID uint64, now time.Time) (bool, error) {
	disabled := false

	err := s.conn(ctx).Transaction(func(conn *gorm.DB) error {
		err := conn.Model(&deviceRow{}).Where("id = ?", deviceID). //nolint:exhaustruct,exhaustruct_v5 // zero by design
										Updates(map[string]any{colFailureCount: gorm.Expr("failure_count + 1"), colUpdatedAt: now}).Error
		if err != nil {
			return err
		}

		res := conn.Model(&deviceRow{}). //nolint:exhaustruct,exhaustruct_v5 // zero by design
							Where("id = ? AND failure_count >= ? AND disabled_at IS NULL", deviceID, MaxConsecutiveFailures).
							Updates(map[string]any{"disabled_at": now})
		disabled = res.RowsAffected == 1

		return res.Error
	})
	if err != nil {
		return false, fmt.Errorf("notification: note the failed push on the device: %w", err)
	}

	return disabled, nil
}
