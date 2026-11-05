//nolint:err113,exhaustruct,exhaustruct_v5,noctx,paralleltest,varnamelen,testpackage,gosec,bodyclose // tests in the style of the repository
package proxy

import (
	"bufio"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serve sends one request through a handler built for cfg and returns the answer.
func serve(t *testing.T, cfg Config, peer Peer, method, target, host, remote string, body string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()

	h, err := New(cfg, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host, req.RemoteAddr = host, remote

	for k, v := range header {
		req.Header[http.CanonicalHeaderKey(k)] = v
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req.WithContext(WithPeer(req.Context(), peer)))

	return rec
}

func addr(srv *httptest.Server) string { return srv.Listener.Addr().String() }

func TestNew_RefusesAnAddressWithoutAPort(t *testing.T) {
	_, err := New(Config{Addr: "localhost"}, nil)
	require.Error(t, err)
}

func TestForwardsPathQueryBodyAndKeepsTheHost(t *testing.T) {
	var got struct{ host, uri, method, body string }

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.host, got.uri, got.method, got.body = r.Host, r.RequestURI, r.Method, string(b)
		_, _ = io.WriteString(w, "legacy")
	}))
	defer up.Close()

	rec := serve(t, Config{Addr: addr(up)}, Peer{}, http.MethodPost, "/a/b?x=1&y=%20z", "lavcevic.hr", "192.0.2.1:5000", "payload", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "legacy", rec.Body.String())
	assert.Equal(t, "lavcevic.hr", got.host)
	assert.Equal(t, "/a/b?x=1&y=%20z", got.uri)
	assert.Equal(t, http.MethodPost, got.method)
	assert.Equal(t, "payload", got.body)
}

// The upstream answers only for its own hostname: the address is fixed, the name (and certificate check) is the Host.
func TestHTTPSUpstreamIsDialledByAddressAndVerifiedAgainstTheHost(t *testing.T) {
	var host string

	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		_, _ = io.WriteString(w, "secure")
	}))
	defer up.Close()

	pool := x509.NewCertPool()
	pool.AddCert(up.Certificate())
	cfg := Config{Addr: addr(up), HTTPS: true, RootCAs: pool}

	// the test certificate is for example.com
	rec := serve(t, cfg, Peer{}, http.MethodGet, "/", "example.com", "192.0.2.1:5000", "", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "secure", rec.Body.String())
	assert.Equal(t, "example.com", host)

	// another hostname does not match the certificate: refused, not served
	failed := false
	rec = serve(t, cfg, Peer{Failed: func(error) { failed = true }}, http.MethodGet, "/", "other.test", "192.0.2.1:5000", "", nil)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.True(t, failed)

	// verification is only off when asked
	cfg.InsecureSkipVerify = true
	rec = serve(t, cfg, Peer{}, http.MethodGet, "/", "other.test", "192.0.2.1:5000", "", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestForwardedHeaders(t *testing.T) {
	var seen http.Header

	up := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r.Header.Clone() }))
	defer up.Close()

	spoof := http.Header{
		"X-Forwarded-For":   {"1.1.1.1"},
		"X-Forwarded-Proto": {"https"},
		"X-Real-IP":         {"1.1.1.1"},
	}

	t.Run("untrusted peer: the headers it sent are replaced", func(t *testing.T) {
		serve(t, Config{Addr: addr(up)}, Peer{}, http.MethodGet, "/", "a.test", "198.51.100.7:4000", "", spoof)

		assert.Equal(t, "198.51.100.7", seen.Get("X-Forwarded-For"))
		assert.Equal(t, "http", seen.Get("X-Forwarded-Proto"))
		assert.Equal(t, "198.51.100.7", seen.Get("X-Real-IP"))
		assert.Equal(t, "a.test", seen.Get("X-Forwarded-Host"))
	})

	t.Run("trusted peer: the client is appended and the scheme kept", func(t *testing.T) {
		serve(t, Config{Addr: addr(up)}, Peer{Trusted: true}, http.MethodGet, "/", "a.test", "127.0.0.1:4000", "", spoof)

		assert.Equal(t, "1.1.1.1, 127.0.0.1", seen.Get("X-Forwarded-For"))
		assert.Equal(t, "https", seen.Get("X-Forwarded-Proto"))
		assert.Equal(t, "1.1.1.1", seen.Get("X-Real-IP"))
	})
}

func TestRedirectAndCookiesReachTheClientUntouched(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "laravel_session", Value: "abc", Path: "/", Domain: "lavcevic.hr", HttpOnly: true, Secure: true})
		http.SetCookie(w, &http.Cookie{Name: "XSRF-TOKEN", Value: "t", Path: "/"})
		w.Header().Set("Location", "https://lavcevic.hr/hr/")
		w.WriteHeader(http.StatusFound)
	}))
	defer up.Close()

	rec := serve(t, Config{Addr: addr(up)}, Peer{}, http.MethodGet, "/", "lavcevic.hr", "192.0.2.1:5000", "", nil)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "https://lavcevic.hr/hr/", rec.Header().Get("Location"))
	assert.Equal(t, []string{
		"laravel_session=abc; Path=/; Domain=lavcevic.hr; HttpOnly; Secure",
		"XSRF-TOKEN=t; Path=/",
	}, rec.Header().Values("Set-Cookie"))
}

func TestUpstreamDownAnswers502AndReportsOnce(t *testing.T) {
	up := httptest.NewServer(http.NotFoundHandler())
	dead := addr(up)
	up.Close()

	var reported []error

	rec := serve(t, Config{Addr: dead}, Peer{Failed: func(err error) { reported = append(reported, err) }}, http.MethodGet, "/", "a.test", "192.0.2.1:5000", "", nil)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "Bad Gateway")
	assert.Len(t, reported, 1)
}

func TestUpgradePassesThrough(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()

		line, _ := rw.ReadString('\n')
		_, _ = rw.WriteString("echo:" + line)
		_ = rw.Flush()
	}))
	defer up.Close()

	h, err := New(Config{Addr: addr(up)}, nil)
	require.NoError(t, err)

	front := httptest.NewServer(h)
	defer front.Close()

	conn, err := net.Dial("tcp", front.Listener.Addr().String())
	require.NoError(t, err)

	defer conn.Close()

	_, err = io.WriteString(conn, "GET /ws HTTP/1.1\r\nHost: a.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	require.NoError(t, err)

	rd := bufio.NewReader(conn)
	resp, err := http.ReadResponse(rd, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	_, err = io.WriteString(conn, "ping\n")
	require.NoError(t, err)

	line, err := rd.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "echo:ping\n", line)
}
