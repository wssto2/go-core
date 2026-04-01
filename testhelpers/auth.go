package testhelpers

import (
	"context"

	"github.com/wssto2/go-core/auth"
)

// TestUser is a trivial Identifiable for tests.
type TestUser struct {
	ID   int
	Name string
}

func (u TestUser) GetID() int { return u.ID }

// FakeAuthProvider is a simple AuthProvider that returns a preset user or error.
type FakeAuthProvider struct {
	User auth.Identifiable
	Err  error
}

func NewFakeAuthProvider(user auth.Identifiable, err error) auth.Provider {
	return &FakeAuthProvider{User: user, Err: err}
}

func (f *FakeAuthProvider) Verify(ctx context.Context, token string) (auth.Identifiable, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.User, nil
}
