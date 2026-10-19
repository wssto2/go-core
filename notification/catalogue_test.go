package notification_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/access"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/notification"
)

type team struct{}

func (team) SubjectNames(context.Context, []authz.Subject) (map[authz.Subject]string, error) {
	return nil, nil
}

// The module defines its own dead-letter permissions on the application's catalogue, so the application does not call
// anything for them and the order of notification.Install and access.Install does not matter.
func TestInstallDefinesItsPermissionsInEitherOrderWithAccess(t *testing.T) {
	for name, notificationFirst := range map[string]bool{"notification first": true, "access first": false} {
		t.Run(name, func(t *testing.T) {
			app := gocoretest.New(t)
			cat := authz.NewCatalogue()
			people := &directory{Users: identitytest.Users(t, accounts()...)}

			installNotification := func() { notification.Install(app, people, notification.WithCatalogue(cat)) }
			installAccess := func() *access.Access { return access.Install(app, cat, team{}) }

			if notificationFirst {
				installNotification()
				installAccess()
			} else {
				installAccess()
				installNotification()
			}

			for _, id := range []string{notification.ViewDeadLetters, notification.RetryDeadLetters} {
				_, ok := cat.Lookup(id)
				require.True(t, ok, id)
			}

			var startup *gocore.StartupError
			if err := app.Check(); err != nil {
				require.ErrorAs(t, err, &startup)
				require.NotContains(t, err.Error(), "no permission catalogue defines")
			}
		})
	}
}
