package httpclient_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/httpclient"
	"github.com/wssto2/go-core/observability/tracing"
)

type product struct {
	ID   int    `json:"id"   xml:"id"`
	Name string `json:"name" xml:"name"`
}

func TestGet_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(product{ID: 1, Name: "Widget"})
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL)
	var out product
	_, err := c.Get(context.Background(), "/products/1", &out)
	require.NoError(t, err)
	assert.Equal(t, 1, out.ID)
	assert.Equal(t, "Widget", out.Name)
}

func TestGet_XML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		xml.NewEncoder(w).Encode(product{ID: 2, Name: "Gadget"})
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL)
	var out product
	_, err := c.Get(context.Background(), "/products/2", &out)
	require.NoError(t, err)
	assert.Equal(t, 2, out.ID)
}

func TestPost_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var in product
		json.NewDecoder(r.Body).Decode(&in)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(product{ID: 3, Name: in.Name})
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL)
	var out product
	resp, err := c.Post(context.Background(), "/products", product{Name: "Thing"}, &out)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "Thing", out.Name)
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL)
	_, err := c.Get(context.Background(), "/missing", nil)
	require.Error(t, err)
	assert.True(t, httpclient.IsStatus(err, http.StatusNotFound))
}

func TestBearerAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL, httpclient.WithAuth(httpclient.BearerAuth{Token: "secret-token"}))
	_, err := c.Get(context.Background(), "/", nil)
	require.NoError(t, err)
}

func TestAPIKeyAuth_Header(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "my-key", r.Header.Get("X-Api-Key"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL, httpclient.WithAuth(httpclient.APIKeyAuth{Key: "my-key", HeaderName: "X-Api-Key"}))
	_, err := c.Get(context.Background(), "/", nil)
	require.NoError(t, err)
}

func TestAPIKeyAuth_QueryParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "my-key", r.URL.Query().Get("api_key"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL, httpclient.WithAuth(httpclient.APIKeyAuth{Key: "my-key", QueryParam: "api_key"}))
	_, err := c.Get(context.Background(), "/", nil)
	require.NoError(t, err)
}

func TestBasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "user", u)
		assert.Equal(t, "pass", p)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL, httpclient.WithAuth(httpclient.BasicAuth{Username: "user", Password: "pass"}))
	_, err := c.Get(context.Background(), "/", nil)
	require.NoError(t, err)
}

func TestOAuth2ClientCredentials(t *testing.T) {
	callCount := 0
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "oauth-token",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer oauth-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer apiSrv.Close()

	auth := httpclient.NewOAuth2ClientCredentials(tokenSrv.URL, "id", "secret", nil, nil)
	c := httpclient.New(apiSrv.URL, httpclient.WithAuth(auth))

	_, err := c.Get(context.Background(), "/", nil)
	require.NoError(t, err)

	// Second call should reuse the cached token (no extra token request).
	_, err = c.Get(context.Background(), "/", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "token should be fetched once and cached")
}

func TestPost_XMLBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/xml", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL)
	_, err := c.Post(context.Background(), "/", httpclient.XMLBody{V: product{ID: 1, Name: "x"}}, nil)
	require.NoError(t, err)
}

func TestRetry_SucceedsOnSecondAttempt(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL, httpclient.WithRetry(3, 1*time.Millisecond))
	_, err := c.Get(context.Background(), "/", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestRetry_PostBodyResentOnEachAttempt(t *testing.T) {
	bodies := []string{}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if calls < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL, httpclient.WithRetry(3, 1*time.Millisecond))
	_, err := c.Post(context.Background(), "/", product{ID: 1, Name: "Widget"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	// Both attempts must have the full body, not an empty reader.
	for _, body := range bodies {
		assert.Contains(t, body, "Widget")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// threshold=2: opens after 2 failures
	c := httpclient.New(srv.URL, httpclient.WithCircuitBreaker(2, 10*time.Second))

	_, _ = c.Get(context.Background(), "/", nil) // failure 1
	_, _ = c.Get(context.Background(), "/", nil) // failure 2 → opens circuit

	// This call should be rejected by the open circuit, not hitting the server.
	_, err := c.Get(context.Background(), "/", nil)
	require.ErrorIs(t, err, httpclient.ErrCircuitOpen)
	assert.Equal(t, 2, calls, "third call should not reach the server")
}

func TestTraceID_ForwardedAsHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := tracing.NewSimpleTracer()
	ctx, finish := tr.StartSpan(context.Background(), "test")
	defer finish(nil)

	traceID, _ := tracing.TraceIDFromContext(ctx)
	require.NotEmpty(t, traceID)

	c := httpclient.New(srv.URL)
	_, err := c.Get(ctx, "/", nil)
	require.NoError(t, err)
	assert.Equal(t, traceID, gotHeader)
}

func TestTraceID_NotSetWhenMissingFromContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("X-Request-ID"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpclient.New(srv.URL)
	_, err := c.Get(context.Background(), "/", nil)
	require.NoError(t, err)
}

func TestMetrics_RecordedOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	reg := prometheus.NewRegistry()
	c := httpclient.New(srv.URL, httpclient.WithMetrics(reg))

	_, err := c.Get(context.Background(), "/", nil)
	require.NoError(t, err)

	mfs, err := reg.Gather()
	require.NoError(t, err)

	var found bool
	for _, mf := range mfs {
		if mf.GetName() == "httpclient_requests_total" {
			for _, m := range mf.GetMetric() {
				if getLabel(m, "status") == "200" && getLabel(m, "method") == "GET" {
					assert.Equal(t, float64(1), m.GetCounter().GetValue())
					found = true
				}
			}
		}
	}
	assert.True(t, found, "httpclient_requests_total{method=GET,status=200} not found")
}

func TestMetrics_RecordedOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	reg := prometheus.NewRegistry()
	c := httpclient.New(srv.URL, httpclient.WithMetrics(reg))

	_, _ = c.Get(context.Background(), "/", nil)

	mfs, err := reg.Gather()
	require.NoError(t, err)

	var found bool
	for _, mf := range mfs {
		if mf.GetName() == "httpclient_requests_total" {
			for _, m := range mf.GetMetric() {
				if getLabel(m, "status") == "500" {
					found = true
				}
			}
		}
	}
	assert.True(t, found, "httpclient_requests_total{status=500} not found")
}

func getLabel(m *dto.Metric, name string) string {
	for _, lp := range m.GetLabel() {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}
