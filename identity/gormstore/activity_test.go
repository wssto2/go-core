package gormstore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/auth"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/gormstore"
	"gorm.io/gorm"
)

var day = time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)

// put writes an audit row of actor 7 at a chosen time, the way an application's audit trail would.
func put(t *testing.T, db *gorm.DB, recordType *string, id int, action string, at time.Time) {
	t.Helper()

	require.NoError(t, db.Exec(
		"INSERT INTO audit_logs (entity_type, entity_id, action, actor_id, created_at) VALUES (?, ?, ?, 7, ?)", recordType, id, action, at,
	).Error)
}

func ptr(s string) *string { return &s }

// What a person did is read by record type, a prefix of it and a day range; the
// patterns of a prefix are text, and a row without a type is outside every set.
func TestActivityFiltersOnEveryDatabase(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		tables(t, db)

		put(t, db, ptr("customers"), 1, "create", day)
		put(t, db, ptr("contracts.line"), 2, "update", day.Add(time.Hour))
		put(t, db, ptr("contracts.head"), 3, "delete", day.Add(24*time.Hour))
		put(t, db, ptr("contracts_x"), 4, "update", day.Add(25*time.Hour)) // "_" in a prefix is not a wildcard
		put(t, db, nil, 5, "update", day.Add(48*time.Hour))

		log := gormstore.NewActivityLog(db)
		ids := func(q account.ActivityQuery) []int {
			q.ActorID, q.Limit = 7, 10

			got, total, err := log.Activity(t.Context(), q)
			require.NoError(t, err)
			require.Equal(t, len(got), total)

			out := make([]int, len(got))
			for i, e := range got {
				out[i] = e.RecordID
			}

			return out
		}

		assert.Equal(t, []int{5, 4, 3, 2, 1}, ids(account.ActivityQuery{}), "newest first")
		assert.Equal(t, []int{3, 2}, ids(account.ActivityQuery{Within: account.RecordSet{Prefixes: []string{"contracts."}}}))
		assert.Equal(t, []int{4, 1}, ids(account.ActivityQuery{Within: account.RecordSet{Types: []string{"customers", "contracts_x"}}}))
		assert.Equal(t, []int{2}, ids(account.ActivityQuery{Within: account.RecordSet{Prefixes: []string{"contracts.l"}}, Outside: account.RecordSet{Types: []string{"contracts.head"}}}))
		assert.Equal(t, []int{5, 4, 3, 2, 1}, ids(account.ActivityQuery{Outside: account.RecordSet{Types: []string{"nothing"}}}), "outside keeps a row without a type")
		assert.Equal(t, []int{5}, ids(account.ActivityQuery{Outside: account.RecordSet{Types: []string{"customers", "contracts_x"}, Prefixes: []string{"contracts."}}}), "other")
		assert.Equal(t, []int{4, 3}, ids(account.ActivityQuery{From: day.Add(24 * time.Hour), To: day.Add(48 * time.Hour)}), "From inclusive, To exclusive")
		assert.Empty(t, ids(account.ActivityQuery{Within: account.RecordSet{Prefixes: []string{"%"}}}), "a percent sign is not a wildcard")

		counts, err := log.ActivityByType(t.Context(), account.ActivityQuery{ActorID: 7, From: day.Add(24 * time.Hour), To: day.Add(72 * time.Hour)})
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"contracts.head": 1, "contracts_x": 1, "": 1}, counts, "per type in the days, a row without a type under the empty one")

		got, _, err := log.Activity(t.Context(), account.ActivityQuery{ActorID: 7, Limit: 1, Offset: 2})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, account.ActivityDeleted, got[0].Action)
		assert.Equal(t, day.Add(24*time.Hour), got[0].At)
	})
}

// Rows done while somebody was signed in as the person are marked: from the session's
// opening to its last use, never by a session the person opened themselves.
func TestActivityMarksWhoWasSignedInAs(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
		tables(t, db)

		sessions := gormstore.New(db).Sessions
		open := func(actor int, at, lastUsed time.Time) {
			creds, err := sessions.Open(t.Context(), account.NewSession{AccountID: 7, ActorID: actor, Device: "d", At: at, ExpiresAt: at.Add(24 * time.Hour)})
			require.NoError(t, err)
			require.NoError(t, db.Model(&auth.Token{}).Where("token_value = ?", creds.Access).Update("last_used_at", lastUsed).Error)
		}

		open(3, day, day.Add(2*time.Hour))                  // 3 was signed in as 7 from 10:00 to 12:00
		open(0, day.Add(5*time.Hour), day.Add(6*time.Hour)) // 7's own session marks nothing
		open(4, day.Add(10*time.Hour), day.Add(10*time.Hour))

		for id, at := range map[int]time.Time{
			1: day.Add(-time.Minute),                 // before
			2: day.Add(90 * time.Minute),             // during 3's session
			3: day.Add(2*time.Hour + 30*time.Second), // within the minute a use is recorded at
			4: day.Add(5 * time.Hour),                // 7's own
			5: day.Add(10*time.Hour + 30*time.Second),
		} {
			put(t, db, ptr("customers"), id, "update", at)
		}

		got, _, err := gormstore.NewActivityLog(db).Activity(t.Context(), account.ActivityQuery{ActorID: 7, Limit: 10})
		require.NoError(t, err)

		marks := map[int]int{}
		for _, e := range got {
			marks[e.RecordID] = e.SignedInAs
		}

		assert.Equal(t, map[int]int{1: 0, 2: 3, 3: 3, 4: 0, 5: 4}, marks)
	})
}
