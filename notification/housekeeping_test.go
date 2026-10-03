package notification_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/notification"
	"gorm.io/gorm"
)

const day = 24 * time.Hour

// insertNotifications writes n notifications of person 1 made at created, with distinct dedupe keys, and returns their ids.
func insertNotifications(t *testing.T, db *gorm.DB, tag string, n int, created time.Time) []uint64 {
	t.Helper()

	rows := make([]map[string]any, 0, n)
	for i := range n {
		rows = append(rows, map[string]any{
			"user_id": 1, "category": "tickets.assigned", "title": "Old", "body": "", "link": "", "data": "{}",
			"dedupe_key": fmt.Sprintf("%s:%d", tag, i), "created_at": created,
		})
	}

	require.NoError(t, db.Table("notifications").CreateInBatches(rows, 200).Error)

	var ids []uint64
	require.NoError(t, db.Table("notifications").Where("dedupe_key LIKE ?", tag+":%").Order("id").Pluck("id", &ids).Error)

	return ids
}

// insertDelivery writes a delivery of a notification with the status and the time it was last changed.
func insertDelivery(t *testing.T, db *gorm.DB, notificationID uint64, status string, changed time.Time) {
	t.Helper()

	require.NoError(t, db.Table("notification_deliveries").Create(map[string]any{
		"notification_id": notificationID, "device_id": 0, "address": "ana@example.test", "channel": "email", "status": status,
		"attempts": 1, "next_attempt_at": changed, "expires_at": changed.Add(day), "created_at": changed, "updated_at": changed,
	}).Error)
}

func countWhere(t *testing.T, db *gorm.DB, table, where string, args ...any) int64 {
	t.Helper()

	var n int64
	require.NoError(t, db.Table(table).Where(where, args...).Count(&n).Error)

	return n
}

// Retention (NOTIF-READ-001, rule 5): finished deliveries go 30 days after their last change, notifications 90 days after they were made.
func TestHousekeepingDeletesOldNotificationsAndFinishedDeliveries(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db).start()
		now := w.clock.Now()

		old := insertNotifications(t, db, "old", 3, now.Add(-91*day))       // past 90 days
		recent := insertNotifications(t, db, "recent", 2, now.Add(-89*day)) // inside 90 days

		insertDelivery(t, db, old[0], "sent", now.Add(-31*day))         // finished, past 30 days: goes
		insertDelivery(t, db, old[1], "failed", now.Add(-31*day))       // goes
		insertDelivery(t, db, recent[0], "cancelled", now.Add(-29*day)) // inside 30 days: stays
		insertDelivery(t, db, recent[1], "sent", now.Add(-31*day))      // goes; its notification is young: stays

		swept, err := w.notices.Sweep(t.Context())
		require.NoError(t, err)

		require.Equal(t, notification.Swept{Deliveries: 3, Notifications: 3}, swept, "the three old ones: two lost their deliveries, the third never had one")
		require.Equal(t, int64(1), w.count("notification_deliveries"))
		require.Equal(t, int64(2), w.count("notifications"), "the recent ones")
		require.Zero(t, countWhere(t, db, "notifications", "dedupe_key LIKE 'old:%'"))

		again, err := w.notices.Sweep(t.Context())
		require.NoError(t, err)
		require.Equal(t, notification.Swept{}, again)
	})
}

// A delivery still owed (pending or held) is never deleted, however old; its notification stays with it.
func TestHousekeepingKeepsWhatIsStillOwed(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db).start()
		now := w.clock.Now()

		ids := insertNotifications(t, db, "owed", 2, now.Add(-120*day))
		insertDelivery(t, db, ids[0], "pending", now.Add(-60*day))
		insertDelivery(t, db, ids[1], "held", now.Add(-60*day))

		swept, err := w.notices.Sweep(t.Context())
		require.NoError(t, err)
		require.Equal(t, notification.Swept{}, swept)
		require.Equal(t, int64(2), w.count("notifications"))
		require.Equal(t, int64(2), w.count("notification_deliveries"))
	})
}

// A big backlog goes in batches by primary key, all of it.
func TestHousekeepingDeletesABacklogInBatches(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		w := newMailWorld(t, db).start()
		now := w.clock.Now()

		ids := insertNotifications(t, db, "backlog", 1200, now.Add(-100*day))

		deliveries := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			deliveries = append(deliveries, map[string]any{
				"notification_id": id, "device_id": 0, "address": "ana@example.test", "channel": "email", "status": "sent",
				"attempts": 1, "next_attempt_at": now.Add(-40 * day), "expires_at": now.Add(-39 * day), "created_at": now.Add(-40 * day), "updated_at": now.Add(-40 * day),
			})
		}

		require.NoError(t, db.Table("notification_deliveries").CreateInBatches(deliveries, 200).Error)

		swept, err := w.notices.Sweep(t.Context())
		require.NoError(t, err)
		require.Equal(t, notification.Swept{Deliveries: 1200, Notifications: 1200}, swept)
		require.Zero(t, w.count("notifications"))
		require.Zero(t, w.count("notification_deliveries"))
	})
}
