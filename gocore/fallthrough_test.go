//nolint:testpackage,err113,exhaustruct,exhaustruct_v5,noctx,paralleltest,varnamelen,goconst // tests the unexported builder gocore is built on
package gocore

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/frontend"
	"github.com/wssto2/go-core/proxy"
)

// legacy is an upstream that sets its own CSP and echoes what it received.
func legacy(t *testing.T) (http.Handler, *http.Request) {
	t.Helper()

	var seen http.Request

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = *r.Clone(r.Context())
		seen.Body = io.NopCloser(strings.NewReader(string(body)))
		w.Header().Set("Content-Security-Policy", "legacy-policy")
		_, _ = io.WriteString(w, "legacy:"+string(body))
	}))
	t.Cleanup(up.Close)

	h, err := proxy.New(proxy.Config{Addr: up.Listener.Addr().String()}, nil)
	require.NoError(t, err)

	return h, &seen
}

func fallthroughApp(t *testing.T, h http.Handler, tweak func(*appBuilder)) *lifecycle {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := DefaultConfig()
	cfg.App.Env = "production"
	cfg.I18n.Dir = tempI18nDir(t)
	cfg.HTTP.TrustedProxies = nil

	b := newBuilder(cfg).DefaultInfrastructure().WithFallthrough(h).WithTrustedOrigins("https://partner.example").WithCSP("img-src", "https://cdn.example")
	if tweak != nil {
		tweak(b)
	}

	app, err := b.Build()
	require.NoError(t, err)

	// after Build, so the route has the middleware (modules register theirs after it, too)
	app.engine.GET("/own", func(ctx *gin.Context) { ctx.String(http.StatusOK, "go") })

	return app
}

// viaServer answers req on a real server: gin's writer needs a connection under it for the proxy (CloseNotify).
func viaServer(t *testing.T, app *lifecycle, req *http.Request) *http.Response {
	t.Helper()

	srv := httptest.NewServer(app.engine)
	t.Cleanup(srv.Close)

	req.RequestURI = ""
	req.URL.Scheme, req.URL.Host = "http", srv.Listener.Addr().String()

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	return resp
}

// got is what a request for the host lavcevic.hr is answered.
type got struct {
	Code   int
	Header http.Header
	Body   string
}

func do(t *testing.T, app *lifecycle, method, target, body string) got {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "lavcevic.hr"

	resp := viaServer(t, app, req)
	b, _ := io.ReadAll(resp.Body)

	return got{Code: resp.StatusCode, Header: resp.Header, Body: string(b)}
}

func TestFallthrough_UnmatchedGoesToTheUpstreamWithoutOurSecurityHeaders(t *testing.T) {
	h, seen := legacy(t)
	app := fallthroughApp(t, h, nil)

	own := do(t, app, http.MethodGet, "/own", "")
	assert.Equal(t, "go", own.Body)
	assert.Contains(t, own.Header.Get("Content-Security-Policy"), "img-src 'self' data: https://partner.example https://cdn.example")
	assert.Equal(t, "nosniff", own.Header.Get("X-Content-Type-Options"))

	// an inline-script page and a body that must arrive as sent
	rec := do(t, app, http.MethodPost, "/legacy/page?x=1", `{"a":1}`)
	assert.Equal(t, "legacy:{\"a\":1}", rec.Body)
	assert.Equal(t, []string{"legacy-policy"}, rec.Header.Values("Content-Security-Policy"))
	assert.Empty(t, rec.Header.Get("X-Content-Type-Options"))
	assert.Empty(t, rec.Header.Get("X-Frame-Options"))
	assert.Empty(t, rec.Header.Get("Referrer-Policy"))
	assert.Empty(t, rec.Header.Get("Strict-Transport-Security"))
	assert.NotEmpty(t, rec.Header.Get("X-Request-Id"), "request id still applies")
	assert.Equal(t, "lavcevic.hr", seen.Host)

	// a method no route accepts is unmatched too
	assert.Equal(t, "legacy:", do(t, app, http.MethodPost, "/own", "").Body)
}

func TestFallthrough_ForwardedHeadersFollowTheConfiguredProxies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		proxies []string
		xff     string
		proto   string
	}{
		{"untrusted peer", nil, "127.0.0.1", "http"},
		{"trusted peer", []string{"127.0.0.0/8"}, "203.0.113.9, 127.0.0.1", "https"},
		{"trusted by address", []string{"127.0.0.1"}, "203.0.113.9, 127.0.0.1", "https"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, seen := legacy(t)
			app := fallthroughApp(t, h, func(b *appBuilder) { b.cfg.HTTP.TrustedProxies = tc.proxies })

			req := httptest.NewRequest(http.MethodGet, "/x", nil) // the peer is the loopback
			req.Header.Set("X-Forwarded-For", "203.0.113.9")
			req.Header.Set("X-Forwarded-Proto", "https")
			viaServer(t, app, req)

			assert.Equal(t, tc.xff, seen.Header.Get("X-Forwarded-For"))
			assert.Equal(t, tc.proto, seen.Header.Get("X-Forwarded-Proto"))
		})
	}
}

func TestFallthrough_UpstreamDownAnswers502(t *testing.T) {
	up := httptest.NewServer(http.NotFoundHandler())
	dead := up.Listener.Addr().String()
	up.Close()

	h, err := proxy.New(proxy.Config{Addr: dead}, nil)
	require.NoError(t, err)

	rec := do(t, fallthroughApp(t, h, nil), http.MethodGet, "/x", "")
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body, "Bad Gateway")
}

func TestFallthrough_SPAOwnsItsBasePathAndTheRestFallsThrough(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<html><body>shell</body></html>`), 0o644))

	h, _ := legacy(t)
	app := fallthroughApp(t, h, func(b *appBuilder) {
		b.WithSPAConfig(frontend.SPAConfig{
			TemplatesPath: filepath.Join(dir, "*.html"),
			BasePath:      "/app",
			APIPrefix:     "/api",
			Vite:          frontend.ViteConfig{ManifestPath: filepath.Join(dir, "none", ".vite", "manifest.json")},
		})
	})

	shell := do(t, app, http.MethodGet, "/app/dashboard", "")
	assert.Contains(t, shell.Body, "shell")
	assert.NotEmpty(t, shell.Header.Get("Content-Security-Policy"), "the shell keeps its security headers")

	assert.Contains(t, do(t, app, http.MethodGet, "/app", "").Body, "shell")

	api := do(t, app, http.MethodGet, "/api/missing", "")
	assert.Equal(t, http.StatusNotFound, api.Code)
	assert.JSONEq(t, `{"error":"not found"}`, api.Body)

	for _, path := range []string{"/legacy", "/application"} { // "/application" is not under "/app"
		rec := do(t, app, http.MethodGet, path, "")
		assert.Equal(t, "legacy:", rec.Body, path)
		assert.Equal(t, []string{"legacy-policy"}, rec.Header.Values("Content-Security-Policy"), path)
	}
}

func TestFallthrough_SPAWithoutABasePathIsAWiringError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := DefaultConfig()
	cfg.I18n.Dir = tempI18nDir(t)
	h, _ := legacy(t)

	_, err := newBuilder(cfg).DefaultInfrastructure().WithSPA(nil).WithFallthrough(h).Build()
	require.ErrorContains(t, err, "BasePath")
}

func TestWithFallthroughOption_ReachesTheBuiltEngine(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, _ := legacy(t)
	app, _ := runnable(t)
	WithFallthrough(h)(app)

	booted, err := app.build()
	require.NoError(t, err)

	assert.Equal(t, "legacy:", do(t, booted, http.MethodGet, "/anything", "").Body)
}
