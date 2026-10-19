package incident

import (
	"os"
	"strings"
	"time"

	"github.com/wssto2/go-core/env"
)

// AlertsFromEnv reads the alerts' settings from the environment: INCIDENT_NOTIFICATIONS_ENABLED (off),
// INCIDENT_IMMEDIATE_SEVERITIES (a comma-separated list, "critical,high"), INCIDENT_IMMEDIATE_COOLDOWN_MINUTES
// (DefaultCooldown, 5) and INCIDENT_DIGEST_INTERVAL_MINUTES (DefaultDigestEvery, 30). Notices and Holders are not
// settings of the environment: set Notices on the returned value before passing it to Install. An unknown severity is
// reported by Install.
func AlertsFromEnv() Alerts {
	// The reader's problems are dropped: a malformed value keeps its default, as before. R2-b makes them start-up problems.
	e := env.New(os.LookupEnv)
	severities := []Severity{}

	for s := range strings.SplitSeq(e.String("INCIDENT_IMMEDIATE_SEVERITIES", "critical,high"), ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			severities = append(severities, Severity(s))
		}
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design: Notices and Holders are not settings of the environment
	return Alerts{
		Enabled:   e.Bool("INCIDENT_NOTIFICATIONS_ENABLED", false),
		Immediate: severities,
		Cooldown: time.Duration(
			e.Int("INCIDENT_IMMEDIATE_COOLDOWN_MINUTES", int(DefaultCooldown/time.Minute)),
		) * time.Minute,
		DigestEvery: time.Duration(
			e.Int("INCIDENT_DIGEST_INTERVAL_MINUTES", int(DefaultDigestEvery/time.Minute)),
		) * time.Minute,
	}
}
