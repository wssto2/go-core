package incident

import (
	"os"
	"strings"
	"time"

	"github.com/wssto2/go-core/env"
)

// AlertsFromEnv reads the alerts' settings from the environment: INCIDENT_NOTIFICATIONS_ENABLED (off),
// INCIDENT_IMMEDIATE_SEVERITIES (a comma-separated list, "critical,high"), INCIDENT_IMMEDIATE_COOLDOWN_MINUTES
// (DefaultCooldown, 5) and INCIDENT_DIGEST_INTERVAL_MINUTES (DefaultDigestEvery, 30). Holders is not a
// setting of the environment. An unknown severity is
// reported by Install.
func AlertsFromEnv() Alerts {
	// The reader's problems are dropped: a malformed value keeps its default, as before (R2-b makes them start-up ones).
	vars := env.New(os.LookupEnv)
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
