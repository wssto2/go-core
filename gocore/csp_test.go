//nolint:testpackage,paralleltest // uses the package's runnable helper; a real server that sets gin's global mode
package gocore

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWithCSPAddsSourcesAndCreatesDirectivesInDevAndProduction(t *testing.T) {
	for _, env := range []string{"development", "production", "staging"} {
		t.Run(env, func(t *testing.T) {
			app, port := runnable(t)
			app.cfg.App.Env = env
			WithCSP("frame-src", "'self'", "https://www.google.com/maps/")(app)
			WithCSP("img-src", "https://tiles.example")(app)
			app.authenticate = []gin.HandlerFunc{passthrough}
			app.Routes(upRoute.To(pong))

			csp := fetch(t, serving(t, app, port)+"/up").header.Get("Content-Security-Policy")

			for _, want := range []string{
				"frame-src 'self' https://www.google.com/maps/;",
				"img-src 'self' data: https://tiles.example;",
			} {
				if !strings.Contains(csp, want) {
					t.Errorf("%q is missing from %s", want, csp)
				}
			}

			if dev := env == "development"; dev != strings.Contains(csp, "'unsafe-eval'") {
				t.Errorf("'unsafe-eval' in the %s policy: %s", env, csp)
			}

			if env != "development" && !strings.Contains(csp, "script-src 'self' 'nonce-") {
				t.Errorf("production must stay nonce-only: %s", csp)
			}
		})
	}
}
