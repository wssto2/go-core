//nolint:goconst,varnamelen // the names of the people the tests seed
package identity_test

import (
	"context"
	"fmt"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/auth"
	authgorm "github.com/wssto2/go-core/auth/gormstore"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/bootstrap"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/database/dbtest"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/route"
	"gorm.io/gorm"
)

// hostUser is what the host's own authentication resolves a token to.
type hostUser int

func (u hostUser) GetID() int { return int(u) }

// forgetting is the whole wiring between identity and a host's token cache: when identity
// ends a person's sessions or changes the person, the host forgets what it cached.
type forgetting struct {
	account.DiscardNotices

	tokens *auth.DBTokenProvider
}

func (f forgetting) SessionsEnded(_ context.Context, e account.SessionsEnded) {
	f.tokens.InvalidateUser(e.AccountID)
}

func (f forgetting) AccountChanged(_ context.Context, id int) { f.tokens.InvalidateUser(id) }

// withHostRoute installs identity on app (with extra options), with the cache wired to it when wired, and mounts
// /host/ping behind a DBTokenProvider that caches verified tokens for 30 s over the same tokens table.
func withHostRoute(tb testing.TB, app *gocore.App, db *gorm.DB, wired bool, extra ...identity.Option) {
	tb.Helper()

	provider := auth.NewDBTokenProvider(authgorm.NewGormTokenStore(db),
		func(_ context.Context, id string) (auth.Identifiable, error) {
			n, err := strconv.Atoi(id)

			return hostUser(n), err
		}, nil, auth.WithVerifyCache(30*time.Second))
	tb.Cleanup(provider.Stop)

	opts := append([]identity.Option{identity.WithoutMail()}, extra...)
	if wired {
		opts = append(opts, identity.WithNotices(forgetting{DiscardNotices: account.DiscardNotices{}, tokens: provider}))
	}

	identity.Install(app, opts...)

	authenticate := auth.Authenticated(provider)
	ping := func(ctx *gin.Context) {
		if authenticate(ctx); !ctx.IsAborted() {
			ctx.JSON(nethttp.StatusOK, gin.H{"user": auth.MustGetUser[hostUser](ctx).GetID()})
		}
	}

	app.Routes(route.Raw(nethttp.MethodGet, "/host/ping").Public().To(ping))
}

func hostPing(tb testing.TB, web *client, access string) int {
	tb.Helper()

	req := httptest.NewRequestWithContext(tb.Context(), nethttp.MethodGet, "/host/ping", nil)
	req.Header.Set("Authorization", "Bearer "+access)

	rec := httptest.NewRecorder()
	web.handler.ServeHTTP(rec, req)

	return rec.Code
}

func accessOf(web *client) string {
	for _, ck := range web.cookies {
		if ck.Name == "access_token" {
			return ck.Value
		}
	}

	return ""
}

// hostAppOn starts an application with identity on db and the host's route.
func hostAppOn(t *testing.T, conn *gorm.DB, wired bool) *gocore.App {
	t.Helper()

	quiet := slog.New(slog.DiscardHandler)
	reg := database.NewRegistry(quiet, database.RegistryConfig{}) //nolint:exhaustruct,exhaustruct_v5 // the defaults
	reg.AddConnection("local", conn)

	opts := []gocore.Option{
		gocore.WithRegistry(reg), gocore.WithLogger(quiet), gocore.WithAuthorizer(authztest.AllowAll()),
	}
	if conn.Name() == dbtest.SQLite.Name {
		opts = append(opts, gocore.WithAutoMigrate(t.Context()))
	}

	app := gocore.New(bootstrap.DefaultConfig(), opts...)
	cat := authz.NewCatalogue()
	require.NoError(t, identity.DefinePermissions(cat))
	app.Permissions(cat)

	withHostRoute(t, app, conn, wired, identity.On("local"))

	if conn.Name() != dbtest.SQLite.Name {
		require.NoError(t, app.Migrate(t.Context()))
	}

	return app
}

// A host that caches verified tokens hears identity: after logout or a refresh the old
// access token is refused by the host's route on the very next request. Without the
// wiring the cache keeps answering, which is the hole this closes.
func TestAHostsTokenCacheForgetsWhatIdentityEnds(t *testing.T) {
	t.Parallel()

	for _, wired := range []bool{false, true} {
		t.Run(fmt.Sprintf("wired=%v", wired), func(t *testing.T) {
			t.Parallel()

			dbtest.Run(t, func(t *testing.T, conn *gorm.DB) {
				t.Helper()

				app := hostAppOn(t, conn, wired)
				seed(t, conn, identitytest.Account(1, "ana", "secret"))

				web := newClient(t, app)
				login(t, web, "ana", "secret")

				// after a refresh
				old := accessOf(web)
				require.NotEmpty(t, old)
				require.Equal(t, nethttp.StatusOK, hostPing(t, web, old), "the host accepts it, and caches it")
				require.Equal(t, nethttp.StatusOK, web.do(nethttp.MethodPost, "/v1/auth/refresh", nil).Code)

				fresh := accessOf(web)
				require.NotEqual(t, old, fresh)
				assert.Equal(t, nethttp.StatusOK, hostPing(t, web, fresh))
				assert.Equal(t, answer(wired), hostPing(t, web, old), "the replaced access token on the host's route")

				// after logout
				require.Equal(t, nethttp.StatusNoContent, web.do(nethttp.MethodPost, "/v1/auth/logout", nil).Code)
				assert.Equal(t, answer(wired), hostPing(t, web, fresh), "the signed-out token on the host's route")
			})
		})
	}
}

// answer is what the host's route says to a token identity ended: refused at once when the
// cache hears identity, served from the cache (the hole) when it does not.
func answer(wired bool) int {
	if wired {
		return nethttp.StatusUnauthorized
	}

	return nethttp.StatusOK
}

// ExampleWithNotices_tokenCache shows the wiring: a host that caches verified tokens hands
// identity a Notices that calls the provider's InvalidateUser (forgetting, above).
func ExampleWithNotices_tokenCache() {
	t := &exampleT{} //nolint:exhaustruct,exhaustruct_v5 // as the other examples
	defer t.done()

	app := newApp(t, gocoretest.At(time.Now())) // the host's provider reads the real clock
	withHostRoute(t, app, app.Database(), true)
	seed(t, app.Database(), identitytest.Account(1, "ana", "secret"))

	web := newClient(t, app)
	web.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": "ana", "password": "secret"})

	token := accessOf(web)
	fmt.Println(hostPing(t, web, token))
	web.do(nethttp.MethodPost, "/v1/auth/logout", nil)
	fmt.Println(hostPing(t, web, token))
	// Output:
	// 200
	// 401
}
