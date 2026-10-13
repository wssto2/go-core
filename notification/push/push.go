// Package push sends Web Push messages (RFC 8030) with the standard library only: the payload is encrypted with
// RFC 8291 (aes128gcm) and the request is authorised with a VAPID token (RFC 8292, ES256). It knows nothing of
// notifications; package notification uses it through notification.WithPush.
//
//	public, private, _ := push.GenerateKeys() // once per environment; keep both stable
//	sender, err := push.NewSender(push.Config{PublicKey: public, PrivateKey: private, Subject: "mailto:ops@example.com"})
//	result, err := sender.Send(ctx, sub, payload, push.Options{TTL: time.Hour, Urgency: push.UrgencyHigh})
//
// The endpoint of a subscription is chosen by the person who subscribes, so the default HTTP client refuses to connect
// to loopback, private and link-local addresses: a subscription cannot point the server at its own network.
package push

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// EndpointMax, KeyMax and AuthMax bound a Subscription: the longest endpoint and the longest keys, as text.
const (
	EndpointMax = 1024
	KeyMax      = 128
	AuthMax     = 64

	requestTimeout = 15 * time.Second
	// detailMax bounds how much of a push service's error answer is kept.
	detailMax = 300
)

// Urgency is the Urgency header of RFC 8030: how soon the push service should wake the device.
type Urgency string

// The two urgencies notifications use.
const (
	UrgencyNormal Urgency = "normal"
	UrgencyHigh   Urgency = "high"
)

// Config is the application server's identity.
type Config struct {
	// PublicKey and PrivateKey are the VAPID key pair from GenerateKeys, base64url. They are per environment and stable.
	PublicKey  string
	PrivateKey string
	// Subject is how a push service reaches the operator: an e-mail address (mailto: is added) or an https URL.
	Subject string
	// Client sends the requests. The default times out after 15 seconds, follows no redirect and refuses private
	// addresses; give your own only to change that, such as a client with a proxy, or in a test.
	Client *http.Client
}

// Subscription is what a browser's Push API reports for one device.
type Subscription struct {
	Endpoint string
	// P256DH is the device's public key and Auth its secret, both base64url.
	P256DH string
	Auth   string
}

// Validate accepts only what can be pushed to: an https endpoint, a key that is a point on P-256 and a 16-byte secret.
func (s Subscription) Validate() error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(s.Endpoint) > EndpointMax {
		return fmt.Errorf("%w: the endpoint is not an https URL of at most %d characters",
			ErrSubscriptionUnusable, EndpointMax)
	}

	_, _, err = s.keys()

	return err
}

func (s Subscription) keys() (userKey, authSecret []byte, err error) {
	userKey, err = decode(s.P256DH)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: the key is not base64", ErrSubscriptionUnusable)
	}

	if _, err := ecdh.P256().NewPublicKey(userKey); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrSubscriptionUnusable, err)
	}

	authSecret, err = decode(s.Auth)
	if err != nil || len(authSecret) != 16 {
		return nil, nil, fmt.Errorf("%w: the auth secret is not 16 bytes of base64", ErrSubscriptionUnusable)
	}

	return userKey, authSecret, nil
}

// Options are the headers of one push.
type Options struct {
	// TTL is how long the push service keeps the message for a device that is off; zero is "deliver now or drop it".
	TTL     time.Duration
	Urgency Urgency
}

// Result is what the push service answered. Status is 2xx when it took the message.
type Result struct {
	Status int
	// RetryAfter is the answer's Retry-After, zero when absent.
	RetryAfter time.Duration
	// Detail is a short text for a failure: the status line and the start of the body.
	Detail string
}

// Sender sends messages with one VAPID identity.
type Sender struct {
	signer *signer
	client *http.Client
	now    func() time.Time
}

// NewSender checks the key pair and the subject and returns a sender.
func NewSender(c Config) (*Sender, error) {
	sg, err := newSigner(c.PublicKey, c.PrivateKey, c.Subject)
	if err != nil {
		return nil, err
	}

	client := c.Client
	if client == nil {
		client = publicOnlyClient()
	}

	return &Sender{signer: sg, client: client, now: time.Now}, nil
}

// PublicKey is the application server key a browser subscribes with, base64url.
func (s *Sender) PublicKey() string { return s.signer.public }

// Send encrypts payload for the subscription and posts it. An answer of any status comes back as a Result; an error
// means no answer arrived (the network, a timeout) or, wrapping ErrSubscriptionUnusable or ErrPayloadTooLarge, that the
// message can never be sent.
func (s *Sender) Send(ctx context.Context, sub Subscription, payload []byte, o Options) (Result, error) {
	userKey, authSecret, err := sub.keys()
	if err != nil {
		return Result{}, err
	}

	body, err := Encrypt(payload, userKey, authSecret)
	if err != nil {
		return Result{}, err
	}

	auth, err := s.signer.authorization(sub.Endpoint, s.now())
	if err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrSubscriptionUnusable, err)
	}

	urgency := o.Urgency
	if urgency == "" {
		urgency = UrgencyNormal
	}

	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(max(int(o.TTL.Seconds()), 0)))
	req.Header.Set("Urgency", string(urgency))

	resp, err := s.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("push: send: %w", err)
	}

	defer func() { _ = resp.Body.Close() }() // the answer is read; a failed close changes nothing

	res := Result{Status: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After"), s.now())}

	if resp.StatusCode >= http.StatusMultipleChoices {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, detailMax)) // best effort: the status is what counts
		res.Detail = strings.TrimSpace(resp.Status + " " + string(detail))
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16)) // lets the connection be reused

	return res, nil
}

// retryAfter reads Retry-After as seconds or an HTTP date.
func retryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(max(seconds, 0)) * time.Second
	}

	if at, err := http.ParseTime(value); err == nil {
		return max(at.Sub(now), 0)
	}

	return 0
}

var errNotPublic = errors.New("push: the address is not a public one")

// publicOnlyClient is the default client: it connects only to public addresses, checked on the resolved address so a
// name that points inside does not pass, follows no redirect and gives up after requestTimeout.
func publicOnlyClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: requestTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return errNotPublic
			}

			ip := net.ParseIP(host)
			if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				return errNotPublic
			}

			return nil
		},
	}

	transport, _ := http.DefaultTransport.(*http.Transport) // always a *Transport unless the program replaced it
	transport = transport.Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil // a proxy would be dialled instead of the endpoint; give Config.Client for one

	return &http.Client{
		Timeout:       requestTimeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
