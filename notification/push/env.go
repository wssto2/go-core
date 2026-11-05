package push

import (
	"os"

	"github.com/wssto2/go-core/env"
)

// ConfigFromEnv reads the VAPID key pair and the operator contact from VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY and
// VAPID_SUBJECT. Generate the pair once per environment with GenerateKeys and never rotate it casually: a new pair
// invalidates every device subscription. Push is off while any value is empty (see Config.Enabled).
func ConfigFromEnv() Config { return ConfigFrom(env.New(os.LookupEnv)) }

// ConfigFrom is ConfigFromEnv over vars, such as app.Env(), so the keys come through the application's lookup
// (gocore.WithLookup) instead of the process environment.
func ConfigFrom(vars *env.Reader) Config {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design: the default client
	return Config{
		PublicKey:  vars.String("VAPID_PUBLIC_KEY", ""),
		PrivateKey: vars.String("VAPID_PRIVATE_KEY", ""),
		Subject:    vars.String("VAPID_SUBJECT", ""),
	}
}

// Enabled reports whether the key pair and the subject are all set.
func (c Config) Enabled() bool {
	return c.PublicKey != "" && c.PrivateKey != "" && c.Subject != ""
}
