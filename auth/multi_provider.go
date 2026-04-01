package auth

import (
	"context"
	"errors"
)

// MultiProvider tries multiple AuthProvider implementations in order and
// returns the first successful verification. If any provider returns
// ErrExpiredToken the error is returned immediately so callers can trigger
// refresh flows.
type MultiProvider struct {
	providers []Provider
}

func NewMultiProvider(providers ...Provider) *MultiProvider {
	return &MultiProvider{providers: providers}
}

func (m *MultiProvider) Verify(ctx context.Context, token string) (Identifiable, error) {
	var lastErr error
	for _, p := range m.providers {
		user, err := p.Verify(ctx, token)
		if err == nil {
			return user, nil
		}
		// If token expired, surface that immediately so callers can take
		// appropriate action (refresh vs re-auth).
		if errors.Is(err, ErrExpiredToken) {
			return nil, ErrExpiredToken
		}
		// remember last error and try the next provider
		lastErr = err
	}
	// None matched — report unauthorized to the caller.
	_ = lastErr // keep for future diagnostics
	return nil, ErrUnauthorized
}
