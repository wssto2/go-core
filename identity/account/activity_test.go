package account_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

// The first area to cover a record type is its area, an exact type and a prefix both count,
// and a type nobody covers is "other".
func TestAnAreaIsTheFirstThatCoversTheType(t *testing.T) {
	areas, err := account.NewActivityAreas(
		account.Area("crm").Types("customers").Prefix("contracts."),
		account.Area("sales").Types("contracts.line", "offers"),
	)
	require.NoError(t, err)

	for recordType, want := range map[string]string{
		"customers": "crm", "contracts.head": "crm", "contracts.line": "crm", "offers": "sales",
		"account": "identity", "vehicles": "other", "": "other",
	} {
		assert.Equal(t, want, areas.AreaOf(recordType), recordType)
	}

	assert.Equal(t, []string{"crm", "sales", "identity", "other"}, areas.Keys())
	assert.Equal(t, "identity", account.ActivityAreas{}.AreaOf("account"), "the zero value has the default")
}

// An area that names identity itself replaces the default; mistakes say what to change.
func TestActivityAreasAreChecked(t *testing.T) {
	areas, err := account.NewActivityAreas(account.Area("people").Types("account"), account.Area("identity").Types("sessions"))
	require.NoError(t, err)
	assert.Equal(t, "people", areas.AreaOf("account"))
	assert.Equal(t, []string{"people", "identity", "other"}, areas.Keys())

	for want, area := range map[string]account.ActivityArea{
		"is not a key":      account.Area("CRM").Types("x"),
		"is reserved":       account.Area("other").Types("x"),
		"covers no record":  account.Area("crm"),
		"covers no record ": account.Area("crm").Types(""),
	} {
		_, err := account.NewActivityAreas(area)
		require.ErrorContains(t, err, want)
	}

	_, err = account.NewActivityAreas(account.Area("crm").Types("a"), account.Area("crm").Types("b"))
	require.ErrorContains(t, err, "named twice")
}

// What a person did, with its area: filtered by area and by days.
func TestAdminActivity(t *testing.T) {
	areas, err := account.NewActivityAreas(account.Area("people").Types("account"))
	require.NoError(t, err)

	k := seeded(t)
	admin, err := account.NewAdmin(account.AdminDeps{
		Users: k.Users, Search: k.Accounts, History: k.SignIns, Changes: k.Changes, Activity: k.Activity, Areas: areas, Transact: identitytest.Transactor{},
	})
	require.NoError(t, err)

	_, err = admin.Create(t.Context(), account.CreateAccount{Login: "dora", Name: "Dora", Email: "dora@example.com", Locale: "en", Password: "a long password", ActorID: 1})
	require.NoError(t, err)
	require.NoError(t, admin.Deactivate(t.Context(), account.DeactivateInput{ID: 2, ActorID: 1}))

	rows, total, err := admin.Activity(t.Context(), 1, account.ActivityFilter{}, account.Paging{PerPage: 1})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	require.Len(t, rows, 1)
	assert.Equal(t, account.ActivityChanged, rows[0].Action, "newest first: the deactivation")
	assert.Equal(t, 2, rows[0].RecordID)
	assert.Equal(t, "people", rows[0].Area)

	epoch := identitytest.Epoch
	for name, f := range map[string]account.ActivityFilter{
		"the area":     {Area: "people"},
		"the day":      {From: epoch, To: epoch.Add(6 * time.Hour)},
		"open to":      {From: epoch.AddDate(0, 0, -3)},
		"open from":    {To: epoch},
		"area and day": {Area: "people", From: epoch, To: epoch},
	} {
		_, n, err := admin.Activity(t.Context(), 1, f, account.Paging{})
		require.NoError(t, err, name)
		assert.Equal(t, 2, n, name)
	}

	for name, f := range map[string]account.ActivityFilter{
		"another area":    {Area: "other"},
		"the day before":  {To: epoch.AddDate(0, 0, -1)},
		"the day after":   {From: epoch.AddDate(0, 0, 1)},
		"a person's none": {Area: "people", From: epoch.AddDate(0, 0, 1)},
	} {
		_, n, err := admin.Activity(t.Context(), 1, f, account.Paging{})
		require.NoError(t, err, name)
		assert.Zero(t, n, name)
	}

	_, _, err = admin.Activity(t.Context(), 1, account.ActivityFilter{Area: "crm"}, account.Paging{})
	assert.True(t, apperr.HasReason(err, account.ReasonActivityAreaUnknown), "an area the application did not name")

	_, _, err = admin.Activity(t.Context(), 1, account.ActivityFilter{From: epoch.AddDate(0, 0, 1), To: epoch}, account.Paging{})
	assert.True(t, apperr.HasReason(err, account.ReasonActivityRangeInvalid), "ends before it starts")

	_, _, err = admin.Activity(t.Context(), 99, account.ActivityFilter{}, account.Paging{})
	assert.Error(t, err, "nobody by that id")
}

// Somebody signed in as the person at the time marks the entry; a later one is the person's own.
func TestAdminActivityMarksWhoWasSignedInAs(t *testing.T) {
	k := seeded(t)
	ctx := t.Context()

	_, err := k.Sessions.Open(ctx, account.NewSession{AccountID: 2, ActorID: 1, At: identitytest.Epoch.Add(-time.Minute), ExpiresAt: identitytest.Epoch.Add(time.Hour)})
	require.NoError(t, err)

	change := account.Change{AccountID: 3, ActorID: 2, Action: account.ChangeUpdated}
	require.NoError(t, k.Changes.Record(ctx, change))
	k.Clock.Advance(30 * time.Minute) // the session was last used at its opening
	require.NoError(t, k.Changes.Record(ctx, change))

	rows, _, err := k.Admin.Activity(ctx, 2, account.ActivityFilter{}, account.Paging{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Zero(t, rows[0].SignedInAs, "later: the person's own")
	assert.Equal(t, 1, rows[1].SignedInAs, "while 1 was signed in as 2")
}
