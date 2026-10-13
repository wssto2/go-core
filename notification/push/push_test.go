package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func unb64(t *testing.T, s string) []byte {
	t.Helper()

	out, err := b64.DecodeString(s)
	require.NoError(t, err)

	return out
}

// The example of RFC 8291, section 5 and appendix A: the same inputs must give the RFC's body byte for byte. Padding is
// none there, so this is the single record with its delimiter.
func TestEncryptionMatchesTheRFC8291TestVector(t *testing.T) {
	asPrivate, err := ecdh.P256().NewPrivateKey(unb64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	require.NoError(t, err)
	require.Equal(t, unb64(t, "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"), asPrivate.PublicKey().Bytes())

	body, err := encrypt([]byte("When I grow up, I want to be a watermelon"), 0,
		unb64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		unb64(t, "BTBZMqHH6r4Tts7J_aSIgg"), asPrivate, unb64(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	require.NoError(t, err)

	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A" +
		"_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	require.Equal(t, want, b64.EncodeToString(body))
	require.Len(t, body, 86+41+1+16, "header, plaintext, delimiter, tag (the RFC prints Content-Length 145: an erratum)")
}

// decrypt is what a user agent does (RFC 8291, section 3.4), written apart from encrypt so a test proves one against the other.
func decrypt(t *testing.T, body []byte, uaPrivate *ecdh.PrivateKey, authSecret []byte) []byte {
	t.Helper()

	salt, keyLen := body[:16], int(body[20])
	asPublic, record := body[21:21+keyLen], body[21+keyLen:]

	peer, err := ecdh.P256().NewPublicKey(asPublic)
	require.NoError(t, err)

	secret, err := uaPrivate.ECDH(peer)
	require.NoError(t, err)

	info := append(append([]byte("WebPush: info\x00"), uaPrivate.PublicKey().Bytes()...), asPublic...)
	ikm, err := hkdf.Key(sha256.New, secret, authSecret, string(info), 32)
	require.NoError(t, err)

	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	require.NoError(t, err)

	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	require.NoError(t, err)

	block, err := aes.NewCipher(cek)
	require.NoError(t, err)

	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)

	plain, err := gcm.Open(nil, nonce, record, nil)
	require.NoError(t, err)

	end := len(plain) - 1
	for plain[end] == 0 {
		end--
	}

	require.Equal(t, byte(0x02), plain[end], "the delimiter")

	return plain[:end]
}

type device struct {
	private *ecdh.PrivateKey
	auth    []byte
	sub     Subscription
}

func newDevice(t *testing.T, endpoint string) device {
	t.Helper()

	key, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)

	auth := []byte("0123456789abcdef")

	return device{private: key, auth: auth, sub: Subscription{Endpoint: endpoint, P256DH: b64.EncodeToString(key.PublicKey().Bytes()), Auth: b64.EncodeToString(auth)}}
}

func TestAMessageIsPaddedAndReadableByTheDevice(t *testing.T) {
	d := newDevice(t, "https://push.example.test/x")

	for _, text := range []string{"", "a", strings.Repeat("é", 200), strings.Repeat("x", MaxPayload)} {
		body, err := Encrypt([]byte(text), d.private.PublicKey().Bytes(), d.auth)
		require.NoError(t, err)
		require.Equal(t, text, string(decrypt(t, body, d.private, d.auth)))
		require.LessOrEqual(t, len(body), recordSize, "within what every push service takes")
	}

	short, err := Encrypt([]byte("a"), d.private.PublicKey().Bytes(), d.auth)
	require.NoError(t, err)

	other, err := Encrypt([]byte(strings.Repeat("b", 100)), d.private.PublicKey().Bytes(), d.auth)
	require.NoError(t, err)
	require.Len(t, other, len(short), "the push service cannot read the length")
}

func TestAPayloadThatDoesNotFitAndAKeyThatIsNoPointAreRefused(t *testing.T) {
	d := newDevice(t, "https://push.example.test/x")

	_, err := Encrypt(make([]byte, MaxPayload+1), d.private.PublicKey().Bytes(), d.auth)
	require.ErrorIs(t, err, ErrPayloadTooLarge)

	_, err = Encrypt([]byte("x"), make([]byte, 65), d.auth)
	require.ErrorIs(t, err, ErrSubscriptionUnusable)

	_, err = Encrypt([]byte("x"), d.private.PublicKey().Bytes(), []byte("short"))
	require.ErrorIs(t, err, ErrSubscriptionUnusable)
}

func TestSubscriptionsAreValidated(t *testing.T) {
	good := newDevice(t, "https://push.example.test/x").sub
	require.NoError(t, good.Validate())

	bad := map[string]Subscription{
		"http":       {Endpoint: "http://push.example.test/x", P256DH: good.P256DH, Auth: good.Auth},
		"no host":    {Endpoint: "https:///x", P256DH: good.P256DH, Auth: good.Auth},
		"too long":   {Endpoint: "https://push.example.test/" + strings.Repeat("a", EndpointMax), P256DH: good.P256DH, Auth: good.Auth},
		"bad key":    {Endpoint: good.Endpoint, P256DH: b64.EncodeToString(make([]byte, 65)), Auth: good.Auth},
		"not b64":    {Endpoint: good.Endpoint, P256DH: "!!", Auth: good.Auth},
		"short auth": {Endpoint: good.Endpoint, P256DH: good.P256DH, Auth: b64.EncodeToString(make([]byte, 8))},
	}
	for name, sub := range bad {
		require.ErrorIs(t, sub.Validate(), ErrSubscriptionUnusable, name)
	}

	// The key a browser reports may be padded or standard base64.
	padded := good
	padded.P256DH = strings.NewReplacer("-", "+", "_", "/").Replace(good.P256DH) + "="
	require.NoError(t, padded.Validate())
}

func TestTheKeyPairIsChecked(t *testing.T) {
	public, private, err := GenerateKeys()
	require.NoError(t, err)

	_, err = NewSender(Config{PublicKey: public, PrivateKey: private, Subject: "mailto:ops@example.test"})
	require.NoError(t, err)

	otherPublic, _, err := GenerateKeys()
	require.NoError(t, err)

	for name, c := range map[string]Config{
		"mismatch":    {PublicKey: otherPublic, PrivateKey: private, Subject: "ops@example.test"},
		"no private":  {PublicKey: public, Subject: "ops@example.test"},
		"short pub":   {PublicKey: "AAAA", PrivateKey: private, Subject: "ops@example.test"},
		"no subject":  {PublicKey: public, PrivateKey: private},
		"bad subject": {PublicKey: public, PrivateKey: private, Subject: "ops"},
	} {
		_, err := NewSender(c)
		require.ErrorIs(t, err, ErrInvalidKeys, name)
	}

	for _, subject := range []string{"ops@example.test", "mailto:ops@example.test", "https://example.test/contact"} {
		_, err := NewSender(Config{PublicKey: public, PrivateKey: private, Subject: subject})
		require.NoError(t, err, subject)
	}
}

// A push service as the test sees it: it checks the headers, the VAPID token's signature against the public key the
// sender announced and decrypts the body as the device would.
func TestASendIsEncryptedSignedAndCarriesItsHeaders(t *testing.T) {
	var (
		got  *http.Request
		body []byte
	)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r, must(io.ReadAll(r.Body))
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	defer srv.Close()

	public, private, err := GenerateKeys()
	require.NoError(t, err)

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	sender, err := NewSender(Config{PublicKey: public, PrivateKey: private, Subject: "ops@example.test", Client: srv.Client()})
	require.NoError(t, err)

	sender.now = func() time.Time { return now }
	require.Equal(t, public, sender.PublicKey())

	d := newDevice(t, srv.URL+"/push/abc")

	res, err := sender.Send(t.Context(), d.sub, []byte(`{"title":"Hi"}`), Options{TTL: 90 * time.Minute, Urgency: UrgencyHigh})
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, res.Status)
	require.Equal(t, 2*time.Minute, res.RetryAfter)
	require.Contains(t, res.Detail, "slow down")

	require.Equal(t, "POST", got.Method)
	require.Equal(t, "/push/abc", got.URL.Path)
	require.Equal(t, "aes128gcm", got.Header.Get("Content-Encoding"))
	require.Equal(t, "5400", got.Header.Get("TTL"))
	require.Equal(t, "high", got.Header.Get("Urgency"))
	require.Equal(t, `{"title":"Hi"}`, string(decrypt(t, body, d.private, d.auth)))

	// Authorization: vapid t=<jwt>, k=<public key>
	auth := got.Header.Get("Authorization")
	token, key, ok := strings.Cut(strings.TrimPrefix(auth, "vapid t="), ", k=")
	require.True(t, ok, auth)
	require.Equal(t, public, key)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	require.JSONEq(t, `{"typ":"JWT","alg":"ES256"}`, string(unb64(t, parts[0])))

	var claims struct {
		Aud, Sub string
		Exp      int64
	}
	require.NoError(t, json.Unmarshal(unb64(t, parts[1]), &claims))
	require.Equal(t, "https://"+got.Host, claims.Aud, "the origin of the endpoint")
	require.Equal(t, "mailto:ops@example.test", claims.Sub)
	require.Equal(t, now.Add(vapidLifetime).Unix(), claims.Exp)

	sig := unb64(t, parts[2])
	require.Len(t, sig, 64)

	pub, err := ecdh.P256().NewPublicKey(unb64(t, key))
	require.NoError(t, err)

	x, y := elliptic.Unmarshal(elliptic.P256(), pub.Bytes()) //nolint:staticcheck // the test builds a verification key from the raw point
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	require.True(t, ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])))
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

func TestTheDefaultClientNeverConnectsToAPrivateAddress(t *testing.T) {
	hit := false

	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()

	public, private, err := GenerateKeys()
	require.NoError(t, err)

	sender, err := NewSender(Config{PublicKey: public, PrivateKey: private, Subject: "ops@example.test"})
	require.NoError(t, err)

	_, err = sender.Send(t.Context(), newDevice(t, srv.URL).sub, []byte("x"), Options{})
	require.ErrorIs(t, err, errNotPublic)
	require.False(t, hit, "a loopback server is not reached")
}

func TestASubscriptionThatCannotEncryptIsNotSent(t *testing.T) {
	public, private, err := GenerateKeys()
	require.NoError(t, err)

	sender, err := NewSender(Config{PublicKey: public, PrivateKey: private, Subject: "ops@example.test"})
	require.NoError(t, err)

	_, err = sender.Send(t.Context(), Subscription{Endpoint: "https://push.example.test/x", P256DH: "AAAA", Auth: "AAAA"}, []byte("x"), Options{})
	require.True(t, errors.Is(err, ErrSubscriptionUnusable))
}

func TestRetryAfterIsSecondsOrADate(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	require.Equal(t, 30*time.Second, retryAfter("30", now))
	require.Equal(t, time.Minute, retryAfter(now.Add(time.Minute).Format(http.TimeFormat), now))
	require.Zero(t, retryAfter("", now))
	require.Zero(t, retryAfter("soon", now))
	require.Zero(t, retryAfter("-5", now))
}
