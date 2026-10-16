//nolint:exhaustruct,exhaustruct_v5 // tests in the style of the package's others
package push_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wssto2/go-core/notification/push"
)

func TestConfigFromEnvReadsTheVariables(t *testing.T) {
	t.Setenv("VAPID_PUBLIC_KEY", "pub")
	t.Setenv("VAPID_PRIVATE_KEY", "priv")
	t.Setenv("VAPID_SUBJECT", "mailto:ops@example.com")

	got := push.ConfigFromEnv()

	assert.Equal(t, push.Config{PublicKey: "pub", PrivateKey: "priv", Subject: "mailto:ops@example.com"}, got)
	assert.True(t, got.Enabled())
}

func TestConfigIsOffWhileAValueIsMissing(t *testing.T) {
	t.Setenv("VAPID_PUBLIC_KEY", "pub")
	t.Setenv("VAPID_PRIVATE_KEY", "priv")
	t.Setenv("VAPID_SUBJECT", "")

	assert.False(t, push.ConfigFromEnv().Enabled())
	assert.False(t, push.Config{}.Enabled())
}
