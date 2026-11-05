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
	if _, _, err := net.SplitHostPort(cfg.Addr); err != nil {
		return nil, fmt.Errorf("proxy: Addr %q is not host:port: %w", cfg.Addr, err)
	}

	if log == nil {
		log = slog.Default()
	}

	scheme := "http"
	if cfg.HTTPS {
		scheme = "https"
	}

	dialer := &net.Dialer{Timeout: positive(cfg.DialTimeout, 5*time.Second)} //nolint:exhaustruct // the rest is the default

	//nolint:exhaustruct // the rest is the default
	transport := &http.Transport{
		// Always the configured address; the URL host stays the request's Host, which Go uses for SNI and verification.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, cfg.Addr)
		},
		TLSClientConfig:       &tls.Config{RootCAs: cfg.RootCAs, InsecureSkipVerify: cfg.InsecureSkipVerify, MinVersion: tls.VersionTLS12}, //nolint:gosec // opt-in, off by default
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: positive(cfg.HeaderTimeout, time.Minute),
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
	}

	//nolint:exhaustruct // the rest is the default
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			peer, _ := pr.In.Context().Value(peerKey{}).(Peer)

			pr.Out.URL.Scheme, pr.Out.URL.Host, pr.Out.Host = scheme, pr.In.Host, pr.In.Host

			// Rewrite drops X-Forwarded-*; put back what a trusted proxy said, so the client is appended, not replaced.
			if peer.Trusted {
				pr.Out.Header["X-Forwarded-For"] = pr.In.Header.Values("X-Forwarded-For")
			}

			pr.SetXForwarded()

			if !peer.Trusted {
				pr.Out.Header.Del("X-Real-IP")

				if ip, _, err := net.SplitHostPort(pr.In.RemoteAddr); err == nil {
					pr.Out.Header.Set("X-Real-IP", ip)
				}
			} else if proto := pr.In.Header.Get("X-Forwarded-Proto"); proto != "" {
				pr.Out.Header.Set("X-Forwarded-Proto", proto)
			}
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, context.Canceled) { // the client went away
				return
			}

			log.WarnContext(r.Context(), "proxy: upstream failed", "host", r.Host, "path", r.URL.Path, "upstream", cfg.Addr, "error", err)

			if peer, _ := r.Context().Value(peerKey{}).(Peer); peer.Failed != nil {
				peer.Failed(apperr.Internal(fmt.Errorf("proxy: upstream %s: %w", cfg.Addr, err)))
			}

			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		},
	}, nil
}

func positive(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}

	return d
}
