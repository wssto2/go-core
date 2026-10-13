// Package pushtest is a push service for tests: a TLS server that answers what a test tells it to, and browsers that
// can decrypt what was sent to them, so an application's tests prove a push arrived without a real push service.
//
//	svc := pushtest.NewService(t)
//	phone := svc.NewDevice(t)                       // a subscription on the service, with the keys to read it
//	sender, _ := push.NewSender(push.Config{PublicKey: pub, PrivateKey: priv, Subject: "ops@example.test", Client:
//
// svc.Client()})
//
//	// ... send to phone.Subscription ...
//	svc.Requests()[0].Payload                        // the decrypted JSON
//	svc.Answer(http.StatusGone)                      // the next pushes are refused: the subscription is gone
package pushtest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/wssto2/go-core/notification/push"
)

// Device is a browser's side of a subscription: the subscription to give the application, and the keys that read what
// is pushed to it.
type Device struct {
	Subscription push.Subscription

	private *ecdh.PrivateKey
	auth    []byte
}

// NewDevice makes a subscription at endpoint with fresh keys.
func NewDevice(tb testing.TB, endpoint string) Device {
	tb.Helper()

	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		tb.Fatalf("pushtest: %v", err)
	}

	auth := make([]byte, authSize)

	_, err = rand.Read(auth)
	if err != nil {
		tb.Fatalf("pushtest: %v", err)
	}

	enc := base64.RawURLEncoding

	return Device{
		Subscription: push.Subscription{
			Endpoint: endpoint,
			P256DH:   enc.EncodeToString(key.PublicKey().Bytes()),
			Auth:     enc.EncodeToString(auth),
		},
		private: key,
		auth:    auth,
	}
}

// The sizes RFC 8291 fixes, written out here apart from package push, so the test side does not share its mistakes.
const (
	headerSize = 21 // salt, record size and key id length
	pointSize  = 65
	saltSize   = 16
	authSize   = 16
	ikmSize    = 32
	cekSize    = 16
	nonceSize  = 12
	delimiter  = 0x02
)

var (
	errShort     = errors.New("the push body is too short")
	errDelimiter = errors.New("the padding delimiter is not 0x02")
)

// Decrypt reads a push body as the browser does (RFC 8291, section 3.4) and returns the plaintext without its padding.
func (d Device) Decrypt(tb testing.TB, body []byte) []byte {
	tb.Helper()

	plain, err := d.decrypt(body)
	if err != nil {
		tb.Fatalf("pushtest: %v", err)
	}

	return plain
}

func (d Device) decrypt(body []byte) ([]byte, error) {
	if len(body) < headerSize+pointSize {
		return nil, fmt.Errorf("%w: %d bytes", errShort, len(body))
	}

	salt, keyLen := body[:saltSize], int(body[headerSize-1])
	asPublic, record := body[headerSize:headerSize+keyLen], body[headerSize+keyLen:]

	gcm, nonce, err := d.cipherFor(salt, asPublic)
	if err != nil {
		return nil, err
	}

	plain, err := gcm.Open(nil, nonce, record, nil)
	if err != nil {
		return nil, fmt.Errorf("the body does not decrypt: %w", err)
	}

	end := len(plain) - 1
	for end > 0 && plain[end] == 0 {
		end--
	}

	if plain[end] != delimiter {
		return nil, fmt.Errorf("%w: it is %#x", errDelimiter, plain[end])
	}

	return plain[:end], nil
}

// cipherFor derives the content key and the nonce from the sender's public key and the salt.
func (d Device) cipherFor(salt, asPublic []byte) (cipher.AEAD, []byte, error) {
	peer, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		return nil, nil, fmt.Errorf("the sender's key: %w", err)
	}

	secret, err := d.private.ECDH(peer)
	if err != nil {
		return nil, nil, fmt.Errorf("ECDH: %w", err)
	}

	info := append(append([]byte("WebPush: info\x00"), d.private.PublicKey().Bytes()...), asPublic...)

	ikm, err := hkdf.Key(sha256.New, secret, d.auth, string(info), ikmSize)
	if err != nil {
		return nil, nil, fmt.Errorf("HKDF: %w", err)
	}

	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", cekSize)
	if err != nil {
		return nil, nil, fmt.Errorf("HKDF: %w", err)
	}

	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", nonceSize)
	if err != nil {
		return nil, nil, fmt.Errorf("HKDF: %w", err)
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, nil, fmt.Errorf("AES: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("GCM: %w", err)
	}

	return gcm, nonce, nil
}

// Request is one push the service received.
type Request struct {
	// Path is the endpoint's path, which tells the devices apart.
	Path   string
	Header http.Header
	// Payload is the decrypted body, nil when the path belongs to no device made here.
	Payload []byte
}

// Service is a fake push service.
type Service struct {
	srv *httptest.Server
	tb  testing.TB

	mu         sync.Mutex
	status     int
	retryAfter int
	devices    map[string]Device
	requests   []Request
	n          int
}

// NewService starts a push service that answers 201 Created, until Answer says otherwise. It stops with the test.
func NewService(tb testing.TB) *Service {
	tb.Helper()

	//nolint:exhaustruct,exhaustruct_v5 // zero by design
	s := &Service{tb: tb, status: http.StatusCreated, devices: map[string]Device{}}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	tb.Cleanup(s.srv.Close)

	return s
}

// Client is the HTTP client that trusts the service's certificate: give it to push.Config.
func (s *Service) Client() *http.Client { return s.srv.Client() }

// NewDevice makes a subscription on this service; what is pushed to it shows in Requests, decrypted.
func (s *Service) NewDevice(tb testing.TB) Device {
	tb.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.n++
	path := "/push/" + strconv.Itoa(s.n)
	d := NewDevice(tb, s.srv.URL+path)
	s.devices[path] = d

	return d
}

// Answer sets the status the service gives from now on.
func (s *Service) Answer(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status = status
}

// RetryAfter sets the Retry-After, in seconds, the service adds to its answers; 0 adds none.
func (s *Service) RetryAfter(seconds int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.retryAfter = seconds
}

// Requests are the pushes received so far, oldest first.
func (s *Service) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]Request(nil), s.requests...)
}

// Reset forgets the requests received.
func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests = nil
}

func (s *Service) serve(resp http.ResponseWriter, incoming *http.Request) {
	body, _ := io.ReadAll(incoming.Body) // a truncated body fails the test's decryption, with its own message

	s.mu.Lock()
	defer s.mu.Unlock()

	req := Request{Path: incoming.URL.Path, Header: incoming.Header.Clone(), Payload: nil}

	if d, ok := s.devices[incoming.URL.Path]; ok {
		var err error

		req.Payload, err = d.decrypt(body)
		if err != nil {
			s.tb.Errorf("pushtest: %s: %v", incoming.URL.Path, err)
		}
	}

	s.requests = append(s.requests, req)

	if s.retryAfter > 0 {
		resp.Header().Set("Retry-After", strconv.Itoa(s.retryAfter))
	}

	resp.WriteHeader(s.status)
}
