package notification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// deviceByID reads one device, and false when it does not exist (any more).
func (s *store) deviceByID(ctx context.Context, id uint64) (deviceRow, bool, error) {
	return s.oneDevice(ctx, "id = ?", id)
}

func (s *store) deviceByHash(ctx context.Context, hash string) (deviceRow, bool, error) {
	return s.oneDevice(ctx, "endpoint_hash = ?", hash)
}

func (s *store) oneDevice(ctx context.Context, where string, arg any) (deviceRow, bool, error) {
	var r deviceRow

	err := s.conn(ctx).Where(where, arg).First(&r).Error

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return deviceRow{}, false, nil
	case err != nil:
		return deviceRow{}, false, fmt.Errorf("notification: read the device: %w", err)
	}

	return r, true, nil
}

// devicesOfUser lists a person's devices, the most recently seen first.
func (s *store) devicesOfUser(ctx context.Context, userID int) ([]deviceRow, error) {
	var rows []deviceRow

	if err := s.conn(ctx).Where("user_id = ?", userID).Order("last_seen_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("notification: list the devices: %w", err)
	}

	return rows, nil
}

// activeDevices returns the devices pushes still go to, by person, in one query.
func (s *store) activeDevices(ctx context.Context, ids []int) (map[uint32][]deviceRow, error) {
	var rows []deviceRow

	if err := s.conn(ctx).Where("user_id IN ? AND disabled_at IS NULL", ids).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("notification: read the devices: %w", err)
	}

	out := map[uint32][]deviceRow{}
	for _, r := range rows {
		out[r.UserID] = append(out[r.UserID], r)
	}

	return out, nil
}

// registerDevice saves a subscription: a new device, or the refresh of the one with the same endpoint (NOTIF-DEVICE-001,
// rule 2). Two registrations of one endpoint racing end in one row: the unique endpoint_hash refuses the second insert,
// which then refreshes the first. The refresh writes only the registration's own columns, so a push result recorded
// meanwhile keeps its time.
func (s *store) registerDevice(ctx context.Context, next deviceRow, now time.Time) (deviceRow, error) {
	cur, found, err := s.deviceByHash(ctx, next.EndpointHash)
	if err != nil {
		return deviceRow{}, err
	}

	if !found {
		res := s.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&next)
		if res.Error != nil {
			return deviceRow{}, fmt.Errorf("notification: save the device: %w", res.Error)
		}

		if res.RowsAffected == 1 {
			return next, nil
		}

		if cur, found, err = s.deviceByHash(ctx, next.EndpointHash); err != nil || !found {
			return deviceRow{}, errors.Join(err, errors.New("notification: the device vanished while it was being saved")) //nolint:err113 // not a condition callers branch on
		}
	}

	moved := cur.UserID != next.UserID
	fields := map[string]any{
		"user_id": next.UserID, "p256dh": next.P256DH, "auth": next.Auth, "platform": next.Platform,
		"last_seen_at": now, "failure_count": 0, "disabled_at": nil, "updated_at": now,
	}

	if moved {
		fields["label"] = next.Label // a label the earlier person chose is not the new person's
	}

	err = s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if moved {
			// Pushes queued for the earlier person must not reach the new one.
			if err := cancelPushOfDevice(tx, cur.ID, "device moved to another user", now); err != nil {
				return err
			}
		}

		return tx.Model(&deviceRow{}).Where("id = ?", cur.ID).Updates(fields).Error
	})
	if err != nil {
		return deviceRow{}, fmt.Errorf("notification: refresh the device: %w", err)
	}

	out, _, err := s.deviceByID(ctx, cur.ID)

	return out, err
}

// renameDevice writes the label of the person's own device.
func (s *store) renameDevice(ctx context.Context, userID int, id uint64, label string, now time.Time) error {
	err := s.conn(ctx).Model(&deviceRow{}).Where("id = ? AND user_id = ?", id, userID).Updates(map[string]any{"label": label, "updated_at": now}).Error
	if err != nil {
		return fmt.Errorf("notification: rename the device: %w", err)
	}

	return nil
}

// removeDevice deletes the person's device and cancels the pushes queued for it, in one transaction.
func (s *store) removeDevice(ctx context.Context, userID int, id uint64, reason string, now time.Time) error {
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := cancelPushOfDevice(tx, id, reason, now); err != nil {
			return err
		}

		return tx.Where("id = ? AND user_id = ?", id, userID).Delete(&deviceRow{}).Error
	})
	if err != nil {
		return fmt.Errorf("notification: remove the device: %w", err)
	}

	return nil
}

// dropGoneDevice deletes the device whose subscription the push service says is gone, by its endpoint: the device may
// have moved to another person while the push was in flight, and its subscription is still the dead one.
func (s *store) dropGoneDevice(ctx context.Context, id uint64, hash string, now time.Time) error {
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := cancelPushOfDevice(tx, id, "subscription gone", now); err != nil {
			return err
		}

		return tx.Where("endpoint_hash = ?", hash).Delete(&deviceRow{}).Error
	})
	if err != nil {
		return fmt.Errorf("notification: remove the gone device: %w", err)
	}

	return nil
}

// cancelPushOfDevice ends the pushes a device is still owed.
func cancelPushOfDevice(tx *gorm.DB, deviceID uint64, reason string, now time.Time) error {
	return tx.Model(&deliveryRow{}).
		Where("device_id = ? AND channel = ? AND status IN ?", deviceID, pushChannel, dueStatuses).
		Updates(map[string]any{"status": statusCancelled, "last_error": reason, "updated_at": now}).Error
}

// cancelPushOf is cancelPushOfDevice for a caller outside a transaction.
func (s *store) cancelPushOf(ctx context.Context, deviceID uint64, reason string, now time.Time) error {
	if err := cancelPushOfDevice(s.conn(ctx), deviceID, reason, now); err != nil {
		return fmt.Errorf("notification: cancel the pushes of the device: %w", err)
	}

	return nil
}

// pushSucceeded notes a push the push service took: only this, so a registration written meanwhile stands.
func (s *store) pushSucceeded(ctx context.Context, id uint64, now time.Time) error {
	err := s.conn(ctx).Model(&deviceRow{}).Where("id = ?", id).
		Updates(map[string]any{"last_success_at": now, "failure_count": 0, "updated_at": now}).Error
	if err != nil {
		return fmt.Errorf("notification: note the push on the device: %w", err)
	}

	return nil
}

// pushFailed counts a failure that was the device's fault and disables the device at the limit (NOTIF-DEVICE-001,
// rule 5). It reports whether the device was just disabled.
func (s *store) pushFailed(ctx context.Context, id uint64, now time.Time) (bool, error) {
	disabled := false

	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&deviceRow{}).Where("id = ?", id).
			Updates(map[string]any{"failure_count": gorm.Expr("failure_count + 1"), "updated_at": now}).Error
		if err != nil {
			return err
		}

		res := tx.Model(&deviceRow{}).Where("id = ? AND failure_count >= ? AND disabled_at IS NULL", id, MaxConsecutiveFailures).
			Updates(map[string]any{"disabled_at": now})
		disabled = res.RowsAffected == 1

		return res.Error
	})
	if err != nil {
		return false, fmt.Errorf("notification: note the failed push on the device: %w", err)
	}

	return disabled, nil
}
