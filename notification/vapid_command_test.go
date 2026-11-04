//nolint:goconst,lll // tests in the style of the package's others
package notification_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/notification"
)

func vapidApp(t *testing.T) *gocore.App {
	t.Helper()

	reg, cleanup := database.NewTestRegistry("local")

	t.Cleanup(func() { _ = cleanup() })

	app := gocore.New(gocore.WithConfig(gocore.DefaultConfig()), gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)))
	notification.VapidKeysCommand(app, "mailto:it@example.com")

	return app
}

func TestVapidKeysPrintsTheEnvLines(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	require.NoError(t, vapidApp(t).RunCommand(t.Context(), []string{"notification:vapid-keys"}, &out))
	assert.Regexp(t, `^VAPID_PUBLIC_KEY=\S+\nVAPID_PRIVATE_KEY=\S+\nVAPID_SUBJECT=mailto:it@example.com\n$`, out.String())
}

func TestVapidKeysHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	cancel()

	var out bytes.Buffer

	require.ErrorIs(t, vapidApp(t).RunCommand(ctx, []string{"notification:vapid-keys"}, &out), context.Canceled)
	assert.Empty(t, out.String())
}

func TestVapidKeysHelpHasItsText(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	require.NoError(t, vapidApp(t).RunCommand(t.Context(), []string{"notification:vapid-keys", "-h"}, &out))
	assert.Regexp(t, `(?s)Usage: .*\n\n.+\n\nFlags:`, out.String())
}
