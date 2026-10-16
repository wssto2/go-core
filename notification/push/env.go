package push

import "os"

// ConfigFromEnv reads the VAPID key pair and the operator contact from VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY and
// VAPID_SUBJECT. Generate the pair once per environment with GenerateKeys and never rotate it casually: a new pair
// invalidates every device subscription. Push is off while any value is empty (see Config.Enabled).
func ConfigFromEnv() Config {
	//nolint:exhaustruct,exhaustruct_v5 // zero by design: the default client
	return Config{
		PublicKey:  os.Getenv("VAPID_PUBLIC_KEY"),
		PrivateKey: os.Getenv("VAPID_PRIVATE_KEY"),
		Subject:    os.Getenv("VAPID_SUBJECT"),
	}
}

// Enabled reports whether the key pair and the subject are all set.
func (c Config) Enabled() bool {
	return c.PublicKey != "" && c.PrivateKey != "" && c.Subject != ""
}
