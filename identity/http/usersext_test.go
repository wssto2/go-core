//nolint:exhaustruct,exhaustruct_v5,forcetypeassert,goconst,lll,noinlineerr,paralleltest,tagliatelle,varnamelen,wrapcheck // tests in the style of the package's others
package http_test

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
)

// dealers is a UserExtension that keeps a dealer per person, as an application's own table would.
type dealers struct {
	mu     sync.Mutex
	dealer map[int]int
	calls  []string
	refuse error // returned by Update and Deactivate
}

func (d *dealers) Ext(_ context.Context, ids []int) (map[int]any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := map[int]any{}

	for _, id := range ids {
		if dealer, ok := d.dealer[id]; ok {
			out[id] = map[string]any{"dealer_id": dealer}
		}
	}

	return out, nil
}

func (d *dealers) Create(_ context.Context, id int, ext json.RawMessage, actor int) (any, error) {
	var in struct {
		DealerID int `json:"dealer_id"`
	}

	if len(ext) > 0 {
		if err := json.Unmarshal(ext, &in); err != nil {
			return nil, err
		}
	}

	d.mu.Lock()
	d.dealer[id] = in.DealerID
	d.mu.Unlock()
	d.log("create " + itoa(id) + " " + string(ext) + " by " + itoa(actor))

	return map[string]any{"skipped": []string{}}, nil
}

func (d *dealers) Update(_ context.Context, id int, ext json.RawMessage, actor int) error {
	d.log("update " + itoa(id) + " " + string(ext) + " by " + itoa(actor))

	if d.refuse != nil {
		return d.refuse
	}

	var in struct {
		DealerID int `json:"dealer_id"`
	}

	if len(ext) > 0 {
		_ = json.Unmarshal(ext, &in)

		d.mu.Lock()
		d.dealer[id] = in.DealerID
		d.mu.Unlock()
	}

	return nil
}

func (d *dealers) Deactivate(_ context.Context, a account.Account, details json.RawMessage, actor int) (any, error) {
	d.log("deactivate " + itoa(a.ID) + " " + string(details) + " by " + itoa(actor))

	if d.refuse != nil {
		return nil, d.refuse
	}

	return map[string]any{"moved": 3}, nil
}

func (d *dealers) log(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.calls = append(d.calls, s)
}

func (d *dealers) take() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := d.calls
	d.calls = nil

	return out
}

func extHarness(t *testing.T, x *dealers) (*harness, func(*nethttp.Request)) {
	t.Helper()

	h := newHarness(t, nil, identitytest.WithUserExtension(x))

	return h, h.as("ana", "secret")
}

func newDealers() *dealers { return &dealers{dealer: map[int]int{1: 10, 2: 20}} }

// ext is on the rows and the record, and read once per page.
func TestTheExtensionIsOnRowsAndRecord(t *testing.T) {
	h, ana := extHarness(t, newDealers())

	rows := changesRows(t, h.do(nethttp.MethodGet, "/v1/iam/users?view=all&order_col=login", nil, ana))
	require.Len(t, rows, 3)
	assert.Equal(t, map[string]any{"dealer_id": 10.0}, rows[0]["ext"], "ana")
	assert.Equal(t, map[string]any{"dealer_id": 20.0}, rows[1]["ext"], "boris")
	assert.NotContains(t, rows[2], "ext", "ines has none")

	shown := data(t, h.do(nethttp.MethodGet, "/v1/iam/users/2", nil, ana))
	assert.Equal(t, map[string]any{"dealer_id": 20.0}, shown["ext"])
}

// Without an extension nothing about the shape changes.
func TestWithoutAnExtensionThereIsNoExt(t *testing.T) {
	h := newHarness(t, nil)
	ana := h.as("ana", "secret")

	assert.NotContains(t, h.do(nethttp.MethodGet, "/v1/iam/users/2", nil, ana).Body.String(), `"ext"`)
	assert.NotContains(t, h.do(nethttp.MethodGet, "/v1/iam/users", nil, ana).Body.String(), `"ext"`)
	assert.JSONEq(t, `{"success":true,"data":{}}`, h.do(nethttp.MethodPost, "/v1/iam/users/2/deactivate", nil, ana).Body.String())
}

func TestCreateAndUpdateTakeAndAnswerExt(t *testing.T) {
	x := newDealers()
	h, ana := extHarness(t, x)

	created := h.do(nethttp.MethodPost, "/v1/iam/users", map[string]any{
		"login": "dora", "name": "Dora", "email": "dora@example.test", "locale": "hr", "password": "a long password",
		"ext": map[string]any{"dealer_id": 7},
	}, ana)
	require.Equal(t, nethttp.StatusOK, created.Code, created.Body.String())

	user := data(t, created)
	assert.Equal(t, map[string]any{"skipped": []any{}}, user["ext"], "what Create answered")

	id := int(user["id"].(float64))
	assert.Equal(t, []string{"create " + itoa(id) + ` {"dealer_id":7} by 1`}, x.take())
	assert.Equal(t, 7, x.dealer[id])

	updated := h.do(nethttp.MethodPut, "/v1/iam/users/"+itoa(id), map[string]any{
		"login": "dora", "name": "Dora", "email": "dora@example.test", "locale": "hr", "ext": map[string]any{"dealer_id": 8},
	}, ana)
	require.Equal(t, nethttp.StatusOK, updated.Code, updated.Body.String())
	assert.Equal(t, map[string]any{"dealer_id": 8.0}, data(t, updated)["ext"], "the record afterwards")
	assert.Equal(t, []string{"update " + itoa(id) + ` {"dealer_id":8} by 1`}, x.take(), "run although no account field differs")

	h.do(nethttp.MethodPut, "/v1/iam/users/"+itoa(id), map[string]any{
		"login": "dora", "name": "Dora", "email": "dora@example.test", "locale": "hr",
	}, ana)
	assert.Equal(t, []string{"update " + itoa(id) + "  by 1"}, x.take(), "with no ext in the body it is given none")
}

// A refusal by the extension is the answer, and a person is not changed by the account's own write either.
func TestAnExtensionRefusalIsTheAnswer(t *testing.T) {
	x := newDealers()
	x.refuse = apperr.Wrap(nil, "dealer_bindings", apperr.CodeAlreadyExists).WithReason("shop.user.dealer_bindings")
	h, ana := extHarness(t, x)

	out := h.do(nethttp.MethodPut, "/v1/iam/users/2", map[string]any{
		"login": "boris", "name": "Boris Novi", "email": "boris@example.test", "locale": "hr", "ext": map[string]any{"dealer_id": 1},
	}, ana)
	assert.Equal(t, nethttp.StatusConflict, out.Code)
	assert.Equal(t, "shop.user.dealer_bindings", out.json()["code"])

	dec := h.do(nethttp.MethodPost, "/v1/iam/users/2/deactivate", map[string]any{"details": map[string]any{"leads": 3}}, ana)
	assert.Equal(t, nethttp.StatusConflict, dec.Code)

	boris, err := h.kit.Accounts.Find(t.Context(), 2)
	require.NoError(t, err)
	assert.True(t, boris.Active, "a refused deactivation changed nothing")
}

func TestDeactivateGivesDetailsAndAnswersExt(t *testing.T) {
	x := newDealers()
	h, ana := extHarness(t, x)

	out := h.do(nethttp.MethodPost, "/v1/iam/users/2/deactivate", map[string]any{"details": map[string]any{"handover": map[string]any{"leads": 5}}}, ana)
	require.Equal(t, nethttp.StatusOK, out.Code, out.Body.String())
	assert.Equal(t, map[string]any{"moved": 3.0}, data(t, out)["ext"])
	assert.Equal(t, []string{`deactivate 2 {"handover":{"leads":5}} by 1`}, x.take())

	boris, err := h.kit.Accounts.Find(t.Context(), 2)
	require.NoError(t, err)
	assert.False(t, boris.Active, "go-core still deactivated")

	again := h.do(nethttp.MethodPost, "/v1/iam/users/2/deactivate", nil, ana)
	assert.Equal(t, nethttp.StatusConflict, again.Code)
	assert.Empty(t, x.take(), "an inactive person is refused before the extension is asked")
}

// extra query keys reach the Searcher, which is how an application filters its list.
type filterSpy struct {
	account.Searcher

	mu   sync.Mutex
	seen []account.Query
}

func (s *filterSpy) Search(ctx context.Context, q account.Query) (account.Page, error) {
	s.mu.Lock()
	s.seen = append(s.seen, q)
	s.mu.Unlock()

	return s.Searcher.Search(ctx, q)
}

func (s *filterSpy) Counts(ctx context.Context, q account.Query) (account.StatusCounts, error) {
	s.mu.Lock()
	s.seen = append(s.seen, q)
	s.mu.Unlock()

	return s.Searcher.Counts(ctx, q)
}

func TestExtraQueryKeysReachTheSearcher(t *testing.T) {
	spy := &filterSpy{}
	h := newHarness(t, nil, identitytest.WithSearcher(func(s account.Searcher) account.Searcher {
		spy.Searcher = s

		return spy
	}))
	ana := h.as("ana", "secret")

	out := h.do(nethttp.MethodGet, "/v1/iam/users?view=all&search=a&dealer_id=3&role=x&role=y&last_sign_in=never&per_page=5", nil, ana)
	require.Equal(t, nethttp.StatusOK, out.Code, out.Body.String())

	require.Len(t, spy.seen, 2, "the page and the counts")

	for _, q := range spy.seen {
		assert.Equal(t, url.Values{"dealer_id": {"3"}, "role": {"x", "y"}}, q.Filters, "only the keys identity has no field for")
		assert.Equal(t, account.LastSignInNever, q.LastSignIn)
		assert.Equal(t, "a", q.Search)
	}

	bad := h.do(nethttp.MethodGet, "/v1/iam/users?last_sign_in=sometimes", nil, ana)
	assert.Equal(t, nethttp.StatusUnprocessableEntity, bad.Code)
	assert.Equal(t, "identity.list.last_sign_in_invalid", bad.json()["code"])
}

// An administrator who sets their own password stays signed in; the others sessions of theirs end.
func TestSettingYourOwnPasswordKeepsThisSession(t *testing.T) {
	h := newHarness(t, nil)
	here := h.as("ana", "secret")
	other := h.as("ana", "secret")
	boris := h.as("boris", "hunter2")

	set := h.do(nethttp.MethodPut, "/v1/iam/users/1/password", map[string]string{"password": "a brand new one"}, here)
	require.Equal(t, nethttp.StatusNoContent, set.Code, set.Body.String())

	assert.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodGet, "/v1/auth/me", nil, here).Code, "this session stays")
	assert.Equal(t, nethttp.StatusUnauthorized, h.do(nethttp.MethodGet, "/v1/auth/me", nil, other).Code, "the other one ends")

	// somebody else's password: all of their sessions end, and the actor keeps theirs
	set = h.do(nethttp.MethodPut, "/v1/iam/users/2/password", map[string]string{"password": "another new one"}, here)
	require.Equal(t, nethttp.StatusNoContent, set.Code, set.Body.String())

	assert.Equal(t, nethttp.StatusUnauthorized, h.do(nethttp.MethodGet, "/v1/auth/me", nil, boris).Code)
	assert.Equal(t, nethttp.StatusOK, h.do(nethttp.MethodGet, "/v1/auth/me", nil, here).Code)
}

// The last sign-in filter reads the history: never, or idle past the time identity was given.
func TestTheLastSignInFilterOverHTTP(t *testing.T) {
	h := newHarness(t, nil, identitytest.WithIdleAfter(30*24*time.Hour), identitytest.WithConfig(account.Config{TokenTTL: 90 * 24 * time.Hour}))
	ana := h.as("ana", "secret") // signed in at the epoch

	logins := func(query string) []string {
		out := h.do(nethttp.MethodGet, "/v1/iam/users?view=all&"+query, nil, ana)
		require.Equal(t, nethttp.StatusOK, out.Code, out.Body.String())

		rows := changesRows(t, out)
		got := make([]string, len(rows))

		for i, row := range rows {
			got[i] = row["login"].(string)
		}

		return got
	}

	assert.Equal(t, []string{"boris", "ines"}, logins("last_sign_in=never"))
	assert.Empty(t, logins("last_sign_in=idle"), "ana signed in just now")

	h.kit.Clock.Advance(31 * 24 * time.Hour)
	assert.Equal(t, []string{"ana"}, logins("last_sign_in=idle"), "past 30 days")
	assert.Equal(t, []string{"boris", "ines"}, logins("last_sign_in=never"), "never is not idle")

	// the counts of the views follow the filter
	counts := h.do(nethttp.MethodGet, "/v1/iam/users?last_sign_in=never", nil, ana)
	body := counts.json()["data"].(map[string]any)
	assert.Equal(t, []any{
		map[string]any{"key": "active", "count": 1.0}, map[string]any{"key": "locked", "count": 0.0},
		map[string]any{"key": "inactive", "count": 1.0}, map[string]any{"key": "all", "count": 2.0},
	}, body["meta"].(map[string]any)["views"])
}

// ChangeEntry.Ext of a change log of the application's own reaches the row.
type extLog struct{ account.ChangeLog }

func (l extLog) Changes(ctx context.Context, q account.ChangeQuery) ([]account.ChangeEntry, int, error) {
	rows, total, err := l.ChangeLog.Changes(ctx, q)
	for i := range rows {
		rows[i].Ext = map[string]any{"source": "own"}
	}

	return rows, total, err
}

func TestAChangeRowCarriesItsExt(t *testing.T) {
	plain := newHarness(t, nil)
	ana := plain.as("ana", "secret")
	plain.do(nethttp.MethodPost, "/v1/iam/users/3/activate", nil, ana)
	assert.NotContains(t, plain.do(nethttp.MethodGet, "/v1/iam/users/3/changes", nil, ana).Body.String(), `"ext"`, "none by default")

	h := newHarness(t, nil, identitytest.WithChangeLogWrap(func(l account.ChangeLog) account.ChangeLog { return extLog{l} }))
	ana = h.as("ana", "secret")
	h.do(nethttp.MethodPost, "/v1/iam/users/3/activate", nil, ana)

	rows := changesRows(t, h.do(nethttp.MethodGet, "/v1/iam/users/3/changes", nil, ana))
	require.Len(t, rows, 1)
	assert.Equal(t, map[string]any{"source": "own"}, rows[0]["ext"])
}
