package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// The limits of one Web Push message (RFC 8291, section 4): a push service need not take more than 4096 bytes of body,
// which leaves 3993 bytes of plaintext after the 86-byte header, one delimiter byte and the 16-byte GCM tag.
const (
	recordSize = 4096
	headerSize = 21 + 65 // salt, record size, key id length, and the 65-byte application server key
	tagSize    = 16

	// MaxPayload is the longest plaintext a message may carry.
	MaxPayload = recordSize - headerSize - 1 - tagSize

	// padTo is the multiple the plaintext is padded up to, so the push service cannot read the length of a notification.
	padTo = 128
)

// ErrPayloadTooLarge means the payload does not fit one Web Push record.
var ErrPayloadTooLarge = fmt.Errorf("push: the payload is longer than %d bytes", MaxPayload)

// ErrSubscriptionUnusable means the subscription's keys cannot encrypt a message: it can never be pushed to.
var ErrSubscriptionUnusable = errors.New("push: the subscription's keys are unusable")

// Encrypt encrypts payload for a subscription with RFC 8291 (aes128gcm): one record, with a fresh key pair and salt
// per message, padded to a multiple of 128 bytes. userKey is the subscription's 65-byte P-256 public key and authSecret
// its 16-byte secret. The result is the body of the push request.
func Encrypt(payload, userKey, authSecret []byte) ([]byte, error) {
	if len(payload) > MaxPayload {
		return nil, ErrPayloadTooLarge
	}

	own, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("push: generate the message key: %w", err)
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("push: generate the salt: %w", err)
	}

	pad := min(MaxPayload, (len(payload)+padTo-1)/padTo*padTo) - len(payload)

	return encrypt(payload, pad, userKey, authSecret, own, salt)
}

// encrypt is Encrypt with the key pair and salt given, which the RFC's test vector fixes.
func encrypt(payload []byte, pad int, userKey, authSecret []byte, own *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(authSecret) != 16 {
		return nil, fmt.Errorf("%w: the auth secret is %d bytes, not 16", ErrSubscriptionUnusable, len(authSecret))
	}

	peer, err := ecdh.P256().NewPublicKey(userKey) // rejects a point that is not on the curve
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSubscriptionUnusable, err)
	}

	secret, err := own.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSubscriptionUnusable, err)
	}

	ownKey := own.PublicKey().Bytes()

	// RFC 8291, section 3.4: combine the ECDH secret and the auth secret, then derive RFC 8188's key and nonce.
	keyInfo := append(append([]byte("WebPush: info\x00"), userKey...), ownKey...)

	ikm, err := hkdf.Key(sha256.New, secret, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, fmt.Errorf("push: derive the input key: %w", err)
	}

	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, fmt.Errorf("push: derive the content key: %w", err)
	}

	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, fmt.Errorf("push: derive the nonce: %w", err)
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("push: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("push: %w", err)
	}

	// The header: salt, record size, key id length and the key id, which is the application server's public key.
	body := make([]byte, 0, headerSize+len(payload)+pad+1+tagSize)
	body = append(body, salt...)
	body = binary.BigEndian.AppendUint32(body, recordSize)
	body = append(body, byte(len(ownKey))) //nolint:gosec // 65
	body = append(body, ownKey...)

	record := append(append(append([]byte{}, payload...), 0x02), make([]byte, pad)...) // delimiter, then zero padding

	return gcm.Seal(body, nonce, record, nil), nil
}
