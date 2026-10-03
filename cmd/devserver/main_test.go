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
				SignedInAs *int   `json:"signed_in_as"`
			} `json:"data"`
		} `json:"data"`
	}

	require.NoError(t, json.Unmarshal(raw, &envelope), string(raw))
	page := envelope.Data.Rows
	require.Len(t, page, 2)
	assert.Equal(t, "crm", page[0].Area)
	assert.Equal(t, "changed", page[0].Action)
	require.NotNil(t, page[0].SignedInAs, "the later one was done as the administrator")
	assert.Equal(t, 1, *page[0].SignedInAs)
	assert.Nil(t, page[1].SignedInAs)
}
