// Package httpclient provides a reusable HTTP client for calling external APIs.
// It handles authentication (Bearer, API key, Basic, OAuth2 client credentials),
// request/response encoding (JSON, XML), and consistent error handling.
//
// The client is safe for concurrent use by any number of goroutines. A single
// Client instance should be shared across the application (not created per-request)
// so its connection pool is reused effectively.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/observability/tracing"
	"github.com/wssto2/go-core/resilience"
)

// Client is a configured HTTP client for a single external API base URL.
// Create one instance per external API and reuse it across the application.
type Client struct {
	baseURL    string
	httpClient *http.Client
	auth       AuthProvider
	log        *slog.Logger
	headers    map[string]string

	cb            *resilience.CircuitBreaker
	retryAttempts int
	retryBackoff  time.Duration

	// outbound metrics (nil = disabled)
	metricRequests  *prometheus.CounterVec
	metricDuration  *prometheus.HistogramVec
}

// Option configures a Client.
type Option func(*Client)

// WithTimeout sets a custom HTTP timeout (default: 30s).
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// WithLogger attaches a logger for request/response tracing.
func WithLogger(log *slog.Logger) Option {
	return func(c *Client) { c.log = log }
}

// WithHeader sets a default header sent on every request.
func WithHeader(key, value string) Option {
	return func(c *Client) { c.headers[key] = value }
}

// WithAuth sets the authentication provider.
func WithAuth(a AuthProvider) Option {
	return func(c *Client) { c.auth = a }
}

// WithRetry enables automatic retry with exponential backoff for failed requests.
// Only use this for idempotent operations (GET, DELETE, PUT) or when you are sure
// the target API handles duplicate requests safely.
// attempts must be >= 1; initialBackoff is the first wait interval (doubled each retry).
func WithRetry(attempts int, initialBackoff time.Duration) Option {
	return func(c *Client) {
		c.retryAttempts = attempts
		c.retryBackoff = initialBackoff
	}
}

// WithCircuitBreaker attaches a circuit breaker to the client.
// When failureThreshold consecutive requests fail, the breaker opens and
// subsequent calls return ErrCircuitOpen immediately (without hitting the network)
// until openTimeout elapses and the breaker allows a trial request through.
//
// This prevents goroutine pile-ups when an external API is down: instead of
// every goroutine waiting for the full HTTP timeout, they fail fast.
func WithCircuitBreaker(failureThreshold int, openTimeout time.Duration) Option {
	return func(c *Client) {
		c.cb = resilience.NewCircuitBreaker(failureThreshold, openTimeout)
	}
}

// WithMetrics registers Prometheus counters and histograms for outbound requests.
// Use the same registerer as the rest of the application (from observability.Telemetry)
// so metrics are served on the existing /metrics endpoint.
//
// Recorded metrics:
//
//	httpclient_requests_total{host, method, status}    – request count per outcome
//	httpclient_request_duration_seconds{host, method}  – latency histogram
func WithMetrics(reg prometheus.Registerer) Option {
	return func(c *Client) {
		host := hostFromURL(c.baseURL)

		requests := prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   "httpclient",
			Name:        "requests_total",
			Help:        "Total outbound HTTP requests made by httpclient.",
			ConstLabels: prometheus.Labels{"host": host},
		}, []string{"method", "status"})

		duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   "httpclient",
			Name:        "request_duration_seconds",
			Help:        "Latency of outbound HTTP requests made by httpclient.",
			Buckets:     prometheus.DefBuckets,
			ConstLabels: prometheus.Labels{"host": host},
		}, []string{"method"})

		reg.MustRegister(requests, duration)
		c.metricRequests = requests
		c.metricDuration = duration
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = resilience.ErrOpen

// New constructs a Client for the given base URL.
//
// A single Client instance is safe for concurrent use and should be shared
// across the application rather than created per-request. The underlying
// http.Transport maintains a connection pool; sharing the client allows
// connections to be reused, reducing latency and TCP connection churn.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				// Go's default is 2, which causes excessive connection churn under
				// high concurrency. 100 idle connections per host is a reasonable
				// starting point for a service with many concurrent outbound requests.
				MaxIdleConnsPerHost: 100,
				MaxIdleConns:        500,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		headers: make(map[string]string),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Get performs a GET request and decodes the response body into out.
// Pass nil for out to discard the response body.
func (c *Client) Get(ctx context.Context, path string, out any) (*http.Response, error) {
	return c.do(ctx, http.MethodGet, path, nil, "", out)
}

// Post performs a POST request, encoding body as JSON, and decodes the response into out.
func (c *Client) Post(ctx context.Context, path string, body, out any) (*http.Response, error) {
	return c.doEncoded(ctx, http.MethodPost, path, body, out)
}

// Put performs a PUT request, encoding body as JSON, and decodes the response into out.
func (c *Client) Put(ctx context.Context, path string, body, out any) (*http.Response, error) {
	return c.doEncoded(ctx, http.MethodPut, path, body, out)
}

// Patch performs a PATCH request, encoding body as JSON, and decodes the response into out.
func (c *Client) Patch(ctx context.Context, path string, body, out any) (*http.Response, error) {
	return c.doEncoded(ctx, http.MethodPatch, path, body, out)
}

// Delete performs a DELETE request and decodes the response into out.
func (c *Client) Delete(ctx context.Context, path string, out any) (*http.Response, error) {
	return c.do(ctx, http.MethodDelete, path, nil, "", out)
}

func (c *Client) doEncoded(ctx context.Context, method, path string, body, out any) (*http.Response, error) {
	var bodyBytes []byte
	var contentType string

	if body != nil {
		switch v := body.(type) {
		case XMLBody:
			b, err := xml.Marshal(v.V)
			if err != nil {
				return nil, apperr.Internal(fmt.Errorf("httpclient: xml encode: %w", err))
			}
			bodyBytes = b
			contentType = "application/xml"
		default:
			b, err := json.Marshal(body)
			if err != nil {
				return nil, apperr.Internal(fmt.Errorf("httpclient: json encode: %w", err))
			}
			bodyBytes = b
			contentType = "application/json"
		}
	}

	return c.do(ctx, method, path, bodyBytes, contentType, out)
}

// do orchestrates retry and circuit breaker around a single HTTP call.
// bodyBytes is serialized once before any retry attempt so the reader can be
// recreated on each attempt — an io.Reader would be exhausted after the first try.
func (c *Client) do(ctx context.Context, method, path string, bodyBytes []byte, contentType string, out any) (*http.Response, error) {
	var (
		lastResp *http.Response
	)

	attempt := func(ctx context.Context) error {
		resp, err := c.doOnce(ctx, method, path, bodyBytes, contentType, out)
		lastResp = resp
		return err
	}

	var exec func(context.Context) error
	if c.retryAttempts > 1 {
		exec = func(ctx context.Context) error {
			return resilience.Retry(ctx, c.retryAttempts, c.retryBackoff, attempt)
		}
	} else {
		exec = attempt
	}

	if c.cb != nil {
		err := c.cb.Execute(ctx, exec)
		return lastResp, err
	}
	err := exec(ctx)
	return lastResp, err
}

// doOnce performs a single HTTP round-trip. bodyBytes is converted to a fresh
// reader on each call so retry attempts always send the full body.
func (c *Client) doOnce(ctx context.Context, method, path string, bodyBytes []byte, contentType string, out any) (*http.Response, error) {
	u := c.baseURL + "/" + strings.TrimLeft(path, "/")

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("httpclient: build request: %w", err))
	}

	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Propagate the trace ID from context so the external API can correlate
	// its own logs with ours. The header is only set when a trace ID exists.
	if traceID, ok := tracing.TraceIDFromContext(ctx); ok {
		req.Header.Set("X-Request-ID", traceID)
	}

	if c.auth != nil {
		if err := c.auth.Apply(ctx, req); err != nil {
			return nil, apperr.Internal(fmt.Errorf("httpclient: auth: %w", err))
		}
	}

	start := time.Now()

	if c.log != nil {
		c.log.DebugContext(ctx, "httpclient: request", "method", method, "url", u)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.recordMetrics(method, "error", time.Since(start))
		return nil, apperr.Internal(fmt.Errorf("httpclient: %w", err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.recordMetrics(method, "error", time.Since(start))
		return resp, apperr.Internal(fmt.Errorf("httpclient: read body: %w", err))
	}

	statusLabel := fmt.Sprintf("%d", resp.StatusCode)
	c.recordMetrics(method, statusLabel, time.Since(start))

	if c.log != nil {
		c.log.DebugContext(ctx, "httpclient: response", "method", method, "url", u, "status", resp.StatusCode)
	}

	if resp.StatusCode >= 400 {
		return resp, &APIError{StatusCode: resp.StatusCode, Body: raw}
	}

	if out != nil && len(raw) > 0 {
		if err := decode(resp, raw, out); err != nil {
			return resp, apperr.Internal(fmt.Errorf("httpclient: decode: %w", err))
		}
	}

	return resp, nil
}

func (c *Client) recordMetrics(method, status string, elapsed time.Duration) {
	if c.metricRequests != nil {
		c.metricRequests.WithLabelValues(method, status).Inc()
	}
	if c.metricDuration != nil {
		c.metricDuration.WithLabelValues(method).Observe(elapsed.Seconds())
	}
}

func hostFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}

func decode(resp *http.Response, raw []byte, out any) error {
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "xml") {
		return xml.Unmarshal(raw, out)
	}
	return json.Unmarshal(raw, out)
}

// XMLBody wraps a value to signal it should be encoded as XML instead of JSON.
type XMLBody struct{ V any }

// APIError is returned when the server responds with a 4xx or 5xx status.
type APIError struct {
	StatusCode int
	Body       []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("httpclient: server returned %d: %s", e.StatusCode, string(e.Body))
}

// IsStatus returns true if the error is an APIError with the given status code.
func IsStatus(err error, code int) bool {
	if err == nil {
		return false
	}
	if aerr, ok := err.(*APIError); ok {
		return aerr.StatusCode == code
	}
	return false
}

// AuthProvider applies authentication to an outgoing request.
type AuthProvider interface {
	Apply(ctx context.Context, req *http.Request) error
}

// BearerAuth sets a static Bearer token on every request.
type BearerAuth struct{ Token string }

func (b BearerAuth) Apply(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer "+b.Token)
	return nil
}

// APIKeyAuth sets an API key in a header or query param.
type APIKeyAuth struct {
	Key        string
	HeaderName string // e.g. "X-Api-Key"; if empty, uses query param
	QueryParam string // e.g. "api_key"; used when HeaderName is empty
}

func (a APIKeyAuth) Apply(_ context.Context, req *http.Request) error {
	if a.HeaderName != "" {
		req.Header.Set(a.HeaderName, a.Key)
	} else if a.QueryParam != "" {
		q := req.URL.Query()
		q.Set(a.QueryParam, a.Key)
		req.URL.RawQuery = q.Encode()
	}
	return nil
}

// BasicAuth sets HTTP Basic authentication.
type BasicAuth struct {
	Username string
	Password string
}

func (b BasicAuth) Apply(_ context.Context, req *http.Request) error {
	req.SetBasicAuth(b.Username, b.Password)
	return nil
}

// OAuth2ClientCredentials fetches and caches tokens using the client_credentials grant.
// It automatically refreshes the token when it expires.
type OAuth2ClientCredentials struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string

	mu         sync.Mutex
	token      string
	expiry     time.Time
	httpClient *http.Client
}

// NewOAuth2ClientCredentials constructs an OAuth2 provider.
// An optional custom *http.Client can be passed (e.g. to set TLS or proxy); pass nil for default.
func NewOAuth2ClientCredentials(tokenURL, clientID, clientSecret string, scopes []string, hc *http.Client) *OAuth2ClientCredentials {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &OAuth2ClientCredentials{
		TokenURL:     tokenURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       scopes,
		httpClient:   hc,
	}
}

func (o *OAuth2ClientCredentials) Apply(ctx context.Context, req *http.Request) error {
	token, err := o.getToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func (o *OAuth2ClientCredentials) getToken(ctx context.Context) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.token != "" && time.Now().Before(o.expiry) {
		return o.token, nil
	}

	vals := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {o.ClientID},
		"client_secret": {o.ClientSecret},
	}
	if len(o.Scopes) > 0 {
		vals.Set("scope", strings.Join(o.Scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL, strings.NewReader(vals.Encode()))
	if err != nil {
		return "", fmt.Errorf("oauth2: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth2: token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("oauth2: token endpoint returned %d: %s", resp.StatusCode, string(b))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("oauth2: decode token response: %w", err)
	}

	o.token = tok.AccessToken
	if tok.ExpiresIn > 0 {
		// Subtract 10s buffer to avoid using a token that expires in-flight.
		o.expiry = time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second - 10*time.Second)
	} else {
		o.expiry = time.Now().Add(55 * time.Minute)
	}

	return o.token, nil
}
