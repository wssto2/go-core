package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T) *httptest.Server {
	t.Helper()

	handler, err := build(t.Context(), []string{"http://localhost:5173"}, slog.New(slog.DiscardHandler), time.Now)
	require.NoError(t, err)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv
}

func TestTheSeededPeopleSignInAndAdministratorHasAccess(t *testing.T) {
	srv := serve(t)

	for _, p := range people {
		resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", //nolint:noctx // a test of a local server
			strings.NewReader(`{"login":"`+p.Login+`","password":"`+p.Password+`"}`))
		require.NoError(t, err)

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", p.Login, body)

		if p.Login == "admin" {
			assert.Contains(t, string(body), "iam.role:manage")
		} else {
			assert.Contains(t, string(body), "crm.customer:view")
		}
	}
}

func TestTheAdministratorSignsInAsUserAndReturns(t *testing.T) {
	srv := serve(t)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	client := &http.Client{Jar: jar}
	post := func(path, body string) (int, string) {
		resp, err := client.Post(srv.URL+path, "application/json", strings.NewReader(body)) //nolint:noctx // a test of a local server
		require.NoError(t, err)

		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		return resp.StatusCode, string(raw)
	}

	status, body := post("/api/v1/auth/login", `{"login":"admin","password":"admin-password"}`)
	require.Equal(t, http.StatusOK, status, body)

	status, body = post("/api/v1/auth/login-as", `{"user_id":2}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, `"impersonator":{"id":1,"name":"admin"}`)

	status, body = post("/api/v1/auth/login-as/return", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.NotContains(t, body, "impersonator")
	assert.Contains(t, body, `"login":"admin"`)
}

func TestACallFromThePlaygroundOriginMayCarryCookies(t *testing.T) {
	srv := serve(t)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodOptions, srv.URL+"/api/v1/auth/login", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()

	assert.Equal(t, "http://localhost:5173", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
}

func TestItRefusesWhatIsNotDevelopment(t *testing.T) {
	none := func(string) string { return "" }
	env := func(k string) string {
		if k == "GO_ENV" {
			return "production"
		}

		return ""
	}

	require.NoError(t, refuse(options{addr: "127.0.0.1:8090"}, none))
	require.NoError(t, refuse(options{addr: "localhost:8090"}, none))
	require.NoError(t, refuse(options{addr: "[::1]:8090"}, none))

	require.ErrorContains(t, refuse(options{addr: "127.0.0.1:8090"}, env), "GO_ENV=production")
	require.ErrorContains(t, refuse(options{addr: ":8090"}, none), "-allow-remote")
	require.ErrorContains(t, refuse(options{addr: "0.0.0.0:8090"}, none), "-allow-remote")
	require.ErrorContains(t, refuse(options{addr: "192.168.1.5:8090"}, none), "-allow-remote")
	require.ErrorContains(t, refuse(options{addr: "8090"}, none), "host:port")

	require.NoError(t, refuse(options{addr: ":8090", allowRemote: true}, env))
}

func TestTheUsersSeededActivityIsReadByTheAdministrator(t *testing.T) {
	srv := serve(t)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	client := &http.Client{Jar: jar}

	resp, err := client.Post(srv.URL+"/api/v1/auth/login", "application/json", //nolint:noctx // a test of a local server
		strings.NewReader(`{"login":"admin","password":"admin-password"}`))
	require.NoError(t, err)
	_ = resp.Body.Close()

	resp, err = client.Get(srv.URL + "/api/v1/iam/users/2/activity") //nolint:noctx // a test of a local server
	require.NoError(t, err)

	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, string(raw))

	var envelope struct {
		Data struct {
			Rows []struct {
				Area       string `json:"area"`
				Action     string `json:"action"`
				SignedInAs *struct {
					ID   int    `json:"id"`
					Name string `json:"name"`
				} `json:"signed_in_as"`
			} `json:"data"`
		} `json:"data"`
	}

	require.NoError(t, json.Unmarshal(raw, &envelope), string(raw))
	page := envelope.Data.Rows
	require.Len(t, page, 2)
	assert.Equal(t, "crm", page[0].Area)
	assert.Equal(t, "changed", page[0].Action)
	require.NotNil(t, page[0].SignedInAs, "the later one was done as the administrator")
	assert.Equal(t, 1, page[0].SignedInAs.ID)
	assert.Equal(t, "admin", page[0].SignedInAs.Name)
	assert.Nil(t, page[1].SignedInAs)

	resp, err = client.Get(srv.URL + "/api/v1/iam/users/2/signins?view=failed") //nolint:noctx // a test of a local server
	require.NoError(t, err)

	raw, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	assert.Contains(t, string(raw), `"event":"wrong_password"`, "the seeded refused attempt")
	assert.Contains(t, string(raw), `"views":[{"key":"all","count":1},{"key":"failed","count":1}]`)

	resp, err = client.Get(srv.URL + "/api/v1/iam/users/2/changes") //nolint:noctx // a test of a local server
	require.NoError(t, err)

	raw, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	assert.Contains(t, string(raw), `"actor":{"id":1,"name":"admin"}`, "the seeded change names who made it")
}

// user's inbox has the two seeded notifications, one read and one unread, and the test notification
// reaches it through the event queue the server drains.
func TestTheUsersSeededInboxHasOneUnreadAndTheTestNotificationArrives(t *testing.T) {
	srv := serve(t)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	client := &http.Client{Jar: jar}
	call := func(method, path string) (int, string) {
		req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)

		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		return resp.StatusCode, string(raw)
	}

	resp, err := client.Post(srv.URL+"/api/v1/auth/login", "application/json", //nolint:noctx // a test of a local server
		strings.NewReader(`{"login":"user","password":"user-password"}`))
	require.NoError(t, err)
	_ = resp.Body.Close()

	status, body := call(http.MethodGet, "/api/v1/notifications/unread")
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, `"unread_count":1`)

	status, body = call(http.MethodGet, "/api/v1/notifications")
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "A ticket was assigned to you")
	assert.Contains(t, body, "Your account is ready")

	status, body = call(http.MethodPost, "/api/v1/notifications/test")
	require.Equal(t, http.StatusNoContent, status, body)

	require.Eventually(t, func() bool {
		_, body := call(http.MethodGet, "/api/v1/notifications/unread")

		return strings.Contains(body, `"unread_count":2`)
	}, 5*time.Second, 100*time.Millisecond, "the test notification is handled by the drain loop")
}

// The administrator holds the dead-letter permissions; the plain user does not.
func TestTheWebmasterListsDeadLettersAndTheUserMayNot(t *testing.T) {
	srv := serve(t)

	listFor := func(login, password string) int {
		jar, err := cookiejar.New(nil)
		require.NoError(t, err)

		client := &http.Client{Jar: jar}
		resp, err := client.Post(srv.URL+"/api/v1/auth/login", "application/json", //nolint:noctx // a test of a local server
			strings.NewReader(`{"login":"`+login+`","password":"`+password+`"}`))
		require.NoError(t, err)
		_ = resp.Body.Close()

		resp, err = client.Get(srv.URL + "/api/v1/events/dead-letters") //nolint:noctx // a test of a local server
		require.NoError(t, err)
		_ = resp.Body.Close()

		return resp.StatusCode
	}

	assert.Equal(t, http.StatusOK, listFor("admin", "admin-password"))
	assert.Equal(t, http.StatusForbidden, listFor("user", "user-password"))
}

// logBuffer is a log writer a test can read while the server writes to it.
type logBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// The sample notification is e-mailed by default: the e-mail goes to the sink and is printed, with a link to the playground
// (the first -origin). It is made at noon, outside quiet hours, so the worker sends it at once.
func TestTheSampleNotificationIsEmailedToTheUserThroughTheSink(t *testing.T) {
	logs := &logBuffer{}
	noon := time.Date(2026, 6, 10, 12, 0, 0, 0, time.Local)

	_, err := build(t.Context(), []string{"http://localhost:5173"}, slog.New(slog.NewTextHandler(logs, nil)), func() time.Time { return noon })
	require.NoError(t, err)

	require.Eventually(t, func() bool { return strings.Contains(logs.String(), "to=user@dev.test") }, 5*time.Second, 50*time.Millisecond, logs.String())

	out := logs.String()
	assert.Contains(t, out, "A ticket was assigned to you")
	assert.Contains(t, out, "http://localhost:5173/tickets/7", "the link opens the playground")
	assert.NotContains(t, out, "Welcome to the playground", "the older notification was read before it was sent: no e-mail for it")
}
