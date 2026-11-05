package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/wssto2/go-core/apperr"
)

// Transport limits that are not worth a setting.
const (
	defaultDial      = 5 * time.Second
	defaultHeader    = time.Minute
	handshakeTimeout = 10 * time.Second
	idlePerHost      = 16 // a site has one pool per hostname it serves
	idleTimeout      = 90 * time.Second
)

// Config says where the upstream is and how long to wait for it.
type Config struct {
	// Addr is the host:port every request is sent to, whatever its Host.
	Addr string
	// HTTPS reaches the upstream over TLS, with the request's Host as the server name.
	HTTPS bool
	// DialTimeout limits connecting; 5 seconds when zero.
	DialTimeout time.Duration
	// HeaderTimeout limits the wait for the response header after the request is sent; 60 seconds when zero.
	HeaderTimeout time.Duration
	// RootCAs verifies the upstream certificate instead of the system roots (a private CA).
	RootCAs *x509.CertPool
	// InsecureSkipVerify turns certificate verification off. Leave it false.
	InsecureSkipVerify bool
}

// Peer is what the server knows about who sent a request and cannot be read from it.
type Peer struct {
	// Trusted says the sender is a trusted proxy, so its X-Forwarded-* and X-Real-IP headers are believed.
	Trusted bool
	// Failed is called with an upstream failure, once per request; nil to only log it.
	Failed func(err error)
}

type peerKey struct{}

// WithPeer attaches p to the context of a request handed to the handler New returns.
func WithPeer(ctx context.Context, p Peer) context.Context {
	return context.WithValue(ctx, peerKey{}, p)
}

// New returns a handler that forwards every request to cfg.Addr. A nil log is slog.Default().
func New(cfg Config, log *slog.Logger) (http.Handler, error) {
	_, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("proxy: Addr %q is not host:port: %w", cfg.Addr, err)
	}

	if log == nil {
		log = slog.Default()
	}

	//nolint:exhaustruct,exhaustruct_v5 // the rest is the default
	return &httputil.ReverseProxy{
		Rewrite:      rewriter(cfg),
		Transport:    newTransport(cfg),
		ErrorHandler: failureHandler(cfg, log),
	}, nil
}

// newTransport dials cfg.Addr whatever the URL says; the URL host stays the request's Host, which Go uses for the TLS
// server name and the certificate check.
func newTransport(cfg Config) *http.Transport {
	//nolint:exhaustruct,exhaustruct_v5 // the rest is the default
	dialer := &net.Dialer{Timeout: positive(cfg.DialTimeout, defaultDial)}

	//nolint:exhaustruct,exhaustruct_v5 // the rest is the default
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, cfg.Addr)
		},
		//nolint:exhaustruct,exhaustruct_v5,gosec // the rest is the default; skipping verification is opt-in
		TLSClientConfig: &tls.Config{
			RootCAs: cfg.RootCAs, InsecureSkipVerify: cfg.InsecureSkipVerify, MinVersion: tls.VersionTLS12,
		},
		TLSHandshakeTimeout:   handshakeTimeout,
		ResponseHeaderTimeout: positive(cfg.HeaderTimeout, defaultHeader),
		MaxIdleConnsPerHost:   idlePerHost,
		IdleConnTimeout:       idleTimeout,
	}
}

// rewriter keeps the Host, points the URL at the upstream's scheme, and sets the forwarding headers.
func rewriter(cfg Config) func(*httputil.ProxyRequest) {
	scheme := "http"
	if cfg.HTTPS {
		scheme = "https"
	}

	return func(pr *httputil.ProxyRequest) { //nolint:varnamelen // the name the standard library documents
		peer, _ := pr.In.Context().Value(peerKey{}).(Peer)

		pr.Out.URL.Scheme, pr.Out.URL.Host, pr.Out.Host = scheme, pr.In.Host, pr.In.Host

		// Rewrite drops X-Forwarded-*; put back what a trusted proxy said, so the client is appended, not replaced.
		if peer.Trusted {
			pr.Out.Header["X-Forwarded-For"] = pr.In.Header.Values("X-Forwarded-For")
		}

		pr.SetXForwarded()

		if !peer.Trusted {
			pr.Out.Header.Del("X-Real-IP")

			ip, _, err := net.SplitHostPort(pr.In.RemoteAddr)
			if err == nil {
				pr.Out.Header.Set("X-Real-IP", ip)
			}
		} else if proto := pr.In.Header.Get("X-Forwarded-Proto"); proto != "" {
			pr.Out.Header.Set("X-Forwarded-Proto", proto)
		}
	}
}

// failureHandler answers 502, logs, and hands the failure to the peer's Failed. A client that went away is no failure.
func failureHandler(cfg Config, log *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(resp http.ResponseWriter, req *http.Request, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}

		log.WarnContext(req.Context(), "proxy: upstream failed",
			"host", req.Host, "path", req.URL.Path, "upstream", cfg.Addr, "error", err)

		peer, _ := req.Context().Value(peerKey{}).(Peer)
		if peer.Failed != nil {
			peer.Failed(apperr.Internal(fmt.Errorf("proxy: upstream %s: %w", cfg.Addr, err)))
		}

		http.Error(resp, "Bad Gateway", http.StatusBadGateway)
	}
}

func positive(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}

	return d
}
