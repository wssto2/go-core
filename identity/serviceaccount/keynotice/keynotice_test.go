//nolint:exhaustruct,exhaustruct_v5,funlen,gochecknoglobals,lll,paralleltest // tests in the style of the package's others
package keynotice_test

import (
	"context"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/gormstore"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/identity/serviceaccount"
	sagorm "github.com/wssto2/go-core/identity/serviceaccount/gormstore"
	"github.com/wssto2/go-core/identity/serviceaccount/keynotice"
	"github.com/wssto2/go-core/notification"
)

var now = gocoretest.Epoch

type clock struct{}

func (clock) Now() time.Time { return now }

func day(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

// The whole path: the daily run queues one event per key and condition, the consumer makes the owner's
// notification, and a second run tells nobody twice.
func TestTheOwnerIsToldOnceOfEachCondition(t *testing.T) {
	cat := authz.NewCatalogue()
	require.NoError(t, identity.DefinePermissions(cat))

	app := gocoretest.New(t, gocoretest.Authorizer(authztest.AllowAll()))
	app.Permissions(cat)

	users := identity.Install(app, identity.WithoutMail(), identity.WithServiceAccounts(serviceaccount.Config{}))
	notices := notification.Install(app, users, notification.WithCatalogue(cat), keynotice.Expiring, keynotice.Unused)
	keynotice.Install(app, notices, nil)

	owner := identitytest.Account(7, "owner", "secret")
	owner.CreatedAt = now
	_, err := gormstore.New(app.Database()).Accounts.Create(t.Context(), owner)
	require.NoError(t, err)

	store := sagorm.New(app.Database())
	ctx := t.Context()

	robot := serviceaccount.Account{Name: "DMS export", OwnerID: 7, CreatedBy: 7, CreatedAt: now.Add(-day(100))}
	require.NoError(t, store.Create(ctx, &robot))

	key := func(name string, mod func(*serviceaccount.Key)) serviceaccount.Key {
		made := serviceaccount.Key{
			ServiceAccountID: robot.ID, Name: name, Prefix: name, Hash: serviceaccount.HashSecret(name), CreatedBy: 7, CreatedAt: now.Add(-day(10)),
		}
		mod(&made)
		require.NoError(t, store.CreateKey(ctx, &made))

		return made
	}

	soon := now.Add(day(10))

	key("expiring", func(k *serviceaccount.Key) { k.ExpiresAt = &soon })
	key("forgotten", func(k *serviceaccount.Key) { k.CreatedAt = now.Add(-day(70)) })
	key("fine", func(*serviceaccount.Key) {})
	revoked := key("revoked", func(k *serviceaccount.Key) { k.CreatedAt = now.Add(-day(70)) })
	require.NoError(t, store.RevokeKey(ctx, revoked.ID, now.Add(-day(1)), 7))

	run := serviceaccount.NewNotices(store, database.NewTransactor(app.Database()), slog.New(slog.DiscardHandler), clock{})

	sent, err := run.Send(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, sent, "one for the key that expires, one for the key nobody uses; not the healthy, not the revoked")

	require.NoError(t, app.DrainEvents(ctx))

	page, err := notices.Inbox.List(ctx, 7, notification.ListQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)

	byCategory := map[string]notification.Item{}
	for _, item := range page.Items {
		byCategory[item.Category] = item
	}

	expiring := byCategory[keynotice.Expiring.Code()]
	assert.Equal(t, "An API key is about to expire", expiring.Title)
	assert.Contains(t, expiring.Body, "“expiring” of “DMS export” expires on "+soon.Format(time.DateOnly)+" (in 10 days)")
	assert.Equal(t, "/iam/service-accounts/"+strconv.Itoa(robot.ID), expiring.Link)

	unused := byCategory[keynotice.Unused.Code()]
	assert.Equal(t, "An API key has not been used", unused.Title)
	assert.Contains(t, unused.Body, "has not been used for 70 days")

	// a second run, the next day and the one after, tells nobody twice
	sent, err = run.Send(ctx)
	require.NoError(t, err)
	assert.Zero(t, sent)
	require.NoError(t, app.DrainEvents(ctx))

	unread, err := notices.Inbox.Unread(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, 2, unread)
}

// Their own words, in the owner's language.
func TestTextIsTheApplications(t *testing.T) {
	cat := authz.NewCatalogue()
	require.NoError(t, identity.DefinePermissions(cat))

	app := gocoretest.New(t, gocoretest.Authorizer(authztest.AllowAll()))
	app.Permissions(cat)

	users := identity.Install(app, identity.WithoutMail(), identity.WithServiceAccounts(serviceaccount.Config{}))
	notices := notification.Install(app, users, notification.WithCatalogue(cat), keynotice.Expiring, keynotice.Unused)
	keynotice.Install(app, notices, func(r notification.Recipient, n serviceaccount.KeyNotice) notification.Message {
		return notification.Message{Title: r.Locale + ": " + n.Account, Link: "/keys"}
	})

	owner := identitytest.Account(7, "owner", "secret")
	owner.CreatedAt, owner.Locale = now, "hr"
	_, err := gormstore.New(app.Database()).Accounts.Create(t.Context(), owner)
	require.NoError(t, err)

	gocoretest.Publish(t, app, serviceaccount.KeyNoticed, serviceaccount.KeyNotice{
		OwnerID: 7, AccountID: 3, Account: "Feed", KeyID: 9, Key: "ci", Kind: serviceaccount.NoticeUnused, Date: "2026-01-01", Days: 61,
	})

	page, err := notices.Inbox.List(t.Context(), 7, notification.ListQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "hr: Feed", page.Items[0].Title)
	assert.Equal(t, "/keys", page.Items[0].Link)
	assert.Equal(t, map[string]string{"account_id": "3", "key_id": "9"}, page.Items[0].Data)
}

// The daily worker sends nothing while no consumer is installed: a notice would be queued with nobody to read it.
func TestTheWorkerWaitsForAConsumer(t *testing.T) {
	app := gocoretest.New(t)
	store := sagorm.New(app.Database())
	require.NoError(t, sagorm.Migrate(app.Database()))

	robot := serviceaccount.Account{Name: "feed", OwnerID: 7, CreatedBy: 7, CreatedAt: now.Add(-day(100))}
	require.NoError(t, store.Create(t.Context(), &robot))
	require.NoError(t, store.CreateKey(t.Context(), &serviceaccount.Key{
		ServiceAccountID: robot.ID, Name: "old", Prefix: "old", Hash: serviceaccount.HashSecret("old"), CreatedBy: 7, CreatedAt: now.Add(-day(70)),
	}))

	run := serviceaccount.NewNotices(store, database.NewTransactor(app.Database()), slog.New(slog.DiscardHandler), clock{})

	for name, consumers := range map[string][]string{"without a consumer": nil, "with another one": {"tickets.assigned"}} {
		ctx, stop := context.WithCancel(t.Context())
		worker := serviceaccount.NewNoticeWorker(run, func() []string {
			stop()

			return consumers
		}, slog.New(slog.DiscardHandler))

		require.NoError(t, worker.Run(ctx), name)

		sent, err := store.NoticesSent(t.Context(), []int{1})
		require.NoError(t, err)
		assert.Empty(t, sent, name)
	}
}
