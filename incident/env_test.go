//nolint:paralleltest // the tests set environment variables
package incident_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/incident"
)

func newApp() *gocore.App { return gocore.New(gocore.WithConfig(gocore.DefaultConfig())) }

func clearAlertEnv(t *testing.T) {
	t.Helper()

	for _, k := range []string{
		"INCIDENT_NOTIFICATIONS_ENABLED", "INCIDENT_IMMEDIATE_SEVERITIES",
		"INCIDENT_IMMEDIATE_COOLDOWN_MINUTES", "INCIDENT_DIGEST_INTERVAL_MINUTES",
	} {
		t.Setenv(k, "")
	}
}

func TestAlertsFromEnvDefaults(t *testing.T) {
	clearAlertEnv(t)

	got := incident.AlertsFromEnv(newApp())

	assert.False(t, got.Enabled)
	assert.Equal(t, []incident.Severity{incident.SeverityCritical, incident.SeverityHigh}, got.Immediate)
	assert.Equal(t, incident.DefaultCooldown, got.Cooldown)
	assert.Equal(t, incident.DefaultDigestEvery, got.DigestEvery)
}

func TestAlertsFromEnvReadsTheVariables(t *testing.T) {
	clearAlertEnv(t)
	t.Setenv("INCIDENT_NOTIFICATIONS_ENABLED", "true")
	t.Setenv("INCIDENT_IMMEDIATE_SEVERITIES", " Critical, ,MEDIUM ")
	t.Setenv("INCIDENT_IMMEDIATE_COOLDOWN_MINUTES", "10")
	t.Setenv("INCIDENT_DIGEST_INTERVAL_MINUTES", "60")

	got := incident.AlertsFromEnv(newApp())

	assert.True(t, got.Enabled)
	assert.Equal(t, []incident.Severity{incident.SeverityCritical, incident.SeverityMedium}, got.Immediate)
	assert.Equal(t, 10*time.Minute, got.Cooldown)
	assert.Equal(t, time.Hour, got.DigestEvery)
}

func TestAlertsFromEnvStopsStartUpOnAMalformedValue(t *testing.T) {
	clearAlertEnv(t)
	t.Setenv("INCIDENT_IMMEDIATE_COOLDOWN_MINUTES", "soon")
	t.Setenv("INCIDENT_NOTIFICATIONS_ENABLED", "maybe")

	app := newApp()
	incident.AlertsFromEnv(app)

	err := app.Check()
	require.ErrorContains(t, err, "INCIDENT_IMMEDIATE_COOLDOWN_MINUTES=soon")
	require.ErrorContains(t, err, "INCIDENT_NOTIFICATIONS_ENABLED=maybe")
}
