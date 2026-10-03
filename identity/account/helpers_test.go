package account_test

import (
	"context"
	"log"
	"strconv"
	"sync"
	"testing"

	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

// exampleT lets an Example use the helpers that take a testing.TB.
type exampleT struct {
	testing.TB
}

func (exampleT) Helper()                      {}
func (exampleT) Context() context.Context     { return context.Background() }
func (exampleT) Fatalf(f string, args ...any) { log.Fatalf(f, args...) }

// countingHasher counts how often passwords are compared.
type countingHasher struct {
	account.PasswordHasher
	mu      sync.Mutex
	matches int
}

func (h *countingHasher) Matches(hash, password string) bool {
	h.mu.Lock()
	h.matches++
	h.mu.Unlock()

	return h.PasswordHasher.Matches(hash, password)
}

// notices remembers what was published.
type notices struct {
	mu     sync.Mutex
	events []string
}

func (n *notices) add(s string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.events = append(n.events, s)
}

func (n *notices) SignedInAs(_ context.Context, actor, target int) {
	n.add("in " + strconv.Itoa(actor) + ">" + strconv.Itoa(target))
}
func (n *notices) SignedOutAs(_ context.Context, actor, target int) {
	n.add("out " + strconv.Itoa(actor) + ">" + strconv.Itoa(target))
}
func (n *notices) SessionsRevoked(_ context.Context, id, actor, count int) {
	n.add("revoked " + strconv.Itoa(id) + " by " + strconv.Itoa(actor) + " x" + strconv.Itoa(count))
}

// permitAll is an Impersonation that lets everybody sign in as anybody.
type permitAll struct{ refuse map[int]bool }

func (permitAll) Permitted(context.Context, account.Account) error { return nil }

func (p permitAll) Covers(_ context.Context, _, target account.Account) error {
	if p.refuse[target.ID] {
		return account.ErrNotFound // any error will do for the test
	}

	return nil
}

func seeded(t testing.TB, opts ...identitytest.Option) identitytest.Kit {
	t.Helper()

	inactive := identitytest.Account(3, "ines", "secret")
	inactive.Active = false

	return identitytest.New(t, []account.Account{
		identitytest.Account(1, "ana", "secret"),
		identitytest.Account(2, "boris", "hunter2"),
		inactive,
	}, opts...)
}
