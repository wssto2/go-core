package authz

import (
	"context"
	"fmt"
)

// Kind says what sort of principal acts: a person or a service account.
type Kind string

const (
	// KindUser is a person who signs in.
	KindUser Kind = "user"
	// KindServiceAccount is a system that only ever uses the API, with API keys.
	// It gets roles at a scope exactly like a person, but has no password and
	// never matches an Own qualifier (which compares a user ID).
	KindServiceAccount Kind = "service"
)

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool { return k == KindUser || k == KindServiceAccount }

// Subject identifies who a binding belongs to.
type Subject struct {
	Kind Kind `json:"kind"`
	ID   int  `json:"id"`
}

func (s Subject) String() string { return fmt.Sprintf("%s:%d", s.Kind, s.ID) }

// Valid reports whether the subject has a known kind and a positive ID.
func (s Subject) Valid() bool { return s.Kind.Valid() && s.ID > 0 }

// Principal is the acting subject of a request.
type Principal struct {
	Subject

	// Location is the ID, at the hierarchy's leaf level, of the principal's own
	// location. It is what the OwnLocation qualifier compares against. Zero
	// means none, and OwnLocation then matches nothing.
	Location int `json:"location,omitempty"`
}

// User is a Principal for a person.
func User(id, location int) Principal {
	return Principal{Subject: Subject{Kind: KindUser, ID: id}, Location: location}
}

// ServiceAccount is a Principal for a service account.
func ServiceAccount(id int) Principal {
	return Principal{Subject: Subject{Kind: KindServiceAccount, ID: id}}
}

type principalKey struct{}

// WithPrincipal stores the acting principal in the context. Middleware does it
// after authentication (see authzhttp).
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal stored by WithPrincipal.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok && p.Valid()
}
