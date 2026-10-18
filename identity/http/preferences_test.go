package http_test

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/preferences"
)

type memoryPrefs struct {
	mu   sync.Mutex
	rows map[int]json.RawMessage
}

func (m *memoryPrefs) Get(_ context.Context, id int) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.rows[id], nil
}

func (m *memoryPrefs) Set(_ context.Context, id int, p json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.rows[id] = p

	return nil
}

func withPreferences(t *testing.T, cfg preferences.Config) func(*identityhttp.Config) {
	t.Helper()

	cfg.Store = &memoryPrefs{rows: map[int]json.RawMessage{}}
	svc, err := preferences.New(cfg)
	require.NoError(t, err)

	return func(c *identityhttp.Config) { c.Preferences = svc }
}

func (r reply) pdata() map[string]any {
	d, _ := r.json()["data"].(map[string]any)

	return d
}

func TestPreferencesAreMergedAndAnsweredWhole(t *testing.T) {
	h := newHarness(t, withPreferences(t, preferences.Config{}))
	ana := h.as("ana", "secret")

	put := h.do(nethttp.MethodPut, "/v1/auth/preferences", map[string]any{"preferences": map[string]any{"dark_mode": true, "tips": []string{"a"}}}, ana)
	require.Equal(t, nethttp.StatusOK, put.Code, put.Body.String())
	assert.JSONEq(t, `{"success":true,"data":{"preferences":{"dark_mode":true,"tips":["a"]}}}`, put.Body.String())

	put = h.do(nethttp.MethodPut, "/v1/auth/preferences", map[string]any{"preferences": map[string]any{"tips": nil, "lang": "hr"}}, ana)
	require.Equal(t, nethttp.StatusOK, put.Code, put.Body.String())
	assert.JSONEq(t, `{"dark_mode":true,"lang":"hr"}`, string(mustJSON(t, put.pdata()["preferences"])))

	// and the session payload carries it, on every route that answers one
	me := h.do(nethttp.MethodGet, "/v1/auth/me", nil, ana)
	assert.JSONEq(t, `{"dark_mode":true,"lang":"hr"}`, string(mustJSON(t, me.pdata()["preferences"])))

	login := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"})
	assert.JSONEq(t, `{"dark_mode":true,"lang":"hr"}`, string(mustJSON(t, login.pdata()["preferences"])))
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()

	raw, err := json.Marshal(v)
	require.NoError(t, err)

	return raw
}

func TestThePayloadHasAnEmptyObjectWhenNothingIsStored(t *testing.T) {
	h := newHarness(t, withPreferences(t, preferences.Config{}))

	r := h.login()
	assert.JSONEq(t, `{}`, string(mustJSON(t, r.pdata()["preferences"])))
}

func TestWithoutPreferencesThePayloadHasNoField(t *testing.T) {
	h := newHarness(t, nil)

	r := h.login()
	assert.NotContains(t, r.pdata(), "preferences")
	assert.NotContains(t, r.Body.String(), "preferences")
}

func TestWithoutPreferencesThereIsNoRoute(t *testing.T) {
	h := newHarness(t, nil)
	ana := h.as("ana", "secret")

	r := h.do(nethttp.MethodPut, "/v1/auth/preferences", map[string]any{"preferences": map[string]any{"a": 1}}, ana)
	assert.Equal(t, nethttp.StatusNotFound, r.Code)
}

func TestPreferencesNeedASignedInPerson(t *testing.T) {
	h := newHarness(t, withPreferences(t, preferences.Config{}))

	r := h.do(nethttp.MethodPut, "/v1/auth/preferences", map[string]any{"preferences": map[string]any{"a": 1}})
	assert.Equal(t, nethttp.StatusUnauthorized, r.Code)
}

func TestPreferencesOfOnePersonAreNotAnothers(t *testing.T) {
	h := newHarness(t, withPreferences(t, preferences.Config{}))
	ana := h.as("ana", "secret")

	require.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodPut, "/v1/auth/preferences", map[string]any{"preferences": map[string]any{"a": 1}}, ana).Code)

	// boris signs in on the same harness, with his own cookies
	other := h.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "boris", "password": "hunter2"})
	assert.JSONEq(t, `{}`, string(mustJSON(t, other.pdata()["preferences"])))
}

func TestPreferencesRefuseWhatIsNotAnObject(t *testing.T) {
	h := newHarness(t, withPreferences(t, preferences.Config{}))
	ana := h.as("ana", "secret")

	for _, body := range []any{
		map[string]any{}, map[string]any{"preferences": nil}, map[string]any{"preferences": []int{1}}, map[string]any{"preferences": "x"},
	} {
		r := h.do(nethttp.MethodPut, "/v1/auth/preferences", body, ana)
		require.Equal(t, nethttp.StatusUnprocessableEntity, r.Code, string(mustJSON(t, body)))
		assert.Contains(t, r.Body.String(), `"preferences"`)
		assert.Contains(t, r.Body.String(), "must_be_object")
	}
}

func TestPreferencesAreCappedAndValidatedWithTheValidationShape(t *testing.T) {
	h := newHarness(t, withPreferences(t, preferences.Config{
		MaxBytes: 40,
		Validate: func(merged json.RawMessage) error {
			var m map[string]any

			_ = json.Unmarshal(merged, &m)

			if m["dark_mode"] == "yes" {
				return preferences.Invalid("preferences.dark_mode", "must_be_boolean")
			}

			return nil
		},
	}))
	ana := h.as("ana", "secret")

	big := h.do(nethttp.MethodPut, "/v1/auth/preferences", map[string]any{"preferences": map[string]any{"a": "0123456789012345678901234567890123456789"}}, ana)
	require.Equal(t, nethttp.StatusUnprocessableEntity, big.Code, big.Body.String())
	assert.Contains(t, big.Body.String(), `"max"`)

	bad := h.do(nethttp.MethodPut, "/v1/auth/preferences", map[string]any{"preferences": map[string]any{"dark_mode": "yes"}}, ana)
	require.Equal(t, nethttp.StatusUnprocessableEntity, bad.Code, bad.Body.String())
	assert.Contains(t, bad.Body.String(), "preferences.dark_mode")
	assert.Contains(t, bad.Body.String(), "must_be_boolean")
}
