package incident

import (
	"strings"
	"time"

	"github.com/wssto2/go-core/gocore"
)

// AlertsFromEnv reads the alerts' settings from the environment: INCIDENT_NOTIFICATIONS_ENABLED (off),
// INCIDENT_IMMEDIATE_SEVERITIES (a comma-separated list, "critical,high"), INCIDENT_IMMEDIATE_COOLDOWN_MINUTES
// (DefaultCooldown, 5) and INCIDENT_DIGEST_INTERVAL_MINUTES (DefaultDigestEvery, 30). Holders is not a
// setting of the environment. A malformed number or flag stops start-up, naming the variable, like any
// app.Env() read; an unknown severity is reported by Install.
func AlertsFromEnv(app *gocore.App) Alerts {
	vars := app.Env()
	severities := []Severity{}

	for s := range strings.SplitSeq(vars.String("INCIDENT_IMMEDIATE_SEVERITIES", "critical,high"), ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			severities = append(severities, Severity(s))
		}
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design: Holders is not a setting of the environment
	return Alerts{
		Enabled:   vars.Bool("INCIDENT_NOTIFICATIONS_ENABLED", false),
		Immediate: severities,
		Cooldown: time.Duration(
			vars.Int("INCIDENT_IMMEDIATE_COOLDOWN_MINUTES", int(DefaultCooldown/time.Minute)),
		) * time.Minute,
		DigestEvery: time.Duration(
			vars.Int("INCIDENT_DIGEST_INTERVAL_MINUTES", int(DefaultDigestEvery/time.Minute)),
		) * time.Minute,
	}
}
