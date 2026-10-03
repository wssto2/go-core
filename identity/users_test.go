package identity_test

import (
	"context"
	"regexp"
	"testing"

	nethttp "net/http"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/identity"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/identity/identitytest"
	"github.com/wssto2/go-core/identity/mailtext"
	"github.com/wssto2/go-core/mail"
)

const secret = "an-installation-secret-of-32-chars!"

// Mail is the one collaborator identity cannot default: without a sender, or the
// explicit WithoutMail, start-up stops and says what to pass.
func TestInstallNeedsAMailSenderOrWithoutMail(t *testing.T) {
	for name, tc := range map[string]struct {
		opts []identity.Option
		want string
	}{
		"nothing":                {nil, "identity needs a mail.Sender"},
		"both":                   {[]identity.Option{identity.WithMail(mail.NewSink()), identity.WithoutMail()}, "both WithMail and WithoutMail"},
		"a sender, no secret":    {[]identity.Option{identity.WithMail(mail.NewSink())}, "identity.WithCodeSecret"},
		"a sender, short secret": {[]identity.Option{identity.WithMail(mail.NewSink()), identity.WithCodeSecret("short")}, "identity.WithCodeSecret"},
	} {
		app := newApp(t)
		identity.Install(app, tc.opts...)

		err := app.Check()
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tc.want, name)
		assert.Contains(t, err.Error(), "Fix:", name)
	}

	app := newApp(t)
	identity.Install(app, identity.WithoutMail())
	require.NoError(t, app.Check())

	app = newApp(t)
	identity.Install(app, identity.WithMail(mail.NewSink()), identity.WithCodeSecret(secret))
	require.NoError(t, app.Check())
}

func TestAStoreThatCannotListIsRefusedWithTheFix(t *testing.T) {
	app := newApp(t)
	identity.Install(app, identity.WithoutMail(), identity.WithAccounts(storeWithoutSearch{identitytest.NewAccounts()}))

	err := app.Check()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account.Searcher")
}

// storeWithoutSearch hides the Search method of a memory store.
type storeWithoutSearch struct{ account.Store }

func login(t *testing.T, c *client, name, password string) {
	t.Helper()

	r := c.do(nethttp.MethodPost, "/v1/auth/login", map[string]string{"login": name, "password": password})
	require.Equal(t, nethttp.StatusOK, r.Code, r.Body.String())
}

// With a sender, the e-mail change of a person works end to end through the real
// tables: the code arrives by mail, the old address is told, the history is in audit_logs.
func TestEmailChangeThroughTheInstalledModule(t *testing.T) {
	app := newApp(t)
	sink := mail.NewSink()

	users := identity.Install(app, identity.WithMail(sink), identity.WithCodeSecret(secret))

	ana := identitytest.Account(1, "ana", "secret")
	ana.Name, ana.Email = "Ana Anić", "ana@old.example"
	seed(t, app.Database(), ana)

	c := newClient(t, app)
	login(t, c, "ana", "secret")

	req := c.do(nethttp.MethodPost, "/v1/iam/profile/email", map[string]string{"email": "Ana@New.example", "current_password": "secret"})
	require.Equal(t, nethttp.StatusOK, req.Code, req.Body.String())

	sent := sink.To("ana@new.example")
	require.Len(t, sent, 1, "the code goes to the new address")
	assert.Equal(t, "Your confirmation code", sent[0].Subject)
	assert.Contains(t, sent[0].Text, "Hello Ana Anić")
	assert.NotEmpty(t, sent[0].HTML)

	code := regexp.MustCompile(`\b\d{6}\b`).FindString(sent[0].Text)
	require.NotEmpty(t, code, "six digits in the text")
	assert.Contains(t, sent[0].Text, "15 minutes")

	assert.NotContains(t, req.Body.String(), code)

	confirm := c.do(nethttp.MethodPost, "/v1/iam/profile/email/confirm", map[string]string{"code": code})
	require.Equal(t, nethttp.StatusOK, confirm.Code, confirm.Body.String())
	assert.Contains(t, confirm.Body.String(), "ana@new.example")

	told := sink.To("ana@old.example")
	require.Len(t, told, 1, "the previous address is told")
	assert.Equal(t, "The e-mail address of your account was changed", told[0].Subject)
	assert.Contains(t, told[0].Text, "a***@new.example", "with the new one masked")
	assert.NotContains(t, told[0].Text, "ana@new.example")

	// the change is on the history in the audit trail, which Install made the table for
	changes, err := users.Admin().Changes(t.Context(), 1, account.ChangesAll, account.Paging{})
	require.NoError(t, err)
	require.Equal(t, 1, changes.Total)
	assert.Equal(t, account.ChangeEmail, changes.Rows[0].Action)
	assert.Equal(t, map[string]string{"email": "ana@old.example"}, changes.Rows[0].Before)
}

func TestAPasswordChangeIsMailedToTheAccount(t *testing.T) {
	app := newApp(t)
	sink := mail.NewSink()

	identity.Install(app, identity.WithMail(sink), identity.WithCodeSecret(secret))

	ana := identitytest.Account(1, "ana", "secret")
	ana.Name, ana.Email = "Ana Anić", "ana@example.test"
	seed(t, app.Database(), ana)

	c := newClient(t, app)
	login(t, c, "ana", "secret")

	r := c.do(nethttp.MethodPut, "/v1/iam/profile/password", map[string]string{
		"current_password": "secret", "new_password": "a better one", "new_password_confirmation": "a better one",
	})
	require.Equal(t, nethttp.StatusNoContent, r.Code, r.Body.String())

	sent := sink.To("ana@example.test")
	require.Len(t, sent, 1)
	assert.Equal(t, "Your password was changed", sent[0].Subject)
}

// A failing mail does not undo a change that is done.
func TestAFailingNoticeMailDoesNotUndoThePasswordChange(t *testing.T) {
	app := newApp(t)

	identity.Install(app, identity.WithMail(mail.SenderFunc(func(context.Context, mail.Message) error { return assert.AnError })), identity.WithCodeSecret(secret))

	ana := identitytest.Account(1, "ana", "secret")
	ana.Email = "ana@example.test"
	seed(t, app.Database(), ana)

	c := newClient(t, app)
	login(t, c, "ana", "secret")

	r := c.do(nethttp.MethodPut, "/v1/iam/profile/password", map[string]string{
		"current_password": "secret", "new_password": "a better one", "new_password_confirmation": "a better one",
	})
	assert.Equal(t, nethttp.StatusNoContent, r.Code, "the notice is best effort")

	req := c.do(nethttp.MethodPost, "/v1/iam/profile/email", map[string]string{"email": "new@example.test", "current_password": "a better one"})
	assert.Equal(t, nethttp.StatusInternalServerError, req.Code, "but the code that cannot be sent is an error")
	assert.Contains(t, req.Body.String(), "identity.code.not_delivered")
}

// The application's own text wins for the mails it writes; identity's English fills the rest.
func TestWithMailContentOverridesOnlyWhatItWrites(t *testing.T) {
	app := newApp(t)
	sink := mail.NewSink()

	mine := mail.RendererFunc(func(_ context.Context, locale string, name mail.Name, data any) (mail.Content, error) {
		if name != mailtext.EmailCode {
			return mail.Content{}, mail.ErrNoTemplate
		}

		d, _ := data.(mailtext.CodeData)

		return mail.Content{Subject: "Vaša šifra (" + locale + ")", Text: "Šifra: " + d.Code}, nil
	})

	identity.Install(app, identity.WithMail(sink), identity.WithCodeSecret(secret), identity.WithMailContent(mine))

	ana := identitytest.Account(1, "ana", "secret")
	ana.Email, ana.Locale = "ana@old.example", "hr"
	seed(t, app.Database(), ana)

	c := newClient(t, app)
	login(t, c, "ana", "secret")

	require.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodPost, "/v1/iam/profile/email", map[string]string{"email": "new@example.test", "current_password": "secret"}).Code)
	assert.Equal(t, "Vaša šifra (hr)", sink.Sent()[0].Subject, "the code is the application's text, in the person's language")

	r := c.do(nethttp.MethodPut, "/v1/iam/profile/password", map[string]string{
		"current_password": "secret", "new_password": "a better one", "new_password_confirmation": "a better one",
	})
	require.Equal(t, nethttp.StatusNoContent, r.Code)
	assert.Equal(t, "Your password was changed", sink.Sent()[1].Subject, "the notice falls back to identity's English")
}

// users.Admin() is how code creates the first administrator.
func TestUsersAdminCreatesAPersonWhoCanSignIn(t *testing.T) {
	app := newApp(t)
	users := identity.Install(app, identity.WithoutMail())

	_, err := users.Admin().Create(t.Context(), account.CreateAccount{
		Login: "root", Name: "Root", Email: "root@example.test", Locale: "en", Password: "a long password",
	})
	require.NoError(t, err)

	c := newClient(t, app)
	login(t, c, "root", "a long password")

	require.NotNil(t, users.Profile())
	assert.Equal(t, nethttp.StatusOK, c.do(nethttp.MethodGet, "/v1/iam/users?view=all", nil).Code)
}

// An application's hook can refuse a deactivation over HTTP, with a reason of its own.
func TestADeactivationHookRefusesOverHTTP(t *testing.T) {
	app := newApp(t)

	hook := identity.DeactivationHookFunc(func(_ context.Context, a account.Account, _ int) error {
		if a.Login == "boris" {
			return refusal()
		}

		return nil
	})

	users := identity.Install(app, identity.WithoutMail(), identity.WithDeactivationHook(hook))

	for _, login := range []string{"ana", "boris", "cvita"} {
		_, err := users.Admin().Create(t.Context(), account.CreateAccount{
			Login: login, Name: login, Email: login + "@example.test", Locale: "en", Password: "a long password",
		})
		require.NoError(t, err)
	}

	c := newClient(t, app)
	login(t, c, "ana", "a long password")

	veto := c.do(nethttp.MethodPost, "/v1/iam/users/2/deactivate", nil)
	assert.Equal(t, nethttp.StatusBadRequest, veto.Code)
	assert.Contains(t, veto.Body.String(), "crm.owns_leads")

	ok := c.do(nethttp.MethodPost, "/v1/iam/users/3/deactivate", nil)
	assert.Equal(t, nethttp.StatusNoContent, ok.Code, ok.Body.String())

}

func refusal() error {
	return apperr.BadRequest("owns open leads").WithReason("crm.owns_leads")
}

// Users.Mail is where identity's mail goes, for a feature that mails the same people: the sender it was
// given, the renderer with identity's defaults behind the application's, and nothing when there is no mail.
func TestUsersMailIsWhereIdentitysMailGoes(t *testing.T) {
	sink := mail.NewSink()
	app := newApp(t)
	users := identity.Install(app, identity.WithMail(sink), identity.WithCodeSecret(secret))

	m, ok := users.Mail()
	require.True(t, ok)
	require.NoError(t, m.Sender.Send(t.Context(), mail.Message{To: []string{"ana@example.test"}, Text: "hi"}))
	require.Len(t, sink.Sent(), 1)

	content, err := m.Renderer.Render(t.Context(), "en", mailtext.PasswordChanged, mailtext.PasswordChangedData{Name: "Ana"})
	require.NoError(t, err)
	require.Contains(t, content.Text, "Ana")

	_, err = m.Renderer.Render(t.Context(), "en", "tickets.assigned", nil)
	require.ErrorIs(t, err, mail.ErrNoTemplate, "a mail identity does not know is for the feature's own renderer")

	bare := identity.Install(newApp(t), identity.WithoutMail())
	_, ok = bare.Mail()
	require.False(t, ok, "WithoutMail: nothing to mail through")
}
