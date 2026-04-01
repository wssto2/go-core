package auth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are the standard fields encoded in every JWT token.
// App-specific data is NOT stored in the token — it is loaded
// from the database after token validation by the app's resolver.
type Claims struct {
	jwt.RegisteredClaims

	UserID   int      `json:"user_id"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Roles    []string `json:"roles"`
	Language string   `json:"language"`
}

// TokenConfig holds the configuration required to sign and parse tokens.
// Algorithm can be "HS256" (default) or "RS256". For RS256 provide the RSA
// keys as PEM-encoded strings in RSAPrivateKeyPEM and RSAPublicKeyPEM.
type TokenConfig struct {
	SecretKey        string
	Issuer           string
	Audience         string
	TokenDuration    time.Duration
	Algorithm        string
	RSAPrivateKeyPEM string
	RSAPublicKeyPEM  string
}

// ParseToken validates a JWT string and returns its claims.
// It returns ErrMissingToken, ErrInvalidToken, ErrExpiredToken, or ErrInvalidClaims
// depending on what failed — callers can use errors.Is() to handle each case.
func ParseToken(tokenString string, cfg TokenConfig) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrMissingToken
	}

	alg := cfg.Algorithm
	if alg == "" {
		alg = "HS256"
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		switch alg {
		case "HS256":
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(cfg.SecretKey), nil
		case "RS256":
			if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			pub, err := parseRSAPublicKeyFromPEM([]byte(cfg.RSAPublicKeyPEM))
			if err != nil {
				return nil, fmt.Errorf("parse public key: %w", err)
			}
			return pub, nil
		default:
			return nil, fmt.Errorf("unsupported signing algorithm: %s", alg)
		}
	})

	if err != nil {
		// Distinguish expiry from other invalidity so callers can respond differently
		// (e.g. refresh vs. re-login)
		if isExpiredError(err) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidClaims
	}

	// Validate issuer if configured
	if cfg.Issuer != "" && claims.Issuer != cfg.Issuer {
		return nil, ErrInvalidClaims
	}

	// Validate audience if configured
	if cfg.Audience != "" {
		found := false
		for _, a := range claims.Audience {
			if a == cfg.Audience {
				found = true
				break
			}
		}
		if !found {
			return nil, ErrInvalidClaims
		}
	}

	return claims, nil
}

// IssueToken creates and signs a new JWT token from the given claims.
func IssueToken(claims Claims, cfg TokenConfig) (string, error) {
	now := time.Now()

	rc := jwt.RegisteredClaims{
		Issuer:    cfg.Issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(cfg.TokenDuration)),
	}
	if cfg.Audience != "" {
		rc.Audience = jwt.ClaimStrings{cfg.Audience}
	}
	claims.RegisteredClaims = rc

	alg := cfg.Algorithm
	if alg == "" {
		alg = "HS256"
	}

	switch alg {
	case "HS256":
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString([]byte(cfg.SecretKey))
		if err != nil {
			return "", fmt.Errorf("IssueToken: failed to sign: %w", err)
		}
		return signed, nil
	case "RS256":
		priv, err := parseRSAPrivateKeyFromPEM([]byte(cfg.RSAPrivateKeyPEM))
		if err != nil {
			return "", fmt.Errorf("IssueToken: parse private key: %w", err)
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		signed, err := token.SignedString(priv)
		if err != nil {
			return "", fmt.Errorf("IssueToken: failed to sign: %w", err)
		}
		return signed, nil
	default:
		return "", fmt.Errorf("unsupported signing algorithm: %s", alg)
	}
}

func isExpiredError(err error) bool {
	return errors.Is(err, jwt.ErrTokenExpired)
}

// ---------------- helpers for RSA keys ----------------

func parseRSAPrivateKeyFromPEM(pemBytes []byte) (*rsa.PrivateKey, error) {
	if len(pemBytes) == 0 {
		return nil, fmt.Errorf("empty private key PEM")
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM block")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		switch key := k.(type) {
		case *rsa.PrivateKey:
			return key, nil
		default:
			return nil, fmt.Errorf("unexpected private key type: %T", key)
		}
	default:
		return nil, fmt.Errorf("unsupported private key type: %s", block.Type)
	}
}

func parseRSAPublicKeyFromPEM(pemBytes []byte) (*rsa.PublicKey, error) {
	if len(pemBytes) == 0 {
		return nil, fmt.Errorf("empty public key PEM")
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM block")
	}
	switch block.Type {
	case "PUBLIC KEY":
		k, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		switch key := k.(type) {
		case *rsa.PublicKey:
			return key, nil
		default:
			return nil, fmt.Errorf("unexpected public key type: %T", key)
		}
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported public key type: %s", block.Type)
	}
}
