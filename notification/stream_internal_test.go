package notification

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
	"github.com/wssto2/go-core/authz/authztest"
	"github.com/wssto2/go-core/gocoretest"
	"github.com/wssto2/go-core/identity/account"
	identityhttp "github.com/wssto2/go-core/identity/http"
	"github.com/wssto2/go-core/identity/identitytest"
)

// An open stream re-checks, at each heartbeat, the identity session it was opened with: signing out,
// or ending every session, closes it, and the app reconnects through authentication, which refuses it.
func TestAnOpenStreamEndsWhenItsSessionIsRevoked(t *testing.T) {
	kit := identitytest.New(t, []account.Account{identitytest.Account(1, "ana", "secret")})

	app := gocoretest.New(t, gocoretest.Authorizer(authztest.AllowAll()))
	app.Authenticate(identityhttp.Authentication(kit.SignIn, identityhttp.Cookies{}, nil))

	cat := authz.NewCatalogue()
	require.NoError(t, DefinePermissions(cat))
	app.Permissions(cat)

	notices := Install(app, kit.Users)
	notices.heartbeat = 20 * time.Millisecond

	handler, err := app.Handler()
	require.NoError(t, err)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	signed, err := kit.SignIn.Login(t.Context(), account.LoginInput{Login: "ana", Password: "secret"})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL+"/v1/notifications/stream", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+signed.Credentials.Access)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	lines := make(chan string, 64)

	go func() {
		defer close(lines)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	require.Eventually(t, func() bool {
		select {
		case l := <-lines:
			return strings.HasPrefix(l, ": ping")
		default:
			return false
		}
	}, 5*time.Second, 5*time.Millisecond, "the session is live: the heartbeat goes out")

	require.Equal(t, 1, notices.Inbox.hub.Subscribers(1))

	require.NoError(t, kit.Users.RevokeSessions(t.Context(), account.RevokeSessionsInput{AccountID: 1}))

	require.Eventually(t, func() bool {
		for {
			select {
			case _, open := <-lines:
				if !open {
					return true // the server ended the response
				}
			default:
				return false
			}
		}
	}, 5*time.Second, 10*time.Millisecond, "the revoked session ends the stream")

	require.Eventually(t, func() bool { return notices.Inbox.hub.Subscribers(1) == 0 }, 5*time.Second, 10*time.Millisecond, "and its subscription")
}
