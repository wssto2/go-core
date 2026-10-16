package incident

import (
	"strings"
	"time"

	"github.com/wssto2/go-core/bootstrap"
)

// AlertsFromEnv reads the alerts' settings from the environment: INCIDENT_NOTIFICATIONS_ENABLED (off),
// INCIDENT_IMMEDIATE_SEVERITIES (a comma-separated list, "critical,high"), INCIDENT_IMMEDIATE_COOLDOWN_MINUTES
// (DefaultCooldown, 5) and INCIDENT_DIGEST_INTERVAL_MINUTES (DefaultDigestEvery, 30). Notices and Holders are not
// settings of the environment: set Notices on the returned value before passing it to Install. An unknown severity is
// reported by Install.
func AlertsFromEnv() Alerts {
	severities := []Severity{}

	for s := range strings.SplitSeq(bootstrap.EnvStr("INCIDENT_IMMEDIATE_SEVERITIES", "critical,high"), ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			severities = append(severities, Severity(s))
		}
	}

	//nolint:exhaustruct,exhaustruct_v5 // zero by design: Notices and Holders are not settings of the environment
	return Alerts{
		Enabled:   bootstrap.EnvBool("INCIDENT_NOTIFICATIONS_ENABLED", false),
		Immediate: severities,
		Cooldown: time.Duration(
			bootstrap.EnvInt("INCIDENT_IMMEDIATE_COOLDOWN_MINUTES", int(DefaultCooldown/time.Minute)),
		) * time.Minute,
		DigestEvery: time.Duration(
			bootstrap.EnvInt("INCIDENT_DIGEST_INTERVAL_MINUTES", int(DefaultDigestEvery/time.Minute)),
		) * time.Minute,
	}
}
