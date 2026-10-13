package account_test

import (
	"context"
	"fmt"
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
	account.DiscardNotices
	mu     sync.Mutex
	events []string
	facts  []account.LoginAs
}

func (n *notices) add(s string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.events = append(n.events, s)
}

func (n *notices) SignedInAs(_ context.Context, f account.LoginAs) {
	n.fact(f)
	n.add("in " + strconv.Itoa(f.ActorID) + ">" + strconv.Itoa(f.TargetID))
}
func (n *notices) SignedOutAs(_ context.Context, f account.LoginAs) {
	n.fact(f)
	n.add("out " + strconv.Itoa(f.ActorID) + ">" + strconv.Itoa(f.TargetID))
}

// outOneTwo is what the notices record for a session of 2 that 1 had opened ending.
const outOneTwo = "out 1>2"

func (n *notices) SessionsEnded(_ context.Context, f account.SessionsEnded) {
	n.add(fmt.Sprintf("ended %d by %d x%d %s", f.AccountID, f.ActorID, len(f.SessionIDs), f.Reason))
}

func (n *notices) AccountChanged(_ context.Context, id int) {
	n.add("changed " + strconv.Itoa(id))
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

func (n *notices) AccountCreated(_ context.Context, id, actor int) {
	n.add("created " + strconv.Itoa(id) + " by " + strconv.Itoa(actor))
}

func (n *notices) AccountDeactivated(_ context.Context, id, actor int) {
	n.add("deactivated " + strconv.Itoa(id) + " by " + strconv.Itoa(actor))
}

func (n *notices) AccountActivated(_ context.Context, id, actor int) {
	n.add("activated " + strconv.Itoa(id) + " by " + strconv.Itoa(actor))
}

func (n *notices) PasswordChanged(_ context.Context, id, actor int) {
	n.add("password " + strconv.Itoa(id) + " by " + strconv.Itoa(actor))
}

func (n *notices) EmailChanged(_ context.Context, id int, from, to string) {
	n.add("email " + strconv.Itoa(id) + " " + from + ">" + to)
}

func (n *notices) fact(f account.LoginAs) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.facts = append(n.facts, f)
}
