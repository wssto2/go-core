//nolint:testpackage,paralleltest,gochecknoglobals,goconst,lll,varnamelen // uses the package's runnable and serving helpers
package gocore

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/wssto2/go-core/route"
	"github.com/wssto2/go-core/validation"
)

var ipRoute = route.Raw(http.MethodGet, "/ip").Name("proxy.ip").Public()

func showIP(ctx *gin.Context) { ctx.String(http.StatusOK, ctx.ClientIP()) }

// A request from 127.0.0.1 claiming to be 203.0.113.9: who the handler sees depends on the configured proxies.
func TestConfiguredTrustedProxyDecidesTheClientIPThroughRunAndHandler(t *testing.T) {
	cases := []struct {
		name    string
		trusted []string
		want    string
	}{
		{"trusted", []string{"127.0.0.1"}, "203.0.113.9"},
		{"another proxy", []string{"10.0.0.0/8"}, "127.0.0.1"},
		{"none", nil, "127.0.0.1"},
	}

	for _, tc := range cases {
		t.Run("Run "+tc.name, func(t *testing.T) {
			app, port := runnable(t)
			app.cfg.HTTP.TrustedProxies = tc.trusted
			app.Routes(ipRoute.To(showIP))

			base := serving(t, app, port)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/ip", nil)
			if err != nil {
				t.Fatal(err)
			}

			req.Header.Set("X-Forwarded-For", "203.0.113.9")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = resp.Body.Close() }()

			body, _ := io.ReadAll(resp.Body)
			if string(body) != tc.want {
				t.Errorf("Run: client IP = %q, want %q", body, tc.want)
			}
		})

		t.Run("Handler "+tc.name, func(t *testing.T) {
			app, _ := runnable(t)
			app.cfg.HTTP.TrustedProxies = tc.trusted
			app.Routes(ipRoute.To(showIP))

			h, err := app.Handler()
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ip", nil)
			req.RemoteAddr = "127.0.0.1:4000"
			req.Header.Set("X-Forwarded-For", "203.0.113.9")

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Body.String(); got != tc.want {
				t.Errorf("Handler: client IP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHandlerLeavesGinsGlobalModeAlone(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	t.Cleanup(func() { gin.SetMode(gin.TestMode) })

	app, _ := runnable(t)
	app.Routes(ipRoute.To(showIP))

	_, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}

	if gin.Mode() != gin.ReleaseMode {
		t.Errorf("Handler switched gin to %s", gin.Mode())
	}
}

func TestHandlerHidesDebugDetailInProductionAndShowsItInDevelopment(t *testing.T) {
	for env, wantDebug := range map[string]bool{"production": false, "staging": false, "development": true, "": true} {
		t.Run("env "+env, func(t *testing.T) {
			app, _ := runnable(t)
			app.cfg.App.Env = env
			app.Routes(ipRoute.To(func(ctx *gin.Context) {
				_ = ctx.Error(validation.NewValidationError("bad", nil, map[string][]string{"name": {"internal rule"}}))
			}))

			h, err := app.Handler()
			if err != nil {
				t.Fatal(err)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ip", nil))

			if got := strings.Contains(rec.Body.String(), "debug_errors"); got != wantDebug {
				t.Errorf("debug_errors in the body = %v, want %v: %s", got, wantDebug, rec.Body)
			}
		})
	}
}

func TestHSTSIsSentBehindATrustedProxyThatSaysHTTPS(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trusted []string
		proto   string
		want    bool
	}{
		{"trusted proxy says https", []string{"127.0.0.1"}, "https", true},
		{"trusted proxy says http", []string{"127.0.0.1"}, "http", false},
		{"no trusted proxy", nil, "https", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, port := runnable(t)
			app.cfg.App.Env = "production"
			app.cfg.HTTP.TrustedProxies = tc.trusted
			app.Routes(ipRoute.To(showIP))

			base := serving(t, app, port)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/ip", nil)
			if err != nil {
				t.Fatal(err)
			}

			req.Header.Set("X-Forwarded-Proto", tc.proto)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}

			_ = resp.Body.Close()

			if got := resp.Header.Get("Strict-Transport-Security") != ""; got != tc.want {
				t.Errorf("HSTS sent = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAnUnknownAppEnvIsAStartUpProblemNamingIt(t *testing.T) {
	for env, ok := range map[string]bool{"": true, "development": true, "staging": true, "production": true, "prod": false, "Production": false} {
		app, _ := runnable(t)
		cfg := app.cfg
		cfg.App.Env = env

		app = New(cfg, WithLogger(app.log))
		app.Routes(ipRoute.To(showIP))

		err := app.Check()
		if ok && err != nil {
			t.Errorf("APP_ENV=%q: %v", env, err)
		}

		if !ok && (err == nil || !strings.Contains(err.Error(), `"`+env+`"`)) {
			t.Errorf("APP_ENV=%q: want a problem naming it, got %v", env, err)
		}
	}
}
