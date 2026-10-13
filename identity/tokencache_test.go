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

// withHostRoute installs identity on app, with the cache wired to it when wired, and mounts
// /host/ping behind a DBTokenProvider that caches verified tokens for 30 s over the same tokens table.
func withHostRoute(t testing.TB, app *gocore.App, db *gorm.DB, wired bool) {
	t.Helper()

	provider := auth.NewDBTokenProvider(authgorm.NewGormTokenStore(db),
		func(_ context.Context, id string) (auth.Identifiable, error) {
			n, err := strconv.Atoi(id)

			return hostUser(n), err
		}, nil, auth.WithVerifyCache(30*time.Second))
	t.Cleanup(provider.Stop)

	opts := []identity.Option{identity.WithoutMail(), identity.On("local")}
	if wired {
		opts = append(opts, identity.WithNotices(forgetting{tokens: provider}))
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

func hostPing(t testing.TB, c *client, access string) int {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/host/ping", nil)
	req.Header.Set("Authorization", "Bearer "+access)

	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)

	return rec.Code
}

func accessOf(c *client) string {
	for _, ck := range c.cookies {
		if ck.Name == "access_token" {
			return ck.Value
		}
	}

	return ""
}

// A host that caches verified tokens hears identity: after logout or a refresh the old
// access token is refused by the host's route on the very next request. Without the
// wiring the cache keeps answering, which is the hole this closes.
func TestAHostsTokenCacheForgetsWhatIdentityEnds(t *testing.T) {
	for _, wired := range []bool{false, true} {
		t.Run(fmt.Sprintf("wired=%v", wired), func(t *testing.T) {
			dbtest.Run(t, func(t *testing.T, db *gorm.DB) {
				reg := database.NewRegistry(slog.New(slog.DiscardHandler), database.RegistryConfig{})
				reg.AddConnection("local", db)

				opts := []gocore.Option{gocore.WithRegistry(reg), gocore.WithLogger(slog.New(slog.DiscardHandler)), gocore.WithAuthorizer(authztest.AllowAll())}
				if db.Name() == "sqlite" {
					opts = append(opts, gocore.WithAutoMigrate(t.Context()))
				}

				app := gocore.New(bootstrap.DefaultConfig(), opts...)
				cat := authz.NewCatalogue()
				require.NoError(t, identity.DefinePermissions(cat))
				app.Permissions(cat)

				withHostRoute(t, app, db, wired)

				if db.Name() != "sqlite" {
					require.NoError(t, app.Migrate(t.Context()))
				}

				seed(t, db, identitytest.Account(1, "ana", "secret"))

				c := newClient(t, app)
				login(t, c, "ana", "secret")

				// after a refresh
				old := accessOf(c)
				require.NotEmpty(t, old)
				require.Equal(t, nethttp.StatusOK, hostPing(t, c, old), "the host accepts it, and caches it")
				require.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodPost, "/v1/auth/refresh", nil).Code)

				fresh := accessOf(c)
				require.NotEqual(t, old, fresh)
				assert.Equal(t, nethttp.StatusOK, hostPing(t, c, fresh))

				if wired {
					assert.Equal(t, nethttp.StatusUnauthorized, hostPing(t, c, old), "the replaced access token dies on the host's route at once")
				} else {
					assert.Equal(t, nethttp.StatusOK, hostPing(t, c, old), "without the wiring the cache serves it")
				}

				// after logout
				require.Equal(t, nethttp.StatusNoContent, c.do(nethttp.MethodPost, "/v1/auth/logout", nil).Code)

				if wired {
					assert.Equal(t, nethttp.StatusUnauthorized, hostPing(t, c, fresh), "a signed-out token dies on the host's route at once")
				} else {
					assert.Equal(t, nethttp.StatusOK, hostPing(t, c, fresh), "without the wiring the cache serves it")
				}
			})
		})
	}
}

// ExampleWithNotices_tokenCache shows the wiring: a host that caches verified tokens hands
// identity a Notices that calls the provider's InvalidateUser (see forgetting above).
// It has no Output line, so it is compiled and not run; the test above runs it.
func ExampleWithNotices_tokenCache() {
	var db *gorm.DB // the host's connection

	tokens := auth.NewDBTokenProvider(authgorm.NewGormTokenStore(db), func(_ context.Context, id string) (auth.Identifiable, error) {
		n, err := strconv.Atoi(id)

		return hostUser(n), err
	}, nil, auth.WithVerifyCache(30*time.Second))

	var app *gocore.App

	identity.Install(app, identity.WithoutMail(), identity.WithNotices(forgetting{tokens: tokens}))
}
