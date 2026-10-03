package notification

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// How long the module keeps what it made (NOTIF-READ-001, rule 5): a notification 90 days after it was made, read or not;
// a finished delivery (sent, failed or cancelled) 30 days after its last change. A delivery that is still owed (pending or held)
// is never deleted, and a notification is deleted only once none of its deliveries is left, so none is orphaned.
const (
	NotificationRetention = 90 * 24 * time.Hour
	DeliveryRetention     = 30 * 24 * time.Hour
)

const (
	housekeepingInterval = time.Hour
	pruneBatch           = 500
)

// Swept is what one sweep of the housekeeping deleted.
type Swept struct {
	Deliveries    int
	Notifications int
}

// Sweep deletes the finished deliveries older than DeliveryRetention and then the notifications older than
// NotificationRetention that have none left, in batches by primary key (portable SQL: MariaDB 10.3 has neither
// multi-table deletes with limits nor SKIP LOCKED). The worker Install starts calls it every hour; a command can call it to
// prune now.
func (n *Notices) Sweep(ctx context.Context) (Swept, error) {
	var out Swept

	now := whole(n.clock.Now())

	var err error

	if out.Deliveries, err = n.store.pruneDeliveries(ctx, now.Add(-DeliveryRetention)); err != nil {
		return out, err
	}

	out.Notifications, err = n.store.pruneNotifications(ctx, now.Add(-NotificationRetention))

	return out, err
}

// pruneDeliveries deletes the finished deliveries last changed before cutoff, a batch at a time.
func (s *store) pruneDeliveries(ctx context.Context, cutoff time.Time) (int, error) {
	finished := []string{statusSent, statusFailed, statusCancelled}

	return prune(ctx, s, &deliveryRow{}, "delivery",
		func(q *gorm.DB) *gorm.DB { return q.Where("status IN ? AND updated_at < ?", finished, cutoff) })
}

// pruneNotifications deletes the notifications made before cutoff that no delivery points to, a batch at a time.
func (s *store) pruneNotifications(ctx context.Context, cutoff time.Time) (int, error) {
	return prune(ctx, s, &row{}, "notification", func(q *gorm.DB) *gorm.DB {
		return q.Where("created_at < ? AND NOT EXISTS (SELECT 1 FROM notification_deliveries d WHERE d.notification_id = notifications.id)", cutoff)
	})
}

// prune deletes the rows of model's table that match, 500 at a time: the ids of a batch are read by primary key, then deleted
// by the same condition again, so a row that stopped matching meanwhile stays.
func prune(ctx context.Context, s *store, model any, what string, matching func(*gorm.DB) *gorm.DB) (int, error) {
	deleted := 0
	after := uint64(0)

	for {
		var ids []uint64

		if err := matching(s.conn(ctx).Model(model)).Where("id > ?", after).Order("id ASC").Limit(pruneBatch).Pluck("id", &ids).Error; err != nil {
			return deleted, fmt.Errorf("notification housekeeping: select %ss: %w", what, err)
		}

		if len(ids) == 0 {
			return deleted, nil
		}

		res := matching(s.conn(ctx).Where("id IN ?", ids)).Delete(model)
		if res.Error != nil {
			return deleted, fmt.Errorf("notification housekeeping: delete %ss: %w", what, res.Error)
		}

		deleted += int(res.RowsAffected)
		after = ids[len(ids)-1]
	}
}

// housekeeper is the background work that runs Sweep: once at start and then every hour.
type housekeeper struct{ notices *Notices }

// Name identifies the worker in logs and metrics.
func (housekeeper) Name() string { return "notification.housekeeping" }

// Run sweeps until ctx is cancelled.
func (h housekeeper) Run(ctx context.Context) error {
	ticker := time.NewTicker(housekeepingInterval)
	defer ticker.Stop()

	for {
		swept, err := h.notices.Sweep(ctx)

		switch {
		case err != nil && ctx.Err() == nil:
			h.notices.log.ErrorContext(ctx, "notification: housekeeping failed", "error", err)
		case swept != Swept{}:
			h.notices.log.InfoContext(ctx, "notification: deleted old rows", "notifications", swept.Notifications, "deliveries", swept.Deliveries)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
